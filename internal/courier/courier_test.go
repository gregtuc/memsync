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
