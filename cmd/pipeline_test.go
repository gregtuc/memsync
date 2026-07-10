package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/dedup"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/project"
	"github.com/gregtuc/memsync/internal/simhash"
	"github.com/gregtuc/memsync/internal/vault"
)

func TestSelectForWriteDropsMarkerAndEchoKeepsDistinct(t *testing.T) {
	seen := []dedup.Fingerprint{{Origin: "claude", Hash: simhash.Hash("hold customer services for now")}}
	mems := []courier.Memory{
		{Origin: "codex", Title: "copied", Body: "a note [synced-from:claude] pasted in"},
		{Origin: "codex", Title: "echo", Body: "for now, hold customer services"},
		{Origin: "codex", Title: "distinct", Body: "use redis for user sessions in the gateway"},
	}
	out := selectForWrite(mems, seen)
	if len(out) != 1 || out[0].Title != "distinct" {
		t.Fatalf("selectForWrite kept the wrong set: %+v", out)
	}
}

func TestSelectForWriteKeepsSameOriginUpdate(t *testing.T) {
	body := "prod-yellow US1 is still in bring-up"
	seen := []dedup.Fingerprint{{Origin: "claude", Hash: simhash.Hash(body)}}
	out := selectForWrite([]courier.Memory{{Origin: "claude", Title: "deploy", Body: body}}, seen)
	if len(out) != 1 {
		t.Fatalf("a same-origin update must be kept, got %d", len(out))
	}
}

func TestInjectionStatusDoesNotCallMissingStateHealthy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	if ctx, cached, err := injectionContextStatus("codex", true); err == nil || ctx != "" || cached {
		t.Fatalf("missing key/device reported healthy: ctx=%q cached=%v err=%v", ctx, cached, err)
	}
}

func TestInjectionContextFiltersByOrigin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := device.LoadOrCreate(paths.DeviceIDPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.VaultDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(r record) {
		b, _ := json.Marshal(r)
		e, _ := crypto.Encrypt(key, b)
		if err := os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), e, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(record{Origin: "claude", Scope: "repo", Title: "deploy", Body: "hold customer traffic"})
	write(record{Origin: "codex", Scope: "global", Title: "env", Body: "use the setup helper"})

	ctx := injectionContext("codex")
	if !strings.Contains(ctx, "hold customer traffic") {
		t.Fatalf("codex should see claude's memory, got: %q", ctx)
	}
	if strings.Contains(ctx, "use the setup helper") {
		t.Fatal("codex must not be shown its own memory")
	}

	ctx = injectionContext("claude")
	if !strings.Contains(ctx, "use the setup helper") || strings.Contains(ctx, "hold customer traffic") {
		t.Fatalf("claude injection is wrong: %q", ctx)
	}
}

func TestInjectionContextIncludesSameToolFromAnotherDevice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	local, _, err := device.LoadOrCreate(paths.DeviceIDPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.VaultDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(r record) {
		b, _ := json.Marshal(r)
		e, _ := crypto.Encrypt(key, b)
		if err := os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), e, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(record{Origin: "claude", DeviceID: local.ID, Scope: "repo", Title: "local", Body: "local-only canary"})
	write(record{Origin: "claude", DeviceID: "11111111111111111111111111111111", DeviceName: "other-mac", Scope: "repo", Title: "remote", Body: "remote canary"})

	ctx := injectionContext("claude")
	if !strings.Contains(ctx, "remote canary") || !strings.Contains(ctx, "other-mac") {
		t.Fatalf("remote same-tool memory missing: %q", ctx)
	}
	if strings.Contains(ctx, "local-only canary") {
		t.Fatalf("local same-tool memory echoed back: %q", ctx)
	}
}

func TestInjectionContextIncludesRemoteCodexOnCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	key, _, _ := crypto.LoadOrCreateKey(paths.KeyPath())
	local, _, _ := device.LoadOrCreate(paths.DeviceIDPath())
	os.MkdirAll(paths.VaultDir(), 0o755)
	for _, r := range []record{
		{Origin: "codex", DeviceID: local.ID, Scope: "global", Title: "local", Body: "local codex canary"},
		{Origin: "codex", DeviceID: "22222222222222222222222222222222", DeviceName: "travel-mac", Scope: "global", Title: "remote", Body: "remote codex canary"},
	} {
		b, _ := json.Marshal(r)
		e, _ := crypto.Encrypt(key, b)
		os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), e, 0o644)
	}
	ctx := injectionContext("codex")
	if !strings.Contains(ctx, "remote codex canary") || strings.Contains(ctx, "local codex canary") {
		t.Fatalf("Codex-to-Codex device filtering is wrong: %q", ctx)
	}
}

