package detect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDetectsFreshCLIInstallBeforeHomeExists(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\necho test-version\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-home"))
	t.Setenv("PATH", binDir)

	for _, tool := range All() {
		if !tool.Present {
			t.Fatalf("%s binary was ignored because its home did not exist", tool.Name)
		}
	}
}

func TestStaleHomeWithoutCLIIsNotInstalled(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	for _, tool := range All() {
		if tool.Present {
			t.Fatalf("stale %s home was mistaken for an installed CLI", tool.Name)
		}
	}
}

func TestVersionProbeCannotHangOnInheritedPipe(t *testing.T) {
	fakeBin := t.TempDir()
	claude := filepath.Join(fakeBin, "claude")
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\n/bin/sleep 30 &\necho $! > \"$CHILD_PID_FILE\"\necho claude-test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)
	t.Setenv("CHILD_PID_FILE", pidFile)
	t.Cleanup(func() { killRecordedChild(pidFile) })
	started := time.Now()
	_ = All()
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("tool detection exceeded its bound: %s", elapsed)
	}
}

func killRecordedChild(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return
	}
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Kill()
	}
}
