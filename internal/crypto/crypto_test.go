package crypto

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("prod-yellow US1 still in bring-up")
	env, err := Encrypt(key, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !IsCiphertext(env) {
		t.Fatal("envelope missing magic header")
	}
	if bytes.Contains(env, msg) {
		t.Fatal("plaintext leaked into the envelope")
	}
	back, err := Decrypt(key, env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, msg) {
		t.Fatalf("round-trip mismatch: %q != %q", back, msg)
	}
}

// Deterministic output is what keeps the vault churn-free: encrypting the same
// content twice must produce identical bytes (a git no-op on re-sync).
func TestDeterministic(t *testing.T) {
	key, _ := GenerateKey()
	msg := []byte("use the env-setup helper for staging")
	a, err := Encrypt(key, msg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(key, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same key+plaintext produced different ciphertext (would churn the vault)")
	}
}

func TestDifferentPlaintextDiffersInNonceAndBody(t *testing.T) {
	key, _ := GenerateKey()
	a, _ := Encrypt(key, []byte("alpha"))
	b, _ := Encrypt(key, []byte("beta"))
	if bytes.Equal(a, b) {
		t.Fatal("different plaintext produced identical ciphertext")
	}
	// nonce lives right after the 8-byte magic; distinct content must not reuse it.
	if bytes.Equal(a[8:20], b[8:20]) {
		t.Fatal("distinct plaintexts reused a nonce")
	}
}

func TestWrongKeyFails(t *testing.T) {
	k1, _ := GenerateKey()
	k2, _ := GenerateKey()
	env, _ := Encrypt(k1, []byte("secret"))
	if _, err := Decrypt(k2, env); err == nil {
		t.Fatal("decrypt succeeded under the wrong key")
	}
}

func TestTamperDetected(t *testing.T) {
	key, _ := GenerateKey()
	env, _ := Encrypt(key, []byte("do not tamper with me"))
	env[len(env)-1] ^= 0x01 // flip a bit in the tag
	if _, err := Decrypt(key, env); err == nil {
		t.Fatal("tampered ciphertext decrypted without error")
	}
}

func TestIsCiphertextRejectsPlaintext(t *testing.T) {
	if IsCiphertext([]byte("this is plaintext")) {
		t.Fatal("plaintext accepted as ciphertext")
	}
	if IsCiphertext(nil) {
		t.Fatal("nil accepted as ciphertext")
	}
}

func TestSaveLoadKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/key"
	orig, _ := GenerateKey()
	if err := SaveKey(path, orig); err != nil {
		t.Fatal(err)
	}
	loaded, created, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("LoadOrCreateKey reported created for an existing key")
	}
	if !bytes.Equal(orig, loaded) {
		t.Fatal("loaded key does not match saved key")
	}
}