func TestRecordNameIsDeviceScoped(t *testing.T) {
	a := record{Origin: "claude", DeviceID: "a", Scope: "repo", Title: "deploy"}
	b := record{Origin: "claude", DeviceID: "b", Scope: "repo", Title: "deploy"}
	if recordName(a) == recordName(b) {
		t.Fatal("records from different devices collided")
	}
}

func TestInjectionContextFiltersOtherProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	cwd := filepath.Join(home, "current-project")
	os.MkdirAll(cwd, 0o755)
	oldCWD, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldCWD)

	key, _, _ := crypto.LoadOrCreateKey(paths.KeyPath())
	local, _, _ := device.LoadOrCreate(paths.DeviceIDPath())
	os.MkdirAll(paths.VaultDir(), 0o755)
	write := func(r record) {
		b, _ := json.Marshal(r)
		e, _ := crypto.Encrypt(key, b)
		os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), e, 0o644)
	}
	write(record{Origin: "claude", DeviceID: "remote-a", ProjectID: project.Identify(cwd).ID, Scope: "current", Title: "same", Body: "same project canary"})
	write(record{Origin: "claude", DeviceID: "remote-b", ProjectID: "different-project-id", Scope: "other", Title: "other", Body: "other project secret"})
	write(record{Origin: "codex", DeviceID: local.ID, Scope: "global", Title: "global", Body: "global codex context"})

	ctx := injectionContext("codex")
	if !strings.Contains(ctx, "same project canary") || strings.Contains(ctx, "other project secret") {
		t.Fatalf("project filtering is wrong: %q", ctx)
	}
}

func TestRemoveStaleRecordsIsDeviceAndProjectScoped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	key, _, _ := crypto.LoadOrCreateKey(paths.KeyPath())
	os.MkdirAll(paths.VaultDir(), 0o755)
	records := []record{
		{Origin: "claude", DeviceID: "mine", ProjectID: "project-a", Scope: "a", Title: "delete", Body: "gone"},
		{Origin: "claude", DeviceID: "mine", ProjectID: "project-b", Scope: "b", Title: "keep-other-project", Body: "keep"},
		{Origin: "claude", DeviceID: "theirs", ProjectID: "project-a", Scope: "a", Title: "keep-other-device", Body: "keep"},
	}
	for _, r := range records {
		plain, _ := json.Marshal(r)
		env, _ := crypto.Encrypt(key, plain)
		os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), env, 0o644)
	}
	removed, err := removeStaleRecords(key, "claude", "mine", "project-a", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d records, want 1", removed)
	}
	files, _ := vault.Records()
	if len(files) != 2 {
		t.Fatalf("stale reconciliation removed unrelated records; %d remain", len(files))
	}
}

func TestRemoveStaleRecordsRetiresOnlyAuthoritativeLegacyScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	key, _, _ := crypto.LoadOrCreateKey(paths.KeyPath())
	os.MkdirAll(paths.VaultDir(), 0o755)
	for _, r := range []record{
		{Origin: "claude", Scope: "-current-checkout", Title: "old-current", Body: "retire me"},
		{Origin: "claude", Scope: "-other-checkout", Title: "old-other", Body: "leave for its owner"},
	} {
		plain, _ := json.Marshal(r)
		envelope, _ := crypto.Encrypt(key, plain)
		os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), envelope, 0o600)
	}
	removed, err := removeStaleRecords(key, "claude", "new-device", "new-project", map[string]bool{}, map[string]bool{"-current-checkout": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("retired %d legacy records, want 1", removed)
	}
	files, _ := vault.Records()
	if len(files) != 1 {
		t.Fatalf("legacy migration removed an unrelated scope; %d records remain", len(files))
	}
}
