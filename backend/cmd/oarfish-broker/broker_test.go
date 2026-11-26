package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Class C (ADR 0011 B9): these tests prove argument shapes, validation, the
// order of the checks and the error mapping against a fake systemctl. They do
// not prove anything about systemd's behaviour.

const demo = "demo-web.service"

// job is the expected call of a service job.
func job(verb, unit string) []string { return shapeUnitJob(verb, unit) }

func show(unit string) []string { return shapeShow(unit) }

func wantCalls(t *testing.T, tb *testBroker, want ...[]string) {
	t.Helper()
	got := tb.sys.callList()
	if len(got) != len(want) {
		t.Fatalf("systemctl calls = %q, want %q", got, want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Fatalf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func wantCode(t *testing.T, a answer, c code, state string) {
	t.Helper()
	if a.Code != c || a.State != state {
		t.Fatalf("answer = %+v, want code %s state %q", a, c, state)
	}
}

// BF16 (class C part): the success paths, with the exact shapes of B2.
func TestServiceSuccess(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "ActiveState", "inactive", "SubState", "dead"), showOut(demo)))
		wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeSuccess, "active")
		wantCalls(t, tb, show(demo), job("start", demo), show(demo))
	})
	t.Run("stop", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo), showOut(demo, "ActiveState", "inactive", "SubState", "dead")))
		wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeSuccess, "inactive")
		wantCalls(t, tb, show(demo), job("stop", demo), show(demo))
	})
	t.Run("stop ends failed", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo), showOut(demo, "ActiveState", "failed")))
		wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeSuccess, "failed")
	})
	t.Run("restart with a new start timestamp", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo), showOut(demo, "ExecMainStartTimestampMonotonic", "2000000")))
		wantCode(t, tb.ask(t, svc(opServiceRestart, demo)), codeSuccess, "active")
		wantCalls(t, tb, show(demo), job("restart", demo), show(demo))
	})
	t.Run("restart with a changed MainPID", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo), showOut(demo, "MainPID", "4343")))
		wantCode(t, tb.ask(t, svc(opServiceRestart, demo)), codeSuccess, "active")
	})
	t.Run("restart of an ordinary inactive unit starts it", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(
			showOut(demo, "ActiveState", "inactive", "MainPID", "0", "ExecMainStartTimestampMonotonic", "0"), showOut(demo)))
		wantCode(t, tb.ask(t, svc(opServiceRestart, demo)), codeSuccess, "active")
	})
	t.Run("start of a failed unit", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "ActiveState", "failed"), showOut(demo)))
		wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeSuccess, "active")
	})
}

// A3.3: the verification read after start and restart comes about
// verifyDelay after systemctl returned; after stop it is immediate.
func TestVerificationDelay(t *testing.T) {
	for _, op := range []string{opServiceStart, opServiceRestart} {
		sys := (&fakeSys{}).show(showOut(demo, "ActiveState", "failed"), showOut(demo, "MainPID", "9"))
		tb := newTestBroker(t, sys)
		tb.o.verifyDelay = 150 * time.Millisecond
		tb.ask(t, svc(op, demo))
		if gap := sys.times[2].Sub(sys.times[1]); gap < 150*time.Millisecond {
			t.Errorf("%s: the verification read came %v after the job", op, gap)
		}
	}
}

