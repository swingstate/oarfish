package main

import (
	"fmt"
	"slices"
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
	// An escaped instance with an alias, as systemctl prints them (A8).
	raw, _ = parseShow([]byte(showOut(`a@x\x2dy.service`, "Names", `"a@x\\x2dy.service" b.service`)))
	if u, err := unitFacts(raw); err != nil || !slices.Equal(u.Names, []string{`a@x\x2dy.service`, "b.service"}) {
		t.Fatalf("names: %+v %v", u, err)
	}
}

// fsckShow is the shape systemd 257 printed on the reference device for a
// systemd-fsck@ instance (G7.6c, finding A7.5), with a synthetic device path:
// Id as it is, the entry of Names in double quotes with the backslashes
// doubled.
const fsckUnit = `systemd-fsck@dev-disk-by\x2dpartuuid-00000000\x2d02.service`

const fsckShow = `Type=oneshot
Result=success
MainPID=0
ExecMainStartTimestampMonotonic=0
Id=systemd-fsck@dev-disk-by\x2dpartuuid-00000000\x2d02.service
Names="systemd-fsck@dev-disk-by\\x2dpartuuid-00000000\\x2d02.service"
LoadState=loaded
ActiveState=active
SubState=exited
UnitFileState=static
RefuseManualStart=no
RefuseManualStop=no
Transient=no
`

func TestShowEscapedNameFromDevice(t *testing.T) {
	raw, err := parseShow([]byte(fsckShow))
	if err != nil {
		t.Fatal(err)
	}
	u, err := unitFacts(raw)
	if err != nil {
		t.Fatalf("the device shape is refused: %v", err)
	}
	if u.ID != fsckUnit || !slices.Equal(u.Names, []string{fsckUnit}) {
		t.Fatalf("got %+v", u)
	}
	if fsckShow != showOut(fsckUnit, "Type", "oneshot", "MainPID", "0", "ExecMainStartTimestampMonotonic", "0",
		"SubState", "exited", "UnitFileState", "static") {
		t.Fatal("showOut does not print Names as systemd does")
	}
}

// The value of Names, read by systemd's quoting rule (A8): what is accepted
// and what it unquotes to. The quote, the space and the other characters
// that the rule escapes or quotes are unquoted correctly and then refused by
// rule 1, because no unit name holds them.
func TestSplitNames(t *testing.T) {
	for v, want := range map[string][]string{
		"a.service":                           {"a.service"},
		"a.service b.service":                 {"a.service", "b.service"},
		`"a@x\\x2dy.service"`:                 {`a@x\x2dy.service`},
		`"a@x\\x2dy.service" b.service`:       {`a@x\x2dy.service`, "b.service"},
		`b.service "a@x\\x2dy.service"`:       {"b.service", `a@x\x2dy.service`},
		`"a@\\x2d.service" "b@\\x2e.service"`: {`a@\x2d.service`, `b@\x2e.service`},
		`"a b.service"`:                       {"a b.service"},
		`"a\"b.service"`:                      {`a"b.service`},
		`"a\$b.service"`:                      {"a$b.service"},
		"\"a\\`b.service\"":                   {"a`b.service"},
		`"a*b.service"`:                       {"a*b.service"},
		`"a'b.service"`:                       {"a'b.service"},
		`"a\\\\b.service"`:                    {`a\\b.service`},
	} {
		got, err := splitNames(v)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: %q %v, want %q", v, got, err, want)
		}
		if sdNames(want...) != v {
			t.Errorf("%s: systemd would print %s", v, sdNames(want...))
		}
	}
	malformed := []string{
		"", " ", " a.service", "a.service ", "a.service  b.service",
		`"a@x\\x2dy.service`,               // unterminated quote
		`"a@x\\x2dy.service" "`,            // unterminated second entry
		`"a@x\x2dy.service"`,               // a backslash before x: a bad escape
		`"a@x\\x2dy.service\"`,             // the closing quote escaped
		`"a\tb.service"`, `"a\nb.service"`, // C escapes are not written for printable characters
		`"a@x\\x2dy.service"b.service`,                       // a quoted word joined to another
		`b.service"a@x\\x2dy.service"`,                       // and the other way round
		`"a"b".service"`, `"a$b.service"`, "\"a`b.service\"", // a lone character of SHELL_NEED_ESCAPE
		`"a.service"`, `""`, // quoted without need
		`a@x\x2dy.service`, `a@x\\x2dy.service`, // unquoted with a backslash
		`a"b.service`, "a'b.service", "a*b.service", "a;b.service", "a(b).service",
	}
	for _, v := range malformed {
		if got, err := splitNames(v); err == nil {
			t.Errorf("%s accepted: %q", v, got)
		}
	}
	// The bound: maxNames entries, quoted or not, and not one more.
	many := make([]string, maxNames+1)
	for i := range many {
		many[i] = fmt.Sprintf(`n@\x%02x.service`, i%256)
	}
	if got, err := splitNames(sdNames(many[:maxNames]...)); err != nil || len(got) != maxNames {
		t.Errorf("%d names: %d %v", maxNames, len(got), err)
	}
	if _, err := splitNames(sdNames(many...)); err == nil {
		t.Errorf("%d names accepted", maxNames+1)
	}
}

