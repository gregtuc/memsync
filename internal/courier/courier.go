// Package courier reads each tool's own memory (never editing it) and renders
// the other tool's memory as a labeled, reference-only context block.
package courier

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/project"
)

// Memory is one couriered note with enough provenance to prevent sync loops.
type Memory struct {
	Origin     string // "claude" | "codex"
	Scope      string // project name, or "global"
	Title      string
	Body       string
	DeviceID   string
	DeviceName string
	ProjectID  string
	UpdatedAt  int64
}

// referenceMarker tags injected context; syncedTag prefixes each injected line.
// If either shows up in a captured memory, that memory is an echo of something
// memsync itself injected, so it must not be re-captured.
const (
	referenceMarker = "memsync:reference-only"
	syncedTag       = "[synced-from:"
)

// LooksSynced reports whether a memory body is (verbatim) memsync-injected
// content that an agent copied into its own store.
func LooksSynced(body string) bool {
	return strings.Contains(body, referenceMarker) || strings.Contains(body, syncedTag)
}

// CollectClaude reads Claude's per-project auto-memory topic files (read-only).
func CollectClaude() ([]Memory, error) {
	var out []Memory
	root := filepath.Join(paths.ClaudeDir(), "projects")
	projects, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		memDir := filepath.Join(root, p.Name(), "memory")
		mems, err := collectClaudeDir(memDir, p.Name(), "")
		if err != nil {
			return nil, err
		}
		out = append(out, mems...)
	}
	return out, nil
}

// CollectClaudeAt reads only the current Claude project's memory. Hook
// commands run in the session cwd, so this avoids leaking unrelated project
// notes while a portable Git-derived ProjectID lets the same repo match on a
// different machine.
func CollectClaudeAt(cwd string) ([]Memory, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info := project.Identify(abs)
	encoded := strings.ReplaceAll(filepath.Clean(abs), string(filepath.Separator), "-")
	memDir := filepath.Join(paths.ClaudeDir(), "projects", encoded, "memory")
	return collectClaudeDir(memDir, info.Name, info.ID)
}

func collectClaudeDir(memDir, scope, projectID string) ([]Memory, error) {
	files, err := os.ReadDir(memDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Memory
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(memDir, f.Name()))
		if err != nil {
			return nil, err
		}
		m := Memory{
			Origin:    "claude",
			Scope:     scope,
			Title:     strings.TrimSuffix(f.Name(), ".md"),
			Body:      string(b),
			ProjectID: projectID,
		}
		info, err := f.Info()
		if err != nil {
			return nil, err
		}
		m.UpdatedAt = info.ModTime().Unix()
		out = append(out, m)
	}
	return out, nil
}

// CollectCodex reads Codex's generated memory workspace (read-only, global
// scope). Modern Codex keeps staging data in SQLite but writes the supported,
// user-visible memory artifacts here. memory_summary.md is the same dense file
// Codex injects into its own sessions, so prefer it over raw/evidence files.
func CollectCodex() ([]Memory, error) {
	root := paths.CodexMemories()
	for _, candidate := range []struct {
		name  string
		title string
	}{
		{name: "memory_summary.md", title: "memory summary"},
		{name: "MEMORY.md", title: "memory handbook"},
	} {
		m, ok, err := readCodexMemory(filepath.Join(root, candidate.name), candidate.title)
		if err != nil {
			return nil, err
		}
		if ok {
			return []Memory{m}, nil
		}
	}

	// Compatibility fallback for older/custom Codex builds that wrote simple
	// top-level Markdown notes. Generated raw/evidence files are intentionally
	// excluded because they duplicate the consolidated memory and may be large.
	var out []Memory
	files, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".md") || isCodexWorkingFile(f.Name()) {
			continue
		}
		m, ok, err := readCodexMemory(filepath.Join(root, f.Name()), strings.TrimSuffix(f.Name(), filepath.Ext(f.Name())))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func readCodexMemory(path, title string) (Memory, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, err
	}
	if strings.TrimSpace(string(b)) == "" {
		return Memory{}, false, nil
	}
	m := Memory{Origin: "codex", Scope: "global", Title: title, Body: string(b)}
	info, err := os.Stat(path)
	if err != nil {
		return Memory{}, false, err
	}
	m.UpdatedAt = info.ModTime().Unix()
	return m, true, nil
}

func isCodexWorkingFile(name string) bool {
	switch strings.ToLower(name) {
	case "raw_memories.md", "phase2_workspace_diff.md":
		return true
	}
	return false
}

// RenderContext builds the read-only block injected into the receiving tool.
// It is provenance-stamped (so a round-trip is recognized, not re-captured) and
// size-capped (so it never displaces the tool's own memory budget).
func RenderContext(fromLabel string, mems []Memory, maxBytes int) string {
	if len(mems) == 0 {
		return ""
	}
	sort.SliceStable(mems, func(i, j int) bool {
		if mems[i].UpdatedAt != mems[j].UpdatedAt {
			return mems[i].UpdatedAt > mems[j].UpdatedAt
		}
		return mems[i].Title < mems[j].Title
	})

	var b strings.Builder
	b.WriteString("<!-- " + referenceMarker + " -->\n")
	b.WriteString("### From " + fromLabel + " (reference only, may be stale; do not copy into your own memory).\n\n")
	for _, m := range mems {
		block := renderMemory(m)
		if b.Len()+len(block) > maxBytes {
			b.WriteString("- … (truncated to fit context budget; more available on request)\n")
			break
		}
		b.WriteString(block)
	}
	return b.String()
}

func renderMemory(m Memory) string {
	source := syncedTag + m.Origin + "]"
	if m.DeviceName != "" {
		source += " [device:" + safeLabel(m.DeviceName) + "]"
	}
	prefix := "- " + source + " (" + m.Scope + ") " + m.Title + ": "
	if m.Origin == "codex" && (m.Title == "memory summary" || m.Title == "memory handbook") {
		body := codexSummary(m.Body)
		if body != "" {
			return prefix + "\n" + quoteLines(clipBytes(body, 2400), "  > ") + "\n"
		}
	}
	return prefix + oneLine(m.Body) + "\n"
}

func codexSummary(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "v1" {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func quoteLines(s, prefix string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, prefix+line)
	}
	return strings.Join(out, "\n")
}

func safeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == ']' || r == '[' || r == '\n' || r == '\r' {
			return '-'
		}
		return r
	}, s)
	return clipBytes(strings.TrimSpace(s), 60)
}

// oneLine summarizes a note, preferring the first real content line over a
// markdown heading (falling back to the heading if that's all there is).
func oneLine(s string) string {
	var firstHeading string
	for _, raw := range strings.Split(strings.TrimSpace(s), "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		heading := strings.HasPrefix(t, "#")
		clean := strings.TrimSpace(strings.TrimLeft(t, "#-*> "))
		if clean == "" {
			continue
		}
		if heading {
			if firstHeading == "" {
				firstHeading = clean
			}
			continue
		}
		return clip(clean)
	}
	if firstHeading != "" {
		return clip(firstHeading)
	}
	return "(empty)"
}

func clip(s string) string {
	return clipBytes(s, 240)
}

func clipBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && cut < len(s) && (s[cut]&0xc0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
