// Package pair implements the public-key sealed handshake that carries the vault
// key to a second machine. Nothing copied between machines is a secret: the
// invite is a public key, and the reply is sealed so only the joining machine's
// private key (which never leaves it) can open it.
//
// Scheme: ephemeral-static X25519 ECDH → HKDF(SHA-256) → AES-256-GCM. Stdlib only.
package pair

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	invitePrefix = "msync-invite-"
	replyPrefix  = "msync-reply-"
	info         = "memsync-pair-v1"
)

// Identity is a joining machine's throwaway keypair.
type Identity struct{ priv *ecdh.PrivateKey }

// NewIdentity generates a fresh X25519 identity for one pairing.
func NewIdentity() (*Identity, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{priv}, nil
}

// Invite is the public token the joining machine shares (safe over any channel).
func (id *Identity) Invite() string {
	return invitePrefix + b64(id.priv.PublicKey().Bytes())
}

// InviteFingerprint is a short human-comparison code. The invite is public but
// must still be authentic: comparing this code prevents a substituted invite
// from receiving the sealed vault key.
func InviteFingerprint(invite string) (string, error) {
	raw, err := decode(invite, invitePrefix)
	if err != nil {
		return "", err
	}
	if _, err := ecdh.X25519().NewPublicKey(raw); err != nil {
		return "", fmt.Errorf("invalid invite: %w", err)
	}
	sum := sha256.Sum256(raw)
	hex := strings.ToUpper(fmt.Sprintf("%x", sum[:4]))
	return hex[:4] + "-" + hex[4:], nil
}

// Seal wraps payload to the invite's public key, returning a reply token.
func Seal(invite string, payload []byte) (string, error) {
	recipBytes, err := decode(invite, invitePrefix)
	if err != nil {
		return "", err
	}
	recipient, err := ecdh.X25519().NewPublicKey(recipBytes)
	if err != nil {
		return "", fmt.Errorf("invalid invite: %w", err)
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	shared, err := eph.ECDH(recipient)
	if err != nil {
		return "", err
	}
	ephBytes := eph.PublicKey().Bytes()
	aead, err := newGCM(kdf(shared, ephBytes, recipBytes))
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, payload, aad(ephBytes, recipBytes))
	out := append(append(append([]byte{}, ephBytes...), nonce...), ct...)
	return replyPrefix + b64(out), nil
}

// Open reverses Seal with this identity's private key.
func (id *Identity) Open(reply string) ([]byte, error) {
	raw, err := decode(reply, replyPrefix)
	if err != nil {
		return nil, err
	}
	const pubLen = 32
	if len(raw) < pubLen+12 {
		return nil, errors.New("reply token too short")
	}
	ephBytes := raw[:pubLen]
	rest := raw[pubLen:]
	eph, err := ecdh.X25519().NewPublicKey(ephBytes)
	if err != nil {
		return nil, err
	}
	shared, err := id.priv.ECDH(eph)
	if err != nil {
		return nil, err
	}
	myBytes := id.priv.PublicKey().Bytes()
	aead, err := newGCM(kdf(shared, ephBytes, myBytes))
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(rest) < ns {
		return nil, errors.New("reply token truncated")
	}
	return aead.Open(nil, rest[:ns], rest[ns:], aad(ephBytes, myBytes))
}

// kdf derives a 32-byte AES-256 key from the ECDH secret (HKDF-SHA256, one block).
func kdf(shared, ephPub, recipPub []byte) []byte {
	salt := append(append([]byte{}, ephPub...), recipPub...)
	prk := mac(salt, shared)
	return mac(prk, append([]byte(info), 0x01))
}

func aad(ephPub, recipPub []byte) []byte {
	return append(append([]byte(info), ephPub...), recipPub...)
}

func mac(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func decode(tok, prefix string) ([]byte, error) {
	tok = strings.TrimSpace(tok)
	if !strings.HasPrefix(tok, prefix) {
		return nil, fmt.Errorf("expected a %s… token", prefix)
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tok, prefix))
}
