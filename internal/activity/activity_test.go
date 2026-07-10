package activity

import (
	"errors"
	"testing"
)

func TestRecordAndRead(t *testing.T) {
	dir := t.TempDir()
	if err := Record(dir, "claude", "inject", "2 memories", nil); err != nil {
		t.Fatal(err)
	}
	e, err := Read(dir, "claude", "inject")
	if err != nil {
		t.Fatal(err)
	}
	if !e.OK || e.Detail != "2 memories" || e.At.IsZero() {
		t.Fatalf("unexpected event: %+v", e)
	}
	if err := Record(dir, "claude", "capture", "", errors.New("offline")); err != nil {
		t.Fatal(err)
	}
	e, _ = Read(dir, "claude", "capture")
	if e.OK || e.Detail != "hook failed; run `memsync doctor`" {
		t.Fatalf("failure not recorded: %+v", e)
	}
}
