// Package hooks wires memsync into each tool's user-scope config, idempotently
// and reversibly. It always backs up before writing and never touches config it
// did not add (memsync entries are identified by the marker below).
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregtuc/memsync/internal/paths"
)

// marker identifies memsync-owned entries so uninstall removes exactly its own.
const marker = "memsync"

type claudeEvent struct {
	name    string
	matcher string // "" = no matcher
	command []string
}

func claudeEvents(bin string) []claudeEvent {
	return []claudeEvent{
		{name: "SessionStart", command: []string{bin, "inject", "--tool", "claude"}},
		{name: "FileChanged", matcher: paths.ClaudeMemoryGlob(), command: []string{bin, "sync", "--tool", "claude"}},
		{name: "SessionEnd", command: []string{bin, "sync", "--tool", "claude"}},
	}
}

// ClaudeWired reports whether memsync's SessionStart hook is present.
func ClaudeWired() (bool, error) {
	root, err := readJSON(paths.ClaudeSettings())
	if err != nil {
		return false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	return eventHasMarker(hooks, "SessionStart"), nil
}

// ClaudeInstall adds memsync's hooks to ~/.claude/settings.json. Idempotent.
func ClaudeInstall(bin string) error {
	path := paths.ClaudeSettings()
	root, err := readJSON(path)
	if err != nil {
		return err
	}
	if err := backup(path); err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range claudeEvents(bin) {
		arr := stripMarker(asSlice(hooks[ev.name]))
		entry := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": strings.Join(ev.command, " ")}},
		}
		if ev.matcher != "" {
			entry["matcher"] = ev.matcher
		}
		hooks[ev.name] = append(arr, entry)
	}
	root["hooks"] = hooks
	return writeJSON(path, root)
}

// ClaudeUninstall removes only memsync's entries. Returns whether anything changed.
func ClaudeUninstall() (bool, error) {
	path := paths.ClaudeSettings()
	root, err := readJSON(path)
	if err != nil || root == nil {
		return false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return false, nil
	}
	changed := false
	for name := range hooks {
		before := asSlice(hooks[name])
		after := stripMarker(before)
		if len(after) != len(before) {
			changed = true
		}
		if len(after) == 0 {
			delete(hooks, name)
		} else {
			hooks[name] = after
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
	if !changed {
		return false, nil
	}
	if err := backup(path); err != nil {
		return false, err
	}
	return true, writeJSON(path, root)
}

func eventHasMarker(hooks map[string]any, name string) bool {
	for _, e := range asSlice(hooks[name]) {
		if entryHasMarker(e) {
			return true
		}
	}
	return false
}

func stripMarker(arr []any) []any {
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		if !entryHasMarker(e) {
			out = append(out, e)
		}
	}
	return out
}

func entryHasMarker(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	for _, h := range asSlice(m["hooks"]) {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
			return true
		}
	}
	return false
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func readJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (refusing to touch it): %w", path, err)
	}
	return root, nil
}

func writeJSON(path string, root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func backup(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path+".memsync.bak", b, 0o600)
}
