// Package dedup decides whether a freshly captured memory is really just the
// other tool's memory echoed back in different words. Exact hashing can't catch
// that (both tools paraphrase), so it compares SimHash fingerprints.
package dedup

import "github.com/gregtuc/memsync/internal/simhash"

// DefaultThreshold is the SimHash Hamming distance under which two memories are
// treated as the same fact. Kept deliberately low: measurements show distinct
// same-domain facts can sit around distance 14, so a tighter bound only catches
// reorderings and minor rewordings and avoids merging genuinely different notes.
// Heavier paraphrases are handled by the marker filter (courier.LooksSynced),
// not by widening this.
const DefaultThreshold = 8

// Fingerprint is a stored memory's origin plus its SimHash.
type Fingerprint struct {
	Origin string
	Hash   uint64
}

// IsEcho reports whether a candidate from candOrigin near-duplicates an existing
// memory from a DIFFERENT origin, i.e. a cross-tool echo that should be dropped
// rather than re-shipped. Same-origin updates are left alone.
func IsEcho(candOrigin string, candHash uint64, existing []Fingerprint, threshold int) bool {
	for _, e := range existing {
		if e.Origin == candOrigin {
			continue
		}
		if simhash.Similar(e.Hash, candHash, threshold) {
			return true
		}
	}
	return false
}
