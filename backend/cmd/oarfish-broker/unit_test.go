package main

import (
	"strings"
	"testing"
)

func TestParseShow(t *testing.T) {
	raw, err := parseShow([]byte(showOut(demo)))
	if err != nil {
		t.Fatal(err)
	}
	u, err := unitFacts(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := unitInfo{ID: demo, Names: []string{demo}, LoadState: "loaded", ActiveState: "active", Type: "exec",
		StartMonotonic: 1000000, MainPID: 4242}
	if u.ID != want.ID || len(u.Names) != 1 || u.LoadState != want.LoadState || u.ActiveState != want.ActiveState ||
		u.Type != want.Type || u.StartMonotonic != want.StartMonotonic || u.MainPID != want.MainPID || u.Transient {
		t.Fatalf("got %+v", u)
	}
	// Without the final newline, and with the keys in the order of --property=.
	var b strings.Builder
	for _, k := range showProperties {
		b.WriteString(k + "=" + raw[k] + "\n")
	}
	if _, err := parseShow([]byte(strings.TrimSuffix(b.String(), "\n"))); err != nil {
		t.Fatal(err)
	}
	// not-found keeps its shape (exit 0, A2 S7): parsed, then refused by rule 3.
	raw, err = parseShow([]byte(notFoundOut("gone.service")))
	if err != nil || raw["LoadState"] != "not-found" {
		t.Fatalf("not-found: %v %v", raw, err)
	}
	// An alias and an escaped instance in Names.
	raw, _ = parseShow([]byte(showOut(`a@x\x2dy.service`, "Names", `a@x\x2dy.service b.service`)))
	if u, err := unitFacts(raw); err != nil || len(u.Names) != 2 {
		t.Fatalf("names: %+v %v", u, err)
	}
}

// Every word of the closed sets is accepted; anything else is not.
func TestShowValueSets(t *testing.T) {
	for s := range activeStates {
		raw, _ := parseShow([]byte(showOut(demo, "ActiveState", s)))
		if _, err := unitFacts(raw); err != nil {
			t.Errorf("ActiveState=%s refused", s)
		}
	}
	for s := range serviceTypes {
		raw, _ := parseShow([]byte(showOut(demo, "Type", s)))
		if _, err := unitFacts(raw); err != nil {
			t.Errorf("Type=%s refused", s)
		}
	}
	for _, kv := range [][2]string{
		{"ActiveState", "Active"}, {"ActiveState", ""}, {"Type", ""}, {"Type", "Oneshot"},
		{"MainPID", ""}, {"MainPID", "01"}, {"MainPID", "4294967296"}, {"MainPID", "+1"}, {"MainPID", " 1"},
		{"ExecMainStartTimestampMonotonic", "18446744073709551616"}, {"Transient", "Yes"}, {"RefuseManualStop", ""},
		{"SubState", "Running"}, {"UnitFileState", "enabled runtime"}, {"Result", "a_b"},
	} {
		raw, err := parseShow([]byte(showOut(demo, kv[0], kv[1])))
		if err != nil {
			continue
		}
		if _, err := unitFacts(raw); err == nil {
			t.Errorf("%s=%q accepted", kv[0], kv[1])
		}
	}
}

func TestLists(t *testing.T) {
	for _, n := range []string{"user@.service", "sshd@.service", "systemd-anything.service"} {
		if !denyList.matches(n) {
			t.Errorf("%s not denied", n)
		}
	}
	for _, n := range []string{"users.service", "sshd.service", "ssh.service", "getty.service", "systemd.service", "my-systemd-x.service", "dbus-x.service"} {
		if denyList.matches(n) {
			t.Errorf("%s denied", n)
		}
	}
	for _, n := range []string{"ModemManager.service", "wpa_supplicant-other@x.service", "pppd.service"} {
		if restartOnly.matches(n) {
			t.Errorf("%s restart only", n)
		}
	}
}

func TestSplitTemplate(t *testing.T) {
	for n, want := range map[string][2]any{
		"a.service":     {false, ""},
		"a@.service":    {true, ""},
		"a@b.service":   {true, "b"},
		"a@b.c.service": {true, "b.c"},
	} {
		isT, inst := splitTemplate(n)
		if isT != want[0] || inst != want[1] {
			t.Errorf("%s: %v %q", n, isT, inst)
		}
	}
}

// FuzzParseShow: the parser never panics, and what it accepts has each of the
// 13 keys exactly once, with printable ASCII values.
func FuzzParseShow(f *testing.F) {
	f.Add([]byte(showOut(demo)))
	f.Add([]byte(notFoundOut(demo)))
	f.Add([]byte(showOut(demo, "Names", demo+" x.service")))
	f.Add([]byte("Id=a\nId=a\n"))
	f.Add([]byte("=\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		raw, err := parseShow(data)
		if err != nil {
			return
		}
		if len(raw) != len(showProperties) {
			t.Fatalf("accepted %d keys", len(raw))
		}
		for _, k := range showProperties {
			v, ok := raw[k]
			if !ok {
				t.Fatalf("missing %s", k)
			}
			for i := 0; i < len(v); i++ {
				if v[i] < 0x20 || v[i] > 0x7e {
					t.Fatalf("%s carries byte %#x", k, v[i])
				}
			}
		}
		u, err := unitFacts(raw)
		if err != nil {
			return
		}
		if !validUnitName(u.ID) || !activeStates[u.ActiveState] || !serviceTypes[u.Type] {
			t.Fatalf("accepted %+v", u)
		}
	})
}

// FuzzUnitName: whatever rule 1 accepts is ASCII, ends in .service, has no
// slash, no space, no "..", no leading "-" and only \xHH escapes.
func FuzzUnitName(f *testing.F) {
	for _, s := range []string{"a.service", `a@b\x2d.service`, "a@.service", "-a.service", "a/b.service", "a..service"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !validUnitName(s) {
			return
		}
		if len(s) > maxUnitName || !strings.HasSuffix(s, ".service") || strings.ContainsAny(s, "/ \t\n*?[]") ||
			strings.Contains(s, "..") || s[0] == '-' || strings.Count(s, "@") > 1 {
			t.Fatalf("accepted %q", s)
		}
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 || s[i] < 0x21 {
				t.Fatalf("accepted %q", s)
			}
			if s[i] == '\\' && (i+3 >= len(s) || s[i+1] != 'x' || !lowerHex(s[i+2]) || !lowerHex(s[i+3])) {
				t.Fatalf("accepted escape in %q", s)
			}
		}
	})
}
