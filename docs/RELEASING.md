# Releasing

How a version tag becomes a signed GitHub Release (Gate G2) that includes the installer (Gate G3). The release path is pull-based: GitHub-hosted runners build and publish, nothing connects to a device (ADR 0003, ORB-CICD-005). What a user does with a release is in [`INSTALL.md`](INSTALL.md).

## What a release contains

| File | Purpose |
|---|---|
| `oarfish_<version>_linux_arm64.tar.gz` | Bundle: `oarfish_<version>_linux_arm64/oarfish` (static binary, frontend embedded), `oarfish.service`, since G6.3b also the update helper `oarfish-update-helper` and the units `oarfish-update-helper.socket`, `oarfish-update-helper.service` and `oarfish-update-recover.service` (see "Bundle shapes"), `LICENSE` and `THIRD_PARTY_NOTICES.txt` (licence texts of the Svelte runtime, the Vite helper snippet, the Go runtime and, since G5.1, `golang.org/x/crypto`, which are compiled into the binary and whose licences require the notice to travel with it) |
| `install.sh` | The installer ([`INSTALL.md`](INSTALL.md)): `install/install.sh` with the release version written into one line (below). Listed in `checksums.txt`, so the signature covers it |
| `manifest.json` | Machine-readable metadata (see below) |
| `checksums.txt` | SHA-256 of the bundle, `install.sh`, `manifest.json` and the SBOMs, in `sha256sum` format |
| `checksums.txt.sig` | minisign signature of `checksums.txt`, legacy mode: plain Ed25519 over the file bytes (ADR 0005) |
| `sbom-backend.cdx.json`, `sbom-frontend.cdx.json` | CycloneDX SBOMs (ORB-SEC-009, SOLL) |

`<version>` is the tag without the leading `v`. A tag with a hyphen (`v0.1.0-alpha.1`) is published as a prerelease.

The bundle contains `oarfish`, `oarfish.service` (the systemd unit the installer installs), `LICENSE` and `THIRD_PARTY_NOTICES.txt`. The manifest schema did not change for the installer: `install.sh` is a release asset next to the bundle, covered by `checksums.txt`, not part of the tarball, so the manifest's `contents` do not list it. `bundlegen check` requires it in `checksums.txt` and checks its stamp instead.

`manifest.json` fields: `schema_version`, `name`, `version`, `tag`, `prerelease`, `commit` (40 hex), `build_date` (RFC 3339, equal to the commit timestamp so the file is reproducible), `os`, `architecture`, `requirements` (`os`, `architecture`, `cpu`, `libc`, `runtime_dependencies`), `bundle` (`file`, `sha256`, `size`) and `contents` (path, mode, size, SHA-256 of every file in the tarball). Kernel and memory minimums are not included yet because they have not been measured on a device (ORB-REL-002 is met for architecture, OS and libc only). Three optional fields are added by ADR 0010 and described in the next section; a manifest without them is exactly the manifest above.

## Bundle shapes (ADR 0010, steps G6.2 and G6.3b)

The release tooling knows two bundle shapes. `bundlegen check` accepts exactly these two and nothing in between.

| Shape | Tarball entries (root directory `oarfish_<version>_linux_arm64/`) | Manifest |
|---|---|---|
| **Legacy** (explicit opt-out only) | the root directory and the four files above: `LICENSE` 0644, `THIRD_PARTY_NOTICES.txt` 0644, `oarfish` 0755, `oarfish.service` 0644 | no `components` object |
| **Components** (the default since G6.3b) | the five entries of the legacy shape plus exactly four more: `oarfish-update-helper` 0755 (an ARM64 ELF executable) and the units `oarfish-update-helper.service`, `oarfish-update-helper.socket` and `oarfish-update-recover.service`, each 0644 | the object `components` is present, and `contents` lists the four new files like every other file |

