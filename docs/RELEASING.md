# Releasing

How a version tag becomes a signed GitHub Release (Gate G2) that includes the installer (Gate G3). The release path is pull-based: GitHub-hosted runners build and publish, nothing connects to a device (ADR 0003, ORB-CICD-005). What a user does with a release is in [`INSTALL.md`](INSTALL.md).

## What a release contains

| File | Purpose |
|---|---|
| `oarfish_<version>_linux_arm64.tar.gz` | Bundle: `oarfish_<version>_linux_arm64/oarfish` (static binary, frontend embedded), `oarfish.service`, `LICENSE` and `THIRD_PARTY_NOTICES.txt` (licence texts of the Svelte runtime, the Vite helper snippet, the Go runtime and, since G5.1, `golang.org/x/crypto`, which are compiled into the binary and whose licences require the notice to travel with it) |
| `install.sh` | The installer ([`INSTALL.md`](INSTALL.md)): `install/install.sh` with the release version written into one line (below). Listed in `checksums.txt`, so the signature covers it |
| `manifest.json` | Machine-readable metadata (see below) |
| `checksums.txt` | SHA-256 of the bundle, `install.sh`, `manifest.json` and the SBOMs, in `sha256sum` format |
| `checksums.txt.sig` | minisign signature of `checksums.txt`, legacy mode: plain Ed25519 over the file bytes (ADR 0005) |
| `sbom-backend.cdx.json`, `sbom-frontend.cdx.json` | CycloneDX SBOMs (ORB-SEC-009, SOLL) |

`<version>` is the tag without the leading `v`. A tag with a hyphen (`v0.1.0-alpha.1`) is published as a prerelease.

The bundle contains `oarfish`, `oarfish.service` (the systemd unit the installer installs), `LICENSE` and `THIRD_PARTY_NOTICES.txt`. The manifest schema did not change for the installer: `install.sh` is a release asset next to the bundle, covered by `checksums.txt`, not part of the tarball, so the manifest's `contents` do not list it. `bundlegen check` requires it in `checksums.txt` and checks its stamp instead.

`manifest.json` fields: `schema_version`, `name`, `version`, `tag`, `prerelease`, `commit` (40 hex), `build_date` (RFC 3339, equal to the commit timestamp so the file is reproducible), `os`, `architecture`, `requirements` (`os`, `architecture`, `cpu`, `libc`, `runtime_dependencies`), `bundle` (`file`, `sha256`, `size`) and `contents` (path, mode, size, SHA-256 of every file in the tarball). Kernel and memory minimums are not included yet because they have not been measured on a device (ORB-REL-002 is met for architecture, OS and libc only).

## One-time setup: signing key (maintainer, local machine)

The private key must never enter the repository, a log or a chat. Claude Code does not generate or read it. Run these on your own machine:

```bash
brew install minisign        # macOS; Debian/Ubuntu: sudo apt install minisign

# 1. Key pair. The public key is written into the repository working tree,
#    the private key outside it. -W = no password (see ADR 0005).
mkdir -p -m 700 ~/.config/oarfish-release
minisign -G -W -p release/oarfish-release.pub -s ~/.config/oarfish-release/oarfish-release.key

# 2. Store the private key as a repository secret, read from the file (not printed).
gh secret set OARFISH_RELEASE_SIGNING_KEY --repo swingstate/oarfish < ~/.config/oarfish-release/oarfish-release.key
gh secret list --repo swingstate/oarfish

# 3. Back up ~/.config/oarfish-release/oarfish-release.key in a password manager.
#    Losing it means devices that embed the public key cannot accept new releases.
```

Commit only `release/oarfish-release.pub`. `*.key` is git-ignored as a safety net. Recommended, not configured by this gate: a tag protection rule for `v*` so only maintainers can create release tags.

## Dry run

Actions → Release → Run workflow, or:

```bash
gh workflow run release.yml --ref <branch>
```

It runs CI, builds the bundle, checks reproducibility, size budgets and the ARM64 smoke test, generates SBOMs and checksums, signs with a throwaway key and verifies. A manual run is always a dry run, and so is the run triggered by a pull request that touches the release files. It uses version `0.0.0-dryrun.<sha>` and publishes nothing; the signed output is discarded. The job that references the release secret (`sign`) is skipped in these runs, so the secret is never sent to a runner; they use the job `sign-dry-run`.

## Release procedure

1. Merge the change to `main`; wait for CI.
2. Tag a commit that is on `main` and push the tag:
   ```bash
   git tag v0.1.0-alpha.1 && git push origin v0.1.0-alpha.1
   ```
   The workflow rejects tags that are not `vMAJOR.MINOR.PATCH[-prerelease]` and tags whose commit is not on `main`.
