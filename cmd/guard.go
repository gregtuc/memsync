package cmd

import (
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

// runGuard is invoked by the vault's git hooks. Commit/merge mode validates the
// index and working tree; push mode consumes Git's ref updates on stdin and
// validates every outgoing commit tree. It always fails closed.
func runGuard(args []string) int {
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "memsync guard: cannot load key, refusing: %v\n", err)
		return 1
	}
	var guardErr error
	switch mode := flagValue(args, "--mode"); mode {
	case "", "tree": // empty keeps older installed hooks fail-closed and compatible
		guardErr = vault.GuardTree(key)
	case "push":
		guardErr = vault.GuardPush(key, os.Stdin)
	default:
		guardErr = fmt.Errorf("unknown guard mode %q", mode)
	}
	if guardErr != nil {
		fmt.Fprintf(os.Stderr, "memsync guard: %v\n", guardErr)
		return 1
	}
	return 0
}
