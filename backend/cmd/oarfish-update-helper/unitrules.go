package main

import (
	"net/netip"
	"strings"
)

// The rule tables of the unit lint (ADR 0010 U15 rule 3, amendment A15). They
// are compiled in: no file, no argument and no manifest member changes them. A
// new directive needs a release that carries new tables first (the two-step
// rule of U15 rule 3).

// The four unit names, from the closed list of U2. Each has a unit type, which
// decides the sections it may hold, and the one target its [Install] section
// may name ("" means the unit has no [Install] section).
const (
	unitService       = "oarfish.service"
	unitHelperSocket  = "oarfish-update-helper.socket"
	unitHelperService = "oarfish-update-helper.service"
	unitRecover       = "oarfish-update-recover.service"
)

// lintedUnits is the order in which stage lints the units of a release.
var lintedUnits = []string{unitHelperSocket, unitHelperService, unitRecover, unitService}

type unitKind struct {
	sections []string // the sections the unit type allows
	wantedBy string   // the only value WantedBy= may have
}

var unitKinds = map[string]unitKind{
	unitService:       {sections: []string{"Unit", "Service", "Install"}, wantedBy: "multi-user.target"},
	unitHelperSocket:  {sections: []string{"Unit", "Socket", "Install"}, wantedBy: "sockets.target"},
	unitHelperService: {sections: []string{"Unit", "Service", "Install"}},
	unitRecover:       {sections: []string{"Unit", "Service", "Install"}, wantedBy: "multi-user.target"},
}

// ---- the helper units: accepted canonical forms (D3) ------------------------

// maxHelperForms bounds each list: the current form and, during a two-step
// change, the next one.
const maxHelperForms = 2

// helperSandbox is the sandbox of U10, shared by the helper service and the
// recover unit.
const helperSandbox = `User=root
Group=root
UMask=0077
StateDirectory=oarfish-update
StateDirectoryMode=0700
ReadWritePaths=/opt/oarfish
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
IPAddressDeny=any
IPAddressAllow=localhost
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM
MemoryMax=64M
TasksMax=16
Nice=10`

// helperForms holds, for each helper unit, the ordered list of accepted
// canonical forms. A form is the unit text after normalisation: comment lines
// and blank lines removed, the values of Description= and Documentation=
// removed, lines joined by a newline, no newline at the end. The order of the
// sections and of the lines inside a section is part of the form. An entry is
// removed only by a later amendment or decision; in this step every list has
// one entry, the text the repository ships (TestInstallUnitsAreTheFirstForms).
var helperForms = map[string][]string{
	unitHelperSocket: {`[Unit]
Description=
Documentation=
[Socket]
ListenStream=/run/oarfish-update/helper.sock
SocketUser=root
SocketGroup=oarfish
SocketMode=0660
DirectoryMode=0755
Accept=no
Backlog=8
RemoveOnStop=yes
[Install]
WantedBy=sockets.target`},

	unitHelperService: {`[Unit]
Description=
Documentation=
OnFailure=oarfish-update-recover.service
StartLimitIntervalSec=10
StartLimitBurst=3
[Service]
Type=exec
ExecStart=/usr/libexec/oarfish-update-helper serve
Restart=on-failure
RestartSec=1
SuccessExitStatus=75
RestartForceExitStatus=75
RuntimeMaxSec=900
` + helperSandbox},

	unitRecover: {`[Unit]
Description=
Documentation=
DefaultDependencies=no
After=local-fs.target
Before=oarfish-update-helper.socket oarfish.service
[Service]
Type=oneshot
TimeoutStartSec=120
ExecStart=/opt/oarfish/components/known-good/oarfish-update-helper recover
` + helperSandbox + `
[Install]
WantedBy=multi-user.target`},
}

// ---- oarfish.service: required baseline and allowed extras -------------------

// serviceRule is one row of the table for oarfish.service. A baseline row must
// be present; an extra row may be. The value must pass ok. A directive without
// a row is rejected.
type serviceRule struct {
	section, key string
	baseline     bool
	ok           func(string) bool
}

func exactly(want string) func(string) bool {
	return func(v string) bool { return v == want }
}

func oneOf(values ...string) func(string) bool {
	return func(v string) bool {
		for _, w := range values {
			if v == w {
				return true
			}
		}
		return false
	}
}

// The bounds of the two timing extras, in whole seconds. RestartSec: at least
// 1 so that a crashing service does not spin, at most 10 so that a crash loop
// raises NRestarts well inside the 45 s health deadline (U6). TimeoutStopSec:
// at least 5 so that the service can end cleanly, at most 30 so that a stop
// and a start fit into the 60 s limit of one systemctl call.
const (
	restartSecMin, restartSecMax         = 1, 10
	timeoutStopSecMin, timeoutStopSecMax = 5, 30
)

