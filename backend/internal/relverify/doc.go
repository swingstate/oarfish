// Package relverify verifies a release of Oarfish the way ADR 0010 U4 and U5
// describe: the signature of checksums.txt, the checksums, the manifest, the
// version rules and the inspection of the tarball. It is the shared
// verification package of gate G6.4. The update helper (G6.5) and the service
// (G6.7) will both import it; in G6.4 no binary imports it yet, so it is not
// linked into the service or the helper and changes no behaviour on a device.
//
// # Design
//
// The package is standard library plus internal/trustkey, nothing else, and
// every function is pure: no network, no clock, no environment, no file access.
// Input is bytes (the three small files) or an io.Reader (the bundle), always
// bounded: checksums.txt, checksums.txt.sig and manifest.json at most 64 KiB, a
// version at most 64 characters, the files of a bundle at most 128 MiB in total.
// Nothing panics on malformed input, nothing starts a goroutine, there is no
// global mutable state, and no regular expression is applied to input.
//
// The surface is small:
//
//	VerifySignature   minisign legacy ("Ed") signature of checksums.txt
//	ParseChecksums    strict parser of checksums.txt
//	ParseManifest     strict-but-additive decoder of manifest.json
//	VerifyMetadata    the three above in the order of U4, plus the checks that
//	                  tie them to the request; returns a Release
//	Release.CheckBundle  hash and size of the downloaded bundle
//	Inspect           streaming inspection of the tarball, handing verified
//	                  files to a Sink
//	ParseVersion, Compare, CheckNewer, Eligible, Latest
//	CheckMinInstalled, CheckHelperProtocol, CheckUpgrade
//	                  the version and requirement rules
//
// A caller runs VerifyMetadata first (the service can do that before it
// downloads the bundle, to fail early; the helper does it on its private
// copies), then CheckBundle with the SHA-256 and size it computed itself over
// the bytes it holds, then CheckUpgrade against the installed version, and
// finally Inspect, ideally twice (once with a nil Sink to verify, once with the
// real one).
//
// # Errors
//
// Every failure is an *Error with a Code from the closed list of ADR 0010 U2 and
// the fixed text of that code. Error() never contains anything from the verified
// files; Detail() is a fixed sentence for the log that says which rule failed.
// U2 has no code for a malformed bundle: such failures are mapped to the nearest
// one (CodeManifestInvalid for a violation of the extraction rules,
// CodeChecksumMismatch for a size or hash that differs, CodeRequirementsNotMet
// for a program that is not an arm64 ELF executable, CodeIOError for a failing
// reader or sink).
//
// # Where it is deliberately not like bundlegen
//
// scripts/bundlegen check is the producer side and decodes manifest.json
// strictly: an unknown field is an error. This package is the consumer on the
// device, so unknown fields are ignored and additive changes of the manifest
// stay compatible. Its other rules are not weaker: a known field that is
// missing, mistyped or holds an unknown value fails. It is also stricter than
// the producer in two places: checksums.txt names are limited to [0-9A-Za-z._-],
// and a key that differs from a known manifest field only in letter case, or a
// repeated key, is refused (encoding/json would accept both).
//
// # The Sink and what the caller must clean up
//
// Inspect decides about a file only after its last byte, and about the bundle
// only after its last entry, so a Sink can have received bytes of a file or
// whole files before a later failure. Finish is called only for a file that
// matched the manifest, and never after a failure. The caller (the helper's
// staging code) must treat everything the sink wrote as unusable after any error
// from Inspect and remove the whole staging directory; a crash leaves the same
// state, which the helper's recovery removes (ADR 0010 U7, row 2).
//
// Nothing in this package is verified on hardware.
package relverify
