package main

// The hook for the capability check of G8 (ADR 0011 B7). G7 has the constant
// policy corePolicy. The check runs before any unit lookup, so a refused
// caller learns nothing about units. Caller identity comes only from the
// socket: the request has no member that names a caller or a plugin.

// Caller is who connected, as the broker determined it from SO_PEERCRED.
type Caller struct {
	UID    uint32
	Plugin string // "" for the core; set only by the broker from the peer uid (G8)
}

// Operation is one operation of the allowlist that needs a policy decision.
// status needs none (B2: peer only).
type Operation string

// Policy decides whether a caller may run an operation. unit is "" for the
// power operations; for a service operation it is the requested name before
// any check of B3.
type Policy interface {
	Allow(c Caller, op Operation, unit string) (ok bool, refusal code)
}

// corePolicy allows every operation of the allowlist for the core caller and
// nothing for any other caller. In G7 no other caller passes the peer check,
// so its refusal is not reachable through the socket; it answers internal,
// because the closed set of B2 has no capability code yet (A4).
type corePolicy struct{ coreUID uint32 }

func newCorePolicy(coreUID uint32) Policy { return corePolicy{coreUID: coreUID} }

func (p corePolicy) Allow(c Caller, op Operation, _ string) (bool, code) {
	if c.Plugin != "" || c.UID != p.coreUID {
		return false, codeInternal
	}
	switch op {
	case opServiceStart, opServiceStop, opServiceRestart, opSystemReboot, opSystemPoweroff:
		return true, ""
	}
	return false, codeInternal
}
