package cmd

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/gregtuc/memsync/internal/activity"
	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

type doctorReport struct {
	failures int
	warnings int
}

func (r *doctorReport) pass(label, detail string) {
	fmt.Printf("  ✓ %-20s %s\n", label, detail)
}

func (r *doctorReport) warn(label, detail string) {
	r.warnings++
	fmt.Printf("  ! %-20s %s\n", label, detail)
}

func (r *doctorReport) fail(label, detail string) {
	r.failures++
	fmt.Printf("  ✗ %-20s %s\n", label, detail)
}

func runDoctor(args []string) int {
	fix := hasFlag(args, "--fix")
	if fix {
		if err := repairSetup(); err != nil {
			return fail(err)
		}
	}

	fmt.Println("\nmemsync doctor")
	var report doctorReport
	tools := detect.All()
	present := 0

	fmt.Println("\nTools and memory sources")
	for _, t := range tools {
		if !t.Present {
			report.warn(t.Name, "not detected")
			continue
		}
		present++
		version := t.Version
		if version == "" {
			version = "version unavailable"
		}
		report.pass(t.Name, version)
		switch t.Name {
		case "Claude Code":
			mems, err := courier.CollectClaude()
			if err != nil {
				report.fail("Claude memories", err.Error())
			} else {
				report.pass("Claude memories", fmt.Sprintf("%d readable Markdown files", len(mems)))
			}
			if enabled, err := hooks.ClaudeHooksEnabled(); err != nil {
				report.fail("Claude hook settings", err.Error())
			} else if !enabled {
				report.fail("Claude hooks", "disabled by disableAllHooks=true in settings.json")
			}
		case "Codex CLI":
			features := detect.DetectCodexFeatures()
			if features.CommandError != nil {
				report.fail("Codex configuration", features.CommandError.Error())
				continue
			}
			if features.Hooks == detect.FeatureDisabled {
				report.fail("Codex hooks", "disabled; run `memsync doctor --fix`")
			}
			if features.Memories == detect.FeatureEnabled {
				mems, err := courier.CollectCodex()
				if err != nil {
					report.fail("Codex memories", err.Error())
				} else if len(mems) == 0 {
					report.warn("Codex memories", "enabled, but no consolidated memory yet (generation is asynchronous)")
				} else {
					report.pass("Codex memories", fmt.Sprintf("enabled · %d consolidated source", len(mems)))
				}
			} else {
				report.warn("Codex memories", "off; Codex → other tools is waiting. Run `memsync init --enable-codex-memories`")
			}
		}
	}
	if present == 0 {
		report.fail("Agent tools", "neither Claude Code nor Codex is available")
	}

	fmt.Println("\nHook wiring")
	for _, t := range tools {
		if !t.Present {
			continue
		}
		configured := wiredFor(t.Name)
		tool := "claude"
		if t.Name == "Codex CLI" {
			tool = "codex"
		}
		if !configured {
			report.fail(t.Name+" hooks", "not configured; run `memsync doctor --fix`")
			continue
		}
		report.pass(t.Name+" hooks", "configured")
		if observed, err := activity.Read(paths.DataDir(), tool, "inject"); err == nil {
			detail := "last observed " + relativeTime(observed.At)
			if !observed.OK {
				report.fail(t.Name+" delivery", observed.Detail)
			} else {
				report.pass(t.Name+" delivery", detail)
			}
		} else if tool == "codex" {
			report.warn("Codex hook trust", "not observed yet; open Codex and trust the memsync hooks in `/hooks`")
		} else {
			report.warn(t.Name+" delivery", "not observed yet; restart the tool once")
		}
		if observed, err := activity.Read(paths.DataDir(), tool, "capture"); err == nil {
			if observed.OK {
				report.pass(t.Name+" capture", "last observed "+relativeTime(observed.At))
			} else {
				report.fail(t.Name+" capture", observed.Detail)
			}
		}
	}

	fmt.Println("\nEncrypted storage")
	key, keyErr := crypto.LoadKey(paths.KeyPath())
	if keyErr != nil {
		if recovery, _ := keyRecoveryRequired(); recovery {
			report.fail("Encryption key", missingEstablishedKeyError().Error())
		} else {
			report.fail("Encryption key", "missing or invalid; run `memsync doctor --fix`")
		}
	} else {
		report.pass("Encryption key", "valid")
		if mode, err := fileMode(paths.KeyPath()); err == nil && mode != 0o600 {
			report.fail("Key permissions", fmt.Sprintf("%04o; expected 0600", mode))
		} else {
			report.pass("Key permissions", "0600")
		}
	}
	if dev, err := device.Load(paths.DeviceIDPath()); err != nil {
		report.fail("Device identity", "missing or invalid; run `memsync doctor --fix`")
	} else {
		report.pass("Device identity", fmt.Sprintf("%s · %s", dev.Name, dev.ID[:8]))
		if mode, err := fileMode(paths.DeviceIDPath()); err == nil && mode != 0o600 {
			report.fail("Device permissions", fmt.Sprintf("%04o; expected 0600", mode))
		}
	}
	if !fileExists(paths.VaultDir()) {
		report.fail("Vault", "missing; run `memsync doctor --fix`")
	} else {
		records, err := vault.Records()
		if err != nil {
			report.fail("Vault", err.Error())
		} else {
			report.pass("Vault", fmt.Sprintf("%d encrypted records", len(records)))
		}
		if !vault.GuardsInstalled() {
			report.fail("Git guards", "missing; run `memsync doctor --fix`")
		} else if keyErr == nil {
			if err := vault.GuardTree(key); err != nil {
				report.fail("Vault integrity", err.Error())
			} else if err := vault.GuardHistory(key); err != nil {
				report.fail("Vault history", err.Error())
			} else {
				report.pass("Vault integrity", "working tree and reachable history are ciphertext-only")
			}
		}
	}
	if keyErr == nil {
		canary := []byte("memsync doctor canary")
		if envelope, err := crypto.Encrypt(key, canary); err != nil {
			report.fail("Crypto round-trip", err.Error())
		} else if back, err := crypto.Decrypt(key, envelope); err != nil || !bytes.Equal(back, canary) {
			report.fail("Crypto round-trip", "encrypt/decrypt mismatch")
		} else {
			report.pass("Crypto round-trip", "verified in memory")
		}
	}

	remote := vault.RemoteURL()
	if remote == "" {
		report.pass("Sync mode", "this machine only")
	} else if vault.RemoteHasCredentials(remote) {
		report.fail("Remote URL", "contains credentials; replace it with `memsync remote set <credential-free-url>`")
	} else if vault.RemoteReachable() {
		report.pass("Remote", vault.DisplayRemoteURL(remote)+" · reachable")
	} else {
		report.fail("Remote", vault.DisplayRemoteURL(remote)+" · authentication or network failed")
	}

	fmt.Println()
	if report.failures > 0 {
		fmt.Printf("Doctor found %d problem(s) and %d note(s).\n", report.failures, report.warnings)
		if !fix {
			fmt.Println("Follow the action above. `memsync doctor --fix` repairs memsync-owned local setup but does not override tool-wide security choices.")
		}
		return 1
	}
	if report.warnings > 0 {
		fmt.Printf("Core setup is healthy, with %d action/note(s) above.\n", report.warnings)
	} else {
		fmt.Println("Everything looks healthy.")
	}
	return 0
}

