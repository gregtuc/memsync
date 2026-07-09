package dedup

import (
	"testing"

	"github.com/gregtuc/memsync/internal/simhash"
)

const claudeNote = "prod-yellow US1 is still in bring-up, hold customer-services"

func TestCrossOriginReorderEchoIsDropped(t *testing.T) {
	existing := []Fingerprint{{Origin: "claude", Hash: simhash.Hash(claudeNote)}}
	// Codex surfaces the same fact reordered/reworded lightly: an echo.
	echo := simhash.Hash("hold customer-services, prod-yellow US1 is still in bring-up")
	if !IsEcho("codex", echo, existing, DefaultThreshold) {
		t.Fatal("a light cross-tool echo was not detected")
	}
}

func TestUnrelatedIsKept(t *testing.T) {
	existing := []Fingerprint{{Origin: "claude", Hash: simhash.Hash(claudeNote)}}
	fresh := simhash.Hash("use the env-setup helper to bring up a staging environment")
	if IsEcho("codex", fresh, existing, DefaultThreshold) {
		t.Fatal("unrelated memory wrongly dropped as an echo")
	}
}

// Two genuinely different facts in the same domain must never be merged.
func TestDistinctSameDomainNotMerged(t *testing.T) {
	existing := []Fingerprint{{Origin: "claude", Hash: simhash.Hash("use redis for the caching layer")}}
	other := simhash.Hash("use redis for user sessions")
	if IsEcho("codex", other, existing, DefaultThreshold) {
		t.Fatal("distinct same-domain facts were wrongly deduped")
	}
}

func TestSameOriginIsNotDeduped(t *testing.T) {
	h := simhash.Hash(claudeNote)
	existing := []Fingerprint{{Origin: "claude", Hash: h}}
	if IsEcho("claude", h, existing, DefaultThreshold) {
		t.Fatal("same-origin memory should be updated in place, not treated as an echo")
	}
}
