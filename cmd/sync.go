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
	"github.com/gregtuc/memsync/internal/dedup"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/simhash"
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

	toWrite := selectForWrite(mems, loadFingerprints(key))
	for _, m := range toWrite {
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
	if err := vault.CommitAll(fmt.Sprintf("sync %s (%d records)", tool, len(toWrite))); err != nil {
		return err
	}
	return vault.Push()
}

// selectForWrite drops memories that are (1) verbatim copies of memsync-injected
// context or (2) near-exact echoes of another tool's already-stored memory, so
// captures don't loop or accumulate. Same-origin updates pass through.
func selectForWrite(mems []courier.Memory, seen []dedup.Fingerprint) []courier.Memory {
	var out []courier.Memory
	for _, m := range mems {
		if courier.LooksSynced(m.Body) {
			continue
		}
		h := simhash.Hash(m.Body)
		if dedup.IsEcho(m.Origin, h, seen, dedup.DefaultThreshold) {
			continue
		}
		out = append(out, m)
		seen = append(seen, dedup.Fingerprint{Origin: m.Origin, Hash: h})
	}
	return out
}

// loadFingerprints reads the existing vault records so a new capture can be
// deduped against what is already stored.
func loadFingerprints(key []byte) []dedup.Fingerprint {
	files, err := vault.Records()
	if err != nil {
		return nil
	}
	var fps []dedup.Fingerprint
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		plain, err := crypto.Decrypt(key, b)
		if err != nil {
			continue
		}
		var r record
		if json.Unmarshal(plain, &r) != nil {
			continue
		}
		fps = append(fps, dedup.Fingerprint{Origin: r.Origin, Hash: simhash.Hash(r.Body)})
	}
	return fps
}

// recordName is content-addressed on IDENTITY (origin+scope+title) so re-syncing
// an edited memory overwrites its file instead of piling up duplicates.
func recordName(r record) string {
	sum := sha256.Sum256([]byte(r.Origin + "\x00" + r.Scope + "\x00" + r.Title))
	return hex.EncodeToString(sum[:8]) + ".enc"
}
