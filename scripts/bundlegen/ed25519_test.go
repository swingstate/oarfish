package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minisignPayload returns the two payload lines of a minisign key or signature
// file: the untrusted comment is skipped, the base64 line is decoded.
func minisignPayload(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "untrusted comment:") {
		t.Fatalf("%s: not a minisign file", path)
	}
	b, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The release signature is minisign legacy mode: algorithm "Ed", key id, then
// plain Ed25519 over the file bytes. This proves crypto/ed25519 alone verifies
// it, which is the reason ADR 0005 chose legacy mode. The fixture was signed by
// scripts/sign-release.sh with a throwaway key that was discarded.
func TestMinisignLegacySignatureVerifiesWithStdlib(t *testing.T) {
	pubRaw := minisignPayload(t, filepath.Join("testdata", "throwaway.pub"))
	sigRaw := minisignPayload(t, filepath.Join("testdata", "checksums.txt.sig"))
	msg, err := os.ReadFile(filepath.Join("testdata", "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if len(pubRaw) != 2+8+ed25519.PublicKeySize || string(pubRaw[:2]) != "Ed" {
		t.Fatalf("unexpected public key layout (%d bytes, algorithm %q)", len(pubRaw), pubRaw[:2])
	}
	if len(sigRaw) != 2+8+ed25519.SignatureSize || string(sigRaw[:2]) != "Ed" {
		t.Fatalf("signature is not legacy Ed25519 (%d bytes, algorithm %q)", len(sigRaw), sigRaw[:2])
	}
	if string(pubRaw[2:10]) != string(sigRaw[2:10]) {
		t.Fatal("key id in signature does not match the public key")
	}
	pub, sig := ed25519.PublicKey(pubRaw[10:]), sigRaw[10:]

	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("crypto/ed25519 rejects a valid signature")
	}
	tampered := append([]byte(nil), msg...)
	tampered[0] ^= 1
	if ed25519.Verify(pub, tampered, sig) {
		t.Fatal("crypto/ed25519 accepts a tampered message")
	}
}

// The committed release public key, when present in the working tree, must
// have the same layout. Skipped in checkouts that do not have it.
func TestReleasePublicKeyLayout(t *testing.T) {
	p := filepath.Join("..", "..", "release", "oarfish-release.pub")
	if _, err := os.Stat(p); err != nil {
		t.Skip("release/oarfish-release.pub not present")
	}
	b := minisignPayload(t, p)
	if len(b) != 2+8+ed25519.PublicKeySize || string(b[:2]) != "Ed" {
		t.Fatalf("release public key has unexpected layout (%d bytes, algorithm %q)", len(b), b[:2])
	}
}