// BF3: invalid names; systemctl is not started.
func TestRule1InvalidNames(t *testing.T) {
	for _, n := range []string{
		"", "-x.service", "--now.service", "a/b.service", "../x.service", "a..b.service", "a b.service",
		"dämon.service", strings.Repeat("a", 248) + ".service", "x.socket", "x.timer", "x.target", "x",
		`a@b\.service`, `a@b\x2.service`, `a@b\X2d.service`, `a@b\x2D.service`, `a\x2d.service`, "a@b@c.service",
		".service", "a.service\n", "a.service ", "*.service", "a?.service", "[a].service", "@x.service",
	} {
		tb := newTestBroker(t, &fakeSys{})
		for _, op := range []string{opServiceStart, opServiceStop, opServiceRestart} {
			wantCode(t, tb.ask(t, svc(op, n)), codeUnitInvalid, "")
		}
		if c := tb.sys.callList(); len(c) != 0 {
			t.Fatalf("%q: systemctl ran: %q", n, c)
		}
	}
	// Valid forms reach show.
	for _, n := range []string{"a.service", `getty@tty\x2d1.service`, "x:y_z-1.2.service", strings.Repeat("a", 247) + ".service"} {
		if !validUnitName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
}

// BF4 with A3.7 and D-C89 items 2 to 4: templates and instances.
func TestRule5Templates(t *testing.T) {
	const tpl = "demo-worker@.service"
	const inst = "demo-worker@one.service"
	t.Run("template without instance", func(t *testing.T) {
		for _, op := range []string{opServiceStart, opServiceStop, opServiceRestart} {
			tb := newTestBroker(t, &fakeSys{})
			wantCode(t, tb.ask(t, svc(op, tpl)), codeUnitTemplate, "")
			wantCalls(t, tb)
		}
	})
	cases := []struct {
		op, state string
		want      code
		runs      bool
	}{
		// Never started: show reports it loaded and inactive (A2 S1).
		{opServiceStart, "inactive", codeUnitTemplate, false},
		{opServiceStop, "inactive", codeAlreadyInState, false},  // D-C89 item 2
		{opServiceRestart, "inactive", codeUnitTemplate, false}, // D-C89 item 3
		// Failed: every operation is accepted.
		{opServiceStart, "failed", codeSuccess, true},
		{opServiceStop, "failed", codeSuccess, true},
		{opServiceRestart, "failed", codeSuccess, true},
		// Running.
		{opServiceStart, "active", codeAlreadyInState, false}, // D-C89 item 4, before unit_template
		{opServiceStop, "active", codeSuccess, true},
		{opServiceRestart, "active", codeSuccess, true},
		// Neither active nor failed: start stays unit_template (A3 rule 5).
		{opServiceStart, "activating", codeUnitTemplate, false},
		{opServiceStart, "deactivating", codeUnitTemplate, false},
		{opServiceStart, "reloading", codeUnitTemplate, false},
	}
	for _, c := range cases {
		after := showOut(inst, "ActiveState", "active", "MainPID", "77")
		if c.op == opServiceStop {
			after = showOut(inst, "ActiveState", "inactive")
		}
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(inst, "ActiveState", c.state), after))
		a := tb.ask(t, svc(c.op, inst))
		if a.Code != c.want {
			t.Errorf("%s of an instance in %s: %s, want %s", c.op, c.state, a.Code, c.want)
		}
		if ran := slices.Contains(tb.sys.verbs(), serviceVerb[c.op]); ran != c.runs {
			t.Errorf("%s of an instance in %s: job ran = %v", c.op, c.state, ran)
		}
	}
	// No call ever names a new instance in another form than the request.
	tb := newTestBroker(t, (&fakeSys{}).show(showOut(inst, "ActiveState", "inactive")))
	tb.ask(t, svc(opServiceStart, inst))
	wantCalls(t, tb, show(inst))
}

// BF5: unknown names, also not-found with exit code 0 (A3 rule 3).
func TestRule3Unknown(t *testing.T) {
	for _, ls := range []string{"not-found", "bad-setting", "error", "stub", "merged", "something-new"} {
		out := notFoundOut(demo)
		out = strings.Replace(out, "LoadState=not-found", "LoadState="+ls, 1)
		tb := newTestBroker(t, (&fakeSys{}).show(out))
		wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeUnitUnknown, "")
		wantCalls(t, tb, show(demo))
	}
	// A show that fails is internal, not unknown.
	tb := newTestBroker(t, &fakeSys{shows: []runResult{{Exit: 1}}})
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeInternal, "")
}

// BF7: an alias of an allowed unit.
func TestRule4Alias(t *testing.T) {
	tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "Names", demo+" demo-alias.service")))
	wantCode(t, tb.ask(t, svc(opServiceRestart, "demo-alias.service")), codeUnitNotCanonical, "")
	wantCalls(t, tb, show("demo-alias.service"))
}

// BF8: masked units.
func TestRule6Masked(t *testing.T) {
	m := showOut(demo, "LoadState", "masked", "UnitFileState", "masked")
	for _, op := range []string{opServiceStart, opServiceRestart} {
		tb := newTestBroker(t, (&fakeSys{}).show(m))
		wantCode(t, tb.ask(t, svc(op, demo)), codeUnitMasked, "")
	}
	tb := newTestBroker(t, (&fakeSys{}).show(m, showOut(demo, "LoadState", "masked", "ActiveState", "inactive")))
	wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeSuccess, "inactive")
	// A masked one-shot unit is refused by rule 8 for stop as well (A3.7).
	tb = newTestBroker(t, (&fakeSys{}).show(showOut(demo, "LoadState", "masked", "Type", "oneshot")))
	wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeUnitOneshot, "")
}

// BF9: transient units.
func TestRule7Transient(t *testing.T) {
	for _, op := range []string{opServiceStart, opServiceStop, opServiceRestart} {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "Transient", "yes", "UnitFileState", "transient")))
		wantCode(t, tb.ask(t, svc(op, demo)), codeUnitTransient, "")
		wantCalls(t, tb, show(demo))
	}
}

// BF10 with A3.7: one-shot units, every operation.
func TestRule8Oneshot(t *testing.T) {
	for _, op := range []string{opServiceStart, opServiceStop, opServiceRestart} {
		for _, state := range []string{"active", "inactive"} {
			tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "Type", "oneshot", "ActiveState", state)))
			wantCode(t, tb.ask(t, svc(op, demo)), codeUnitOneshot, "")
			wantCalls(t, tb, show(demo))
		}
	}
}

