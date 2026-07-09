package simhash

import "testing"

const base = "prod-yellow US1 is still in bring-up, hold customer-services for now"

func TestIdenticalIsZero(t *testing.T) {
	if d := Distance(Hash(base), Hash(base)); d != 0 {
		t.Fatalf("identical text distance = %d, want 0", d)
	}
}

func TestParaphraseCloserThanUnrelated(t *testing.T) {
	paraphrase := "for now, hold customer-services because prod-yellow us1 is still in bringup"
	unrelated := "the quick brown fox jumps over the lazy dog near the river bank"

	dp := Distance(Hash(base), Hash(paraphrase))
	du := Distance(Hash(base), Hash(unrelated))
	t.Logf("paraphrase distance=%d unrelated distance=%d", dp, du)

	if dp >= du {
		t.Fatalf("paraphrase (%d) should be closer than unrelated (%d)", dp, du)
	}
	if !Similar(Hash(base), Hash(paraphrase), 16) {
		t.Fatalf("paraphrase not similar within threshold (distance %d)", dp)
	}
	if Similar(Hash(base), Hash(unrelated), 16) {
		t.Fatalf("unrelated wrongly flagged similar (distance %d)", du)
	}
}

func TestEmptyOnlySimilarToEmpty(t *testing.T) {
	if !Similar(Hash(""), Hash("  ,. "), 16) {
		t.Fatal("two empties should be similar")
	}
	if Similar(Hash(""), Hash(base), 16) {
		t.Fatal("empty should not be similar to real text")
	}
}
