package trustkey

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

const (
	repoReleaseKey = "../../../release/oarfish-release.pub"
	throwawayKey   = "../../../scripts/bundlegen/testdata/throwaway.pub"
)

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The embedded copy must equal the committed release key. The same rule is
// enforced for CI by scripts/check-helper-key.sh.
func TestEmbeddedCopyEqualsReleaseKey(t *testing.T) {
	if !bytes.Equal(releasePub, read(t, repoReleaseKey)) {
		t.Fatal("embedded key differs from release/oarfish-release.pub")
	}
}

// The convention: ID() is the upper-case id of the comment line, lower-cased.
// The comment line is where minisign prints it, and it is what install.sh
// carries as OARFISH_KEY_ID. The throwaway key of the bundlegen fixtures has
// the opposite byte pattern in the file, so a missing reversal would show.
func TestIDFollowsTheCommentLineConvention(t *testing.T) {
	tests := []struct {
		name, path, want string
	}{
		{"release key", repoReleaseKey, "11f1c52777090bfc"},
		{"bundlegen throwaway fixture", throwawayKey, "82ae01312ca8ea61"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := read(t, tt.path)
			k, err := Parse(file)
			if err != nil {
				t.Fatal(err)
			}
			if got := k.ID(); got != tt.want {
				t.Fatalf("ID() = %s, want %s", got, tt.want)
			}
			comment := strings.SplitN(string(file), "\n", 2)[0]
			if !strings.HasSuffix(comment, " "+strings.ToUpper(k.ID())) {
				t.Fatalf("comment line %q does not end in the upper-case id", comment)
			}
		})
	}
}

func TestReleaseKey(t *testing.T) {
	k, err := Release()
	if err != nil {
		t.Fatal(err)
	}
	if k.ID() != "11f1c52777090bfc" || len(k.Public) != 32 {
		t.Fatalf("unexpected release key: id %s, %d key bytes", k.ID(), len(k.Public))
	}
}

func TestParseRefuses(t *testing.T) {
	good := string(releasePub)
	payload := func(mutate func([]byte) []byte) string {
		p, _ := base64.StdEncoding.DecodeString(strings.Split(good, "\n")[1])
		return "untrusted comment: x\n" + base64.StdEncoding.EncodeToString(mutate(p)) + "\n"
	}
	tests := map[string]string{
		"empty":             "",
		"one line":          "untrusted comment: x\n",
		"no comment":        strings.SplitN(good, "\n", 2)[1],
		"three lines":       good + "extra\n",
		"wrong comment":     "comment: x\n" + strings.Split(good, "\n")[1] + "\n",
		"not base64":        "untrusted comment: x\n!!!!\n",
		"prehashed ED":      payload(func(p []byte) []byte { p[1] = 'D'; return p }),
		"unknown algorithm": payload(func(p []byte) []byte { p[0] = 'X'; return p }),
		"short payload":     payload(func(p []byte) []byte { return p[:len(p)-1] }),
		"long payload":      payload(func(p []byte) []byte { return append(p, 0) }),
		"oversized file":    good + strings.Repeat("a", 600),
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseAcceptsMissingFinalNewline(t *testing.T) {
	if _, err := Parse(bytes.TrimSuffix(releasePub, []byte("\n"))); err != nil {
		t.Fatal(err)
	}
}
