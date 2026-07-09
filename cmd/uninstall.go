package cmd

import (
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runUninstall(args []string) int {
	purge := hasFlag(args, "--purge")

	step("Removing memsync hooks (only memsync's own entries)...")
	if changed, err := hooks.ClaudeUninstall(); err != nil {
		return fail(err)
	} else if changed {
		ok("removed from %s", paths.ClaudeSettings())
	} else {
		ok("nothing in %s", paths.ClaudeSettings())
	}
	if changed, err := hooks.CodexUninstall(); err != nil {
		return fail(err)
	} else if changed {
		ok("removed from %s", paths.CodexConfig())
	} else {
		ok("nothing in %s", paths.CodexConfig())
	}

	if !purge {
		fmt.Println("\nLeft in place (re-init with no data loss):")
		fmt.Println("  key   " + paths.KeyPath())
		fmt.Println("  vault " + paths.VaultDir())
		fmt.Println("\nRun `memsync uninstall --purge` to remove these too.")
		return 0
	}

	step("Purging local key + vault...")
	for _, p := range []string{paths.KeyPath(), paths.VaultDir(), paths.MirrorDir(), vault.HooksDir()} {
		if err := os.RemoveAll(p); err == nil {
			ok("removed %s", p)
		}
	}
	if remote := vault.RemoteURL(); remote != "" {
		warn("the remote repo still exists on GitHub — delete it yourself: %s", remote)
	}
	fmt.Println("\nNote: this does not remove the installed binary or any PATH line the installer added.")
	return 0
}
