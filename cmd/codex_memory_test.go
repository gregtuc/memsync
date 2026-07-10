package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/paths"
)

func TestCodexMemorySetupPreservesALaterDisable(t *testing.T) {
	home := t.TempDir()
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "features.log")
	script := "#!/bin/sh\necho \"$2 $3\" >> \"$FEATURE_LOG\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FEATURE_LOG", logPath)

	if err := configureCodexMemories(detect.FeatureDisabled, nil); err != nil {
		t.Fatal(err)
	}
	if got := loadCodexMemoryState(); got != codexMemoryManaged {
		t.Fatalf("first setup ownership = %q", got)
	}
	if err := configureCodexMemories(detect.FeatureDisabled, nil); err != nil {
		t.Fatal(err)
	}
	if got := loadCodexMemoryState(); got != codexMemoryOptedOut {
		t.Fatalf("later disable was not preserved: %q", got)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(log), "enable memories"); count != 1 {
		t.Fatalf("memory feature was re-enabled %d times:\n%s", count, log)
	}
}

func TestUninstallRestoresOnlyMemsyncManagedMemorySetting(t *testing.T) {
	for _, test := range []struct {
		name        string
		state       string
		wantDisable bool
	}{
		{name: "managed", state: codexMemoryManaged, wantDisable: true},
		{name: "preexisting", state: codexMemoryPreexisting, wantDisable: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			fakeBin := filepath.Join(home, "bin")
			if err := os.MkdirAll(fakeBin, 0o755); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(home, "features.log")
			script := `#!/bin/sh
if [ "$1 $2" = "features list" ]; then
  echo 'hooks stable true'
  echo 'memories experimental true'
elif [ "$1 $2" = "features disable" ]; then
  echo "$2 $3" >> "$FEATURE_LOG"
fi
`
			if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
			t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FEATURE_LOG", logPath)
			if err := saveCodexMemoryState(test.state); err != nil {
				t.Fatal(err)
			}
			if err := restoreCodexMemoryPreference(); err != nil {
				t.Fatal(err)
			}
			log, _ := os.ReadFile(logPath)
			gotDisable := strings.Contains(string(log), "disable memories")
			if gotDisable != test.wantDisable {
				t.Fatalf("disable called = %v, want %v; log=%q", gotDisable, test.wantDisable, log)
			}
			if _, err := os.Stat(paths.CodexMemoryStatePath()); !os.IsNotExist(err) {
				t.Fatalf("ownership state survived uninstall: %v", err)
			}
		})
	}
}