// BF11 with A3.7: the unit's own refusal.
func TestRule9Refusal(t *testing.T) {
	cases := []struct {
		key, op string
		want    code
	}{
		{"RefuseManualStop", opServiceStop, codeRefusedByUnit},
		{"RefuseManualStop", opServiceRestart, codeRefusedByUnit},
		{"RefuseManualStart", opServiceStart, codeRefusedByUnit},
		{"RefuseManualStart", opServiceRestart, codeRefusedByUnit},
	}
	for _, c := range cases {
		state := "active"
		if c.op == opServiceStart {
			state = "inactive"
		}
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, c.key, "yes", "ActiveState", state)))
		wantCode(t, tb.ask(t, svc(c.op, demo)), c.want, "")
		wantCalls(t, tb, show(demo))
	}
	// The other direction is not refused by rule 9.
	tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "RefuseManualStop", "yes", "ActiveState", "inactive"), showOut(demo)))
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeSuccess, "active")
	tb = newTestBroker(t, (&fakeSys{}).show(showOut(demo, "RefuseManualStart", "yes"), showOut(demo, "ActiveState", "inactive")))
	wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeSuccess, "inactive")
}

// BF6 with A3.7: every entry of the deny list, as Id and as an alias of the
// canonical unit (Names). An alias requested by its own name is refused by
// rule 4 first.
func TestRule10Denied(t *testing.T) {
	denied := []string{
		"oarfish.service", "oarfish-update-helper.service", "oarfish-update-recover.service", "oarfish-broker.service",
		"systemd-reboot.service", "systemd-poweroff.service", "systemd-halt.service", "systemd-kexec.service",
		"systemd-soft-reboot.service", "systemd-journald.service", "systemd-logind.service", "systemd-udevd.service",
		"systemd-timesyncd.service", "dbus.service", "dbus-broker.service", "polkit.service",
		"emergency.service", "rescue.service", "debug-shell.service",
		"user@1000.service", "user-runtime-dir@1000.service", "getty@tty1.service", "serial-getty@ttyS0.service",
		"autovt@tty2.service", "console-getty.service", "container-getty@1.service",
		"sshd@one.service", "sshd-unix-local@one.service",
	}
	for _, n := range denied {
		for _, op := range []string{opServiceStart, opServiceStop, opServiceRestart} {
			state := "active"
			if op == opServiceStart {
				state = "failed" // also passes rule 5 for the instances
			}
			tb := newTestBroker(t, (&fakeSys{}).show(showOut(n, "ActiveState", state)))
			if a := tb.ask(t, svc(op, n)); a.Code != codeUnitDenied {
				t.Errorf("%s %s: %s, want unit_denied", op, n, a.Code)
			}
			if v := tb.sys.verbs(); len(v) != 1 {
				t.Errorf("%s %s: calls %q", op, n, v)
			}
			// The same entry as an alias of an allowed-looking canonical name.
			tb = newTestBroker(t, (&fakeSys{}).show(showOut(demo, "Names", demo+" "+n, "ActiveState", state)))
			if a := tb.ask(t, svc(op, demo)); a.Code != codeUnitDenied {
				t.Errorf("%s %s as alias: %s, want unit_denied", op, n, a.Code)
			}
		}
	}
	// Requested by the alias name itself: rule 4 answers first.
	tb := newTestBroker(t, (&fakeSys{}).show(showOut("dbus.service", "Names", "dbus.service dbus-alias.service")))
	wantCode(t, tb.ask(t, svc(opServiceRestart, "dbus-alias.service")), codeUnitNotCanonical, "")
	// An active denied instance gets unit_denied for start, not already_in_state.
	tb = newTestBroker(t, (&fakeSys{}).show(showOut("getty@tty1.service")))
	wantCode(t, tb.ask(t, svc(opServiceStart, "getty@tty1.service")), codeUnitDenied, "")
}

