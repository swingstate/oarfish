package relverify

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

var sigMsg = []byte("0000000000000000000000000000000000000000000000000000000000000000  fixture.bin\n")

// sigLines returns the four lines of a signature file without line feeds.
func sigLines(f []byte) []string { return strings.Split(strings.TrimSuffix(string(f), "\n"), "\n") }

func joinLines(l ...string) []byte { return []byte(strings.Join(l, "\n") + "\n") }

func TestSignatureValid(t *testing.T) {
	k := newTestKey(t, "a")
	if err := VerifySignature(sigMsg, k.sign(sigMsg), k.key); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureTamperedMessage(t *testing.T) {
	k := newTestKey(t, "a")
	sig := k.sign(sigMsg)
	for _, tc := range []struct {
		name string
		msg  []byte
	}{
		{"extra line", append(append([]byte(nil), sigMsg...), "extra\n"...)},
		{"first byte", append([]byte("1"), sigMsg[1:]...)},
		{"last byte", append(append([]byte(nil), sigMsg[:len(sigMsg)-1]...), 'X')},
		{"truncated", sigMsg[:len(sigMsg)-1]},
		{"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, VerifySignature(tc.msg, sig, k.key), CodeSignatureInvalid)
		})
	}
	for i := range sigMsg {
		m := append([]byte(nil), sigMsg...)
		m[i] ^= 0x01
		if VerifySignature(m, sig, k.key) == nil {
			t.Fatalf("message with bit flipped at byte %d accepted", i)
		}
	}
}

// Every byte of the 74-byte payload, flipped in turn, must fail: the algorithm
// bytes, the key id and the signature.
func TestSignaturePayloadEveryByteFlipped(t *testing.T) {
	k := newTestKey(t, "a")
	payload := k.payload("Ed", sigMsg)
	if len(payload) != 74 {
		t.Fatalf("payload is %d bytes", len(payload))
	}
	for i := range payload {
		for _, mask := range []byte{0x01, 0x80, 0xff} {
			p := append([]byte(nil), payload...)
			p[i] ^= mask
			err := VerifySignature(sigMsg, sigFile(p, "oarfish test"), k.key)
			if err == nil {
				t.Fatalf("payload with byte %d xor %#x accepted", i, mask)
			}
			if codeOf(err) != CodeSignatureInvalid {
				t.Fatalf("byte %d: code %s", i, codeOf(err))
			}
		}
	}
}

func TestSignatureOtherKey(t *testing.T) {
	a := newTestKey(t, "a")
	b := newTestKey(t, "b")
	t.Run("different key id", func(t *testing.T) {
		err := VerifySignature(sigMsg, b.sign(sigMsg), a.key)
		wantDetail(t, err, CodeSignatureInvalid, "key id")
	})
	t.Run("same key id, different key", func(t *testing.T) {
		// A key with a's id and b's key material: only the Ed25519 check can
		// tell them apart.
		seed := [32]byte{1, 2, 3}
		imposter := testKeyWithID(t, seed, a.id)
		err := VerifySignature(sigMsg, imposter.sign(sigMsg), a.key)
		wantDetail(t, err, CodeSignatureInvalid, "does not verify")
	})
	t.Run("signature of a, trusted key b", func(t *testing.T) {
		wantCode(t, VerifySignature(sigMsg, a.sign(sigMsg), b.key), CodeSignatureInvalid)
	})
	t.Run("only the key id bytes changed", func(t *testing.T) {
		// The Ed25519 signature is untouched and valid for the key; the id is
		// compared and refused, with no retry under another id.
		p := a.payload("Ed", sigMsg)
		p[2] ^= 0xff
		wantDetail(t, VerifySignature(sigMsg, sigFile(p, "x"), a.key), CodeSignatureInvalid, "key id")
	})
	t.Run("zero key", func(t *testing.T) {
		wantCode(t, VerifySignature(sigMsg, a.sign(sigMsg), trustkey.Key{}), CodeInternal)
	})
}

