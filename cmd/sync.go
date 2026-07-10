package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregtuc/memsync/internal/activity"
	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/dedup"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/project"
	"github.com/gregtuc/memsync/internal/simhash"
	"github.com/gregtuc/memsync/internal/vault"
)

type record struct {
	SchemaVersion int    `json:"schema_version,omitempty"`
	Origin        string `json:"origin"`
	DeviceID      string `json:"device_id,omitempty"`
	DeviceName    string `json:"device_name,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	Scope         string `json:"scope"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

type syncResult struct {
	Found   int
	Written int
	Removed int
}

// runSync is hook-invoked (FileChanged / SessionEnd / Stop). It captures the
// tool's own memory into the encrypted vault. Fails open so it can't break a session.
func runSync(args []string) int {
	tool := flagValue(args, "--tool")
	if tool == "" {
		return runSyncNow()
	}
	result, err := syncTool(tool)
	detail := fmt.Sprintf("%d found, %d updated, %d removed", result.Found, result.Written, result.Removed)
	_ = activity.Record(paths.DataDir(), tool, "capture", detail, err)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memsync sync: %v\n", err)
	}
	// Codex Stop hooks require valid JSON on stdout when exiting 0; an empty
	// object is a no-op for both Codex (Stop) and Claude (SessionEnd).
	fmt.Fprintln(os.Stdout, "{}")
	return 0
}

func runSyncNow() int {
	fmt.Println("\nSyncing local memories now...")
	failed := false
	sources := []struct {
		name  string
		label string
	}{}
	for _, installed := range detect.All() {
		if !installed.Present {
			continue
		}
		if installed.Name == "Claude Code" {
			sources = append(sources, struct{ name, label string }{name: "claude", label: "Claude Code"})
		} else if installed.Name == "Codex CLI" {
			sources = append(sources, struct{ name, label string }{name: "codex", label: "Codex"})
		}
	}
	if len(sources) == 0 {
		return fail(fmt.Errorf("neither Claude Code nor Codex is installed"))
	}
	if vault.HasRemote() {
		if err := vault.WithOperationLock(vault.Pull); err != nil {
			warn("Remote       %v", err)
			failed = true
		} else {
			ok("Remote       latest encrypted vault fetched")
		}
	}
	for _, tool := range sources {
		result, err := syncTool(tool.name)
		if err != nil {
			warn("%-12s %v", tool.label, err)
			failed = true
			continue
		}
		ok("%-12s %d found · %d updated · %d removed", tool.label, result.Found, result.Written, result.Removed)
	}
	if failed {
		return 1
	}
	fmt.Println("\nSync complete.")
	return 0
}

func syncTool(tool string) (syncResult, error) {
	var result syncResult
	err := vault.WithOperationLock(func() error {
		key, err := crypto.LoadKey(paths.KeyPath())
		if err != nil {
			return err
		}
		dev, err := device.Load(paths.DeviceIDPath())
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
		projectID := ""
		legacyScopes := map[string]bool{}
		switch tool {
		case "claude":
			cwd, _ := os.Getwd()
			if abs, absErr := filepath.Abs(cwd); absErr == nil {
				if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
					abs = resolved
				}
				legacyScopes[strings.ReplaceAll(filepath.Clean(abs), string(filepath.Separator), "-")] = true
			}
			projectID = project.Identify(cwd).ID
			mems, err = courier.CollectClaudeAt(cwd)
		case "codex":
			legacyScopes["global"] = true
			mems, err = courier.CollectCodex()
		default:
			return fmt.Errorf("unknown --tool %q", tool)
		}
		if err != nil {
			return fmt.Errorf("read %s memory source: %w", tool, err)
		}

		toWrite := selectForWrite(mems, loadFingerprints(key))
		result.Found = len(mems)
		keep := make(map[string]bool, len(toWrite))
		for _, m := range toWrite {
			rec := record{
				SchemaVersion: 2,
				Origin:        m.Origin,
				DeviceID:      dev.ID,
				DeviceName:    dev.Name,
				ProjectID:     m.ProjectID,
				Scope:         m.Scope,
				Title:         m.Title,
				Body:          m.Body,
				UpdatedAt:     m.UpdatedAt,
			}
			plain, err := json.Marshal(rec)
			if err != nil {
				return err
			}
			name := recordName(rec)
			keep[name] = true
			changed, err := writeRecord(key, filepath.Join(paths.VaultDir(), name), plain)
			if err != nil {
				return err
			}
			if changed {
				result.Written++
			}
		}
		removed, err := removeStaleRecords(key, tool, dev.ID, projectID, keep, legacyScopes)
		if err != nil {
			return err
		}
		result.Removed = removed
		if err := vault.CommitAll(fmt.Sprintf("sync %s (%d records)", tool, len(toWrite))); err != nil {
			return err
		}
		if vault.NeedsPush() {
			return vault.Push()
		}
		return nil
	})
	return result, err
}

func writeRecord(key []byte, path string, plain []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil {
		decrypted, err := crypto.Decrypt(key, current)
		if err != nil {
			return false, fmt.Errorf("existing record %s is corrupt: %w", filepath.Base(path), err)
		}
		if bytes.Equal(decrypted, plain) {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	env, err := crypto.Encrypt(key, plain)
	if err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(paths.DataDir(), ".record-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if _, err := tmp.Write(env); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, os.Chmod(path, 0o600)
}

func removeStaleRecords(key []byte, origin, deviceID, projectID string, keep map[string]bool, legacyScopes ...map[string]bool) (int, error) {
	files, err := vault.Records()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, path := range files {
		name := filepath.Base(path)
		if keep[name] {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return removed, err
		}
		plain, err := crypto.Decrypt(key, b)
		if err != nil {
			return removed, err
		}
		var r record
		if err := json.Unmarshal(plain, &r); err != nil {
			return removed, err
		}
		legacyOwned := false
		if r.Origin == origin && r.DeviceID == "" && r.ProjectID == "" && len(legacyScopes) > 0 {
			legacyOwned = legacyScopes[0][r.Scope]
		}
		if legacyOwned || r.Origin == origin && r.DeviceID == deviceID && r.ProjectID == projectID {
			if err := os.Remove(path); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
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
	scopeIdentity := r.Scope
	if r.ProjectID != "" {
		scopeIdentity = r.ProjectID
	}
	identity := r.DeviceID + "\x00" + r.Origin + "\x00" + scopeIdentity + "\x00" + r.Title
	// Preserve legacy names while reading/migrating records written before
	// device identity existed.
	if r.DeviceID == "" {
		identity = r.Origin + "\x00" + r.Scope + "\x00" + r.Title
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:8]) + ".enc"
}
