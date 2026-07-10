//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package vault

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregtuc/memsync/internal/paths"
)

func TestOperationLockHelperProcess(t *testing.T) {
	if os.Getenv("MEMSYNC_OPERATION_LOCK_HELPER") != "1" {
		return
	}
	lock, err := acquireOperationLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	fmt.Println("locked")
	// The parent deliberately kills this process to model a hook crash. The
	// deferred release therefore does not run; the kernel must recover the lock.
	time.Sleep(time.Hour)
}

func TestOperationLockContendsAcrossProcessesAndRecoversAfterCrash(t *testing.T) {
	setupHome(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationLockHelperProcess$")
	cmd.Env = append(os.Environ(), "MEMSYNC_OPERATION_LOCK_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("lock helper did not become ready: line=%q err=%v", line, err)
	}

	oldTimeout := operationLockTimeout
	operationLockTimeout = 125 * time.Millisecond
	defer func() { operationLockTimeout = oldTimeout }()
	called := false
	err = WithOperationLock(func() error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "vault is busy") {
		t.Fatalf("contending process acquired lock unexpectedly: %v", err)
	}
	if called {
		t.Fatal("callback ran without acquiring the contended lock")
	}

	// SIGKILL skips every cleanup path. flock must still release the lock, and
	// the persistent owner file must not be mistaken for a live lease.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed helper exited successfully unexpectedly")
	}

	called = false
	if err := WithOperationLock(func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("stale crashed-process lock was not recovered: %v", err)
	}
	if !called {
		t.Fatal("callback did not run after stale lock recovery")
	}
}

func TestOperationLockReusesStaleOwnerFile(t *testing.T) {
	setupHome(t)
	if err := os.MkdirAll(paths.DataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.DataDir(), operationLockName)
	if err := os.WriteFile(path, []byte("pid=999999 acquired=1970-01-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WithOperationLock(func() error { return nil }); err != nil {
		t.Fatalf("stale owner file blocked acquisition: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pid=999999") || !strings.Contains(string(b), fmt.Sprintf("pid=%d", os.Getpid())) {
		t.Fatalf("stale owner metadata was not replaced: %q", b)
	}
}

func TestOperationLockReleasesAfterCallbackError(t *testing.T) {
	setupHome(t)
	want := fmt.Errorf("callback failed")
	if err := WithOperationLock(func() error { return want }); err != want {
		t.Fatalf("callback error not preserved: got %v want %v", err, want)
	}
	if err := WithOperationLock(func() error { return nil }); err != nil {
		t.Fatalf("lock was not released after callback error: %v", err)
	}
}
