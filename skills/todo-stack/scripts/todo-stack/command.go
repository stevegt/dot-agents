package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type options struct {
	root        string
	scope       string
	stack       string
	from        string
	to          string
	id          string
	depth       int
	afterID     string
	input       string
	output      string
	expect      stringList
	expectItem  string
	reason      string
	approval    string
	base        string
	changedURLs stringList
}

type application struct {
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	store   *store
	command string
	options options
}

var commands = map[string]bool{
	"init": true, "push": true, "pop": true, "peek": true, "empty": true,
	"list": true, "clear": true, "move": true, "validate": true, "graph": true,
}

var valueFlags = map[string]bool{
	"root": true, "scope": true, "stack": true, "from": true, "to": true,
	"id": true, "depth": true, "after-id": true, "input": true, "output": true,
	"expect": true, "expect-item": true, "reason": true, "approval": true,
	"base": true, "changed-url": true,
}

func (a *application) run(args []string) error {
	// Intent: Parse every command through one application object so the same
	// confined store, lock, recovery, and error classification apply. Source: standalone stack specification
	command, rest, err := extractCommand(args)
	if err != nil {
		return err
	}
	a.command = command
	flags := flag.NewFlagSet("todo-stack "+command, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.StringVar(&a.options.root, "root", "stacks", "stack root")
	flags.StringVar(&a.options.scope, "scope", "default", "stack scope")
	flags.StringVar(&a.options.stack, "stack", "default", "stack name")
	flags.StringVar(&a.options.from, "from", "", "source stack")
	flags.StringVar(&a.options.to, "to", "", "destination stack")
	flags.StringVar(&a.options.id, "id", "", "item ID")
	flags.IntVar(&a.options.depth, "depth", -1, "item depth from stack top")
	flags.StringVar(&a.options.afterID, "after-id", "", "destination anchor ID")
	flags.StringVar(&a.options.input, "input", "", "input file")
	flags.StringVar(&a.options.output, "output", "", "output file")
	flags.Var(&a.options.expect, "expect", "expected stack revision stack=CID")
	flags.StringVar(&a.options.expectItem, "expect-item", "", "expected item revision CID")
	flags.StringVar(&a.options.reason, "reason", "", "move or clear reason")
	flags.StringVar(&a.options.approval, "approval", "", "approval evidence")
	flags.StringVar(&a.options.base, "base", "", "prior graph for semantic comparison")
	flags.Var(&a.options.changedURLs, "changed-url", "linked Markdown URL changed in this revision")
	if err := flags.Parse(rest); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: unexpected arguments %s", errUsage, strings.Join(flags.Args(), " "))
	}
	create := command == "init"
	value, err := openStore(a.options.root, a.options.scope, create)
	if err != nil {
		return err
	}
	a.store = value
	if err := value.lock(); err != nil {
		return err
	}
	switch command {
	case "init":
		return a.runInit()
	case "push":
		return a.runPush()
	case "pop":
		return a.runPop()
	case "peek":
		return a.runPeek()
	case "empty":
		return a.runEmpty()
	case "list":
		return a.runList()
	case "clear":
		return a.runClear()
	case "move":
		return a.runMove()
	case "validate":
		return a.runValidate()
	case "graph":
		return a.runGraph()
	default:
		return fmt.Errorf("%w: unsupported command %s", errUsage, command)
	}
}

func extractCommand(args []string) (string, []string, error) {
	command := ""
	commandIndex := -1
	for index := 0; index < len(args); index++ {
		value := args[index]
		if strings.HasPrefix(value, "-") {
			name := strings.TrimLeft(value, "-")
			if equals := strings.IndexByte(name, '='); equals >= 0 {
				name = name[:equals]
			}
			if valueFlags[name] && !strings.Contains(value, "=") {
				index++
			}
			continue
		}
		if commands[value] {
			if command != "" {
				return "", nil, fmt.Errorf("%w: expected one command, found %s and %s", errUsage, command, value)
			}
			command = value
			commandIndex = index
		}
	}
	if command == "" {
		return "", nil, fmt.Errorf("%w: expected one command: init, push, pop, peek, empty, list, clear, move, validate, or graph", errUsage)
	}
	rest := append([]string{}, args[:commandIndex]...)
	rest = append(rest, args[commandIndex+1:]...)
	return command, rest, nil
}

