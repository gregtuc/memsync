package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runRemote(args []string) int {
	if len(args) == 0 {
		fmt.Println("usage: memsync remote create | set <url>")
		return 2
	}
	switch args[0] {
	case "set":
		if len(args) < 2 {
			fmt.Println("usage: memsync remote set <url>")
			return 2
		}
		if !fileExists(paths.VaultDir()) {
			return fail(fmt.Errorf("memsync is not initialized; run `memsync init` first"))
		}
		if err := activateRemote(args[1]); err != nil {
			return fail(err)
		}
		ok("origin set to %s", vault.DisplayRemoteURL(args[1]))
		ok("vault pushed")
		return 0
	case "create":
		return remoteCreate()
	default:
		fmt.Println("usage: memsync remote create | set <url>")
		return 2
	}
}

func remoteCreate() int {
	if !fileExists(paths.VaultDir()) {
		return fail(fmt.Errorf("memsync is not initialized; run `memsync init` first"))
	}
	if existing := vault.RemoteURL(); existing != "" {
		if !vault.RemoteReachable() {
			return fail(fmt.Errorf("the configured remote is unreachable: %s; fix its Git authentication/network access, or replace it explicitly with `memsync remote set <url>`", vault.DisplayRemoteURL(existing)))
		}
		ok("private sync remote is already ready: %s", vault.DisplayRemoteURL(existing))
		fmt.Println("\nOn the new machine, install memsync, run `memsync init`, then run `memsync join`.")
		return 0
	}
	if _, err := exec.LookPath("gh"); err != nil {
		ghGuidance()
		return 1
	}
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		ghGuidance()
		return 1
	}
	login, err := exec.Command("gh", "api", "user", "-q", ".login").Output()
	if err != nil {
		return fail(err)
	}
	owner := strings.TrimSpace(string(login))
	name := "memsync-vault"
	repo := owner + "/" + name
	info, exists, err := githubRepoInfo(repo)
	if err != nil {
		return fail(err)
	}
	if exists {
		if !info.Private {
			return fail(fmt.Errorf("%s already exists but is public; memsync refuses to use a public vault", repo))
		}
		if !info.Empty {
			return fail(fmt.Errorf("%s already exists and is not empty; refusing to guess whether it belongs to this key. Recover it with pairing/join, choose it explicitly with `memsync remote set <url>` after verification, or rename/delete it", repo))
		}
		ok("reusing existing private repository %s", repo)
	} else if out, err := exec.Command("gh", "repo", "create", repo, "--private").CombinedOutput(); err != nil {
		return fail(fmt.Errorf("gh repo create failed: %s", strings.TrimSpace(string(out))))
	} else {
		info, _, err = githubRepoInfo(repo)
		if err != nil {
			return fail(err)
		}
	}
	url := info.URL
	if url == "" {
		return fail(fmt.Errorf("GitHub did not return a clone URL for %s", repo))
	}
	if err := activateRemote(url); err != nil {
		return fail(err)
	}
	ok("private repo set as origin: %s", vault.DisplayRemoteURL(url))
	ok("vault pushed")
	fmt.Println("\nOn the new machine:")
	fmt.Println("  1. Sign in to Git there (`gh auth login` and `gh auth setup-git`).")
	fmt.Println("  2. Install memsync, run `memsync init`, then run `memsync join`; leave it open.")
	fmt.Println("  3. Back here, run `memsync pair` with the invite it prints.")
	return 0
}

func activateRemote(candidate string) error {
	if vault.RemoteHasCredentials(candidate) {
		return fmt.Errorf("remote URLs must not contain HTTP credentials; use a Git credential helper, SSH, or `gh auth setup-git`")
	}
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return err
	}
	stage, err := vault.StageClone(candidate)
	if err != nil {
		return fmt.Errorf("candidate remote is not reachable without prompting (%s): %w", vault.DisplayRemoteURL(candidate), err)
	}
	defer stage.Discard()
	if err := stage.Validate(key); err != nil {
		return fmt.Errorf("candidate remote is not a compatible ciphertext-only vault: %w", err)
	}
	previous := vault.RemoteURL()
	return vault.WithOperationLock(func() error {
		// Publish first by URL. This proves write access and fast-forward
		// compatibility without fetching/rebasing or changing the live origin.
		if err := vault.PushToURL(candidate); err != nil {
			return fmt.Errorf("candidate remote was not activated (%s): %w", vault.DisplayRemoteURL(candidate), err)
		}
		if err := vault.SetRemote(candidate); err != nil {
			return err
		}
		if err := vault.RefreshOriginTracking(); err != nil {
			var rollbackErr error
			if previous == "" {
				rollbackErr = vault.RemoveRemote()
			} else {
				rollbackErr = vault.SetRemote(previous)
			}
			if rollbackErr != nil {
				return fmt.Errorf("candidate was published but tracking setup failed, and restoring the previous origin also failed: %v (rollback: %w)", err, rollbackErr)
			}
			return fmt.Errorf("candidate was published but tracking setup failed; the previous origin was preserved: %w", err)
		}
		return nil
	})
}

type repoInfo struct {
	Private bool   `json:"isPrivate"`
	Empty   bool   `json:"isEmpty"`
	URL     string `json:"url"`
}

func githubRepoInfo(repo string) (repoInfo, bool, error) {
	out, err := exec.Command("gh", "repo", "view", repo, "--json", "isPrivate,isEmpty,url").CombinedOutput()
	if err != nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, "not found") || strings.Contains(text, "could not resolve") {
			return repoInfo{}, false, nil
		}
		return repoInfo{}, false, fmt.Errorf("cannot inspect %s: %s", repo, strings.TrimSpace(string(out)))
	}
	var info repoInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return repoInfo{}, false, err
	}
	return info, true, nil
}

func ghGuidance() {
	fmt.Println("gh not ready. Two options:")
	fmt.Println("  1) run: gh auth login    then re-run `memsync remote create`")
	fmt.Println("  2) bring your own repo:  memsync remote set git@github.com:you/memsync-vault.git")
}
