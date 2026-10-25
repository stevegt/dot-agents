package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMarkdownRoundTrip(t *testing.T) {
	value := sampleItem("new", "alpha", nil)
	data := canonicalStack(stack{Name: "default", Items: []item{value}})
	parsed, err := parseStack("default", data, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicalStack(parsed); !bytes.Equal(got, data) {
		t.Fatalf("round trip changed Markdown\nwant:\n%s\ngot:\n%s", data, got)
	}
	bad := bytes.Replace(data, []byte("- Kind: task"), []byte("- Kind: mystery"), 1)
	if _, err := parseStack("default", bad, true); err == nil {
		t.Fatal("invalid graph kind was accepted")
	}
	// Intent: Prove long visible details survive parsing instead of hitting the
	// former scanner token ceiling. Source: standalone stack specification
	value.Details = strings.Repeat("long details ", 7000)
	data = canonicalStack(stack{Name: "default", Items: []item{value}})
	parsed, err = parseStack("default", data, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicalStack(parsed); !bytes.Equal(got, data) {
		t.Fatal("long Markdown line did not round trip")
	}
}

func TestCommandWorkflow(t *testing.T) {
	repo, root := testRepo(t)
	runOK(t, nil, "init", "--root", root, "--scope", "alpha")
	defaultRoot := filepath.Join(repo, "default-stacks")
	runOK(t, nil, "init", "--root", defaultRoot)
	if _, err := os.Stat(filepath.Join(defaultRoot, "default", "default.md")); err != nil {
		t.Fatalf("default scope was not initialized: %v", err)
	}

	batch := append(canonicalItem(sampleItem("new", "alpha", nil)), '\n')
	second := sampleItem("new", "beta", []prerequisite{{
		Ref: "alias:alpha", Class: "desired", Label: "finish alpha", URL: "item.md#alpha",
	}})
	batch = append(batch, canonicalItem(second)...)
	pushOutput := runOK(t, batch, "push", "--root", root, "--scope", "alpha", "--stack", "default")
	pushed, err := parseStack("default", stripMetadata(pushOutput), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pushed.Items) != 2 {
		t.Fatalf("push emitted %d items, want 2", len(pushed.Items))
	}
	alphaID, betaID := pushed.Items[0].ID, pushed.Items[1].ID

	listed := runOK(t, nil, "list", "--root", root, "--scope", "alpha", "--stack", "default")
	listedStack, err := parseStack("default", stripMetadata(listed), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{listedStack.Items[0].ID, listedStack.Items[1].ID}; got[0] != alphaID || got[1] != betaID {
		t.Fatalf("push order = %v, want [%s %s]", got, alphaID, betaID)
	}
	peeked := runOK(t, nil, "peek", "--root", root, "--scope", "alpha", "--stack", "default")
	if !bytes.Contains(peeked, []byte("# "+betaID+" —")) {
		t.Fatalf("peek did not return top item: %s", peeked)
	}
	if got := strings.TrimSpace(string(runOK(t, nil, "empty", "--root", root, "--scope", "alpha", "--stack", "default"))); got != "false" {
		t.Fatalf("empty = %q, want false", got)
	}

	if err := runError(nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "blocked", "--id", betaID); !errors.Is(err, errConflict) {
		t.Fatalf("move without blocked reason = %v, want conflict", err)
	}
	runOK(t, nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "blocked", "--id", betaID, "--reason", "wait for release")
	runOK(t, nil, "move", "--root", root, "--scope", "alpha", "--from", "blocked", "--to", "blocked", "--id", betaID)
	runOK(t, nil, "move", "--root", root, "--scope", "alpha", "--from", "blocked", "--to", "default", "--id", betaID, "--reason", "release abc is available")
	runOK(t, nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "default", "--id", alphaID, "--after-id", betaID)
	reordered := mustLoad(t, root, "alpha", "default")
	if reordered.Items[len(reordered.Items)-1].ID != betaID || reordered.Items[len(reordered.Items)-2].ID != alphaID {
		t.Fatalf("processing order was not beta then alpha: %#v", reordered.Items)
	}

	if err := runError(nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "wontfix", "--id", betaID, "--reason", "obsolete"); !errors.Is(err, errConflict) {
		t.Fatalf("wontfix without approval = %v, want conflict", err)
	}
	runOK(t, nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "wontfix", "--id", betaID, "--reason", "obsolete", "--approval", "chat approval")
	runOK(t, nil, "move", "--root", root, "--scope", "alpha", "--from", "wontfix", "--to", "wontfix", "--id", betaID)
	wontfix := mustLoad(t, root, "alpha", "wontfix")
	if !strings.Contains(wontfix.Items[0].Details, "Approval evidence: chat approval") {
		t.Fatal("wontfix move did not record approval")
	}
	wontfixRevision, _ := stackRevision(wontfix)
	runOK(t, nil, "clear", "--root", root, "--scope", "alpha", "--stack", "wontfix",
		"--expect", "wontfix="+wontfixRevision, "--reason", "test cleanup", "--approval", "test owner approval")

	outputFile := filepath.Join(repo, "popped.md")
	runOK(t, nil, "pop", "--root", root, "--scope", "alpha", "--stack", "default", "--id", alphaID, "--output", outputFile)
	if data, err := os.ReadFile(outputFile); err != nil || !bytes.Contains(data, []byte("# "+alphaID+" —")) {
		t.Fatalf("pop output = %q, %v", data, err)
	}
	if got := strings.TrimSpace(string(runOK(t, nil, "empty", "--root", root, "--scope", "alpha", "--stack", "default"))); got != "true" {
		t.Fatalf("empty after pop = %q, want true", got)
	}

	runOK(t, nil, "validate", "--root", root, "--scope", "alpha")

	runOK(t, canonicalItem(sampleItem("new", "gamma", nil)), "push", "--root", root, "--scope", "alpha", "--stack", "default")
	graphPath := filepath.Join(repo, "specs", "generated.dot")
	runOK(t, nil, "graph", "--root", root, "--scope", "alpha", "--output", graphPath)
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(graphData, []byte("digraph prereq")) || !bytes.Contains(graphData, []byte("gamma")) {
		t.Fatalf("unexpected graph:\n%s", graphData)
	}
	secondScopeRoot := filepath.Join(repo, "second-scope-stacks")
	runOK(t, nil, "init", "--root", secondScopeRoot, "--scope", "beta")
	runOK(t, canonicalItem(sampleItem("new", "delta", nil)), "push", "--root", secondScopeRoot, "--scope", "beta")
	secondGraphPath := filepath.Join(repo, "specs", "second-generated.dot")
	runOK(t, nil, "graph", "--root", secondScopeRoot, "--scope", "beta", "--output", secondGraphPath)
	secondGraph, err := os.ReadFile(secondGraphPath)
	if err != nil || !bytes.Contains(secondGraph, []byte(`scope "beta"`)) {
		t.Fatalf("arbitrary-scope graph = %q, %v", secondGraph, err)
	}
}

func TestCommandExtractionSkipsFlagValues(t *testing.T) {
	command, rest, err := extractCommand([]string{"--reason", "push", "move", "--from", "default", "--to", "default"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "move" || strings.Join(rest, " ") != "--reason push --from default --to default" {
		t.Fatalf("extracted command %q and arguments %q", command, rest)
	}
	if _, _, err := extractCommand([]string{"push", "move"}); !errors.Is(err, errUsage) {
		t.Fatalf("multiple commands = %v, want usage error", err)
	}
}

func TestStaleRevisionsAndItemSelection(t *testing.T) {
	_, root := testRepo(t)
	runOK(t, nil, "init", "--root", root, "--scope", "alpha")
	output := runOK(t, canonicalItem(sampleItem("new", "alpha", nil)), "push", "--root", root, "--scope", "alpha")
	values, err := parseStack("default", stripMetadata(output), false)
	if err != nil {
		t.Fatal(err)
	}
	id := values.Items[0].ID
	if err := runError(nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "completed",
		"--id", id, "--expect", "default=bafkqaaa"); !errors.Is(err, errConflict) {
		t.Fatalf("stale stack revision = %v, want conflict", err)
	}
	if err := runError(nil, "move", "--root", root, "--scope", "alpha", "--from", "default", "--to", "completed",
		"--id", id, "--expect-item", "bafkqaaa"); !errors.Is(err, errConflict) {
		t.Fatalf("stale item revision = %v, want conflict", err)
	}
}

func TestConcurrentPushes(t *testing.T) {
	_, root := testRepo(t)
	runOK(t, nil, "init", "--root", root, "--scope", "alpha")
	const count = 20
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			value := sampleItem("new", fmt.Sprintf("node_%02d", index), nil)
			errs <- runError(canonicalItem(value), "push", "--root", root, "--scope", "alpha")
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	value := mustLoad(t, root, "alpha", "default")
	if len(value.Items) != count {
		t.Fatalf("concurrent pushes retained %d items, want %d", len(value.Items), count)
	}
	seen := map[string]bool{}
	for _, it := range value.Items {
		if seen[it.ID] {
			t.Fatalf("duplicate concurrent ID %s", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestJournalRecoveryPhases(t *testing.T) {
	points := []struct {
		name    string
		applied bool
	}{
		{name: "images-synced", applied: false},
		{name: "journal-synced", applied: true},
		{name: "file-installed:alpha/default.md", applied: true},
		{name: "log-synced", applied: true},
	}
	for _, point := range points {
		t.Run(point.name, func(t *testing.T) {
			_, root := testRepo(t)
			runOK(t, nil, "init", "--root", root, "--scope", "alpha")
			s, err := openStore(root, "alpha", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.lock(); err != nil {
				t.Fatal(err)
			}
			value := stack{Name: "default", Items: []item{sampleItem("1", "alpha", nil)}}
			crash := errors.New("injected crash")
			crashHook = func(name string) error {
				if name == point.name {
					return crash
				}
				return nil
			}
			err = s.mutate("test", map[string][]byte{s.stackPath("default"): canonicalStack(value)}, logEntry{Command: "test", Outcome: "success"})
			if !errors.Is(err, crash) {
				t.Fatalf("mutation error = %v, want injected crash", err)
			}
			if err := s.close(); err != nil {
				t.Fatal(err)
			}
			crashHook = func(string) error { return nil }
			recovered, err := openStore(root, "alpha", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := recovered.lock(); err != nil {
				t.Fatal(err)
			}
			got, err := recovered.loadStack("default")
			if err != nil {
				t.Fatal(err)
			}
			if (len(got.Items) == 1) != point.applied {
				t.Fatalf("recovered item count %d, applied want %v", len(got.Items), point.applied)
			}
			logData, err := recovered.root.ReadFile(filepath.ToSlash(filepath.Join(recovered.runtimeDir(), "operations.jsonl")))
			if point.applied {
				if err != nil || bytes.Count(logData, []byte("\"Command\":\"test\"")) != 1 {
					t.Fatalf("operation log = %s, %v", logData, err)
				}
			}
			if err := recovered.close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	crashHook = func(string) error { return nil }
}

func TestGraphIDsMarkersAndValidation(t *testing.T) {
	repo, root := testRepo(t)
	alpha := sampleItem("1", "alpha", nil)
	beta := sampleItem("2", "beta", []prerequisite{{
		Ref: "alpha/1", Class: "desired", Label: "finish alpha", URL: "item.md#alpha",
	}})
	values := emptyStacks()
	values["completed"] = stack{Name: "completed", Items: []item{alpha}}
	values["default"] = stack{Name: "default", Items: []item{beta}}
	if err := validateCollection("alpha", values); err != nil {
		t.Fatal(err)
	}
	graph, err := generateGraph("alpha", values, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantID := edgeID("alpha", "beta")
	if !bytes.Contains(graph, []byte("id=\""+wantID+"\"")) {
		t.Fatalf("graph lacks endpoint-derived ID %s:\n%s", wantID, graph)
	}
	if bytes.Count(graph, []byte("color=\"#00c853\"")) != 3 {
		t.Fatalf("first graph should mark two nodes and one edge:\n%s", graph)
	}
	base := filepath.Join(repo, "specs", "base.dot")
	if err := os.WriteFile(base, graph, 0o600); err != nil {
		t.Fatal(err)
	}
	same, err := generateGraph("alpha", values, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(same, graph) {
		t.Fatalf("unchanged regeneration did not preserve graph byte for byte:\n%s", same)
	}
	changed, err := generateGraph("alpha", values, base, []string{"item.md#alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(changed, []byte("color=\"#00c853\"")) != 2 {
		t.Fatalf("changed URL should mark linked nodes and edge:\n%s", changed)
	}

	cyclic := emptyStacks()
	alpha.Prerequisites = []prerequisite{{Ref: "alpha/2", Class: "desired", Label: "beta", URL: "item.md"}}
	beta.Prerequisites = []prerequisite{{Ref: "alpha/1", Class: "desired", Label: "alpha", URL: "item.md"}}
	cyclic["default"] = stack{Name: "default", Items: []item{alpha, beta}}
	if err := validateCollection("alpha", cyclic); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle validation = %v", err)
	}

	values["default"] = stack{Name: "default", Items: []item{beta}}
	values["wontfix"] = stack{Name: "wontfix", Items: []item{alpha}}
	values["completed"] = stack{Name: "completed"}
	if err := validateCollection("alpha", values); err == nil || !strings.Contains(err.Error(), "wontfix") {
		t.Fatalf("runnable wontfix dependency = %v", err)
	}
	if err := validateSpecURL(root, "missing.md#x"); err == nil {
		t.Fatal("missing local URL was accepted")
	}
}

func TestLogsDoNotContainItemBodies(t *testing.T) {
	_, root := testRepo(t)
	runOK(t, nil, "init", "--root", root, "--scope", "alpha")
	value := sampleItem("new", "secret_node", nil)
	value.Details = "do not log SECRET-MARKER"
	runOK(t, canonicalItem(value), "push", "--root", root, "--scope", "alpha")
	runOK(t, nil, "list", "--root", root, "--scope", "alpha")
	data, err := os.ReadFile(filepath.Join(root, ".runtime", "alpha", "operations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("SECRET-MARKER")) {
		t.Fatalf("operation log contains item body:\n%s", data)
	}
	bad := fmt.Errorf("%w: invalid Graph entry SECRET-MARKER", errUsage)
	if category := failureCategory(bad); strings.Contains(category, "SECRET-MARKER") || category != "invalid input" {
		t.Fatalf("failure category leaked item text: %q", category)
	}
}

func sampleItem(id, graphName string, prereqs []prerequisite) item {
	return item{
		ID: id, Title: "Work on " + graphName, Actor: "alpha",
		Purpose:         "Complete " + graphName,
		RequestedResult: "Produce the requested result.",
		Acceptance:      []string{"Requested result exists."},
		Graph:           graphMeta{Name: graphName, Kind: "task", URL: "item.md#" + graphName},
		Prerequisites:   prereqs,
		SourceEvidence:  []string{"test fixture"},
		Details:         "Test details.",
	}
}

func emptyStacks() map[string]stack {
	values := map[string]stack{}
	for _, name := range stackOrder {
		values[name] = stack{Name: name}
	}
	return values
}

func testRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# alpha\n\n# beta\n\n# gamma\n\n# node\n"
	if err := os.WriteFile(filepath.Join(repo, "specs", "item.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo, filepath.Join(repo, "stacks")
}

func runOK(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := &application{stdin: bytes.NewReader(input), stdout: &stdout, stderr: &stderr}
	err := app.run(args)
	if app.store != nil {
		closeErr := app.store.close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		t.Fatalf("command %v failed: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.Bytes()
}

func runError(input []byte, args ...string) error {
	var stdout, stderr bytes.Buffer
	app := &application{stdin: bytes.NewReader(input), stdout: &stdout, stderr: &stderr}
	err := app.run(args)
	if app.store != nil {
		err = errors.Join(err, app.store.close())
	}
	return err
}

func mustLoad(t *testing.T, root, scope, name string) stack {
	t.Helper()
	s, err := openStore(root, scope, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.close(); err != nil {
			t.Errorf("close stack store: %v", err)
		}
	}()
	if err := s.lock(); err != nil {
		t.Fatal(err)
	}
	value, err := s.loadStack(name)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustLoadAll(t *testing.T, root, scope string) map[string]stack {
	t.Helper()
	s, err := openStore(root, scope, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.close(); err != nil {
			t.Errorf("close stack store: %v", err)
		}
	}()
	if err := s.lock(); err != nil {
		t.Fatal(err)
	}
	value, err := s.loadAll()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
