package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func TestActivateRemoteRollsBackFailedCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
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
	oldRemote := filepath.Join(home, "old.git")
	newRemote := filepath.Join(home, "reject.git")
	for _, remote := range []string{oldRemote, newRemote} {
		cmd := exec.Command("git", "init", "--bare", "-q", remote)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("init bare: %v\n%s", err, out)
		}
	}
	if err := vault.SetRemote(oldRemote); err != nil {
		t.Fatal(err)
	}
	if err := vault.Push(); err != nil {
		t.Fatal(err)
	}
	rejectHook := filepath.Join(newRemote, "hooks", "pre-receive")
	if err := os.WriteFile(rejectHook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := activateRemote(newRemote); err == nil {
		t.Fatal("rejecting candidate remote unexpectedly activated")
	}
	if got := vault.RemoteURL(); got != oldRemote {
		t.Fatalf("failed candidate replaced working origin: got %q want %q", got, oldRemote)
	}
	if err := activateRemote("https://token-value@example.invalid/vault.git"); err == nil {
		t.Fatal("credential-bearing HTTP remote was accepted")
	}
	if got := vault.RemoteURL(); got != oldRemote {
		t.Fatalf("credential rejection changed origin: got %q want %q", got, oldRemote)
	}
}