// A8 (finding A7.5): a unit whose Id or alias holds a \xHH escape is judged
// by rules 2 to 11 like any other unit. showOut prints Names quoted as
// systemd does; each answer is the rule's own code, never internal.
func TestEscapedNames(t *testing.T) {
	const inst = `demo-disk@dev-disk-by\x2dlabel-data.service`
	t.Run("the device shape of a systemd-fsck@ instance", func(t *testing.T) {
		// A one-shot unit: rule 8 answers before the deny list.
		for _, op := range []string{opServiceStart, opServiceStop, opServiceRestart} {
			tb := newTestBroker(t, (&fakeSys{}).show(fsckShow))
			wantCode(t, tb.ask(t, svc(op, fsckUnit)), codeUnitOneshot, "")
			wantCalls(t, tb, show(fsckUnit))
		}
		// The same instance as a long-running service: rule 10.
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(fsckUnit)))
		wantCode(t, tb.ask(t, svc(opServiceRestart, fsckUnit)), codeUnitDenied, "")
	})
	t.Run("success", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(inst), showOut(inst, "MainPID", "4343")))
		wantCode(t, tb.ask(t, svc(opServiceRestart, inst)), codeSuccess, "active")
		wantCalls(t, tb, show(inst), job("restart", inst), show(inst))
		tb = newTestBroker(t, (&fakeSys{}).show(showOut(inst), showOut(inst, "ActiveState", "inactive")))
		wantCode(t, tb.ask(t, svc(opServiceStop, inst)), codeSuccess, "inactive")
	})
	t.Run("rules 4 to 9", func(t *testing.T) {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(inst, "ActiveState", "inactive")))
		wantCode(t, tb.ask(t, svc(opServiceRestart, inst)), codeUnitTemplate, "")
		tb = newTestBroker(t, (&fakeSys{}).show(showOut(inst, "LoadState", "masked")))
		wantCode(t, tb.ask(t, svc(opServiceRestart, inst)), codeUnitMasked, "")
		tb = newTestBroker(t, (&fakeSys{}).show(showOut(inst, "Transient", "yes")))
		wantCode(t, tb.ask(t, svc(opServiceStop, inst)), codeUnitTransient, "")
		tb = newTestBroker(t, (&fakeSys{}).show(showOut(inst, "RefuseManualStop", "yes")))
		wantCode(t, tb.ask(t, svc(opServiceStop, inst)), codeRefusedByUnit, "")
		tb = newTestBroker(t, (&fakeSys{}).show(showOut(inst)))
		wantCode(t, tb.ask(t, svc(opServiceStart, inst)), codeAlreadyInState, "")
	})
	t.Run("aliases with and without escapes", func(t *testing.T) {
		// Requested by an escaped alias: rule 4.
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo, "Names", sdNames(demo, `demo@a\x2db.service`))))
		wantCode(t, tb.ask(t, svc(opServiceRestart, `demo@a\x2db.service`)), codeUnitNotCanonical, "")
		// An escaped canonical name with a plain alias, and a plain one with an
		// escaped alias, both allowed.
		for _, names := range [][]string{{inst, "demo-alias.service"}, {demo, `demo@a\x2db.service`}} {
			tb = newTestBroker(t, (&fakeSys{}).show(showOut(names[0], "Names", sdNames(names...)), showOut(names[0], "MainPID", "1")))
			wantCode(t, tb.ask(t, svc(opServiceRestart, names[0])), codeSuccess, "active")
		}
	})
	t.Run("deny list", func(t *testing.T) {
		for _, n := range []string{`getty@tty\x2d1.service`, `user@1\x2d0.service`, `systemd-x@a\x2dy.service`, `sshd@a\x2db.service`} {
			tb := newTestBroker(t, (&fakeSys{}).show(showOut(n)))
			wantCode(t, tb.ask(t, svc(opServiceRestart, n)), codeUnitDenied, "")
			// As an escaped alias of an allowed-looking canonical name.
			tb = newTestBroker(t, (&fakeSys{}).show(showOut(demo, "Names", sdNames(demo, n))))
			wantCode(t, tb.ask(t, svc(opServiceRestart, demo)), codeUnitDenied, "")
		}
	})
	t.Run("restart only", func(t *testing.T) {
		for _, n := range []string{`wpa_supplicant@wl\x2d0.service`, `ppp@my\x2ddsl.service`} {
			tb := newTestBroker(t, (&fakeSys{}).show(showOut(n)))
			wantCode(t, tb.ask(t, svc(opServiceStop, n)), codeOperationNotOffered, "")
			tb = newTestBroker(t, (&fakeSys{}).show(showOut(n), showOut(n, "MainPID", "1")))
			wantCode(t, tb.ask(t, svc(opServiceRestart, n)), codeSuccess, "active")
		}
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(inst, "Names", sdNames(inst, `wpa_supplicant@wl\x2d0.service`))))
		wantCode(t, tb.ask(t, svc(opServiceStop, inst)), codeOperationNotOffered, "")
	})
	t.Run("malformed quoting is internal", func(t *testing.T) {
		for _, v := range []string{`"` + `demo-disk@dev-disk-by\\x2dlabel-data.service`, `"demo-disk@dev-disk-by\x2dlabel-data.service"`,
			`"demo-disk@dev-disk-by\\x2dlabel-data.service"x.service`, inst} {
			tb := newTestBroker(t, (&fakeSys{}).show(showOut(inst, "Names", v)))
			wantCode(t, tb.ask(t, svc(opServiceRestart, inst)), codeInternal, "")
			wantCalls(t, tb, show(inst))
		}
	})
}

