package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	pre := `{"model":"opus","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]},{"hooks":[{"type":"command","command":"/opt/bin/memsync-notifier --upload"}]}]}}`
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
	mem, echo, notifier := 0, 0, 0
	for _, e := range ss {
		c := firstCmd(e)
		if strings.Contains(c, claudeMarker) {
			mem++
		}
		if strings.Contains(c, "echo hi") {
			echo++
		}
		if strings.Contains(c, "memsync-notifier") {
			notifier++
		}
	}
	if mem != 1 {
		t.Fatalf("want exactly 1 memsync SessionStart hook, got %d", mem)
	}
	if echo != 1 {
		t.Fatalf("user's own hook was lost or duplicated (%d)", echo)
	}
	if notifier != 1 {
		t.Fatalf("unrelated hook containing the word memsync was changed (%d)", notifier)
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
			if isManagedClaudeCommand(firstCmd(e)) {
				t.Fatal("a memsync hook survived uninstall")
			}
		}
	}
	if root["model"] != "opus" {
		t.Fatal("uninstall clobbered an unrelated setting")
	}
	found := false
	notifierFound := false
	if hooks, ok := root["hooks"].(map[string]any); ok {
		for _, e := range asSlice(hooks["SessionStart"]) {
			command := firstCmd(e)
			if strings.Contains(command, "echo hi") {
				found = true
			}
			if strings.Contains(command, "memsync-notifier") {
				notifierFound = true
			}
		}
	}
	if !found {
		t.Fatal("uninstall removed the user's own hook")
	}
	if !notifierFound {
		t.Fatal("uninstall removed an unrelated hook containing the word memsync")
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

func TestClaudeInstallMigratesLegacyUnquotedHooksWithoutDuplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/usr/local/bin/memsync inject --tool claude"}]}],"SessionEnd":[{"hooks":[{"type":"command","command":"/usr/local/bin/memsync sync --tool claude"}]}],"FileChanged":[{"matcher":"MEMORY.md","hooks":[{"type":"command","command":"/usr/local/bin/memsync sync --tool claude"}]},{"hooks":[{"type":"command","command":"echo user-file-hook"}]}]}}`
	if err := os.WriteFile(paths.ClaudeSettings(), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClaudeInstall("/new/path/memsync"); err != nil {
		t.Fatal(err)
	}
	root := readClaudeSettings(t)
	hookMap := root["hooks"].(map[string]any)
	for _, event := range []string{"SessionStart", "SessionEnd"} {
		entries := asSlice(hookMap[event])
		if len(entries) != 1 {
			t.Fatalf("%s has %d entries after migration, want 1", event, len(entries))
		}
		if !strings.Contains(firstCmd(entries[0]), claudeMarker) {
			t.Fatalf("%s was not replaced with the explicitly managed hook", event)
		}
	}
	fileEntries := asSlice(hookMap["FileChanged"])
	if len(fileEntries) != 1 || firstCmd(fileEntries[0]) != "echo user-file-hook" {
		t.Fatalf("legacy FileChanged cleanup was destructive or incomplete: %+v", fileEntries)
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

func TestClaudeHooksEnabledDetectsGlobalDisable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(paths.ClaudeSettings(), []byte(`{"disableAllHooks":true}`), 0o644)
	enabled, err := ClaudeHooksEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("disableAllHooks=true was ignored")
	}
}

func TestWiredRequiresCompleteLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := ClaudeInstall("/x/memsync"); err != nil {
		t.Fatal(err)
	}
	root := readClaudeSettings(t)
	delete(root["hooks"].(map[string]any), "SessionEnd")
	if err := writeJSON(paths.ClaudeSettings(), root); err != nil {
		t.Fatal(err)
	}
	if wired, err := ClaudeWired(); err != nil || wired {
		t.Fatalf("Claude missing capture hook reported wired: %v, %v", wired, err)
	}

	brokenCodex := codexBegin + "\n[[hooks.SessionStart]]\n" + codexEnd + "\n"
	if err := os.MkdirAll(filepath.Dir(paths.CodexConfig()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CodexConfig(), []byte(brokenCodex), 0o644); err != nil {
		t.Fatal(err)
	}
	if wired, err := CodexWired(); err != nil || wired {
		t.Fatalf("Codex missing Stop hook reported wired: %v, %v", wired, err)
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

// Regression: a config that already has a [features] table must not end up with
// a duplicate (TOML forbids it and Codex then fails to load config).
func TestCodexInstallDoesNotDuplicateFeatures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(paths.CodexConfig(), []byte("[features]\nweb_search = true\n"), 0o644)

	if err := CodexInstall("/x/memsync"); err != nil {
		t.Fatal(err)
	}
	cs, _ := os.ReadFile(paths.CodexConfig())
	if n := strings.Count(string(cs), "[features]"); n != 1 {
		t.Fatalf("want exactly 1 [features] table, got %d", n)
	}
	if strings.Contains(codexBlock("/x/memsync"), "[features]") {
		t.Fatal("codexBlock must not emit its own [features] table")
	}
}

func TestCodexInstallPreservesExistingHookTrustState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `model = "gpt"

[hooks.state]
"user:existing-hook" = { trusted_hash = "sha256:abc", enabled = false }
`
	if err := os.WriteFile(paths.CodexConfig(), []byte(pre), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := CodexInstall("/x/memsync"); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
	}
	content, err := os.ReadFile(paths.CodexConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`[hooks.state]`,
		`"user:existing-hook" = { trusted_hash = "sha256:abc", enabled = false }`,
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("Codex install lost existing config %q:\n%s", want, content)
		}
	}
}

