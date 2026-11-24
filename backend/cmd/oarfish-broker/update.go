package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// The update check of a power operation (ADR 0011 B6; ADR 0010 A19.2). The
// broker is a read-only reader of the update helper's state directory: it
// opens the directory read-only, takes LOCK_SH|LOCK_NB on it, and reads the
// member `phase` of state.json. It writes nothing there. The helper holds
// LOCK_EX for a whole transaction; while the broker holds LOCK_SH, the
// helper's next LOCK_EX|LOCK_NB answers busy, so no transaction starts
// before systemd has queued the reboot or power-off.
//
// This is the broker's own code: it shares nothing with the helper (ADR 0010
// U2). The format it reads is the interface of A19.2.

// updateDir is the helper's state directory (ADR 0010 U8).
const updateDir = "/var/lib/oarfish-update"

const (
	helperStateFile  = "state.json"
	helperSchema     = 1
	maxHelperState   = 64 << 10 // the helper's own bound of state.json
	maxHelperMembers = 256
)

// finishedPhases are the phases in which no transaction is unfinished (B6).
// Every other phase, including the five component phases and the unknown
// ones, is refused: none of c_switching, c_switched, c_verified, c_committed
// and c_rolling_back is a phase in which a transaction has ended (ADR 0010
// A16.2; A4).
var finishedPhases = set("idle", "staged", "committed", "rolled_back", "rollback_failed")

// updateLock is a held shared lock. release is idempotent.
type updateLock struct{ fd int }

func (l *updateLock) release() {
	if l == nil || l.fd < 0 {
		return
	}
	syscall.Close(l.fd)
	l.fd = -1
}

// checkUpdate returns a held shared lock (nil when no helper is installed)
// and true when a power operation may go on. It returns false (update_running)
// when the helper holds its lock, when the directory cannot be opened or
// locked for any reason other than its absence, and when state.json is
// unreadable, a link or not a regular file, too large, not of schema 1 or
// names a phase that is not finished. A missing state.json in the locked
// directory is no transaction, as the helper treats it (D-C89 item 8). The
// caller holds the lock until systemctl has returned.
func checkUpdate(dir string) (*updateLock, bool) {
	fd, err := openDir(dir)
	if errors.Is(err, syscall.ENOENT) {
		return nil, true // no helper installed: not an update (B6)
	}
	if err != nil {
		return nil, false
	}
	for {
		err = syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		syscall.Close(fd)
		return nil, false // EWOULDBLOCK: the helper holds LOCK_EX; anything else fails closed
	}
	l := &updateLock{fd: fd}
	phase, err := readHelperPhase(dir)
	if err != nil || !finishedPhases[phase] {
		l.release()
		return nil, false
	}
	return l, true
}

func openDir(dir string) (int, error) {
	for {
		fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != syscall.EINTR {
			return fd, err
		}
	}
}

var errHelperState = errors.New("helper state not readable")

// readHelperPhase reads state.json read-only, without following a link, as a
// regular file of at most maxHelperState bytes, and returns transaction.phase.
// A missing file is the phase idle: the helper writes state.json only when a
// transaction or a recovery first saves its state, and reads a missing file
// as a fresh idle state (D-C89 item 8). Only ENOENT counts as missing; a link
// named state.json, dangling or not, fails the open with O_NOFOLLOW and is
// refused like every other open error.
func readHelperPhase(dir string) (string, error) {
	f, err := os.OpenFile(filepath.Join(dir, helperStateFile), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return "idle", nil
	}
	if err != nil {
		return "", errHelperState
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxHelperState {
		return "", errHelperState
	}
	data, err := io.ReadAll(io.LimitReader(f, maxHelperState+1))
	if err != nil || len(data) > maxHelperState {
		return "", errHelperState
	}
	return decodeHelperPhase(data)
}

// decodeHelperPhase checks the schema and returns transaction.phase. Unknown
// members are ignored at both levels (the helper adds members additively,
// ADR 0010 U15 rule 6); the members it reads must appear exactly once, with
// their exact name and type: schema_version the integer 1, transaction an
// object, phase a string.
func decodeHelperPhase(data []byte) (string, error) {
	top, err := jsonObject(data, "schema_version", "transaction")
	if err != nil {
		return "", err
	}
	if string(bytes.TrimSpace(top["schema_version"])) != "1" {
		return "", errHelperState
	}
	txn, err := jsonObject(top["transaction"], "phase")
	if err != nil {
		return "", err
	}
	var phase *string
	if json.Unmarshal(txn["phase"], &phase) != nil || phase == nil {
		return "", errHelperState
	}
	return *phase, nil
}

// jsonObject splits one JSON object into its members and requires each name
// of need to be present. A repeated member, a member whose name differs from
// a needed name only in letter case, and data after the object are errors.
func jsonObject(raw []byte, need ...string) (map[string]json.RawMessage, error) {
	if raw == nil {
		return nil, errHelperState
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errHelperState
	}
	m := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok || len(m) >= maxHelperMembers {
			return nil, errHelperState
		}
		if _, dup := m[key]; dup {
			return nil, errHelperState
		}
		for _, n := range need {
			if key != n && strings.EqualFold(key, n) {
				return nil, errHelperState
			}
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, errHelperState
		}
		m[key] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errHelperState
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errHelperState
	}
	for _, n := range need {
		if _, ok := m[n]; !ok {
			return nil, errHelperState
		}
	}
	return m, nil
}