// BF12 with A1.2 and A3.7: restart only.
func TestRule11RestartOnly(t *testing.T) {
	for _, n := range []string{
		"NetworkManager.service", "networking.service", "dhcpcd.service", "wpa_supplicant.service",
		"wpa_supplicant@wlan0.service", "wpa_supplicant-nl80211@wlan0.service", "wpa_supplicant-wired@eth0.service",
		"avahi-daemon.service", "rpi-usb-gadget-ics.service", "ppp@dsl.service",
		"ssh.service", "sshd.service", "tailscaled.service",
	} {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(n)))
		if a := tb.ask(t, svc(opServiceStop, n)); a.Code != codeOperationNotOffered {
			t.Errorf("stop %s: %s", n, a.Code)
		}
		tb = newTestBroker(t, (&fakeSys{}).show(showOut(n), showOut(n, "MainPID", "1")))
		if a := tb.ask(t, svc(opServiceRestart, n)); a.Code != codeSuccess {
			t.Errorf("restart %s: %s", n, a.Code)
		}
	}
	// The alias of ssh.service is checked as well.
	tb := newTestBroker(t, (&fakeSys{}).show(showOut("ssh.service", "Names", "ssh.service sshd.service")))
	wantCode(t, tb.ask(t, svc(opServiceStop, "ssh.service")), codeOperationNotOffered, "")
	// Stop stays offered for the rest of the table of rule 11.
	for _, n := range []string{"ModemManager.service", "docker.service", "containerd.service", "podman.service", "chrony.service", "ntp.service", "ntpsec.service"} {
		tb := newTestBroker(t, (&fakeSys{}).show(showOut(n), showOut(n, "ActiveState", "inactive")))
		if a := tb.ask(t, svc(opServiceStop, n)); a.Code != codeSuccess {
			t.Errorf("stop %s: %s", n, a.Code)
		}
	}
}

func TestAlreadyInState(t *testing.T) {
	tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo)))
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeAlreadyInState, "")
	tb = newTestBroker(t, (&fakeSys{}).show(showOut(demo, "ActiveState", "inactive")))
	wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeAlreadyInState, "")
	wantCalls(t, tb, show(demo))
}

// BF13 with A3.7: a non-zero exit is job_failed with the state read
// afterwards; exit 0 with a unit that failed at once is state_unexpected.
func TestJobFailed(t *testing.T) {
	sys := (&fakeSys{job: runResult{Exit: 1}}).show(showOut(demo, "ActiveState", "inactive"), showOut(demo, "ActiveState", "failed", "Result", "exit-code"))
	tb := newTestBroker(t, sys)
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeJobFailed, "failed")
	wantCalls(t, tb, show(demo), job("start", demo), show(demo))

	// The state read fails: job_failed without a state word.
	sys = &fakeSys{job: runResult{Exit: 1}, shows: []runResult{{Out: []byte(showOut(demo, "ActiveState", "inactive"))}, {Exit: 1}}}
	tb = newTestBroker(t, sys)
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeJobFailed, "")

	sys = (&fakeSys{}).show(showOut(demo, "ActiveState", "inactive"), showOut(demo, "ActiveState", "failed", "Result", "exit-code"))
	tb = newTestBroker(t, sys)
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeStateUnexpected, "failed")
}

// BF14: a job that does not end by the deadline is timeout with the state
// read then.
func TestTimeout(t *testing.T) {
	sys := (&fakeSys{job: runResult{TimedOut: true, Exit: -1}}).show(showOut(demo, "ActiveState", "inactive"), showOut(demo, "ActiveState", "activating"))
	tb := newTestBroker(t, sys)
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeTimeout, "activating")
}

// BF15 with A3.7: systemctl reports success, the read about 1 s later does not.
func TestStateUnexpected(t *testing.T) {
	cases := []struct {
		op, before, after, state string
	}{
		{opServiceStart, showOut(demo, "ActiveState", "inactive"), showOut(demo, "ActiveState", "inactive"), "inactive"},
		{opServiceStop, showOut(demo), showOut(demo, "ActiveState", "deactivating"), "deactivating"},
		{opServiceRestart, showOut(demo), showOut(demo, "ActiveState", "failed", "MainPID", "1"), "failed"},
		// D-C89 item 5: active, but neither a new start timestamp nor a new MainPID.
		{opServiceRestart, showOut(demo), showOut(demo), "active"},
	}
	for _, c := range cases {
		tb := newTestBroker(t, (&fakeSys{}).show(c.before, c.after))
		wantCode(t, tb.ask(t, svc(c.op, demo)), codeStateUnexpected, c.state)
	}
	// The verification read fails: internal.
	tb := newTestBroker(t, &fakeSys{shows: []runResult{{Out: []byte(showOut(demo))}, {Out: []byte("garbage")}}})
	wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeInternal, "")
}