// Regression: after the user approves the hooks in `/hooks`, Codex records trust
// as a [hooks.state] table that it writes INSIDE memsync's managed block. That
// must neither be read as "not configured" nor destroyed by a later refresh
// (either would silently revoke the approval and loop the user through it again).
func TestCodexToleratesAndPreservesToolStateInsideManagedBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	const bin = "/x/memsync"
	if err := CodexInstall(bin); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(paths.CodexConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Simulate Codex inserting its trust record just before the end marker.
	const trust = "[hooks.state]\n\n[hooks.state.\"config.toml:stop:0:0\"]\ntrusted_hash = \"sha256:deadbeef\"\n"
	injected := strings.Replace(string(installed), codexEnd, trust+codexEnd, 1)
	if injected == string(installed) {
		t.Fatal("could not inject trust state before the end marker")
	}
	if err := os.WriteFile(paths.CodexConfig(), []byte(injected), 0o600); err != nil {
		t.Fatal(err)
	}

	// Detection must still consider the hooks wired for this binary.
	if wired, err := CodexWiredFor(bin); err != nil || !wired {
		t.Fatalf("tool-written [hooks.state] inside the block broke detection: wired=%v err=%v", wired, err)
	}

	// A refresh onto a new binary path must rewrite memsync's block yet keep the
	// tool's trust record intact.
	const newBin = "/opt/memsync/memsync"
	if err := CodexInstall(newBin); err != nil {
		t.Fatal(err)
	}
	refreshed, err := os.ReadFile(paths.CodexConfig())
	if err != nil {
		t.Fatal(err)
	}
	got := string(refreshed)
	if !strings.Contains(got, `trusted_hash = "sha256:deadbeef"`) || !strings.Contains(got, "[hooks.state]") {
		t.Fatalf("refresh destroyed tool-written hook trust state:\n%s", got)
	}
	if n := strings.Count(got, codexBegin); n != 1 {
		t.Fatalf("want exactly 1 managed block after refresh, got %d", n)
	}
	if wired, err := CodexWiredFor(newBin); err != nil || !wired {
		t.Fatalf("not wired for the new binary after refresh: wired=%v err=%v", wired, err)
	}
	if strings.Contains(got, shellCommand(bin, "inject", "--tool", "codex")) {
		t.Fatalf("stale binary command survived the refresh:\n%s", got)
	}
}

func TestCodexRefusesMalformedManagedMarkersWithoutChangingConfig(t *testing.T) {
	for name, malformed := range map[string]string{
		"missing end":  "model = \"gpt\"\n" + codexBegin + "\nunrelated = true\n",
		"end first":    "model = \"gpt\"\n" + codexEnd + "\nunrelated = true\n",
		"nested begin": codexBegin + "\n" + codexBegin + "\n" + codexEnd + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.CodexConfig(), []byte(malformed), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := CodexInstall("/x/memsync"); err == nil {
				t.Fatal("malformed managed block was silently rewritten")
			}
			got, err := os.ReadFile(paths.CodexConfig())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != malformed {
				t.Fatalf("malformed config changed:\n%s", got)
			}
			if wired, err := CodexWired(); err == nil || wired {
				t.Fatalf("malformed block reported as wired: wired=%v err=%v", wired, err)
			}
		})
	}
}

func TestHookCommandsQuoteBinaryPathForClaudeAndCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, "a bin directory", "team's memsync")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ClaudeInstall(bin); err != nil {
		t.Fatal(err)
	}
	root := readClaudeSettings(t)
	hooks := root["hooks"].(map[string]any)
	claudeStart := firstCmd(asSlice(hooks["SessionStart"])[0])
	claudeEnd := firstCmd(asSlice(hooks["SessionEnd"])[0])
	if want := shellCommand(bin, "inject", "--tool", "claude") + " " + claudeMarker; claudeStart != want {
		t.Fatalf("Claude SessionStart command\n got: %q\nwant: %q", claudeStart, want)
	}
	if want := shellCommand(bin, "sync", "--tool", "claude") + " " + claudeMarker; claudeEnd != want {
		t.Fatalf("Claude SessionEnd command\n got: %q\nwant: %q", claudeEnd, want)
	}

	codexCommands := codexBlockCommands(t, codexBlock(bin))
	wantCodex := []string{
		shellCommand(bin, "inject", "--tool", "codex"),
		shellCommand(bin, "sync", "--tool", "codex"),
	}
	if len(codexCommands) != len(wantCodex) {
		t.Fatalf("got %d Codex commands, want %d", len(codexCommands), len(wantCodex))
	}
	for i := range wantCodex {
		if codexCommands[i] != wantCodex[i] {
			t.Fatalf("Codex command %d\n got: %q\nwant: %q", i, codexCommands[i], wantCodex[i])
		}
	}

	if runtime.GOOS == "windows" {
		t.Skip("POSIX hook command execution check")
	}
	out, err := exec.Command("/bin/sh", "-c", claudeStart).CombinedOutput()
	if err != nil {
		t.Fatalf("quoted command failed: %v: %s", err, out)
	}
	if got, want := string(out), "inject\n--tool\nclaude\n"; got != want {
		t.Fatalf("quoted command argv\n got: %q\nwant: %q", got, want)
	}
}

func codexBlockCommands(t *testing.T, block string) []string {
	t.Helper()
	var commands []string
	for _, line := range strings.Split(block, "\n") {
		value, ok := strings.CutPrefix(line, "command = ")
		if !ok {
			continue
		}
		command, err := strconv.Unquote(value)
		if err != nil {
			t.Fatalf("invalid quoted Codex command %q: %v", value, err)
		}
		commands = append(commands, command)
	}
	return commands
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
