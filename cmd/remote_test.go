package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestGitHubHTTPSAuthSetup(t *testing.T) {
	if !isGitHubHTTPS("https://github.com/example/memsync-vault") {
		t.Fatal("GitHub HTTPS URL was not recognized")
	}
	for _, rawURL := range []string{
		"git@github.com:example/memsync-vault.git",
		"https://gitlab.com/example/memsync-vault",
		filepath.Join(t.TempDir(), "vault.git"),
	} {
		if isGitHubHTTPS(rawURL) {
			t.Fatalf("non-GitHub-HTTPS URL was recognized: %q", rawURL)
		}
	}

	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")
	gh := filepath.Join(fakeBin, "gh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_LOG\"\nexit 0\n"
	if err := os.WriteFile(gh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_LOG", logPath)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := ensureGitHubCLIAuth(); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(log)
	for _, want := range []string{
		"auth status --hostname github.com",
		"auth setup-git --hostname github.com",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("GitHub setup did not run %q:\n%s", want, text)
		}
	}
}

func TestGitHubHTTPSAuthMissingCLIHasExactRecovery(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := ensureGitHubCLIAuth()
	if err == nil {
		t.Fatal("missing GitHub CLI was accepted")
	}
	for _, want := range []string{"https://cli.github.com", "gh auth login --web --git-protocol https"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing-CLI guidance lacks %q: %v", want, err)
		}
	}
}
