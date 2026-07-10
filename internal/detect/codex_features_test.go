package detect

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCodexFeatureList(t *testing.T) {
	output := `apply_patch_freeform                 removed            false
hooks                                stable             true
memories                             under development  false
malformed                            true-ish
`
	got := parseCodexFeatureList(output)
	if got.Hooks != FeatureEnabled {
		t.Fatalf("hooks = %s, want enabled", got.Hooks)
	}
	if got.Memories != FeatureDisabled {
		t.Fatalf("memories = %s, want disabled", got.Memories)
	}
}

func TestParseCodexFeatureListMissingValuesAreUnknown(t *testing.T) {
	got := parseCodexFeatureList("hooks stable maybe\n")
	if got.Hooks != FeatureUnknown || got.Memories != FeatureUnknown {
		t.Fatalf("got %+v, want both unknown", got)
	}
}

func TestDetectCodexFeaturesFallsBackToExplicitHooksDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	config := `# hooks = true
[unrelated]
hooks = true

[features]
hooks = false # intentionally disabled
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := detectCodexFeatures("", errors.New("features list unsupported"), path)
	if got.CommandError == nil {
		t.Fatal("feature command failure was hidden")
	}
	if got.Hooks != FeatureDisabled {
		t.Fatalf("hooks = %s, want disabled", got.Hooks)
	}
	if got.Memories != FeatureUnknown {
		t.Fatalf("memories = %s, want unknown", got.Memories)
	}
}

func TestDetectCodexFeaturesCombinesListWithDottedHooksFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("features.hooks = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := detectCodexFeatures("memories experimental true\n", nil, path)
	if got.Memories != FeatureEnabled || got.Hooks != FeatureDisabled {
		t.Fatalf("got %+v, want memories enabled and hooks disabled", got)
	}
}

func TestFeatureListEffectiveStateWinsOverConfigFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[features]\nhooks = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := detectCodexFeatures("hooks stable true\nmemories experimental false\n", nil, path)
	if got.Hooks != FeatureEnabled || got.Memories != FeatureDisabled {
		t.Fatalf("got %+v", got)
	}
}

func TestExplicitHooksConfigParsing(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   FeatureState
	}{
		{name: "table disabled", config: "[features]\nhooks = false\n", want: FeatureDisabled},
		{name: "table enabled", config: "[features]\nhooks = true\n", want: FeatureEnabled},
		{name: "legacy alias", config: "[features]\ncodex_hooks = false\n", want: FeatureDisabled},
		{name: "canonical wins", config: "[features]\ncodex_hooks = false\nhooks = true\n", want: FeatureEnabled},
		{name: "dotted key", config: "features.hooks = false\n[other]\nx = 1\n", want: FeatureDisabled},
		{name: "comment only", config: "# [features]\n# hooks = false\n", want: FeatureUnknown},
		{name: "wrong table", config: "[other]\nhooks = false\n", want: FeatureUnknown},
		{name: "dotted key inside array table", config: "[[other]]\nfeatures.hooks = false\n", want: FeatureUnknown},
		{name: "string is not bool", config: "[features]\nhooks = \"false\"\n", want: FeatureUnknown},
		{name: "hash inside string", config: "name = \"# not a comment\"\n[features]\nhooks = false\n", want: FeatureDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseExplicitHooksFeatureState(tt.config); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFeatureStateString(t *testing.T) {
	if FeatureUnknown.String() != "unknown" || FeatureDisabled.String() != "disabled" || FeatureEnabled.String() != "enabled" {
		t.Fatal("unexpected FeatureState string")
	}
}
