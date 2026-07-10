// Package device owns the stable, non-secret identifier for one memsync
// installation. The AES vault key is shared by paired machines; this ID is not.
package device

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const idBytes = 16

// Info identifies the machine that produced a memory record.
type Info struct {
	ID   string
	Name string
}

// LoadOrCreate returns this installation's stable identity, creating it with
// private file permissions when it does not yet exist.
func LoadOrCreate(path string) (Info, bool, error) {
	if id, err := loadID(path); err == nil {
		return Info{ID: id, Name: hostName()}, false, nil
	} else if !os.IsNotExist(err) {
		return Info{}, false, err
	}

	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		return Info{}, false, err
	}
	id := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Info{}, false, err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".device-id-*.tmp")
	if err != nil {
		return Info{}, false, err
	}
	tmpName := f.Name()
	defer os.Remove(tmpName)
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return Info{}, false, err
	}
	if _, err := f.WriteString(id + "\n"); err != nil {
		_ = f.Close()
		return Info{}, false, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return Info{}, false, err
	}
	if err := f.Close(); err != nil {
		return Info{}, false, err
	}
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) { // another concurrent bootstrap won the race
			winner, loadErr := loadID(path)
			return Info{ID: winner, Name: hostName()}, false, loadErr
		}
		return Info{}, false, err
	}
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(path, 0o600)
	return Info{ID: id, Name: hostName()}, true, nil
}

// Load reads an existing identity without creating one.
func Load(path string) (Info, error) {
	id, err := loadID(path)
	if err != nil {
		return Info{}, err
	}
	return Info{ID: id, Name: hostName()}, nil
}

func loadID(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != idBytes {
		return "", fmt.Errorf("device id file %s is corrupt", path)
	}
	return id, nil
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "this-machine"
	}
	name = strings.TrimSpace(strings.Split(name, "\n")[0])
	if short, _, ok := strings.Cut(name, "."); ok && short != "" {
		return short
	}
	return name
}
