package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type store struct {
	rootPath string
	root     *os.Root
	scope    string
	lockFile *os.File
}

type journal struct {
	OperationID string
	Command     string
	Files       []journalFile
	Log         logEntry
}

type journalFile struct {
	Path     string
	Image    string
	Revision string
}

type logEntry struct {
	Time        string
	OperationID string
	Command     string
	Stacks      []string
	ItemIDs     []string
	Outcome     string
	Error       string
}

var crashHook = func(string) error { return nil }

func openStore(rootPath, scope string, create bool) (*store, error) {
	// Intent: Confine every helper-managed path beneath one non-symlink stack
	// root before creating locks, journals, or tracked stack files. Source: standalone stack specification
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(abs); err != nil {
		if !errors.Is(err, fs.ErrNotExist) || !create {
			return nil, fmt.Errorf("open stack root: %w", err)
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, fmt.Errorf("create stack root: %w", err)
		}
		info, err = os.Lstat(abs)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("stack root is a symlink")
		}
	} else if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("stack root is a symlink")
	} else if !info.IsDir() {
		return nil, fmt.Errorf("stack root is not a directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open confined stack root: %w", err)
	}
	s := &store{rootPath: abs, root: root, scope: scope}
	if create {
		if err := root.MkdirAll(scope, 0o755); err != nil {
			return nil, fmt.Errorf("create scope stack directory: %w", errors.Join(err, root.Close()))
		}
	}
	if err := s.rejectSymlink(scope); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return s, nil
}

func (s *store) close() error {
	var first error
	if s.lockFile != nil {
		if err := unix.Flock(int(s.lockFile.Fd()), unix.LOCK_UN); err != nil {
			first = err
		}
		if err := s.lockFile.Close(); err != nil && first == nil {
			first = err
		}
		s.lockFile = nil
	}
	if err := s.root.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

func (s *store) runtimeDir() string { return filepath.ToSlash(filepath.Join(".runtime", s.scope)) }

func (s *store) stackPath(name string) string {
	return filepath.ToSlash(filepath.Join(s.scope, name+".md"))
}

func (s *store) lock() error {
	if err := s.root.MkdirAll(s.runtimeDir(), 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if err := s.rejectSymlink(".runtime"); err != nil {
		return err
	}
	if err := s.rejectSymlink(s.runtimeDir()); err != nil {
		return err
	}
	path := filepath.ToSlash(filepath.Join(s.runtimeDir(), "lock"))
	if err := s.rejectSymlinkIfPresent(path); err != nil {
		return err
	}
	file, err := s.root.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open scope lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("acquire scope lock: %w", errors.Join(err, file.Close()))
	}
	s.lockFile = file
	if err := s.recoverJournal(); err != nil {
		return err
	}
	return nil
}

func (s *store) rejectSymlink(path string) error {
	info, err := s.root.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symlink %s", path)
	}
	return nil
}

func (s *store) rejectSymlinkIfPresent(path string) error {
	info, err := s.root.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symlink %s", path)
	}
	return nil
}

func (s *store) initStacks() error {
	files := map[string][]byte{}
	for _, name := range stackOrder {
		path := s.stackPath(name)
		if err := s.rejectSymlinkIfPresent(path); err != nil {
			return err
		}
		if _, err := s.root.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		files[path] = nil
	}
	if len(files) == 0 {
		return nil
	}
	return s.mutate("init", files, logEntry{Command: "init", Stacks: append([]string(nil), stackOrder...), Outcome: "success"})
}

func (s *store) loadStack(name string) (stack, error) {
	if !isStackName(name) {
		return stack{}, fmt.Errorf("unsupported stack %q", name)
	}
	path := s.stackPath(name)
	if err := s.rejectSymlink(path); err != nil {
		return stack{}, err
	}
	data, err := s.root.ReadFile(path)
	if err != nil {
		return stack{}, fmt.Errorf("read %s: %w", path, err)
	}
	value, err := parseStack(name, data, false)
	if err != nil {
		return stack{}, err
	}
	if !bytes.Equal(data, canonicalStack(value)) {
		return stack{}, fmt.Errorf("stack %s is not in canonical Markdown form", name)
	}
	return value, nil
}

