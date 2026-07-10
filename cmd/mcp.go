package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/mcp"
)

// runMCP serves memsync's read-only recall tools over stdio MCP. It lets an
// agent pull the full text of memories saved by the other tool (or another
// machine), the same way each tool recalls its own memory. It never exposes or
// edits the calling tool's own memory store.
func runMCP(args []string) int {
	tool := flagValue(args, "--tool")
	if tool != "claude" && tool != "codex" {
		fmt.Fprintln(os.Stderr, "usage: memsync mcp --tool <claude|codex>")
		return 2
	}
	other := otherToolName(tool)
	tools := []mcp.Tool{
		{
			Name:        "memory_search",
			Description: "Search memories saved by " + other + " and your other machines (kept in sync by memsync). Returns matching entries by name with a short description; call memory_get to read one in full. Empty query lists everything.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "keywords to match against memory names, descriptions, and bodies; omit to list all",
					},
				},
			},
			Handler: func(a map[string]any) (string, error) {
				q, _ := a["query"].(string)
				return memorySearch(tool, q)
			},
		},
		{
			Name:        "memory_get",
			Description: "Read the full text of a memory saved by " + other + " or another machine, by its name (as shown in the memsync index or memory_search).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "the memory name to read in full",
					},
				},
				"required": []any{"name"},
			},
			Handler: func(a map[string]any) (string, error) {
				n, _ := a["name"].(string)
				return memoryGet(tool, n)
			},
		},
	}
	if err := mcp.Serve(os.Stdin, os.Stdout, mcp.ServerInfo{Name: "memsync", Version: Version}, tools); err != nil {
		fmt.Fprintf(os.Stderr, "memsync mcp: %v\n", err)
		return 1
	}
	return 0
}

func otherToolName(tool string) string {
	if tool == "codex" {
		return "Claude Code"
	}
	return "Codex"
}

func memorySearch(tool, query string) (string, error) {
	mems, err := foreignMemories(tool)
	if err != nil {
		return "", err
	}
	if len(mems) == 0 {
		return "No memories from your other tools or machines yet.", nil
	}
	ranked := rankMemories(mems, query)
	var b strings.Builder
	if strings.TrimSpace(query) == "" {
		fmt.Fprintf(&b, "%d memories from your other tools and machines (newest first):\n", len(ranked))
	} else if len(ranked) == 0 {
		return fmt.Sprintf("No memories matched %q. Call memory_search with an empty query to list all.", query), nil
	} else {
		fmt.Fprintf(&b, "%d memories matching %q (most relevant first):\n", len(ranked), query)
	}
	for _, m := range ranked {
		fmt.Fprintf(&b, "- %s (%s, from %s) — %s\n", m.Title, m.Scope, m.Origin, courier.Summarize(m.Body))
	}
	b.WriteString("\nCall memory_get with a name above to read it in full.")
	return b.String(), nil
}

func memoryGet(tool, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("provide a memory name (see the memsync index or memory_search)")
	}
	mems, err := foreignMemories(tool)
	if err != nil {
		return "", err
	}
	lname := strings.ToLower(name)
	var exact, partial []courier.Memory
	for _, m := range mems {
		lt := strings.ToLower(m.Title)
		switch {
		case lt == lname:
			exact = append(exact, m)
		case strings.Contains(lt, lname):
			partial = append(partial, m)
		}
	}
	match := exact
	if len(match) == 0 {
		match = partial
	}
	if len(match) == 0 {
		return fmt.Sprintf("No memory named %q. Call memory_search to find the right name.", name), nil
	}
	if len(match) > 1 {
		var b strings.Builder
		fmt.Fprintf(&b, "Several memories match %q; call memory_get with an exact name:\n", name)
		for _, m := range match {
			fmt.Fprintf(&b, "- %s (%s, from %s)\n", m.Title, m.Scope, m.Origin)
		}
		return b.String(), nil
	}
	m := match[0]
	device := ""
	if m.DeviceName != "" {
		device = " [device:" + m.DeviceName + "]"
	}
	// The [synced-from:] header marks this as couriered content so that, if the
	// model copies it wholesale, memsync's own capture will not re-store it.
	header := fmt.Sprintf("[synced-from:%s]%s (%s) %s (reference only; do not copy into your own memory)",
		m.Origin, device, m.Scope, m.Title)
	return header + "\n\n" + m.Body, nil
}

// rankMemories orders memories for search: by keyword overlap when a query is
// given (most matches first), otherwise newest first. Relevance judgment is left
// to the model; this is only a coarse pre-filter so the model sees likely hits.
func rankMemories(mems []courier.Memory, query string) []courier.Memory {
	terms := tokenize(query)
	if len(terms) == 0 {
		out := append([]courier.Memory(nil), mems...)
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].UpdatedAt != out[j].UpdatedAt {
				return out[i].UpdatedAt > out[j].UpdatedAt
			}
			return out[i].Title < out[j].Title
		})
		return out
	}
	type scored struct {
		m     courier.Memory
		score int
	}
	var hits []scored
	for _, m := range mems {
		haystack := tokenizeSet(m.Title + " " + m.Body)
		score := 0
		for _, t := range terms {
			if haystack[t] {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{m, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].m.UpdatedAt > hits[j].m.UpdatedAt
	})
	out := make([]courier.Memory, len(hits))
	for i, h := range hits {
		out[i] = h.m
	}
	return out
}

// tokenize lowercases s and splits it into distinct word tokens of length >= 3,
// dropping punctuation. Short tokens are dropped so common noise ("the", "of")
// does not dominate the coarse keyword score.
func tokenize(s string) []string {
	set := tokenizeSet(s)
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return out
}

func tokenizeSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, field := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(field) >= 3 {
			set[field] = true
		}
	}
	return set
}