func (a *application) runInit() error {
	if err := a.store.initStacks(); err != nil {
		return err
	}
	values, err := a.store.loadAll()
	if err != nil {
		return err
	}
	lines, err := revisions(values)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(a.stdout, strings.Join(lines, "\n")); err != nil {
		return err
	}
	return nil
}

func (a *application) runPush() error {
	if !isStackName(a.options.stack) {
		return fmt.Errorf("%w: unsupported stack %q", errUsage, a.options.stack)
	}
	if a.options.stack == "wontfix" {
		return fmt.Errorf("%w: push cannot create wontfix items; move an existing item with approval", errConflict)
	}
	if a.options.id != "" || a.options.afterID != "" {
		return fmt.Errorf("%w: push has no --id or --after-id form", errUsage)
	}
	input, err := a.readInput()
	if err != nil {
		return err
	}
	incoming, err := parseStack(a.options.stack, input, true)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if len(incoming.Items) == 0 {
		return fmt.Errorf("%w: push input has no items", errUsage)
	}
	for _, it := range incoming.Items {
		if it.ID != "new" {
			return fmt.Errorf("%w: push input item IDs must be new", errUsage)
		}
	}
	values, err := a.store.loadAll()
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := checkExpectations(values, parseExpectations(a.options.expect)); err != nil {
		return err
	}
	if err := allocateIDs(a.options.scope, values, incoming.Items); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	target := values[a.options.stack]
	target.Items = append(target.Items, incoming.Items...)
	values[a.options.stack] = target
	if err := validateCollection(a.options.scope, values); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	entry := logEntry{Stacks: []string{a.options.stack}, Outcome: "success"}
	for _, it := range incoming.Items {
		entry.ItemIDs = append(entry.ItemIDs, it.ID)
	}
	if err := a.store.mutate("push", map[string][]byte{a.store.stackPath(a.options.stack): canonicalStack(target)}, entry); err != nil {
		return err
	}
	rev, err := stackRevision(target)
	if err != nil {
		return err
	}
	for _, it := range incoming.Items {
		if err := emitItem(a.stdout, it, rev); err != nil {
			return err
		}
	}
	return nil
}

func (a *application) runPeek() error {
	value, err := a.store.loadStack(a.options.stack)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	index, err := selectedIndex(value, a.options.id, a.options.depth)
	if err != nil {
		return err
	}
	rev, err := stackRevision(value)
	if err != nil {
		return err
	}
	if err := emitItem(a.stdout, value.Items[index], rev); err != nil {
		return err
	}
	return a.store.logResult(logEntry{Command: "peek", Stacks: []string{a.options.stack}, ItemIDs: []string{value.Items[index].ID}, Outcome: "success"})
}

func (a *application) runList() error {
	value, err := a.store.loadStack(a.options.stack)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := emitStack(a.stdout, value); err != nil {
		return err
	}
	return a.store.logResult(logEntry{Command: "list", Stacks: []string{a.options.stack}, Outcome: "success"})
}

func (a *application) runEmpty() error {
	value, err := a.store.loadStack(a.options.stack)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if _, err := fmt.Fprintln(a.stdout, len(value.Items) == 0); err != nil {
		return err
	}
	return a.store.logResult(logEntry{Command: "empty", Stacks: []string{a.options.stack}, Outcome: "success"})
}

func (a *application) runPop() error {
	if a.options.depth >= 0 {
		return fmt.Errorf("%w: pop accepts --id, not --depth", errUsage)
	}
	if a.options.stack == "wontfix" && (a.options.reason == "" || a.options.approval == "") {
		return fmt.Errorf("%w: removing wontfix evidence requires --reason and --approval", errConflict)
	}
	values, err := a.store.loadAll()
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := checkExpectations(values, parseExpectations(a.options.expect)); err != nil {
		return err
	}
	value := values[a.options.stack]
	index, err := selectedIndex(value, a.options.id, -1)
	if err != nil {
		return err
	}
	selected := value.Items[index]
	if err := checkItemExpectation(selected, a.options.expectItem); err != nil {
		return err
	}
	rev, err := stackRevision(value)
	if err != nil {
		return err
	}
	var output bytes.Buffer
	if err := emitItem(&output, selected, rev); err != nil {
		return err
	}
	if err := a.writeOutput(output.Bytes()); err != nil {
		return err
	}
	value.Items = append(value.Items[:index], value.Items[index+1:]...)
	if err := a.store.mutate("pop", map[string][]byte{a.store.stackPath(a.options.stack): canonicalStack(value)}, logEntry{
		Stacks: []string{a.options.stack}, ItemIDs: []string{selected.ID}, Outcome: "success",
	}); err != nil {
		return err
	}
	return nil
}

