package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

var (
	validID        = regexp.MustCompile("^[0-9]+$")
	validScope     = regexp.MustCompile("^[a-z0-9][a-z0-9._-]*$")
	validGraphName = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")
)

var stackOrder = []string{"inbox", "default", "blocked", "completed", "wontfix"}

type graphMeta struct {
	Name string
	Kind string
	URL  string
}

type prerequisite struct {
	Ref   string
	Class string
	Label string
	URL   string
}

type item struct {
	ID              string
	Title           string
	Actor           string
	Purpose         string
	RequestedResult string
	Acceptance      []string
	Graph           graphMeta
	Prerequisites   []prerequisite
	SourceEvidence  []string
	Details         string
}

type stack struct {
	Name  string
	Items []item
}

func isStackName(name string) bool {
	for _, candidate := range stackOrder {
		if name == candidate {
			return true
		}
	}
	return false
}

func validateScope(scope string) error {
	if !validScope.MatchString(scope) || scope == "." || scope == ".." {
		return fmt.Errorf("invalid scope name %q", scope)
	}
	return nil
}

func validateItem(it item, allowNew bool) error {
	if it.ID == "" || (it.ID != "new" && !validID.MatchString(it.ID)) || (!allowNew && it.ID == "new") {
		return fmt.Errorf("invalid item ID %q", it.ID)
	}
	for field, value := range map[string]string{
		"title": it.Title, "actor": it.Actor, "purpose": it.Purpose,
		"requested result": it.RequestedResult, "graph name": it.Graph.Name,
		"graph kind": it.Graph.Kind, "graph URL": it.Graph.URL,
		"source evidence": strings.Join(it.SourceEvidence, "\n"),
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("item %s has empty %s", it.ID, field)
		}
	}
	if len(it.Acceptance) == 0 {
		return fmt.Errorf("item %s has no acceptance checks", it.ID)
	}
	if !validGraphName.MatchString(it.Graph.Name) {
		return fmt.Errorf("item %s has invalid graph name %q", it.ID, it.Graph.Name)
	}
	switch it.Graph.Kind {
	case "task", "decision", "terminal":
	default:
		return fmt.Errorf("item %s has invalid graph kind %q", it.ID, it.Graph.Kind)
	}
	for _, p := range it.Prerequisites {
		if strings.TrimSpace(p.Ref) == "" || strings.TrimSpace(p.Label) == "" || strings.TrimSpace(p.URL) == "" {
			return fmt.Errorf("item %s has incomplete prerequisite", it.ID)
		}
		if strings.Contains(p.Label, "|") {
			return fmt.Errorf("item %s prerequisite label contains |", it.ID)
		}
		if p.Class != "desired" && p.Class != "ordinary" {
			return fmt.Errorf("item %s prerequisite has invalid class %q", it.ID, p.Class)
		}
	}
	return nil
}

func canonicalItem(it item) []byte {
	// Intent: Emit one stable human-readable form so content IDs can detect
	// stale decisions without making the numeric item ID an ordering signal.
	// Source: standalone stack specification
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", it.ID, strings.TrimSpace(it.Title))
	writeSection(&b, "Actor", strings.TrimSpace(it.Actor))
	writeSection(&b, "Purpose", strings.TrimSpace(it.Purpose))
	writeSection(&b, "Requested result", strings.TrimSpace(it.RequestedResult))
	b.WriteString("## Acceptance\n")
	for _, value := range it.Acceptance {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(value))
	}
	b.WriteString("\n## Graph\n")
	fmt.Fprintf(&b, "- Name: %s\n- Kind: %s\n- URL: %s\n", it.Graph.Name, it.Graph.Kind, it.Graph.URL)
	b.WriteString("\n## Prerequisites\n")
	if len(it.Prerequisites) == 0 {
		b.WriteString("None.\n")
	} else {
		for _, p := range it.Prerequisites {
			fmt.Fprintf(&b, "- %s | %s | %s | %s\n", p.Ref, p.Class, p.Label, p.URL)
		}
	}
	b.WriteString("\n## Source evidence\n")
	for _, value := range it.SourceEvidence {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(value))
	}
	b.WriteByte('\n')
	writeSection(&b, "Details", strings.TrimSpace(it.Details))
	return []byte(strings.TrimRight(b.String(), "\n") + "\n")
}

func writeSection(b *strings.Builder, heading, body string) {
	fmt.Fprintf(b, "## %s\n", heading)
	if body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
}

func canonicalStack(s stack) []byte {
	if len(s.Items) == 0 {
		return nil
	}
	parts := make([]string, 0, len(s.Items))
	for _, it := range s.Items {
		parts = append(parts, strings.TrimRight(string(canonicalItem(it)), "\n"))
	}
	return []byte(strings.Join(parts, "\n\n") + "\n")
}

func revision(data []byte) (string, error) {
	prefix := cid.Prefix{Version: 1, Codec: cid.Raw, MhType: multihash.SHA2_256, MhLength: -1}
	c, err := prefix.Sum(data)
	if err != nil {
		return "", err
	}
	return c.String(), nil
}

func itemRevision(it item) (string, error) { return revision(canonicalItem(it)) }

func stackRevision(s stack) (string, error) { return revision(canonicalStack(s)) }

func edgeID(source, target string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + target))
	return "PQ-" + strings.ToUpper(hex.EncodeToString(sum[:8]))
}

func findItem(items []item, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var (
	errUsage    = errors.New("usage")
	errConflict = errors.New("conflict")
)
