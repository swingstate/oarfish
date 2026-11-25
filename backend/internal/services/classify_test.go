package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// brokerLists reads backend/cmd/oarfish-broker/unit.go as source and returns
// the literal contents of a unitList variable: the arguments of set(...) for
// exact, and the []string literals for families and prefixes.
func brokerList(t *testing.T, name string) (exact, families, prefixes []string) {
	t.Helper()
	path := filepath.Join("..", "..", "cmd", "oarfish-broker", "unit.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	strs := func(exprs []ast.Expr) []string {
		var out []string
		for _, e := range exprs {
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("%s: an element is not a string literal", name)
			}
			s, _ := strconv.Unquote(lit.Value)
			out = append(out, s)
		}
		return out
	}
	found := false
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			if len(vs.Names) != 1 || vs.Names[0].Name != name || len(vs.Values) != 1 {
				continue
			}
			cl, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s is not a composite literal", name)
			}
			found = true
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					t.Fatalf("%s: unkeyed field", name)
				}
				key := kv.Key.(*ast.Ident).Name
				switch key {
				case "exact":
					call, ok := kv.Value.(*ast.CallExpr)
					if !ok || call.Fun.(*ast.Ident).Name != "set" {
						t.Fatalf("%s.exact is not set(...)", name)
					}
					exact = strs(call.Args)
				case "families":
					families = strs(kv.Value.(*ast.CompositeLit).Elts)
				case "prefixes":
					prefixes = strs(kv.Value.(*ast.CompositeLit).Elts)
				default:
					t.Fatalf("%s has a field %s this test does not know; compare it by hand and extend the test", name, key)
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s not found in the broker's unit.go", name)
	}
	return exact, families, prefixes
}