**Since G6.3b the default output of `scripts/release-bundle.sh` is the components shape, and so is what `ci.yml` and `release.yml` build** (neither workflow was changed: they call the script without arguments). `install/install.sh` installs both shapes and accepts exactly these two ([`INSTALL.md`](INSTALL.md#the-update-helper-layout-g63b)). The helper in the bundle is still the stub of G6.3a; it answers the specified subcommands and does nothing else. The legacy shape stays reachable only through the explicit opt-out `OARFISH_BUNDLE_SHAPE=legacy` (for tests and comparison); with it, `VERSION=0.0.0-g63a` and `COMMIT=ef7441cb5615a0ae35f846c4a38866a9f8dc6ec4` produce the same tarball and manifest as G6.3a (sha256 of the tarball `f7d76eeceb5afe600e958d71904691e1deb415c2207500c093e96dbe44b8dce4`, of the manifest `b83a74bf2db3932a50d2b7e81b64ed5c1280719faf360ec083300190773b48f9`; `install.sh` differs, as the installer changed). `scripts/bundlegen/testdata/legacy/` still holds the reference output of the tool before G6.2 and a test compares against it. The components bundle is exercised by the installer tests with fake files and by the CI jobs below; none of it is evidence that a helper works on a device, except that a test release in the components shape (throwaway key, test copy of the installer, never published) was installed on one device at the G6.3 owner stop (device observation, 2026-11-03; see [`INSTALL.md`](INSTALL.md#results-on-the-reference-device-g63)). The production key path with the real installer was not exercised there, and nothing is Verified.

Additive manifest fields (all optional, all absent from a legacy manifest, `schema_version` stays 1; the readers on the device (the update helper and the service, later steps) ignore fields they do not know, while `bundlegen check` rejects them):

| Field | Type and rule | Meaning |
|---|---|---|
| `requirements.min_installed_version` | SemVer (no leading `v`), not greater than `version` | The oldest installed version the update helper accepts an update from |
| `requirements.helper_protocol` | integer, at least 1; not greater than `components.protocol` when both are present | The lowest helper protocol the release's service needs |
| `components` | object, present if and only if the four helper files are in the bundle | Describes the helper the bundle delivers |
| `components.key_id` | 16 lowercase hexadecimal digits | The key id embedded in the delivered helper. (Key id files and documents often print it in upper case; the manifest and `-expect-key-id` use lower case.) |
| `components.protocol` | integer, at least 1 | The highest protocol the delivered helper implements |

Building the components shape. `bundlegen bundle` takes the four files, the protocol and the key id as one set: all six flags or none. A partial set is an error, so a missing helper never silently yields the legacy shape. Without the six flags the output is the legacy bundle.

| Flag | Meaning |
|---|---|
| `-helper`, `-helper-service`, `-helper-socket`, `-recover-service` | Paths of the four component files (the helper must be an ARM64 ELF) |
| `-components-protocol N` | `components.protocol` |
| `-components-key-id HEX16` | `components.key_id` |
| `-min-installed-version V`, `-requires-helper-protocol N` | Optional, independent of the set above: `requirements.min_installed_version` and `requirements.helper_protocol` (both are valid in either shape) |
| `check -expect-key-id HEX16` | `bundlegen check` requires `components.key_id` to equal this value; a release without `components` then fails. `scripts/release-bundle.sh` passes it in the default shape (below) |

`scripts/release-bundle.sh` selects the shape:

| Setting | Result |
|---|---|
| nothing set (the default) | **Components shape.** The script builds the helper with `scripts/build-helper.sh` (without the release `VERSION` and `COMMIT`, see "The helper does not carry the release version" below), takes the three helper units and the recover unit from `install/`, sets `components.protocol` to 1 and derives `components.key_id` from `release/oarfish-release.pub` with the method of `scripts/check-helper-key.sh` (the 16 digits of the first line, in lower case; `11f1c52777090bfc` for the release key). After the build it runs `bundlegen check -expect-key-id` with that value on a throwaway copy of the three files with a generated `checksums.txt`; a different key id fails the build. This is where the key id is checked: the installer does not compare it with its own (the installer tests use a throwaway installer key while the helper embeds the release key). |
| `OARFISH_BUNDLE_SHAPE=legacy` | The legacy bundle, byte for byte as before ADR 0010. Cannot be combined with `OARFISH_COMPONENTS_*`. |
| `OARFISH_COMPONENTS_DIR`, `OARFISH_COMPONENTS_PROTOCOL`, `OARFISH_COMPONENTS_KEY_ID` (any of them) | Their G6.2 meaning, all or nothing (a partial set is refused by `bundlegen`); nothing is built or derived by the script and no key id check runs. |
| `OARFISH_MIN_INSTALLED_VERSION`, `OARFISH_REQUIRES_HELPER_PROTOCOL` | Independent of the above; both are valid in either shape. |
| any other value of `OARFISH_BUNDLE_SHAPE` | An error. |

The release workflow's dry run (and `release.yml`) therefore builds, signs with the throwaway or production key as before, and verifies the components shape; the size budget (35,000,000 bytes) applies to the bundle with the helper in it. Reproducibility is checked on the components bundle by `scripts/check-reproducible.sh`.

**The helper does not carry the release version** (D-C80, ADR 0010 A13). The update helper on the device compares the helper, its three units and `oarfish.service` of a staged release with the installed files by SHA-256 and answers `installer_required` when one differs. So the helper's bytes depend only on its code, the packages it links and the Go toolchain: `scripts/build-helper.sh` passes no `-X` flag, the helper does not link `backend/internal/version`, and `helper_version` in its `status` answer is the constant `protocol-1`, not the release version (the main binary `oarfish` keeps its version). A release that does not change the helper code, a package it links, the Go version or a unit text therefore needs no installer run. A release that changes one of them needs one until G6.10, and its release notes say so. A device on `v0.1.0-alpha.7` needs one installer run for the next release, because that helper embeds its release version. Check it before tagging:

```bash
scripts/check-helper-independent.sh   # two helper builds with different VERSION and COMMIT are identical
make check-reproducible               # also: two bundles of different versions carry the same five compared files
```

CI runs both in the job "Reproducible bundle". To see whether a release changes the components compared with the one before, compare the `contents` entries of the five files in the two manifests. The first components bundle measured for this change was 4,361,895 bytes against 3,008,364 bytes for the legacy bundle of the same commit (the helper stub is 3,342,496 bytes uncompressed).

`bundlegen check` rejects, in addition to everything it rejected before: a missing or extra tarball entry in either shape, a wrong mode, a helper that is not an ARM64 ELF (the installer's header check: 64-bit, little-endian, machine `0xB7`), `components` without the files or the files without `components`, a malformed `key_id`, a protocol below 1, `requirements.helper_protocol` greater than `components.protocol`, an invalid or too new `min_installed_version`, a path listed or stored twice, and any difference between `contents` and the tarball. It also rejects unknown manifest fields, as before G6.2: it validates the manifest the release tooling wrote, so every field must be one this tool knows. This is not the rule for the readers on the device, which ignore unknown fields so that additive changes stay compatible (ADR 0010, U4). The main binary is still not checked for its ELF header by `bundlegen`; the installer does that.

## One-time setup: signing key (maintainer, local machine)

The private key must never enter the repository, a log or a chat. The implementation agent does not generate or read it. Run these on your own machine:

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
2. **Before tagging the first release that reports the process ID in `/healthz` and serves `/readyz`** (ADR 0010, A11.2, D-C68): confirm or adjust the constant `strictHealthFrom` in `backend/cmd/oarfish-update-helper/health.go` so that it equals the version of that release (it is set to `0.1.0-alpha.7` at the time of writing). A wrong value either skips the stricter health check for that release or makes the helper refuse a healthy one.
3. **Every release after the bootstrap (`v0.1.0-alpha.8`)**: compare the five files the update helper compares (`oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, `oarfish.service`) with the previous release before tagging. Build the bundle of the commit (`make release-bundle`) and compare the `sha256` of these five entries in `contents` of its `manifest.json` with the published `manifest.json` of the previous release. All equal: a device on the previous release updates from the GUI. One differs: until G6.10 the update page answers `installer_required`, and the release notes must say that this release needs one installer run on the device (`sudo sh install.sh` from the release page), and why. Devices on `v0.1.0-alpha.7` or earlier always need one installer run ([`UPDATE.md`](UPDATE.md#when-the-installer-is-required)); say that in the notes as well.
4. Tag a commit that is on `main` and push the tag:
   ```bash
   git tag v0.1.0-alpha.1 && git push origin v0.1.0-alpha.1
   ```
   The workflow rejects tags that are not `vMAJOR.MINOR.PATCH[-prerelease]` and tags whose commit is not on `main`.
5. Watch it: `gh run watch`. Jobs: `ci` (the full CI workflow) → `build` → `sign` → `publish`. Only `sign` references the secret and it only starts for a tag push; only `publish` has `contents: write`.
6. Verify the published assets (below).
7. **Release notes of the G5.1 release (and any release a device may reach from `v0.1.0-alpha.5` or earlier)** must say: "After this update the device has no administrator account and starts in setup mode. The GUI shows the setup page; read the setup token on the device with `sudo cat /var/lib/oarfish/setup-token`. The dashboard and the device page show data only after setup and login. Every `/api/v1` route except the session status, login and setup now needs a session; `/healthz` stays public." (ADR 0009, Consequences; D-C46)

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

Status: verified on 2026-10-26 for `v0.1.0-alpha.2`: anonymous `curl` without a token, the signature, the SHA-256 of the files and the version stamp in `install.sh`. Verified again for `v0.1.0-alpha.6`, the most recent release checked (2026-10-31): token-free download, the signature against `release/oarfish-release.pub`, the SHA-256 of all listed files, `scripts/verify-release.sh` printed `VERIFIED`, and the version stamp and commit in `install.sh` and `manifest.json` matched. (`v0.1.0-alpha.1` has no `install.sh`; the loop above does not fit it.) The `releases/latest/download/` address is not covered by this; it is to be verified with the first stable release (see `INSTALL.md`).

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

`scripts/check-reproducible.sh` compares the SHA-256 of both bundles and the manifests, runs `scripts/check-helper-independent.sh` and builds a third bundle with another version, whose helper, helper units and `oarfish.service` must equal those of the first (D-C80). CI runs it on every change (job "Reproducible bundle"), and the release workflow additionally compares two rebuilds with the bundle it is about to publish. Not covered: a different Go version or Node version can change the output (the Go version is pinned through `go.mod`, Node through `frontend/.node-version` and the lockfile); SBOM files contain timestamps and UUIDs and are not reproducible.

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

- The installer is described in `INSTALL.md`, including what has and has not been verified. It has run on one Raspberry Pi Zero 2 W (single data point, see the hardware test results there); nothing else has run on real hardware. The components shape was installed once on one device with a test release and a test copy of the installer (device observation, G6.3 owner stop, 2026-11-03), so the release-key path of the installer for that shape, the installer's rollback after a failed health check, `--purge` and a first installation without a previous release have not run on a device.
- `v0.1.0-alpha.1` was published before the installer existed. It has no `install.sh`, no `oarfish.service` and no `THIRD_PARTY_NOTICES.txt` in the bundle, so the current `verify-release.sh` and the installer reject it by design. Releases from `v0.1.0-alpha.2` on follow the layout described here.

- Unverified until the first real release: the publish job, `gh release create`, prerelease flag, and the secret-based signing path. The dry run exercises everything except those and the committed public key.
- The ARM64 smoke test runs under qemu-user on an x86-64 runner. It proves the binary starts and serves; it is not a test on real hardware (the hardware tests, G3, G4, G5, G5.1 and the G6.3 owner stop, are recorded in `INSTALL.md`).
- No SLSA provenance or artifact attestation, no signature on the SBOMs other than through `checksums.txt`.
- Key rotation and revocation are not designed (ADR 0005).
