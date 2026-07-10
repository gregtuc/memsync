package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
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

func TestEncryptionIsRandomized(t *testing.T) {
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
	if bytes.Equal(a, b) {
		t.Fatal("same key+plaintext produced identical ciphertext")
	}
	for _, env := range [][]byte{a, b} {
		plain, err := Decrypt(key, env)
		if err != nil || !bytes.Equal(plain, msg) {
			t.Fatalf("randomized envelope did not round-trip: %q, %v", plain, err)
		}
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

func TestLoadKeyDoesNotCreateMissingKey(t *testing.T) {
	path := t.TempDir() + "/missing/key"
	if _, err := LoadKey(path); err == nil {
		t.Fatal("missing key unexpectedly loaded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("LoadKey created or touched the missing key: %v", err)
	}
}

func TestConcurrentLoadOrCreateChoosesOneCompleteKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "key")
	const workers = 24
	type result struct {
		key     []byte
		created bool
		err     error
	}
	results := make(chan result, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			key, created, err := LoadOrCreateKey(path)
			results <- result{key: key, created: created, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var winner []byte
	created := 0
	for got := range results {
		if got.err != nil {
			t.Fatalf("concurrent create failed: %v", got.err)
		}
		if winner == nil {
			winner = got.key
		}
		if !bytes.Equal(winner, got.key) {
			t.Fatal("concurrent bootstraps returned different keys")
		}
		if got.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created count = %d, want exactly 1", created)
	}
}
