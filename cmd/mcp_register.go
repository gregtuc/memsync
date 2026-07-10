package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// mcpServerName is how memsync's recall server is registered in each tool.
const mcpServerName = "memsync"

// registerRecall makes memsync's recall MCP server available to the tool. It is
// idempotent: the server is added only when absent, so a re-run never disturbs
// an existing registration or any approval the user granted it.
func registerRecall(toolName, bin string) error {
	switch toolName {
	case "Claude Code":
		if mcpPresent("claude") {
			return nil
		}
		return toolCommand("claude", "mcp", "add", "-s", "user", mcpServerName, "--", bin, "mcp", "--tool", "claude")
	case "Codex CLI":
		if mcpPresent("codex") {
			return nil
		}
		return toolCommand("codex", "mcp", "add", mcpServerName, "--", bin, "mcp", "--tool", "codex")
	}
	return nil
}

// unregisterRecall removes memsync's recall server. Best effort; a missing server
// is not an error.
func unregisterRecall(toolName string) {
	switch toolName {
	case "Claude Code":
		_ = toolCommand("claude", "mcp", "remove", mcpServerName)
	case "Codex CLI":
		_ = toolCommand("codex", "mcp", "remove", mcpServerName)
	}
}

// recallRegistered reports whether memsync's recall server is registered with
// the tool.
func recallRegistered(toolName string) bool {
	switch toolName {
	case "Claude Code":
		return mcpPresent("claude")
	case "Codex CLI":
		return mcpPresent("codex")
	}
	return false
}

// mcpPresent reports whether the tool already has a memsync MCP server. `mcp get`
// exits non-zero when the server is absent.
func mcpPresent(cli string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "mcp", "get", mcpServerName)
	cmd.WaitDelay = 500 * time.Millisecond
	return cmd.Run() == nil
}

func toolCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s %s timed out", name, strings.Join(args, " "))
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
