package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
)

func setupHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@e.co")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@e.co")
}

func TestEnsureCreatesRepoAndGuards(t *testing.T) {
	setupHome(t)
	if err := Ensure("/bin/true"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), ".git")); err != nil {
		t.Fatal("vault is not a git repo")
	}
	if !GuardsInstalled() {
		t.Fatal("guards not installed")
	}
	if _, err := os.Stat(filepath.Join(paths.VaultDir(), ".gitattributes")); err != nil {
		t.Fatal(".gitattributes missing")
	}
}

func TestGuardTreeAcceptsCiphertextAndRejectsPlaintext(t *testing.T) {
	setupHome(t)
	if err := Ensure("/bin/true"); err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.GenerateKey()
	env, _ := crypto.Encrypt(key, []byte(`{"origin":"claude","body":"x"}`))
	if err := os.WriteFile(filepath.Join(paths.VaultDir(), "a.enc"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	// ciphertext record + .gitattributes control file must pass
	if err := GuardTree(key); err != nil {
		t.Fatalf("guard rejected a clean tree: %v", err)
	}
	// a plaintext file must fail closed
	os.WriteFile(filepath.Join(paths.VaultDir(), "leak.txt"), []byte("secret"), 0o644)
	if err := GuardTree(key); err == nil {
		t.Fatal("guard accepted plaintext")
	}
}

func TestGuardTreeRejectsWrongKeyCiphertext(t *testing.T) {
	setupHome(t)
	Ensure("/bin/true")
	k1, _ := crypto.GenerateKey()
	k2, _ := crypto.GenerateKey()
	env, _ := crypto.Encrypt(k1, []byte("data"))
	os.WriteFile(filepath.Join(paths.VaultDir(), "a.enc"), env, 0o644)
	if err := GuardTree(k2); err == nil {
		t.Fatal("guard accepted ciphertext that does not decrypt under the local key")
	}
}

func TestRecordsListsOnlyEnc(t *testing.T) {
	setupHome(t)
	Ensure("/bin/true")
	os.WriteFile(filepath.Join(paths.VaultDir(), "a.enc"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(paths.VaultDir(), "notes.txt"), []byte("y"), 0o644)
	recs, err := Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 .enc record, got %d", len(recs))
	}
}
