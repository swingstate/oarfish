package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// The unit lint (ADR 0010 U4 step 9, U15 rule 3, amendment A15). A unit text
// inside a signed release is checked against the compiled-in rule tables of
// unitrules.go before any link can point to it. The lint judges text only and
// fails closed: what it does not know it rejects.
//
// lintUnit is the one function. Its result is typed: accepted, or rejected
// with a reason from a closed set. No byte of the unit text, no path and no
// parser message is part of the result, so none can reach an answer or the
// journal.

const (
	// maxUnitBytes is the largest unit file the lint reads (16 KiB).
	maxUnitBytes = 16 << 10
	// maxUnitText bounds the values of Description= and Documentation=.
	maxUnitText = 200
)

// lintReason is one of the closed reasons of a rejection.
type lintReason string

const (
	lintUnknownUnit     lintReason = "unknown_unit"          // the name is not one of the four units
	lintTooLarge        lintReason = "too_large"             // more than 16 KiB
	lintNUL             lintReason = "nul_byte"              // a NUL byte
	lintNotUTF8         lintReason = "not_utf8"              // not valid UTF-8
	lintControl         lintReason = "control_character"     // a control character other than the newline
	lintContinuation    lintReason = "continuation"          // a line that ends in a backslash
	lintSpecifier       lintReason = "specifier"             // a percent sign outside a comment
	lintSyntax          lintReason = "syntax"                // not a section, key=value or comment line
	lintSection         lintReason = "section"               // a section the unit type does not allow, or one twice
	lintDuplicateKey    lintReason = "duplicate_key"         // a key twice in one section
	lintInstall         lintReason = "install_section"       // [Install] holds something else than the expected WantedBy=
	lintText            lintReason = "text_value"            // Description= or Documentation= not printable or too long
	lintForm            lintReason = "form_mismatch"         // a helper unit that equals no accepted form
	lintNotAllowed      lintReason = "directive_not_allowed" // oarfish.service: a directive without a rule
	lintBaselineMissing lintReason = "baseline_missing"      // oarfish.service: a baseline directive is absent
	lintBaselineValue   lintReason = "baseline_value"        // oarfish.service: a baseline directive has another value
	lintExtraValue      lintReason = "extra_value"           // oarfish.service: an extra with a value outside its rule
)

// lintReasons is the closed set, in the order of the checks.
var lintReasons = []lintReason{
	lintUnknownUnit, lintTooLarge, lintNUL, lintNotUTF8, lintControl, lintContinuation, lintSpecifier, lintSyntax,
	lintSection, lintDuplicateKey, lintInstall, lintText, lintForm, lintNotAllowed, lintBaselineMissing,
	lintBaselineValue, lintExtraValue,
}

// lintResult is the typed result: accepted, or unit_rejected with its reason.
type lintResult struct {
	Accepted bool
	Reason   lintReason // empty when accepted
}

func rejected(r lintReason) lintResult { return lintResult{Reason: r} }

// unitLine is one section header or one key=value line of a parsed unit.
type unitLine struct {
	section    string
	key, value string // key is empty for a section header
}

// lintUnit checks the text of one unit, named by the closed list of U2.
func lintUnit(name string, text []byte) lintResult {
	kind, ok := unitKinds[name]
	if !ok {
		return rejected(lintUnknownUnit)
	}
	lines, reason := parseUnit(kind, text)
	if reason != "" {
		return rejected(reason)
	}
	if name == unitService {
		return lintService(lines)
	}
	norm := normalisedUnit(lines)
	for _, form := range helperForms[name] {
		if norm == form {
			return lintResult{Accepted: true}
		}
	}
	return rejected(lintForm)
}

