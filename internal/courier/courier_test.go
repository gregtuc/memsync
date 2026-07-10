package courier

import (
	"fmt"
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
	if !strings.Contains(small, "not shown") {
		t.Fatalf("expected an omission note under a tight budget, got: %q", small)
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

// Regression: Claude auto-memories lead with YAML frontmatter. The summary must
// be the purpose-built `description:` rather than the `name:` header line (the
// latter is what shipped, so every injected line read "<title>: name: <slug>").
func TestOneLineUsesFrontmatterDescription(t *testing.T) {
	body := "---\n" +
		"name: project_memsync\n" +
		"description: \"memsync — Greg's personal Go CLI syncing agent memories\"\n" +
		"metadata:\n" +
		"  type: project\n" +
		"---\n\n" +
		"**memsync** — a personal side project.\n"
	if got := oneLine(body); got != "memsync — Greg's personal Go CLI syncing agent memories" {
		t.Fatalf("frontmatter description not used: %q", got)
	}
}

func TestOneLineSkipsFrontmatterWithoutDescription(t *testing.T) {
	body := "---\nname: deploy\nmetadata:\n  type: reference\n---\n\n# Heading\nHold traffic during a roll.\n"
	if got := oneLine(body); got != "Hold traffic during a roll." {
		t.Fatalf("frontmatter not skipped to first body line: %q", got)
	}
}

func TestOneLineSingleQuotedDescriptionIsUnquoted(t *testing.T) {
	if got := oneLine("---\nname: x\ndescription: 'it''s fine'\n---\nbody\n"); got != "it's fine" {
		t.Fatalf("single-quoted description not unquoted: %q", got)
	}
}

func TestOneLineNoFrontmatterUnchanged(t *testing.T) {
	if got := oneLine("plain note, no frontmatter\n"); got != "plain note, no frontmatter" {
		t.Fatalf("plain note regressed: %q", got)
	}
	// A "---" that is not a leading fence must not be treated as frontmatter.
	if got := oneLine("real content\n---\nfooter\n"); got != "real content" {
		t.Fatalf("mid-body --- misread as frontmatter: %q", got)
	}
}

func TestRenderContextTruncationReportsRemainingCount(t *testing.T) {
	var mems []Memory
	for i := 0; i < 200; i++ {
		mems = append(mems, Memory{Origin: "claude", Scope: "repo", Title: "m", Body: "a memory body", UpdatedAt: int64(i)})
	}
	out := RenderContext("Claude", mems, 4000)
	shown := strings.Count(out, "- "+syncedTag)
	if shown == 0 || shown == len(mems) {
		t.Fatalf("expected a partial render, shown=%d of %d", shown, len(mems))
	}
	want := fmt.Sprintf("and %d more not shown", len(mems)-shown)
	if !strings.Contains(out, want) {
		t.Fatalf("omission note wrong; want %q in:\n%s", want, out)
	}
}
