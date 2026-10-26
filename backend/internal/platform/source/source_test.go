package source

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

func TestReadFileLimit(t *testing.T) {
	fsys := fstest.MapFS{
		"small": {Data: []byte("abc")},
		"exact": {Data: []byte(strings.Repeat("x", 100))},
		"big":   {Data: []byte(strings.Repeat("x", 101))},
	}
	if got, err := ReadFile(fsys, "small", 100); err != nil || string(got) != "abc" {
		t.Fatalf("small = %q, %v", got, err)
	}
	if _, err := ReadFile(fsys, "exact", 100); err != nil {
		t.Fatalf("file of exactly the limit must be readable: %v", err)
	}
	if _, err := ReadFile(fsys, "big", 100); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized file: err = %v, want ErrTooLarge", err)
	}
	if _, err := ReadFile(fsys, "missing", 100); err == nil {
		t.Fatal("missing file must be an error")
	}
}

func TestCleanMakesUntrustedTextSafe(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"  padded \t tab ", "padded   tab"},
		{"esc\x1b[31mred\x1b[0m", "esc[31mred[0m"},
		{"nul\x00inside", "nulinside"},
		{"bell\a and newline\nhere", "bell and newlinehere"},
		{"bad\xff\xfeutf8", "bad�utf8"}, // a run of invalid bytes becomes one U+FFFD
		{"zero​width", "zerowidth"},
		{"Zürich ✓", "Zürich ✓"},
	}
	for _, c := range cases {
		if got := Clean(c.in, 100); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := Clean(strings.Repeat("ä", 50), 10); utf8.RuneCountInString(got) != 10 {
		t.Errorf("Clean must cut to 10 runes, got %d", utf8.RuneCountInString(got))
	}
}

func TestIsPlain(t *testing.T) {
	for in, want := range map[string]bool{
		"ok value": true, "": true, "tab\there": false, "nul\x00": false, "bad\xff": false, "esc\x1b[0m": false,
	} {
		if got := IsPlain(in); got != want {
			t.Errorf("IsPlain(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestExistsAndIsDir(t *testing.T) {
	fsys := fstest.MapFS{"a/b": {Data: []byte("x")}}
	if !Exists(fsys, "a/b") || !Exists(fsys, "a") || Exists(fsys, "c") {
		t.Error("Exists is wrong")
	}
	if !IsDir(fsys, "a") || IsDir(fsys, "a/b") || IsDir(fsys, "c") {
		t.Error("IsDir is wrong")
	}
}

func TestHostIsRootedAtSlash(t *testing.T) {
	// "tmp" exists at the root of every system the tests run on.
	if !IsDir(Host(), "tmp") {
		t.Skip("no /tmp on this system")
	}
}
