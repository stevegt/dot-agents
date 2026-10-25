package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type locatedItem struct {
	Item  item
	State string
	Index int
}

type graphNode struct {
	Name       string
	Attributes map[string]string
	URL        string
}

type graphEdge struct {
	Source     string
	Target     string
	Attributes map[string]string
	URL        string
}

type baseGraph struct {
	Nodes       map[string]string
	Edges       map[string]string
	NodeMarkers map[string]bool
	EdgeMarkers map[string]bool
}

var (
	nodeLine = regexp.MustCompile("^\\s*([A-Za-z_][A-Za-z0-9_]*)\\s+\\[(.*)\\];\\s*$")
	edgeLine = regexp.MustCompile("^\\s*([A-Za-z_][A-Za-z0-9_]*)\\s*->\\s*([A-Za-z_][A-Za-z0-9_]*)\\s+\\[(.*)\\];\\s*$")
	attrPart = regexp.MustCompile("([A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*(\\\"(?:\\\\.|[^\\\"])*\\\"|[^,\\]]+)")
)

func validateURLs(rootPath string, stacks map[string]stack) error {
	for _, name := range stackOrder {
		for _, it := range stacks[name].Items {
			if err := validateSpecURL(rootPath, it.Graph.URL); err != nil {
				return fmt.Errorf("item %s graph URL: %w", it.ID, err)
			}
			for _, prereq := range it.Prerequisites {
				if err := validateSpecURL(rootPath, prereq.URL); err != nil {
					return fmt.Errorf("item %s prerequisite URL: %w", it.ID, err)
				}
			}
		}
	}
	return nil
}

