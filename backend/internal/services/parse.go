package services

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/swingstate/oarfish/backend/internal/unitname"
)

// The parsers of the two read shapes (ADR 0011 B2 as amended by A3.2). The
// output of systemctl is untrusted: every word is checked against its closed
// set, every name against rule 1 of B3, and anything else fails the whole
// read, so the page never shows half a list.

// maxUnits bounds the lines of one output and the rows of the list (B2).
const maxUnits = 1000

// maxDescription is the longest description kept, in characters (B2).
const maxDescription = 200

var errOutput = errors.New("systemctl output has not the expected shape")

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// The closed sets of the words of list-units.
var (
	// LoadState of a unit. not-found entries are dropped (A3.2).
	loadStates = set("loaded", "not-found", "bad-setting", "error", "merged", "masked", "stub")
	// ActiveState: the closed set the broker uses as well (A4.3).
	activeStates = set("active", "reloading", "inactive", "failed", "activating", "deactivating", "maintenance", "refreshing")
	// SubState of a service unit, as systemd names them (the service state
	// table of systemd up to version 257; assumption, not read from a device).
	subStates = set("dead", "condition", "start-pre", "start", "start-post", "running", "exited", "reload",
		"reload-signal", "reload-notify", "refresh-extensions", "mounting", "stop", "stop-watchdog", "stop-sigterm",
		"stop-sigkill", "stop-post", "final-watchdog", "final-sigterm", "final-sigkill", "failed",
		"dead-before-auto-restart", "failed-before-auto-restart", "dead-resources-pinned", "auto-restart",
		"auto-restart-queued", "cleaning")
	// The unit-file states of list-unit-files, exactly the ten of A3.2.
	fileStates = set("enabled", "enabled-runtime", "disabled", "static", "masked", "indirect", "linked", "alias", "generated", "transient")
	// The third column of list-unit-files (vendor preset) is not used; it must
	// be one plain word.
	presetWord = regexp.MustCompile(`^(-|[a-z]{1,16})$`)
)

// unitLine is one line of list-units.
type unitLine struct {
	name, load, active, sub, description string
}

// lines splits an output into its lines: newline-separated, one final newline
// allowed, no empty line, at most maxUnits lines. An empty output is no line.
func lines(out []byte) ([][]byte, error) {
	if len(out) == 0 {
		return nil, nil
	}
	out = bytes.TrimSuffix(out, []byte("\n"))
	ls := bytes.Split(out, []byte("\n"))
	if len(ls) > maxUnits {
		return nil, errOutput
	}
	for _, l := range ls {
		if len(l) == 0 {
			return nil, errOutput
		}
	}
	return ls, nil
}

// cutWord returns the first word of l (separated by spaces) and the rest
// after the spaces that follow it.
func cutWord(l []byte) (word, rest []byte) {
	i := bytes.IndexByte(l, ' ')
	if i < 0 {
		return l, nil
	}
	return l[:i], bytes.TrimLeft(l[i:], " ")
}

// parseListUnits parses `list-units --type=service --all --no-legend
// --no-pager --plain`: four words (name, load, active, sub) and the rest of
// the line as the description (A3.2).
func parseListUnits(out []byte) ([]unitLine, error) {
	ls, err := lines(out)
	if err != nil {
		return nil, err
	}
	units := make([]unitLine, 0, len(ls))
	seen := map[string]bool{}
	for _, l := range ls {
		var w [4][]byte
		rest := l
		for i := range w {
			w[i], rest = cutWord(rest)
		}
		u := unitLine{name: string(w[0]), load: string(w[1]), active: string(w[2]), sub: string(w[3])}
		if !unitname.Valid(u.name) || !loadStates[u.load] || !activeStates[u.active] || !subStates[u.sub] || seen[u.name] {
			return nil, errOutput
		}
		seen[u.name] = true
		u.description = cleanDescription(rest)
		units = append(units, u)
	}
	return units, nil
}

// parseListUnitFiles parses `list-unit-files --type=service --no-legend
// --no-pager`: exactly three words (name, state, vendor preset) per line.
func parseListUnitFiles(out []byte) (map[string]string, error) {
	ls, err := lines(out)
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(ls))
	for _, l := range ls {
		f := strings.Fields(string(l))
		if len(f) != 3 || strings.Join(f, "") == "" {
			return nil, errOutput
		}
		// Only single spaces or column padding between the words: no tab, no
		// other white space (strings.Fields would accept them).
		if bytes.ContainsFunc(l, func(r rune) bool { return r != ' ' && unicode.IsSpace(r) }) {
			return nil, errOutput
		}
		name, state, preset := f[0], f[1], f[2]
		if !unitname.Valid(name) || !fileStates[state] || !presetWord.MatchString(preset) {
			return nil, errOutput
		}
		if _, dup := files[name]; dup {
			return nil, errOutput
		}
		files[name] = state
	}
	return files, nil
}

// bidiControls are format characters that reorder the text around them; a
// description could use them to make one name look like another.
var bidiControls = set("‎", "‏", "‪", "‫", "‬", "‭", "‮", "⁦", "⁧", "⁨", "⁩")

// cleanDescription makes an untrusted description plain text: invalid UTF-8
// becomes U+FFFD, control characters and bidirectional format characters are
// removed, the spaces at both ends are trimmed and at most maxDescription
// characters are kept. The page shows it as a text node only.
func cleanDescription(raw []byte) string {
	s := strings.ToValidUTF8(string(raw), "�")
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) || bidiControls[string(r)] {
			continue
		}
		if n == maxDescription {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