// The prehashed format (minisign's default) is refused even when it is a valid
// signature of the trusted key, and even when its algorithm bytes are the only
// thing wrong.
func TestSignaturePrehashedRefused(t *testing.T) {
	k := newTestKey(t, "a")
	t.Run("valid prehashed signature made by minisign", func(t *testing.T) {
		// A real signature in minisign's default format, with a discarded
		// throwaway key (testdata/README.md).
		pub, err := os.ReadFile("testdata/prehashed/throwaway.pub")
		if err != nil {
			t.Fatal(err)
		}
		msg, err := os.ReadFile("testdata/prehashed/message.txt")
		if err != nil {
			t.Fatal(err)
		}
		sig, err := os.ReadFile("testdata/prehashed/message.txt.minisig")
		if err != nil {
			t.Fatal(err)
		}
		pk, err := trustkey.Parse(pub)
		if err != nil {
			t.Fatal(err)
		}
		if p := sigLines(sig)[1]; !strings.HasPrefix(p, "RU") { // base64 of "ED"
			t.Fatalf("the fixture is not a prehashed signature: %s", p)
		}
		wantDetail(t, VerifySignature(msg, sig, pk), CodeSignatureInvalid, "prehashed")
		// It is a valid signature for minisign itself, when minisign is here.
		if bin, err := exec.LookPath("minisign"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, bin, "-V", "-p", "testdata/prehashed/throwaway.pub",
				"-x", "testdata/prehashed/message.txt.minisig", "-m", "testdata/prehashed/message.txt").CombinedOutput()
			if err != nil {
				t.Fatalf("minisign rejects the fixture: %v\n%s", err, out)
			}
		}
	})
	t.Run("legacy signature with the algorithm bytes changed to ED", func(t *testing.T) {
		// The Ed25519 signature over the message is valid, so the algorithm
		// check is the only thing that refuses it.
		p := k.payload("Ed", sigMsg)
		p[1] = 'D'
		if err := VerifySignature(sigMsg, sigFile(p, "x"), k.key); err == nil {
			t.Fatal("accepted")
		} else if !strings.Contains(detail(err), "prehashed") {
			t.Fatalf("refused for another reason: %s", detail(err))
		}
	})
	t.Run("unknown algorithm", func(t *testing.T) {
		p := k.payload("Ed", sigMsg)
		p[0], p[1] = 'X', 'Y'
		wantDetail(t, VerifySignature(sigMsg, sigFile(p, "x"), k.key), CodeSignatureInvalid, "unknown")
	})
}

func TestSignatureContainer(t *testing.T) {
	k := newTestKey(t, "a")
	good := k.sign(sigMsg)
	l := sigLines(good)
	cases := []struct {
		name string
		file []byte
	}{
		{"empty", nil},
		{"only a line feed", []byte("\n")},
		{"one line", joinLines(l[0])},
		{"two lines", joinLines(l[0], l[1])},
		{"three lines", joinLines(l[0], l[1], l[2])},
		{"five lines", joinLines(l[0], l[1], l[2], l[3], "extra")},
		{"blank line at the end", append(append([]byte(nil), good...), '\n')},
		{"blank line in the middle", joinLines(l[0], l[1], "", l[2], l[3])},
		{"no final line feed", bytes.TrimSuffix(good, []byte("\n"))},
		{"CRLF line endings", bytes.ReplaceAll(good, []byte("\n"), []byte("\r\n"))},
		{"stray carriage return", bytes.Replace(good, []byte("test"), []byte("te\rst"), 1)},
		{"NUL byte in a comment", bytes.Replace(good, []byte("secret"), []byte("sec\x00ret"), 1)},
		{"missing untrusted comment prefix", joinLines("comment: x", l[1], l[2], l[3])},
		{"missing trusted comment prefix", joinLines(l[0], l[1], "comment: x", l[3])},
		{"payload line first", joinLines(l[1], l[0], l[2], l[3])},
		{"too long", append(append([]byte(nil), good...), bytes.Repeat([]byte("a"), maxSignatureFile)...)},
		{"just over the limit", append([]byte(l[0]+"\n"), bytes.Repeat([]byte("a"), maxSignatureFile)...)},
		{"garbage", []byte("not a signature\n")},
		{"the message instead", sigMsg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, VerifySignature(sigMsg, tc.file, k.key), CodeSignatureInvalid)
		})
	}
	t.Run("every strict prefix fails", func(t *testing.T) {
		for n := 0; n < len(good); n++ {
			if VerifySignature(sigMsg, good[:n], k.key) == nil {
				t.Fatalf("prefix of %d bytes accepted", n)
			}
		}
	})
}

