// Package activity records small, local hook heartbeats so status and doctor
// can distinguish "configured" from "actually observed running".
package activity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Event is a redacted record of one hook execution. It never contains memory
// bodies, prompts, paths, credentials, or command output.
type Event struct {
	Tool   string    `json:"tool"`
	Name   string    `json:"name"`
	At     time.Time `json:"at"`
	OK     bool      `json:"ok"`
	Detail string    `json:"detail,omitempty"`
}

// Record atomically stores the latest execution of a tool/event pair.
func Record(dataDir, tool, name, detail string, runErr error) error {
	dir := filepath.Join(dataDir, "activity")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	e := Event{Tool: tool, Name: name, At: time.Now().UTC(), OK: runErr == nil, Detail: detail}
	if runErr != nil {
		// Hook failures can contain filesystem paths, remote URLs, or subprocess
		// output. Keep the heartbeat intentionally redacted; the user can run the
		// live diagnostic when they are present to inspect a failure.
		e.Detail = "hook failed; run `memsync doctor`"
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fileName(tool, name))
	tmp, err := os.CreateTemp(dir, ".activity-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Read returns the latest event, if one has been observed.
func Read(dataDir, tool, name string) (Event, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, "activity", fileName(tool, name)))
	if err != nil {
		return Event{}, err
	}
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return Event{}, err
	}
	return e, nil
}

// All reads every well-formed heartbeat without exposing their file names.
func All(dataDir string) ([]Event, error) {
	dir := filepath.Join(dataDir, "activity")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var e Event
		if json.Unmarshal(b, &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func fileName(tool, name string) string {
	clean := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return '-'
		}, s)
	}
	return fmt.Sprintf("%s-%s.json", clean(tool), clean(name))
}
