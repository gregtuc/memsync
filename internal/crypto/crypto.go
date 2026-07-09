// Package crypto is memsync's encryption envelope: the only bytes that ever
// enter the git vault. Local files are never touched by this package.
//
// Envelope layout: magic(8) || nonce(12) || AES-256-GCM(ciphertext||tag).
// Target primitive is AES-256-GCM-SIV (nonce-misuse resistant); this scaffold
// ships AES-256-GCM with a random nonce and re-encrypt-on-change. Swapping the
// AEAD is isolated to seal/open below.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

var magic = []byte("MSYNCv1\x00")

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

// Encrypt wraps plaintext in the memsync envelope.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
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
