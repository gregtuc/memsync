package vault

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
)

func setupHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@e.co")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@e.co")
}

func TestEnsureCreatesRepoAndGuards(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), ".git")); err != nil {
		t.Fatal("vault is not a git repo")
	}
	if !GuardsInstalled() {
		t.Fatal("guards not installed")
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), ".gitattributes")); err != nil {
		t.Fatal(".gitattributes missing")
	}
	tracked, err := git(paths.VaultDir(), "show", "HEAD:.gitattributes")
	if err != nil || tracked != attributesContent {
		t.Fatalf("managed attributes were not committed at initialization: %q, %v", tracked, err)
	}
	for name, mode := range map[string]string{
		"pre-commit":       "tree",
		"pre-merge-commit": "tree",
		"pre-push":         "push",
	} {
		b, err := os.ReadFile(filepath.Join(HooksDir(), name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "guard --mode "+mode) {
			t.Fatalf("%s does not select %s guard mode: %q", name, mode, b)
		}
	}
}

func TestEnsureOverridesGlobalSigningHooksAndIdentity(t *testing.T) {
	setupHome(t)
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	failingHooks := filepath.Join(t.TempDir(), "global-hooks")
	if err := os.MkdirAll(failingHooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failingHooks, "pre-commit"), []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, t.TempDir(), "config", "--global", "commit.gpgSign", "true")
	runGitAt(t, t.TempDir(), "config", "--global", "gpg.program", "/bin/false")
	runGitAt(t, t.TempDir(), "config", "--global", "core.hooksPath", failingHooks)

	if err := Ensure(truePath(t)); err != nil {
		t.Fatalf("global Git settings broke vault initialization: %v", err)
	}
	author, err := git(paths.VaultDir(), "show", "-s", "--format=%an <%ae>", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(author) != "memsync <memsync@localhost.invalid>" {
		t.Fatalf("vault leaked the user's Git identity: %q", author)
	}
	if sign, err := git(paths.VaultDir(), "config", "--local", "--get", "commit.gpgSign"); err != nil || strings.TrimSpace(sign) != "false" {
		t.Fatalf("commit signing was not disabled locally: %q, %v", sign, err)
	}
}

func TestNeedsPushSkipsNetworkWhenVaultIsCurrent(t *testing.T) {
	setupHome(t)
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitAt(t, filepath.Dir(remote), "init", "--bare", "-q", remote)
	if err := SetRemote(remote); err != nil {
		t.Fatal(err)
	}
	if !NeedsPush() {
		t.Fatal("first push was not detected")
	}
	if err := Push(); err != nil {
		t.Fatal(err)
	}
	if NeedsPush() {
		t.Fatal("current vault should not need another push")
	}
	writeRecord(t, paths.VaultDir(), key, "new.enc", "pending")
	if err := CommitAll("pending"); err != nil {
		t.Fatal(err)
	}
	if !NeedsPush() {
		t.Fatal("local commit was not detected")
	}
}

