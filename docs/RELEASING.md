# Releasing

How a version tag becomes a signed GitHub Release (Gate G2). The release path is pull-based: GitHub-hosted runners build and publish, nothing connects to a device (ADR 0003, ORB-CICD-005). There is no installer yet; `install.sh` is Gate G3.

## What a release contains

| File | Purpose |
|---|---|
| `orbit_<version>_linux_arm64.tar.gz` | Bundle: `orbit_<version>_linux_arm64/orbit` (static binary, frontend embedded) and `LICENSE` |
| `manifest.json` | Machine-readable metadata (see below) |
| `checksums.txt` | SHA-256 of the bundle, `manifest.json` and the SBOMs, in `sha256sum` format |
| `checksums.txt.sig` | minisign (Ed25519) signature of `checksums.txt` |
| `sbom-backend.cdx.json`, `sbom-frontend.cdx.json` | CycloneDX SBOMs (ORB-SEC-009, SOLL) |

`<version>` is the tag without the leading `v`. A tag with a hyphen (`v0.1.0-alpha.1`) is published as a prerelease.

`manifest.json` fields: `schema_version`, `name`, `version`, `tag`, `prerelease`, `commit` (40 hex), `build_date` (RFC 3339, equal to the commit timestamp so the file is reproducible), `os`, `architecture`, `requirements` (`os`, `architecture`, `cpu`, `libc`, `runtime_dependencies`), `bundle` (`file`, `sha256`, `size`) and `contents` (path, mode, size, SHA-256 of every file in the tarball). Kernel and memory minimums are not included yet because they have not been measured on a device (ORB-REL-002 is met for architecture, OS and libc only).

## One-time setup: signing key (maintainer, local machine)

The private key must never enter the repository, a log or a chat. Claude Code does not generate or read it. Run these on your own machine:

```bash
brew install minisign        # macOS; Debian/Ubuntu: sudo apt install minisign

# 1. Key pair. The public key is written into the repository working tree,
#    the private key outside it. -W = no password (see ADR 0005).
mkdir -p -m 700 ~/.config/orbit-release
minisign -G -W -p release/orbit-release.pub -s ~/.config/orbit-release/orbit-release.key

# 2. Store the private key as a repository secret, read from the file (not printed).
gh secret set ORBIT_RELEASE_SIGNING_KEY --repo swingstate/orbit-linux < ~/.config/orbit-release/orbit-release.key
gh secret list --repo swingstate/orbit-linux

# 3. Back up ~/.config/orbit-release/orbit-release.key in a password manager.
#    Losing it means devices that embed the public key cannot accept new releases.
```

Commit only `release/orbit-release.pub`. `*.key` is git-ignored as a safety net. Recommended, not configured by this gate: a tag protection rule for `v*` so only maintainers can create release tags.

## Dry run

Actions → Release → Run workflow (leave "Dry run" ticked), or:

```bash
gh workflow run release.yml --ref <branch> -f dry_run=true
```

It runs CI, builds the bundle, checks reproducibility, size budgets and the ARM64 smoke test, generates SBOMs and checksums, signs with a throwaway key and verifies. It needs no secret, uses version `0.0.0-dryrun.<sha>` and publishes nothing. The signed output of a dry run is discarded.

## Release procedure

1. Merge the change to `main`; wait for CI.
2. Tag a commit that is on `main` and push the tag:
   ```bash
   git tag v0.1.0-alpha.1 && git push origin v0.1.0-alpha.1
   ```
   The workflow rejects tags that are not `vMAJOR.MINOR.PATCH[-prerelease]` and tags whose commit is not on `main`.
3. Watch it: `gh run watch`. Jobs: `ci` (the full CI workflow) → `build` → `sign` → `publish`. Only `sign` sees the secret; only `publish` has `contents: write`.
4. Verify the published assets (below).

A tag run can also be started manually with `dry_run` unticked, selecting the tag as the ref. If a release fails halfway, delete the draft/partial release and the tag, fix the cause and tag again. Never reuse a published version.

## Verify a release manually

Needs `minisign` and the public key `release/orbit-release.pub` from the repository at the release commit.

```bash
mkdir verify && cd verify
gh release download v0.1.0-alpha.1 --repo swingstate/orbit-linux
minisign -Vm checksums.txt -x checksums.txt.sig -p /path/to/orbit-release.pub   # 1. signature
sha256sum -c checksums.txt                                                      # 2. hashes (macOS: shasum -a 256 -c checksums.txt)
jq . manifest.json                                                              # 3. metadata
tar -tzvf orbit_0.1.0-alpha.1_linux_arm64.tar.gz                                # 4. contents
```

The repository script does the same plus consistency checks between manifest, checksums and tarball contents: `scripts/verify-release.sh <dir> release/orbit-release.pub`.

Order matters: check the signature first, because the hashes are only as trustworthy as the signed `checksums.txt`.

## Build a bundle locally (unsigned)

```bash
make release-bundle        # frontend build + scripts/release-bundle.sh -> dist/release/
```

This is the same script CI uses. `VERSION=<semver>` and `COMMIT=<sha>` can be set; the default version is `0.0.0-dev.<short sha>` (plus `.dirty` with local changes). The bundle is only reproducible for the same commit, the same version string and the same Go toolchain (`backend/go.mod`).

## Reproducibility

The bundle is a pure function of commit, version and toolchain:

- Go: `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, `-mod=readonly`, version and commit via `-ldflags`.
- Tar and gzip are written by `scripts/bundlegen` (Go standard library), not by the platform `tar`: entries sorted, mtime = commit timestamp, owner 0:0 numeric without names, gzip header without name or timestamp, fixed compression level. `build_date` in the manifest is the commit timestamp.

Check it:

```bash
make check-reproducible    # rebuilds the frontend and the bundle twice, empty Go cache each time
```

`scripts/check-reproducible.sh` compares the SHA-256 of both bundles and the manifests. CI runs it on every change (job "Reproducible bundle"), and the release workflow additionally compares two rebuilds with the bundle it is about to publish. Not covered: a different Go version or Node version can change the output (the Go version is pinned through `go.mod`, Node through `frontend/.node-version` and the lockfile); SBOM files contain timestamps and UUIDs and are not reproducible.

## Public key in installer and updater (not implemented in this gate)

`release/orbit-release.pub` is the single source of the trusted key. Gate G3 will embed its key line in `install.sh`; Gate G6 will embed the file in the Orbit binary with `go:embed`. Both must verify `checksums.txt.sig` before trusting any hash. See ADR 0005 for the open points (verification mechanics on the device, key rotation).

## Known limitations

- Unverified until the first real release: the publish job, `gh release create`, prerelease flag, and the secret-based signing path. The dry run exercises everything except those and the committed public key.
- The ARM64 smoke test runs under qemu-user on an x86-64 runner. It proves the binary starts and serves; it is not a test on real hardware (nothing has run on a Raspberry Pi yet).
- No SLSA provenance or artifact attestation, no signature on the SBOMs other than through `checksums.txt`.
- Key rotation and revocation are not designed (ADR 0005).
