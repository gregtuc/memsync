// Package crypto is memsync's encryption envelope: the only bytes that ever
// enter the git vault. Local files are never touched by this package.
//
// Envelope layout: magic(8) || nonce(12) || AES-256-GCM(ciphertext||tag).
//
// The nonce is derived from the content, not random: nonce = HMAC(key, plaintext).
// Two consequences, both wanted here:
//  1. A nonce is never reused across two different messages, so AES-256-GCM keeps
//     its guarantees (the failure mode of GCM is nonce reuse across distinct
//     plaintexts, which this construction makes impossible).
//  2. Identical content encrypts to identical bytes, so re-syncing an unchanged
//     record is a git no-op. The accepted cost is that someone who steals the
//     vault can tell whether two records are equal (documented in SECURITY.md).
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

var magic = []byte("MSYNCv2\x00")

// GenerateKey returns a fresh 256-bit key from the OS CSPRNG.
func GenerateKey() ([]byte, error) {
	k := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// LoadOrCreateKey reads the key at path, creating one (dir 0700, file 0600) if absent.
func LoadOrCreateKey(path string) ([]byte, bool, error) {
	if b, err := os.ReadFile(path); err == nil {
		k, derr := hex.DecodeString(string(bytes.TrimSpace(b)))
		if derr != nil {
			return nil, false, fmt.Errorf("key file %s is corrupt: %w", path, derr)
		}
		if len(k) != KeySize {
			return nil, false, fmt.Errorf("key file %s has wrong length %d", path, len(k))
		}
		return k, false, nil
	}
	k, err := GenerateKey()
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	enc := []byte(hex.EncodeToString(k) + "\n")
	if err := os.WriteFile(path, enc, 0o600); err != nil {
		return nil, false, err
	}
	return k, true, nil
}

// SaveKey writes key to path (dir 0700, file 0600), replacing any existing key.
// Used by `join` to adopt the vault's key received during pairing.
func SaveKey(path string, key []byte) error {
	if len(key) != KeySize {
		return fmt.Errorf("key must be %d bytes, got %d", KeySize, len(key))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600)
}

// Encrypt wraps plaintext in the memsync envelope.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := syntheticNonce(key, plaintext, aead.NonceSize())
	out := make([]byte, 0, len(magic)+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, magic...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, magic), nil
}

// Decrypt reverses Encrypt, verifying the envelope and tag.
func Decrypt(key, envelope []byte) ([]byte, error) {
	if !IsCiphertext(envelope) {
		return nil, errors.New("not a memsync envelope")
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	body := envelope[len(magic):]
	ns := aead.NonceSize()
	if len(body) < ns+aead.Overhead() {
		return nil, errors.New("envelope truncated")
	}
	nonce, ct := body[:ns], body[ns:]
	return aead.Open(nil, nonce, ct, magic)
}

// IsCiphertext reports whether data carries the memsync magic header. The vault
// guard uses this (plus a decrypt check) to refuse anything that isn't sealed.
func IsCiphertext(data []byte) bool {
	return len(data) >= len(magic) && bytes.Equal(data[:len(magic)], magic)
}

// syntheticNonce derives a deterministic nonce from the content so it is never
// reused across distinct plaintexts and identical content stays byte-identical.
func syntheticNonce(key, plaintext []byte, size int) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("memsync-nonce-v1"))
	m.Write(plaintext)
	return m.Sum(nil)[:size]
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
