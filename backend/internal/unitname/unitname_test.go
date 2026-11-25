package unitname

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The broker's rule 1 and this one must not drift apart: the pattern literal
// in backend/cmd/oarfish-broker/unit.go is the same text as Pattern, and the
// same checks (length, ASCII, "..", escapes) are there.
func TestPatternMatchesTheBroker(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "oarfish-broker", "unit.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?m)^var unitPattern = regexp\\.MustCompile\\(`([^`]*)`\\)$").FindSubmatch(src)
	if m == nil {
		t.Fatal("unitPattern not found in the broker's unit.go")
	}
	if string(m[1]) != Pattern {
		t.Fatalf("broker pattern %q\nservice pattern %q", m[1], Pattern)
	}
	text := string(src)
	for _, want := range []string{"const maxUnitName = 255", `strings.Contains(s, "..")`, "s[i] >= 0x80", `s[i+1] != 'x'`} {
		if !strings.Contains(text, want) {
			t.Errorf("the broker's rule 1 no longer contains %q; compare it with this package", want)
		}
	}
	if MaxLength != 255 {
		t.Error(MaxLength)
	}
}

func TestValid(t *testing.T) {
	for _, s := range []string{
		"a.service", "ssh.service", "NetworkManager.service", "getty@tty1.service", "getty@.service",
		"systemd-fsck@dev-disk-by\\x2duuid-0a1b.service", "a:b_c.d-e.service", "x@a:b.c\\x2d-d.service",
		strings.Repeat("a", 247) + ".service",
	} {
		if !Valid(s) {
			t.Errorf("refused %q", s)
		}
	}
	for _, s := range []string{
		"", ".service", "-a.service", "a.socket", "a.timer", "a", "a/b.service", "../a.service", "a..b.service",
		"a b.service", "a.service ", "ä.service", "a\\x2D.service", "a\\x2.service", "a\\y20.service", "a\\.service",
		"a@b@c.service", "a.service\n", "@a.service", "*.service", "a?.service", "a[1].service",
		strings.Repeat("a", 248) + ".service",
	} {
		if Valid(s) {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestTemplate(t *testing.T) {
	if !IsTemplate("getty@.service") || IsTemplate("getty@tty1.service") || IsTemplate("a.service") {
		t.Error("IsTemplate")
	}
	if tpl, ok := Template("getty@tty1.service"); !ok || tpl != "getty@.service" {
		t.Error(tpl, ok)
	}
	for _, s := range []string{"a.service", "getty@.service"} {
		if _, ok := Template(s); ok {
			t.Error(s)
		}
	}
}

func FuzzValid(f *testing.F) {
	for _, s := range []string{"a.service", "getty@tty1.service", "a\\x2d.service", "-a.service"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !Valid(s) {
			return
		}
		if len(s) > MaxLength || strings.ContainsAny(s, "/ \n\t") || strings.HasPrefix(s, "-") || strings.Contains(s, "..") || !strings.HasSuffix(s, ".service") {
			t.Fatalf("accepted %q", s)
		}
	})
}