func repairSetup() error {
	bin, err := selfPath()
	if err != nil {
		return err
	}
	if _, _, err := loadOrCreateSetupKey(); err != nil {
		return err
	}
	if err := os.Chmod(paths.KeyPath(), 0o600); err != nil {
		return err
	}
	if _, _, err := device.LoadOrCreate(paths.DeviceIDPath()); err != nil {
		return err
	}
	if err := os.Chmod(paths.DeviceIDPath(), 0o600); err != nil {
		return err
	}
	if err := vault.Ensure(bin); err != nil {
		return err
	}
	tools := detect.All()
	if hasPresentTool(tools, "Codex CLI") {
		features := detect.DetectCodexFeatures()
		if features.CommandError != nil {
			return features.CommandError
		}
		if features.Hooks == detect.FeatureDisabled {
			if err := setCodexFeature("hooks", true); err != nil {
				return err
			}
		}
	}
	for _, t := range tools {
		if t.Present {
			if err := wire(t.Name, bin); err != nil {
				return err
			}
		}
	}
	return nil
}

func wiredFor(name string) bool {
	bin, err := selfPath()
	if err != nil {
		return false
	}
	switch name {
	case "Claude Code":
		w, _ := hooks.ClaudeWiredFor(bin)
		return w
	case "Codex CLI":
		w, _ := hooks.CodexWiredFor(bin)
		return w
	}
	return false
}

func relativeTime(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	d := time.Since(at)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return at.Local().Format("2006-01-02 15:04")
}

func fileMode(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Mode().Perm(), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
