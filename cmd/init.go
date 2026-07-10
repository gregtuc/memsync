package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runInit(args []string) int {
	dry := hasFlag(args, "--dry-run")
	if hasFlag(args, "--enable-codex-memories") && hasFlag(args, "--no-codex-memories") {
		return fail(fmt.Errorf("choose only one of --enable-codex-memories or --no-codex-memories"))
	}

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
	if !dry {
		if err := validateSetupKeyState(); err != nil {
			return fail(err)
		}
	}

	codexMemories := detect.FeatureUnknown
	for _, t := range tools {
		if t.Name != "Codex CLI" || !t.Present {
			continue
		}
		features := detect.DetectCodexFeatures()
		if features.CommandError != nil {
			return fail(fmt.Errorf("Codex could not load its configuration/features; fix the reported config error or update Codex before wiring hooks: %w", features.CommandError))
		}
		codexMemories = features.Memories
		if features.Hooks == detect.FeatureDisabled {
			step("Preparing Codex...")
			if dry {
				ok("would enable Codex lifecycle hooks")
			} else if err := setCodexFeature("hooks", true); err != nil {
				return fail(err)
			} else {
				ok("enabled Codex lifecycle hooks")
			}
		}
		if features.Memories != detect.FeatureEnabled {
			enable := hasFlag(args, "--enable-codex-memories")
			if !enable && !hasFlag(args, "--no-codex-memories") && stdinIsTerminal() && !dry {
				fmt.Println("\nCodex Memories is off by default. It must be enabled for Codex → Claude sync.")
				fmt.Println("Codex generates it in the background and may use a small amount of your Codex quota.")
				fmt.Print("Enable Codex Memories now? [y/N]: ")
				answer := strings.ToLower(readLine())
				enable = answer == "y" || answer == "yes"
			}
			if enable {
				if dry {
					ok("would enable Codex Memories")
				} else if err := setCodexFeature("memories", true); err != nil {
					return fail(err)
				} else {
					codexMemories = detect.FeatureEnabled
					ok("enabled Codex Memories (background generation may take a few sessions)")
				}
			}
		}
	}

	step("Wiring user-scope hooks (idempotent, marked memsync-managed)...")
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
		if hasPresentTool(tools, "Claude Code") {
			if enabled, err := hooks.ClaudeHooksEnabled(); err == nil && !enabled {
				warn("Claude Code has disableAllHooks=true; remove or disable that setting before memsync hooks can run")
			}
		}
	}

	step("Setting up local vault...")
	if dry {
		ok("would create key %s and vault %s", paths.KeyPath(), paths.VaultDir())
	} else {
		_, created, err := loadOrCreateSetupKey()
		if err != nil {
			return fail(err)
		}
		if created {
			ok("key      %s   AES-256 · 0600 · never stored in Git or the remote", paths.KeyPath())
		} else {
			ok("key      %s   (existing)", paths.KeyPath())
		}
		if err := vault.Ensure(bin); err != nil {
			return fail(err)
		}
		ok("vault    %s   ciphertext-only · guards armed", paths.VaultDir())
		dev, created, err := device.LoadOrCreate(paths.DeviceIDPath())
		if err != nil {
			return fail(err)
		}
		if created {
			ok("device   %s   %s", dev.Name, dev.ID[:8])
		} else {
			ok("device   %s   (existing)", dev.Name)
		}
	}

	if dry {
		fmt.Println("\nDry run complete. Re-run without --dry-run to apply.")
		return 0
	}

	step("Checking the encrypted local pipeline...")
	if err := selfTest(); err != nil {
		return fail(fmt.Errorf("self-test failed: %w", err))
	}

	step("Capturing existing memories (so the next session already has context)...")
	for _, installed := range tools {
		if !installed.Present {
			continue
		}
		source := struct{ tool, label string }{tool: "claude", label: "Claude Code"}
		if installed.Name == "Codex CLI" {
			source = struct{ tool, label string }{tool: "codex", label: "Codex"}
		}
		result, err := syncTool(source.tool)
		if err != nil {
			warn("%-12s %v", source.label, err)
			continue
		}
		ok("%-12s %d readable memories", source.label, result.Found)
	}

	if vault.HasRemote() {
		fmt.Println("\nSetup is complete. The configured remote was synchronized where reachable.")
	} else {
		fmt.Println("\nLocal setup is complete. Nothing crossed the network (single-machine mode).")
	}
	fmt.Println("↻ Restart any open Claude Code / Codex sessions.")
	if hasPresentTool(tools, "Codex CLI") {
		fmt.Println("\nOne Codex security step remains:")
		fmt.Println("  Open Codex and choose “Review hooks” → trust the two memsync hooks.")
		fmt.Println("  You can review them again anytime with /hooks.")
	}
	if codexMemories != detect.FeatureEnabled && hasPresentTool(tools, "Codex CLI") {
		fmt.Println("\nCodex → Claude is waiting because Codex Memories is off.")
		fmt.Println("  Enable it later with: memsync init --enable-codex-memories")
	}
	fmt.Println("\nNext (all optional):")
	fmt.Println("  memsync status     see what is ready and what still needs attention")
	fmt.Println("  memsync sync       capture everything now")
	fmt.Println("  memsync doctor     verify or repair setup")
	fmt.Println("  memsync remote create   prepare to add another machine")
	return 0
}

func setCodexFeature(name string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	out, err := exec.Command("codex", "features", action, name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not %s Codex feature %q: %w: %s", action, name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func hasPresentTool(tools []detect.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name && t.Present {
			return true
		}
	}
	return false
}

func wire(toolName, bin string) error {
	switch toolName {
	case "Claude Code":
		if err := hooks.ClaudeInstall(bin); err != nil {
			return err
		}
		ok("%s   hooks: SessionStart, SessionEnd", filepath.Base(paths.ClaudeSettings()))
	case "Codex CLI":
		if err := hooks.CodexInstall(bin); err != nil {
			return err
		}
		ok("%s      hooks: SessionStart, Stop", filepath.Base(paths.CodexConfig()))
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
