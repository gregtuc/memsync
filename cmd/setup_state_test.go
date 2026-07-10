package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregtuc/memsync/internal/paths"
)

func TestSetupNeverReplacesMissingEstablishedKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	if err := os.MkdirAll(paths.VaultDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.VaultDir(), "existing.enc"), []byte("established ciphertext placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadOrCreateSetupKey(); err == nil || !strings.Contains(err.Error(), "refusing to create") {
		t.Fatalf("missing established key was replaced: %v", err)
	}
	if _, err := os.Stat(paths.KeyPath()); !os.IsNotExist(err) {
		t.Fatalf("recovery check created a replacement key: %v", err)
	}
}

func TestSetupCreatesKeyForGenuinelyFreshState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	key, created, err := loadOrCreateSetupKey()
	if err != nil || !created || len(key) == 0 {
		t.Fatalf("fresh key setup failed: created=%v len=%d err=%v", created, len(key), err)
	}
}
