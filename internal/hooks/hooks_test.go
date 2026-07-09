package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregtuc/memsync/internal/paths"
)

func TestClaudeInstallIsIdempotentReversibleAndNonDestructive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A user with their own unrelated setting and their own SessionStart hook.
	pre := `{"model":"opus","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`
	if err := os.WriteFile(paths.ClaudeSettings(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ { // running twice must not duplicate
		if err := ClaudeInstall("/usr/local/bin/memsync"); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
	}
	if w, _ := ClaudeWired(); !w {
		t.Fatal("not wired after install")
	}

	root := readClaudeSettings(t)
	ss := asSlice(root["hooks"].(map[string]any)["SessionStart"])
	mem, echo := 0, 0
	for _, e := range ss {
		c := firstCmd(e)
		if strings.Contains(c, "memsync") {
			mem++
		}
		if strings.Contains(c, "echo hi") {
			echo++
		}
	}
	if mem != 1 {
		t.Fatalf("want exactly 1 memsync SessionStart hook, got %d", mem)
	}
	if echo != 1 {
		t.Fatalf("user's own hook was lost or duplicated (%d)", echo)
	}
	if root["model"] != "opus" {
		t.Fatal("unrelated setting was clobbered")
	}
	if _, err := os.Stat(paths.ClaudeSettings() + ".memsync.bak"); err != nil {
		t.Fatal("no backup written")
	}

	changed, err := ClaudeUninstall()
	if err != nil || !changed {
		t.Fatalf("uninstall: changed=%v err=%v", changed, err)
	}
	root = readClaudeSettings(t)
	if hooks, ok := root["hooks"].(map[string]any); ok {
		for _, e := range asSlice(hooks["SessionStart"]) {
			if strings.Contains(firstCmd(e), "memsync") {
				t.Fatal("a memsync hook survived uninstall")
			}
		}
	}
	if root["model"] != "opus" {
		t.Fatal("uninstall clobbered an unrelated setting")
	}
	found := false
	if hooks, ok := root["hooks"].(map[string]any); ok {
		for _, e := range asSlice(hooks["SessionStart"]) {
			if strings.Contains(firstCmd(e), "echo hi") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("uninstall removed the user's own hook")
	}
}

func TestClaudeInstallOnEmptyHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := ClaudeInstall("/x/memsync"); err != nil {
		t.Fatal(err)
	}
	if w, _ := ClaudeWired(); !w {
		t.Fatal("not wired")
	}
}

func TestClaudeRefusesInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(paths.ClaudeSettings(), []byte("{ not json"), 0o644)
	if err := ClaudeInstall("/x/memsync"); err == nil {
		t.Fatal("expected refusal on unparseable settings.json, not a clobber")
	}
}

func TestCodexInstallIsIdempotentReversibleAndNonDestructive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(paths.CodexConfig(), []byte("model = \"gpt\"\n"), 0o644)

	for i := 0; i < 2; i++ {
		if err := CodexInstall("/x/memsync"); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
	}
	if w, _ := CodexWired(); !w {
		t.Fatal("codex not wired")
	}
	content, _ := os.ReadFile(paths.CodexConfig())
	cs := string(content)
	if n := strings.Count(cs, codexBegin); n != 1 {
		t.Fatalf("want exactly 1 managed block, got %d", n)
	}
	if !strings.Contains(cs, "model = \"gpt\"") {
		t.Fatal("pre-existing config lost")
	}
	// verified Codex schema: PascalCase events, nested [[hooks.<Event>.hooks]], string command
	for _, want := range []string{"[[hooks.SessionStart]]", "[[hooks.SessionStart.hooks]]", "[[hooks.Stop]]", "type = \"command\""} {
		if !strings.Contains(cs, want) {
			t.Fatalf("codex block missing %q", want)
		}
	}
	if strings.Contains(cs, "session_start") || strings.Contains(cs, "[[hooks.stop]]") {
		t.Fatal("codex block uses invalid lowercase event names")
	}

	changed, err := CodexUninstall()
	if err != nil || !changed {
		t.Fatalf("uninstall: changed=%v err=%v", changed, err)
	}
	content, _ = os.ReadFile(paths.CodexConfig())
	if strings.Contains(string(content), codexBegin) {
		t.Fatal("managed block survived uninstall")
	}
	if !strings.Contains(string(content), "model = \"gpt\"") {
		t.Fatal("uninstall clobbered pre-existing config")
	}
}

func readClaudeSettings(t *testing.T) map[string]any {
	b, err := os.ReadFile(paths.ClaudeSettings())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func firstCmd(e any) string {
	m, _ := e.(map[string]any)
	for _, h := range asSlice(m["hooks"]) {
		hm, _ := h.(map[string]any)
		if c, ok := hm["command"].(string); ok {
			return c
		}
	}
	return ""
}
