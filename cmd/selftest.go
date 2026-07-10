package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

// selfTest exercises the real encrypt → guard → decrypt → render pipeline
// without ever touching the agents' own memory folders.
func selfTest() error {
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return err
	}

	canary := []byte("memsync canary - round-trip check")
	env, err := crypto.Encrypt(key, canary)
	if err != nil {
		return err
	}
	if !crypto.IsCiphertext(env) {
		return fmt.Errorf("encrypted canary is missing the envelope header")
	}
	back, err := crypto.Decrypt(key, env)
	if err != nil {
		return err
	}
	if !bytes.Equal(back, canary) {
		return fmt.Errorf("canary did not survive encrypt/decrypt")
	}
	if err := vault.GuardTree(key); err != nil {
		return err
	}

	_ = courier.RenderContext("Codex", []courier.Memory{
		{Origin: "codex", Scope: "global", Title: "canary", Body: "hello from the other tool"},
	}, 4096)

	// Exercise the actual encrypted-record -> decrypt -> filtering -> hook JSON
	// path without reading or writing either agent's own memory store.
	r := record{
		SchemaVersion: 2,
		Origin:        "claude",
		DeviceID:      "selftest-device",
		DeviceName:    "selftest",
		Scope:         "selftest",
		Title:         "round-trip canary",
		Body:          "memsync hook-payload canary",
		UpdatedAt:     time.Now().Add(time.Hour).Unix(),
	}
	if err := vault.WithOperationLock(func() error {
		plain, err := json.Marshal(r)
		if err != nil {
			return err
		}
		envelope, err := crypto.Encrypt(key, plain)
		if err != nil {
			return err
		}
		path := filepath.Join(paths.VaultDir(), recordName(r))
		if err := os.WriteFile(path, envelope, 0o600); err != nil {
			return err
		}
		defer os.Remove(path)
		if err := vault.GuardTree(key); err != nil {
			return err
		}
		ctx, _, err := injectionContextLocked("codex", false)
		if err != nil {
			return err
		}
		if !strings.Contains(ctx, r.Body) {
			return fmt.Errorf("hook payload did not contain the encrypted canary")
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}
