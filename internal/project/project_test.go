package project

import "testing"

func TestNormalizeRemoteStripsCredentialsAndUnifiesForms(t *testing.T) {
	want := "github.com/acme/widgets"
	for _, raw := range []string{
		"git@github.com:Acme/Widgets.git",
		"https://token@github.com/Acme/Widgets.git",
		"ssh://git@github.com/Acme/Widgets.git",
	} {
		got := normalizeRemote(raw)
		if got != want {
			t.Fatalf("normalizeRemote(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeRemotePreservesCaseForGenericHosts(t *testing.T) {
	upper := normalizeRemote("ssh://git@example.com/Team/Repo.git")
	lower := normalizeRemote("ssh://git@example.com/team/repo.git")
	if upper == lower {
		t.Fatalf("case-sensitive remote paths collided: %q", upper)
	}
}
