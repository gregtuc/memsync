package cmd

import (
	"fmt"

	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runStatus(args []string) int {
	fmt.Println("\nmemsync status")
	fmt.Println()
	for _, t := range detect.All() {
		if t.Present {
			ok("%-13s detected, hooks %s", t.Name, tick(wiredFor(t.Name)))
		} else {
			warn("%-13s not installed", t.Name)
		}
	}

	remote := vault.RemoteURL()
	mode := "single-machine (no remote)"
	if remote != "" {
		mode = remote
	}
	fmt.Println()
	ok("mode      %s", mode)
	ok("guards    %s", present(vault.GuardsInstalled()))
	if last := vault.LastCommit(); last != "" {
		ok("last sync %s", last)
	}
	if remote != "" && fileExists(paths.KeyPath()) {
		fmt.Println("\n  reminder: back up your key - key loss is unrecoverable by design.")
		fmt.Println("            " + paths.KeyPath())
	}
	return 0
}

func tick(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
