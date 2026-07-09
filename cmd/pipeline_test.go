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
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/simhash"
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

func TestInjectionContextFiltersByOrigin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	key, _, err := crypto.LoadOrCreateKey(paths.KeyPath())
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
