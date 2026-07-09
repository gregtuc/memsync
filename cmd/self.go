package cmd

import "fmt"

func runSelf(args []string) int {
	if len(args) > 0 && args[0] == "update" {
		fmt.Println(`memsync self update — not yet implemented

Planned: re-run the installer, verify the GitHub attestation before executing
anything, then rewrite the absolute binary path in every hook. Never runs on the
session hook path.`)
		return 0
	}
	fmt.Println("usage: memsync self update")
	return 2
}
