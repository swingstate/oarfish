package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// status removes a staged release that has expired (30 minutes or more of
// uptime since staging, or a smaller uptime: a reboot) when it can take the
// lock without waiting (G7).
func TestStatusExpiresAStagedRelease(t *testing.T) {
	tests := []struct {
		name   string
		uptime int64
		expire bool
	}{
		{"just staged", 1000, false},
		{"one second before 30 minutes", 1000 + 1799, false},
		{"exactly 30 minutes", 1000 + 1800, true},
		{"one second after", 1000 + 1801, true},
		{"a day later", 1000 + 86400, true},
		{"reboot: uptime below the recorded one", 999, true},
		{"reboot: uptime 0", 0, true},
		{"reboot, but the uptime already passed the old value", 1001, false}, // not noticed, bounded by the 30 minutes
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTenv(t)
			v.stageable("1.1.0")
			wantOK(t, v.stage("1.1.0"))
			v.uptime.Store(tt.uptime)
			r := v.status()
			if !r.OK {
				t.Fatalf("status %+v", r)
			}
			_, err := os.Stat(filepath.Join(v.root, "releases", "1.1.0"))
			if tt.expire {
				if err == nil || r.Phase != phaseIdle || r.StagedVersion != "" {
					t.Fatalf("not expired: %+v (stat %v)", r, err)
				}
				if r.LastResult == nil || r.LastResult.Outcome != "aborted" || r.LastResult.ReasonCode != "stage_expired" ||
					r.LastResult.To != "1.1.0" || r.LastResult.From != "1.0.0" || r.LastResult.CorrelationID != testCID || r.LastResult.Acknowledged {
					t.Fatalf("last result %+v", r.LastResult)
				}
				v.wantClean()
				if !strings.Contains(v.journal.String(), "phase=idle to=1.1.0 from=1.0.0 cid="+testCID+" result=stage_expired") {
					t.Fatalf("journal %q", v.journal.String())
				}
			} else if err != nil || r.Phase != phaseStaged || r.StagedVersion != "1.1.0" {
				t.Fatalf("expired too early: %+v (stat %v)", r, err)
			}
			if !lockFree(t, v.stateDir) {
				t.Fatal("lock held")
			}
		})
	}
}

// A status request without cleanup work does not touch the lock or the disk.
func TestStatusWithoutWorkDoesNotMutateOrLock(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	held, _ := lockDir(v.stateDir)
	defer held.release()
	for i := 0; i < 3; i++ {
		if r := v.status(); !r.OK || r.Phase != phaseStaged {
			t.Fatalf("%+v", r)
		}
	}
	if ops := v.fs.Ops(); len(ops) != 0 {
		t.Fatalf("status mutated: %v", ops)
	}
}

// The clock failing stops the expiry cleanup but not the answer.
func TestStatusWhenTheClockFails(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	v.h.uptime = func() (int64, error) { return 0, errNoClock }
	if r := v.status(); !r.OK || r.Phase != phaseStaged {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(v.root, "releases", "1.1.0")); err != nil {
		t.Fatal("removed without a clock")
	}
}

// The expiry never removes the installed or the recorded previous version.
func TestExpiryGuards(t *testing.T) {
	for _, name := range []string{"installed", "previous", "installed unknown"} {
		t.Run(name, func(t *testing.T) {
			v := newTenv(t)
			st := freshState("x")
			to := "1.0.0"
			if name == "previous" {
				to = "0.9.0"
				st.Previous = to
				os.MkdirAll(filepath.Join(v.root, "releases", to), 0o755)
			}
			if name == "installed unknown" {
				to = "1.2.0"
				os.MkdirAll(filepath.Join(v.root, "releases", to), 0o755)
				os.Remove(filepath.Join(v.root, "current"))
			}
			st.Txn = txn{ID: "00000000000000aa", Phase: phaseStaged, To: to, From: "0.8.0", StagedAtUptimeS: 1}
			v.writeState(st)
			v.uptime.Store(5000)
			r := v.status()
			if _, err := os.Stat(filepath.Join(v.root, "releases", to)); err != nil {
				t.Fatalf("%s: the release directory is gone", name)
			}
			if name == "installed unknown" {
				if r.Phase != phaseStaged {
					t.Fatalf("expired without knowing the installed version: %+v", r)
				}
				return
			}
			// The state is repaired, the directory kept.
			if r.Phase != phaseIdle || r.LastResult == nil || r.LastResult.ReasonCode != "stage_expired" {
				t.Fatalf("%+v", r)
			}
			for _, op := range v.fs.Ops() {
				if strings.HasPrefix(op.Path, "releases/"+to) {
					t.Fatalf("touched %v", op)
				}
			}
		})
	}
}

