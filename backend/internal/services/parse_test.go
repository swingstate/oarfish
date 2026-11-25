package services

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseListUnits(t *testing.T) {
	units, err := parseListUnits([]byte(fixtureUnits))
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 11 {
		t.Fatalf("%d units", len(units))
	}
	if u := units[0]; u != (unitLine{"demo-app.service", "loaded", "active", "running", "Demo application server"}) {
		t.Errorf("%+v", u)
	}
	if u := units[3]; u.load != "not-found" || u.description != "demo-gone.service" {
		t.Errorf("%+v", u)
	}
	// One space between the words and no description is fine too.
	if u, err := parseListUnits([]byte("a.service loaded active running\n")); err != nil || u[0].description != "" {
		t.Errorf("%+v %v", u, err)
	}
	if u, err := parseListUnits(nil); err != nil || len(u) != 0 {
		t.Errorf("empty: %v %v", u, err)
	}
	// Every word of the closed sets is accepted.
	for w := range loadStates {
		if _, err := parseListUnits([]byte("a.service " + w + " inactive dead x\n")); err != nil {
			t.Errorf("load %s: %v", w, err)
		}
	}
	for w := range activeStates {
		if _, err := parseListUnits([]byte("a.service loaded " + w + " dead x\n")); err != nil {
			t.Errorf("active %s: %v", w, err)
		}
	}
	for w := range subStates {
		if _, err := parseListUnits([]byte("a.service loaded active " + w + " x\n")); err != nil {
			t.Errorf("sub %s: %v", w, err)
		}
	}
}

// Fail closed (A3.2, A3.7 BF21): a wrong word, a missing word, a name that
// fails rule 1, a duplicated name, an empty line, a tab between the words,
// more than 1,000 lines. The whole read fails, never half a list.
func TestParseListUnitsFailsClosed(t *testing.T) {
	for _, in := range []string{
		"a.service loaded active\n",
		"a.service loaded active running-ish x\n",
		"a.service Loaded active running x\n",
		"a.service loaded up running x\n",
		"a.service loaded active running x\nb.service loaded active nonsense y\n",
		"-a.service loaded active running x\n",
		"a.socket loaded active running x\n",
		"../a.service loaded active running x\n",
		"ä.service loaded active running x\n",
		"● a.service loaded active running x\n",
		"a.service\tloaded active running x\n",
		"a.service loaded active running x\n\nb.service loaded active running y\n",
		"a.service loaded active running x\na.service loaded active running y\n",
		"\n",
		" a.service loaded active running x\n",
		strings.Repeat("a.service loaded active running x\n", 1) + strings.Repeat("b", 300) + ".service loaded active running x\n",
		"<b>.service loaded active running x\n",
	} {
		if _, err := parseListUnits([]byte(in)); err == nil {
			t.Errorf("accepted %.80q", in)
		}
	}
	var many strings.Builder
	for i := 0; i <= maxUnits; i++ {
		many.WriteString("demo-" + strings.Repeat("x", i%7) + itoa(i) + ".service loaded active running x\n")
	}
	if _, err := parseListUnits([]byte(many.String())); err == nil {
		t.Error("accepted 1,001 lines")
	}
}

func itoa(i int) string {
	const d = "0123456789"
	if i < 10 {
		return d[i : i+1]
	}
	return itoa(i/10) + d[i%10:i%10+1]
}

func TestParseListUnitFiles(t *testing.T) {
	files, err := parseListUnitFiles([]byte(fixtureFiles))
	if err != nil {
		t.Fatal(err)
	}
	if files["demo-app.service"] != "enabled" || files["sshd.service"] != "alias" || files["getty@.service"] != "enabled" || len(files) != 13 {
		t.Errorf("%v", files)
	}
	for w := range fileStates {
		if _, err := parseListUnitFiles([]byte("a.service " + w + " enabled\n")); err != nil {
			t.Errorf("state %s: %v", w, err)
		}
	}
	if len(fileStates) != 10 {
		t.Errorf("%d states", len(fileStates))
	}
}

