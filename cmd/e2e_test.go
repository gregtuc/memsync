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
	env := append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.co",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.co",
	)
	memdir := filepath.Join(home, ".claude", "projects", "r", "memory")
	if err := os.MkdirAll(memdir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(memdir, "deploy.md"), []byte("# Deploy\n- hold customer traffic\n"), 0o644)

	run := func(args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Env = env
		out, err := c.CombinedOutput()
		return string(out), err
	}

	if out, err := run("init"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
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