// BF17: one operation at a time. A request that arrives while another runs is
// busy and starts nothing.
func TestBusy(t *testing.T) {
	sys := (&fakeSys{}).show(showOut(demo), showOut(demo, "MainPID", "5"))
	tb := newTestBroker(t, sys)
	inJob, release := make(chan struct{}), make(chan struct{})
	sys.during = func() { close(inJob); <-release }
	done := make(chan answer)
	go func() { done <- tb.ask(t, svc(opServiceRestart, demo)) }()
	<-inJob
	wantCode(t, tb.ask(t, svc(opServiceStop, "other.service")), codeBusy, "")
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeBusy, "")
	if s := tb.status(); !s.Busy {
		t.Error("status does not report busy")
	}
	close(release)
	wantCode(t, <-done, codeSuccess, "active")
	if n := len(sys.callList()); n != 3 {
		t.Fatalf("calls = %d, want 3 (the busy requests ran nothing)", n)
	}
}

// BF18 (class C part) with D-C89 item 7: at most 10 mutating service
// operations per 60 s; a slot is used whatever the later result.
func TestRateLimit(t *testing.T) {
	tb := newTestBroker(t, &fakeSys{})
	for i := 0; i < 10; i++ {
		// unit_invalid: refused after the rate check, still uses a slot.
		wantCode(t, tb.ask(t, svc(opServiceStart, "bad name")), codeUnitInvalid, "")
		tb.clock.add(time.Second)
	}
	a := tb.ask(t, svc(opServiceRestart, demo))
	if a.Code != codeRateLimited || a.RetryAfterS != 50 {
		t.Fatalf("11th request: %+v, want rate_limited with 50 s", a)
	}
	if len(tb.sys.callList()) != 0 {
		t.Fatal("a rate-limited request ran systemctl")
	}
	// A rate-limited request uses no slot, and status uses none.
	tb.respond(Caller{UID: testUID}, []byte(`{"v":1,"op":"status","correlation_id":"`+testCID+`"}`))
	tb.clock.add(49 * time.Second) // the first slot leaves the window at 60 s after it was used
	if a := tb.ask(t, svc(opServiceStart, "bad name")); a.Code != codeRateLimited || a.RetryAfterS != 1 {
		t.Fatalf("at 59 s: %+v", a)
	}
	tb.clock.add(time.Second)
	wantCode(t, tb.ask(t, svc(opServiceStart, "bad name")), codeUnitInvalid, "")
	if a := tb.ask(t, svc(opServiceStart, "bad name")); a.Code != codeRateLimited {
		t.Fatalf("the window is full again: %+v", a)
	}

	// busy uses a slot as well (D-C89 item 7).
	tb = newTestBroker(t, &fakeSys{})
	tb.running = true
	for i := 0; i < 10; i++ {
		wantCode(t, tb.ask(t, svc(opServiceStop, demo)), codeBusy, "")
	}
	if a := tb.ask(t, svc(opServiceStop, demo)); a.Code != codeRateLimited {
		t.Fatalf("after 10 busy answers: %+v", a)
	}
	// Power operations are not counted in this window.
	tb.running = false
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeAccepted, "")
}

// BF28 with D-C89 item 6: one accepted power operation per broker lifetime.
func TestPowerOnce(t *testing.T) {
	for _, first := range []string{opSystemReboot, opSystemPoweroff} {
		tb := newTestBroker(t, &fakeSys{})
		wantCode(t, tb.ask(t, power(first)), codeAccepted, "")
		wantCalls(t, tb, shapePower(powerVerb[first]))
		for _, second := range []string{opSystemReboot, opSystemPoweroff} {
			wantCode(t, tb.ask(t, power(second)), codeAlreadyRequested, "")
		}
		wantCalls(t, tb, shapePower(powerVerb[first]))
		if s := tb.status(); !s.PowerRequested {
			t.Error("status does not report the power request")
		}
	}
	// A failed or timed-out power call is not accepted and sets no flag.
	tb := newTestBroker(t, &fakeSys{job: runResult{Exit: 1}})
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeJobFailed, "")
	tb.sys.job = runResult{TimedOut: true, Exit: -1}
	wantCode(t, tb.ask(t, power(opSystemPoweroff)), codeTimeout, "")
	tb.sys.job = runResult{}
	wantCode(t, tb.ask(t, power(opSystemPoweroff)), codeAccepted, "")
}

// The policy runs before any unit lookup (B7) and before the rate check.
func TestPolicyFirst(t *testing.T) {
	tb := newTestBroker(t, &fakeSys{})
	var seen []Operation
	tb.policy = policyFunc(func(c Caller, op Operation, unit string) (bool, code) {
		seen = append(seen, op)
		return false, codeInternal
	})
	for _, line := range []string{svc(opServiceStart, demo), svc(opServiceStop, demo), svc(opServiceRestart, demo), power(opSystemReboot), power(opSystemPoweroff)} {
		wantCode(t, tb.ask(t, line), codeInternal, "")
	}
	if len(tb.sys.callList()) != 0 || len(tb.window) != 0 {
		t.Fatalf("a refused caller reached systemctl or the window: %q %v", tb.sys.callList(), tb.window)
	}
	if len(seen) != 5 {
		t.Fatalf("policy saw %v", seen)
	}
}

