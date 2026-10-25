# ADR 0006: How the installer verifies the release signature

- Status: Proposed
- Date: 2026-10-25

## Context

`install.sh` (Gate G3) must verify the Ed25519 signature of `checksums.txt` before it installs anything (ORB-INS-004, ORB-REL-003, spec 9.8). Releases are signed with minisign in legacy mode (`Ed`, ADR 0005): the signature is plain Ed25519 over the bytes of `checksums.txt`, and the public key is the 32 bytes in `release/oarfish-release.pub`. ADR 0005 chose legacy mode so that no BLAKE2b implementation is needed on the verifying side.

The problem: `minisign` is not installed on a fresh Raspberry Pi OS Lite or Ubuntu Server image.

Constraints, from the specification and the gate prompt:

- No Python, Node.js or compiler on the device (ORB-NFR-008). No step may execute unverified code.
- The device trusts neither HTTPS nor a hash from the download source alone (spec 9.8). The public key must not be fetched together with the files it is supposed to authenticate.
- No GitHub token on the device (D-C13), no `curl | sh` surprises. The recommended invocation is: download, inspect, run.

### Where trust starts

Whatever option is chosen, the root of trust on the device is `install.sh` itself, which the user downloads over HTTPS from `github.com`, can read, and then runs. The public key is a constant inside that file (spec 9.8: "in Installer und Updater eingebettet"). The bundle, `manifest.json` and `checksums.txt` are then authenticated against that constant, not against anything that arrives in the same download. This does not protect against an attacker who controls the repository or the account and replaces `install.sh` together with the key. What helps there is an independent copy of the key id and key line, which the documentation prints (key id `11F1C52777090BFC`) so a user can compare them; no separate channel exists yet. `install.sh` is also listed in the signed `checksums.txt`, so anyone who already has minisign can verify the script before running it (optional, documented). This ADR does not claim more than that.

## Options

### A. The system's `openssl` (recommended)

Raspberry Pi OS (Bookworm and later) and Ubuntu Server 22.04/24.04 ship OpenSSL 3.0 or newer. `openssl pkeyutl -verify -rawin` verifies a raw Ed25519 signature. The installer derives what it needs from the two minisign text files with POSIX tools only:

1. public key: base64-decode the key line (42 bytes: `Ed`, 8-byte key id, 32-byte key), prepend the fixed 12-byte Ed25519 SubjectPublicKeyInfo header, giving a DER public key;
2. signature: base64-decode line 2 of `checksums.txt.sig` (74 bytes: `Ed`, 8-byte key id, 64-byte signature); require algorithm `Ed` and a key id equal to the embedded key's; the last 64 bytes are the raw signature;
3. `openssl pkeyutl -verify -pubin -inkey key.der -keyform DER -rawin -in checksums.txt -sigfile sig.raw`.

The trusted comment (line 3 and 4) is ignored, as ADR 0005 requires of any verifier that does not check the comment signature.

Proof of concept, run on the maintainer's machine (OpenSSL 3.6.2) against the throwaway fixture in `scripts/bundlegen/testdata/` (no release key involved): the committed legacy signature verifies (`Signature Verified Successfully`, exit 0) and the same file with one appended byte is rejected (exit 1).

- Pro: nothing is downloaded or executed that is not already on the system and signed by the distribution; about 20 lines of shell; no extra network dependency; consistent with ADR 0005's reason for legacy mode.
- Pro: the same Ed25519 check works for the updater later with Go's `crypto/ed25519`; the format does not change.
- Con: needs `openssl` 3.0 or newer on the device. Pi OS Bullseye (OpenSSL 1.1.1) cannot verify raw Ed25519 with the command line tool and is refused with a clear message. Presence of the `openssl` binary on a fresh Pi OS Lite image is indicated by package metadata and user reports, not confirmed by me on an image; the installer checks for it and refuses clearly if it is missing (it does not install packages itself).
- Con: hand-built DER header and offsets are security-relevant code. Mitigations: tests with real minisign output, a key id check, a length check on both decoded values, and a negative control (below).
- Negative control: before trusting the verifier, the installer runs it once on a copy of `checksums.txt` with one byte changed and requires it to reject. This guards against a broken or unexpected `openssl` that returns success without checking.

