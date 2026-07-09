// Package paths resolves the well-known locations memsync reads and writes.
package paths

import (
	"os"
	"path/filepath"
)

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// ConfigDir is memsync's own config/state root (XDG-aware). Holds the key.
func ConfigDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "memsync")
	}
	return filepath.Join(home(), ".config", "memsync")
}

// DataDir is memsync's data root (XDG-aware). Holds the vault.
func DataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "memsync")
	}
	return filepath.Join(home(), ".local", "share", "memsync")
}

// KeyPath is the AES key file. Never leaves the machine.
func KeyPath() string { return filepath.Join(ConfigDir(), "key") }

// VaultDir is the ciphertext-only git repo that crosses machines.
func VaultDir() string { return filepath.Join(DataDir(), "vault") }

// MirrorDir is the local plaintext working copy (never committed anywhere).
func MirrorDir() string { return filepath.Join(DataDir(), "mirror") }

// ClaudeDir is Claude Code's home.
func ClaudeDir() string { return filepath.Join(home(), ".claude") }

// ClaudeSettings is the user-scope settings file memsync wires hooks into.
func ClaudeSettings() string { return filepath.Join(ClaudeDir(), "settings.json") }

// CodexDir is Codex CLI's home (honoring CODEX_HOME).
func CodexDir() string {
	if c := os.Getenv("CODEX_HOME"); c != "" {
		return c
	}
	return filepath.Join(home(), ".codex")
}

// CodexConfig is the user-scope Codex config file memsync wires hooks into.
func CodexConfig() string { return filepath.Join(CodexDir(), "config.toml") }

// ClaudeMemoryGlob matches Claude's per-project auto-memory files, used as the
// FileChanged watch matcher.
func ClaudeMemoryGlob() string {
	return filepath.Join(ClaudeDir(), "projects", "**", "memory", "**")
}

// CodexMemories is Codex's own memory store (read-only to memsync).
func CodexMemories() string { return filepath.Join(CodexDir(), "memories") }
