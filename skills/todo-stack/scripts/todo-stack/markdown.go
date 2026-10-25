package main

import (
	"bytes"
	"fmt"
	"strings"
)

var sectionOrder = []string{
	"Actor", "Purpose", "Requested result", "Acceptance", "Graph",
	"Prerequisites", "Source evidence", "Details",
}

func parseStack(name string, data []byte, allowNew bool) (stack, error) {
	// Intent: Accept only the canonical visible item schema so raw Markdown
	// edits cannot create hidden ordering or authority. Source: standalone stack specification
	if !isStackName(name) {
		return stack{}, fmt.Errorf("unsupported stack %q", name)
	}
	data = stripMetadata(data)
	if len(bytes.TrimSpace(data)) == 0 {
		return stack{Name: name}, nil
	}
	lines := splitLines(data)
	var starts []int
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 || starts[0] != 0 {
		return stack{}, fmt.Errorf("stack %s does not begin with an item heading", name)
	}
	result := stack{Name: name}
	seenIDs := map[string]bool{}
	seenGraphs := map[string]bool{}
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		it, err := parseItem(lines[start:end], allowNew)
		if err != nil {
			return stack{}, fmt.Errorf("stack %s item %d: %w", name, i+1, err)
		}
		if it.ID != "new" && seenIDs[it.ID] {
			return stack{}, fmt.Errorf("stack %s has duplicate item ID %s", name, it.ID)
		}
		if seenGraphs[it.Graph.Name] {
			return stack{}, fmt.Errorf("stack %s has duplicate graph name %s", name, it.Graph.Name)
		}
		seenIDs[it.ID] = true
		seenGraphs[it.Graph.Name] = true
		result.Items = append(result.Items, it)
	}
	return result, nil
}

func parseItem(lines []string, allowNew bool) (item, error) {
	if len(lines) == 0 {
		return item{}, fmt.Errorf("empty item")
	}
	heading := strings.TrimPrefix(lines[0], "# ")
	parts := strings.SplitN(heading, " — ", 2)
	if len(parts) != 2 {
		return item{}, fmt.Errorf("heading must be '# <ID> — <title>'")
	}
	it := item{ID: strings.TrimSpace(parts[0]), Title: strings.TrimSpace(parts[1])}
	sections := map[string][]string{}
	current := ""
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "## ") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if _, ok := sections[current]; ok {
				return item{}, fmt.Errorf("duplicate section %q", current)
			}
			sections[current] = nil
			continue
		}
		if current == "" {
			if strings.TrimSpace(line) != "" {
				return item{}, fmt.Errorf("content before Actor section")
			}
			continue
		}
		sections[current] = append(sections[current], line)
	}
	if len(sections) != len(sectionOrder) {
		return item{}, fmt.Errorf("expected sections %s", strings.Join(sectionOrder, ", "))
	}
	for _, name := range sectionOrder {
		if _, ok := sections[name]; !ok {
			return item{}, fmt.Errorf("missing section %q", name)
		}
	}
	it.Actor = sectionText(sections["Actor"])
	it.Purpose = sectionText(sections["Purpose"])
	it.RequestedResult = sectionText(sections["Requested result"])
	it.Acceptance = parseBullets(sections["Acceptance"])
	graph, err := parseGraph(sections["Graph"])
	if err != nil {
		return item{}, err
	}
	it.Graph = graph
	prereqs, err := parsePrerequisites(sections["Prerequisites"])
	if err != nil {
		return item{}, err
	}
	it.Prerequisites = prereqs
	it.SourceEvidence = parseBullets(sections["Source evidence"])
	it.Details = sectionText(sections["Details"])
	if err := validateItem(it, allowNew); err != nil {
		return item{}, err
	}
	return it, nil
}

func splitLines(data []byte) []string {
	// Intent: Split without bufio.Scanner's 64 KiB token ceiling so a long
	// details line is preserved rather than silently truncated. Source: standalone stack specification
	parts := bytes.Split(data, []byte("\n"))
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, strings.TrimSuffix(string(part), "\r"))
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func stripMetadata(data []byte) []byte {
	trimmed := bytes.TrimLeft(data, "\r\n")
	if !bytes.HasPrefix(trimmed, []byte("<!-- stack-revision:")) {
		return data
	}
	if end := bytes.Index(trimmed, []byte("-->")); end >= 0 {
		return bytes.TrimLeft(trimmed[end+3:], "\r\n")
	}
	return data
}

func sectionText(lines []string) string {
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseBullets(lines []string) []string {
	var values []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			return nil
		}
		values = append(values, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
	}
	return values
}

func parseGraph(lines []string) (graphMeta, error) {
	values := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			return graphMeta{}, fmt.Errorf("Graph entries must be bullets")
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "- "), ":", 2)
		if len(parts) != 2 {
			return graphMeta{}, fmt.Errorf("invalid Graph entry %q", line)
		}
		key := strings.TrimSpace(parts[0])
		if _, exists := values[key]; exists {
			return graphMeta{}, fmt.Errorf("duplicate Graph entry %q", key)
		}
		values[key] = strings.TrimSpace(parts[1])
	}
	if len(values) != 3 {
		return graphMeta{}, fmt.Errorf("Graph requires Name, Kind, and URL")
	}
	return graphMeta{Name: values["Name"], Kind: values["Kind"], URL: values["URL"]}, nil
}

func parsePrerequisites(lines []string) ([]prerequisite, error) {
	text := sectionText(lines)
	if text == "None." {
		return nil, nil
	}
	var values []prerequisite
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			return nil, fmt.Errorf("prerequisites must be bullets or None.")
		}
		parts := strings.Split(strings.TrimPrefix(line, "- "), "|")
		if len(parts) != 4 {
			return nil, fmt.Errorf("prerequisite must contain ref, class, label, and URL")
		}
		values = append(values, prerequisite{
			Ref: strings.TrimSpace(parts[0]), Class: strings.TrimSpace(parts[1]),
			Label: strings.TrimSpace(parts[2]), URL: strings.TrimSpace(parts[3]),
		})
	}
	return values, nil
}
