package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectsFreshCLIInstallBeforeHomeExists(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\necho test-version\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-home"))
	t.Setenv("PATH", binDir)

	for _, tool := range All() {
		if !tool.Present {
			t.Fatalf("%s binary was ignored because its home did not exist", tool.Name)
		}
	}
}
