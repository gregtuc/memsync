package cmd

import (
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runDoctor(args []string) int {
	fix := hasFlag(args, "--fix")
	bin, err := selfPath()
	if err != nil {
		return fail(err)
	}

	fmt.Println("\nmemsync doctor")
	fmt.Printf("\n  %-13s %-9s %-6s\n", "Tool", "detected", "hooks")
	for _, t := range detect.All() {
		wired := wiredFor(t.Name)
		if fix && t.Present && !wired {
			if err := wire(t.Name, bin); err == nil {
				wired = true
			}
		}
		fmt.Printf("  %-13s %-9s %-6s\n", t.Name, yn(t.Present), yn(wired))
	}

	if fix {
		_ = vault.Ensure(bin)
	}
	fmt.Println()
	ok("key       %s", present(fileExists(paths.KeyPath())))
	ok("vault     %s", present(fileExists(paths.VaultDir())))
	ok("guards    %s", present(vault.GuardsInstalled()))
	remote := vault.RemoteURL()
	if remote == "" {
		remote = "none (single-machine)"
	}
	ok("remote    %s", remote)

	fmt.Println("\nSelf-test:")
	if err := selfTest(); err != nil {
		return fail(fmt.Errorf("self-test failed: %w", err))
	}
	if !fix {
		fmt.Println("\nRun `memsync doctor --fix` to repair anything above.")
	}
	return 0
}

func wiredFor(name string) bool {
	switch name {
	case "Claude Code":
		w, _ := hooks.ClaudeWired()
		return w
	case "Codex CLI":
		w, _ := hooks.CodexWired()
		return w
	}
	return false
}

func yn(b bool) string {
	if b {
		return "✓"
	}
	return "-"
}

func present(b bool) string {
	if b {
		return "✓"
	}
	return "missing"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
