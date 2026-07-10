package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/activity"
	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/detect"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/hooks"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func runStatus(args []string) int {
	fmt.Println("\nmemsync status")
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		if recovery, _ := keyRecoveryRequired(); recovery {
			fmt.Println("\n  ✗ The encryption key is missing, but an established vault is present.")
			fmt.Println("  Do not create a replacement key. Restore it from a paired laptop/backup,")
			fmt.Println("  or purge this local setup and pair the laptop again.")
		} else {
			fmt.Println("\n  memsync has not been initialized on this machine.")
			fmt.Println("  Run: memsync init")
		}
		return 0
	}
	if dev, err := device.Load(paths.DeviceIDPath()); err == nil {
		fmt.Printf("\n  This machine: %s (%s)\n", dev.Name, dev.ID[:8])
	}

	fmt.Println("\nLocal memory sources")
	for _, t := range detect.All() {
		if !t.Present {
			fmt.Printf("  - %-12s not installed\n", t.Name)
			continue
		}
		switch t.Name {
		case "Claude Code":
			mems, err := courier.CollectClaude()
			if err != nil {
				fmt.Printf("  ✗ %-12s source unreadable; run `memsync doctor`\n", t.Name)
			} else {
				fmt.Printf("  ✓ %-12s %d readable memories\n", t.Name, len(mems))
			}
		case "Codex CLI":
			features := detect.DetectCodexFeatures()
			mems, sourceErr := courier.CollectCodex()
			if features.CommandError != nil {
				fmt.Printf("  ✗ %-12s configuration/features check failed; run `memsync doctor`\n", t.Name)
			} else if features.Memories != detect.FeatureEnabled {
				fmt.Printf("  ! %-12s memories are off (Claude → Codex still works)\n", t.Name)
			} else if sourceErr != nil {
				fmt.Printf("  ✗ %-12s source unreadable; run `memsync doctor`\n", t.Name)
			} else if len(mems) == 0 {
				fmt.Printf("  ! %-12s enabled; waiting for background memory generation\n", t.Name)
			} else {
				fmt.Printf("  ✓ %-12s %d consolidated memory source\n", t.Name, len(mems))
			}
		}
	}

	stats, invalid := encryptedRecordStats(key)
	fmt.Println("\nShared encrypted vault")
	fmt.Printf("  ✓ %d records", stats["claude"]+stats["codex"]+stats["other"])
	if stats["claude"] > 0 || stats["codex"] > 0 {
		fmt.Printf(" · Claude %d · Codex %d", stats["claude"], stats["codex"])
	}
	fmt.Println()
	if invalid > 0 {
		fmt.Printf("  ✗ %d unreadable/corrupt records (run `memsync doctor`)\n", invalid)
	}
	remote := vault.RemoteURL()
	if remote == "" {
		fmt.Println("  ✓ Mode: this machine only")
	} else if vault.RemoteHasCredentials(remote) {
		fmt.Printf("  ✗ Remote: %s (embedded credentials are not allowed)\n", vault.DisplayRemoteURL(remote))
	} else {
		fmt.Printf("  ✓ Remote: %s\n", vault.DisplayRemoteURL(remote))
	}
	if last := vault.LastCommit(); last != "" {
		fmt.Printf("  ✓ Last vault change: %s\n", last)
	}

	fmt.Println("\nHook activity")
	for _, item := range []struct {
		tool  string
		label string
	}{
		{tool: "claude", label: "Claude Code"},
		{tool: "codex", label: "Codex"},
	} {
		configured := wiredFor(mapToolName(item.tool))
		if !configured {
			fmt.Printf("  ✗ %-12s not configured\n", item.label)
			continue
		}
		if item.tool == "claude" {
			if enabled, err := hooks.ClaudeHooksEnabled(); err == nil && !enabled {
				fmt.Printf("  ✗ %-12s disabled by disableAllHooks=true\n", item.label)
				continue
			}
		}
		inject, injectErr := activity.Read(paths.DataDir(), item.tool, "inject")
		capture, captureErr := activity.Read(paths.DataDir(), item.tool, "capture")
		if injectErr != nil && captureErr != nil {
			if item.tool == "codex" {
				fmt.Printf("  ! %-12s configured; not observed (review/trust with `/hooks`)\n", item.label)
			} else {
				fmt.Printf("  ! %-12s configured; restart once to verify\n", item.label)
			}
			continue
		}
		latest := inject
		if capture.At.After(latest.At) {
			latest = capture
		}
		state := "✓"
		if !latest.OK {
			state = "✗"
		}
		fmt.Printf("  %s %-12s observed %s\n", state, item.label, relativeTime(latest.At))
	}

	if remote == "" {
		fmt.Println("\nAdd another laptop: `memsync remote create`, then follow the printed steps.")
	} else {
		fmt.Println("\nAdd another laptop: run `memsync join` there, then `memsync pair` here.")
	}
	return 0
}

func encryptedRecordStats(key []byte) (map[string]int, int) {
	stats := map[string]int{"claude": 0, "codex": 0, "other": 0}
	files, err := vault.Records()
	if err != nil {
		return stats, 0
	}
	invalid := 0
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			invalid++
			continue
		}
		plain, err := crypto.Decrypt(key, b)
		if err != nil {
			invalid++
			continue
		}
		var r record
		if json.Unmarshal(plain, &r) != nil {
			invalid++
			continue
		}
		if _, ok := stats[r.Origin]; ok {
			stats[r.Origin]++
		} else {
			stats["other"]++
		}
	}
	return stats, invalid
}

func mapToolName(tool string) string {
	if tool == "codex" {
		return "Codex CLI"
	}
	return "Claude Code"
}
