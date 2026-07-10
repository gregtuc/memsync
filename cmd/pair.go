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
	"github.com/gregtuc/memsync/internal/device"
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
		fmt.Println("No remote yet - a second machine needs somewhere to sync.")
		fmt.Println("Run `memsync remote create` (or `memsync remote set <url>`) first.")
		return 1
	}
	url := vault.RemoteURL()
	if vault.RemoteHasCredentials(url) {
		return fail(fmt.Errorf("the configured remote contains HTTP credentials; replace it with a credential-free URL before pairing"))
	}
	if !vault.RemoteReachable() {
		return fail(fmt.Errorf("cannot reach %s; authenticate Git on this machine before pairing", vault.DisplayRemoteURL(url)))
	}

	fmt.Println("\nPaste the invite code from the new machine (it printed one after `memsync join`):")
	invite := readLine()
	if invite == "" {
		return fail(fmt.Errorf("no invite provided"))
	}
	fingerprint, err := pair.InviteFingerprint(invite)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("\nVerification code: %s\n", fingerprint)
	if !hasFlag(args, "--yes") {
		fmt.Print("Confirm this matches the code shown on the new machine [y/N]: ")
		answer := strings.ToLower(readLine())
		if answer != "y" && answer != "yes" {
			return fail(fmt.Errorf("pairing cancelled; the invite was not confirmed"))
		}
	}

	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return fail(fmt.Errorf("memsync is not initialized on this machine; run `memsync init`: %w", err))
	}
	payload, err := json.Marshal(pairPayload{Version: 1, AESKeyHex: hex.EncodeToString(key), RemoteURL: url})
	if err != nil {
		return fail(err)
	}
	reply, err := pair.Seal(invite, payload)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("\nSealed reply (encrypted for the verified new machine):\n\n")
	fmt.Println("    " + reply)
	fmt.Println("\nPaste this back on the new machine to finish.")
	return 0
}

// runJoin (machine 2) creates an identity, prints an invite, then unseals the
// reply to adopt the vault key, clone the vault, wire hooks, and self-test.
func runJoin(args []string) int {
	if _, err := crypto.LoadKey(paths.KeyPath()); err != nil || !fileExists(paths.VaultDir()) {
		return fail(fmt.Errorf("set up this laptop first with `memsync init`, then run `memsync join` again"))
	}
	if _, err := device.Load(paths.DeviceIDPath()); err != nil {
		return fail(fmt.Errorf("device setup is incomplete; run `memsync doctor --fix`, then try `memsync join` again"))
	}
	bin, err := selfPath()
	if err != nil {
		return fail(err)
	}
	id, err := pair.NewIdentity()
	if err != nil {
		return fail(err)
	}

	fmt.Printf("\nYour machine's invite code (public, but verify the code below before accepting a reply):\n\n")
	fmt.Println("    " + id.Invite())
	fingerprint, _ := pair.InviteFingerprint(id.Invite())
	fmt.Println("\nVerification code: " + fingerprint)
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
	if p.Version != 1 {
		return fail(fmt.Errorf("unsupported pairing payload version %d", p.Version))
	}
	if strings.TrimSpace(p.RemoteURL) == "" {
		return fail(fmt.Errorf("pairing reply did not contain a remote URL"))
	}
	if vault.RemoteHasCredentials(p.RemoteURL) {
		return fail(fmt.Errorf("pairing reply contained a credential-bearing remote URL; use credential helpers independently on each machine"))
	}
	key, err := hex.DecodeString(p.AESKeyHex)
	if err != nil {
		return fail(err)
	}

	step("Checking remote access before changing this machine...")
	stage, err := vault.StageClone(p.RemoteURL)
	if err != nil {
		return fail(fmt.Errorf("cannot clone the shared vault; authenticate Git on this machine and try again: %w", err))
	}
	defer stage.Discard()
	if err := stage.Validate(key); err != nil {
		return fail(err)
	}
	ok("remote cloned and every record decrypts with the paired key")

	step("Joining vault...")
	if err := vault.WithOperationLock(func() error {
		oldKey, err := crypto.LoadKey(paths.KeyPath())
		if err != nil {
			return fmt.Errorf("existing key is unreadable; refusing to replace it: %w", err)
		}
		if err := crypto.SaveKey(paths.KeyPath(), key); err != nil {
			return err
		}
		if err := stage.Activate(); err != nil {
			_ = crypto.SaveKey(paths.KeyPath(), oldKey)
			return fmt.Errorf("could not activate the cloned vault; previous key and vault were preserved: %w", err)
		}
		return nil
	}); err != nil {
		return fail(err)
	}
	ok("key adopted")
	ok("vault cloned from %s", vault.DisplayRemoteURL(p.RemoteURL))
	if err := vault.InstallGuards(bin); err != nil {
		return fail(err)
	}
	ok("guards installed")
	if _, _, err := device.LoadOrCreate(paths.DeviceIDPath()); err != nil {
		return fail(err)
	}
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
	for _, tool := range []string{"claude", "codex"} {
		if _, err := syncTool(tool); err != nil {
			warn("initial %s capture will retry from its next hook: %v", tool, err)
		}
	}
	fmt.Println("\n✓ This machine now shares memories with your others.")
	fmt.Println("↻ Restart any open Claude Code / Codex sessions.")
	if hasPresentTool(detect.All(), "Codex CLI") {
		fmt.Println("  If Codex is not observed in `memsync status`, review the hooks with `/hooks`.")
	}
	return 0
}

func readLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}
