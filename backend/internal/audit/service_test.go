package audit

import (
	"slices"
	"strings"
	"testing"
)

func keys[K ~string, V any](m map[K]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	slices.Sort(out)
	return out
}

func sorted(s ...string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

// The closed sets, written out in full: adding or removing a value without
// changing this test (and ADR 0009) fails here. A7 adds the three service
// actions and the service results; the power part of A7 (system_reboot,
// system_poweroff, update_running, already_requested, the target system) is
// G7.5 and must not be here yet.
func TestClosedSetsAfterA7ServicePart(t *testing.T) {
	wantActions := sorted("setup_completed", "login_succeeded", "login_failed", "logout", "password_changed",
		"password_change_failed", "request_rejected", "update_started", "update_finished",
		"service_start", "service_stop", "service_restart")
	if got := keys(actions); !slices.Equal(got, wantActions) {
		t.Errorf("actions %v\nwant %v", got, wantActions)
	}
	if got := keys(aggregated); !slices.Equal(got, sorted("login_failed", "request_rejected")) {
		t.Errorf("aggregated %v", got)
	}
	wantTargets := sorted("-", "setup", "session", "account/password", "device", "metrics", "events")
	if got := keys(targets); !slices.Equal(got, wantTargets) {
		t.Errorf("targets %v\nwant %v", got, wantTargets)
	}
	wantResults := sorted("success", "invalid_credentials", "invalid_setup_token", "rate_limited", "wrong_password",
		"policy_rejected", "error", "host_rejected", "origin_rejected", "csrf_failed",
		// A6
		"busy", "signature_invalid", "checksum_mismatch", "manifest_invalid", "downgrade_refused", "already_installed",
		"requirements_not_met", "helper_too_old", "installer_required", "confirmation_required", "key_changed",
		"unit_rejected", "components_damaged", "stage_expired", "no_space", "current_unhealthy", "cooldown", "io_error",
		"rolled_back", "components_rolled_back", "rollback_failed", "aborted", "unavailable",
		// A7, service part
		"unit_invalid", "unit_unknown", "unit_not_canonical", "unit_denied", "unit_masked", "unit_transient",
		"unit_oneshot", "unit_template", "refused_by_unit", "operation_not_offered", "already_in_state",
		"job_failed", "timeout", "state_unexpected")
	if got := keys(results); !slices.Equal(got, wantResults) {
		t.Errorf("results %v\nwant %v", got, wantResults)
	}
	for _, power := range []string{"system_reboot", "system_poweroff"} {
		if actions[Action(power)] {
			t.Errorf("%s is G7.5", power)
		}
	}
	for _, power := range []string{"update_running", "already_requested"} {
		if results[Result(power)] {
			t.Errorf("%s is G7.5", power)
		}
	}
	if targets["system"] {
		t.Error("the target system is G7.5")
	}
}

// Every code a service action can end with maps as A7 says: the broker codes
// of B2 that are results, and the service's own busy and unavailable, map to
// themselves; bad_request and internal become error; the power-only codes and
// anything unknown become error too.
func TestServiceResultMapping(t *testing.T) {
	same := []string{"success", "busy", "rate_limited", "unavailable", "unit_invalid", "unit_unknown",
		"unit_not_canonical", "unit_denied", "unit_masked", "unit_transient", "unit_oneshot", "unit_template",
		"refused_by_unit", "operation_not_offered", "already_in_state", "job_failed", "timeout", "state_unexpected"}
	for _, c := range same {
		if got := ServiceResult(c); got != Result(c) || !results[got] {
			t.Errorf("%s -> %s", c, got)
		}
	}
	for _, c := range []string{"bad_request", "internal", "accepted", "update_running", "already_requested", "", "x", "SUCCESS", "wrong_password"} {
		if got := ServiceResult(c); got != Failed {
			t.Errorf("%s -> %s, want error", c, got)
		}
	}
}

// The target pattern service/<unit>: the unit passes rule 1 of ADR 0011 B3.
func TestServiceTargetPattern(t *testing.T) {
	long := strings.Repeat("a", 247) + ".service" // 255 bytes
	for _, u := range []string{"ssh.service", "getty@tty1.service", "NetworkManager.service", "a@b\\x2dc.service", long} {
		if !validServiceTarget(ServiceTarget(u)) {
			t.Errorf("refused %q", u)
		}
	}
	for _, u := range []string{"", "a" + long, "a/b.service", "-a.service", "a.socket", "../a.service", "a b.service",
		"ä.service", "a\\x2D.service", "a.service\n", "service/a.service"} {
		if validServiceTarget(ServiceTarget(u)) {
			t.Errorf("accepted %q", u)
		}
	}
	// Uppercase letters are allowed by rule 1 (NetworkManager.service), but the prefix is exact.
	for _, tg := range []Target{"Service/a.service", "service/", "services/a.service", "service//a.service", "service"} {
		if validServiceTarget(tg) {
			t.Errorf("accepted target %q", tg)
		}
	}
	if len(long) != 255 || len("a"+long) != 256 {
		t.Fatal("vector lengths")
	}
}

// One entry per service action, with the fixed fields; a bad target is
// dropped and counted.
func TestServiceEventsAreWrittenOrDropped(t *testing.T) {
	f := newFixture(t)
	f.log.Record(Event{User: "admin-test", Action: ServiceRestart, Target: ServiceTarget("demo-app.service"), Result: ServiceResult("success"), CorrelationID: cid})
	f.log.Record(Event{User: "admin-test", Action: ServiceStop, Target: ServiceTarget("demo-app.service"), Result: ServiceResult("internal"), CorrelationID: cid})
	f.log.Record(Event{User: "admin-test", Action: ServiceStart, Target: ServiceTarget("../etc/passwd"), Result: Success, CorrelationID: cid})
	f.log.Record(Event{User: "admin-test", Action: ServiceStart, Target: ServiceTarget(strings.Repeat("a", 248) + ".service"), Result: Success, CorrelationID: cid})
	f.log.Close()
	lines := f.lines(t, FileName)
	if len(lines) != 2 {
		t.Fatalf("lines %v", lines)
	}
	if lines[0]["action"] != "service_restart" || lines[0]["target"] != "service/demo-app.service" || lines[0]["result"] != "success" || lines[0]["count"] != float64(1) {
		t.Errorf("first %v", lines[0])
	}
	if lines[1]["action"] != "service_stop" || lines[1]["result"] != "error" {
		t.Errorf("second %v", lines[1])
	}
	if s := f.log.Stats(); s.Dropped != 2 {
		t.Errorf("dropped %d, want 2", s.Dropped)
	}
}
