# Development

Development happens on macOS (or any machine with the toolchain). Nothing is built on the target device.

## Prerequisites

| Tool | Version |
|---|---|
| Go | the version in `backend/go.mod` (CI reads it from there) |
| Node.js | 22 LTS or newer (`frontend/.node-version`, `engines` in `package.json`) |
| npm | bundled with Node.js |
| make | any recent version |
| minisign | only for `make test-signing`, `make test-installer` and for signing keys (`brew install minisign`); CI installs a pinned release |
| OpenSSL 3.x | only for `make test-installer`. macOS ships LibreSSL as `/usr/bin/openssl`, which cannot verify the signatures; use Homebrew's (`brew install openssl@3`, `/opt/homebrew/bin/openssl`). The test refuses any other implementation |
| shellcheck | `make lint-shell` (`brew install shellcheck`); CI runs it |

Node.js is a build-time tool only. The Oarfish binary and the target device do not need it.

## Make targets

| Target | What it does |
|---|---|
| `make deps` | `npm ci` in `frontend/` (installs exactly the lockfile) |
| `make dev` | Go backend on `127.0.0.1:8484` plus the Vite dev server; open <http://localhost:5173>. Vite proxies `/healthz` to the backend. `LISTEN=<addr>` changes the backend address (also update the proxy target in `frontend/vite.config.ts`). |
| `make test` | `go test ./...` (backend and `scripts/bundlegen`) and `vitest run` |
| `make lint` | gofmt check, `go vet` (backend and `scripts/bundlegen`), `svelte-check` (TypeScript and Svelte type check) |
| `make build` | frontend production build, then host binary `bin/oarfish` with the frontend embedded |
| `make build-arm64` | same, cross-compiled with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` to `bin/oarfish-linux-arm64` |
| `make release-bundle` | frontend build, then `scripts/release-bundle.sh`: unsigned `dist/release/oarfish_<version>_linux_arm64.tar.gz` and `manifest.json`, the same script CI and the release workflow use |
| `make check-reproducible` | builds the bundle twice from scratch (frontend rebuilt, empty Go cache) and compares SHA-256 |
| `make test-signing` | sign, verify and tamper-rejection tests with throwaway keys (needs `minisign`) |
| `make test-installer` | `scripts/test-installer.sh`: the installer against a fake root with a throwaway key, 99 checks including every refusal and tamper case (needs `minisign` and OpenSSL 3). `OARFISH_TEST_OPENSSL=<path>` selects the OpenSSL, `OARFISH_TEST_SH=<shell>` the shell that runs the installer (default `dash`) |
| `make lint-shell` | shellcheck: `install/install.sh` and the container scripts as POSIX sh at every severity, `scripts/*.sh` at warning level |
| `make clean` | removes `bin/`, `dist/` and the generated frontend build (keeps `dist/.gitkeep`) |

`make release-bundle` also writes the stamped `install.sh` next to the bundle. `make build` injects `Version` (from `git describe`) and `Commit` via `-ldflags`; `bin/oarfish -version` prints them.

## Running without make

```bash
cd frontend && npm ci && npm run build   # writes backend/internal/web/dist
cd ../backend && go run ./cmd/oarfish      # http://127.0.0.1:8484
```

## Working on the backend only

`go vet`, `go test` and `go build` work without a frontend build, because `backend/internal/web/dist/.gitkeep` is tracked. UI requests then return `503`.

## Dependencies

- Backend: standard library only. There are no third-party Go modules.
- Frontend (all dev dependencies, pinned exactly in `package.json`, locked in `package-lock.json`): `svelte`, `vite`, `@sveltejs/vite-plugin-svelte`, `typescript`, `svelte-check`, `@tsconfig/svelte`, `vitest`, `jsdom`, `@testing-library/svelte`, `@types/node`. They are build and test tooling and do not ship to the device.

Adding a dependency should be justified in the change that adds it.

`THIRD_PARTY_NOTICES.txt` (repository root, shipped in the release bundle) holds the verbatim licence texts of the third-party code that ends up inside the binary: the Svelte runtime and a small Vite helper in the frontend bundle, and the Go standard library. The production build strips source comments, so the licence notices do not survive in the minified JavaScript. When `svelte` or `vite` is upgraded or a new dependency ships code into the bundle, update the file from the packages' licence files; do not type holder names by hand.

## CI

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`, and is also called by the release workflow (`workflow_call`):

1. **backend**: gofmt, `go vet`, `go test -race`, plus the same three for `scripts/bundlegen` and a check that both modules declare the same Go version
2. **frontend**: `npm ci`, `svelte-check`, `vitest`, `vite build`, upload of the built assets
3. **build** (needs both): host build; the ARM64 release bundle via `scripts/release-bundle.sh`; aarch64 ELF check; size budgets (below); ARM64 smoke test under `qemu-user`
4. **reproducibility**: `scripts/check-reproducible.sh --rebuild-frontend`
5. **signing**: installs the pinned minisign and runs `scripts/test-signing.sh` (positive cases plus rejection cases for tampering, wrong key, replay and wrong signature format, including a tampered, replaced or wrongly stamped `install.sh`; throwaway keys only)
6. **installer**: shellcheck, the check that `install.sh` embeds the committed release key, `systemd-analyze verify` of the unit, and `scripts/test-installer.sh` twice (installer run by `dash`, then by `bash`) with the runner's OpenSSL 3.0
7. **installer-containers** (matrix `debian:12`, `ubuntu:24.04`): builds the real release bundle, signs it with a throwaway key, and runs the test copy of the installer end to end in a plain **arm64** container under qemu-user (`scripts/test-installer-container.sh`). The job summary lists which tools the minimal image already had. There is no systemd in the container: a stand-in starts the binary as the `oarfish` user. This is not a Raspberry Pi OS or Ubuntu Server test
8. **security** (ORB-SEC-009, SOLL): `govulncheck` (pinned), `npm audit --audit-level=high`, and a trial SBOM generation

`.github/workflows/release.yml` (tag `v*`, or manual dry run) is described in [`RELEASING.md`](RELEASING.md).

### Release scripts (`scripts/`)

| Script | Role |
|---|---|
| `release-bundle.sh` | the one place that builds and packages the bundle |
| `bundlegen/` | stdlib-only Go: writes tar.gz, `manifest.json`, `checksums.txt`; `check` validates a release directory. Own module so it can be tested and cannot leak into the product binary |
| `make-checksums.sh`, `make-sbom.sh` | `checksums.txt` for all release files; CycloneDX SBOMs |
| `sign-release.sh`, `verify-release.sh`, `test-signing.sh`, `install-minisign.sh` | signing (ADR 0005) |
| `stamp-installer.sh` | writes `install.sh` for a release: `install/install.sh` with the one line `OARFISH_STAMPED_VERSION='@@OARFISH_VERSION@@'` replaced by the version. Deterministic, replaces exactly one line |
| `check-installer-key.sh` | `install.sh` must embed the key line and key id of `release/oarfish-release.pub` (run in CI and in the release `sign` job) |
| `make-test-installer.sh` | test copy of `install.sh` with a throwaway key; the production script has no way to swap its key, so tests need a copy. Never published |
| `test-installer.sh` | installer tests against a fake root |
| `test-installer-container.sh`, `installer-container/` | installer end to end in an arm64 container under qemu |
| `check-budgets.sh` | enforces and reports the size budgets |
| `smoke-test-arm64.sh` | extracts the bundle, starts the binary, checks `/healthz` and `/` |
| `check-reproducible.sh` | double build and SHA-256 comparison |

### Size budgets

`scripts/check-budgets.sh` fails the job and writes a table to the job summary when:

- the release bundle exceeds 35,000,000 bytes (ORB-NFR-005, SOLL, enforced by decision of Gate G2), or
- the initial frontend (`index.html` plus the scripts and stylesheets it references, each `gzip -9`) exceeds 750,000 bytes (ORB-NFR-006, SOLL, enforced as above), or
- `install.sh` next to the bundle exceeds 50,000 bytes (Gate G3: the installer must stay small enough to read before running it as root).

Limits use decimal units, the stricter reading of "MB" and "KB".

### Supply chain status (SOLL items marked)

| Item | Status |
|---|---|
| Actions pinned to commit SHAs (ORB-SEC-008, MUST) | done; no third-party action is added beyond checkout, setup-go, setup-node, upload/download-artifact |
| Minimal `permissions` (MUST) | `contents: read` everywhere; `contents: write` only in the `publish` job |
| `govulncheck`, `npm audit` (ORB-SEC-009, SOLL) | in CI, pinned `govulncheck@v1.9.0`; results depend on the live vulnerability databases, so a green run can turn red without a code change |
| SBOM per release (ORB-SEC-009, SOLL) | `cyclonedx-gomod` v1.12.0 for the Go side, `npm sbom` for the frontend. Gaps: two files instead of one merged SBOM; the frontend SBOM lists all packages from the lockfile (all are devDependencies, only the Svelte runtime ships inside the JS) without separating build tooling from shipped code; no SBOM signature beyond `checksums.txt`; no provenance attestation |
| Third-party tools fetched in CI | minisign (version and SHA-256 pinned), `govulncheck` and `cyclonedx-gomod` (version pinned, verified by the Go checksum database, not hash-pinned in this repository), `qemu-user` from the Ubuntu archive (version follows the runner image) |

Rules for editing workflows:

- Pin every third-party action to a full commit SHA with the version as a comment (ORB-SEC-008).
- Keep `permissions` minimal.
- Never add SSH keys, device hostnames, Tailscale access, or a self-hosted runner (ADR 0003).

## Local-only information

Device hostnames, usernames, endpoints and credentials belong in `CLAUDE.local.md` (git-ignored) or an untracked `.env`. `.env.example` documents variables the code reads; Gate G1 reads none.

## The installer and its tests

`install/install.sh` is POSIX `sh` (checked with `shellcheck -s sh`, run by `dash` in the tests), `set -eu`, no bashisms. It is a release artifact: the release build copies it and writes the version into the stamped line. A copy without a stamped version refuses to run without `--version`.

The tests need to run the installer without root, without systemd and without the network. That works through one switch: when the environment variable `OARFISH_TEST_ROOT` is set, every path lives below that directory, root and a running systemd are not required, and these variables are honoured: `OARFISH_RELEASE_BASE_URL` (a `file://` URL), `OARFISH_OPENSSL`, `OARFISH_SYSTEMCTL`, `OARFISH_HEALTH_URL`, `OARFISH_HEALTH_TIMEOUT`, `OARFISH_HEALTHCHECK=skip`. Without `OARFISH_TEST_ROOT` all of them are ignored, so a production run cannot be redirected by the environment, and it cannot swap its signing key at all (tests use a copy with another key). Only run the test hooks against a scratch directory.

Rules when changing the installer: keep it dependency-light (only tools Raspberry Pi OS Lite and Ubuntu Server carry), keep every check before the first change to the system, keep the negative control, add a test for every new refusal, and keep the file under the size budget.

## Known limitations at Gate G3

- Nothing has run on a Raspberry Pi, a Raspberry Pi OS image or an Ubuntu Server image. The release bundle's ARM64 binary is exercised under `qemu-user` (smoke test, installer container jobs), which is not a hardware test.
- The unit file has been checked with `systemd-analyze verify` only. No real systemd has started it (the containers have none), so reboot persistence (AT-007), sandboxing options on a real kernel, and the 512 MB memory budget are untested.
- The installer container jobs use plain `debian:12` and `ubuntu:24.04` arm64 images. They are not Raspberry Pi OS Lite or Ubuntu Server images; the results (including which tools the minimal images lacked) say nothing certain about those.
- The embedded UI was verified by fetching the served HTML and assets, and the frontend by unit tests in jsdom. It has not been opened in Chromium, Firefox or Safari.
- Reproducibility is verified for the same commit, version string, Go and Node versions. Cross-toolchain-version reproducibility is not claimed.
- Manifest `requirements` list OS, CPU architecture and libc. Kernel and memory minimums are not stated because they have not been measured.
- Signature verification exists in the installer (system `openssl`) but not inside the Oarfish binary; the update path is Gate G6.
- Two installers started at the same time are not guarded against.
