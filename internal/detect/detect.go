// Package detect finds which agent tools are installed on this machine.
package detect

import (
	"context"
	"os/exec"
	"strings"
	"sync"
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
	tools := make([]Tool, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tools[0] = claude()
	}()
	go func() {
		defer wg.Done()
		tools[1] = codex()
	}()
	wg.Wait()
	return tools
}

func claude() Tool {
	dir := paths.ClaudeDir()
	return Tool{
		Name:    "Claude Code",
		Home:    dir,
		Present: hasExecutable("claude"),
		Version: version("claude", "--version"),
	}
}

func codex() Tool {
	dir := paths.CodexDir()
	return Tool{
		Name:    "Codex CLI",
		Home:    dir,
		Present: hasExecutable("codex"),
		Version: version("codex", "--version"),
	}
}

func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// version runs `bin --version` with a short timeout; best-effort, never fatal.
func version(bin string, args ...string) string {
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, bin, args...)
	command.WaitDelay = 500 * time.Millisecond
	out, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
