package main

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ---- B3 rule 1: syntax -----------------------------------------------------

const maxUnitName = 255

// unitPattern is the pattern of B3 rule 1 with one difference: the instance of
// a template may be empty here, so that rule 2 can answer unit_template for
// `name@.service` instead of unit_invalid (A4). A backslash is checked
// separately by validEscapes.
var unitPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]*(@[A-Za-z0-9:_.\\-]*)?\.service$`)

// validUnitName is rule 1 of B3: ASCII only, 1 to 255 bytes, the pattern, no
// `..`, and a backslash only as `\x` followed by two lowercase hexadecimal
// digits. The pattern already excludes `/`, a space, a leading `-` and every
// unit type other than `.service`.
func validUnitName(s string) bool {
	if len(s) < 1 || len(s) > maxUnitName {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return !strings.Contains(s, "..") && unitPattern.MatchString(s) && validEscapes(s)
}

func validEscapes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			continue
		}
		if i+3 >= len(s) || s[i+1] != 'x' || !lowerHex(s[i+2]) || !lowerHex(s[i+3]) {
			return false
		}
		i += 3
	}
	return true
}

func lowerHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' }

// splitTemplate returns whether a valid name is a template or an instance of
// one, and its instance string ("" for `name@.service`).
func splitTemplate(name string) (isTemplate bool, instance string) {
	at := strings.IndexByte(name, '@')
	if at < 0 {
		return false, ""
	}
	return true, strings.TrimSuffix(name[at+1:], ".service")
}

// ---- B3 rules 10 and 11: the compiled-in lists (A3.4) -----------------------

// A list entry is an exact name, or a template family `prefix@` that matches
// the template and every instance of it.
type unitList struct {
	exact    map[string]bool
	families []string // "user@" matches user@.service and user@1000.service
	prefixes []string // "systemd-" matches every name that begins with it
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

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// denyList is rule 10 of B3 as A3.4 states it: refused for every operation
// with unit_denied, checked against Id and every entry of Names.
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

// restartOnly is the part of rule 11 of B3 (A3.4) that the broker enforces:
// the units marked "No" in the column "Stop offered". service.stop of them is
// operation_not_offered; service.restart is allowed. The warnings of rule 11
// for the other kinds are shown by the page (G7.4) and change nothing here.
var restartOnly = unitList{
	exact: set(
		"NetworkManager.service", "networking.service", "dhcpcd.service", "wpa_supplicant.service",
		"avahi-daemon.service", "rpi-usb-gadget-ics.service",
		"ssh.service", "sshd.service",
		"tailscaled.service",
	),
	families: []string{"wpa_supplicant@", "wpa_supplicant-nl80211@", "wpa_supplicant-wired@", "ppp@"},
}

// anyMatches reports whether l matches the Id or any entry of Names (rule 4).
func (l unitList) anyMatches(u unitInfo) bool {
	if l.matches(u.ID) {
		return true
	}
	for _, n := range u.Names {
		if l.matches(n) {
			return true
		}
	}
	return false
}

// ---- the show shape and its parser (B2, A3.2 P-B2-3) -----------------------

// showProperties are the 13 properties of the show shape of B2, in the order
// of --property=. systemctl prints them in its own order (A2 S7), so the
// output is parsed by key.
var showProperties = []string{
	"Id", "Names", "LoadState", "ActiveState", "SubState", "Result", "Type", "Transient",
	"RefuseManualStart", "RefuseManualStop", "UnitFileState", "ExecMainStartTimestampMonotonic", "MainPID",
}

// The closed set of ActiveState values. Only these words reach an answer.
const (
	stateActive   = "active"
	stateInactive = "inactive"
	stateFailed   = "failed"
)

var activeStates = set(stateActive, "reloading", stateInactive, stateFailed, "activating", "deactivating", "maintenance", "refreshing")

// The closed set of service types. Any other value of Type fails closed.
var serviceTypes = set("simple", "exec", "forking", "oneshot", "dbus", "notify", "notify-reload", "idle")

const maxNames = 64

var (
	errShowShape = errors.New("show output has not the expected shape")
	wordPattern  = regexp.MustCompile(`^[a-z0-9-]{0,64}$`)
)

// rawShow is the output of the show shape split by key: each of the 13 keys
// exactly once, nothing else.
type rawShow map[string]string

// parseShow checks the shape of the output: lines KEY=VALUE separated by
// newlines (one final newline allowed), every key one of the 13, each exactly
// once, every value printable ASCII. Anything else is errShowShape (internal,
// A3 P-B2-3). The values are interpreted by unitFacts.
func parseShow(out []byte) (rawShow, error) {
	out = bytes.TrimSuffix(out, []byte("\n"))
	lines := bytes.Split(out, []byte("\n"))
	if len(lines) != len(showProperties) {
		return nil, errShowShape
	}
	m := rawShow{}
	for _, l := range lines {
		k, v, ok := bytes.Cut(l, []byte("="))
		if !ok {
			return nil, errShowShape
		}
		key := string(k)
		if !isShowKey(key) {
			return nil, errShowShape
		}
		if _, dup := m[key]; dup {
			return nil, errShowShape
		}
		for _, c := range v {
			if c < 0x20 || c > 0x7e {
				return nil, errShowShape
			}
		}
		m[key] = string(v)
	}
	return m, nil
}

func isShowKey(k string) bool {
	for _, p := range showProperties {
		if k == p {
			return true
		}
	}
	return false
}

// unitInfo is what the broker knows about a unit, from its own show read only.
type unitInfo struct {
	ID                string
	Names             []string
	LoadState         string
	ActiveState       string
	Type              string
	Transient         bool
	RefuseManualStart bool
	RefuseManualStop  bool
	StartMonotonic    uint64
	MainPID           uint64
}

// unitFacts interprets a parsed show output. Every value must be of its
// closed set or form; anything else is errShowShape. LoadState is returned
// as it is: rule 3 decides on it.
func unitFacts(m rawShow) (unitInfo, error) {
	u := unitInfo{ID: m["Id"], LoadState: m["LoadState"], ActiveState: m["ActiveState"], Type: m["Type"]}
	if !validUnitName(u.ID) || !activeStates[u.ActiveState] || !serviceTypes[u.Type] {
		return unitInfo{}, errShowShape
	}
	names, err := splitNames(m["Names"])
	if err != nil {
		return unitInfo{}, err
	}
	for _, n := range names {
		if !validUnitName(n) {
			return unitInfo{}, errShowShape
		}
	}
	u.Names = names
	for _, k := range []string{"LoadState", "SubState", "Result", "UnitFileState"} {
		if !wordPattern.MatchString(m[k]) {
			return unitInfo{}, errShowShape
		}
	}
	if u.Transient, err = yesNo(m["Transient"]); err != nil {
		return unitInfo{}, err
	}
	if u.RefuseManualStart, err = yesNo(m["RefuseManualStart"]); err != nil {
		return unitInfo{}, err
	}
	if u.RefuseManualStop, err = yesNo(m["RefuseManualStop"]); err != nil {
		return unitInfo{}, err
	}
	if u.StartMonotonic, err = decimal(m["ExecMainStartTimestampMonotonic"], 64); err != nil {
		return unitInfo{}, err
	}
	if u.MainPID, err = decimal(m["MainPID"], 32); err != nil {
		return unitInfo{}, err
	}
	return u, nil
}

// ---- the quoting of Names (ADR 0011 A8) -------------------------------------

// systemctl show prints a string-array property such as Names as its entries
// separated by one space, each passed through shell_maybe_quote(s, 0)
// (systemd 257, src/shared/bus-print-properties.c and src/basic/escape.c). A
// string-valued property such as Id is printed as it is. shell_maybe_quote
// leaves an entry alone unless it holds a control character, invalid UTF-8,
// white space or a character of SHELL_NEED_QUOTES; then it encloses the
// entry in double quotes and puts a backslash before each character of
// SHELL_NEED_ESCAPE. A backslash of a `\xHH` unit name escape is such a
// character, so `a@x\x2dy.service` is printed as `"a@x\\x2dy.service"`.
// Control characters, which it would write as C escapes, cannot reach here:
// parseShow allows printable ASCII only.
const (
	shellNeedEscape = "\"\\`$"                                          // SHELL_NEED_ESCAPE (src/basic/escape.h)
	shellNeedQuotes = " \t\n\r" + shellNeedEscape + "*?[" + "'()<>|&;!" // WHITESPACE, SHELL_NEED_QUOTES (escape.h, string-util.h)
)