type policyFunc func(Caller, Operation, string) (bool, code)

func (f policyFunc) Allow(c Caller, op Operation, unit string) (bool, code) { return f(c, op, unit) }

func TestCorePolicy(t *testing.T) {
	p := newCorePolicy(testUID)
	for _, op := range []Operation{opServiceStart, opServiceStop, opServiceRestart, opSystemReboot, opSystemPoweroff} {
		if ok, _ := p.Allow(Caller{UID: testUID}, op, ""); !ok {
			t.Errorf("core caller refused %s", op)
		}
		if ok, c := p.Allow(Caller{UID: 0}, op, ""); ok || c != codeInternal {
			t.Errorf("root allowed %s", op)
		}
		if ok, _ := p.Allow(Caller{UID: testUID, Plugin: "x"}, op, ""); ok {
			t.Errorf("plugin allowed %s", op)
		}
	}
	for _, op := range []Operation{"status", "exec", "service.reload", ""} {
		if ok, _ := p.Allow(Caller{UID: testUID}, op, ""); ok {
			t.Errorf("unknown operation %q allowed", op)
		}
	}
}

// BF21 (class C part, broker side): hostile output of show is internal and
// never reaches the answer or the journal.
func TestHostileShowOutput(t *testing.T) {
	big := strings.Repeat("A", 1<<20)
	cases := map[string]runResult{
		"overflow":         {Out: []byte(showOut(demo)), Overflow: true},
		"control":          {Out: []byte(strings.Replace(showOut(demo), "SubState=running", "SubState=run\x1bning", 1))},
		"invalid utf-8":    {Out: []byte(strings.Replace(showOut(demo), "Result=success", "Result=\xff\xfe", 1))},
		"markup in Names":  {Out: []byte(strings.Replace(showOut(demo), "Names="+demo, "Names=<script>.service", 1))},
		"missing key":      {Out: []byte(showOut(demo, "MainPID", "<omit>"))},
		"duplicate key":    {Out: []byte(showOut(demo) + "MainPID=1\n")},
		"unknown key":      {Out: []byte(strings.Replace(showOut(demo), "Transient=no", "Description=x", 1))},
		"extra line":       {Out: []byte(showOut(demo) + "\n")},
		"empty":            {Out: nil},
		"huge value":       {Out: []byte(strings.Replace(showOut(demo), "Result=success", "Result="+big, 1))},
		"unknown state":    {Out: []byte(showOut(demo, "ActiveState", "exploding"))},
		"unknown type":     {Out: []byte(showOut(demo, "Type", "daemonish"))},
		"bad pid":          {Out: []byte(showOut(demo, "MainPID", "-1"))},
		"bad timestamp":    {Out: []byte(showOut(demo, "ExecMainStartTimestampMonotonic", "1e9"))},
		"bad yes/no":       {Out: []byte(showOut(demo, "Transient", "maybe"))},
		"no names":         {Out: []byte(showOut(demo, "Names", ""))},
		"invalid Id":       {Out: []byte(showOut(demo, "Id", "../x.service"))},
		"carriage return":  {Out: []byte(strings.ReplaceAll(showOut(demo), "\n", "\r\n"))},
		"non-zero exit":    {Out: []byte(showOut(demo)), Exit: 3},
		"timed out":        {Out: []byte(showOut(demo)), TimedOut: true},
		"spaces in values": {Out: []byte(showOut(demo, "Names", demo+"  x.service"))},
	}
	for name, r := range cases {
		tb := newTestBroker(t, &fakeSys{shows: []runResult{r}})
		out := tb.respond(Caller{UID: testUID}, []byte(svc(opServiceStart, demo)))
		a := decodeAnswer(t, out)
		if a.Code != codeInternal {
			t.Errorf("%s: %+v, want internal", name, a)
		}
		if len(tb.sys.callList()) != 1 {
			t.Errorf("%s: a job ran after a hostile show", name)
		}
		if j := tb.journal.String(); strings.ContainsAny(j, "<>\x1b\xff") || strings.Contains(j, "AAAA") {
			t.Errorf("%s: journal carries output: %q", name, j)
		}
	}
}