### B. Download a pinned, checksum-verified `minisign`

The installer downloads the upstream static `minisign-0.12-linux.tar.gz` (the version and SHA-256 already pinned for CI in `scripts/install-minisign.sh`), checks its SHA-256 against a value embedded in `install.sh`, then uses `minisign -V`.

- Pro: the reference verifier, handles every minisign feature including the trusted comment.
- Con: executes a third-party binary as root, authenticated only by a hash that is itself a constant in `install.sh` (so the trust is the same as option A's, with a larger attack surface). A second download from a second project's release page, which must stay available for as long as old installers are used; the upstream must be reachable for every install; the pin must be bumped by releasing a new installer.
- Con: more moving parts for the same guarantee.

### C. Small verifier binary built by this project

A `oarfish-verify` static binary (Go `crypto/ed25519`) is published as a release asset; `install.sh` carries its SHA-256.

- Pro: no dependency on the device's OpenSSL.
- Con: the hash must be embedded at release time, so `install.sh` becomes a build product of the release and the pipeline gets a circular dependency (verifier hash, then installer, then checksums, then signature). A second binary to build, reproduce, size-budget and explain, to be executed as root before anything else is verified. More code on the critical path than option A for the same trust root.

### Rejected

- `gpg`/`gpgv`, `ssh-keygen -Y verify`, `signify`: none verifies a minisign legacy signature over a plain file. `ssh-keygen` signs a structured message with a namespace, not the raw file.
- Piping the bundle through `oarfish verify` (the binary verifies itself): executes unverified code.
- Trusting HTTPS plus the `checksums.txt` hash only: forbidden by spec 9.8.
- Fetching the public key at install time from the repository: the key would arrive over the same channel as the files it authenticates (explicitly excluded by the gate prompt).

## Decision (proposed)

Option A.

1. `install.sh` embeds the minisign public key line as a constant. CI checks that it equals the key line of `release/oarfish-release.pub`, so the repository file stays the single source (ADR 0005).
2. Verification order, all in a private staging directory: download `checksums.txt` and `checksums.txt.sig`, verify the signature, download the bundle and `manifest.json`, verify their SHA-256 values against the signed `checksums.txt`, and only then unpack the bundle. The tarball is extracted with `tar` into the staging directory, never executed. The binary is run for the first time after installation, for the health check.
3. Minimum: OpenSSL 3.0. The installer detects the version, runs the negative control, and refuses (exit code 1, nothing changed) when the check is not possible. The refusal message says to install the `openssl` package from the distribution.
4. `install.sh` becomes a release asset listed in `checksums.txt`; the signature therefore covers it. A tampered `install.sh` fails the release verification (`scripts/verify-release.sh`) and the tamper tests.
5. The embedded key does not rotate. Rotation remains the open point of ADR 0005.

## Consequences

- The installer needs only tools that Raspberry Pi OS Lite and Ubuntu Server are expected to carry: `sh`, `openssl` (3.x), `curl` or `wget`, `tar`, `gzip`, `sha256sum`, `od`, `base64`, `mktemp`, `systemctl`, and `useradd` or `adduser`. Each is checked up front.
- Tests run in CI on ubuntu-24.04 (OpenSSL 3.0.x, the same major version as the targets). On macOS the system `openssl` is LibreSSL and cannot do this; the local test target requires OpenSSL 3 (for example Homebrew's) and says so.
- Not verified on real hardware: `openssl` presence and version on a fresh Pi OS Lite or Ubuntu Server image. This stays on the G3 acceptance list until someone runs the installer on a device.
- If a later gate needs the updater to verify in the Go binary, nothing here constrains it: legacy-mode signatures are checked with `crypto/ed25519`.

## Questions for the maintainer

1. Accept option A?
2. Default version of `install.sh`: stamped with the tag of the release it is attached to (my recommendation: the script and the bundle it installs are then signed together; `--version` overrides), or always the newest release? "Latest" cannot be resolved anonymously for prereleases without the GitHub API, which rate-limits unauthenticated calls.
3. Refuse OpenSSL older than 3.0 (my recommendation), or additionally accept a `minisign` already installed on the device (adds a second code path to test)?
