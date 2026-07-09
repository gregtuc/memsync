// Package detect finds which agent tools are installed on this machine.
package detect

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gregtuc/memsync/internal/paths"
)

// Tool describes an installed (or absent) agent CLI.
type Tool struct {
	Name    string
	Home    string
	Present bool
	Version string
}

// All returns the tools memsync knows how to bridge.
func All() []Tool {
	return []Tool{claude(), codex()}
}

func claude() Tool {
	dir := paths.ClaudeDir()
	return Tool{
		Name:    "Claude Code",
		Home:    dir,
		Present: isDir(dir),
		Version: version("claude", "--version"),
	}
}

func codex() Tool {
	dir := paths.CodexDir()
	return Tool{
		Name:    "Codex CLI",
		Home:    dir,
		Present: isDir(dir),
		Version: version("codex", "--version"),
	}
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// version runs `bin --version` with a short timeout; best-effort, never fatal.
func version(bin string, args ...string) string {
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
