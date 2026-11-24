package main

import (
	"encoding/json"
	"maps"
	"slices"
	"syscall"
	"testing"
)

// The interface of ADR 0010 A19.2 for its one reader, the privilege broker
// (ADR 0011 B6): the lock on /var/lib/oarfish-update and the member phase of
// state.json with its closed set of values. A20 (G7.3a) changes neither. The
// values are literals here on purpose: a change of a constant of the helper
// fails this test and needs an amendment of both ADRs. The broker pins its side
// in its own tests; the helper imports nothing of the broker (TestImports).
func TestBrokerReaderInterface(t *testing.T) {
	if stateDirPath != "/var/lib/oarfish-update" || stateFile != "state.json" {
		t.Fatalf("state directory %s, state file %s", stateDirPath, stateFile)
	}
	want := []string{"idle", "staging", "staged", "switching", "verifying", "committed", "rolling_back", "rolled_back",
		"rollback_failed", "c_switching", "c_switched", "c_verified", "c_committed", "c_rolling_back"}
	if got := slices.Sorted(maps.Keys(knownPhases)); !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Fatalf("phases %v, want %v", got, want)
	}

	// phase is a string member of the object transaction, beside schema_version 1.
	var doc struct {
		SchemaVersion json.RawMessage `json:"schema_version"`
		Transaction   struct {
			Phase *string `json:"phase"`
		} `json:"transaction"`
	}
	for _, p := range want {
		st := freshState("0.0.0-test")
		st.Txn.Phase = p
		if err := json.Unmarshal(encodeState(st), &doc); err != nil {
			t.Fatal(err)
		}
		if string(doc.SchemaVersion) != "1" || doc.Transaction.Phase == nil || *doc.Transaction.Phase != p {
			t.Fatalf("phase %s is written as schema_version %s, phase %v", p, doc.SchemaVersion, doc.Transaction.Phase)
		}
	}

	// The lock is an exclusive flock on the directory itself: while the helper
	// holds it, the reader's LOCK_SH|LOCK_NB fails, and while a reader holds a
	// shared lock, the helper's lock answers busy.
	dir := t.TempDir()
	l, err := lockDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != syscall.EWOULDBLOCK {
		t.Fatalf("a shared lock while the helper holds its lock: %v, want EWOULDBLOCK", err)
	}
	l.release()
	if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatalf("a shared lock after the helper released its lock: %v", err)
	}
	if _, err := lockDir(dir); err != errLockBusy {
		t.Fatalf("the helper's lock while a reader holds a shared lock: %v, want busy", err)
	}
}
