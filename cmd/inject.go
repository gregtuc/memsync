package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregtuc/memsync/internal/activity"
	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/project"
	"github.com/gregtuc/memsync/internal/vault"
)

// injectMaxBytes caps the injected block so it never crowds out the tool's own memory.
const injectMaxBytes = 4000

// runInject is hook-invoked at SessionStart. It refreshes the vault, then emits
// every memory NOT written by the receiving tool (i.e. the other tool's, from
// any machine) as read-only context. Always exits 0 - never breaks a session.
func runInject(args []string) int {
	tool := flagValue(args, "--tool")
	ctx, cached, runErr := injectionContextStatus(tool, true)
	detail := "no shared memories yet"
	if ctx != "" {
		detail = "shared context delivered"
	}
	if cached {
		detail += " from cache; remote refresh failed"
	}
	_ = activity.Record(paths.DataDir(), tool, "inject", detail, runErr)
	if ctx == "" {
		return 0
	}
	// NOTE: hook output schema shared by both tools; verify per version.
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": ctx,
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return 0
	}
	fmt.Fprintln(os.Stdout, string(b))
	return 0
}

// injectionContext reads the vault and renders the memories that did NOT come
// from the receiving tool. Returns "" when there is nothing to show.
func injectionContext(tool string) string {
	ctx, _, _ := injectionContextStatus(tool, true)
	return ctx
}

func injectionContextFromVault(tool string, refresh bool) string {
	ctx, _, _ := injectionContextStatus(tool, refresh)
	return ctx
}

func injectionContextStatus(tool string, refresh bool) (string, bool, error) {
	if tool != "claude" && tool != "codex" {
		return "", false, fmt.Errorf("unknown injection tool %q", tool)
	}
	// A hook must never manufacture replacement identity state. A missing key or
	// device ID means setup needs repair, not that an empty new vault should be
	// created beside the established one.
	if _, err := crypto.LoadKey(paths.KeyPath()); err != nil {
		return "", false, fmt.Errorf("load encryption key: %w", err)
	}
	if _, err := device.Load(paths.DeviceIDPath()); err != nil {
		return "", false, fmt.Errorf("load device identity: %w", err)
	}
	var ctx string
	var cached bool
	err := vault.WithOperationLock(func() error {
		var err error
		ctx, cached, err = injectionContextLocked(tool, refresh)
		return err
	})
	return ctx, cached, err
}

// injectionContextLocked performs the pull/read/decrypt/render transaction.
// The caller must hold the vault operation lock.
func injectionContextLocked(tool string, refresh bool) (string, bool, error) {
	var refreshErr error
	if refresh {
		refreshErr = vault.Pull() // cached context remains available offline
	}
	key, err := crypto.LoadKey(paths.KeyPath())
	if err != nil {
		return "", false, err
	}
	dev, err := device.Load(paths.DeviceIDPath())
	if err != nil {
		return "", false, err
	}
	cwd, _ := os.Getwd()
	currentProject := project.Identify(cwd).ID
	files, err := vault.Records()
	if err != nil {
		return "", false, err
	}

	var mems []courier.Memory
	seen := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", false, fmt.Errorf("read encrypted record %s: %w", filepath.Base(f), err)
		}
		plain, err := crypto.Decrypt(key, b)
		if err != nil {
			return "", false, fmt.Errorf("decrypt record %s: %w", filepath.Base(f), err)
		}
		var r record
		if err := json.Unmarshal(plain, &r); err != nil {
			return "", false, fmt.Errorf("decode record %s: %w", filepath.Base(f), err)
		}
		if r.Origin == tool && (r.DeviceID == "" || r.DeviceID == dev.ID) {
			continue // don't echo this exact tool+machine's own memory back to it
		}
		if r.ProjectID != "" && r.ProjectID != currentProject {
			continue // project-scoped memory stays with the same repository
		}
		identity := r.Origin + "\x00" + r.Scope + "\x00" + r.Title + "\x00" + r.Body
		if seen[identity] {
			continue
		}
		seen[identity] = true
		mems = append(mems, courier.Memory{
			Origin:     r.Origin,
			Scope:      r.Scope,
			Title:      r.Title,
			Body:       r.Body,
			DeviceID:   r.DeviceID,
			DeviceName: r.DeviceName,
			ProjectID:  r.ProjectID,
			UpdatedAt:  r.UpdatedAt,
		})
	}
	ctx := courier.RenderContext(injectLabel(tool), mems, injectMaxBytes)
	if refreshErr != nil {
		if ctx == "" {
			return "", false, fmt.Errorf("refresh remote and no cached context is available: %w", refreshErr)
		}
		return ctx, true, nil
	}
	return ctx, false, nil
}

func injectLabel(tool string) string {
	return "your other tools and machines"
}