// The answer never echoes the request, and the journal line carries only the
// validated forms (B2, B5).
func TestJournalLine(t *testing.T) {
	tb := newTestBroker(t, (&fakeSys{}).show(showOut(demo)))
	tb.ask(t, svc(opServiceStart, demo))
	tb.ask(t, svc(opServiceStart, "evil\nop=x.service"))
	tb.ask(t, `{"v":1,"op":"service.start","unit":"a.service","correlation_id":"ZZZ","x":1}`)
	tb.ask(t, power(opSystemReboot))
	want := "op=service.start unit=" + demo + " cid=" + testCID + " result=already_in_state\n" +
		"op=service.start unit=- cid=" + testCID + " result=unit_invalid\n" +
		"op=- unit=- cid=- result=bad_request\n" +
		"op=system.reboot unit=- cid=" + testCID + " result=accepted\n"
	if got := tb.journal.String(); got != want {
		t.Fatalf("journal:\n%s\nwant:\n%s", got, want)
	}
}

func TestStatus(t *testing.T) {
	tb := newTestBroker(t, &fakeSys{})
	out := string(tb.respond(Caller{UID: testUID}, []byte(`{"v":1,"op":"status","correlation_id":"`+testCID+`"}`)))
	want := `{"v":1,"code":"success","protocol_min":1,"protocol_max":1,"build_id":"broker-1","power_requested":false,"busy":false}` + "\n"
	if out != want {
		t.Fatalf("status = %s, want %s", out, want)
	}
	if len(tb.sys.callList()) != 0 {
		t.Fatal("status ran systemctl")
	}
}

// Every code of the closed set is reached by a request (acceptance of G7.2).
// The named tests cover each code in depth; this one checks that none is
// unreachable.
func TestEveryCodeIsReached(t *testing.T) {
	reached := map[code]bool{}
	note := func(a answer) { reached[a.Code] = true }
	one := func(sys *fakeSys, line string) answer {
		tb := newTestBroker(t, sys)
		a := tb.ask(t, line)
		note(a)
		return a
	}
	inst := "demo-worker@one.service"
	one((&fakeSys{}).show(showOut(demo), showOut(demo, "MainPID", "1")), svc(opServiceRestart, demo))       // success
	one(&fakeSys{}, power(opSystemReboot))                                                                  // accepted
	one(&fakeSys{}, `{"v":2}`)                                                                              // bad_request
	one(&fakeSys{}, svc(opServiceStart, "x y"))                                                             // unit_invalid
	one((&fakeSys{}).show(notFoundOut(demo)), svc(opServiceStart, demo))                                    // unit_unknown
	one((&fakeSys{}).show(showOut(demo, "Names", demo+" a.service")), svc(opServiceStart, "a.service"))     // unit_not_canonical
	one((&fakeSys{}).show(showOut("dbus.service")), svc(opServiceRestart, "dbus.service"))                  // unit_denied
	one((&fakeSys{}).show(showOut(demo, "LoadState", "masked")), svc(opServiceStart, demo))                 // unit_masked
	one((&fakeSys{}).show(showOut(demo, "Transient", "yes")), svc(opServiceStart, demo))                    // unit_transient
	one((&fakeSys{}).show(showOut(demo, "Type", "oneshot")), svc(opServiceStart, demo))                     // unit_oneshot
	one((&fakeSys{}).show(showOut(inst, "ActiveState", "inactive")), svc(opServiceStart, inst))             // unit_template
	one((&fakeSys{}).show(showOut(demo, "RefuseManualStop", "yes")), svc(opServiceStop, demo))              // refused_by_unit
	one((&fakeSys{}).show(showOut("ssh.service")), svc(opServiceStop, "ssh.service"))                       // operation_not_offered
	one((&fakeSys{}).show(showOut(demo)), svc(opServiceStart, demo))                                        // already_in_state
	one((&fakeSys{job: runResult{Exit: 1}}).show(showOut(demo)), svc(opServiceStop, demo))                  // job_failed
	one((&fakeSys{job: runResult{TimedOut: true}}).show(showOut(demo)), svc(opServiceStop, demo))           // timeout
	one((&fakeSys{}).show(showOut(demo), showOut(demo, "ActiveState", "active")), svc(opServiceStop, demo)) // state_unexpected
	one(&fakeSys{shows: []runResult{{Exit: 1}}}, svc(opServiceStart, demo))                                 // internal
	tb := newTestBroker(t, &fakeSys{})
	tb.running = true
	note(tb.ask(t, svc(opServiceStart, demo))) // busy
	tb.running = false
	for i := 0; i < 10; i++ {
		tb.ask(t, svc(opServiceStart, "x y"))
	}
	note(tb.ask(t, svc(opServiceStart, demo))) // rate_limited
	note(tb.ask(t, power(opSystemReboot)))
	note(tb.ask(t, power(opSystemReboot))) // already_requested
	tb = newTestBroker(t, &fakeSys{})
	makeUpdateDir(t, tb.dir, helperState("c_switched"))
	note(tb.ask(t, power(opSystemPoweroff))) // update_running
	for _, c := range allCodes {
		if !reached[c] {
			t.Errorf("code %s is not reached", c)
		}
	}
	if len(reached) != len(allCodes) {
		t.Errorf("reached %d codes, the closed set has %d", len(reached), len(allCodes))
	}
}
