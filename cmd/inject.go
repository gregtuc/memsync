package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/courier"
)

// injectMaxBytes caps the injected block so it never crowds out the tool's own memory.
const injectMaxBytes = 4000

// runInject is hook-invoked at SessionStart. It emits the OTHER tool's memories
// as read-only context. It always exits 0 — a sync tool must never break a session.
func runInject(args []string) int {
	tool := flagValue(args, "--tool")
	var (
		mems  []courier.Memory
		label string
	)
	switch tool {
	case "claude": // receiving = claude, so show codex's memories
		mems, _ = courier.CollectCodex()
		label = "Codex (background summaries)"
	case "codex":
		mems, _ = courier.CollectClaude()
		label = "Claude Code"
	default:
		return 0
	}

	ctx := courier.RenderContext(label, mems, injectMaxBytes)
	if ctx == "" {
		return 0
	}
	// NOTE: hook output schema shared by both tools; verify per version.
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": ctx,
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return 0
	}
	fmt.Fprintln(os.Stdout, string(b))
	return 0
}