func listOf(l unitList) (exact, families, prefixes []string) {
	for k := range l.exact {
		exact = append(exact, k)
	}
	return exact, slices.Clone(l.families), slices.Clone(l.prefixes)
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// The service's copies of the deny list (rule 10) and the restart-only list
// (rule 11) are the broker's, entry for entry. The broker's bytes are frozen
// in this step, so the two cannot share a package; this test keeps them equal.
func TestListsMatchTheBroker(t *testing.T) {
	for name, ours := range map[string]unitList{"denyList": denyList, "restartOnly": restartOnly} {
		be, bf, bp := brokerList(t, name)
		oe, of, op := listOf(ours)
		if !sameSet(be, oe) || !sameSet(bf, of) || !sameSet(bp, op) {
			t.Errorf("%s differs from the broker:\nbroker  %v %v %v\nservice %v %v %v", name, be, bf, bp, oe, of, op)
		}
	}
}

// The restart-only list is exactly the "No" rows of the rule 11 table: every
// entry of the kinds network, ssh and remote access except ModemManager.
func TestRestartOnlyIsTheNoRowsOfRule11(t *testing.T) {
	var exact, families []string
	for _, k := range criticalKinds {
		if k.kind == KindNetwork || k.kind == KindSSH || k.kind == KindRemoteAccess {
			for n := range k.list.exact {
				if n != "ModemManager.service" {
					exact = append(exact, n)
				}
			}
			families = append(families, k.list.families...)
		}
	}
	oe, of, op := listOf(restartOnly)
	if !sameSet(exact, oe) || !sameSet(families, of) || len(op) != 0 {
		t.Errorf("restart only %v %v, table %v %v", oe, of, exact, families)
	}
}

// Every entry of the deny list and every family and prefix: no action.
func TestDenyClassification(t *testing.T) {
	names := []string{
		"oarfish.service", "oarfish-update-helper.service", "oarfish-update-recover.service", "oarfish-broker.service",
		"dbus.service", "dbus-broker.service", "polkit.service", "emergency.service", "rescue.service", "debug-shell.service",
		"console-getty.service",
		"user@1000.service", "user@.service", "user-runtime-dir@1000.service", "getty@tty1.service", "serial-getty@ttyS0.service",
		"autovt@tty2.service", "container-getty@1.service", "sshd@demo-conn.service", "sshd-unix-local@demo.service",
		"systemd-journald.service", "systemd-logind.service", "systemd-timesyncd.service", "systemd-reboot.service",
		"systemd-fsck@dev-disk-by\\x2duuid-0000.service",
	}
	for _, n := range names {
		c := classify(n)
		if !c.denied {
			t.Errorf("%s not denied", n)
		}
		for _, active := range []string{"active", "inactive", "failed", "activating"} {
			if a := offeredActions(c, active); len(a) != 0 {
				t.Errorf("%s (%s) offers %v", n, active, a)
			}
		}
	}
	// Near misses are not denied: the prefix is exact and families need the @.
	for _, n := range []string{"oarfish-demo.service", "systemd.service", "my-systemd-helper.service", "getty.service", "user.service", "sshd.service", "dbus-demo.service"} {
		if classify(n).denied {
			t.Errorf("%s denied", n)
		}
	}
}

// Every restart-only entry and family: restart only, never stop or start.
func TestRestartOnlyClassification(t *testing.T) {
	names := []string{
		"NetworkManager.service", "networking.service", "dhcpcd.service", "wpa_supplicant.service", "avahi-daemon.service",
		"rpi-usb-gadget-ics.service", "ssh.service", "sshd.service", "tailscaled.service",
		"wpa_supplicant@wlan0.service", "wpa_supplicant-nl80211@wlan0.service", "wpa_supplicant-wired@eth0.service", "ppp@demo.service",
	}
	for _, n := range names {
		c := classify(n)
		if !c.restartOnly || c.denied || c.critical == "" {
			t.Errorf("%s: %+v", n, c)
		}
		for _, active := range []string{"active", "inactive", "failed", "activating", "reloading"} {
			if a := offeredActions(c, active); !slices.Equal(a, []string{ActionRestart}) {
				t.Errorf("%s (%s) offers %v", n, active, a)
			}
		}
	}
	// ModemManager is critical (network) but may be stopped.
	c := classify("ModemManager.service")
	if c.restartOnly || c.critical != KindNetwork || !slices.Contains(offeredActions(c, "active"), ActionStop) {
		t.Errorf("ModemManager: %+v", c)
	}
}

// The kinds of rule 11 and the buttons of an ordinary row.
func TestCriticalKindsAndActions(t *testing.T) {
	for n, kind := range map[string]string{
		"NetworkManager.service": KindNetwork, "ppp@demo.service": KindNetwork, "ssh.service": KindSSH,
		"tailscaled.service": KindRemoteAccess, "docker.service": KindContainer, "containerd.service": KindContainer,
		"podman.service": KindContainer, "chrony.service": KindTime, "ntp.service": KindTime, "ntpsec.service": KindTime,
		"demo-app.service": "", "systemd-timesyncd.service": "",
	} {
		if got := classify(n).critical; got != kind {
			t.Errorf("%s: %q, want %q", n, got, kind)
		}
	}
	plain := classify("demo-app.service")
	for active, want := range map[string][]string{
		"active":       {ActionStop, ActionRestart},
		"activating":   {ActionStart, ActionStop, ActionRestart},
		"reloading":    {ActionStart, ActionStop, ActionRestart},
		"inactive":     {ActionStart, ActionRestart},
		"failed":       {ActionStart, ActionRestart},
		"deactivating": {ActionStart, ActionRestart},
		"maintenance":  {ActionStart, ActionRestart},
	} {
		if got := offeredActions(plain, active); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", active, got, want)
		}
	}
	// Containers may be stopped, with the note.
	if !slices.Contains(offeredActions(classify("docker.service"), "active"), ActionStop) {
		t.Error("docker stop")
	}
	for _, a := range []string{"start", "stop", "restart"} {
		if !ValidAction(a) {
			t.Error(a)
		}
	}
	for _, a := range []string{"", "reload", "enable", "Start", "kill"} {
		if ValidAction(a) {
			t.Error(a)
		}
	}
}
