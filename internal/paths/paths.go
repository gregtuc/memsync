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

// DeviceIDPath is this installation's non-secret, per-machine identifier. It
// is deliberately not shared during pairing so same-tool memories from another
// machine can be distinguished from local echoes.
func DeviceIDPath() string { return filepath.Join(ConfigDir(), "device-id") }

// JoinStatePath holds the private, local-only state for an interrupted laptop
// pairing so authentication or network failures do not require starting over.
func JoinStatePath() string { return filepath.Join(ConfigDir(), "join-state.json") }

// CodexMemoryStatePath records whether memsync enabled Codex's memory source,
// so later upgrades preserve an explicit disable and uninstall can restore it.
func CodexMemoryStatePath() string { return filepath.Join(ConfigDir(), "codex-memory-state") }

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

// CodexMemories is Codex's own memory store (read-only to memsync).
func CodexMemories() string { return filepath.Join(CodexDir(), "memories") }
