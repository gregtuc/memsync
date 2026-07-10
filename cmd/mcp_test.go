package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregtuc/memsync/internal/courier"
	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/device"
	"github.com/gregtuc/memsync/internal/paths"
)

func mcpIsolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
}

func mcpSeedVault(t *testing.T, records ...record) {
	t.Helper()
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
	for _, r := range records {
		b, _ := json.Marshal(r)
		e, _ := crypto.Encrypt(key, b)
		if err := os.WriteFile(filepath.Join(paths.VaultDir(), recordName(r)), e, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMemoryGetReturnsForeignBodyWithProvenanceAndHidesOwn(t *testing.T) {
	mcpIsolateHome(t)
	mcpSeedVault(t,
		record{Origin: "claude", Scope: "repo", Title: "deploy", Body: "hold customer traffic during the roll"},
		record{Origin: "codex", Scope: "global", Title: "env", Body: "codex's own note"},
	)

	out, err := memoryGet("codex", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hold customer traffic during the roll") {
		t.Fatalf("full body missing: %q", out)
	}
	if !strings.Contains(out, "[synced-from:claude]") {
		t.Fatalf("provenance header missing (needed so a wholesale copy is not re-captured): %q", out)
	}

	// Codex must never recall its own memory back through the courier.
	own, _ := memoryGet("codex", "env")
	if !strings.Contains(own, "No memory named") {
		t.Fatalf("codex recall returned its own memory: %q", own)
	}
}

func TestMemorySearchRanksRelevantFirstHidesOwnAndIsGlobal(t *testing.T) {
	mcpIsolateHome(t)
	mcpSeedVault(t,
		record{Origin: "claude", ProjectID: "proj-A", Scope: "repoA", Title: "imds", Body: "IMDS deny rollout on yellow clusters", UpdatedAt: 3},
		record{Origin: "claude", ProjectID: "proj-B", Scope: "repoB", Title: "redis", Body: "redis-ha deadlock recovery steps", UpdatedAt: 2},
		record{Origin: "codex", Scope: "global", Title: "own", Body: "codex own note mentioning imds", UpdatedAt: 1},
	)

	out, err := memorySearch("codex", "imds rollout")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "imds") {
		t.Fatalf("search missed the relevant claude memory: %q", out)
	}
	if strings.Contains(out, "own note mentioning imds") {
		t.Fatalf("search leaked codex's own memory: %q", out)
	}

	// Recall is deliberately global: a memory bound to a different project is
	// still reachable on demand (unlike SessionStart injection).
	crossProject, _ := memorySearch("codex", "redis deadlock")
	if !strings.Contains(crossProject, "redis") {
		t.Fatalf("cross-project memory not reachable via recall: %q", crossProject)
	}
}

func TestRankMemoriesOrdersByScoreThenRecency(t *testing.T) {
	mems := []courier.Memory{
		{Title: "a", Body: "alpha beta gamma", UpdatedAt: 1},
		{Title: "b", Body: "alpha beta", UpdatedAt: 2},
		{Title: "c", Body: "unrelated words", UpdatedAt: 3},
	}
	got := rankMemories(mems, "alpha beta gamma")
	if len(got) != 2 {
		t.Fatalf("want 2 scored hits (c has no overlap), got %d: %+v", len(got), got)
	}
	if got[0].Title != "a" {
		t.Fatalf("highest keyword overlap should rank first, got %q", got[0].Title)
	}
	all := rankMemories(mems, "")
	if len(all) != 3 || all[0].Title != "c" {
		t.Fatalf("empty query should list all, newest first: %+v", all)
	}
}

func TestTokenizeDropsShortTokensAndPunctuation(t *testing.T) {
	set := tokenizeSet("IMDS-deny, on EKS (yellow)!")
	for _, want := range []string{"imds", "deny", "eks", "yellow"} {
		if !set[want] {
			t.Fatalf("missing token %q in %v", want, set)
		}
	}
	if set["on"] {
		t.Fatal("short token 'on' should be dropped")
	}
}
