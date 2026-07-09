package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runInit(args []string) int {
	dry := hasFlag(args, "--dry-run")

	fmt.Printf("\nmemsync %s - memory courier for Claude Code & Codex\n", Version)

	bin, err := selfPath()
	if err != nil {
		return fail(err)
	}

	step("Detecting tools...")
	tools := detect.All()
	anyPresent := false
	for _, t := range tools {
		if t.Present {
			anyPresent = true
			v := ""
			if t.Version != "" {
				v = "  (" + t.Version + ")"
			}
			ok("%-13s %s%s", t.Name, t.Home, v)
		} else {
			warn("%-13s not found (%s)", t.Name, t.Home)
		}
	}
	if !anyPresent {
		return fail(fmt.Errorf("neither Claude Code nor Codex found; install one and re-run"))
	}

	step("Wiring user-scope hooks (idempotent, tagged [memsync])...")
	if dry {
		warn("--dry-run: no files will be written")
	}
	for _, t := range tools {
		if !t.Present {
			continue
		}
		if dry {
			ok("would wire %s", t.Name)
			continue
		}
		if err := wire(t.Name, bin); err != nil {
			return fail(err)
		}
	}
	if !dry {
		ok("hooks call the full path %s (survives GUI/Dock launches)", bin)
	}

	step("Setting up local vault...")
	if dry {
		ok("would create key %s and vault %s", paths.KeyPath(), paths.VaultDir())
	} else {
		_, created, err := crypto.LoadOrCreateKey(paths.KeyPath())
		if err != nil {
			return fail(err)
		}
		if created {
			ok("key      %s   AES-256 · 0600 · never leaves this machine", paths.KeyPath())
		} else {
			ok("key      %s   (existing)", paths.KeyPath())
		}
		if err := vault.Ensure(bin); err != nil {
			return fail(err)
		}
		ok("vault    %s   ciphertext-only · guards armed", paths.VaultDir())
	}

	if dry {
		fmt.Println("\nDry run complete. Re-run without --dry-run to apply.")
		return 0
	}

	step("Verifying it actually works (round-trip self-test)...")
	if err := selfTest(); err != nil {
		return fail(fmt.Errorf("self-test failed: %w", err))
	}

	fmt.Println("\nDone - synced locally. Nothing crossed the network (single-machine mode).")
	fmt.Println("↻ Restart any open Claude Code / Codex sessions to load the new hooks.")
	fmt.Println("\nNext (all optional):")
	fmt.Println("  memsync status     what's synced right now")
	fmt.Println("  memsync pair       add a second machine")
	fmt.Println("  memsync doctor     re-verify anytime")
	return 0
}

func wire(toolName, bin string) error {
	switch toolName {
	case "Claude Code":
		if err := hooks.ClaudeInstall(bin); err != nil {
			return err
		}
		ok("%s   %s", filepath.Base(paths.ClaudeSettings()), "SessionStart · FileChanged · SessionEnd")
	case "Codex CLI":
		if err := hooks.CodexInstall(bin); err != nil {
			return err
		}
		ok("%s      %s", filepath.Base(paths.CodexConfig()), "SessionStart · Stop")
	}
	return nil
}

// selfPath returns the absolute, symlink-resolved path to this binary so hooks
// keep working under minimal-PATH GUI launches.
func selfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Abs(exe)
}