// A failing expiry leaves the staged state (and the next status tries again).
func TestExpiryFailureKeepsTheState(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	v.uptime.Store(1000 + stageLifetimeS)
	v.h.wrapFS = func(inner fsops) fsops { f := newTestFS(inner); f.failAt = 1; v.fs = f; return f }
	if r := v.status(); !r.OK || r.Phase != phaseStaged {
		t.Fatalf("%+v", r)
	}
	v.h.wrapFS = nil
	if r := v.status(); r.Phase != phaseIdle {
		t.Fatalf("%+v", r)
	}
	v.wantClean()
}

// ---- discard --------------------------------------------------------------

func TestDiscard(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))

	// Another version, or nothing staged for that version: not_found, nothing changes.
	before := snapshot(t, v.root)
	wantErrCode(t, v.request(discardLine("1.2.0")), codeNotFound)
	wantErrCode(t, v.request(discardLine("1.0.0")), codeNotFound) // the installed version
	if d := diffSnapshots(before, snapshot(t, v.root)); len(d) != 0 {
		t.Fatalf("a refused discard changed %v", d)
	}
	if st, _ := v.readState(); st.Txn.Phase != phaseStaged {
		t.Fatalf("state %+v", st.Txn)
	}

	r := wantOK(t, v.request(discardLine("1.1.0")))
	if r.V != 1 {
		t.Fatal(r)
	}
	v.wantClean()
	st, _ := v.readState()
	if st.Txn.Phase != phaseIdle || st.Txn.To != "" || st.Last != nil {
		t.Fatalf("state %+v last %+v: want idle, no last result", st.Txn, st.Last)
	}
	// And now there is nothing to discard.
	wantErrCode(t, v.request(discardLine("1.1.0")), codeNotFound)
	// The next stage works again.
	wantOK(t, v.stage("1.1.0"))
}

func TestDiscardWithNothingStaged(t *testing.T) {
	v := newTenv(t)
	wantErrCode(t, v.request(discardLine("1.1.0")), codeNotFound)
	if _, ok := v.readState(); ok {
		t.Fatal("a refused discard wrote the state")
	}
}

// Leftovers of an exact-version discard are removed too, through .trash.
func TestDiscardRemovesLeftoversOfTheVersion(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	os.WriteFile(filepath.Join(v.root, "releases", "1.1.0", "extra"), []byte("x"), 0o644)
	wantOK(t, v.request(discardLine("1.1.0")))
	v.wantClean()
	var viaTrash bool
	for _, op := range v.fs.Ops() {
		if op.Kind == "rename" && op.Path == "releases/1.1.0" && strings.HasPrefix(op.Path2, "releases/.trash.") {
			viaTrash = true
		}
	}
	if !viaTrash {
		t.Fatal("the release was not removed through .trash")
	}
}

// Never the installed or the recorded previous version, even when the state
// file says they are staged.
func TestDiscardGuards(t *testing.T) {
	for _, name := range []string{"installed", "previous"} {
		v := newTenv(t)
		st := freshState("x")
		to := "1.0.0"
		if name == "previous" {
			to = "0.9.0"
			st.Previous = to
			os.MkdirAll(filepath.Join(v.root, "releases", to), 0o755)
		}
		st.Txn = txn{ID: "00000000000000aa", Phase: phaseStaged, To: to, From: "0.8.0", StagedAtUptimeS: 1000}
		v.writeState(st)
		wantErrCode(t, v.request(discardLine(to)), codeNotFound)
		if _, err := os.Stat(filepath.Join(v.root, "releases", to)); err != nil {
			t.Fatalf("%s: removed", name)
		}
		if len(v.fs.Ops()) != 0 {
			t.Fatalf("%s: mutated %v", name, v.fs.Ops())
		}
	}
}

func TestDiscardWithUnknownInstalledVersion(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	os.Remove(filepath.Join(v.root, "current"))
	wantErrCode(t, v.request(discardLine("1.1.0")), codeCurrentUnhealthy)
	if _, err := os.Stat(filepath.Join(v.root, "releases", "1.1.0")); err != nil {
		t.Fatal("removed without knowing the installed version")
	}
}
