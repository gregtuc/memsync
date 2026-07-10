package courier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectClaudeScopesByProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "myrepo", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "deploy.md"), []byte("# Deploy\n- hold traffic\n"), 0o644)

	mems, err := CollectClaude()
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("want 1 memory, got %d", len(mems))
	}
	m := mems[0]
	if m.Origin != "claude" || m.Scope != "myrepo" || m.Title != "deploy" {
		t.Fatalf("bad memory: %+v", m)
	}
	if !strings.Contains(m.Body, "hold traffic") {
		t.Fatal("body not captured")
	}
}

func TestCollectClaudeAtReadsOnlyCurrentProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work", "myrepo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	encoded := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
	dir := filepath.Join(home, ".claude", "projects", encoded, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "current.md"), []byte("current project note\n"), 0o644)
	other := filepath.Join(home, ".claude", "projects", "other", "memory")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "private.md"), []byte("unrelated note\n"), 0o644)

	mems, err := CollectClaudeAt(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 || mems[0].Title != "current" || mems[0].ProjectID == "" {
		t.Fatalf("current-project collection is wrong: %+v", mems)
	}
}

func TestCollectCodexIsGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex", "memories")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "memory_summary.md"), []byte("- use the helper\n"), 0o644)

	mems, _ := CollectCodex()
	if len(mems) != 1 || mems[0].Origin != "codex" || mems[0].Scope != "global" {
		t.Fatalf("bad codex memories: %+v", mems)
	}
}

func TestCollectorsDistinguishMissingFromUnreadableSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	if mems, err := CollectClaudeAt(cwd); err != nil || len(mems) != 0 {
		t.Fatalf("missing Claude source should be empty, not an error: %+v, %v", mems, err)
	}
	encoded := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
	memoryPath := filepath.Join(home, ".claude", "projects", encoded, "memory")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memoryPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CollectClaudeAt(cwd); err == nil {
		t.Fatal("unreadable Claude source was mistaken for an empty snapshot")
	}

	codexSummary := filepath.Join(home, ".codex", "memories", "memory_summary.md")
	if err := os.MkdirAll(codexSummary, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CollectCodex(); err == nil {
		t.Fatal("unreadable Codex summary was mistaken for an empty snapshot")
	}
}

func TestCollectCodexPrefersGeneratedSummaryAndSkipsVersionMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex", "memories")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "memory_summary.md"), []byte("v1\n# Preferences\n- keep answers concise\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Duplicate handbook\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "raw_memories.md"), []byte("raw duplicate\n"), 0o644)

	mems, err := CollectCodex()
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 || mems[0].Title != "memory summary" {
		t.Fatalf("want only the generated summary, got %+v", mems)
	}
	out := RenderContext("Codex", mems, 4000)
	if !strings.Contains(out, "keep answers concise") || strings.Contains(out, "> v1") {
		t.Fatalf("summary was not rendered usefully: %q", out)
	}
}

func TestRenderContextHasMarkersAndRespectsCap(t *testing.T) {
	mems := []Memory{
		{Origin: "codex", Scope: "global", Title: "a", Body: "first note"},
		{Origin: "codex", Scope: "global", Title: "b", Body: "second note"},
	}
	out := RenderContext("Codex", mems, 4000)
	if !strings.Contains(out, referenceMarker) {
		t.Fatal("missing reference marker")
	}
	if !strings.Contains(out, syncedTag+"codex]") {
		t.Fatal("missing provenance tag")
	}
	if !LooksSynced(out) {
		t.Fatal("rendered context should be detectable as synced")
	}
	small := RenderContext("Codex", mems, 130)
	if !strings.Contains(small, "truncated") {
		t.Fatalf("expected a truncation note under a tight budget, got: %q", small)
	}
}

func TestRenderContextNewestFirstAndLabelsDevice(t *testing.T) {
	mems := []Memory{
		{Origin: "claude", Scope: "repo", Title: "old", Body: "old note", UpdatedAt: 1},
		{Origin: "claude", Scope: "repo", Title: "new", Body: "new note", DeviceName: "work-mac", UpdatedAt: 2},
	}
	out := RenderContext("Claude", mems, 4000)
	if strings.Index(out, "new note") > strings.Index(out, "old note") {
		t.Fatalf("newest memory did not render first: %q", out)
	}
	if !strings.Contains(out, "[device:work-mac]") {
		t.Fatalf("device label missing: %q", out)
	}
}

func TestRenderContextEmpty(t *testing.T) {
	if RenderContext("Codex", nil, 4000) != "" {
		t.Fatal("empty memory set should render nothing")
	}
}

func TestLooksSynced(t *testing.T) {
	if !LooksSynced("something [synced-from:claude] here") {
		t.Fatal("should detect the provenance tag")
	}
	if LooksSynced("a normal note about redis caching") {
		t.Fatal("false positive on ordinary text")
	}
}

func TestOneLinePrefersContentOverHeading(t *testing.T) {
	if got := oneLine("# Heading\n- the real content line\n"); got != "the real content line" {
		t.Fatalf("got %q", got)
	}
	if got := oneLine("# Only Heading"); got != "Only Heading" {
		t.Fatalf("heading fallback failed: %q", got)
	}
}
