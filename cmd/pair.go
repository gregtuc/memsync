package cmd

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/pair"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

type pairPayload struct {
	Version   int    `json:"version"`
	AESKeyHex string `json:"aes_key_hex"`
	RemoteURL string `json:"remote_url"`
}

type joinState struct {
	Version       int    `json:"version"`
	PrivateKeyHex string `json:"private_key_hex"`
	Reply         string `json:"reply,omitempty"`
}

// runPair (machine 1) seals the vault key to the new machine's public invite.
func runPair(args []string) int {
	return runPairFrom(args, bufio.NewReader(os.Stdin))
}

func runPairFrom(args []string, input *bufio.Reader) int {
	fmt.Println("\nPaste the code shown on the new laptop:")
	invite := readInputLine(input)
	if invite == "" {
		return fail(fmt.Errorf("no invite provided"))
	}
	fingerprint, err := pair.InviteFingerprint(invite)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("\nVerification code: %s\n", fingerprint)
	if !hasFlag(args, "--yes") {
		fmt.Print("Does this match the code on the new laptop? [y/N]: ")
		answer := strings.ToLower(readInputLine(input))
		if answer != "y" && answer != "yes" {
			return fail(fmt.Errorf("pairing cancelled; the invite was not confirmed"))
		}
	}

	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return fail(fmt.Errorf("memsync is not initialized on this machine; run `memsync init`: %w", err))
	}

	step("Preparing private sync...")
	if !vault.HasRemote() {
		if code := remoteCreateFlow(false); code != 0 {
			return code
		}
	}
	url := vault.RemoteURL()
	if vault.RemoteHasCredentials(url) {
		return fail(fmt.Errorf("the configured remote contains HTTP credentials; replace it with a credential-free URL before pairing"))
	}
	if !vault.RemoteReachable() {
		if !isGitHubHTTPS(url) {
			return fail(fmt.Errorf("cannot reach %s; authenticate Git on this machine before pairing", vault.DisplayRemoteURL(url)))
		}
		if err := ensureGitHubCLIAuth(); err != nil {
			return fail(err)
		}
		if !vault.RemoteReachable() {
			return fail(fmt.Errorf("cannot access %s after GitHub sign-in; make sure this account can access the repository, then retry", vault.DisplayRemoteURL(url)))
		}
	}
	ok("Private sync is ready")

	payload, err := json.Marshal(pairPayload{Version: 1, AESKeyHex: hex.EncodeToString(key), RemoteURL: url})
	if err != nil {
		return fail(err)
	}
	reply, err := pair.Seal(invite, payload)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("\nReply code:\n\n")
	fmt.Println("    " + reply)
	fmt.Println("\nPaste this on the new laptop to finish.")
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
	stateExists := fileExists(paths.JoinStatePath())
	if vault.HasRemote() && !stateExists {
		ok("This laptop is already connected")
		return 0
	}
	id, state, err := loadOrCreateJoinState()
	if err != nil {
		return fail(err)
	}
	input := bufio.NewReader(os.Stdin)

	reply := state.Reply
	if reply == "" {
		fmt.Printf("\nCopy this code to the laptop that already uses memsync:\n\n")
		fmt.Println("    " + id.Invite())
		fingerprint, _ := pair.InviteFingerprint(id.Invite())
		fmt.Println("\nVerification code: " + fingerprint)
		fmt.Println("\nOn that laptop, run `memsync pair` and paste the code.")
		fmt.Print("\nPaste the reply code here: ")
		reply = readInputLine(input)
	} else {
		fmt.Println("\nResuming this laptop's connection...")
	}

	p, key, err := openPairReply(id, reply)
	if err != nil {
		state.Reply = ""
		_ = saveJoinState(state)
		return fail(fmt.Errorf("the reply code is invalid; run `memsync pair` again with the same invite: %w", err))
	}
	if state.Reply == "" {
		state.Reply = reply
		if err := saveJoinState(state); err != nil {
			return fail(err)
		}
	}

	step("Connecting this laptop...")
	stage, err := vault.StageClone(p.RemoteURL)
	if err != nil && isGitHubHTTPS(p.RemoteURL) {
		if authErr := ensureGitHubCLIAuth(); authErr != nil {
			return fail(authErr)
		}
		stage, err = vault.StageClone(p.RemoteURL)
	}
	if err != nil {
		return fail(fmt.Errorf("cannot clone the shared vault; authenticate Git on this machine and try again: %w", err))
	}
	defer stage.Discard()
	if err := stage.Validate(key); err != nil {
		return fail(err)
	}

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
	if err := vault.InstallGuards(bin); err != nil {
		return fail(err)
	}
	if _, _, err := device.LoadOrCreate(paths.DeviceIDPath()); err != nil {
		return fail(err)
	}
	tools := detect.All()
	ready, issues := wireJoinedTools(tools, bin)

	if err := selfTest(); err != nil {
		return fail(err)
	}
	captureJoinedTools(tools, ready, issues)

	var connected []string
	for _, installed := range tools {
		if ready[installed.Name] {
			connected = append(connected, friendlyToolName(installed.Name))
		}
	}
	if len(connected) == 0 {
		return fail(fmt.Errorf("this laptop joined the private sync, but no installed tool could be connected; run `memsync init` to finish setup"))
	}
	ok("This laptop is set up")
	for _, installed := range tools {
		if installed.Present && issues[installed.Name] != nil {
			warn("%s needs attention; run `memsync init`", friendlyToolName(installed.Name))
		}
	}
	fmt.Printf("\nDone. Restart %s.\n", strings.Join(connected, " and "))
	if ready["Codex CLI"] {
		fmt.Println("When Codex asks, approve memsync to finish.")
	}
	_ = os.Remove(paths.JoinStatePath())
	return 0
}

