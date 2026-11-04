package relverify

import (
	"fmt"
	"strings"
	"testing"
)

const (
	h1 = "1111111111111111111111111111111111111111111111111111111111111111"
	h2 = "2222222222222222222222222222222222222222222222222222222222222222"
	h3 = "abcdef3333333333333333333333333333333333333333333333333333333333"
	bn = "oarfish_0.1.0-test.1_linux_arm64.tar.gz"
)

func csFile(lines ...string) []byte { return []byte(strings.Join(lines, "\n") + "\n") }

func TestChecksumsValid(t *testing.T) {
	c, err := ParseChecksums(csFile(h1+"  install.sh", h2+"  manifest.json", h3+"  "+bn, h1+"  sbom-backend.cdx.json"), bn)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Names(), ","); got != "install.sh,manifest.json,"+bn+",sbom-backend.cdx.json" {
		t.Fatalf("names %s", got)
	}
	if h, ok := c.Hash("install.sh"); !ok || h != h1 {
		t.Fatal("install.sh is exposed with its hash")
	}
	if _, ok := c.Hash("nope"); ok {
		t.Fatal("unknown name")
	}
	if err := c.Check(bn, h3); err != nil {
		t.Fatal(err)
	}
	wantCode(t, c.Check(bn, h2), CodeChecksumMismatch)
	wantCode(t, c.Check("nope", h2), CodeChecksumMismatch)
	wantCode(t, c.Check(bn, strings.ToUpper(h3)), CodeChecksumMismatch)
	// Names returns a copy.
	c.Names()[0] = "x"
	if c.Names()[0] != "install.sh" {
		t.Fatal("Names exposes internal state")
	}
}

func TestChecksumsRejected(t *testing.T) {
	m := h2 + "  manifest.json"
	b := h3 + "  " + bn
	cases := []struct {
		name string
		file []byte
	}{
		{"empty", nil},
		{"only a line feed", []byte("\n")},
		{"no final line feed", []byte(m + "\n" + b)},
		{"CRLF", []byte(m + "\r\n" + b + "\r\n")},
		{"blank line", csFile(m, "", b)},
		{"blank line at the end", append(csFile(m, b), '\n')},
		{"bundle missing", csFile(m)},
		{"manifest.json missing", csFile(b)},
		{"bundle of another version", csFile(m, h3+"  oarfish_0.1.0-test.2_linux_arm64.tar.gz")},
		{"bundle listed twice", csFile(m, b, b)},
		{"bundle listed twice with different hashes", csFile(m, b, h1+"  "+bn)},
		{"manifest.json listed twice", csFile(m, m, b)},
		{"manifest.json listed twice, other hash", csFile(m, h1+"  manifest.json", b)},
		{"another name listed twice", csFile(m, b, h1+"  install.sh", h2+"  install.sh")},
		{"hash of 63 digits", csFile(m, b, h1[:63]+"  install.sh")},
		{"hash of 65 digits", csFile(m, b, h1+"1  install.sh")},
		{"hash in upper case", csFile(m, b, strings.ToUpper("abcdef"+h1[6:])+"  install.sh")},
		{"hash with a non-hex digit", csFile(m, b, "g"+h1[1:]+"  install.sh")},
		{"one space", csFile(m, b, h1+" install.sh")},
		{"three spaces", csFile(m, b, h1+"   install.sh")},
		{"tab separator", csFile(m, b, h1+"\tinstall.sh")},
		{"binary-mode marker", csFile(m, b, h1+" *install.sh")},
		{"no name", csFile(m, b, h1+"  ")},
		{"no hash", csFile(m, b, "  install.sh")},
		{"slash in a name", csFile(m, b, h1+"  dir/install.sh")},
		{"leading slash", csFile(m, b, h1+"  /install.sh")},
		{"backslash in a name", csFile(m, b, h1+`  dir\install.sh`)},
		{"dot dot", csFile(m, b, h1+"  ..")},
		{"dot", csFile(m, b, h1+"  .")},
		{"parent path", csFile(m, b, h1+"  ../install.sh")},
		{"space in a name", csFile(m, b, h1+"  install .sh")},
		{"name of 256 characters", csFile(m, b, h1+"  "+strings.Repeat("a", 256))},
		{"non-ASCII name", csFile(m, b, h1+"  insäll.sh")},
		{"NUL in a name", csFile(m, b, h1+"  inst\x00all.sh")},
		{"too many lines", func() []byte {
			var l []string
			for i := 0; i < 63; i++ {
				l = append(l, fmt.Sprintf("%s  f%02d", h1, i))
			}
			return csFile(append(l, m, b)...)
		}()},
		{"larger than 64 KiB", []byte(strings.Repeat("x", maxChecksumsFile) + "\n")},
		{"minisign signature file", []byte("untrusted comment: x\nabc\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseChecksums(tc.file, bn)
			wantCode(t, err, CodeChecksumMismatch)
		})
	}
}

func TestChecksumsLimits(t *testing.T) {
	// Exactly 64 lines are fine.
	var l []string
	for i := 0; i < 62; i++ {
		l = append(l, fmt.Sprintf("%s  f%02d", h1, i))
	}
	l = append(l, h2+"  manifest.json", h3+"  "+bn)
	if _, err := ParseChecksums(csFile(l...), bn); err != nil {
		t.Fatal(err)
	}
	// An unusable expected name is a caller error, not a bad file.
	_, err := ParseChecksums(csFile(l...), "../x")
	wantCode(t, err, CodeBadRequest)
}

func TestSumHex(t *testing.T) {
	// SHA-256 of the empty string.
	if got := SumHex(nil); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal(got)
	}
}