func (a *application) runClear() error {
	if strings.TrimSpace(a.options.reason) == "" || strings.TrimSpace(a.options.approval) == "" {
		return fmt.Errorf("%w: clear requires --reason and --approval", errConflict)
	}
	values, err := a.store.loadAll()
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	expected := parseExpectations(a.options.expect)
	if _, ok := expected[a.options.stack]; !ok {
		return fmt.Errorf("%w: clear requires --expect %s=CID", errConflict, a.options.stack)
	}
	if err := checkExpectations(values, expected); err != nil {
		return err
	}
	value := values[a.options.stack]
	ids := make([]string, 0, len(value.Items))
	for _, it := range value.Items {
		ids = append(ids, it.ID)
	}
	value.Items = nil
	return a.store.mutate("clear", map[string][]byte{a.store.stackPath(a.options.stack): nil}, logEntry{
		Stacks: []string{a.options.stack}, ItemIDs: ids, Outcome: "success",
	})
}

func (a *application) runMove() error {
	if !isStackName(a.options.from) || !isStackName(a.options.to) {
		return fmt.Errorf("%w: move requires supported --from and --to stacks", errUsage)
	}
	if a.options.from == "wontfix" && a.options.to != "wontfix" {
		return fmt.Errorf("%w: wontfix is permanent; items cannot move out", errConflict)
	}
	if a.options.from != "wontfix" && a.options.to == "wontfix" && (strings.TrimSpace(a.options.reason) == "" || strings.TrimSpace(a.options.approval) == "") {
		return fmt.Errorf("%w: moving to wontfix requires --reason and --approval", errConflict)
	}
	if a.options.from != "blocked" && a.options.to == "blocked" && strings.TrimSpace(a.options.reason) == "" {
		return fmt.Errorf("%w: moving to blocked requires --reason naming the wait", errConflict)
	}
	if a.options.from == "blocked" && a.options.to == "default" && strings.TrimSpace(a.options.reason) == "" {
		return fmt.Errorf("%w: restoring blocked work requires --reason naming verified evidence", errConflict)
	}
	values, err := a.store.loadAll()
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := checkExpectations(values, parseExpectations(a.options.expect)); err != nil {
		return err
	}
	source := values[a.options.from]
	sourceIndex, err := selectedIndex(source, a.options.id, -1)
	if err != nil {
		return err
	}
	selected := source.Items[sourceIndex]
	if err := checkItemExpectation(selected, a.options.expectItem); err != nil {
		return err
	}
	if a.options.from != "wontfix" && a.options.to == "wontfix" {
		selected.Details = appendDetail(selected.Details, "Permanent abandonment reason: "+strings.TrimSpace(a.options.reason))
		selected.Details = appendDetail(selected.Details, "Approval evidence: "+strings.TrimSpace(a.options.approval))
	} else if a.options.from != "blocked" && a.options.to == "blocked" {
		selected.Details = appendDetail(selected.Details, "Blocked waiting for: "+strings.TrimSpace(a.options.reason))
	} else if a.options.from == "blocked" && a.options.to == "default" {
		selected.Details = appendDetail(selected.Details, "Verified unblock evidence: "+strings.TrimSpace(a.options.reason))
	}
	source.Items = append(source.Items[:sourceIndex], source.Items[sourceIndex+1:]...)
	destination := source
	if a.options.from != a.options.to {
		destination = values[a.options.to]
	}
	insertAt := len(destination.Items)
	if a.options.afterID != "" {
		anchor := findItem(destination.Items, a.options.afterID)
		if anchor < 0 {
			return fmt.Errorf("%w: destination anchor %s does not exist", errConflict, a.options.afterID)
		}
		if selected.ID == a.options.afterID && a.options.from == a.options.to {
			return fmt.Errorf("%w: item cannot anchor itself", errConflict)
		}
		insertAt = anchor
	}
	destination.Items = append(destination.Items, item{})
	copy(destination.Items[insertAt+1:], destination.Items[insertAt:])
	destination.Items[insertAt] = selected
	values[a.options.from] = source
	values[a.options.to] = destination
	if a.options.from == a.options.to {
		values[a.options.from] = destination
	}
	if err := validateCollection(a.options.scope, values); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	files := map[string][]byte{a.store.stackPath(a.options.from): canonicalStack(values[a.options.from])}
	if a.options.from != a.options.to {
		files[a.store.stackPath(a.options.to)] = canonicalStack(values[a.options.to])
	}
	return a.store.mutate("move", files, logEntry{
		Stacks: []string{a.options.from, a.options.to}, ItemIDs: []string{selected.ID}, Outcome: "success",
	})
}