3. Watch it: `gh run watch`. Jobs: `ci` (the full CI workflow) → `build` → `sign` → `publish`. Only `sign` references the secret and it only starts for a tag push; only `publish` has `contents: write`.
4. Verify the published assets (below).
5. **Release notes of the G5.1 release (and any release a device may reach from `v0.1.0-alpha.5` or earlier)** must say: "After this update the device has no administrator account and starts in setup mode. The GUI shows the setup page; read the setup token on the device with `sudo cat /var/lib/oarfish/setup-token`. The dashboard and the device page show data only after setup and login. Every `/api/v1` route except the session status, login and setup now needs a session; `/healthz` stays public." (ADR 0009, Consequences; D-C46)

Publishing happens only from a tag push; there is no manual publish path. If a release fails halfway, delete the draft/partial release and the tag, fix the cause and tag again. Never reuse a published version.

## Verify a release manually

Needs `minisign` and the public key `release/oarfish-release.pub` from the repository at the release commit.

```bash
mkdir verify && cd verify
gh release download v0.1.0-alpha.1 --repo swingstate/oarfish
minisign -Vm checksums.txt -x checksums.txt.sig -p /path/to/oarfish-release.pub   # 1. signature
sha256sum -c checksums.txt                                                      # 2. hashes (macOS: shasum -a 256 -c checksums.txt)
jq . manifest.json                                                              # 3. metadata
tar -tzvf oarfish_0.1.0-alpha.1_linux_arm64.tar.gz                                # 4. contents
```

The repository script does the same plus consistency checks between manifest, checksums and tarball contents: `scripts/verify-release.sh <dir> release/oarfish-release.pub`.

Order matters: check the signature first, because the hashes are only as trustworthy as the signed `checksums.txt`.

### Signature format

Releases are signed with `minisign -S -l` (legacy mode, algorithm id `Ed`), so the signature is plain Ed25519 over the bytes of `checksums.txt` and can be verified with Go's `crypto/ed25519` without BLAKE2b. `minisign -V` accepts both modes; `scripts/verify-release.sh` additionally rejects anything that is not `Ed`. **The format is fixed once `v0.1.0-alpha.1` is published:** installer and updater will be written against it, so `-l` in `scripts/sign-release.sh` must not be removed afterwards. Rationale and file layout: ADR 0005, Amendment.

## Verify a release anonymously

For a reader without a GitHub account, token or `gh`. The repository is public (D-C17), so the URLs answer without a login. Needs `curl` and `minisign`.

```bash
TAG=v0.1.0-alpha.2
VER=${TAG#v}
mkdir verify && cd verify
base="https://github.com/swingstate/oarfish/releases/download/$TAG"
for f in checksums.txt checksums.txt.sig install.sh manifest.json sbom-backend.cdx.json sbom-frontend.cdx.json \
         "oarfish_${VER}_linux_arm64.tar.gz"; do
  curl -fsSLO "$base/$f"
done
# The public key at the tagged commit (the file the release workflow verifies against).
curl -fsSL -o oarfish-release.pub \
  "https://raw.githubusercontent.com/swingstate/oarfish/$TAG/release/oarfish-release.pub"

minisign -Vm checksums.txt -x checksums.txt.sig -p oarfish-release.pub   # 1. signature
sha256sum -c checksums.txt                                               # 2. hashes (macOS: shasum -a 256 -c checksums.txt)
```

`minisign` prints `Signature and comment signature verified` and the trusted comment `oarfish <tag> <commit>`. Only then are the hashes in `checksums.txt` worth anything. Download every file listed in `checksums.txt` (the loop does), otherwise `sha256sum -c` reports the missing ones.

What this does and does not prove: the signature proves the release files were signed with the private key behind `release/oarfish-release.pub`. The public key comes from the same repository, so it does not protect against someone who controls the repository itself. For that, compare the key id (`11F1C52777090BFC`, the number in the `untrusted comment` line of the key file) and the key line with a copy obtained through an independent channel. No such second channel exists yet; installer and updater will carry the key themselves (Gates G3 and G6).

Status: verified on 2026-10-26 for `v0.1.0-alpha.2`: anonymous `curl` without a token, the signature, the SHA-256 of the files and the version stamp in `install.sh`. Verified again for `v0.1.0-alpha.5`, the most recent release checked: token-free download, the signature against `release/oarfish-release.pub`, the SHA-256 of all listed files, `scripts/verify-release.sh` printed `VERIFIED`, and the version stamp and commit in `install.sh` and `manifest.json` matched. (`v0.1.0-alpha.1` has no `install.sh`; the loop above does not fit it.) The `releases/latest/download/` address is not covered by this; it is to be verified with the first stable release (see `INSTALL.md`).

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

