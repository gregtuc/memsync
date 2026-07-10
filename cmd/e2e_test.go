package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEndToEndSingleMachine builds the real binary and drives init -> sync ->
// inject through it, so the git guard hook (which shells out to the binary) is
// exercised for real.
func TestEndToEndSingleMachine(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "memsync")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/gregtuc/memsync").CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	home := t.TempDir()
	cwd := filepath.Join(home, "workspace")
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "claude"), []byte("#!/bin/sh\necho 'Claude Code test'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	env := append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.co",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.co",
	)
	encodedCWD := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
	memdir := filepath.Join(home, ".claude", "projects", encodedCWD, "memory")
	if err := os.MkdirAll(memdir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(memdir, "deploy.md"), []byte("# Deploy\n- hold customer traffic\n"), 0o644)

	run := func(args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Env = env
		c.Dir = cwd
		out, err := c.CombinedOutput()
		return string(out), err
	}

	if out, err := run("init"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	status, err := run("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, status)
	}
	if strings.Contains(status, "✗ Codex") {
		t.Fatalf("Claude-only setup reported absent Codex as broken:\n%s", status)
	}
	if !strings.Contains(status, "`memsync join`") || !strings.Contains(status, "`memsync pair`") || strings.Contains(status, "remote create") {
		t.Fatalf("status contradicted the simple two-laptop flow:\n%s", status)
	}
	if out, err := run("sync", "--tool", "claude"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	vaultDir := filepath.Join(home, "data", "memsync", "vault")
	if encs, _ := filepath.Glob(filepath.Join(vaultDir, "*.enc")); len(encs) != 1 {
		t.Fatalf("want 1 encrypted record, got %d", len(encs))
	}

	out, err := run("inject", "--tool", "codex")
	if err != nil {
		t.Fatalf("inject: %v\n%s", err, out)
	}
	var payload struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("inject output is not JSON: %v\n%q", err, out)
	}
	if !strings.Contains(payload.HookSpecificOutput.AdditionalContext, "hold customer traffic") {
		t.Fatalf("claude memory not injected into codex: %q", payload.HookSpecificOutput.AdditionalContext)
	}

	c1 := commitCount(t, vaultDir, env)
	if _, err := run("sync", "--tool", "claude"); err != nil {
		t.Fatal(err)
	}
	if c2 := commitCount(t, vaultDir, env); c1 != c2 {
		t.Fatalf("identical re-sync churned commits: %d -> %d", c1, c2)
	}

	// An unreadable/incomplete native source must never look like an empty
	// snapshot and delete the last good encrypted record.
	if err := os.RemoveAll(memdir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memdir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run("sync", "--tool", "claude"); err != nil || !strings.Contains(out, "read claude memory source") {
		t.Fatalf("source failure was not reported fail-open: %v\n%s", err, out)
	}
	if encs, _ := filepath.Glob(filepath.Join(vaultDir, "*.enc")); len(encs) != 1 {
		t.Fatalf("source read failure deleted the last good record; got %d", len(encs))
	}

	os.WriteFile(filepath.Join(vaultDir, "leak.txt"), []byte("secret"), 0o644)
	git := func(args ...string) *exec.Cmd {
		c := exec.Command("git", args...)
		c.Dir = vaultDir
		c.Env = env
		return c
	}
	git("add", "-A").Run()
	if err := git("commit", "-m", "leak").Run(); err == nil {
		t.Fatal("guard did not block a plaintext commit")
	}
}

func TestInitIsZeroConfigurationAndConcise(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "memsync")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/gregtuc/memsync").CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	home := t.TempDir()
	cwd := filepath.Join(home, "workspace")
	fakeBin := filepath.Join(home, "bin")
	for _, dir := range []string{cwd, fakeBin, filepath.Join(home, ".claude"), filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	claudeScript := "#!/bin/sh\necho '2.1.0 (Claude Code)'\n"
	codexScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo 'codex-cli 1.0.0'
elif [ "$1" = "features" ] && [ "$2" = "list" ]; then
  [ -d "$CODEX_HOME" ] || { echo 'missing CODEX_HOME' >&2; exit 1; }
  echo 'hooks stable true'
  echo 'memories experimental false'
elif [ "$1" = "features" ] && [ "$2" = "enable" ]; then
  echo "$3" >> "$FEATURE_LOG"
  exit 0
else
  exit 0
fi
`
	for name, script := range map[string]string{"claude": claudeScript, "codex": codexScript} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(),
		"HOME="+home,
		"CODEX_HOME="+filepath.Join(home, ".codex"),
		"XDG_CONFIG_HOME="+filepath.Join(home, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FEATURE_LOG="+filepath.Join(home, "features-enabled"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.co",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.co",
	)
	cmd := exec.Command(bin, "init")
	cmd.Dir = cwd
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{
		"Set up Claude Code and Codex",
		"Encrypted memory sync is configured",
		"When Codex asks, approve memsync to finish",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("init output missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"Codex Memories", "SessionStart", "SessionEnd", ".codex", "device-id", "memsync status"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("init output exposed implementation detail %q:\n%s", unwanted, text)
		}
	}
	enabled, err := os.ReadFile(filepath.Join(home, "features-enabled"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(enabled)) != "memories" {
		t.Fatalf("zero-config init did not enable Codex's memory source: %q", enabled)
	}
	if lines := len(strings.Split(strings.TrimSpace(text), "\n")); lines > 8 {
		t.Fatalf("init output is still too noisy (%d lines):\n%s", lines, text)
	}

	// A broken Codex setup must not prevent an otherwise healthy Claude setup.
	brokenCodex := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo codex-test; else echo 'bad Codex config' >&2; exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(brokenCodex), 0o755); err != nil {
		t.Fatal(err)
	}
	secondHome := t.TempDir()
	secondCWD := filepath.Join(secondHome, "workspace")
	if err := os.MkdirAll(secondCWD, 0o755); err != nil {
		t.Fatal(err)
	}
	second := exec.Command(bin, "init")
	second.Dir = secondCWD
	second.Env = append(os.Environ(),
		"HOME="+secondHome,
		"CODEX_HOME="+filepath.Join(secondHome, ".codex"),
		"XDG_CONFIG_HOME="+filepath.Join(secondHome, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(secondHome, "data"),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	secondOut, err := second.CombinedOutput()
	if err != nil {
		t.Fatalf("Claude setup was blocked by broken Codex: %v\n%s", err, secondOut)
	}
	if !strings.Contains(string(secondOut), "Set up Claude Code") || !strings.Contains(string(secondOut), "Codex needs attention") {
		t.Fatalf("per-tool degradation is unclear:\n%s", secondOut)
	}
}

func TestInitDoesNotClaimReadyWhenMemorySourceFails(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "memsync")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/gregtuc/memsync").CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	home := t.TempDir()
	cwd := filepath.Join(home, "workspace")
	fakeBin := filepath.Join(home, "bin")
	for _, dir := range []string{cwd, fakeBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "claude"), []byte("#!/bin/sh\necho claude-test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gitPath, filepath.Join(fakeBin, "git")); err != nil {
		t.Fatal(err)
	}

	// A file where Claude's memory directory should be forces CollectClaudeAt
	// to return a source error instead of a valid empty snapshot.
	encodedCWD := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
	memdir := filepath.Join(home, ".claude", "projects", encodedCWD, "memory")
	if err := os.MkdirAll(filepath.Dir(memdir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memdir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "init")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
		"PATH="+fakeBin,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("init succeeded despite an unreadable memory source:\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "could not connect Claude Code") {
		t.Fatalf("init did not identify the failed tool:\n%s", text)
	}
	for _, falseSuccess := range []string{"Set up Claude Code", "Encrypted memory sync is configured"} {
		if strings.Contains(text, falseSuccess) {
			t.Fatalf("init falsely reported %q after source failure:\n%s", falseSuccess, text)
		}
	}
}

func commitCount(t *testing.T, dir string, env []string) int {
	c := exec.Command("git", "rev-list", "--count", "HEAD")
	c.Dir = dir
	c.Env = env
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}