func (a *application) runValidate() error {
	values, err := a.store.loadAll()
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := validateURLs(a.store.rootPath, values); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	lines, err := revisions(values)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(a.stdout, strings.Join(lines, "\n")); err != nil {
		return err
	}
	return a.store.logResult(logEntry{Command: "validate", Stacks: append([]string(nil), stackOrder...), Outcome: "success"})
}

func (a *application) runGraph() error {
	values, err := a.store.loadAll()
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := validateURLs(a.store.rootPath, values); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	data, err := generateGraph(a.options.scope, values, a.options.base, a.options.changedURLs)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if a.options.output == "" {
		if _, err := a.stdout.Write(data); err != nil {
			return err
		}
	} else if err := writeGraphOutput(a.store.rootPath, a.options.output, data); err != nil {
		return err
	}
	return a.store.logResult(logEntry{Command: "graph", Stacks: append([]string(nil), stackOrder...), Outcome: "success"})
}

func selectedIndex(value stack, id string, depth int) (int, error) {
	if id != "" && depth >= 0 {
		return -1, fmt.Errorf("%w: --id and --depth are mutually exclusive", errUsage)
	}
	if len(value.Items) == 0 {
		return -1, fmt.Errorf("%w: stack %s is empty", errConflict, value.Name)
	}
	if id != "" {
		index := findItem(value.Items, id)
		if index < 0 {
			return -1, fmt.Errorf("%w: item %s is not in stack %s", errConflict, id, value.Name)
		}
		return index, nil
	}
	if depth < 0 {
		depth = 0
	}
	index := len(value.Items) - 1 - depth
	if index < 0 {
		return -1, fmt.Errorf("%w: depth %d exceeds stack %s", errConflict, depth, value.Name)
	}
	return index, nil
}

func checkItemExpectation(value item, expected string) error {
	if expected == "" {
		return nil
	}
	actual, err := itemRevision(value)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%w: item %s is %s, expected %s", errConflict, value.ID, actual, expected)
	}
	return nil
}

func parseExpectations(values []string) map[string]string {
	result := map[string]string{}
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		} else {
			result[value] = ""
		}
	}
	return result
}

func (a *application) readInput() ([]byte, error) {
	if a.options.input == "" {
		return io.ReadAll(a.stdin)
	}
	info, err := os.Lstat(a.options.input)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refuse symlink input %s", a.options.input)
	}
	return os.ReadFile(a.options.input)
}

func (a *application) writeOutput(data []byte) error {
	if a.options.output == "" {
		_, err := a.stdout.Write(data)
		return err
	}
	abs, err := filepath.Abs(a.options.output)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symlink output %s", abs)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(abs)
	temp, err := os.CreateTemp(dir, ".todo-stack-output-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	// Intent: A failed output replacement must report whether its temporary
	// file could also be removed; otherwise failed cleanup becomes invisible.
	// Source: standalone stack specification
	cleanup := func() error { return os.Remove(name) }
	if _, err := temp.Write(data); err != nil {
		err = errors.Join(err, temp.Close())
		return errors.Join(err, cleanup())
	}
	if err := temp.Sync(); err != nil {
		err = errors.Join(err, temp.Close())
		return errors.Join(err, cleanup())
	}
	if err := temp.Close(); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := os.Rename(name, abs); err != nil {
		return errors.Join(err, cleanup())
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(dirFile.Sync(), dirFile.Close())
}
