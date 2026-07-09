package cmd

import "fmt"

// runPair / runJoin implement the public-key sealed pairing flow, where nothing
// copied between machines is a secret. Not yet implemented in this scaffold.
func runPair(args []string) int {
	fmt.Println(`memsync pair — add a second machine (not yet implemented)

Planned flow (nothing you copy is a secret):
  1. On the NEW machine:  memsync join   → prints a public invite code
  2. Here:                memsync pair    → paste the invite; memsync seals the
     vault key TO that machine's public key and prints a sealed reply
  3. On the NEW machine:  paste the reply → only it can unseal the key

Requires a remote first:  memsync remote create`)
	return 0
}

func runJoin(args []string) int {
	fmt.Println(`memsync join — join an existing vault (not yet implemented)

Planned flow:
  - generates a throwaway keypair, prints its PUBLIC half as an invite code
  - after you paste the sealed reply from ` + "`memsync pair`" + `, it unseals the key,
    wires hooks, installs guards, pulls the vault, and runs the self-test`)
	return 0
}
