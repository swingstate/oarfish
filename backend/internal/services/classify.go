package services

import "strings"

// The classification of a row (ADR 0011 B3 rules 10 and 11 as A3.4 states
// them). The broker enforces the deny list and the restart-only part itself;
// what the page shows from these lists is a convenience, never the boundary.
// The lists are copies of backend/cmd/oarfish-broker/unit.go, whose bytes are
// frozen in this step, so there is no shared package; TestListsMatchTheBroker
// reads that file and fails when a copy drifts.

// unitList is an exact name, a template family `prefix@` that matches the
// template and every instance of it, or a name prefix.
type unitList struct {
	exact    map[string]bool
	families []string
	prefixes []string
}

func (l unitList) matches(name string) bool {
	if l.exact[name] {
		return true
	}
	for _, f := range l.families {
		if strings.HasPrefix(name, f) {
			return true
		}
	}
	for _, p := range l.prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// denyList is rule 10 (A3.4): no action is offered for these units.
var denyList = unitList{
	exact: set(
		"oarfish.service", "oarfish-update-helper.service", "oarfish-update-recover.service", "oarfish-broker.service",
		"dbus.service", "dbus-broker.service", "polkit.service",
		"emergency.service", "rescue.service", "debug-shell.service",
		"console-getty.service",
	),
	families: []string{
		"user@", "user-runtime-dir@",
		"getty@", "serial-getty@", "autovt@", "container-getty@",
		"sshd@", "sshd-unix-local@",
	},
	prefixes: []string{"systemd-"},
}

// restartOnly is the part of rule 11 marked "No" in the column "Stop
// offered": restart only, no stop.
var restartOnly = unitList{
	exact: set(
		"NetworkManager.service", "networking.service", "dhcpcd.service", "wpa_supplicant.service",
		"avahi-daemon.service", "rpi-usb-gadget-ics.service",
		"ssh.service", "sshd.service",
		"tailscaled.service",
	),
	families: []string{"wpa_supplicant@", "wpa_supplicant-nl80211@", "wpa_supplicant-wired@", "ppp@"},
}

// The kinds of rule 11 (ORB-SVC-003): each has its own note on the page.
const (
	KindNetwork      = "network"
	KindSSH          = "ssh"
	KindRemoteAccess = "remote_access"
	KindContainer    = "container"
	KindTime         = "time"
)

// criticalKinds is the table of rule 11, every row, in the order of the table.
var criticalKinds = []struct {
	kind string
	list unitList
}{
	{KindNetwork, unitList{
		exact: set("NetworkManager.service", "networking.service", "dhcpcd.service", "wpa_supplicant.service",
			"ModemManager.service", "avahi-daemon.service", "rpi-usb-gadget-ics.service"),
		families: []string{"wpa_supplicant@", "wpa_supplicant-nl80211@", "wpa_supplicant-wired@", "ppp@"},
	}},
	{KindSSH, unitList{exact: set("ssh.service", "sshd.service")}},
	{KindRemoteAccess, unitList{exact: set("tailscaled.service")}},
	{KindContainer, unitList{exact: set("docker.service", "containerd.service", "podman.service")}},
	{KindTime, unitList{exact: set("chrony.service", "ntp.service", "ntpsec.service")}},
}

// The actions of the page and the route (closed set).
const (
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
)

// ValidAction reports whether a is one of the three actions.
func ValidAction(a string) bool { return a == ActionStart || a == ActionStop || a == ActionRestart }

// classification is what the lists say about a name.
type classification struct {
	denied      bool
	restartOnly bool
	critical    string // a kind of rule 11, or ""
}

func classify(name string) classification {
	c := classification{denied: denyList.matches(name), restartOnly: restartOnly.matches(name)}
	for _, k := range criticalKinds {
		if k.list.matches(name) {
			c.critical = k.kind
			break
		}
	}
	return c
}

// offeredActions are the buttons of a row. A denied row has none. A
// restart-only row offers restart only (rule 11: what can cut the access is
// only restarted). Otherwise: start when the unit is not active, stop when it
// is active, activating or reloading, and restart always. The broker decides
// again; a one-shot or transient unit gets buttons here and the broker refuses
// them (the read path does not know the type, A3.3).
func offeredActions(c classification, active string) []string {
	acts := []string{}
	switch {
	case c.denied:
	case c.restartOnly:
		acts = append(acts, ActionRestart)
	default:
		if active != "active" {
			acts = append(acts, ActionStart)
		}
		if active == "active" || active == "activating" || active == "reloading" {
			acts = append(acts, ActionStop)
		}
		acts = append(acts, ActionRestart)
	}
	return acts
}