// Three words exactly, a state of the ten, a plain preset word, rule 1.
func TestParseListUnitFilesFailsClosed(t *testing.T) {
	for _, in := range []string{
		"a.service enabled\n",
		"a.service enabled enabled extra\n",
		"a.service enabled-forever enabled\n",
		"a.service bad enabled\n",
		"a.service linked-runtime enabled\n",
		"a.service masked-runtime enabled\n",
		"a.service Enabled enabled\n",
		"a.service enabled Enabled\n",
		"a.service enabled en/abled\n",
		"a.service\tenabled\tenabled\n",
		"a.service enabled enabled\r\n",
		"a.socket enabled enabled\n",
		"/etc/a.service enabled enabled\n",
		"a.service enabled enabled\na.service disabled enabled\n",
		"a.service enabled enabled\n\n",
	} {
		if _, err := parseListUnitFiles([]byte(in)); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
}

// Descriptions are untrusted: control characters and bidirectional controls
// removed, invalid UTF-8 replaced, at most 200 characters, markup kept as
// plain characters (the page shows it as a text node).
func TestCleanDescription(t *testing.T) {
	for in, want := range map[string]string{
		"Plain text":                    "Plain text",
		"  padded  ":                    "padded",
		"bell\x07 and \x1b[31mred":      "bell and [31mred",
		"bad \xff\xfe utf8":             "bad � utf8",
		"<script>alert(1)</script> & x": "<script>alert(1)</script> & x",
		"rtl ‮gnp.exe":                  "rtl gnp.exe",
		"tab\there":                     "tabhere",
		"Grüße, Gerät":                  "Grüße, Gerät",
		"del\x7f":                       "del",
		"c1 \u0085 next":                "c1  next",
	} {
		if got := cleanDescription([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ä", 500)
	if got := cleanDescription([]byte(long)); utf8.RuneCountInString(got) != 200 {
		t.Errorf("%d characters", utf8.RuneCountInString(got))
	}
}

// Hostile output end to end through the parser: 1 MiB of one line, control
// characters, invalid UTF-8 and markup in a description.
func TestHostileListUnits(t *testing.T) {
	big := "a.service loaded active running " + strings.Repeat("x", 1<<20) + "\n"
	u, err := parseListUnits([]byte(big))
	if err != nil || len(u[0].description) != 200 {
		t.Errorf("1 MiB line: %v, %d", err, len(u))
	}
	u, err = parseListUnits([]byte("a.service loaded active running <img src=x onerror=alert(1)>\x00\x1b\xff\n"))
	if err != nil || u[0].description != "<img src=x onerror=alert(1)>�" {
		t.Errorf("%q %v", u, err)
	}
}

func FuzzParseListUnits(f *testing.F) {
	f.Add([]byte(fixtureUnits))
	f.Add([]byte("a.service loaded active running \xff\x00 desc\n"))
	f.Fuzz(func(t *testing.T, in []byte) {
		units, err := parseListUnits(in)
		if err != nil {
			return
		}
		if len(units) > maxUnits {
			t.Fatal("too many units")
		}
		for _, u := range units {
			if !loadStates[u.load] || !activeStates[u.active] || !subStates[u.sub] || !utf8.ValidString(u.description) ||
				utf8.RuneCountInString(u.description) > maxDescription || strings.ContainsAny(u.description, "\x00\n\r\t\x1b") {
				t.Fatalf("%+v", u)
			}
		}
	})
}

func FuzzParseListUnitFiles(f *testing.F) {
	f.Add([]byte(fixtureFiles))
	f.Add([]byte("a.service enabled -\n"))
	f.Fuzz(func(t *testing.T, in []byte) {
		files, err := parseListUnitFiles(in)
		if err != nil {
			return
		}
		for name, state := range files {
			if !fileStates[state] || strings.ContainsAny(name, " /\t\n") {
				t.Fatalf("%q %q", name, state)
			}
		}
	})
}
