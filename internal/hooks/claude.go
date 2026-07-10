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

// claudeMarker identifies exactly the commands memsync owns. It is a shell
// comment, so it does not change hook execution.
const claudeMarker = "# memsync-managed"

type claudeEvent struct {
	name    string
	matcher string // "" = no matcher
	command []string
}

func claudeEvents(bin string) []claudeEvent {
	// SessionStart injects the other tool's memory; SessionEnd captures this
	// tool's. FileChanged was dropped: its matcher accepts only literal filenames
	// (no globs/paths), so a glob silently never fires, and the session-boundary
	// model does not need mid-session capture.
	return []claudeEvent{
		{name: "SessionStart", command: []string{bin, "inject", "--tool", "claude"}},
		{name: "SessionEnd", command: []string{bin, "sync", "--tool", "claude"}},
	}
}

// ClaudeWired reports whether both required memsync lifecycle hooks are present.
func ClaudeWired() (bool, error) {
	root, err := readJSON(paths.ClaudeSettings())
	if err != nil {
		return false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	return eventHasMarker(hooks, "SessionStart") && eventHasMarker(hooks, "SessionEnd"), nil
}

// ClaudeWiredFor additionally verifies the exact current binary path.
func ClaudeWiredFor(bin string) (bool, error) {
	root, err := readJSON(paths.ClaudeSettings())
	if err != nil {
		return false, err
	}
	hookMap, _ := root["hooks"].(map[string]any)
	wants := map[string]string{
		"SessionStart": shellCommand(bin, "inject", "--tool", "claude") + " " + claudeMarker,
		"SessionEnd":   shellCommand(bin, "sync", "--tool", "claude") + " " + claudeMarker,
	}
	for event, want := range wants {
		found := false
		for _, entry := range asSlice(hookMap[event]) {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, hook := range asSlice(entryMap["hooks"]) {
				hookMap, _ := hook.(map[string]any)
				if command, _ := hookMap["command"].(string); command == want {
					found = true
				}
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

// ClaudeHooksEnabled reports whether the user settings explicitly disable all
// hooks. Managed policy may impose additional restrictions that are visible
// only inside Claude Code.
func ClaudeHooksEnabled() (bool, error) {
	root, err := readJSON(paths.ClaudeSettings())
	if err != nil {
		return false, err
	}
	disabled, _ := root["disableAllHooks"].(bool)
	return !disabled, nil
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
	// Remove every memsync-owned entry first, including events used by older
	// releases (notably FileChanged), then install the current event set.
	for name, raw := range hooks {
		remaining := stripMarker(asSlice(raw))
		if len(remaining) == 0 {
			delete(hooks, name)
		} else {
			hooks[name] = remaining
		}
	}
	for _, ev := range claudeEvents(bin) {
		arr := asSlice(hooks[ev.name])
		entry := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": shellCommand(ev.command...) + " " + claudeMarker}},
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
		if cmd, _ := hm["command"].(string); isManagedClaudeCommand(cmd) {
			return true
		}
	}
	return false
}

func isManagedClaudeCommand(cmd string) bool {
	if strings.Contains(cmd, claudeMarker) {
		return true
	}
	// Migrate the precise quoted and unquoted argv tails emitted by releases
	// that predate the explicit marker. Merely containing the word "memsync" is
	// not ownership; the executable itself must have been named memsync.
	for _, tail := range []string{
		" 'inject' '--tool' 'claude'",
		" 'sync' '--tool' 'claude'",
		" inject --tool claude",
		" sync --tool claude",
	} {
		if !strings.HasSuffix(strings.TrimSpace(cmd), strings.TrimSpace(tail)) {
			continue
		}
		executable := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cmd), strings.TrimSpace(tail)))
		executable = strings.Trim(executable, "'\"")
		if filepath.Base(executable) == "memsync" {
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
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o644)
}

func backup(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeFileAtomic(path+".memsync.bak", b, 0o600)
}
