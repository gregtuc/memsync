// Package vault manages the encrypted git repo — the only thing that crosses
// machines. Its working tree holds nothing but memsync ciphertext, enforced by
// guard hooks installed via core.hooksPath (which, unlike .git/hooks, we control
// and which survives a fresh clone once re-provisioned).
package vault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
)

// HooksDir holds the guard hooks; lives outside the working tree so it is never committed.
func HooksDir() string { return filepath.Join(paths.DataDir(), "git-hooks") }

// Ensure creates the vault repo (if absent) and (re)installs the guards.
func Ensure(binPath string) error {
	dir := paths.VaultDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !isRepo(dir) {
		if _, err := git(dir, "init", "-q"); err != nil {
			return err
		}
		if _, err := git(dir, "commit", "--allow-empty", "-q", "-m", "init vault"); err != nil {
			return err
		}
	}
	if err := writeAttributes(dir); err != nil {
		return err
	}
	return InstallGuards(binPath)
}

// InstallGuards writes the pre-commit/pre-push guard scripts and points the
// vault at them. Re-run at every bootstrap since hooks are not cloned.
func InstallGuards(binPath string) error {
	hd := HooksDir()
	if err := os.MkdirAll(hd, 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\nexec " + shellQuote(binPath) + " guard\n"
	for _, name := range []string{"pre-commit", "pre-merge-commit", "pre-push"} {
		if err := os.WriteFile(filepath.Join(hd, name), []byte(script), 0o755); err != nil {
			return err
		}
	}
	_, err := git(paths.VaultDir(), "config", "core.hooksPath", hd)
	return err
}

// GuardsInstalled reports whether core.hooksPath points at our guard dir.
func GuardsInstalled() bool {
	out, err := git(paths.VaultDir(), "config", "--get", "core.hooksPath")
	return err == nil && strings.TrimSpace(out) == HooksDir()
}

// GuardTree fails if any tracked or staged file in the working tree is not a
// valid memsync envelope under the local key. This is the positive invariant
// that keeps plaintext out of every pushed git object.
func GuardTree(key []byte) error {
	dir := paths.VaultDir()
	out, err := git(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	for _, f := range strings.Split(out, "\x00") {
		if f == "" || allowedControlFile(f) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		if !crypto.IsCiphertext(b) {
			return fmt.Errorf("guard: %s is not memsync ciphertext — refusing", f)
		}
		if _, err := crypto.Decrypt(key, b); err != nil {
			return fmt.Errorf("guard: %s does not decrypt under local key — refusing: %w", f, err)
		}
	}
	return nil
}

// CommitAll stages the working tree and commits if there is anything to commit.
// The commit triggers the pre-commit guard, so a plaintext file aborts it.
func CommitAll(msg string) error {
	dir := paths.VaultDir()
	if _, err := git(dir, "add", "-A"); err != nil {
		return err
	}
	out, err := git(dir, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	_, err = git(dir, "commit", "-q", "-m", msg)
	return err
}

// Push sends the vault to origin (no-op if no remote). The pre-push guard runs.
func Push() error {
	if !HasRemote() {
		return nil
	}
	_, err := git(paths.VaultDir(), "push", "-q", "-u", "origin", "HEAD")
	return err
}

// Pull fast-forwards the vault from origin (no-op if no remote). Best-effort.
func Pull() error {
	if !HasRemote() {
		return nil
	}
	_, err := git(paths.VaultDir(), "pull", "-q", "--ff-only")
	return err
}

// Records returns the absolute paths of the vault's encrypted record files.
func Records() ([]string, error) {
	entries, err := os.ReadDir(paths.VaultDir())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".enc") {
			out = append(out, filepath.Join(paths.VaultDir(), e.Name()))
		}
	}
	return out, nil
}

// LastCommit returns a short description of the latest vault commit, or "".
func LastCommit() string {
	out, err := git(paths.VaultDir(), "log", "-1", "--pretty=%cr — %s")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Clone replaces the local vault with a fresh clone of url (used by `join`).
func Clone(url string) error {
	dir := paths.VaultDir()
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	_, err := git(filepath.Dir(dir), "clone", "-q", url, dir)
	return err
}

// RemoteReachable reports whether origin can be listed (auth + network OK).
func RemoteReachable() bool {
	_, err := git(paths.VaultDir(), "ls-remote", "origin")
	return err == nil
}

// HasRemote reports whether an "origin" remote is configured.
func HasRemote() bool {
	out, err := git(paths.VaultDir(), "remote")
	return err == nil && strings.Contains(out, "origin")
}

// SetRemote points origin at url (adding or updating it).
func SetRemote(url string) error {
	dir := paths.VaultDir()
	if HasRemote() {
		_, err := git(dir, "remote", "set-url", "origin", url)
		return err
	}
	_, err := git(dir, "remote", "add", "origin", url)
	return err
}

// RemoteURL returns origin's URL, or "".
func RemoteURL() string {
	out, err := git(paths.VaultDir(), "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// allowedControlFile is the small allowlist of plaintext files memsync itself
// manages in the vault; everything else must be ciphertext.
func allowedControlFile(name string) bool {
	switch name {
	case ".gitattributes", ".gitignore":
		return true
	}
	return false
}

func isRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func writeAttributes(dir string) error {
	// Treat envelopes as opaque; git's text merge must never run on ciphertext.
	content := "*.enc -text -diff merge=binary\n"
	return os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(content), 0o644)
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
