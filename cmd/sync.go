package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

type record struct {
	Origin string `json:"origin"`
	Scope  string `json:"scope"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// runSync is hook-invoked (FileChanged / SessionEnd / Stop). It captures the
// tool's own memory into the encrypted vault. Fails open so it can't break a session.
func runSync(args []string) int {
	tool := flagValue(args, "--tool")
	if err := syncTool(tool); err != nil {
		fmt.Fprintf(os.Stderr, "memsync sync: %v\n", err)
	}
	return 0
}

func syncTool(tool string) error {
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		return err
	}
	bin, err := selfPath()
	if err != nil {
		return err
	}
	if err := vault.Ensure(bin); err != nil {
		return err
	}

	var mems []courier.Memory
	switch tool {
	case "claude":
		mems, _ = courier.CollectClaude()
	case "codex":
		mems, _ = courier.CollectCodex()
	default:
		return fmt.Errorf("unknown --tool %q", tool)
	}

	for _, m := range mems {
		rec := record{Origin: m.Origin, Scope: m.Scope, Title: m.Title, Body: m.Body}
		plain, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		env, err := crypto.Encrypt(key, plain)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(paths.VaultDir(), recordName(rec)), env, 0o644); err != nil {
			continue
		}
	}
	return vault.CommitAll(fmt.Sprintf("sync %s (%d records)", tool, len(mems)))
}

// recordName is content-addressed on IDENTITY (origin+scope+title) so re-syncing
// an edited memory overwrites its file instead of piling up duplicates.
func recordName(r record) string {
	sum := sha256.Sum256([]byte(r.Origin + "\x00" + r.Scope + "\x00" + r.Title))
	return hex.EncodeToString(sum[:8]) + ".enc"
}