func validateSpecURL(rootPath, value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "" {
		if parsed.Scheme == "https" || parsed.Scheme == "http" {
			return nil
		}
		return fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Host != "" || parsed.Path == "" || filepath.IsAbs(parsed.Path) {
		return fmt.Errorf("local URL must be relative to specs: %q", value)
	}
	repo := filepath.Clean(filepath.Dir(rootPath))
	specs := filepath.Join(repo, "specs")
	target := filepath.Clean(filepath.Join(specs, filepath.FromSlash(parsed.Path)))
	relative, err := filepath.Rel(repo, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("local URL escapes repository: %q", value)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("local URL does not resolve: %q: %w", value, err)
	}
	if info.IsDir() {
		return fmt.Errorf("local URL resolves to a directory: %q", value)
	}
	return nil
}

func generateGraph(scope string, stacks map[string]stack, basePath string, changedURLs []string) ([]byte, error) {
	// Intent: Derive the published prerequisite view solely from validated
	// stack items while retaining review markers for unchanged meaning.
	// Source: standalone stack specification
	all := map[string]locatedItem{}
	byGraph := map[string]string{}
	for _, state := range stackOrder {
		for index, it := range stacks[state].Items {
			ref := scope + "/" + it.ID
			all[ref] = locatedItem{Item: it, State: state, Index: index}
			byGraph[it.Graph.Name] = ref
		}
	}
	included := map[string]bool{}
	var include func(string) error
	include = func(ref string) error {
		if included[ref] {
			return nil
		}
		located, ok := all[ref]
		if !ok {
			return fmt.Errorf("graph cannot include missing local item %s", ref)
		}
		if located.State == "inbox" {
			return fmt.Errorf("included item %s depends on unclassified inbox item", ref)
		}
		included[ref] = true
		for _, prereq := range located.Item.Prerequisites {
			dependency, err := canonicalRef(scope, prereq.Ref)
			if err != nil {
				return err
			}
			if strings.HasPrefix(dependency, scope+"/") {
				if err := include(dependency); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, state := range []string{"default", "blocked"} {
		for _, it := range stacks[state].Items {
			if err := include(scope + "/" + it.ID); err != nil {
				return nil, err
			}
		}
	}
	completed := stacks["completed"].Items
	for index := len(completed) - 1; index >= 0 && index >= len(completed)-2; index-- {
		if err := include(scope + "/" + completed[index].ID); err != nil {
			return nil, err
		}
	}

	base, err := readBaseGraph(basePath)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	for _, value := range changedURLs {
		changed[value] = true
	}

	var nodes []graphNode
	for _, ref := range sortedKeys(included) {
		located := all[ref]
		it := located.Item
		attributes := map[string]string{
			"label": it.Actor + "\n" + it.Purpose,
			"URL":   it.Graph.URL,
		}
		switch it.Graph.Kind {
		case "decision":
			attributes["shape"] = "diamond"
		case "terminal":
			if located.State == "wontfix" {
				attributes["shape"] = "octagon"
			} else {
				attributes["shape"] = "ellipse"
				attributes["peripheries"] = "2"
			}
		}
		switch located.State {
		case "completed":
			attributes["fillcolor"] = "#d9ead3"
		case "wontfix":
			attributes["fillcolor"] = "#f4cccc"
		}
		nodes = append(nodes, graphNode{Name: it.Graph.Name, Attributes: attributes, URL: it.Graph.URL})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	edgeIDs := map[string]string{}
	var edges []graphEdge
	for _, target := range nodes {
		ref := byGraph[target.Name]
		located := all[ref]
		for _, prereq := range located.Item.Prerequisites {
			sourceRef, err := canonicalRef(scope, prereq.Ref)
			if err != nil {
				return nil, err
			}
			source, ok := all[sourceRef]
			if !ok || !included[sourceRef] {
				return nil, fmt.Errorf("graph edge to %s has unavailable source %s", target.Name, sourceRef)
			}
			id := edgeID(source.Item.Graph.Name, target.Name)
			if prior, collision := edgeIDs[id]; collision {
				return nil, fmt.Errorf("edge ID collision %s for %s and %s", id, prior, source.Item.Graph.Name+"->"+target.Name)
			}
			edgeIDs[id] = source.Item.Graph.Name + "->" + target.Name
			attributes := map[string]string{
				"label": prereq.Label,
				"id":    id,
				"URL":   prereq.URL,
			}
			if prereq.Class == "desired" {
				attributes["weight"] = "5"
				attributes["penwidth"] = "2.5"
			} else {
				attributes["weight"] = "1"
				attributes["penwidth"] = "1.0"
			}
			edge := graphEdge{Source: source.Item.Graph.Name, Target: target.Name, Attributes: attributes, URL: prereq.URL}
			edges = append(edges, edge)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source == edges[j].Source {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Source < edges[j].Source
	})

	changedNodes := map[string]bool{}
	changedEdges := map[string]bool{}
	currentNodes := map[string]bool{}
	currentEdges := map[string]bool{}
	semanticRevision := false
	for _, node := range nodes {
		currentNodes[node.Name] = true
		signature := attributeSignature(node.Attributes)
		if old, ok := base.Nodes[node.Name]; !ok || old != signature || changed[node.URL] {
			changedNodes[node.Name] = true
			semanticRevision = true
		}
	}
	for _, edge := range edges {
		key := edge.Source + "->" + edge.Target
		currentEdges[key] = true
		signature := attributeSignature(edge.Attributes)
		if old, ok := base.Edges[key]; !ok || old != signature || changed[edge.URL] {
			changedEdges[key] = true
			semanticRevision = true
		}
	}
	for name := range base.Nodes {
		if !currentNodes[name] {
			semanticRevision = true
		}
	}
	for key := range base.Edges {
		if !currentEdges[key] {
			semanticRevision = true
		}
	}
	for index := range nodes {
		if changedNodes[nodes[index].Name] || (!semanticRevision && base.NodeMarkers[nodes[index].Name]) {
			nodes[index].Attributes["color"] = "#00c853"
		}
	}
	for index := range edges {
		key := edges[index].Source + "->" + edges[index].Target
		if changedEdges[key] || (!semanticRevision && base.EdgeMarkers[key]) {
			edges[index].Attributes["color"] = "#00c853"
		}
	}

	var output strings.Builder
	fmt.Fprintf(&output, "// Generated from validated Markdown stacks for scope %q. Do not edit by hand.\n", scope)
	output.WriteString("// Intent: Show current prerequisites, actors, handoffs, and completion conditions.\n")
	output.WriteString("digraph prereq {\n")
	output.WriteString("    graph [rankdir=TB, label=\"current prerequisite work\", labelloc=t, fontsize=18];\n")
	output.WriteString("    node [shape=box, fontname=\"sans-serif\", style=filled, fillcolor=\"white\"];\n")
	output.WriteString("    edge [fontname=\"sans-serif\"];\n\n")
	for _, node := range nodes {
		fmt.Fprintf(&output, "    %s [%s];\n", node.Name, renderAttributes(node.Attributes))
	}
	output.WriteByte('\n')
	for _, edge := range edges {
		fmt.Fprintf(&output, "    %s -> %s [%s];\n", edge.Source, edge.Target, renderAttributes(edge.Attributes))
	}
	output.WriteString("}\n")
	return []byte(output.String()), nil
}

func readBaseGraph(path string) (baseGraph, error) {
	result := baseGraph{
		Nodes: map[string]string{}, Edges: map[string]string{},
		NodeMarkers: map[string]bool{}, EdgeMarkers: map[string]bool{},
	}
	if path == "" {
		return result, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return baseGraph{}, err
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if match := edgeLine.FindStringSubmatch(line); match != nil {
			key := match[1] + "->" + match[2]
			attributes := parseAttributes(match[3])
			result.Edges[key] = attributeSignature(attributes)
			result.EdgeMarkers[key] = attributes["color"] == "#00c853"
			continue
		}
		if match := nodeLine.FindStringSubmatch(line); match != nil {
			if match[1] == "graph" || match[1] == "node" || match[1] == "edge" {
				continue
			}
			attributes := parseAttributes(match[2])
			result.Nodes[match[1]] = attributeSignature(attributes)
			result.NodeMarkers[match[1]] = attributes["color"] == "#00c853"
		}
	}
	if err := errors.Join(scanner.Err(), file.Close()); err != nil {
		return baseGraph{}, err
	}
	return result, nil
}

func parseAttributes(value string) map[string]string {
	result := map[string]string{}
	for _, match := range attrPart.FindAllStringSubmatch(value, -1) {
		decoded := strings.TrimSpace(match[2])
		if strings.HasPrefix(decoded, "\"") {
			if unquoted, err := strconv.Unquote(decoded); err == nil {
				decoded = unquoted
			}
		}
		result[match[1]] = decoded
	}
	return result
}

func attributeSignature(attributes map[string]string) string {
	var parts []string
	for key, value := range attributes {
		if key == "color" && value == "#00c853" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x00")
}

func renderAttributes(attributes map[string]string) string {
	order := []string{"shape", "peripheries", "label", "fillcolor", "color", "weight", "penwidth", "id", "URL"}
	var parts []string
	for _, key := range order {
		value, ok := attributes[key]
		if !ok {
			continue
		}
		if key == "peripheries" || key == "weight" || key == "penwidth" {
			parts = append(parts, key+"="+value)
		} else {
			parts = append(parts, key+"="+strconv.Quote(value))
		}
	}
	return strings.Join(parts, ", ")
}

func writeGraphOutput(rootPath, outputPath string, data []byte) error {
	repo := filepath.Clean(filepath.Dir(rootPath))
	specs := filepath.Join(repo, "specs")
	abs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(specs, abs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("graph output must remain below %s", specs)
	}
	if info, err := os.Lstat(abs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink graph output %s", abs)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(abs), ".prereq-dot-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	// Intent: Report a failed temporary-file cleanup together with the graph
	// write failure so an operator can repair the leftover state. Source: standalone stack specification
	cleanup := func() error { return os.Remove(tempName) }
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
	if err := os.Rename(tempName, abs); err != nil {
		return errors.Join(err, cleanup())
	}
	dir, err := os.Open(filepath.Dir(abs))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
