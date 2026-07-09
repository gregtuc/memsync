// Package courier reads each tool's own memory (never editing it) and renders
// the other tool's memory as a labeled, reference-only context block.
package courier

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gregtuc/memsync/internal/paths"
)

// Memory is one couriered note with enough provenance to prevent sync loops.
type Memory struct {
	Origin string // "claude" | "codex"
	Scope  string // project name, or "global"
	Title  string
	Body   string
}

// CollectClaude reads Claude's per-project auto-memory topic files (read-only).
func CollectClaude() ([]Memory, error) {
	var out []Memory
	root := filepath.Join(paths.ClaudeDir(), "projects")
	projects, _ := os.ReadDir(root)
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		memDir := filepath.Join(root, p.Name(), "memory")
		files, _ := os.ReadDir(memDir)
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(memDir, f.Name()))
			if err != nil {
				continue
			}
			out = append(out, Memory{
				Origin: "claude",
				Scope:  p.Name(),
				Title:  strings.TrimSuffix(f.Name(), ".md"),
				Body:   string(b),
			})
		}
	}
	return out, nil
}

// CollectCodex reads Codex's consolidated memory files (read-only, global scope).
func CollectCodex() ([]Memory, error) {
	var out []Memory
	files, _ := os.ReadDir(paths.CodexMemories())
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(paths.CodexMemories(), f.Name()))
		if err != nil {
			continue
		}
		out = append(out, Memory{
			Origin: "codex",
			Scope:  "global",
			Title:  strings.TrimSuffix(f.Name(), ".md"),
			Body:   string(b),
		})
	}
	return out, nil
}

// RenderContext builds the read-only block injected into the receiving tool.
// It is provenance-stamped (so a round-trip is recognized, not re-captured) and
// size-capped (so it never displaces the tool's own memory budget).
func RenderContext(fromLabel string, mems []Memory, maxBytes int) string {
	if len(mems) == 0 {
		return ""
	}
	sort.Slice(mems, func(i, j int) bool { return mems[i].Title < mems[j].Title })

	var b strings.Builder
	b.WriteString("<!-- memsync:reference-only -->\n")
	b.WriteString("### From " + fromLabel + " — reference only, may be stale; do not copy into your own memory.\n\n")
	for _, m := range mems {
		line := "- [synced-from:" + m.Origin + "] (" + m.Scope + ") " + m.Title + " — " + oneLine(m.Body) + "\n"
		if b.Len()+len(line) > maxBytes {
			b.WriteString("- … (truncated to fit context budget; more available on request)\n")
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#-* "))
		if line != "" {
			if len(line) > 140 {
				return line[:140] + "…"
			}
			return line
		}
	}
	return "(empty)"
}
