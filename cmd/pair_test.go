package cmd

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func TestReadInputLinePreservesBufferedLines(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("invite\nyes\n"))
	if got := readInputLine(input); got != "invite" {
		t.Fatalf("first line = %q", got)
	}
	if got := readInputLine(input); got != "yes" {
		t.Fatalf("second line was lost: %q", got)
	}
}

func TestJoinStateSurvivesRetryAndStaysPrivate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	first, state, err := loadOrCreateJoinState()
	if err != nil {
		t.Fatal(err)
	}
	state.Reply = "saved-reply"
	if err := saveJoinState(state); err != nil {
		t.Fatal(err)
	}
	second, restored, err := loadOrCreateJoinState()
	if err != nil {
		t.Fatal(err)
	}
	if second.Invite() != first.Invite() || restored.Reply != "saved-reply" {
		t.Fatal("unfinished join state was not resumed")
	}
	info, err := os.Stat(paths.JoinStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("join state permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestRunPairValidatesInviteBeforeRemoteSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	if _, _, err := crypto.LoadOrCreateKey(paths.KeyPath()); err != nil {
		t.Fatal(err)
	}
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Ensure(trueBin); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	ghLog := filepath.Join(home, "gh-called")
	ghScript := "#!/bin/sh\ntouch \"$GH_CALLED\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(ghScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_CALLED", ghLog)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if code := runPairFrom([]string{"--yes"}, bufio.NewReader(strings.NewReader("not-an-invite\n"))); code == 0 {
		t.Fatal("invalid invite was accepted")
	}
	if _, err := os.Stat(ghLog); !os.IsNotExist(err) {
		t.Fatalf("remote setup ran before invite validation: %v", err)
	}
}

func TestWireJoinedToolsEnablesCodexDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	featureLog := filepath.Join(home, "enabled")
	codexScript := `#!/bin/sh
if [ "$1 $2" = "features list" ]; then
  echo 'hooks stable false'
  echo 'memories experimental false'
elif [ "$1 $2" = "features enable" ]; then
  echo "$3" >> "$FEATURE_LOG"
fi
`
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(codexScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FEATURE_LOG", featureLog)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ready, issues := wireJoinedTools([]detect.Tool{{Name: "Codex CLI", Home: filepath.Join(home, ".codex"), Present: true}}, "/tmp/memsync-test")
	if !ready["Codex CLI"] {
		t.Fatalf("Codex was not ready after automatic feature setup: %v", issues["Codex CLI"])
	}
	log, err := os.ReadFile(featureLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hooks", "memories"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("Codex feature %q was not enabled:\n%s", want, log)
		}
	}
}

func TestWireJoinedToolsPreservesCodexMemoryOptOut(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	featureLog := filepath.Join(home, "enabled")
	codexScript := `#!/bin/sh
if [ "$1 $2" = "features list" ]; then
  echo 'hooks stable true'
  echo 'memories experimental false'
elif [ "$1 $2" = "features enable" ]; then
  echo "$3" >> "$FEATURE_LOG"
fi
`
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(codexScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("FEATURE_LOG", featureLog)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := saveCodexMemoryState(codexMemoryOptedOut); err != nil {
		t.Fatal(err)
	}

	ready, issues := wireJoinedTools([]detect.Tool{{Name: "Codex CLI", Home: codexHome, Present: true}}, "/tmp/memsync-test")
	if !ready["Codex CLI"] {
		t.Fatalf("opted-out Codex could not receive shared memory: %v", issues["Codex CLI"])
	}
	if log, _ := os.ReadFile(featureLog); strings.Contains(string(log), "memories") {
		t.Fatalf("join overrode Codex memory opt-out: %s", log)
	}
}

func TestCaptureJoinedToolsMarksFailedSourceNotReady(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "workspace")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCWD) })
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	if _, _, err := crypto.LoadOrCreateKey(paths.KeyPath()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := device.LoadOrCreate(paths.DeviceIDPath()); err != nil {
		t.Fatal(err)
	}
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Ensure(trueBin); err != nil {
		t.Fatal(err)
	}
	encoded := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
	claudeMemory := filepath.Join(home, ".claude", "projects", encoded, "memory")
	if err := os.MkdirAll(filepath.Dir(claudeMemory), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeMemory, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := []detect.Tool{{Name: "Claude Code", Present: true}, {Name: "Codex CLI", Present: true}}
	ready := map[string]bool{"Claude Code": true, "Codex CLI": true}
	issues := make(map[string]error)
	captureJoinedTools(tools, ready, issues)
	if ready["Claude Code"] || issues["Claude Code"] == nil {
		t.Fatalf("failed Claude source remained ready: ready=%v issue=%v", ready["Claude Code"], issues["Claude Code"])
	}
	if !ready["Codex CLI"] || issues["Codex CLI"] != nil {
		t.Fatalf("healthy Codex source was degraded: ready=%v issue=%v", ready["Codex CLI"], issues["Codex CLI"])
	}
}

func TestWireJoinedToolsKeepsHealthyToolReady(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	// A directory where config.toml should be makes Codex wiring fail without
	// affecting Claude Code's independent settings file.
	if err := os.MkdirAll(paths.CodexConfig(), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(fakeBin, "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\nif [ \"$1 $2\" = \"features list\" ]; then\n  echo 'hooks stable true'\n  echo 'memories experimental true'\nfi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tools := []detect.Tool{
		{Name: "Claude Code", Home: filepath.Join(home, ".claude"), Present: true},
		{Name: "Codex CLI", Home: filepath.Join(home, ".codex"), Present: true},
	}
	ready, issues := wireJoinedTools(tools, "/tmp/memsync-test")
	if !ready["Claude Code"] {
		t.Fatalf("healthy Claude Code setup was blocked: %v", issues["Claude Code"])
	}
	if ready["Codex CLI"] || issues["Codex CLI"] == nil {
		t.Fatalf("broken Codex setup was not isolated: ready=%v issue=%v", ready["Codex CLI"], issues["Codex CLI"])
	}
}

func TestWireJoinedToolsDoesNotClaimDisabledClaudeHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ClaudeSettings(), []byte("{\"disableAllHooks\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, issues := wireJoinedTools([]detect.Tool{{Name: "Claude Code", Home: claudeHome, Present: true}}, "/tmp/memsync-test")
	if ready["Claude Code"] || issues["Claude Code"] == nil {
		t.Fatalf("disabled Claude hooks were reported ready: ready=%v issue=%v", ready["Claude Code"], issues["Claude Code"])
	}
}