// After unquoting, rule 1 still holds for every entry of Names: a correctly
// quoted entry that is not a unit name fails the whole read.
func TestShowNamesRule1AfterUnquoting(t *testing.T) {
	for _, n := range []string{
		"a b.service", `a"b.service`, "a$b.service", "a`b.service", "a*b.service", `a\\b.service`, `a\x2Db.service`,
		`a\.service`, "a;b.service", `x@a\x2d`,
	} {
		raw, err := parseShow([]byte(showOut(demo, "Names", sdNames(demo, n))))
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if u, err := unitFacts(raw); err == nil {
			t.Errorf("%s accepted: %q", n, u.Names)
		}
	}
	// Id is printed as it is, never quoted: a quoted Id fails rule 1.
	raw, _ := parseShow([]byte(showOut(fsckUnit, "Id", sdQuote(fsckUnit))))
	if _, err := unitFacts(raw); err == nil {
		t.Error("quoted Id accepted")
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
	for _, n := range []string{"ModemManager.service", "wpa_supplicant-other@x.service", "pppd.service",
		"netplan-ovs-cleanup.service", "netplan-wpa.service", "my-netplan-wpa-wlan0.service", "xnetplan-wpa-wlan0.service"} {
		if restartOnly.matches(n) {
			t.Errorf("%s restart only", n)
		}
	}
	if !restartOnly.matches("netplan-wpa-wlan0.service") {
		t.Error("netplan-wpa-wlan0.service is not restart only")
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
// 13 keys exactly once, with printable ASCII values. What unitFacts accepts
// has a valid Id and only valid names in Names, and systemd's quoting of
// those names gives back the value of Names exactly (A8).
func FuzzParseShow(f *testing.F) {
	f.Add([]byte(showOut(demo)))
	f.Add([]byte(notFoundOut(demo)))
	f.Add([]byte(showOut(demo, "Names", demo+" x.service")))
	f.Add([]byte(fsckShow))
	f.Add([]byte(showOut(`a@x\x2dy.service`, "Names", `"a@x\\x2dy.service" "b@\\x2e.service" c.service`)))
	f.Add([]byte(showOut(demo, "Names", `"a b.service" "a\"b" "a\\x2d`)))
	f.Add([]byte(showOut(demo, "Names", `"a@x\x2dy.service"b`)))
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
		if len(u.Names) == 0 || len(u.Names) > maxNames {
			t.Fatalf("accepted %d names", len(u.Names))
		}
		for _, n := range u.Names {
			if !validUnitName(n) {
				t.Fatalf("accepted name %q", n)
			}
		}
		if sdNames(u.Names...) != raw["Names"] {
			t.Fatalf("Names %q read as %q", raw["Names"], u.Names)
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
