package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

// injectMaxBytes caps the injected block so it never crowds out the tool's own memory.
const injectMaxBytes = 4000

// runInject is hook-invoked at SessionStart. It refreshes the vault, then emits
// every memory NOT written by the receiving tool (i.e. the other tool's, from
// any machine) as read-only context. Always exits 0 - never breaks a session.
func runInject(args []string) int {
	tool := flagValue(args, "--tool")
	if tool != "claude" && tool != "codex" {
		return 0
	}
	_ = vault.Pull() // best-effort refresh from other machines

	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		return 0
	}
	files, err := vault.Records()
	if err != nil {
		return 0
	}

	var mems []courier.Memory
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		plain, err := crypto.Decrypt(key, b)
		if err != nil {
			continue
		}
		var r record
		if err := json.Unmarshal(plain, &r); err != nil {
			continue
		}
		if r.Origin == tool {
			continue // don't echo the receiving tool's own memories back to it
		}
		mems = append(mems, courier.Memory{Origin: r.Origin, Scope: r.Scope, Title: r.Title, Body: r.Body})
	}

	label := "the other tool"
	if tool == "claude" {
		label = "Codex (background summaries)"
	} else {
		label = "Claude Code"
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
