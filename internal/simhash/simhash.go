// Package simhash gives cheap, offline near-duplicate detection over text.
// It exists so a memory that round-trips between tools (and comes back reworded)
// is recognized as the same fact instead of piling up as a fresh copy. No
// embeddings, no model, no network.
package simhash

import (
	"hash/fnv"
	"math/bits"
	"strings"
	"unicode"
)

// Hash returns the 64-bit SimHash of text. Similar text yields a small Hamming
// distance; unrelated text yields a large one.
func Hash(text string) uint64 {
	toks := tokenize(text)
	if len(toks) == 0 {
		return 0
	}
	var col [64]int
	for _, tok := range toks {
		h := fnv64a(tok)
		for i := 0; i < 64; i++ {
			if h&(uint64(1)<<uint(i)) != 0 {
				col[i]++
			} else {
				col[i]--
			}
		}
	}
	var out uint64
	for i := 0; i < 64; i++ {
		if col[i] > 0 {
			out |= uint64(1) << uint(i)
		}
	}
	return out
}

// Distance is the Hamming distance between two hashes (0..64).
func Distance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// Similar reports whether a and b are within threshold bits. Empty text (hash 0)
// is only similar to other empty text.
func Similar(a, b uint64, threshold int) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	return Distance(a, b) <= threshold
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var toks []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			toks = append(toks, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

func fnv64a(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