func (s *store) loadAll() (map[string]stack, error) {
	values := map[string]stack{}
	for _, name := range stackOrder {
		value, err := s.loadStack(name)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}
	if err := validateCollection(s.scope, values); err != nil {
		return nil, err
	}
	return values, nil
}

func validateCollection(scope string, stacks map[string]stack) error {
	ids := map[string]item{}
	graphs := map[string]string{}
	states := map[string]string{}
	for _, name := range stackOrder {
		value, ok := stacks[name]
		if !ok {
			return fmt.Errorf("missing stack %s", name)
		}
		for _, it := range value.Items {
			ref := scope + "/" + it.ID
			if _, exists := ids[ref]; exists {
				return fmt.Errorf("duplicate scope item ID %s", ref)
			}
			if other, exists := graphs[it.Graph.Name]; exists {
				return fmt.Errorf("graph name %s belongs to both %s and %s", it.Graph.Name, other, ref)
			}
			ids[ref] = it
			graphs[it.Graph.Name] = ref
			states[ref] = name
		}
	}
	dependencies := map[string][]string{}
	for ref, it := range ids {
		for _, p := range it.Prerequisites {
			resolved, err := canonicalRef(scope, p.Ref)
			if err != nil {
				return fmt.Errorf("item %s: %w", ref, err)
			}
			if strings.HasPrefix(resolved, scope+"/") {
				if _, exists := ids[resolved]; !exists {
					return fmt.Errorf("item %s references missing prerequisite %s", ref, resolved)
				}
				if states[resolved] == "wontfix" && states[ref] == "default" {
					return fmt.Errorf("runnable item %s depends on wontfix item %s", ref, resolved)
				}
			}
			dependencies[ref] = append(dependencies[ref], resolved)
		}
	}
	color := map[string]uint8{}
	var visit func(string) error
	visit = func(ref string) error {
		switch color[ref] {
		case 1:
			return fmt.Errorf("prerequisite cycle includes %s", ref)
		case 2:
			return nil
		}
		color[ref] = 1
		for _, dep := range dependencies[ref] {
			if _, local := ids[dep]; local {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[ref] = 2
		return nil
	}
	for ref := range ids {
		if err := visit(ref); err != nil {
			return err
		}
	}
	return nil
}

func canonicalRef(scope, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "alias:") {
		return "", fmt.Errorf("unresolved import alias %q", ref)
	}
	if !strings.Contains(ref, "/") {
		if !validID.MatchString(ref) {
			return "", fmt.Errorf("invalid prerequisite reference %q", ref)
		}
		return scope + "/" + ref, nil
	}
	parts := strings.Split(ref, "/")
	if len(parts) != 2 || validateScope(parts[0]) != nil || !validID.MatchString(parts[1]) {
		return "", fmt.Errorf("invalid prerequisite reference %q", ref)
	}
	return ref, nil
}

func (s *store) mutate(command string, files map[string][]byte, entry logEntry) error {
	// Intent: Install a durable forward-recovery record before replacing any
	// tracked stack so helper readers recover one selected final state.
	// Source: standalone stack specification
	opID, err := operationID()
	if err != nil {
		return err
	}
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	entry.OperationID = opID
	entry.Command = command
	entry.Outcome = "success"
	j := journal{OperationID: opID, Command: command, Log: entry}
	for _, path := range sortedKeys(files) {
		if err := s.rejectSymlinkIfPresent(path); err != nil {
			return err
		}
		image := filepath.ToSlash(filepath.Join(s.runtimeDir(), opID+"-"+strconv.Itoa(len(j.Files))+".image"))
		if err := s.writeSync(image, files[path], 0o600); err != nil {
			return errors.Join(err, s.removeImages(j.Files))
		}
		rev, err := revision(files[path])
		if err != nil {
			return errors.Join(err, s.removeImages(j.Files))
		}
		j.Files = append(j.Files, journalFile{Path: path, Image: image, Revision: rev})
	}
	if err := s.syncDir(s.runtimeDir()); err != nil {
		return errors.Join(err, s.removeImages(j.Files))
	}
	if err := crashHook("images-synced"); err != nil {
		return err
	}
	encoded, err := json.Marshal(j)
	if err != nil {
		return errors.Join(err, s.removeImages(j.Files))
	}
	tmp := filepath.ToSlash(filepath.Join(s.runtimeDir(), "journal.next"))
	journalPath := filepath.ToSlash(filepath.Join(s.runtimeDir(), "journal.json"))
	if err := s.writeSync(tmp, append(encoded, '\n'), 0o600); err != nil {
		return errors.Join(err, s.removeImages(j.Files))
	}
	if err := s.root.Rename(tmp, journalPath); err != nil {
		return errors.Join(fmt.Errorf("install journal: %w", err), s.removeImages(j.Files))
	}
	if err := s.syncDir(s.runtimeDir()); err != nil {
		return err
	}
	if err := crashHook("journal-synced"); err != nil {
		return err
	}
	return s.recoverJournal()
}

func (s *store) recoverJournal() error {
	// Intent: Complete the journaled replacement before serving another read or
	// write, including after any tested crash phase. Source: standalone stack specification
	path := filepath.ToSlash(filepath.Join(s.runtimeDir(), "journal.json"))
	if err := s.rejectSymlinkIfPresent(path); err != nil {
		return err
	}
	data, err := s.root.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s.removeOrphanRecoveryFiles()
	}
	if err != nil {
		return fmt.Errorf("read recovery journal: %w", err)
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("decode recovery journal: %w", err)
	}
	if j.OperationID == "" || len(j.Files) == 0 {
		return fmt.Errorf("invalid recovery journal")
	}
	for _, file := range j.Files {
		if err := s.rejectSymlinkIfPresent(file.Path); err != nil {
			return err
		}
		imageData, imageErr := s.root.ReadFile(file.Image)
		if imageErr != nil {
			if !errors.Is(imageErr, fs.ErrNotExist) {
				return fmt.Errorf("read recovery image: %w", imageErr)
			}
			current, currentErr := s.root.ReadFile(file.Path)
			if currentErr != nil {
				return fmt.Errorf("recovery image and target missing for %s", file.Path)
			}
			currentRevision, revErr := revision(current)
			if revErr != nil || currentRevision != file.Revision {
				return fmt.Errorf("recovery image missing before %s reached revision %s", file.Path, file.Revision)
			}
			continue
		}
		imageRevision, revErr := revision(imageData)
		if revErr != nil || imageRevision != file.Revision {
			return fmt.Errorf("recovery image revision mismatch for %s", file.Path)
		}
		if err := s.root.Rename(file.Image, file.Path); err != nil {
			return fmt.Errorf("install recovered image for %s: %w", file.Path, err)
		}
		if err := s.root.Chmod(file.Path, 0o644); err != nil {
			return fmt.Errorf("set recovered stack mode for %s: %w", file.Path, err)
		}
		if err := s.syncDir(filepath.ToSlash(filepath.Dir(file.Path))); err != nil {
			return err
		}
		if err := s.syncDir(s.runtimeDir()); err != nil {
			return err
		}
		if err := crashHook("file-installed:" + file.Path); err != nil {
			return err
		}
	}
	if err := s.appendLogOnce(j.Log); err != nil {
		return err
	}
	if err := crashHook("log-synced"); err != nil {
		return err
	}
	if err := s.root.Remove(path); err != nil {
		return fmt.Errorf("clear recovery journal: %w", err)
	}
	return s.syncDir(s.runtimeDir())
}

