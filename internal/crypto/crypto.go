// Package crypto is memsync's encryption envelope: the only bytes that ever
// enter the git vault. Local files are never touched by this package.
//
// Envelope layout: magic(8) || random nonce(12) || AES-256-GCM(ciphertext||tag).
// Callers avoid git churn by decrypting an existing record and skipping the
// rewrite when its plaintext is unchanged; encryption itself stays conventional
// and randomized.
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
	if k, err := LoadKey(path); err == nil {
		return k, false, nil
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	k, err := GenerateKey()
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".key-create-*.tmp")
	if err != nil {
		return nil, false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return nil, false, err
	}
	if _, err := tmp.WriteString(hex.EncodeToString(k) + "\n"); err != nil {
		_ = tmp.Close()
		return nil, false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, false, err
	}
	if err := tmp.Close(); err != nil {
		return nil, false, err
	}
	// Linking a complete temporary file is an atomic create-if-absent. A
	// concurrent bootstrap can never observe a partially written key and can
	// never replace the winner with a different key.
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) {
			winner, loadErr := LoadKey(path)
			return winner, false, loadErr
		}
		return nil, false, err
	}
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(path, 0o600)
	return k, true, nil
}

// LoadKey reads and validates an existing key without creating one.
func LoadKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	k, err := hex.DecodeString(string(bytes.TrimSpace(b)))
	if err != nil {
		return nil, fmt.Errorf("key file %s is corrupt: %w", path, err)
	}
	if len(k) != KeySize {
		return nil, fmt.Errorf("key file %s has wrong length %d", path, len(k))
	}
	return k, nil
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
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".key-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	return os.Chmod(path, 0o600)
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