## How `install.sh` is built and checked

`release/oarfish-release.pub` is the single source of the trusted key. Gate G3 embeds its key line and key id as constants in `install/install.sh` (ADR 0006); Gate G6 will embed the file in the Oarfish binary with `go:embed`. Both must verify `checksums.txt.sig` before trusting any hash. The updater can use `crypto/ed25519` from the standard library (legacy mode). See ADR 0005 for the open point (key rotation).

- `scripts/release-bundle.sh` calls `scripts/stamp-installer.sh`, which copies `install/install.sh` and replaces the single line `OARFISH_STAMPED_VERSION='@@OARFISH_VERSION@@'` with the release version. No timestamps, no host data: the result is a pure function of the file and the version, and `check-reproducible.sh` compares it across two builds. An installer taken from the release installs that release; `--version` overrides.
- `bundlegen check` (part of `verify-release.sh`, run in the dry run, in `sign` and again in `publish`) requires `install.sh` in `checksums.txt`, a `#!/bin/sh` first line, no remaining placeholder and exactly one stamp line equal to the manifest's version. A tampered installer fails the hash, a replaced one fails the signature, a wrongly stamped one fails the check; `scripts/test-signing.sh` tests all three.
- `scripts/check-installer-key.sh` (CI, and in the `sign` job against the file being released) requires the embedded key and key id to equal `release/oarfish-release.pub`. If the key ever changes, change both and release a new installer.
- `scripts/check-budgets.sh` keeps `install.sh` under 50,000 bytes.
- The installer tests use a copy with a throwaway key (`scripts/make-test-installer.sh`). Such copies are never part of a release.

## Before making the repository public

Decision D-C17 makes the repository public after Gate G2 and this review, before Gate G3. Gate G2.1 did the review; the visibility change is a manual step by the maintainer. Run through this list again right before flipping the switch, because history and workflows may have changed since.

- [ ] **Secret scan, full history and tree.** `gitleaks git --redact --log-opts="--all" .` and `gitleaks dir --redact .` report no leaks. `--redact` keeps values out of the output.
- [ ] **Authors.** `git log --all --format='%an <%ae>%n%cn <%ce>' | sort -u` lists only GitHub noreply addresses (plus `GitHub <noreply@github.com>` for web merges and the `Co-Authored-By` trailer address).
- [ ] **Machine-specific values.** No hits for home directories, local user or host names, private IPs or e-mail addresses in `git grep -I -n -E '<pattern>' $(git rev-list --all)` (zsh: use an array, the list is not word-split).
- [ ] **No private key, `.env` or build artifact ever committed.** `git log --all --diff-filter=A --name-only --format= | sort -u` shows none. Only `release/oarfish-release.pub` and the public test fixtures under `scripts/bundlegen/testdata/` carry minisign material.
- [ ] **Workflows.** No `pull_request_target`, no `workflow_run`, no untrusted `github.event` field in a shell, every third-party action pinned to a full commit SHA, minimal `permissions`, `OARFISH_RELEASE_SIGNING_KEY` referenced in one step of one job that only starts for a tag push.
- [ ] **License.** `LICENSE` is Apache-2.0, `README.md` names it, no manifest claims another one, no dependency with an incompatible license.
- [ ] **GitHub settings** (a) to (f) in `docs/PUBLICATION_CHECKLIST.md`.
- [ ] **Visibility change to public** (step (g) there), then the anonymous download test (step (h)).

Rewriting history is not part of this list. If a scan ever finds a real secret in history, rotate it first (for the release key: new key pair, new public key, and the consequences in ADR 0005), then decide about a rewrite.

## Known limitations

- The installer is described in `INSTALL.md`, including what has and has not been verified. It has run on one Raspberry Pi Zero 2 W (single data point, see the hardware test results there); nothing else has run on real hardware.
- `v0.1.0-alpha.1` was published before the installer existed. It has no `install.sh`, no `oarfish.service` and no `THIRD_PARTY_NOTICES.txt` in the bundle, so the current `verify-release.sh` and the installer reject it by design. Releases from `v0.1.0-alpha.2` on follow the layout described here.

- Unverified until the first real release: the publish job, `gh release create`, prerelease flag, and the secret-based signing path. The dry run exercises everything except those and the committed public key.
- The ARM64 smoke test runs under qemu-user on an x86-64 runner. It proves the binary starts and serves; it is not a test on real hardware (the hardware tests, G3 and G4 on one device, are recorded in `INSTALL.md`).
- No SLSA provenance or artifact attestation, no signature on the SBOMs other than through `checksums.txt`.
- Key rotation and revocation are not designed (ADR 0005).