func (s *store) removeOrphanRecoveryFiles() error {
	dir, err := s.root.Open(s.runtimeDir())
	if err != nil {
		return err
	}
	names, err := dir.Readdirnames(-1)
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	changed := false
	for _, name := range names {
		if !strings.HasSuffix(name, ".image") && name != "journal.next" {
			continue
		}
		path := filepath.ToSlash(filepath.Join(s.runtimeDir(), name))
		if err := s.root.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		changed = true
	}
	if changed {
		return s.syncDir(s.runtimeDir())
	}
	return nil
}

func (s *store) removeImages(files []journalFile) error {
	var result error
	for _, file := range files {
		if err := s.root.Remove(file.Image); err != nil && !errors.Is(err, fs.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove recovery image %s: %w", file.Image, err))
		}
	}
	return result
}

func (s *store) appendLogOnce(entry logEntry) error {
	path := filepath.ToSlash(filepath.Join(s.runtimeDir(), "operations.jsonl"))
	if err := s.rejectSymlinkIfPresent(path); err != nil {
		return err
	}
	if existing, err := s.root.Open(path); err == nil {
		scanner := bufio.NewScanner(existing)
		for scanner.Scan() {
			var candidate logEntry
			if json.Unmarshal(scanner.Bytes(), &candidate) == nil && candidate.OperationID == entry.OperationID && entry.OperationID != "" {
				return existing.Close()
			}
		}
		if err := scanner.Err(); err != nil {
			return errors.Join(err, existing.Close())
		}
		if err := existing.Close(); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := s.root.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open operation log: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write operation log: %w", errors.Join(err, file.Close()))
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync operation log: %w", errors.Join(err, file.Close()))
	}
	return file.Close()
}

