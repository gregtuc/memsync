// Package vault manages the encrypted git repo - the only thing that crosses
// machines. Its working tree holds nothing but memsync ciphertext, enforced by
// guard hooks installed via core.hooksPath (which, unlike .git/hooks, we control
// and which survives a fresh clone once re-provisioned).
package vault

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
)

const (
	attributesContent = "*.enc -text -diff merge=binary\n"
	pushAttempts      = 3
)

// remoteOperationTimeout bounds the whole fetch/rebase/push or clone operation,
// not each individual git subprocess. It is a variable so timeout behavior can
// be exercised quickly in tests.
var remoteOperationTimeout = 12 * time.Second

// A killed git process can leave a credential-helper child holding its output
// pipes open. WaitDelay bounds that cleanup path after the operation context
// expires instead of letting a session hook hang indefinitely.
var remoteCommandWaitDelay = 500 * time.Millisecond

// HooksDir holds the guard hooks; lives outside the working tree so it is never committed.
func HooksDir() string { return filepath.Join(paths.DataDir(), "git-hooks") }

// Ensure creates the vault repo (if absent) and (re)installs the guards.
func Ensure(binPath string) error {
	dir := paths.VaultDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	created := false
	if !isRepo(dir) {
		if _, err := git(dir, "init", "-q"); err != nil {
			return err
		}
		if _, err := git(dir, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
			return err
		}
		created = true
	}
	if err := configureRepo(dir); err != nil {
		return err
	}
	if err := InstallGuards(binPath); err != nil {
		return err
	}
	if err := writeAttributes(dir); err != nil {
		return err
	}
	if created {
		if _, err := git(dir, "add", ".gitattributes"); err != nil {
			return err
		}
		if _, err := git(dir, "commit", "-q", "-m", "init vault"); err != nil {
			return err
		}
	}
	return nil
}

func configureRepo(dir string) error {
	for key, value := range map[string]string{
		"user.name":      "memsync",
		"user.email":     "memsync@localhost.invalid",
		"commit.gpgSign": "false",
	} {
		if _, err := git(dir, "config", "--local", key, value); err != nil {
			return err
		}
	}
	return nil
}

// InstallGuards writes the pre-commit/pre-push guard scripts and points the
// vault at them. Re-run at every bootstrap since hooks are not cloned.
func InstallGuards(binPath string) error {
	hd := HooksDir()
	if err := os.MkdirAll(hd, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"pre-commit", "pre-merge-commit", "pre-push"} {
		mode := "tree"
		if name == "pre-push" {
			mode = "push"
		}
		script := "#!/bin/sh\nexec " + shellQuote(binPath) + " guard --mode " + mode + "\n"
		if err := os.WriteFile(filepath.Join(hd, name), []byte(script), 0o755); err != nil {
			return err
		}
	}
	_, err := git(paths.VaultDir(), "config", "core.hooksPath", hd)
	return err
}

// GuardsInstalled reports whether core.hooksPath points at our guard dir and
// every installed script selects the required validation mode.
func GuardsInstalled() bool {
	out, err := git(paths.VaultDir(), "config", "--get", "core.hooksPath")
	if err != nil || strings.TrimSpace(out) != HooksDir() {
		return false
	}
	for name, mode := range map[string]string{
		"pre-commit":       "tree",
		"pre-merge-commit": "tree",
		"pre-push":         "push",
	} {
		path := filepath.Join(HooksDir(), name)
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), "guard --mode "+mode) {
			return false
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			return false
		}
	}
	return true
}

// GuardTree fails if any blob in Git's index, or any corresponding/untracked
// working-tree file, is not a valid memsync envelope under the local key.
//
// Checking the index is essential: pre-commit commits the staged blob, which
// can differ from the working-tree file. Reading only the working tree lets an
// attacker stage plaintext and then restore ciphertext before committing.
func GuardTree(key []byte) error {
	return guardDir(paths.VaultDir(), key)
}

// GuardHistory validates every file in every commit reachable from any local
// ref. It catches unsafe data hidden in older commits even when the current
// checkout is clean.
func GuardHistory(key []byte) error {
	return guardReachableHistory(paths.VaultDir(), key)
}

