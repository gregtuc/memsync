package cmd

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/pair"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

type pairPayload struct {
	Version   int    `json:"version"`
	AESKeyHex string `json:"aes_key_hex"`
	RemoteURL string `json:"remote_url"`
}

// runPair (machine 1) seals the vault key to the new machine's public invite.
func runPair(args []string) int {
	if !vault.HasRemote() {
		fmt.Println("No remote yet — a second machine needs somewhere to sync.")
		fmt.Println("Run `memsync remote create` (or `memsync remote set <url>`) first.")
		return 1
	}
	url := vault.RemoteURL()
	if !vault.RemoteReachable() {
		warn("cannot reach the remote (%s) — check `gh auth`/network; continuing anyway", url)
	}

	fmt.Println("\nPaste the invite code from the new machine (it printed one after `memsync join`):")
	invite := readLine()
	if invite == "" {
		return fail(fmt.Errorf("no invite provided"))
	}

	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		return fail(err)
	}
	payload, err := json.Marshal(pairPayload{Version: 1, AESKeyHex: hex.EncodeToString(key), RemoteURL: url})
	if err != nil {
		return fail(err)
	}
	reply, err := pair.Seal(invite, payload)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("\nSealed reply (not a secret — only the new machine can open it):\n\n")
	fmt.Println("    " + reply)
	fmt.Println("\nPaste this back on the new machine to finish.")
	return 0
}

// runJoin (machine 2) creates an identity, prints an invite, then unseals the
// reply to adopt the vault key, clone the vault, wire hooks, and self-test.
func runJoin(args []string) int {
	bin, err := selfPath()
	if err != nil {
		return fail(err)
	}
	id, err := pair.NewIdentity()
	if err != nil {
		return fail(err)
	}

	fmt.Printf("\nYour machine's invite code (safe to send over anything — it's a public key):\n\n")
	fmt.Println("    " + id.Invite())
	fmt.Println("\nOn your other machine: run `memsync pair`, paste that invite, copy the reply.")
	fmt.Print("\nPaste the sealed reply here: ")
	reply := readLine()

	payload, err := id.Open(reply)
	if err != nil {
		return fail(fmt.Errorf("could not open the reply (wrong or corrupt token?): %w", err))
	}
	var p pairPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fail(err)
	}
	key, err := hex.DecodeString(p.AESKeyHex)
	if err != nil {
		return fail(err)
	}

	step("Joining vault...")
	if err := crypto.SaveKey(paths.KeyPath(), key); err != nil {
		return fail(err)
	}
	ok("key adopted")
	if err := vault.Clone(p.RemoteURL); err != nil {
		return fail(err)
	}
	ok("vault cloned from %s", p.RemoteURL)
	if err := vault.InstallGuards(bin); err != nil {
		return fail(err)
	}
	ok("guards installed")
	for _, t := range detect.All() {
		if t.Present {
			if err := wire(t.Name, bin); err != nil {
				return fail(err)
			}
		}
	}

	step("Verifying...")
	if err := selfTest(); err != nil {
		return fail(err)
	}
	fmt.Println("\n✓ This machine now shares memories with your others.")
	fmt.Println("↻ Restart any open Claude Code / Codex sessions.")
	return 0
}

func readLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}