func openPairReply(id *pair.Identity, reply string) (pairPayload, []byte, error) {
	var p pairPayload
	payload, err := id.Open(reply)
	if err != nil {
		return p, nil, err
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return p, nil, err
	}
	if p.Version != 1 {
		return p, nil, fmt.Errorf("unsupported pairing payload version %d", p.Version)
	}
	if strings.TrimSpace(p.RemoteURL) == "" {
		return p, nil, fmt.Errorf("pairing reply did not contain a remote URL")
	}
	if vault.RemoteHasCredentials(p.RemoteURL) {
		return p, nil, fmt.Errorf("pairing reply contained a credential-bearing remote URL")
	}
	key, err := hex.DecodeString(p.AESKeyHex)
	if err != nil {
		return p, nil, err
	}
	if len(key) != crypto.KeySize {
		return p, nil, fmt.Errorf("pairing reply contained an invalid encryption key")
	}
	return p, key, nil
}

func loadOrCreateJoinState() (*pair.Identity, *joinState, error) {
	b, err := os.ReadFile(paths.JoinStatePath())
	if err == nil {
		var state joinState
		if json.Unmarshal(b, &state) == nil && state.Version == 1 {
			privateKey, decodeErr := hex.DecodeString(state.PrivateKeyHex)
			if decodeErr == nil {
				id, restoreErr := pair.RestoreIdentity(privateKey)
				if restoreErr == nil {
					return id, &state, nil
				}
			}
		}
		_ = os.Remove(paths.JoinStatePath())
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	id, err := pair.NewIdentity()
	if err != nil {
		return nil, nil, err
	}
	state := &joinState{Version: 1, PrivateKeyHex: hex.EncodeToString(id.PrivateBytes())}
	if err := saveJoinState(state); err != nil {
		return nil, nil, err
	}
	return id, state, nil
}

func saveJoinState(state *joinState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".join-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, paths.JoinStatePath())
}

func captureJoinedTools(tools []detect.Tool, ready map[string]bool, issues map[string]error) {
	for _, installed := range tools {
		if !ready[installed.Name] {
			continue
		}
		tool := "claude"
		if installed.Name == "Codex CLI" {
			tool = "codex"
		}
		if _, err := syncTool(tool); err != nil {
			issues[installed.Name] = err
			ready[installed.Name] = false
		}
	}
}

func wireJoinedTools(tools []detect.Tool, bin string) (map[string]bool, map[string]error) {
	ready := make(map[string]bool)
	issues := make(map[string]error)
	for _, tool := range tools {
		if !tool.Present {
			continue
		}
		ready[tool.Name] = true
		if err := os.MkdirAll(tool.Home, 0o700); err != nil {
			ready[tool.Name] = false
			issues[tool.Name] = err
			continue
		}
		if tool.Name == "Codex CLI" {
			features := detect.DetectCodexFeatures()
			if features.CommandError != nil {
				ready[tool.Name] = false
				issues[tool.Name] = features.CommandError
				continue
			}
			if features.Hooks == detect.FeatureDisabled {
				if err := setCodexFeature("hooks", true); err != nil {
					ready[tool.Name] = false
					issues[tool.Name] = err
					continue
				}
			}
			if err := configureCodexMemories(features.Memories, nil); err != nil {
				ready[tool.Name] = false
				issues[tool.Name] = err
				continue
			}
		}
		if err := wire(tool.Name, bin); err != nil {
			ready[tool.Name] = false
			issues[tool.Name] = err
			continue
		}
		if tool.Name == "Claude Code" {
			if enabled, err := hooks.ClaudeHooksEnabled(); err != nil {
				ready[tool.Name] = false
				issues[tool.Name] = err
			} else if !enabled {
				ready[tool.Name] = false
				issues[tool.Name] = fmt.Errorf("hooks are disabled")
			}
		}
	}
	return ready, issues
}

func readInputLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}