// GuardPush validates every tree that is about to become reachable on the
// remote. Git supplies one ref update per line on pre-push stdin:
//
//	<local ref> <local object> <remote ref> <remote object>
//
// Looking at all outgoing commits, rather than only HEAD/the current index,
// catches plaintext committed with --no-verify and deleted in a later commit.
func GuardPush(key []byte, updates io.Reader) error {
	dir := paths.VaultDir()
	scanner := bufio.NewScanner(updates)
	seenCommits := make(map[string]struct{})
	seenBlobs := make(map[string]struct{})
	line := 0
	for scanner.Scan() {
		line++
		fields := strings.Fields(scanner.Text())
		if len(fields) != 4 {
			return fmt.Errorf("guard: malformed pre-push update on line %d - refusing", line)
		}
		localRef, localOID := fields[0], fields[1]
		remoteOID := fields[3]
		localZero, err := objectIDIsZero(localOID)
		if err != nil {
			return fmt.Errorf("guard: malformed local object for %s: %w", localRef, err)
		}
		if _, err := objectIDIsZero(remoteOID); err != nil {
			return fmt.Errorf("guard: malformed remote object for %s: %w", localRef, err)
		}
		if localZero {
			continue // deleting a remote ref cannot send a new object
		}

		args := []string{"rev-list", localOID}
		remoteZero, _ := objectIDIsZero(remoteOID)
		if !remoteZero {
			args = append(args, "^"+remoteOID)
		}
		out, err := git(dir, args...)
		if err != nil {
			return fmt.Errorf("guard: cannot enumerate commits for %s: %w", localRef, err)
		}
		for _, commit := range strings.Fields(out) {
			if _, ok := seenCommits[commit]; ok {
				continue
			}
			seenCommits[commit] = struct{}{}
			if err := guardCommitTree(dir, commit, key, seenBlobs); err != nil {
				return fmt.Errorf("guard: outgoing commit %.12s on %s is unsafe: %w", commit, localRef, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("guard: read pre-push updates: %w", err)
	}
	return nil
}

func guardCommitTree(dir, commit string, key []byte, seen map[string]struct{}) error {
	out, err := git(dir, "ls-tree", "-r", "-z", commit)
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		tab := strings.IndexByte(entry, '\t')
		if tab < 0 {
			return fmt.Errorf("malformed tree entry")
		}
		meta, name := strings.Fields(entry[:tab]), entry[tab+1:]
		if len(meta) != 3 {
			return fmt.Errorf("malformed tree entry for %s", name)
		}
		if meta[1] != "blob" {
			return fmt.Errorf("%s has unsupported git object type %s - refusing", name, meta[1])
		}
		if meta[0] != "100644" {
			return fmt.Errorf("%s has unsupported git mode %s - refusing", name, meta[0])
		}
		// The same bytes may be valid only at the managed .gitattributes path,
		// so path is part of the de-duplication key.
		seenKey := meta[2] + "\x00" + name
		if _, ok := seen[seenKey]; ok {
			continue
		}
		seen[seenKey] = struct{}{}
		blob, err := git(dir, "cat-file", "blob", meta[2])
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", name, err)
		}
		if err := validateVaultFile(name, []byte(blob), key); err != nil {
			return err
		}
	}
	return nil
}

func guardReachableHistory(dir string, key []byte) error {
	out, err := git(dir, "rev-list", "--all")
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	for _, commit := range strings.Fields(out) {
		if err := guardCommitTree(dir, commit, key, seen); err != nil {
			return fmt.Errorf("guard: reachable commit %.12s is unsafe: %w", commit, err)
		}
	}
	return nil
}

func objectIDIsZero(s string) (bool, error) {
	if len(s) != 40 && len(s) != 64 {
		return false, fmt.Errorf("object id has length %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return false, fmt.Errorf("invalid object id: %w", err)
	}
	for _, v := range b {
		if v != 0 {
			return false, nil
		}
	}
	return true, nil
}

func guardDir(dir string, key []byte) error {
	if err := guardIndex(dir, key); err != nil {
		return err
	}
	return guardWorkingTree(dir, key)
}

func guardIndex(dir string, key []byte) error {
	out, err := git(dir, "ls-files", "-s", "-z")
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		tab := strings.IndexByte(entry, '\t')
		if tab < 0 {
			return fmt.Errorf("guard: malformed git index entry")
		}
		meta, name := strings.Fields(entry[:tab]), entry[tab+1:]
		if len(meta) != 3 {
			return fmt.Errorf("guard: malformed git index entry for %s", name)
		}
		if meta[2] != "0" {
			return fmt.Errorf("guard: %s has an unresolved merge conflict - refusing", name)
		}
		if meta[0] != "100644" {
			return fmt.Errorf("guard: %s has unsupported git mode %s - refusing", name, meta[0])
		}
		blob, err := git(dir, "cat-file", "blob", meta[1])
		if err != nil {
			return fmt.Errorf("guard: cannot read staged blob %s: %w", name, err)
		}
		if err := validateVaultFile(name, []byte(blob), key); err != nil {
			return err
		}
	}
	return nil
}

func guardWorkingTree(dir string, key []byte) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if entry.IsDir() {
			if name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("guard: %s is a symlink - refusing", name)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("guard: cannot inspect %s - refusing: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("guard: %s is not a regular file - refusing", name)
		}
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			// A concurrently removed unstaged file cannot put bytes into a commit;
			// the index copy was already checked above.
			return nil
		}
		if err != nil {
			return fmt.Errorf("guard: cannot read %s - refusing: %w", name, err)
		}
		return validateVaultFile(name, b, key)
	})
}

