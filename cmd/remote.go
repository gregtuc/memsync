package cmd

import (
	"fmt"
	"os/exec"
	"strings"

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
		if err := vault.SetRemote(args[1]); err != nil {
			return fail(err)
		}
		ok("origin set to %s", args[1])
		if err := vault.Push(); err != nil {
			return fail(err)
		}
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
	name := "memsync-vault"
	if out, err := exec.Command("gh", "repo", "create", name, "--private").CombinedOutput(); err != nil {
		return fail(fmt.Errorf("gh repo create failed: %s", strings.TrimSpace(string(out))))
	}
	url := fmt.Sprintf("https://github.com/%s/%s.git", strings.TrimSpace(string(login)), name)
	if err := vault.SetRemote(url); err != nil {
		return fail(err)
	}
	ok("created private repo and set origin: %s", url)
	if err := vault.Push(); err != nil {
		return fail(err)
	}
	ok("vault pushed — run `memsync pair` to add another machine")
	return 0
}

func ghGuidance() {
	fmt.Println("gh not ready. Two options:")
	fmt.Println("  1) run: gh auth login    then re-run `memsync remote create`")
	fmt.Println("  2) bring your own repo:  memsync remote set git@github.com:you/memsync-vault.git")
}
