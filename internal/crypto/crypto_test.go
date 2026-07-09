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

func TestWrongKeyFails(t *testing.T) {
	k1, _ := GenerateKey()
	k2, _ := GenerateKey()
	env, err := Encrypt(k1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(k2, env); err == nil {
		t.Fatal("decrypt succeeded under the wrong key")
	}
}

func TestIsCiphertextRejectsPlaintext(t *testing.T) {
	if IsCiphertext([]byte("this is plaintext")) {
		t.Fatal("plaintext accepted as ciphertext")
	}
}