// splitNames is the inverse of that rule for the value of Names: 1 to
// maxNames entries separated by single spaces, each either a plain word with
// no character of shellNeedQuotes, or a quoted word that systemd would have
// quoted (it holds a character of shellNeedQuotes) whose only escapes are a
// backslash before a character of shellNeedEscape, and in which no other
// character of shellNeedEscape stands alone. Anything else (an empty entry,
// an unterminated quote, a bad escape, a quoted word joined to the next one)
// is errShowShape. The entries come back unquoted; rule 1 is the caller's.
func splitNames(v string) ([]string, error) {
	var names []string
	for i := 0; ; i++ {
		if len(names) == maxNames {
			return nil, errShowShape
		}
		var w string
		var err error
		if i < len(v) && v[i] == '"' {
			w, i, err = quotedName(v, i+1)
		} else {
			w, i, err = plainName(v, i)
		}
		if err != nil {
			return nil, err
		}
		names = append(names, w)
		if i == len(v) {
			return names, nil
		}
		if v[i] != ' ' {
			return nil, errShowShape
		}
	}
}

// plainName reads an unquoted entry from v[i:] up to the next space.
func plainName(v string, i int) (string, int, error) {
	j := i
	for ; j < len(v) && v[j] != ' '; j++ {
		if strings.IndexByte(shellNeedQuotes, v[j]) >= 0 {
			return "", 0, errShowShape
		}
	}
	if j == i {
		return "", 0, errShowShape
	}
	return v[i:j], j, nil
}

// quotedName reads a quoted entry whose opening quote is v[i-1], and returns
// it unquoted with the index after its closing quote.
func quotedName(v string, i int) (string, int, error) {
	var b strings.Builder
	needed := false
	for i < len(v) {
		c := v[i]
		switch {
		case c == '"':
			if !needed {
				return "", 0, errShowShape
			}
			return b.String(), i + 1, nil
		case c == '\\':
			if i+1 == len(v) || strings.IndexByte(shellNeedEscape, v[i+1]) < 0 {
				return "", 0, errShowShape
			}
			b.WriteByte(v[i+1])
			needed = true
			i += 2
			continue
		case strings.IndexByte(shellNeedEscape, c) >= 0:
			return "", 0, errShowShape
		case strings.IndexByte(shellNeedQuotes, c) >= 0:
			needed = true
		}
		b.WriteByte(c)
		i++
	}
	return "", 0, errShowShape
}

func yesNo(s string) (bool, error) {
	switch s {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	}
	return false, errShowShape
}

// decimal accepts only plain digits: no sign, no space, no leading zero
// except "0".
func decimal(s string, bits int) (uint64, error) {
	if s == "" || len(s) > 20 || (len(s) > 1 && s[0] == '0') {
		return 0, errShowShape
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errShowShape
		}
	}
	n, err := strconv.ParseUint(s, 10, bits)
	if err != nil {
		return 0, errShowShape
	}
	return n, nil
}
