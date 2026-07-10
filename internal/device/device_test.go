package device

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadOrCreateIsStableAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "device-id")
	first, created, err := LoadOrCreate(path)
	if err != nil || !created {
		t.Fatalf("first load: created=%v err=%v", created, err)
	}
	second, created, err := LoadOrCreate(path)
	if err != nil || created {
		t.Fatalf("second load: created=%v err=%v", created, err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("device id is not stable: %q != %q", first.ID, second.ID)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("device id mode = %o, want 600", got)
	}
}

func TestLoadRejectsCorruptID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-id")
	if err := os.WriteFile(path, []byte("not-an-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt device id was accepted")
	}
}

func TestConcurrentLoadOrCreateChoosesOneCompleteID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "device-id")
	const workers = 24
	type result struct {
		info    Info
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
			info, created, err := LoadOrCreate(path)
			results <- result{info: info, created: created, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	winner := ""
	created := 0
	for got := range results {
		if got.err != nil {
			t.Fatalf("concurrent create failed: %v", got.err)
		}
		if winner == "" {
			winner = got.info.ID
		}
		if winner != got.info.ID {
			t.Fatal("concurrent bootstraps returned different device IDs")
		}
		if got.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created count = %d, want exactly 1", created)
	}
}
