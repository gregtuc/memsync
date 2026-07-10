package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregtuc/memsync/internal/paths"
)

func TestCommandHelpNeverRunsCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	if code := Execute([]string{"init", "--help"}); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	for _, path := range []string{paths.KeyPath(), paths.VaultDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("help mutated setup state at %s: %v", path, err)
		}
	}
}