// seconds accepts a plain decimal number of whole seconds within the bounds:
// digits only, no sign, no leading zero, no unit and no "infinity".
func seconds(lo, hi int) func(string) bool {
	return func(v string) bool {
		if len(v) == 0 || len(v) > 2 || v[0] == '0' {
			return false
		}
		n := 0
		for i := 0; i < len(v); i++ {
			if v[i] < '0' || v[i] > '9' {
				return false
			}
			n = n*10 + int(v[i]-'0')
		}
		return n >= lo && n <= hi
	}
}

// serviceExecPrefix is the exact program and the exact argument of ExecStart;
// a literal loopback IP address with a port follows it and nothing else.
const serviceExecPrefix = "/opt/oarfish/current/oarfish -listen "

// serviceExecStart accepts exactly serviceExecPrefix followed by a literal
// loopback IP address and port in its canonical spelling (127.0.0.0/8 or
// [::1], no zone, not an IPv4-mapped IPv6 address, a port that is not 0). Any
// prefix character of systemd (+ ! @ : -) fails the comparison of the prefix.
func serviceExecStart(v string) bool {
	rest, ok := strings.CutPrefix(v, serviceExecPrefix)
	if !ok {
		return false
	}
	ap, err := netip.ParseAddrPort(rest)
	if err != nil || ap.Port() == 0 || ap.String() != rest {
		return false
	}
	a := ap.Addr()
	return a.IsLoopback() && !a.Is4In6() && a.Zone() == ""
}

// serviceRules is the complete table. The baseline is every directive that
// install/oarfish.service sets, verbatim, except the five that U15 rule 3
// names as extras (TestServiceRulesMatchTheInstallUnit keeps the two equal).
var serviceRules = []serviceRule{
	{"Unit", "Description", false, unitText},
	{"Unit", "Documentation", false, unitText},

	{"Service", "Type", true, exactly("simple")},
	{"Service", "User", true, exactly("oarfish")},
	{"Service", "Group", true, exactly("oarfish")},
	{"Service", "ExecStart", true, serviceExecStart},
	{"Service", "Restart", false, oneOf("on-failure", "always")},
	{"Service", "RestartSec", false, seconds(restartSecMin, restartSecMax)},
	{"Service", "TimeoutStopSec", false, seconds(timeoutStopSecMin, timeoutStopSecMax)},
	{"Service", "StateDirectory", true, exactly("oarfish")},
	{"Service", "StateDirectoryMode", true, exactly("0700")},
	{"Service", "CacheDirectory", true, exactly("oarfish")},
	{"Service", "RuntimeDirectory", true, exactly("oarfish")},
	{"Service", "RuntimeDirectoryMode", true, exactly("0750")},
	{"Service", "UMask", true, exactly("0077")},
	{"Service", "NoNewPrivileges", true, exactly("yes")},
	{"Service", "CapabilityBoundingSet", true, exactly("")},
	{"Service", "AmbientCapabilities", true, exactly("")},
	{"Service", "ProtectSystem", true, exactly("strict")},
	{"Service", "ProtectHome", true, exactly("yes")},
	{"Service", "PrivateTmp", true, exactly("yes")},
	{"Service", "PrivateDevices", true, exactly("yes")},
	{"Service", "ProtectKernelTunables", true, exactly("yes")},
	{"Service", "ProtectKernelModules", true, exactly("yes")},
	{"Service", "ProtectKernelLogs", true, exactly("yes")},
	{"Service", "ProtectControlGroups", true, exactly("yes")},
	{"Service", "ProtectClock", true, exactly("yes")},
	{"Service", "ProtectHostname", true, exactly("yes")},
	{"Service", "ProtectProc", false, oneOf("invisible", "noaccess")},
	{"Service", "RestrictNamespaces", true, exactly("yes")},
	{"Service", "RestrictRealtime", true, exactly("yes")},
	{"Service", "RestrictSUIDSGID", true, exactly("yes")},
	{"Service", "LockPersonality", true, exactly("yes")},
	{"Service", "RestrictAddressFamilies", true, exactly("AF_UNIX AF_INET AF_INET6")},
	{"Service", "SystemCallArchitectures", true, exactly("native")},
	{"Service", "SystemCallFilter", true, exactly("@system-service")},
	{"Service", "SystemCallErrorNumber", true, exactly("EPERM")},

	{"Install", "WantedBy", true, exactly("multi-user.target")},
}
