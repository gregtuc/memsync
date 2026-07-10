//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregtuc/memsync/internal/paths"
)

const operationLockName = "vault-operation.lock"

// These are variables so contention and timeout behavior can be tested without
// making the suite wait for production deadlines.
var (
	operationLockTimeout      = 15 * time.Second
	operationLockPollInterval = 25 * time.Millisecond
)

type operationLock struct {
	file *os.File
}

// WithOperationLock serializes a complete vault mutation transaction across
// Claude/Codex hook processes on this machine. The callback should encompass
// pull, local record writes/removals, commit, and push as one indivisible unit.
//
// The wait is bounded. The kernel releases the advisory lock if a process exits
// or crashes, so stale owner metadata never requires unsafe lock-file deletion.
func WithOperationLock(fn func() error) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), operationLockTimeout)
	defer cancel()
	lock, err := acquireOperationLock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.release(); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()
	return fn()
}

func acquireOperationLock(ctx context.Context) (*operationLock, error) {
	if err := os.MkdirAll(paths.DataDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create vault lock directory: %w", err)
	}
	path := filepath.Join(paths.DataDir(), operationLockName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open vault operation lock: %w", err)
	}
	_ = file.Chmod(0o600)

	ticker := time.NewTicker(operationLockPollInterval)
	defer ticker.Stop()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := writeLockOwner(file); err != nil {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
				return nil, err
			}
			return &operationLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("acquire vault operation lock: %w", err)
		}

		select {
		case <-ctx.Done():
			owner := readLockOwner(file)
			_ = file.Close()
			if owner != "" {
				return nil, fmt.Errorf("vault is busy (%s); timed out waiting %s: %w", owner, operationLockTimeout, ctx.Err())
			}
			return nil, fmt.Errorf("vault is busy; timed out waiting %s: %w", operationLockTimeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

func writeLockOwner(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("reset vault lock owner: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek vault lock owner: %w", err)
	}
	owner := fmt.Sprintf("pid=%d acquired=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if _, err := file.WriteString(owner); err != nil {
		return fmt.Errorf("write vault lock owner: %w", err)
	}
	return file.Sync()
}

func readLockOwner(file *os.File) string {
	if _, err := file.Seek(0, 0); err != nil {
		return ""
	}
	b := make([]byte, 256)
	n, err := file.Read(b)
	if err != nil && n == 0 {
		return ""
	}
	return strings.TrimSpace(string(b[:n]))
}

func (l *operationLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return fmt.Errorf("release vault operation lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close vault operation lock: %w", closeErr)
	}
	return nil
}
