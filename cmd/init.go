package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

	fmt.Printf("\nSetting up memsync %s...\n", Version)

	bin, err := selfPath()
	if err != nil {
		return fail(err)
	}

	tools := detect.All()
	ready := make(map[string]bool)
	issues := make(map[string]error)
	var found []string
	for _, t := range tools {
		if t.Present {
			ready[t.Name] = true
			found = append(found, friendlyToolName(t.Name))
		}
	}
	if len(found) == 0 {
		return fail(fmt.Errorf("neither Claude Code nor Codex found; install one and re-run"))
	}
	if !dry {
		if err := validateSetupKeyState(); err != nil {
			return fail(err)
		}
	}

	for _, t := range tools {
		if t.Name != "Codex CLI" || !t.Present {
			continue
		}
		if !dry {
			if err := os.MkdirAll(t.Home, 0o700); err != nil {
				ready[t.Name] = false
				issues[t.Name] = err
				continue
			}
		}
		features := detect.DetectCodexFeatures()
		if features.CommandError != nil {
			ready[t.Name] = false
			issues[t.Name] = features.CommandError
			continue
		}
		if features.Hooks == detect.FeatureDisabled {
			if !dry {
				if err := setCodexFeature("hooks", true); err != nil {
					ready[t.Name] = false
					issues[t.Name] = err
					continue
				}
			}
		}
		if !dry {
			if err := configureCodexMemories(features.Memories, args); err != nil {
				ready[t.Name] = false
				issues[t.Name] = err
				continue
			}
		}
	}

	if dry {
		ok("Found %s", strings.Join(found, " and "))
		ok("Would configure encrypted memory sync")
		fmt.Println("\nNo changes made.")
		return 0
	}

	if _, _, err := loadOrCreateSetupKey(); err != nil {
		return fail(err)
	}
	if _, _, err := device.LoadOrCreate(paths.DeviceIDPath()); err != nil {
		return fail(err)
	}
	if err := vault.Ensure(bin); err != nil {
		return fail(err)
	}
	for _, t := range tools {
		if ready[t.Name] {
			if err := wire(t.Name, bin); err != nil {
				ready[t.Name] = false
				issues[t.Name] = err
			}
		}
	}
	if ready["Claude Code"] {
		if enabled, err := hooks.ClaudeHooksEnabled(); err != nil {
			ready["Claude Code"] = false
			issues["Claude Code"] = err
		} else if !enabled {
			ready["Claude Code"] = false
			issues["Claude Code"] = fmt.Errorf("hooks are disabled")
		}
	}
	if err := selfTest(); err != nil {
		return fail(fmt.Errorf("self-test failed: %w", err))
	}

	totalFound := 0
	for _, installed := range tools {
		if !ready[installed.Name] {
			continue
		}
		tool := "claude"
		if installed.Name == "Codex CLI" {
			tool = "codex"
		}
		result, err := syncTool(tool)
		if err != nil {
			ready[installed.Name] = false
			issues[installed.Name] = err
			continue
		}
		totalFound += result.Found
	}

	var connected []string
	for _, tool := range tools {
		if ready[tool.Name] {
			connected = append(connected, friendlyToolName(tool.Name))
		}
	}
	if len(connected) == 0 {
		for _, tool := range tools {
			if err := issues[tool.Name]; err != nil {
				return fail(fmt.Errorf("could not connect %s: %w", friendlyToolName(tool.Name), err))
			}
		}
		return fail(fmt.Errorf("could not connect an installed tool; run `memsync doctor`"))
	}
	ok("Set up %s", strings.Join(connected, " and "))
	ok("Encrypted memory sync is configured")
	if totalFound > 0 {
		ok("Captured %d existing memories", totalFound)
	}
	for _, tool := range tools {
		if tool.Present && !ready[tool.Name] {
			warn("%s needs attention; run `memsync doctor`", friendlyToolName(tool.Name))
		}
	}
	fmt.Printf("\nDone. Restart %s.\n", strings.Join(connected, " and "))
	if ready["Codex CLI"] {
		fmt.Println("When Codex asks, approve memsync to finish.")
	}
	return 0
}

func friendlyToolName(name string) string {
	if name == "Codex CLI" {
		return "Codex"
	}
	return name
}

func setCodexFeature(name string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "codex", "features", action, name)
	command.WaitDelay = 500 * time.Millisecond
	out, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Codex took too long while preparing %s", name)
		}
		return fmt.Errorf("could not %s Codex feature %q: %w: %s", action, name, err, strings.TrimSpace(string(out)))
	}
	return nil
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
		return hooks.ClaudeInstall(bin)
	case "Codex CLI":
		return hooks.CodexInstall(bin)
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
