package main

import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// The tests of the unit lint (ADR 0010 U15 rule 3, amendment A15).

const installDir = "../../../install/"

// installUnit reads a unit file of the repository's install/ directory.
func installUnit(t testing.TB, name string) string {
	t.Helper()
	data, err := os.ReadFile(installDir + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func wantAccepted(t *testing.T, name, text string) {
	t.Helper()
	if res := lintUnit(name, []byte(text)); !res.Accepted || res.Reason != "" {
		t.Fatalf("%s: rejected with %q, want accepted", name, res.Reason)
	}
}

func wantRejected(t *testing.T, name, text string, want lintReason) {
	t.Helper()
	res := lintUnit(name, []byte(text))
	if res.Accepted || res.Reason != want {
		t.Fatalf("%s: accepted=%v reason=%q, want rejected with %q", name, res.Accepted, res.Reason, want)
	}
}

// replaceLine replaces the one line of text that equals old.
func replaceLine(t *testing.T, text, old, new string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	n := 0
	for i, l := range lines {
		if l == old {
			lines[i] = new
			n++
		}
	}
	if n != 1 {
		t.Fatalf("line %q found %d times", old, n)
	}
	return strings.Join(lines, "\n")
}

// dropLine removes the one line of text that equals old.
func dropLine(t *testing.T, text, old string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	var out []string
	for _, l := range lines {
		if l != old {
			out = append(out, l)
		}
	}
	if len(out) != len(lines)-1 {
		t.Fatalf("line %q found %d times", old, len(lines)-len(out))
	}
	return strings.Join(out, "\n")
}

// afterLine inserts add after the one line that equals anchor.
func afterLine(t *testing.T, text, anchor, add string) string {
	t.Helper()
	return replaceLine(t, text, anchor, anchor+"\n"+add)
}

// ---- the units of the repository ---------------------------------------------

// Every unit in install/ passes the lint, so the rules and the shipped units
// cannot drift apart.
func TestInstallUnitsPassTheLint(t *testing.T) {
	for _, name := range lintedUnits {
		wantAccepted(t, name, installUnit(t, name))
	}
	if len(lintedUnits) != 4 || len(unitKinds) != 4 {
		t.Fatalf("%d linted units, %d kinds", len(lintedUnits), len(unitKinds))
	}
	// The four names are the unit names of the closed list of U2.
	for _, name := range lintedUnits {
		found := false
		for _, c := range componentNames {
			found = found || c == name
		}
		if !found || unitKinds[name].sections == nil {
			t.Errorf("%s is not a unit of the closed list", name)
		}
	}
}

// Each helper unit of install/ normalises to the first accepted form of its
// list; every list has exactly one entry in this step and never more than two
// (D3).
func TestInstallUnitsAreTheFirstForms(t *testing.T) {
	if len(helperForms) != 3 {
		t.Fatalf("%d helper units have forms", len(helperForms))
	}
	for _, name := range []string{unitHelperSocket, unitHelperService, unitRecover} {
		forms := helperForms[name]
		if len(forms) != 1 || len(forms) > maxHelperForms {
			t.Fatalf("%s has %d accepted forms, want 1", name, len(forms))
		}
		lines, reason := parseUnit(unitKinds[name], []byte(installUnit(t, name)))
		if reason != "" {
			t.Fatalf("%s: %s", name, reason)
		}
		if got := normalisedUnit(lines); got != forms[0] {
			t.Errorf("%s normalises to\n%s\nwant the first form\n%s", name, got, forms[0])
		}
		// A form is itself a unit text that the lint accepts.
		wantAccepted(t, name, forms[0])
	}
	// The lines that the amendments A1, A9 and A14 added are in the forms.
	for name, lines := range map[string][]string{
		unitHelperService: {"SuccessExitStatus=75", "RestartForceExitStatus=75"},
		unitRecover:       {"TimeoutStartSec=120", "DefaultDependencies=no", "After=local-fs.target"},
	} {
		for _, l := range lines {
			if !strings.Contains("\n"+helperForms[name][0]+"\n", "\n"+l+"\n") {
				t.Errorf("the form of %s has no line %s", name, l)
			}
		}
	}
	if maxHelperForms != 2 {
		t.Fatalf("at most %d forms, want 2", maxHelperForms)
	}
}

// The extras of oarfish.service that U15 rule 3 names; everything else that
// install/oarfish.service sets is baseline.
var namedExtras = map[string]bool{
	"Unit|Description": true, "Unit|Documentation": true,
	"Service|Restart": true, "Service|RestartSec": true, "Service|TimeoutStopSec": true,
}

// The baseline of the table is the set of directives of install/oarfish.service
// minus the named extras, taken over verbatim, and the extras of the table are
// the named ones plus ProtectProc.
func TestServiceRulesMatchTheInstallUnit(t *testing.T) {
	lines, reason := parseUnit(unitKinds[unitService], []byte(installUnit(t, unitService)))
	if reason != "" {
		t.Fatal(reason)
	}
	inFile := map[string]string{}
	for _, l := range lines {
		if l.key != "" {
			inFile[l.section+"|"+l.key] = l.value
		}
	}
	var baseline, extras []string
	seen := map[string]bool{}
	for _, r := range serviceRules {
		id := r.section + "|" + r.key
		if seen[id] {
			t.Errorf("%s has two rules", id)
		}
		seen[id] = true
		if r.baseline {
			baseline = append(baseline, id)
			v, ok := inFile[id]
			if !ok {
				t.Errorf("baseline %s is not set by install/oarfish.service", id)
			} else if !r.ok(v) {
				t.Errorf("baseline %s refuses the value of install/oarfish.service", id)
			}
			if namedExtras[id] {
				t.Errorf("%s is named as an extra by U15 rule 3 and is baseline in the table", id)
			}
		} else {
			extras = append(extras, id)
		}
	}
	for id := range inFile {
		if !seen[id] {
			t.Errorf("install/oarfish.service sets %s, which has no rule", id)
		} else if !namedExtras[id] && !contains(baseline, id) {
			t.Errorf("install/oarfish.service sets %s, which is neither baseline nor a named extra", id)
		}
	}
	sort.Strings(extras)
	if got := strings.Join(extras, " "); got != "Service|ProtectProc Service|Restart Service|RestartSec Service|TimeoutStopSec Unit|Description Unit|Documentation" {
		t.Errorf("extras: %s", got)
	}
	if len(baseline) != 32 || len(serviceRules) != 38 {
		t.Errorf("%d baseline rules of %d", len(baseline), len(serviceRules))
	}
	if restartSecMin != 1 || restartSecMax != 10 || timeoutStopSecMin != 5 || timeoutStopSecMax != 30 {
		t.Error("the bounds of RestartSec and TimeoutStopSec changed; ADR 0010 A15 states them")
	}
}

func contains(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}

// ---- parsing: one rejected case per forbidden construct -----------------------

func TestLintParsing(t *testing.T) {
	for _, name := range lintedUnits {
		t.Run(name, func(t *testing.T) {
			base := installUnit(t, name)
			type tc struct {
				what string
				text string
				want lintReason // empty: accepted
			}
			pad := func(n int) string { // base padded with a comment to exactly n bytes
				return base + "#" + strings.Repeat("x", n-len(base)-2) + "\n"
			}
			foreign := "Socket"
			if name == unitHelperSocket {
				foreign = "Service"
			}
			tests := []tc{
				{"a continuation line", afterLine(t, base, "[Unit]", "After=a.target \\\n b.target"), lintContinuation},
				{"a trailing backslash on the last line", base + `Foo=bar\`, lintContinuation},
				{"a trailing backslash followed by spaces", afterLine(t, base, "[Unit]", `Foo=bar\  `), lintContinuation},
				{"a trailing backslash in a comment", afterLine(t, base, "[Unit]", `# comment \`), lintContinuation},
				{"a duplicate key", afterLine(t, base, "[Unit]", "Documentation=two"), lintDuplicateKey},
				{"an unknown section", base + "[Timer]\nOnBootSec=1\n", lintSection},
				{"a section of another unit type", base + "[" + foreign + "]\n", lintSection},
				{"a section twice", base + "[Unit]\n", lintSection},
				{"a specifier", replaceLine(t, base, "[Unit]", "[Unit]\nAssertPathExists=%t"), lintSpecifier},
				{"a specifier in Description=", strings.Replace(base, "Description=", "Description=%n ", 1), lintSpecifier},
				{"a percent sign in a comment", "# 100 % sure\n" + base, ""},
				{"a 17 KiB file", pad(17 << 10), lintTooLarge},
				{"16 KiB and one byte", pad(16<<10 + 1), lintTooLarge},
				{"exactly 16 KiB", pad(16 << 10), ""},
				{"a NUL byte", base + "#\x00\n", lintNUL},
				{"a NUL byte in a value", strings.Replace(base, "Description=", "Description=\x00", 1), lintNUL},
				{"invalid UTF-8", base + "# \xff\xfe\n", lintNotUTF8},
				{"a truncated UTF-8 sequence", base + "# \xc3", lintNotUTF8},
				{"a carriage return", strings.ReplaceAll(base, "\n", "\r\n"), lintControl},
				{"a tab", base + "#\tcomment\n", lintControl},
				{"an escape character", base + "# \x1b[0m\n", lintControl},
				{"a DEL character", base + "# \x7f\n", lintControl},
				{"a Description of 201 characters", replaceDescription(t, base, strings.Repeat("d", 201)), lintText},
				{"a Description of 200 characters", replaceDescription(t, base, strings.Repeat("d", 200)), ""},
				{"a Documentation of 201 characters", replaceKey(t, base, "Documentation", strings.Repeat("d", 201)), lintText},
				{"a non-ASCII character in Description=", replaceDescription(t, base, "Aktualisierung für Oarfish"), lintSyntax},
				{"a non-ASCII character in a comment", "# für Oarfish\n" + base, ""},
				{"a key=value line before the first section", "Foo=bar\n" + base, lintSyntax},
				{"a line without an equals sign", afterLine(t, base, "[Unit]", "JustAWord"), lintSyntax},
				{"a line that starts with a space", afterLine(t, base, "[Unit]", " Foo=bar"), lintSyntax},
				{"an indented comment", afterLine(t, base, "[Unit]", " # comment"), lintSyntax},
				{"a space before the equals sign", afterLine(t, base, "[Unit]", "Foo =bar"), lintSyntax},
				{"a space after the equals sign", afterLine(t, base, "[Unit]", "Foo= bar"), lintSyntax},
				{"a space at the end of a line", afterLine(t, base, "[Unit]", "Foo=bar "), lintSyntax},
				{"a key with a hyphen", afterLine(t, base, "[Unit]", "X-Foo=bar"), lintSyntax},
				{"an empty key", afterLine(t, base, "[Unit]", "=bar"), lintSyntax},
				{"a section header with a space", base + "[Unit ]\n", lintSyntax},
				{"an unclosed section header", base + "[Unit\n", lintSyntax},
				{"a comment with a semicolon", "; comment\n" + base, ""},
				{"blank lines and lines of spaces", "\n   \n" + base + "\n\n", ""},
				{"no newline at the end", strings.TrimSuffix(base, "\n"), ""},
				{"an empty file", "", wantEmpty(name)},
			}
			if kind := unitKinds[name]; kind.wantedBy != "" {
				tests = append(tests,
					tc{"[Install] with another key", base + "Alias=other.service\n", lintInstall},
					tc{"[Install] with Also=", base + "Also=other.service\n", lintInstall},
					tc{"[Install] with another target", replaceLine(t, base, "WantedBy="+kind.wantedBy, "WantedBy=default.target"), lintInstall},
					tc{"[Install] with two targets", replaceLine(t, base, "WantedBy="+kind.wantedBy, "WantedBy="+kind.wantedBy+" default.target"), lintInstall},
					tc{"[Install] with an empty WantedBy=", replaceLine(t, base, "WantedBy="+kind.wantedBy, "WantedBy="), lintInstall},
				)
			} else {
				tests = append(tests, tc{"an [Install] section where the unit has none", base + "[Install]\nWantedBy=multi-user.target\n", lintInstall})
			}
			for _, tt := range tests {
				t.Run(tt.what, func(t *testing.T) {
					if tt.want == "" {
						wantAccepted(t, name, tt.text)
					} else {
						wantRejected(t, name, tt.text, tt.want)
					}
				})
			}
		})
	}
	wantRejected(t, "other.service", installUnit(t, unitService), lintUnknownUnit)
	wantRejected(t, "", "", lintUnknownUnit)
	wantRejected(t, "oarfish-update-helper", "", lintUnknownUnit) // the program is not a unit
}

func wantEmpty(name string) lintReason {
	if name == unitService {
		return lintBaselineMissing
	}
	return lintForm
}

// replaceKey replaces the value of the one line "key=..." of text.
func replaceKey(t *testing.T, text, key, value string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	n := 0
	for i, l := range lines {
		if strings.HasPrefix(l, key+"=") {
			lines[i] = key + "=" + value
			n++
		}
	}
	if n != 1 {
		t.Fatalf("key %s found %d times", key, n)
	}
	return strings.Join(lines, "\n")
}

func replaceDescription(t *testing.T, text, value string) string {
	t.Helper()
	return replaceKey(t, text, "Description", value)
}

// ---- the helper units: equal to an accepted form ------------------------------

func TestLintHelperUnits(t *testing.T) {
	for _, name := range []string{unitHelperSocket, unitHelperService, unitRecover} {
		t.Run(name, func(t *testing.T) {
			base := installUnit(t, name)
			form := helperForms[name][0]

			// Accepted: other comments, other blank lines, other Description= and
			// Documentation= values.
			var noComments []string
			for _, l := range strings.Split(base, "\n") {
				if !strings.HasPrefix(l, "#") {
					noComments = append(noComments, l)
				}
			}
			wantAccepted(t, name, strings.Join(noComments, "\n"))
			wantAccepted(t, name, "# a new header comment\n; and another\n\n"+base+"# a trailing comment\n")
			wantAccepted(t, name, strings.ReplaceAll(base, "\n", "\n\n"))
			wantAccepted(t, name, replaceDescription(t, base, "Another description, with: punctuation (and more)"))
			wantAccepted(t, name, replaceDescription(t, base, ""))
			wantAccepted(t, name, replaceKey(t, base, "Documentation", "https://example.org/docs man:oarfish(8)"))
			wantAccepted(t, name, replaceKey(t, base, "Documentation", strings.Repeat("u", 200)))

			// Rejected: every line of the form removed, changed, or doubled in
			// another section; an added directive; another order.
			formLines := strings.Split(form, "\n")
			for _, l := range formLines {
				if l == "Description=" || l == "Documentation=" {
					wantRejected(t, name, dropKey(t, base, strings.TrimSuffix(l, "=")), lintForm)
					continue
				}
				if strings.HasPrefix(l, "[") {
					continue
				}
				wantRejected(t, name, dropLine(t, base, l), lintForm)
				if key, value, _ := strings.Cut(l, "="); key != "WantedBy" {
					wantRejected(t, name, replaceLine(t, base, l, key+"="+value+"x"), lintForm)
					if value != "" {
						wantRejected(t, name, replaceLine(t, base, l, key+"="), lintForm)
					}
				}
			}
			for _, add := range []string{
				"Environment=A=b", "ExecStartPre=/bin/true", "ExecStartPost=/bin/true", "ExecStop=/bin/true",
				"SupplementaryGroups=oarfish", "Requires=oarfish.service", "Wants=network.target", "RemainAfterExit=yes",
				"BindPaths=/etc", "PrivateUsers=no", "AppArmorProfile=unconfined", "Conflicts=oarfish.service",
			} {
				section := "[Service]"
				if name == unitHelperSocket {
					section = "[Socket]"
				}
				wantRejected(t, name, afterLine(t, base, section, add), lintForm)
			}
			// The order of the lines inside a section is part of the form: two
			// neighbouring directives swapped are another text.
			for i := 0; i+1 < len(formLines); i++ {
				a, b := formLines[i], formLines[i+1]
				if strings.HasPrefix(a, "[") || strings.HasPrefix(b, "[") || strings.HasPrefix(a, "Description") || strings.HasPrefix(a, "Documentation") {
					continue
				}
				swapped := replaceLine(t, replaceLine(t, replaceLine(t, base, a, "\x00"), b, a), "\x00", b)
				wantRejected(t, name, swapped, lintForm)
			}
			// Description= after Documentation= is another order as well.
			lines := strings.Split(form, "\n")
			lines[1], lines[2] = lines[2], lines[1]
			wantRejected(t, name, strings.Join(lines, "\n"), lintForm)
		})
	}
	// The weakened values that U15 rule 3 names, on the units that run as root.
	for _, name := range []string{unitHelperService, unitRecover} {
		base := installUnit(t, name)
		for old, new := range map[string]string{
			"ProtectSystem=strict":        "ProtectSystem=full",
			"NoNewPrivileges=yes":         "NoNewPrivileges=no",
			"CapabilityBoundingSet=":      "CapabilityBoundingSet=CAP_SYS_ADMIN",
			"AmbientCapabilities=":        "AmbientCapabilities=CAP_DAC_OVERRIDE",
			"ReadWritePaths=/opt/oarfish": "ReadWritePaths=/opt/oarfish /etc/systemd/system",
			"User=root":                   "User=oarfish",
		} {
			wantRejected(t, name, replaceLine(t, base, old, new), lintForm)
		}
	}
	base := installUnit(t, unitHelperService)
	wantRejected(t, unitHelperService, replaceLine(t, base, "ExecStart=/usr/libexec/oarfish-update-helper serve", "ExecStart=+/usr/libexec/oarfish-update-helper serve"), lintForm)
	wantRejected(t, unitHelperService, replaceLine(t, base, "ExecStart=/usr/libexec/oarfish-update-helper serve", "ExecStart=/usr/libexec/oarfish-update-helper recover"), lintForm)
	wantRejected(t, unitHelperSocket, replaceLine(t, installUnit(t, unitHelperSocket), "SocketMode=0660", "SocketMode=0666"), lintForm)
	// A unit text under the name of another unit is not that unit's form.
	wantRejected(t, unitRecover, base, lintForm)
	wantRejected(t, unitHelperService, installUnit(t, unitRecover), lintInstall)
}

// dropKey removes the one line "key=..." of text.
func dropKey(t *testing.T, text, key string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	var out []string
	for _, l := range lines {
		if !strings.HasPrefix(l, key+"=") {
			out = append(out, l)
		}
	}
	if len(out) != len(lines)-1 {
		t.Fatalf("key %s found %d times", key, len(lines)-len(out))
	}
	return strings.Join(out, "\n")
}

// ---- oarfish.service: baseline and extras -------------------------------------

func TestLintServiceBaseline(t *testing.T) {
	base := installUnit(t, unitService)
	lines, _ := parseUnit(unitKinds[unitService], []byte(base))
	value := map[string]string{}
	for _, l := range lines {
		value[l.section+"|"+l.key] = l.value
	}
	// One rejected case per removed baseline directive, and per baseline
	// directive with another value.
	for _, r := range serviceRules {
		if !r.baseline {
			continue
		}
		line := r.key + "=" + value[r.section+"|"+r.key]
		t.Run("without "+r.key, func(t *testing.T) {
			wantRejected(t, unitService, dropLine(t, base, line), lintBaselineMissing)
		})
		t.Run(r.key+" with another value", func(t *testing.T) {
			want := lintBaselineValue
			if r.section == "Install" {
				want = lintInstall // the parser refuses it before the table is consulted
			}
			wantRejected(t, unitService, replaceLine(t, base, line, line+"x"), want)
		})
		t.Run(r.key+" in another section", func(t *testing.T) {
			other := "[Unit]"
			if r.section == "Unit" {
				other = "[Service]"
			}
			wantRejected(t, unitService, afterLine(t, dropLine(t, base, line), other, line), lintNotAllowed)
		})
	}
	const exec = "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:8484"
	weakened := map[string]struct{ old, new string }{
		"ProtectSystem=full":            {"ProtectSystem=strict", "ProtectSystem=full"},
		"ProtectSystem=yes":             {"ProtectSystem=strict", "ProtectSystem=yes"},
		"NoNewPrivileges=no":            {"NoNewPrivileges=yes", "NoNewPrivileges=no"},
		"a capability in the bounding":  {"CapabilityBoundingSet=", "CapabilityBoundingSet=CAP_SYS_ADMIN"},
		"an inverted bounding set":      {"CapabilityBoundingSet=", "CapabilityBoundingSet=~"},
		"an ambient capability":         {"AmbientCapabilities=", "AmbientCapabilities=CAP_NET_BIND_SERVICE"},
		"User=root":                     {"User=oarfish", "User=root"},
		"Group=root":                    {"Group=oarfish", "Group=root"},
		"ProtectHome=read-only":         {"ProtectHome=yes", "ProtectHome=read-only"},
		"PrivateTmp=no":                 {"PrivateTmp=yes", "PrivateTmp=no"},
		"PrivateDevices=no":             {"PrivateDevices=yes", "PrivateDevices=no"},
		"RestrictNamespaces=no":         {"RestrictNamespaces=yes", "RestrictNamespaces=no"},
		"RestrictSUIDSGID=no":           {"RestrictSUIDSGID=yes", "RestrictSUIDSGID=no"},
		"a wider address family list":   {"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6", "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK"},
		"an inverted address family":    {"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6", "RestrictAddressFamilies=~AF_PACKET"},
		"a wider system call filter":    {"SystemCallFilter=@system-service", "SystemCallFilter=@system-service @privileged"},
		"UMask=0022":                    {"UMask=0077", "UMask=0022"},
		"StateDirectoryMode=0755":       {"StateDirectoryMode=0700", "StateDirectoryMode=0755"},
		"another state directory":       {"StateDirectory=oarfish", "StateDirectory=oarfish oarfish-update"},
		"Type=notify":                   {"Type=simple", "Type=notify"},
		"another ExecStart path":        {exec, "ExecStart=/opt/oarfish/releases/1.0.0/oarfish -listen 127.0.0.1:8484"},
		"a program outside the tree":    {exec, "ExecStart=/bin/sh -c true"},
		"a relative program":            {exec, "ExecStart=oarfish -listen 127.0.0.1:8484"},
		"a non-loopback -listen":        {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 0.0.0.0:8484"},
		"a LAN address":                 {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 192.0.2.10:8484"},
		"the IPv6 wildcard":             {exec, "ExecStart=/opt/oarfish/current/oarfish -listen [::]:8484"},
		"a host name":                   {exec, "ExecStart=/opt/oarfish/current/oarfish -listen localhost:8484"},
		"an IPv4-mapped address":        {exec, "ExecStart=/opt/oarfish/current/oarfish -listen [::ffff:127.0.0.1]:8484"},
		"port 0":                        {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:0"},
		"no port":                       {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1"},
		"a port with a leading zero":    {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:08484"},
		"a port out of range":           {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:65536"},
		"no -listen":                    {exec, "ExecStart=/opt/oarfish/current/oarfish"},
		"-listen with an equals sign":   {exec, "ExecStart=/opt/oarfish/current/oarfish -listen=127.0.0.1:8484"},
		"a second argument":             {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:8484 -debug"},
		"a second command":              {exec, "ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:8484 ; /bin/true"},
		"two spaces before the address": {exec, "ExecStart=/opt/oarfish/current/oarfish -listen  127.0.0.1:8484"},
		"a quoted program":              {exec, `ExecStart="/opt/oarfish/current/oarfish" -listen 127.0.0.1:8484`},
		"the prefix +":                  {exec, "ExecStart=+/opt/oarfish/current/oarfish -listen 127.0.0.1:8484"},
		"the prefix !":                  {exec, "ExecStart=!/opt/oarfish/current/oarfish -listen 127.0.0.1:8484"},
		"the prefix !!":                 {exec, "ExecStart=!!/opt/oarfish/current/oarfish -listen 127.0.0.1:8484"},
		"the prefix @":                  {exec, "ExecStart=@/opt/oarfish/current/oarfish oarfish -listen 127.0.0.1:8484"},
		"the prefix :":                  {exec, "ExecStart=:/opt/oarfish/current/oarfish -listen 127.0.0.1:8484"},
		"the prefix -":                  {exec, "ExecStart=-/opt/oarfish/current/oarfish -listen 127.0.0.1:8484"},
		"an empty ExecStart":            {exec, "ExecStart="},
		"WantedBy another target":       {"WantedBy=multi-user.target", "WantedBy=sysinit.target"},
	}
	for what, w := range weakened {
		t.Run(what, func(t *testing.T) {
			want := lintBaselineValue
			if strings.HasPrefix(w.old, "WantedBy=") {
				want = lintInstall // the parser refuses it before the table is consulted
			}
			wantRejected(t, unitService, replaceLine(t, base, w.old, w.new), want)
		})
	}
	// Accepted loopback addresses.
	for _, addr := range []string{"127.0.0.1:8484", "127.0.0.1:1", "127.0.0.1:65535", "127.255.255.254:9000", "[::1]:8484"} {
		wantAccepted(t, unitService, replaceLine(t, base, exec, "ExecStart=/opt/oarfish/current/oarfish -listen "+addr))
	}
}

// Every directive that is not listed is rejected.
func TestLintServiceUnlistedDirectives(t *testing.T) {
	base := installUnit(t, unitService)
	for section, adds := range map[string][]string{
		"[Service]": {
			"ExecStartPre=/bin/true", "ExecStartPost=/bin/true", "ExecStop=/bin/true", "ExecStopPost=/bin/true",
			"ExecReload=/bin/true", "ExecCondition=/bin/true",
			"Environment=A=b", "EnvironmentFile=/etc/oarfish/env", "PassEnvironment=HOME",
			"SupplementaryGroups=root", "ReadWritePaths=/opt/oarfish", "ReadOnlyPaths=/", "BindPaths=/etc", "BindReadOnlyPaths=/etc",
			"PrivateUsers=yes", "AppArmorProfile=unconfined", "SELinuxContext=system_u", "DynamicUser=yes",
			"RootDirectory=/srv", "WorkingDirectory=/tmp", "PrivateNetwork=yes", "ProcSubset=pid", "MemoryDenyWriteExecute=yes",
			"RestrictFileSystems=ext4", "RestrictNetworkInterfaces=lo", "TimeoutStartSec=10", "WatchdogSec=10",
			"SystemCallLog=@privileged", "LoadCredential=a:/etc/passwd", "PAMName=login", "TTYPath=/dev/tty1",
			"StandardInput=tty", "KillMode=none", "Delegate=yes", "IPAddressAllow=any", "DeviceAllow=/dev/null",
			"Description=in the wrong section", "OnFailure=other.service",
		},
		"[Unit]": {
			"OnFailure=oarfish-update-recover.service", "OnSuccess=other.service", "Conflicts=ssh.service",
			"Requires=network.target", "Wants=network-online.target", "After=network-online.target", "Before=ssh.service",
			"BindsTo=other.service", "PartOf=other.service", "Upholds=other.service", "DefaultDependencies=no",
			"JoinsNamespaceOf=other.service", "User=root", "ExecStart=/bin/true",
		},
	} {
		for _, add := range adds {
			t.Run(add, func(t *testing.T) {
				wantRejected(t, unitService, afterLine(t, base, section, add), lintNotAllowed)
			})
		}
	}
}

// The allowed extras: accepted at their bounds and when absent, rejected
// outside. Comments, Description= and Documentation= may change.
func TestLintServiceExtras(t *testing.T) {
	base := installUnit(t, unitService)
	type tc struct {
		line   string // the line of install/oarfish.service, or "" for an extra the file does not set
		values map[string]bool
	}
	for key, c := range map[string]tc{
		"Restart": {"Restart=on-failure", map[string]bool{
			"on-failure": true, "always": true,
			"no": false, "on-success": false, "on-abnormal": false, "on-abort": false, "on-watchdog": false, "": false, "Always": false,
		}},
		"RestartSec": {"RestartSec=2", map[string]bool{
			"1": true, "2": true, "10": true,
			"0": false, "11": false, "100": false, "01": false, "2s": false, "1min": false, "infinity": false, "": false, "-1": false, "1.5": false, "+2": false,
		}},
		"TimeoutStopSec": {"TimeoutStopSec=15", map[string]bool{
			"5": true, "15": true, "30": true,
			"4": false, "31": false, "0": false, "infinity": false, "15s": false, "05": false, "": false, "900": false,
		}},
		"ProtectProc": {"", map[string]bool{
			"invisible": true, "noaccess": true,
			"default": false, "ptraceable": false, "": false, "no": false,
		}},
	} {
		for value, ok := range c.values {
			t.Run(key+"="+value, func(t *testing.T) {
				var text string
				if c.line == "" {
					text = afterLine(t, base, "[Service]", key+"="+value)
				} else {
					text = replaceLine(t, base, c.line, key+"="+value)
				}
				if ok {
					wantAccepted(t, unitService, text)
				} else {
					wantRejected(t, unitService, text, lintExtraValue)
				}
			})
		}
		if c.line != "" {
			wantAccepted(t, unitService, dropLine(t, base, c.line))
		}
	}
	// Comments, Description= and Documentation=.
	var noComments []string
	for _, l := range strings.Split(base, "\n") {
		if !strings.HasPrefix(l, "#") {
			noComments = append(noComments, l)
		}
	}
	wantAccepted(t, unitService, strings.Join(noComments, "\n"))
	wantAccepted(t, unitService, "# another header\n"+base+"; a trailing comment\n")
	wantAccepted(t, unitService, replaceDescription(t, base, "Another description"))
	wantAccepted(t, unitService, replaceKey(t, base, "Documentation", "https://example.org/oarfish"))
	wantAccepted(t, unitService, dropKey(t, dropKey(t, base, "Description"), "Documentation"))
	// The order of the lines is not part of the rule for oarfish.service.
	swapped := replaceLine(t, replaceLine(t, replaceLine(t, base, "User=oarfish", "\x01"), "Group=oarfish", "User=oarfish"), "\x01", "Group=oarfish")
	wantAccepted(t, unitService, swapped)
	// The fixture unit of the other tests passes too.
	wantAccepted(t, unitService, string(baseServiceUnit))
	wantAccepted(t, unitService, string(changedUnit(unitService)))
}

// The reasons are a closed set of fixed words, and the result never carries
// anything else.
func TestLintReasonsAreClosed(t *testing.T) {
	if len(lintReasons) != 17 {
		t.Fatalf("%d reasons", len(lintReasons))
	}
	seen := map[lintReason]bool{}
	for _, r := range lintReasons {
		if seen[r] || !reasonPattern.MatchString(string(r)) {
			t.Errorf("reason %q", r)
		}
		seen[r] = true
	}
	src, err := os.ReadFile("unitlint.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`lintReason = "([a-z0-9_]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(declared) != len(lintReasons) {
		t.Fatalf("%d reasons declared, %d in the set", len(declared), len(lintReasons))
	}
	for _, m := range declared {
		if !seen[lintReason(m[1])] {
			t.Errorf("reason %s is not in the set", m[1])
		}
	}
}

// ---- stage --------------------------------------------------------------------

const lintSecret = "Sentinel-Unit-Text"

// stage answers unit_rejected for a signed bundle with a bad unit and leaves
// nothing behind; the journal names the unit and the reason and nothing of the
// unit text.
func TestStageUnitRejected(t *testing.T) {
	big := "# " + strings.Repeat("x", 17<<10) + "\n"
	tests := []struct {
		what   string
		name   string
		unit   func() []byte
		reason lintReason
	}{
		{"oarfish.service with Environment=", unitService, func() []byte {
			return bytes.Replace(baseServiceUnit, []byte("[Service]\n"), []byte("[Service]\nEnvironment=TOKEN="+lintSecret+"\n"), 1)
		}, lintNotAllowed},
		{"oarfish.service with ProtectSystem=full", unitService, func() []byte {
			return bytes.Replace(baseServiceUnit, []byte("ProtectSystem=strict"), []byte("ProtectSystem=full"), 1)
		}, lintBaselineValue},
		{"oarfish.service without NoNewPrivileges=", unitService, func() []byte {
			return bytes.Replace(baseServiceUnit, []byte("NoNewPrivileges=yes\n"), nil, 1)
		}, lintBaselineMissing},
		{"oarfish.service of 17 KiB", unitService, func() []byte { return append([]byte(big), baseServiceUnit...) }, lintTooLarge},
		{"the socket unit with another mode", unitHelperSocket, func() []byte {
			return bytes.Replace(baseComponents[unitHelperSocket], []byte("SocketMode=0660"), []byte("SocketMode=0666"), 1)
		}, lintForm},
		{"the helper service with ExecStartPre=", unitHelperService, func() []byte {
			return bytes.Replace(baseComponents[unitHelperService], []byte("[Service]\n"), []byte("[Service]\nExecStartPre=/bin/"+lintSecret+"\n"), 1)
		}, lintForm},
		{"the recover unit with a continuation line", unitRecover, func() []byte {
			return append(append([]byte(nil), baseComponents[unitRecover]...), "# "+lintSecret+" \\\n"...)
		}, lintContinuation},
		{"the recover unit with a NUL byte", unitRecover, func() []byte {
			return append(append([]byte(nil), baseComponents[unitRecover]...), 0)
		}, lintNUL},
	}
	for _, tt := range tests {
		t.Run(tt.what, func(t *testing.T) {
			v := newTenv(t)
			r := v.release("1.1.0")
			r.setFile(tt.name, tt.unit())
			v.place("1.1.0", r.build())
			before := snapshot(t, v.root)
			raw := v.stage("1.1.0")
			got := wantErrCode(t, raw, codeUnitRejected)
			if len(got.Differs) != 0 {
				t.Fatalf("differs %v", got.Differs)
			}
			wantNothingLeft(t, v, before)
			v.wantClean()
			want := []string{
				"phase=staging to=1.1.0 from=1.0.0 cid=" + testCID + " result=ok",
				"phase=staging to=1.1.0 from=1.0.0 cid=" + testCID + " result=unit_rejected unit=" + tt.name + " reason=" + string(tt.reason),
				"phase=idle to=1.1.0 from=1.0.0 cid=" + testCID + " result=unit_rejected",
			}
			if got := strings.TrimSuffix(v.journal.String(), "\n"); got != strings.Join(want, "\n") {
				t.Fatalf("journal:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
			}
			for _, blob := range []string{string(raw), v.journal.String()} {
				if strings.Contains(blob, lintSecret) || strings.Contains(blob, "/bin") || strings.Contains(blob, "TOKEN") {
					t.Fatalf("unit text in %q", blob)
				}
			}
			// The same release with the fixture units is staged afterwards.
			v.stageable("1.1.0")
			wantOK(t, v.stage("1.1.0"))
		})
	}
}

// The journal line of a rejected unit has one fixed shape: the unit is one of
// the four names and the reason one of the closed set.
func TestLintJournalLineShape(t *testing.T) {
	var units, reasons []string
	for _, u := range lintedUnits {
		units = append(units, regexp.QuoteMeta(u))
	}
	for _, r := range lintReasons {
		reasons = append(reasons, string(r))
	}
	re := regexp.MustCompile(`^phase=staging to=[0-9A-Za-z.-]+ from=[0-9A-Za-z.-]+ cid=[0-9a-f]{16} result=unit_rejected unit=(` +
		strings.Join(units, "|") + `) reason=(` + strings.Join(reasons, "|") + `)$`)
	v := newTenv(t)
	for _, u := range lintedUnits {
		for _, r := range lintReasons {
			v.journal = &syncBuffer{}
			v.h.journal = v.journal
			v.h.logLint("1.1.0", "1.0.0", testCID, u, r)
			if line := strings.TrimSuffix(v.journal.String(), "\n"); !re.MatchString(line) {
				t.Errorf("journal line %q", line)
			}
		}
	}
	v.h.journal = nil
	v.h.logLint("1.1.0", "1.0.0", testCID, unitService, lintSyntax) // no journal: no panic
}

// The lint runs after the extraction and before the component comparison, and
// the checks before it keep their answers.
func TestStageUnitLintOrder(t *testing.T) {
	bad := bytes.Replace(baseServiceUnit, []byte("User=oarfish"), []byte("User=root"), 1)
	t.Run("before the component comparison", func(t *testing.T) {
		v := newTenv(t)
		r := v.release("1.1.0")
		r.setFile("oarfish-update-helper", elfStub("another helper"))
		r.setFile(unitService, bad)
		v.place("1.1.0", r.build())
		wantErrCode(t, v.stage("1.1.0"), codeUnitRejected)
		v.wantClean()
	})
	t.Run("a difference that the lint accepts is staged and named", func(t *testing.T) {
		for _, name := range lintedUnits {
			v := newTenv(t)
			r := v.release("1.1.0")
			r.setFile(name, changedUnit(name))
			v.place("1.1.0", r.build())
			got := wantOK(t, v.stage("1.1.0"))
			if !got.ComponentsDiffer || strings.Join(got.Differs, ",") != name {
				t.Fatalf("differs %v, want %s", got.Differs, name)
			}
		}
	})
	t.Run("after the version rule", func(t *testing.T) {
		v := newTenv(t)
		r := v.release("0.9.0")
		r.setFile(unitService, bad)
		v.place("0.9.0", r.build())
		wantErrCode(t, v.stage("0.9.0"), codeDowngradeRefused)
	})
	t.Run("a damaged installed set is still components_damaged", func(t *testing.T) {
		v := newTenv(t)
		v.stageable("1.1.0")
		os.Remove(v.root + "/components/1.0.0/oarfish-update-recover.service")
		wantErrCode(t, v.stage("1.1.0"), codeComponentsDamaged)
	})
	t.Run("a legacy bundle: oarfish.service is linted, then requirements_not_met", func(t *testing.T) {
		v := newTenv(t)
		v.place("1.1.0", newRelease(t, v.key, "1.1.0", false).build())
		wantErrCode(t, v.stage("1.1.0"), codeRequirementsNotMet)
		v2 := newTenv(t)
		r := newRelease(t, v2.key, "1.1.0", false)
		r.setFile(unitService, bad)
		v2.place("1.1.0", r.build())
		wantErrCode(t, v2.stage("1.1.0"), codeUnitRejected)
		v2.wantClean()
	})
	t.Run("the installed units are not linted", func(t *testing.T) {
		// The lint judges the release; an installed unit of an earlier release
		// that the present rules would refuse does not block a stage.
		v := newTenv(t)
		p := v.root + "/releases/1.0.0/oarfish.service"
		if err := os.WriteFile(p, append([]byte("# old\n"), bad...), 0o644); err != nil {
			t.Fatal(err)
		}
		v.stageable("1.1.0")
		if got := wantOK(t, v.stage("1.1.0")); strings.Join(got.Differs, ",") != unitService {
			t.Fatalf("differs %v", got.Differs)
		}
	})
	t.Run("a unit that cannot be read is io_error", func(t *testing.T) {
		v := newTenv(t)
		v.stageable("1.1.0")
		hookAfterLstat = func(root *os.Root, name string) {
			if strings.HasSuffix(name, "/components/"+unitHelperSocket) {
				root.Remove(name)
			}
		}
		defer func() { hookAfterLstat = nil }()
		wantErrCode(t, v.stage("1.1.0"), codeIOError)
		v.wantClean()
	})
}

// ---- fuzz ---------------------------------------------------------------------

// FuzzUnitLint: the lint never panics, its result is one of the closed values,
// it is deterministic, and what it accepts has the properties the rules
// promise. Seeds are the units of install/ and one mutation of each rule.
func FuzzUnitLint(f *testing.F) {
	for i, name := range lintedUnits {
		base := installUnit(f, name)
		f.Add(uint8(i), []byte(base))
		for j := range lintedUnits {
			f.Add(uint8(j), []byte(base)) // every text under every name
		}
		for _, mut := range []string{
			base + "Foo=bar\\\n", base + "[Timer]\n", base + "X=%n\n", base + "\x00", base + "\xff", base + "\r\n",
			strings.Replace(base, "Description=", "Description="+strings.Repeat("d", 201), 1),
			strings.Replace(base, "[Unit]", "[Unit]\nDescription=two", 1),
			strings.Replace(base, "=yes", "=no", 1), strings.Replace(base, "ExecStart=", "ExecStart=+", 1),
			strings.Replace(base, "127.0.0.1", "0.0.0.0", 1), strings.Replace(base, "[Service]", "[Service]\nEnvironment=A=b", 1),
			base + strings.Repeat("#", 17<<10), " " + base, "", "[", "[]", "=", "[Unit]\n=",
		} {
			f.Add(uint8(i), []byte(mut))
		}
	}
	for _, name := range []string{unitHelperSocket, unitHelperService, unitRecover} {
		f.Add(uint8(0), []byte(helperForms[name][0]))
	}
	f.Add(uint8(3), baseServiceUnit)
	f.Add(uint8(200), []byte("[Unit]\n"))
	known := map[lintReason]bool{}
	for _, r := range lintReasons {
		known[r] = true
	}
	f.Fuzz(func(t *testing.T, which uint8, text []byte) {
		name := "not-a-unit"
		if int(which) < len(lintedUnits) {
			name = lintedUnits[which]
		}
		res := lintUnit(name, text)
		if again := lintUnit(name, text); again != res {
			t.Fatalf("not deterministic: %+v then %+v", res, again)
		}
		if res.Accepted != (res.Reason == "") {
			t.Fatalf("result %+v", res)
		}
		if !res.Accepted {
			if !known[res.Reason] {
				t.Fatalf("reason %q is not in the closed set", res.Reason)
			}
			return
		}
		if name == "not-a-unit" {
			t.Fatal("accepted under an unknown name")
		}
		if len(text) > maxUnitBytes || !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 || bytes.IndexByte(text, '\r') >= 0 {
			t.Fatal("accepted a text that breaks a parsing rule")
		}
		keys := map[string]bool{}
		section := ""
		for _, line := range strings.Split(string(text), "\n") {
			if strings.HasSuffix(line, `\`) {
				t.Fatalf("accepted a continuation line %q", line)
			}
			if strings.TrimSpace(line) == "" || line[0] == '#' || line[0] == ';' {
				continue
			}
			if strings.Contains(line, "%") {
				t.Fatalf("accepted a specifier %q", line)
			}
			if line[0] == '[' {
				section = line
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || keys[section+key] {
				t.Fatalf("accepted the line %q", line)
			}
			keys[section+key] = true
			if (key == "Description" || key == "Documentation") && len(value) > maxUnitText {
				t.Fatalf("accepted a long %s", key)
			}
			if section == "[Install]" && (key != "WantedBy" || value != unitKinds[name].wantedBy) {
				t.Fatalf("accepted %q in [Install]", line)
			}
			if name == unitService && strings.HasPrefix(key, "Exec") &&
				(key != "ExecStart" || !strings.HasPrefix(value, "/opt/oarfish/current/oarfish -listen ")) {
				t.Fatalf("accepted %q", line)
			}
		}
		lines, reason := parseUnit(unitKinds[name], text)
		if reason != "" {
			t.Fatalf("accepted, but the parser says %s", reason)
		}
		if name == unitService {
			have := map[string]string{}
			for _, l := range lines {
				have[l.section+"|"+l.key] = l.value
			}
			for _, r := range serviceRules {
				v, ok := have[r.section+"|"+r.key]
				if r.baseline && (!ok || !r.ok(v)) {
					t.Fatalf("accepted without the baseline %s", r.key)
				}
			}
			for id, want := range map[string]string{"Service|User": "oarfish", "Service|NoNewPrivileges": "yes", "Service|ProtectSystem": "strict"} {
				if have[id] != want {
					t.Fatalf("accepted with %s=%s", id, have[id])
				}
			}
		} else if norm := normalisedUnit(lines); !contains(helperForms[name], norm) {
			t.Fatal("accepted a helper unit that equals no form")
		}
		// A comment line more does not change the verdict (while the size allows it).
		if more := append(append([]byte(nil), text...), "\n# c\n"...); len(more) <= maxUnitBytes && !lintUnit(name, more).Accepted {
			t.Fatal("a comment changed the verdict")
		}
	})
}