func TestGuardTreeAcceptsCiphertextAndRejectsPlaintext(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	env, _ := crypto.Encrypt(key, []byte(`{"origin":"claude","body":"x"}`))
	if err := os.WriteFile(filepath.Join(paths.VaultDir(), "a.enc"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	// ciphertext record + .gitattributes control file must pass
	if err := GuardTree(key); err != nil {
		t.Fatalf("guard rejected a clean tree: %v", err)
	}
	// a plaintext file must fail closed
	os.WriteFile(filepath.Join(paths.VaultDir(), "leak.txt"), []byte("secret"), 0o644)
	if err := GuardTree(key); err == nil {
		t.Fatal("guard accepted plaintext")
	}
}

func TestGuardTreeRejectsWrongKeyCiphertext(t *testing.T) {
	setupHome(t)
	Ensure(truePath(t))
	k1, _ := crypto.GenerateKey()
	k2, _ := crypto.GenerateKey()
	env, _ := crypto.Encrypt(k1, []byte("data"))
	os.WriteFile(filepath.Join(paths.VaultDir(), "a.enc"), env, 0o644)
	if err := GuardTree(k2); err == nil {
		t.Fatal("guard accepted ciphertext that does not decrypt under the local key")
	}
}

func TestGuardTreeRejectsSymlinks(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(paths.VaultDir(), "linked.enc")
	if err := os.Symlink("outside-the-vault", link); err != nil {
		t.Fatal(err)
	}
	if _, err := git(paths.VaultDir(), "add", "linked.enc"); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	if err := GuardTree(key); err == nil || !strings.Contains(err.Error(), "unsupported git mode") {
		t.Fatalf("guard accepted a symlink: %v", err)
	}
}

func TestGuardTreeSeesIgnoredPlaintextAndCommitForcesValidatedRecords(t *testing.T) {
	setupHome(t)
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	exclude := filepath.Join(paths.VaultDir(), ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("ignored-secret.txt\n*.enc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(paths.VaultDir(), "ignored-secret.txt")
	if err := os.WriteFile(secret, []byte("ignored plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := GuardTree(key); err == nil || !strings.Contains(err.Error(), "ignored-secret.txt") {
		t.Fatalf("guard ignored excluded plaintext: %v", err)
	}
	if err := os.Remove(secret); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, paths.VaultDir(), key, "ignored-by-git.enc", "validated record")
	if err := CommitAll("force validated record"); err != nil {
		t.Fatal(err)
	}
	tracked := gitOutput(t, paths.VaultDir(), "ls-files", "--error-unmatch", "ignored-by-git.enc")
	if tracked != "ignored-by-git.enc" {
		t.Fatalf("validated ignored record was not committed: %q", tracked)
	}
}

func TestGuardTreeChecksStagedBlobNotOnlyWorkingTree(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	env, _ := crypto.Encrypt(key, []byte("safe ciphertext"))
	path := filepath.Join(paths.VaultDir(), "a.enc")
	if err := os.WriteFile(path, []byte("PLAINTEXT STAGED SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(paths.VaultDir(), "add", "a.enc"); err != nil {
		t.Fatal(err)
	}
	// Restore only the working tree. Git would still commit the staged plaintext.
	if err := os.WriteFile(path, env, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GuardTree(key); err == nil || !strings.Contains(err.Error(), "a.enc") {
		t.Fatalf("guard accepted staged plaintext hidden by ciphertext in the working tree: %v", err)
	}
}

func TestGuardTreeRequiresExactManagedAttributes(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	path := filepath.Join(paths.VaultDir(), ".gitattributes")
	if err := os.WriteFile(path, []byte("secret disguised as control metadata\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(paths.VaultDir(), "add", ".gitattributes"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(attributesContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GuardTree(key); err == nil || !strings.Contains(err.Error(), ".gitattributes") {
		t.Fatalf("guard accepted arbitrary staged control-file plaintext: %v", err)
	}
}

func TestGuardPushRejectsPlaintextDeletedFromCurrentTree(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	base := gitOutput(t, paths.VaultDir(), "rev-parse", "HEAD")

	leak := filepath.Join(paths.VaultDir(), "leak.txt")
	if err := os.WriteFile(leak, []byte("PLAINTEXT HISTORICAL SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, paths.VaultDir(), "add", "leak.txt")
	runGitAt(t, paths.VaultDir(), "commit", "--no-verify", "-q", "-m", "hidden plaintext")
	runGitAt(t, paths.VaultDir(), "rm", "-q", "leak.txt")
	runGitAt(t, paths.VaultDir(), "commit", "--no-verify", "-q", "-m", "delete plaintext")

	if err := GuardTree(key); err != nil {
		t.Fatalf("current tree should be clean, got %v", err)
	}
	update := pushUpdate(t, paths.VaultDir(), base)
	if err := GuardPush(key, strings.NewReader(update)); err == nil || !strings.Contains(err.Error(), "leak.txt") {
		t.Fatalf("pre-push guard missed deleted historical plaintext: %v", err)
	}
}

func TestGuardPushAcceptsCiphertextHistoryAndDeletionUpdates(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	base := gitOutput(t, paths.VaultDir(), "rev-parse", "HEAD")
	writeRecord(t, paths.VaultDir(), key, "records/device-a/safe.enc", "safe memory")
	runGitAt(t, paths.VaultDir(), "add", "-A")
	runGitAt(t, paths.VaultDir(), "commit", "--no-verify", "-q", "-m", "ciphertext")

	if err := GuardPush(key, strings.NewReader(pushUpdate(t, paths.VaultDir(), base))); err != nil {
		t.Fatalf("pre-push guard rejected ciphertext-only history: %v", err)
	}
	zero := strings.Repeat("0", len(base))
	deleteUpdate := fmt.Sprintf("(delete) %s refs/heads/main %s\n", zero, base)
	if err := GuardPush(key, strings.NewReader(deleteUpdate)); err != nil {
		t.Fatalf("pre-push guard rejected ref deletion: %v", err)
	}
	if err := GuardPush(key, strings.NewReader("malformed\n")); err == nil {
		t.Fatal("pre-push guard accepted malformed hook input")
	}
}

func TestInstalledPrePushGuardRejectsHiddenPlaintextHistory(t *testing.T) {
	setupHome(t)
	bin := filepath.Join(t.TempDir(), "memsync")
	build := exec.Command("go", "build", "-o", bin, "github.com/gregtuc/memsync")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build memsync: %v\n%s", err, out)
	}
	if _, _, err := crypto.LoadOrCreateKey(paths.KeyPath()); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(bin); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitAt(t, filepath.Dir(remote), "init", "--bare", "-q", remote)
	if err := SetRemote(remote); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, paths.VaultDir(), "push", "-q", "-u", "origin", "HEAD")
	branch := gitOutput(t, paths.VaultDir(), "symbolic-ref", "--short", "HEAD")
	remoteBefore := gitOutput(t, remote, "rev-parse", "refs/heads/"+branch)

	leak := filepath.Join(paths.VaultDir(), "leak.txt")
	if err := os.WriteFile(leak, []byte("should never reach remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, paths.VaultDir(), "add", "leak.txt")
	runGitAt(t, paths.VaultDir(), "commit", "--no-verify", "-q", "-m", "bypass pre-commit")
	runGitAt(t, paths.VaultDir(), "rm", "-q", "leak.txt")
	runGitAt(t, paths.VaultDir(), "commit", "--no-verify", "-q", "-m", "hide leak")

	push := exec.Command("git", "-C", paths.VaultDir(), "push", "-q", "origin", "HEAD")
	out, err := push.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "leak.txt") {
		t.Fatalf("installed pre-push hook did not reject hidden history: err=%v\n%s", err, out)
	}
	remoteAfter := gitOutput(t, remote, "rev-parse", "refs/heads/"+branch)
	if remoteAfter != remoteBefore {
		t.Fatalf("remote advanced despite rejected history: %s -> %s", remoteBefore, remoteAfter)
	}
}

func TestRecordsListsOnlyEnc(t *testing.T) {
	setupHome(t)
	Ensure(truePath(t))
	os.WriteFile(filepath.Join(paths.VaultDir(), "a.enc"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(paths.VaultDir(), "records", "device-a"), 0o755)
	os.WriteFile(filepath.Join(paths.VaultDir(), "records", "device-a", "b.enc"), []byte("z"), 0o644)
	os.WriteFile(filepath.Join(paths.VaultDir(), "notes.txt"), []byte("y"), 0o644)
	recs, err := Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 nested/root .enc records, got %d", len(recs))
	}
}

func TestPushRebasesDivergentAppendOnlyRecords(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "cfg"))
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@e.co")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@e.co")

	remote := filepath.Join(root, "remote.git")
	runGitAt(t, root, "init", "--bare", "-q", remote)
	aData := filepath.Join(root, "a-data")
	bData := filepath.Join(root, "b-data")
	t.Setenv("XDG_DATA_HOME", aData)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	if err := crypto.SaveKey(paths.KeyPath(), key); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, paths.VaultDir(), key, "records/device-a/first.enc", "from a first")
	if err := CommitAll("a first"); err != nil {
		t.Fatal(err)
	}
	if err := SetRemote(remote); err != nil {
		t.Fatal(err)
	}
	if err := Push(); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(bData, "memsync"), 0o755); err != nil {
		t.Fatal(err)
	}
	bVault := filepath.Join(bData, "memsync", "vault")
	runGitAt(t, root, "clone", "-q", "--branch", "main", remote, bVault)
	t.Setenv("XDG_DATA_HOME", bData)
	if err := InstallGuards(truePath(t)); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, paths.VaultDir(), key, "records/device-b/only.enc", "from b")
	if err := CommitAll("b diverges"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_DATA_HOME", aData)
	writeRecord(t, paths.VaultDir(), key, "records/device-a/second.enc", "from a second")
	if err := CommitAll("a advances remote"); err != nil {
		t.Fatal(err)
	}
	if err := Push(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_DATA_HOME", bData)
	if err := Push(); err != nil {
		t.Fatalf("divergent append-only push did not converge: %v", err)
	}

	check := filepath.Join(root, "check")
	runGitAt(t, root, "clone", "-q", "--branch", "main", remote, check)
	for _, name := range []string{
		"records/device-a/first.enc",
		"records/device-a/second.enc",
		"records/device-b/only.enc",
	} {
		if _, err := os.Stat(filepath.Join(check, filepath.FromSlash(name))); err != nil {
			t.Fatalf("reconciled remote lost %s: %v", name, err)
		}
	}
}

func TestPullRefusesUnsafeFetchedHistoryBeforeMovingHEAD(t *testing.T) {
	setupHome(t)
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, paths.VaultDir(), key, "safe.enc", "safe current memory")
	if err := CommitAll("safe"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitAt(t, filepath.Dir(remote), "init", "--bare", "-q", remote)
	runGitAt(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := SetRemote(remote); err != nil {
		t.Fatal(err)
	}
	if err := Push(); err != nil {
		t.Fatal(err)
	}
	before := gitOutput(t, paths.VaultDir(), "rev-parse", "HEAD")

	attacker := filepath.Join(t.TempDir(), "attacker")
	runGitAt(t, filepath.Dir(attacker), "clone", "-q", "--branch", "main", remote, attacker)
	if err := os.WriteFile(filepath.Join(attacker, "remote-plaintext.txt"), []byte("malicious remote plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, attacker, "add", "remote-plaintext.txt")
	runGitAt(t, attacker, "commit", "-q", "-m", "unsafe remote update")
	runGitAt(t, attacker, "push", "-q", "origin", "main")

	if err := Pull(); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("unsafe fetched history was accepted: %v", err)
	}
	if after := gitOutput(t, paths.VaultDir(), "rev-parse", "HEAD"); after != before {
		t.Fatalf("unsafe fetch moved live HEAD: %s -> %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), "remote-plaintext.txt")); !os.IsNotExist(err) {
		t.Fatalf("unsafe remote file reached working tree: %v", err)
	}
}

func TestGitRemoteIsBoundedAndNonInteractive(t *testing.T) {
	binDir := t.TempDir()
	gitPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldTimeout := remoteOperationTimeout
	remoteOperationTimeout = 75 * time.Millisecond
	defer func() { remoteOperationTimeout = oldTimeout }()
	oldWaitDelay := remoteCommandWaitDelay
	remoteCommandWaitDelay = 100 * time.Millisecond
	defer func() { remoteCommandWaitDelay = oldWaitDelay }()
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	started := time.Now()
	_, err := gitRemote(ctx, t.TempDir(), "fetch", "origin")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want bounded timeout, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("remote git exceeded bounded timeout: %s", elapsed)
	}

	// Killing the top-level process must also stop waiting on a helper child
	// that inherited Git's output pipes.
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nsleep 5 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	started = time.Now()
	if _, err := gitRemote(ctx, t.TempDir(), "fetch", "origin"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("hanging helper child was not timed out: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("credential-helper child held pipes past WaitDelay: %s", elapsed)
	}

	script := "#!/bin/sh\nprintf '%s|%s|%s|%s' \"$GIT_TERMINAL_PROMPT\" \"$GCM_INTERACTIVE\" \"$SSH_ASKPASS_REQUIRE\" \"$*\"\n"
	if err := os.WriteFile(gitPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := gitRemote(ctx, binDir, "fetch", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "0|never|never|") || !strings.Contains(out, "-c credential.interactive=never") {
		t.Fatalf("remote git was not fully noninteractive: %q", out)
	}

	secretURL := "https://token-value@example.invalid/private.git"
	script = "#!/bin/sh\nprintf '%s' \"$*\" >&2\nexit 1\n"
	if err := os.WriteFile(gitPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = gitRemote(ctx, binDir, "ls-remote", "--", secretURL)
	if err == nil || strings.Contains(err.Error(), "token-value") {
		t.Fatalf("credential-bearing URL leaked through Git error: %v", err)
	}
}

func TestRemoteCredentialDetectionAndDisplay(t *testing.T) {
	secret := "https://user:token-value@example.com/private/vault.git"
	if !RemoteHasCredentials(secret) {
		t.Fatal("HTTP credentials were not detected")
	}
	if display := DisplayRemoteURL(secret); strings.Contains(display, "user") || strings.Contains(display, "token-value") {
		t.Fatalf("credential-bearing URL was not redacted: %q", display)
	}
	if RemoteHasCredentials("ssh://git@example.com/team/vault.git") {
		t.Fatal("ordinary SSH username was treated as a secret")
	}
}

func TestStageCloneValidatesBeforeAtomicActivation(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, paths.VaultDir(), key, "records/old/only.enc", "old live vault")
	if err := CommitAll("old live"); err != nil {
		t.Fatal(err)
	}
	oldHead, err := git(paths.VaultDir(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	remote := makeRemoteVault(t, key)
	stage, err := StageClone(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Discard()
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), "records", "old", "only.enc")); err != nil {
		t.Fatalf("staging changed live vault: %v", err)
	}
	wrong, _ := crypto.GenerateKey()
	if err := stage.Validate(wrong); err == nil {
		t.Fatal("staged vault validated under the wrong key")
	}
	if err := stage.Activate(); err == nil {
		t.Fatal("unvalidated staged vault activated")
	}
	if head, _ := git(paths.VaultDir(), "rev-parse", "HEAD"); head != oldHead {
		t.Fatal("failed staged validation changed live HEAD")
	}

	if err := stage.Validate(key); err != nil {
		t.Fatal(err)
	}
	stagedRecord := filepath.Join(stage.Path(), "records", "new", "only.enc")
	goodRecord, err := os.ReadFile(stagedRecord)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedRecord, []byte("changed after validation"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stage.Activate(); err == nil {
		t.Fatal("staged vault changed after validation but still activated")
	}
	if head, _ := git(paths.VaultDir(), "rev-parse", "HEAD"); head != oldHead {
		t.Fatal("post-validation mutation changed live HEAD")
	}
	if err := os.WriteFile(stagedRecord, goodRecord, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stage.Validate(key); err != nil {
		t.Fatal(err)
	}
	if err := stage.Activate(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), "records", "new", "only.enc")); err != nil {
		t.Fatalf("activated vault is missing staged record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), "records", "old", "only.enc")); !os.IsNotExist(err) {
		t.Fatalf("old vault survived atomic replacement unexpectedly: %v", err)
	}
}

func TestStageCloneRejectsPlaintextHiddenInHistory(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	remote := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "init", "-q")
	runGitAt(t, source, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(source, ".gitattributes"), []byte(attributesContent), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "add", ".gitattributes")
	runGitAt(t, source, "commit", "-q", "-m", "safe base")
	if err := os.WriteFile(filepath.Join(source, "forgotten-secret.txt"), []byte("plaintext should not survive in history"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "add", "forgotten-secret.txt")
	runGitAt(t, source, "commit", "-q", "-m", "unsafe history")
	if err := os.Remove(filepath.Join(source, "forgotten-secret.txt")); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "add", "-A")
	runGitAt(t, source, "commit", "-q", "-m", "clean current tree")
	runGitAt(t, root, "init", "--bare", "-q", remote)
	runGitAt(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	runGitAt(t, source, "push", "-q", remote, "main:main")

	stage, err := StageClone(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Discard()
	key, _ := crypto.GenerateKey()
	if err := stage.Validate(key); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("staged clone accepted plaintext hidden in history: %v", err)
	}
}

func TestCloneFailurePreservesLiveVault(t *testing.T) {
	setupHome(t)
	if err := Ensure(truePath(t)); err != nil {
		t.Fatal(err)
	}
	before, err := git(paths.VaultDir(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := Clone(filepath.Join(t.TempDir(), "missing.git")); err == nil {
		t.Fatal("clone of missing remote unexpectedly succeeded")
	}
	after, err := git(paths.VaultDir(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("failed clone destroyed live vault: %v", err)
	}
	if before != after {
		t.Fatalf("failed clone changed live vault: %s -> %s", before, after)
	}
}

func TestStageCloneDoesNotTrustBrokenRemoteHEAD(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	remote := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "init", "-q")
	runGitAt(t, source, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(source, ".gitattributes"), []byte(attributesContent), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "add", ".gitattributes")
	runGitAt(t, source, "commit", "-q", "-m", "main vault")
	runGitAt(t, root, "init", "--bare", "-q", remote)
	// Deliberately leave the bare remote's HEAD pointing at nonexistent master.
	runGitAt(t, remote, "symbolic-ref", "HEAD", "refs/heads/master")
	runGitAt(t, source, "push", "-q", remote, "main:main")

	stage, err := StageClone(remote)
	if err != nil {
		t.Fatalf("stage clone trusted broken remote HEAD: %v", err)
	}
	defer stage.Discard()
	branch := gitOutput(t, stage.Path(), "symbolic-ref", "--short", "HEAD")
	if branch != "main" {
		t.Fatalf("staged branch = %q, want main", branch)
	}
	if _, err := os.Stat(filepath.Join(stage.Path(), ".gitattributes")); err != nil {
		t.Fatalf("main branch was not checked out: %v", err)
	}
}

func writeRecord(t *testing.T, dir string, key []byte, name, body string) {
	t.Helper()
	env, err := crypto.Encrypt(key, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, env, 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeRemoteVault(t *testing.T, key []byte) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	remote := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitAt(t, source, "init", "-q")
	if err := os.WriteFile(filepath.Join(source, ".gitattributes"), []byte(attributesContent), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, source, key, "records/new/only.enc", "new staged vault")
	runGitAt(t, source, "add", "-A")
	runGitAt(t, source, "commit", "-q", "-m", "new vault")
	runGitAt(t, root, "init", "--bare", "-q", remote)
	runGitAt(t, source, "remote", "add", "origin", remote)
	runGitAt(t, source, "push", "-q", "-u", "origin", "HEAD")
	return remote
}

func runGitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func pushUpdate(t *testing.T, dir, remoteOID string) string {
	t.Helper()
	branch := gitOutput(t, dir, "symbolic-ref", "--short", "HEAD")
	localOID := gitOutput(t, dir, "rev-parse", "HEAD")
	ref := "refs/heads/" + branch
	return fmt.Sprintf("%s %s %s %s\n", ref, localOID, ref, remoteOID)
}

func truePath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
