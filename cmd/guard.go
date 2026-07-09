package cmd

import (
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

// runGuard is invoked by the vault's git hooks (pre-commit / pre-merge-commit /
// pre-push). It FAILS CLOSED: if the key is missing or any file isn't valid
// ciphertext, it aborts the git operation so plaintext can never be committed
// or pushed.
func runGuard(args []string) int {
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "memsync guard: cannot load key, refusing: %v\n", err)
		return 1
	}
	if err := vault.GuardTree(key); err != nil {
		fmt.Fprintf(os.Stderr, "memsync guard: %v\n", err)
		return 1
	}
	return 0
}
