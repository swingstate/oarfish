package main

import "errors"

// Time since boot (ADR 0010 U6, U10): whole seconds of CLOCK_BOOTTIME. The
// wall clock is never used for a decision, and /proc/uptime is not read
// because the unit hides it (ProcSubset=pid, V12). bootSeconds is in
// clock_linux.go; a test replaces the whole function through the handler.
var errNoClock = errors.New("no boot clock")

// stageLifetime is the time a staged release stays valid (U5, U7 row 4).
const stageLifetimeS = 30 * 60

// cooldownMarker is a cooldown marker (U6, R1). It is declared in G6.5 and enforced in
// G6.6.
type cooldownMarker struct {
	SetAtUptimeS int64
	UntilUptimeS int64
}

// active reports whether the cooldownMarker still holds at uptime now. A cooldownMarker is
// void when the uptime is smaller than the uptime at which it was set: a
// reboot happened. The cooldownMarker ends at UntilUptimeS (exclusive). A zero cooldownMarker
// is never active.
func (m cooldownMarker) active(now int64) bool {
	if m.UntilUptimeS <= 0 || now < m.SetAtUptimeS {
		return false
	}
	return now < m.UntilUptimeS
}

// stageExpired reports whether a release staged at uptime stagedAt is too old
// at uptime now: 30 minutes or more, or a smaller uptime (a reboot happened).
func stageExpired(stagedAt, now int64) bool {
	return now < stagedAt || now-stagedAt >= stageLifetimeS
}
