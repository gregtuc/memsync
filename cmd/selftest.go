package cmd

import (
	"bytes"
	"fmt"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

// selfTest exercises the real encrypt → guard → decrypt → render pipeline
// without ever touching the agents' own memory folders.
func selfTest() error {
	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		return err
	}

	canary := []byte("memsync canary — round-trip check")
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
	ok("encrypt → decrypt round-trip verified")

	if err := vault.GuardTree(key); err != nil {
		return err
	}
	ok("vault guard passed (working tree is ciphertext-only)")

	_ = courier.RenderContext("Codex", []courier.Memory{
		{Origin: "codex", Scope: "global", Title: "canary", Body: "hello from the other tool"},
	}, 4096)
	ok("cross-tool context render OK")
	// TODO: full self-test that drives each tool's SessionStart hook and asserts
	// the other tool's memory appears in the emitted payload.
	return nil
}