func TestSignatureBase64(t *testing.T) {
	k := newTestKey(t, "a")
	l := sigLines(k.sign(sigMsg))
	enc := l[1]
	cases := map[string]string{
		"invalid character":    "!" + enc[1:],
		"url-safe alphabet":    strings.NewReplacer("+", "-", "/", "_").Replace(strings.Repeat("+/", 50)),
		"padding removed":      enc[:len(enc)-2],
		"too short (73 bytes)": base64.StdEncoding.EncodeToString(make([]byte, 73)),
		"too long (75 bytes)":  base64.StdEncoding.EncodeToString(make([]byte, 75)),
		"space inside":         enc[:10] + " " + enc[11:],
		"line 100 chars of x":  strings.Repeat("x", 100),
		"empty":                "",
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			wantCode(t, VerifySignature(sigMsg, joinLines(l[0], e, l[2], l[3]), k.key), CodeSignatureInvalid)
		})
	}
	t.Run("non-canonical trailing bits", func(t *testing.T) {
		// 74 bytes end in "==" with two spare bits; setting them is accepted
		// by a lenient decoder, which would give a second spelling of the
		// same signature.
		b := []byte(enc)
		i := len(b) - 3
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
		idx := strings.IndexByte(alphabet, b[i])
		b[i] = alphabet[idx|1]
		if string(b) == enc {
			b[i] = alphabet[idx&^1]
		}
		wantCode(t, VerifySignature(sigMsg, joinLines(l[0], string(b), l[2], l[3]), k.key), CodeSignatureInvalid)
	})
	t.Run("bad line 4", func(t *testing.T) {
		wantCode(t, VerifySignature(sigMsg, joinLines(l[0], l[1], l[2], "short"), k.key), CodeSignatureInvalid)
		wantCode(t, VerifySignature(sigMsg, joinLines(l[0], l[1], l[2], strings.Repeat("!", 88)), k.key), CodeSignatureInvalid)
	})
}

// The trusted comment and its signature are ignored (ADR 0005, ADR 0010 U4).
func TestSignatureTrustedCommentIgnored(t *testing.T) {
	k := newTestKey(t, "a")
	l := sigLines(k.sign(sigMsg))
	other := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x55}, 64))
	for name, file := range map[string][]byte{
		"another trusted comment":         joinLines(l[0], l[1], "trusted comment: something else entirely", l[3]),
		"empty trusted comment":           joinLines(l[0], l[1], "trusted comment: ", l[3]),
		"another global signature":        joinLines(l[0], l[1], l[2], other),
		"another untrusted comment":       joinLines("untrusted comment: ", l[1], l[2], l[3]),
		"a long comment with odd symbols": joinLines("untrusted comment: "+strings.Repeat("é€ ", 500), l[1], "trusted comment: "+strings.Repeat("\t", 100), l[3]),
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifySignature(sigMsg, file, k.key); err != nil {
				t.Fatalf("%v (%s)", err, detail(err))
			}
		})
	}
}

func TestSignatureOversizedMessage(t *testing.T) {
	k := newTestKey(t, "a")
	big := bytes.Repeat([]byte("a"), maxChecksumsFile+1)
	wantCode(t, VerifySignature(big, k.sign(big), k.key), CodeChecksumMismatch)
}

// A signature over the exact bytes: it does not cover a different spelling of
// the same text.
func TestSignatureCoversExactBytes(t *testing.T) {
	k := newTestKey(t, "a")
	crlf := bytes.ReplaceAll(sigMsg, []byte("\n"), []byte("\r\n"))
	wantCode(t, VerifySignature(crlf, k.sign(sigMsg), k.key), CodeSignatureInvalid)
}