// parseUnit applies the parsing rules of U15 rule 3 and returns the section
// headers and key=value lines in file order. It is stricter than systemd where
// systemd is lenient, never the other way round:
//
//   - at most 16 KiB, valid UTF-8, no NUL and no other control character but
//     the newline (systemd also ends a line at a carriage return and at a NUL);
//   - no line may end in a backslash, comment lines included;
//   - a blank line is empty or holds spaces only; a comment line starts with
//     "#" or ";" in the first column;
//   - every other line starts in the first column and is a section header
//     "[Name]" or "Key=value", without spaces around the "=" and at the end;
//   - outside comments only printable ASCII, and no percent sign (specifier);
//   - only the sections the unit type allows, each at most once; no key=value
//     line before the first section; no key twice in one section;
//   - [Install] may hold only WantedBy= with the expected target;
//   - Description= and Documentation= hold at most 200 characters.
func parseUnit(kind unitKind, text []byte) ([]unitLine, lintReason) {
	if len(text) > maxUnitBytes {
		return nil, lintTooLarge
	}
	for _, b := range text {
		if b == 0 {
			return nil, lintNUL
		}
	}
	if !utf8.Valid(text) {
		return nil, lintNotUTF8
	}
	for _, b := range text {
		if (b < 0x20 && b != '\n') || b == 0x7f {
			return nil, lintControl
		}
	}
	var lines []unitLine
	section := ""
	sections := map[string]bool{}
	keys := map[string]bool{}
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " "), `\`) {
			return nil, lintContinuation
		}
		if strings.TrimLeft(line, " ") == "" {
			continue
		}
		if line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.Contains(line, "%") {
			return nil, lintSpecifier
		}
		if !printableASCII(line) || line[0] == ' ' || line[len(line)-1] == ' ' {
			return nil, lintSyntax
		}
		if line[0] == '[' {
			name, ok := sectionName(line)
			if !ok {
				return nil, lintSyntax
			}
			if !allowedSection(kind, name) || sections[name] {
				return nil, lintSection
			}
			sections[name] = true
			section = name
			lines = append(lines, unitLine{section: name})
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !unitKey(key) || strings.HasPrefix(value, " ") || section == "" {
			return nil, lintSyntax
		}
		if keys[section+"\n"+key] {
			return nil, lintDuplicateKey
		}
		keys[section+"\n"+key] = true
		if section == "Install" && (key != "WantedBy" || kind.wantedBy == "" || value != kind.wantedBy) {
			return nil, lintInstall
		}
		if (key == "Description" || key == "Documentation") && !unitText(value) {
			return nil, lintText
		}
		lines = append(lines, unitLine{section: section, key: key, value: value})
	}
	return lines, ""
}

// sectionName returns the name of a section header line "[Name]".
func sectionName(line string) (string, bool) {
	if len(line) < 3 || line[len(line)-1] != ']' {
		return "", false
	}
	name := line[1 : len(line)-1]
	return name, unitKey(name)
}

func allowedSection(kind unitKind, name string) bool {
	for _, s := range kind.sections {
		if s == name {
			return true
		}
	}
	return false
}

// unitKey reports whether s is a directive or section name: ASCII letters and
// digits, starting with a letter.
func unitKey(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		letter := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// unitText is the rule for the values of Description= and Documentation=:
// printable ASCII (the parser has checked that) and at most 200 characters.
func unitText(v string) bool { return len(v) <= maxUnitText && printableASCII(v) }

// normalisedUnit is the text that the accepted forms of a helper unit are
// compared with: the section headers and key=value lines in file order, with
// the values of Description= and Documentation= removed, joined by newlines.
func normalisedUnit(lines []unitLine) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		switch {
		case l.key == "":
			out = append(out, "["+l.section+"]")
		case l.key == "Description" || l.key == "Documentation":
			out = append(out, l.key+"=")
		default:
			out = append(out, l.key+"="+l.value)
		}
	}
	return strings.Join(out, "\n")
}

// lintService checks oarfish.service against the table: every directive must
// have a rule and a value its rule accepts, and every baseline directive must
// be present. The order of the lines is not part of the rule.
func lintService(lines []unitLine) lintResult {
	seen := map[int]bool{}
	for _, l := range lines {
		if l.key == "" {
			continue
		}
		rule := -1
		for i, r := range serviceRules {
			if r.section == l.section && r.key == l.key {
				rule = i
				break
			}
		}
		if rule < 0 {
			return rejected(lintNotAllowed)
		}
		if r := serviceRules[rule]; !r.ok(l.value) {
			if r.baseline {
				return rejected(lintBaselineValue)
			}
			return rejected(lintExtraValue)
		}
		seen[rule] = true
	}
	for i, r := range serviceRules {
		if r.baseline && !seen[i] {
			return rejected(lintBaselineMissing)
		}
	}
	return lintResult{Accepted: true}
}

// ---- stage -------------------------------------------------------------------

// readUnit reads a unit file of the staged tree. A file larger than the limit
// is not read; the lint rejects it by its size.
func readUnit(root *os.Root, name string) ([]byte, bool, error) {
	f, fi, err := openRegular(root, name)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	if fi.Size() > maxUnitBytes {
		return nil, true, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxUnitBytes+1))
	if err != nil {
		return nil, false, err
	}
	return data, len(data) > maxUnitBytes, nil
}

// lintUnits lints the unit files of the staged tree (U4 step 9): always
// oarfish.service, and the three helper units when the release carries the
// components. The first rejection ends it with unit_rejected and one journal
// line that names the unit and the reason.
func (r *stageRun) lintUnits(dir string, components bool) errorCode {
	for _, name := range lintedUnits {
		path := dir + "/" + dirComponents + "/" + name
		if name == unitService {
			path = dir + "/" + dirRelease + "/" + name
		} else if !components {
			continue
		}
		text, tooLarge, err := readUnit(r.e.opt, path)
		if err != nil {
			return codeIOError
		}
		res := rejected(lintTooLarge)
		if !tooLarge {
			res = lintUnit(name, text)
		}
		if !res.Accepted {
			r.h.logLint(r.req.Version, r.instStr, r.req.CorrelationID, name, res.Reason)
			return codeUnitRejected
		}
	}
	return ""
}

// logLint writes the journal line of a rejected unit: the line of U8 for the
// phase staging with the result unit_rejected, followed by the unit name from
// the closed list and the reason from the closed set. Nothing of the unit text
// is part of it.
func (h *handler) logLint(to, from, cid, unit string, reason lintReason) {
	if h.journal == nil {
		return
	}
	fmt.Fprintf(h.journal, "phase=%s to=%s from=%s cid=%s result=%s unit=%s reason=%s\n",
		phaseStaging, to, from, cid, codeUnitRejected, unit, reason)
}
