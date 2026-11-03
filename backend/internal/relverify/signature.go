package relverify

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

const (
	// maxSignatureFile bounds checksums.txt.sig. A real one is about 300 bytes.
	maxSignatureFile = 64 << 10

	sigAlgorithm  = "Ed"
	sigPrehashed  = "ED"
	sigKeyIDLen   = 8
	sigPayloadLen = len(sigAlgorithm) + sigKeyIDLen + ed25519.SignatureSize // 74

	// Base64 lengths of the two signature lines (with padding).
	sigPayloadB64Len = 100
	sigGlobalB64Len  = 88

	untrustedPrefix = "untrusted comment: "
	trustedPrefix   = "trusted comment: "
)

// VerifySignature checks the minisign legacy-mode signature of checksums.txt
// (ADR 0010 U4, ADR 0005) against key:
//
//	untrusted comment: <text>
//	<base64 of "Ed" + 8-byte key id + 64-byte Ed25519 signature>
//	trusted comment: <text>
//	<base64 of the 64-byte signature over the trusted comment>
//
// The container is strict: exactly these four lines, each ended by a line feed,
// no carriage return or NUL byte, at most 64 KiB. The payload must be exactly
// 74 bytes with the algorithm "Ed"; the prehashed "ED" and every other
// algorithm are refused. The key id in the payload must equal the id of key; a
// different id is a failure and the signature is not tried against anything
// else. The signature must verify over the exact bytes of checksums.
//
// The trusted comment (lines 3 and 4) is ignored, like the installer ignores
// it: its text is not interpreted and its signature is not checked. Line 4 must
// only have the shape of a signature, so that a file that is not a minisign
// signature is not accepted just because line 2 is right.
//
// Every failure is a *Error with CodeSignatureInvalid, except an oversized
// checksums.txt (CodeChecksumMismatch) and a key without a public part
// (CodeInternal).
func VerifySignature(checksums, sigFile []byte, key trustkey.Key) error {
	if len(key.Public) != ed25519.PublicKeySize {
		return fail(CodeInternal, "the trusted key has no Ed25519 public key")
	}
	if len(checksums) > maxChecksumsFile {
		return fail(CodeChecksumMismatch, "checksums.txt is larger than the limit")
	}
	payload, err := parseSignatureFile(sigFile)
	if err != nil {
		return err
	}
	if string(payload[:len(sigAlgorithm)]) != sigAlgorithm {
		if string(payload[:len(sigAlgorithm)]) == sigPrehashed {
			return fail(CodeSignatureInvalid, "the signature is prehashed (ED), only legacy (Ed) is accepted")
		}
		return fail(CodeSignatureInvalid, "unknown signature algorithm")
	}
	// minisign prints a key id with the bytes reversed (trustkey.Key.ID); the
	// signature holds them in file order.
	var rev [sigKeyIDLen]byte
	for i := 0; i < sigKeyIDLen; i++ {
		rev[sigKeyIDLen-1-i] = payload[len(sigAlgorithm)+i]
	}
	if hex.EncodeToString(rev[:]) != key.ID() {
		return fail(CodeSignatureInvalid, "the key id of the signature differs from the trusted key")
	}
	if !ed25519.Verify(key.Public, checksums, payload[len(sigAlgorithm)+sigKeyIDLen:]) {
		return fail(CodeSignatureInvalid, "the signature does not verify")
	}
	return nil
}

// parseSignatureFile checks the container and returns the 74-byte payload of
// line 2.
func parseSignatureFile(file []byte) ([sigPayloadLen]byte, error) {
	var out [sigPayloadLen]byte
	if len(file) == 0 || len(file) > maxSignatureFile {
		return out, fail(CodeSignatureInvalid, "the signature file is empty or larger than the limit")
	}
	if bytes.IndexByte(file, '\r') >= 0 || bytes.IndexByte(file, 0) >= 0 {
		return out, fail(CodeSignatureInvalid, "the signature file contains a carriage return or a NUL byte")
	}
	if file[len(file)-1] != '\n' {
		return out, fail(CodeSignatureInvalid, "the signature file does not end in a line feed")
	}
	lines := bytes.Split(file[:len(file)-1], []byte{'\n'})
	if len(lines) != 4 {
		return out, fail(CodeSignatureInvalid, "the signature file does not have exactly four lines")
	}
	if !bytes.HasPrefix(lines[0], []byte(untrustedPrefix)) || !bytes.HasPrefix(lines[2], []byte(trustedPrefix)) {
		return out, fail(CodeSignatureInvalid, "the comment lines of the signature file are malformed")
	}
	if len(lines[1]) != sigPayloadB64Len || len(lines[3]) != sigGlobalB64Len {
		return out, fail(CodeSignatureInvalid, "a signature line has the wrong length")
	}
	enc := base64.StdEncoding.Strict()
	payload, err := enc.DecodeString(string(lines[1]))
	if err != nil || len(payload) != sigPayloadLen {
		return out, fail(CodeSignatureInvalid, "the signature line is not a 74-byte base64 payload")
	}
	if g, err := enc.DecodeString(string(lines[3])); err != nil || len(g) != ed25519.SignatureSize {
		return out, fail(CodeSignatureInvalid, "the trusted comment signature line is malformed")
	}
	copy(out[:], payload)
	return out, nil
}