func validateVaultFile(name string, b, key []byte) error {
	if name == ".gitattributes" {
		if string(b) != attributesContent {
			return fmt.Errorf("guard: .gitattributes is not the memsync-managed content - refusing")
		}
		return nil
	}
	if !crypto.IsCiphertext(b) {
		return fmt.Errorf("guard: %s is not memsync ciphertext - refusing", name)
	}
	if _, err := crypto.Decrypt(key, b); err != nil {
		return fmt.Errorf("guard: %s does not decrypt under local key - refusing: %w", name, err)
	}
	return nil
}

// CommitAll stages the working tree and commits if there is anything to commit.
// The commit triggers the pre-commit guard, so a plaintext file aborts it.
func CommitAll(msg string) error {
	dir := paths.VaultDir()
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return err
	}
	if err := guardDir(dir, key); err != nil {
		return err
	}
	if _, err := git(dir, "add", "-A", "-f", "--", "."); err != nil {
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

// Push reconciles the current branch with origin and sends the vault. A push
// race is retried after another fetch/rebase. Device-scoped records are
// append-only and live at disjoint paths, so this converges without choosing a
// lossy winner. The whole operation is deadline-bound and noninteractive.
func Push() error {
	if !HasRemote() {
		return nil
	}
	dir := paths.VaultDir()
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()

	var last error
	for attempt := 1; attempt <= pushAttempts; attempt++ {
		if err := reconcile(ctx, dir); err != nil {
			return err
		}
		if _, err := gitRemote(ctx, dir, "push", "-q", "-u", "origin", "HEAD"); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return fmt.Errorf("vault push did not converge after %d attempts: %w", pushAttempts, last)
}

// PushToURL publishes the current HEAD directly to a candidate remote without
// fetching, rebasing, changing origin, or otherwise mutating local history. It
// is used to prove write access before a remote switch is committed locally.
func PushToURL(rawURL string) error {
	dir := paths.VaultDir()
	branch, err := git(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot determine vault branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	_, err = gitRemote(ctx, dir, "push", "-q", "--", rawURL, "HEAD:refs/heads/"+branch)
	return err
}

// RefreshOriginTracking fetches origin and records the current branch's
// upstream without moving HEAD or the working tree.
func RefreshOriginTracking() error {
	dir := paths.VaultDir()
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	if _, err := gitRemote(ctx, dir, "fetch", "-q", "--prune", "origin"); err != nil {
		return err
	}
	if err := validateFetchedHistory(dir); err != nil {
		return err
	}
	branch, err := git(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot determine vault branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	_, err = git(dir, "branch", "--set-upstream-to=origin/"+branch, branch)
	return err
}

// NeedsPush reports whether HEAD contains commits that are not known to be on
// its upstream. A missing upstream means this is a first push. This keeps
// per-turn hooks entirely local when no memory changed while still retrying a
// previously failed push on the next hook invocation.
func NeedsPush() bool {
	if !HasRemote() {
		return false
	}
	dir := paths.VaultDir()
	if _, err := git(dir, "rev-parse", "--verify", "@{upstream}"); err != nil {
		return true
	}
	out, err := git(dir, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != "0"
}

// Pull fetches origin and rebases any local append-only commits on top of it.
// It is deadline-bound and noninteractive so a session-start hook cannot wait
// forever for credentials or a broken network.
func Pull() error {
	if !HasRemote() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	return reconcile(ctx, paths.VaultDir())
}

func reconcile(ctx context.Context, dir string) error {
	if _, err := gitRemote(ctx, dir, "fetch", "-q", "--prune", "origin"); err != nil {
		return err
	}
	if err := validateFetchedHistory(dir); err != nil {
		return err
	}
	branch, err := git(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot determine vault branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	remoteRef := "refs/remotes/origin/" + branch
	if _, err := git(dir, "show-ref", "--verify", "--quiet", remoteRef); err != nil {
		// An empty remote has no branch yet; the first push creates it.
		return nil
	}
	if _, err := gitRemote(ctx, dir, "rebase", "--quiet", remoteRef); err != nil {
		_, _ = git(dir, "rebase", "--abort")
		return fmt.Errorf("cannot reconcile append-only vault with origin; local commits were preserved: %w", err)
	}
	return nil
}

func validateFetchedHistory(dir string) error {
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return fmt.Errorf("load key before validating fetched vault history: %w", err)
	}
	if err := guardReachableHistory(dir, key); err != nil {
		return fmt.Errorf("fetched vault history is unsafe; refusing to move the working tree: %w", err)
	}
	return nil
}

// Records returns the absolute paths of the vault's encrypted record files.
func Records() ([]string, error) {
	var out []string
	dir := paths.VaultDir()
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != dir && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".enc") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// LastCommit returns a short description of the latest vault commit, or "".
func LastCommit() string {
	out, err := git(paths.VaultDir(), "log", "-1", "--pretty=%cr - %s")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// HasEncryptedHistory reports whether any reachable commit names an encrypted
// record, including records deleted from the current checkout.
func HasEncryptedHistory() (bool, error) {
	dir := paths.VaultDir()
	if !isRepo(dir) {
		return false, nil
	}
	out, err := git(dir, "rev-list", "--objects", "--all")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		_, name, ok := strings.Cut(line, " ")
		if ok && strings.HasSuffix(strings.TrimSpace(name), ".enc") {
			return true, nil
		}
	}
	return false, nil
}

// StagedClone is a candidate vault cloned beside the live vault. Call Validate
// with the candidate key before Activate. Discard is safe and idempotent.
type StagedClone struct {
	path         string
	dest         string
	validatedKey []byte
}

// StageClone clones url into a temporary sibling of the live vault. Network or
// authentication failure leaves the live vault untouched.
func StageClone(url string) (*StagedClone, error) {
	dest := paths.VaultDir()
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, ".vault-stage-")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	if _, err := gitRemote(ctx, parent, "clone", "-q", "--no-checkout", "--", url, stage); err != nil {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	if err := checkoutRemoteBranch(stage); err != nil {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	return &StagedClone{path: stage, dest: dest}, nil
}

func checkoutRemoteBranch(dir string) error {
	out, err := git(dir, "for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin")
	if err != nil {
		return err
	}
	var branches []string
	for _, branch := range strings.Fields(out) {
		if branch != "HEAD" {
			branches = append(branches, branch)
		}
	}
	if len(branches) == 0 {
		// Empty remotes are valid candidates; establish memsync's standard unborn
		// branch without relying on the server's symbolic HEAD.
		_, err := git(dir, "symbolic-ref", "HEAD", "refs/heads/main")
		return err
	}
	chosen := ""
	for _, branch := range branches {
		if branch == "main" {
			chosen = branch
			break
		}
	}
	if chosen == "" && len(branches) == 1 {
		chosen = branches[0]
	}
	if chosen == "" {
		return fmt.Errorf("remote has multiple branches and no main branch; refusing to guess which is the vault")
	}
	if _, err := git(dir, "checkout", "-q", "-B", chosen, "refs/remotes/origin/"+chosen); err != nil {
		return err
	}
	return nil
}

// Path returns the temporary clone path for additional read-only checks.
func (s *StagedClone) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Validate checks the staged index and working tree under the key that will be
// adopted by the joining machine. The live vault remains untouched on failure.
func (s *StagedClone) Validate(key []byte) error {
	if s == nil || s.path == "" {
		return fmt.Errorf("staged clone is no longer available")
	}
	s.clearValidation()
	if !isRepo(s.path) {
		return fmt.Errorf("staged clone is not a git repository")
	}
	if err := guardDir(s.path, key); err != nil {
		return fmt.Errorf("staged vault validation failed: %w", err)
	}
	if err := guardReachableHistory(s.path, key); err != nil {
		return fmt.Errorf("staged vault history validation failed: %w", err)
	}
	s.validatedKey = append([]byte(nil), key...)
	return nil
}

// Activate atomically swaps a validated staged clone into the live vault path.
// If the swap fails, the previous vault is restored.
func (s *StagedClone) Activate() error {
	if s == nil || s.path == "" {
		return fmt.Errorf("staged clone is no longer available")
	}
	if len(s.validatedKey) == 0 {
		return fmt.Errorf("staged clone must be validated before activation")
	}
	// Re-check immediately before the swap so a staged worktree/index change
	// after Validate cannot cross the trust boundary.
	if err := guardDir(s.path, s.validatedKey); err != nil {
		s.clearValidation()
		return fmt.Errorf("staged vault changed after validation: %w", err)
	}
	if err := guardReachableHistory(s.path, s.validatedKey); err != nil {
		s.clearValidation()
		return fmt.Errorf("staged vault history changed after validation: %w", err)
	}
	parent := filepath.Dir(s.dest)
	backup, err := os.MkdirTemp(parent, ".vault-backup-")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}

	hadLive := false
	if _, err := os.Lstat(s.dest); err == nil {
		hadLive = true
		if err := os.Rename(s.dest, backup); err != nil {
			return fmt.Errorf("stage vault backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(s.path, s.dest); err != nil {
		if hadLive {
			_ = os.Rename(backup, s.dest)
		}
		return fmt.Errorf("activate staged vault: %w", err)
	}
	s.path = ""
	s.clearValidation()
	if hadLive {
		_ = os.RemoveAll(backup)
	}
	return nil
}

// Discard removes an unactivated staged clone. It is safe to defer immediately
// after StageClone succeeds.
func (s *StagedClone) Discard() error {
	if s == nil || s.path == "" {
		return nil
	}
	err := os.RemoveAll(s.path)
	s.path = ""
	s.clearValidation()
	return err
}

func (s *StagedClone) clearValidation() {
	for i := range s.validatedKey {
		s.validatedKey[i] = 0
	}
	s.validatedKey = nil
}

// Clone is the compatibility wrapper used by older callers. It now clones and
// validates beside the live vault before an atomic swap. New join flows should
// use StageClone directly so they can validate before changing the key.
func Clone(url string) error {
	stage, err := StageClone(url)
	if err != nil {
		return err
	}
	defer stage.Discard()
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return err
	}
	if err := stage.Validate(key); err != nil {
		return err
	}
	return stage.Activate()
}

// RemoteReachable reports whether origin can be listed (auth + network OK).
func RemoteReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	_, err := gitRemote(ctx, paths.VaultDir(), "ls-remote", "origin")
	return err == nil
}

// RemoteURLReachable checks a candidate without changing the configured
// origin. It accepts empty repositories as long as Git can authenticate/list.
func RemoteURLReachable(rawURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteOperationTimeout)
	defer cancel()
	_, err := gitRemote(ctx, paths.VaultDir(), "ls-remote", "--", rawURL)
	return err
}

// HasRemote reports whether an "origin" remote is configured.
func HasRemote() bool {
	out, err := git(paths.VaultDir(), "remote")
	if err != nil {
		return false
	}
	for _, name := range strings.Fields(out) {
		if name == "origin" {
			return true
		}
	}
	return false
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

// RemoveRemote removes origin when present.
func RemoveRemote() error {
	if !HasRemote() {
		return nil
	}
	_, err := git(paths.VaultDir(), "remote", "remove", "origin")
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

// RemoteHasCredentials reports URL userinfo that could contain an HTTP token
// or password. SSH usernames such as ssh://git@host are not secrets and remain
// supported; password-bearing userinfo is rejected for every scheme.
func RemoteHasCredentials(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User == nil {
		return false
	}
	if strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https") {
		return true
	}
	_, hasPassword := u.User.Password()
	return hasPassword
}

// DisplayRemoteURL removes URL userinfo before a remote is printed in status,
// diagnostics, or errors.
func DisplayRemoteURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func isRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func writeAttributes(dir string) error {
	// Treat envelopes as opaque; git's text merge must never run on ciphertext.
	return os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(attributesContent), 0o644)
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = vaultGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// gitRemote runs a network-adjacent git command without prompts under the
// caller's deadline. The credential config covers Git Credential Manager while
// GIT_TERMINAL_PROMPT covers Git's built-in terminal prompt path.
func gitRemote(ctx context.Context, dir string, args ...string) (string, error) {
	gitArgs := append([]string{"-c", "credential.interactive=never"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = nonInteractiveEnv()
	cmd.WaitDelay = remoteCommandWaitDelay
	out, err := cmd.CombinedOutput()
	displayArgs := make([]string, len(args))
	redactOutput := false
	for i, arg := range args {
		displayArgs[i] = DisplayRemoteURL(arg)
		if displayArgs[i] != arg {
			redactOutput = true
		}
		if arg == "origin" {
			if configured := originURLFromConfig(dir); DisplayRemoteURL(configured) != configured {
				redactOutput = true
			}
		}
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("git %s: remote operation timed out after %s: %w", strings.Join(displayArgs, " "), remoteOperationTimeout, ctx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if redactOutput {
			detail = "remote command output hidden because the URL contained credentials"
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(displayArgs, " "), err, detail)
	}
	return string(out), nil
}

func originURLFromConfig(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return ""
	}
	inOrigin := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inOrigin = strings.EqualFold(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "url") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonInteractiveEnv() []string {
	env := vaultGitEnv()
	for _, key := range []string{"GIT_TERMINAL_PROMPT=", "GCM_INTERACTIVE=", "SSH_ASKPASS_REQUIRE="} {
		filtered := env[:0]
		for _, entry := range env {
			if !strings.HasPrefix(entry, key) {
				filtered = append(filtered, entry)
			}
		}
		env = filtered
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"SSH_ASKPASS_REQUIRE=never",
	)
}

func vaultGitEnv() []string {
	blocked := []string{
		"GIT_AUTHOR_NAME=", "GIT_AUTHOR_EMAIL=", "GIT_AUTHOR_DATE=",
		"GIT_COMMITTER_NAME=", "GIT_COMMITTER_EMAIL=", "GIT_COMMITTER_DATE=",
	}
	var env []string
	for _, entry := range os.Environ() {
		skip := false
		for _, prefix := range blocked {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			env = append(env, entry)
		}
	}
	return append(env,
		"GIT_AUTHOR_NAME=memsync",
		"GIT_AUTHOR_EMAIL=memsync@localhost.invalid",
		"GIT_COMMITTER_NAME=memsync",
		"GIT_COMMITTER_EMAIL=memsync@localhost.invalid",
	)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
