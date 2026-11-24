package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The two unit files are exactly the text of ADR 0011 B8 (A3 P-B8-1: no
// change), one file per unit of the block.
func TestUnitFilesMatchTheADR(t *testing.T) {
	adr := readRepo(t, "docs/adr/0011-privilege-broker.md")
	start := strings.Index(adr, "```ini\n# oarfish-broker.socket\n")
	if start < 0 {
		t.Fatal("B8 block not found")
	}
	block := adr[start+len("```ini\n"):]
	block = block[:strings.Index(block, "```")]
	socket, service, ok := strings.Cut(block, "\n# oarfish-broker.service\n")
	if !ok {
		t.Fatal("B8 block has no service part")
	}
	want := map[string]string{
		"install/oarfish-broker.socket":  strings.TrimSuffix(socket, "\n") + "\n",
		"install/oarfish-broker.service": "# oarfish-broker.service\n" + service,
	}
	for file, text := range want {
		if got := readRepo(t, file); got != text {
			t.Errorf("%s differs from the text of B8:\n%s\nwant:\n%s", file, got, text)
		}
	}
	// What the code relies on is in that text.
	if !strings.Contains(service, "ExecStart=/usr/libexec/oarfish-broker serve\n") || !strings.Contains(socket, "ListenStream=/run/oarfish-broker/broker.sock\n") {
		t.Error("ExecStart or ListenStream changed")
	}
}

// The broker's codes are the closed set of B2 (as amended by A3 and D-C89),
// without the service's own codes, plus success and accepted.
func TestCodesMatchTheADR(t *testing.T) {
	adr := readRepo(t, "docs/adr/0011-privilege-broker.md")
	re := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\| broker \\|")
	var fromADR []string
	for _, m := range re.FindAllStringSubmatch(adr, -1) {
		if !slices.Contains(fromADR, m[1]) {
			fromADR = append(fromADR, m[1])
		}
	}
	var ours []string
	for _, c := range allCodes {
		if c != codeSuccess && c != codeAccepted {
			ours = append(ours, string(c))
		}
	}
	slices.Sort(fromADR)
	slices.Sort(ours)
	if !slices.Equal(fromADR, ours) {
		t.Fatalf("codes of B2: %v\nbroker: %v", fromADR, ours)
	}
}
