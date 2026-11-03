package relverify

import "github.com/swingstate/oarfish/backend/internal/trustkey"

// Metadata is the input of VerifyMetadata: the three small files of a release
// as bytes, and what the verifier knows.
type Metadata struct {
	// Key is the trusted release key (trustkey.Release in production).
	Key trustkey.Key
	// Version is the requested version, without a leading "v".
	Version string
	// Architecture is the architecture the verifier runs for ("arm64").
	Architecture string

	Checksums []byte // checksums.txt
	Signature []byte // checksums.txt.sig
	Manifest  []byte // manifest.json
}

// Release is a release whose small files have been verified.
type Release struct {
	Checksums *Checksums
	Manifest  *Manifest
}

// VerifyMetadata runs the checks of ADR 0010 U4 that need no bundle, in this
// order, and stops at the first failure:
//
//  1. the signature of checksums.txt (VerifySignature);
//  2. the requested version is valid;
//  3. checksums.txt parses and lists the bundle and manifest.json once each;
//  4. the SHA-256 of manifest.json equals the listed one;
//  5. the manifest parses and agrees with the request (ParseManifest, Manifest.Check).
//
// It does not look at the installed version: replay and downgrade protection
// is the separate step CheckUpgrade, because a validly signed old release must
// verify here and be refused there. The bundle is checked by Release.CheckBundle
// and Inspect once it is available.
func VerifyMetadata(in Metadata) (*Release, error) {
	if err := VerifySignature(in.Checksums, in.Signature, in.Key); err != nil {
		return nil, err
	}
	ver, err := ParseVersion(in.Version)
	if err != nil {
		return nil, err
	}
	bundle := BundleFileName(ver)
	cs, err := ParseChecksums(in.Checksums, bundle)
	if err != nil {
		return nil, err
	}
	if len(in.Manifest) > maxManifestFile {
		return nil, fail(CodeManifestInvalid, "manifest.json is larger than the limit")
	}
	if err := cs.Check(ManifestName, SumHex(in.Manifest)); err != nil {
		return nil, err
	}
	m, err := ParseManifest(in.Manifest)
	if err != nil {
		return nil, err
	}
	listed, _ := cs.Hash(bundle)
	if err := m.Check(Expect{Version: in.Version, Architecture: in.Architecture, BundleFile: bundle, BundleSHA256: listed}); err != nil {
		return nil, err
	}
	return &Release{Checksums: cs, Manifest: m}, nil
}

// CheckBundle requires the SHA-256 and the size of the bundle file, as the
// caller computed them over the bytes it holds, to equal the signed values: the
// hash listed in checksums.txt (which VerifyMetadata already showed to equal
// bundle.sha256) and bundle.size.
func (r *Release) CheckBundle(sha256hex string, size int64) error {
	if r == nil || r.Checksums == nil || r.Manifest == nil {
		return fail(CodeInternal, "no verified release")
	}
	if err := r.Checksums.Check(r.Manifest.Bundle.File, sha256hex); err != nil {
		return err
	}
	if size != r.Manifest.Bundle.Size {
		return fail(CodeChecksumMismatch, "the bundle size differs from bundle.size")
	}
	return nil
}
