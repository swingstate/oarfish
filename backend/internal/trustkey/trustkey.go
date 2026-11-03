// Package trustkey holds the release public key that the update helper trusts
// (ADR 0010, U4) and parses the minisign public key format.
//
// The key file is a copy of release/oarfish-release.pub inside this package,
// because go:embed cannot reach outside the module directory. The CI script
// scripts/check-helper-key.sh requires the copy to equal the original, like
// scripts/check-installer-key.sh does for install.sh. No key is read from the
// file system at run time: an on-disk key would be a mutable trust root.
//
// The package only reads a public key. It does not verify signatures (the
// shared verification package belongs to G6.4) and it holds no secret.
package trustkey

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

//go:embed oarfish-release.pub
var releasePub []byte

const (
	commentPrefix = "untrusted comment: "
	algorithm     = "Ed"
	keyIDLen      = 8
	payloadLen    = len(algorithm) + keyIDLen + ed25519.PublicKeySize
	maxFileLen    = 512
)

// Key is a parsed minisign public key (legacy mode, algorithm "Ed").
type Key struct {
	// rawID is the key id in file order: the 8 bytes after the algorithm.
	rawID  [keyIDLen]byte
	Public ed25519.PublicKey
}

// ID returns the key id as 16 lowercase hexadecimal digits, in the convention
// of the manifest field components.key_id and of `bundlegen check
// -expect-key-id`: the digits minisign prints in the comment line of the key
// file (there in upper case), which are the 8 id bytes in reverse file order.
// For the release key the file holds fc0b097727c5f111 and ID returns
// 11f1c52777090bfc.
func (k Key) ID() string {
	var rev [keyIDLen]byte
	for i, b := range k.rawID {
		rev[keyIDLen-1-i] = b
	}
	return hex.EncodeToString(rev[:])
}

// Release parses the embedded release key.
func Release() (Key, error) { return Parse(releasePub) }

// Parse reads a minisign public key file: the line "untrusted comment: ...",
// then one base64 line holding "Ed", the 8-byte key id and the 32-byte
// Ed25519 key. The comment is not trusted and not interpreted. Prehashed or
// unknown algorithms, other lengths and extra lines are refused.
func Parse(file []byte) (Key, error) {
	if len(file) == 0 || len(file) > maxFileLen {
		return Key{}, errors.New("trustkey: key file has an unexpected size")
	}
	lines := strings.Split(strings.TrimSuffix(string(file), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], commentPrefix) {
		return Key{}, errors.New("trustkey: key file is not a two-line minisign public key")
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(lines[1])
	if err != nil || len(payload) != payloadLen {
		return Key{}, errors.New("trustkey: key line is not a 42-byte minisign payload")
	}
	if string(payload[:len(algorithm)]) != algorithm {
		return Key{}, errors.New("trustkey: key algorithm is not Ed")
	}
	var k Key
	copy(k.rawID[:], payload[len(algorithm):])
	k.Public = ed25519.PublicKey(append([]byte(nil), payload[len(algorithm)+keyIDLen:]...))
	return k, nil
}
