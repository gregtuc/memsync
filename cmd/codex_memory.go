package cmd

import (
	"errors"
	"os"
	"strings"

	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/paths"
)

const (
	codexMemoryManaged     = "managed"
	codexMemoryPreexisting = "preexisting"
	codexMemoryOptedOut    = "opted-out"
)

// configureCodexMemories makes first-time setup automatic while preserving a
// later explicit disable. The tiny ownership marker also lets uninstall undo a
// feature that memsync itself enabled without touching a preexisting setting.
func configureCodexMemories(current detect.FeatureState, args []string) error {
	state := loadCodexMemoryState()
	if hasFlag(args, "--no-codex-memories") {
		if current == detect.FeatureEnabled {
			if err := setCodexFeature("memories", false); err != nil {
				return err
			}
		}
		return saveCodexMemoryState(codexMemoryOptedOut)
	}
	force := hasFlag(args, "--enable-codex-memories")
	if state == codexMemoryOptedOut && !force {
		return nil
	}
	if !force && state != "" && current != detect.FeatureEnabled {
		// memsync previously completed setup, so a later disabled value is a
		// user preference rather than the first-run default.
		return saveCodexMemoryState(codexMemoryOptedOut)
	}
	if current == detect.FeatureEnabled {
		if state == "" {
			return saveCodexMemoryState(codexMemoryPreexisting)
		}
		if force && state == codexMemoryOptedOut {
			return saveCodexMemoryState(codexMemoryPreexisting)
		}
		return nil
	}
	if err := setCodexFeature("memories", true); err != nil {
		return err
	}
	return saveCodexMemoryState(codexMemoryManaged)
}

func restoreCodexMemoryPreference() error {
	state := loadCodexMemoryState()
	if state == "" {
		return nil
	}
	if state == codexMemoryManaged {
		features := detect.DetectCodexFeatures()
		if features.CommandError != nil {
			return features.CommandError
		}
		if features.Memories == detect.FeatureEnabled {
			if err := setCodexFeature("memories", false); err != nil {
				return err
			}
		}
	}
	return removeCodexMemoryState()
}

func codexMemoriesOptedOut() bool {
	return loadCodexMemoryState() == codexMemoryOptedOut
}

func loadCodexMemoryState() string {
	b, err := os.ReadFile(paths.CodexMemoryStatePath())
	if err != nil {
		return ""
	}
	switch state := strings.TrimSpace(string(b)); state {
	case codexMemoryManaged, codexMemoryPreexisting, codexMemoryOptedOut:
		return state
	default:
		return ""
	}
}

func saveCodexMemoryState(state string) error {
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".codex-memory-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(state + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, paths.CodexMemoryStatePath()); err != nil {
		return err
	}
	return os.Chmod(paths.CodexMemoryStatePath(), 0o600)
}

func removeCodexMemoryState() error {
	err := os.Remove(paths.CodexMemoryStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