func (s *store) logResult(entry logEntry) error {
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if entry.OperationID == "" {
		id, err := operationID()
		if err != nil {
			return err
		}
		entry.OperationID = id
	}
	return s.appendLogOnce(entry)
}

func (s *store) writeSync(path string, data []byte, mode os.FileMode) error {
	if err := s.rejectSymlinkIfPresent(path); err != nil {
		return err
	}
	file, err := s.root.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", path, errors.Join(err, file.Close()))
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", path, errors.Join(err, file.Close()))
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func (s *store) syncDir(path string) error {
	dir, err := s.root.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %s for sync: %w", path, err)
	}
	if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}

func operationID() (string, error) {
	var random [4]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		return "", err
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + hex.EncodeToString(random[:]), nil
}

func allocateIDs(scope string, existing map[string]stack, values []item) error {
	used := map[string]bool{}
	graphToRef := map[string]string{}
	for _, value := range existing {
		for _, it := range value.Items {
			used[it.ID] = true
			graphToRef[it.Graph.Name] = scope + "/" + it.ID
		}
	}
	next := time.Now().UnixNano()
	batchGraphs := map[string]int{}
	for index := range values {
		if _, duplicate := batchGraphs[values[index].Graph.Name]; duplicate {
			return fmt.Errorf("batch has duplicate graph name %s", values[index].Graph.Name)
		}
		batchGraphs[values[index].Graph.Name] = index
		for used[strconv.FormatInt(next, 10)] {
			next++
		}
		values[index].ID = strconv.FormatInt(next, 10)
		used[values[index].ID] = true
		graphToRef[values[index].Graph.Name] = scope + "/" + values[index].ID
		next++
	}
	for index := range values {
		for p := range values[index].Prerequisites {
			ref := values[index].Prerequisites[p].Ref
			if !strings.HasPrefix(ref, "alias:") {
				continue
			}
			name := strings.TrimPrefix(ref, "alias:")
			resolved, ok := graphToRef[name]
			if !ok {
				return fmt.Errorf("item %s references unknown alias %s", values[index].Graph.Name, name)
			}
			values[index].Prerequisites[p].Ref = resolved
			values[index].SourceEvidence = append(values[index].SourceEvidence, "Import alias: "+name)
		}
	}
	return nil
}

func checkExpectations(stacks map[string]stack, expected map[string]string) error {
	for name, wanted := range expected {
		value, ok := stacks[name]
		if !ok {
			return fmt.Errorf("%w: expected revision names unknown stack %s", errConflict, name)
		}
		actual, err := stackRevision(value)
		if err != nil {
			return err
		}
		if actual != wanted {
			return fmt.Errorf("%w: stack %s is %s, expected %s", errConflict, name, actual, wanted)
		}
	}
	return nil
}

func emitItem(writer io.Writer, value item, stackRev string) error {
	itemRev, err := itemRevision(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "<!-- stack-revision: %s item-revision: %s -->\n", stackRev, itemRev); err != nil {
		return err
	}
	_, err = writer.Write(canonicalItem(value))
	return err
}

func emitStack(writer io.Writer, value stack) error {
	rev, err := stackRevision(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "<!-- stack-revision: %s -->\n", rev); err != nil {
		return err
	}
	_, err = writer.Write(canonicalStack(value))
	return err
}

func appendDetail(details, line string) string {
	details = strings.TrimSpace(details)
	if details == "" {
		return line
	}
	return details + "\n\n" + line
}

func revisions(stacks map[string]stack) ([]string, error) {
	var lines []string
	for _, name := range stackOrder {
		rev, err := stackRevision(stacks[name])
		if err != nil {
			return nil, err
		}
		lines = append(lines, name+"="+rev)
	}
	sort.Strings(lines)
	return lines, nil
}
