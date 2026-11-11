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
| `make dev` | Go backend on `127.0.0.1:8484` plus the Vite dev server; open <http://localhost:5173>. Vite proxies `/healthz` and `/api` to the backend. The backend keeps its account, setup token and audit in `.dev-state/` (`DEV_STATE=<dir>` changes it; git-ignored); the setup page asks for the token in `.dev-state/setup-token`, and deleting `.dev-state/auth/account.json` returns to setup mode. `LISTEN=<addr>` changes the backend address (also update the proxy target in `frontend/vite.config.ts`); it must be a loopback IP address with a port, the binary refuses anything else (D-C29, D-C48). |
| `make test` | `go test ./...` (backend and `scripts/bundlegen`) and `vitest run` |
| `make lint` | gofmt check, `go vet` (backend and `scripts/bundlegen`), `svelte-check` (TypeScript and Svelte type check) |
| `make fuzz` | native Go fuzzing of the os-release parser, the device-tree compatible parser, the six metrics parsers (`/proc/stat`, `/proc/meminfo`, `fib_trie`, `if_inet6`, `mountinfo`, `route`), the password-hash (PHC) parser, the update helper's decoders (`FuzzDecodeRequest`, `FuzzDecodeActivate`, `FuzzDecodeAck`, `FuzzDecodeState`), its health-body reader (`FuzzHealthzBody`) and unit lint (`FuzzUnitLint`, G6.10 step a), the update service's readers of the helper answer, the GitHub release list and the release notes (`FuzzHelperResponse`, `FuzzReleaseList`, `FuzzPlainNotes`) and the five release-verification targets (`FuzzParseSignature`, `FuzzParseChecksums`, `FuzzParseManifest`, `FuzzVersion`, `FuzzInspectTar`), 23 targets, `FUZZTIME=10s` each (CI runs 5 s each; the seed corpora, committed in `testdata/fuzz/`, also run in `make test`; `FUZZTIME=60s make fuzz` is the length asked for at the review of G6.5) |
| `make bench-arm64` | cross-builds the Argon2id benchmark test binary for linux/arm64 to `build/bench/oarfish-auth-bench.test` (git-ignored, never part of a release); the commands for the device are in [`AUTH.md`](AUTH.md#measurements-and-checks-on-the-device) |
| `make build` | frontend production build, then host binary `bin/oarfish` with the frontend embedded |
| `make build-arm64` | same, cross-compiled with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` to `bin/oarfish-linux-arm64` |
| `make build-helper` | `scripts/build-helper.sh --twice`: cross-builds the update helper stub to `bin/oarfish-update-helper-linux-arm64` with the flags of the main binary and fails unless two builds from an empty Go cache are identical. The helper is part of the default (components) bundle since G6.3b; this target builds the standalone file ([`ARCHITECTURE.md`](ARCHITECTURE.md#the-update-helper-stub-gate-g6-step-g63a)) |
| `make test-helper` | `go vet` (host, linux/arm64, linux/amd64 and the `faulthooks` test build) and `go test -race -count=1` for `cmd/oarfish-update-helper` and `internal/trustkey`, then the fault-hook tests with `-tags faulthooks` (the helper's tests include the crash sweep and take about a minute under `-race`) |
| `make check-helper` | `test-helper`, `build-helper`, the check that the helper's bytes do not depend on the release version (`scripts/check-helper-independent.sh`), the key copy check, the check that the release helper has no fault hooks, the unit check and its mutation self-test: what the CI jobs for the helper run, except `systemd-analyze` and the qemu smoke test, which need Linux |
| `make release-bundle` | frontend build, then `scripts/release-bundle.sh`: unsigned `dist/release/oarfish_<version>_linux_arm64.tar.gz` and `manifest.json`, the same script CI and the release workflow use |
| `make check-reproducible` | builds the bundle twice from scratch (frontend rebuilt, empty Go cache) and compares SHA-256; then checks that the helper, its units and `oarfish.service` do not depend on the release version (D-C80) |
| `make test-relverify-producer` | `TestProducerCompatibility` with `OARFISH_RELVERIFY_PRODUCER=1`: builds `scripts/bundlegen`, makes a legacy and a components release with the real scripts, signs each with a throwaway minisign key and verifies them with `internal/relverify` (needs `bash` and `minisign`; without the variable the test is skipped; the signing job of CI runs it) |
| `make test-signing` | sign, verify and tamper-rejection tests with throwaway keys (needs `minisign`) |
| `make test-installer` | `scripts/test-installer.sh`: the installer against a fake root with a throwaway key, 126 checks on Linux (127 on macOS) including every refusal and tamper case (needs `minisign` and OpenSSL 3). `OARFISH_TEST_OPENSSL=<path>` selects the OpenSSL, `OARFISH_TEST_SH=<shell>` the shell that runs the installer (default `dash`) |
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

## Detection code and its tests (Gate G4)

`backend/internal/platform/` is read-only detection ([`DETECTION.md`](DETECTION.md)). Working on it:

- Detection takes an `fs.FS`. Tests give it a fixture directory from `backend/internal/platform/testdata/` (synthetic, see its README) or an in-memory `fstest.MapFS` built with `platformtest.Clone`. Never read an absolute path in detection code; only `platform/source` touches the host.
- `platformtest.Recording` records which files a detector opens. `TestDetectionOpensOnlyAllowListedFiles` is the list of files detection may read; adding a new file is a privacy decision and needs a line in that list and in `DETECTION.md`.
- `architecture_test.go` checks the import graph of the detection packages. It parses imports with `go/parser`; it needs no network and no `go list`.
- Fixtures must stay synthetic: no serial numbers, MAC addresses, machine ids, host names, user names or IP addresses (`TestFixturesContainNoPrivateData`). Tests that need such values create them in memory.
- UI texts for values coming from the API must be added to `frontend/src/lib/locales/de.ts` and `en.ts`; `TestEveryAPIValueHasATranslation` (backend) and `i18n.test.ts` (frontend) check that.
- `TestTraceabilityTableIsComplete` checks the requirement-to-test table in `DETECTION.md`. When you rename a test, update the table.

## Metrics code and its tests (Gate G5)

`backend/internal/metrics/` is the live dashboard's backend ([`DASHBOARD.md`](DASHBOARD.md)). Working on it:

- Like detection, it takes an `fs.FS`, never an absolute path. Tests use the synthetic trees in `backend/internal/metrics/testdata/` (see its README) through `metricstest.Fixture`, or `metricstest.NewLive` for a copy that a test can change while a sampler reads it. Hostile content that would be large (huge files, thousands of mounts, sensors and interfaces) is generated in memory, not committed.
- The sampler takes a `Clock`. `metricstest.Clock` (and the equivalent in the package's own tests) fires timers only when the test advances it, so tests never wait. A test that expects *no* timer or goroutine checks `Pending()` and the goroutine count.
- `TestMetricsOpenOnlyAllowListedFiles` (`internal/platform/privacy_test.go`) is the list of files the collector may open. Adding a file is a privacy decision and needs a line in that list and in `DASHBOARD.md`. The hardware address of an interface stays off it.
- `TestFixturesContainNoPrivateData` allows exactly two things in the metrics fixtures: the host name `oarfish-test` and IP addresses from the documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`). Link-local and loopback IPv6 lines are added in memory by the tests that need them.
- New parsers go in `metrics/parse` (pure, bounded, fuzzed). Add a `Fuzz` target, add it to `make fuzz` and to the CI step. `architecture_test.go` checks that `parse` touches nothing, that only `statfs.go` makes a system call, and that no product code imports `os/exec`.
- Alert thresholds are constants in `internal/metrics/alerts.go`; a new alert needs translation keys in `AlertKeys()`, both locale files, a row in `DASHBOARD.md`, and table-driven tests at every threshold edge.
- `TestDashboardTraceabilityTableIsComplete` checks the requirement-to-test table in `DASHBOARD.md`. When you rename a test, update the table.
- Frontend: `pages/Dashboard.svelte` is driven by `lib/stream.ts` (the `EventSource` lifecycle) and `lib/metrics.ts` (validation and derived values). Tests use `lib/eventsource.fake.ts`, `lib/harness.fake.ts` and `lib/metrics.fixtures.ts`. Appearance goes through the tokens in `styles/tokens.css`: no literal colour in a component (a test checks), a new colour pair goes into `styles/contrast-pairs.ts`, and `docs/UI.md` must list every token, component and state text (`docs.test.ts` checks). How to add a page or a card: [`UI.md`](UI.md#add-a-page). `BenchmarkCollect` gives the work of one sample (`go test -run '^$' -bench Collect ./internal/metrics`).

## Dependencies

- Backend: the standard library and one module, `golang.org/x/crypto` (`v0.58.0`, a released tag), of which only the package `argon2` is imported, for the Argon2id password hash (ADR 0009, E1). It brings `golang.org/x/sys` (`v0.49.0`) as a module; the linux/arm64 binary links `argon2` and its `blake2b` and not `x/sys/cpu` (amd64 only). Justification: PBKDF2 from the standard library needs almost no memory and is therefore weaker against GPU attacks, and would need an estimated 1 to 2 s per login on the Pi for comparable cost (ADR 0009, alternatives). `TestArchitectureOnlyArgon2FromXCrypto` fails on any other third-party import, and a CI step lists the non-standard packages of the linux/arm64 build and runs `go mod verify`. No vendoring: `go.sum` pins the hashes, release builds use `GOFLAGS=-mod=readonly`, and `govulncheck` and the SBOM job see the module.
- Frontend (all dev dependencies, pinned exactly in `package.json`, locked in `package-lock.json`): `svelte`, `vite`, `@sveltejs/vite-plugin-svelte`, `typescript`, `svelte-check`, `@tsconfig/svelte`, `vitest`, `jsdom`, `@testing-library/svelte`, `@types/node`. They are build and test tooling and do not ship to the device.

Adding a dependency should be justified in the change that adds it.

`THIRD_PARTY_NOTICES.txt` (repository root, shipped in the release bundle) holds the verbatim licence texts of the third-party code that ends up inside the binary: the Svelte runtime and a small Vite helper in the frontend bundle, the Go standard library, and `golang.org/x/crypto` (argon2, blake2b; copied from the module's `LICENSE`, which is identical to that of `golang.org/x/sys`). The production build strips source comments, so the licence notices do not survive in the minified JavaScript. When `svelte` or `vite` is upgraded or a new dependency ships code into the bundle, update the file from the packages' licence files; do not type holder names by hand.

## CI

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`, and is also called by the release workflow (`workflow_call`):

1. **backend**: gofmt, `go vet`, `go test -race`, an explicit vet, build (linux/arm64 and linux/amd64) and `-race -count=1` test of the update helper stub, a 5-second fuzz run of each detection, metrics, password-hash and update-helper request parser and of the update helper's unit lint, plus gofmt, vet and test for `scripts/bundlegen` and a check that both modules declare the same Go version (the 5-second fuzz runs include the five targets of `internal/relverify`)
2. **frontend**: `npm ci`, `svelte-check`, `vitest`, `vite build`, upload of the built assets
3. **build** (needs both): host build; the ARM64 release bundle via `scripts/release-bundle.sh`; aarch64 ELF check; the update helper stub built reproducibly (`scripts/build-helper.sh --twice`, not part of the bundle); size budgets (below, the helper's size is only reported); ARM64 smoke test under `qemu-user`, which also checks that the helper's `recover` exits 0 and its `serve` without socket activation exits non-zero
4. **reproducibility**: `scripts/check-reproducible.sh --rebuild-frontend`, which also runs `scripts/check-helper-independent.sh` and compares the five component files of a bundle of another version (D-C80)
5. **signing**: installs the pinned minisign and runs `scripts/test-signing.sh` (positive cases plus rejection cases for tampering, wrong key, replay and wrong signature format, including a tampered, replaced or wrongly stamped `install.sh`; throwaway keys only), then `make test-relverify-producer` (the release verification package accepts what `bundlegen` produces, in both bundle shapes, signed with a throwaway key)
6. **installer**: shellcheck, the check that `install.sh` embeds the committed release key, `systemd-analyze verify` of the unit, and `scripts/test-installer.sh` twice (installer run by `dash`, then by `bash`) with the runner's OpenSSL 3.0
7. **installer-containers** (matrix `debian:12`, `ubuntu:24.04`): builds the real release bundle, signs it with a throwaway key, and runs the test copy of the installer end to end in a plain **arm64** container under qemu-user (`scripts/test-installer-container.sh`). The job summary lists which tools the minimal image already had. There is no systemd in the container: a stand-in starts the binary as the `oarfish` user. This is not a Raspberry Pi OS or Ubuntu Server test
8. **helper-units**: the update helper's embedded key equals `release/oarfish-release.pub`, `scripts/check-units.sh` (and its mutation self-test), and `scripts/verify-units.sh` (`systemd-analyze verify` of the three units and of the boot transaction of `multi-user.target` against a temporary root, plus `systemd-analyze security --offline` printed for information, no threshold)
9. **security** (ORB-SEC-009, SOLL): `govulncheck` (pinned), `npm audit --audit-level=high`, and a trial SBOM generation

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
| `check-helper-hooks.sh` | the release helper contains none of the strings of the fault hooks, and a build with the tag `faulthooks` contains all of them (so the check can fail); `make check-helper` runs it on the built helper |
| `check-helper-key.sh` | the update helper's embedded copy of the release key must be byte-identical to `release/oarfish-release.pub` (run in CI) |
| `check-units.sh` | the three update-helper units keep the directives of ADR 0010 U2/U10/A1 and gain no forbidden one (dependencies, `Environment=`, extra `Exec*`, any mention of `oarfish.service` except the recover unit's `Before=`; the recover unit keeps `DefaultDependencies=no` and `After=local-fs.target`); `--self-test` removes, changes and adds lines in a copy and requires the check to fail every time |
| `verify-units.sh` | `systemd-analyze verify` of those units against a temporary root that holds the paths they name, then of `multi-user.target` with the wants links of the installer, which fails on an ordering cycle (ADR 0010 A14); prints `systemd-analyze security --offline` for information (needs `systemd-analyze`, so Linux) |
| `build-helper.sh` | builds the update helper for linux/arm64 with the flags of the main binary except its `-X` flags (the helper carries no release version, D-C80); `--twice` checks that the build is reproducible |
| `check-helper-independent.sh` | builds the helper with two different `VERSION` and `COMMIT` values and fails unless both builds are identical (D-C80) |
| `make-test-installer.sh` | test copy of `install.sh` with a throwaway key; the production script has no way to swap its key, so tests need a copy. Never published |
| `test-installer.sh` | installer tests against a fake root |
| `test-installer-container.sh`, `installer-container/` | installer end to end in an arm64 container under qemu |
| `check-budgets.sh` | enforces and reports the size budgets; with a second argument it also reports the update helper's size (no budget of its own; the bundle has carried one since G6.3b) |
| `smoke-test-arm64.sh` | extracts the bundle, starts the binary, checks `/healthz` and `/`; with a second argument also runs the update helper stub (`recover` on a machine without `/var/lib/oarfish-update` exits 1 with its fixed message, `serve` without socket activation exits 1, an unknown subcommand exits 2) |
| `check-reproducible.sh` | double build and SHA-256 comparison, then the helper check above and a bundle of another version whose five compared component files must be identical |

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

The standing rules for the implementation agent are tracked in [`AGENT_RULES.md`](AGENT_RULES.md) (D-C79); gate prompts, hand-over notes, package manifests and device notes stay outside the repository (D-C65). The entry file of the development tool is local and untracked, listed in `.git/info/exclude`, and only points to `AGENT_RULES.md`. Device hostnames, usernames, endpoints and credentials belong in an untracked `*.local.md` file (git-ignored) or an untracked `.env`. Never copy such values into a tracked file, a commit message or a pull request text. `.env.example` documents variables the code reads; Gate G1 reads none.

## The installer and its tests

`install/install.sh` is POSIX `sh` (checked with `shellcheck -s sh`, run by `dash` in the tests), `set -eu`, no bashisms. It is a release artifact: the release build copies it and writes the version into the stamped line. A copy without a stamped version refuses to run without `--version`.

The tests need to run the installer without root, without systemd and without the network. That works through one switch: when the environment variable `OARFISH_TEST_ROOT` is set, every path lives below that directory, root and a running systemd are not required, and these variables are honoured: `OARFISH_RELEASE_BASE_URL` (a `file://` URL), `OARFISH_OPENSSL`, `OARFISH_SYSTEMCTL`, `OARFISH_HEALTH_URL`, `OARFISH_HEALTH_TIMEOUT`, `OARFISH_HEALTHCHECK=skip`, and `OARFISH_TEST_USERS=1` (do the user and ownership steps as if root, which the tests combine with stub `useradd`, `userdel`, `getent`, `id` and `chown` on `PATH`). Since G6.3b `OARFISH_LOCK_WAIT` (seconds to wait for the lock) and `OARFISH_TEST_ROOT_UID`/`OARFISH_TEST_ROOT_GID` (the ids that stand for root when the owner of `/var/lib/oarfish-update` is checked, default: the caller's) are honoured too. Without `OARFISH_TEST_ROOT` all of them are ignored, so a production run cannot be redirected by the environment, and it cannot swap its signing key at all (tests use a copy with another key). Only run the test hooks against a scratch directory.

The tests of the components shape (`scripts/test-installer.sh`, section "Components shape") use a stub `systemctl` that keeps state per unit, refuses to start or restart the helper socket while the helper service is "active" (as ADR 0010 A9 describes systemd) and can hold `daemon-reload` open so that a second installer meets the lock of a running one. A host without util-linux `flock` (macOS) gets a small perl stand-in with the same `flock -x -w SECONDS FD` interface; it needs a real OpenSSL 3 (`OARFISH_TEST_OPENSSL`). `scripts/verify-units.sh --installed ROOT` runs `systemd-analyze verify` on the installed link layout of such a root; the test calls it when `systemd-analyze` exists (CI), and skips it with a message otherwise.

The installer must stay under 50,000 bytes (`scripts/check-budgets.sh`, G3): trim comments and messages before raising that.

Rules when changing the installer: keep it dependency-light (only tools Raspberry Pi OS Lite and Ubuntu Server carry), keep every check before the first change to the system, keep the negative control, add a test for every new refusal, and keep the file under the size budget.

## Access-control code and its tests (Gate G5.1)

`backend/internal/auth/` and `backend/internal/audit/` ([`AUTH.md`](AUTH.md)). Working on them:

- Time comes from `internal/clock`. Tests use `clocktest.Fake`, whose timers run inside `Advance`; a test that expects no background work checks `Pending() == 0` (no session, no unwritten audit entry, no timer).
- Tests use cheap Argon2 parameters (`auth.NewHasherWithParams`); `TestProductionParametersAreTheADRValues` checks the real ones and runs one real hash (skipped with `-short`).
- Credentials in tests are synthetic sentinels (`Sentinel-Pw-…`); `TestRedactionOfSecrets` searches the audit file, the journal messages and every response body for them and for the tokens. Add any new secret-carrying field to that test.
- Server tests use `newRequest` (Host `127.0.0.1:8484`); `httptest.NewRequest`'s default host `example.com` is refused by the Host guard. The read-only API tests run through `signedIn`, a wrapper that adds a session cookie; the access-control matrix (`access_test.go`) talks to the bare handler.
- A new audit action, target or result goes into the closed sets in `audit.go` and the table in `AUTH.md`. A new state-changing route needs an entry in `TestStateChangingRequestsMatrix`.
- `TestAuthTraceabilityTableIsComplete` checks the requirement-to-test table in `AUTH.md`.
- The benchmark: `go test -run '^$' -bench LoginVerify ./internal/auth` on the development machine; `make bench-arm64` for the device.

## Release verification code and its tests (Gate G6, step G6.4)

`backend/internal/relverify` ([`ARCHITECTURE.md`](ARCHITECTURE.md#the-release-verification-package-gate-g6-step-g64)) is not imported by any binary yet. `make test-backend`, `make lint` and the Go job of CI cover it like every other package (`go test -race ./...`); `cd backend && go test -race -count=1 ./internal/relverify` runs just this package. Working on it:

- Every key in a test is a throwaway key derived from a label (`newTestKey`). The embedded release key appears only in `golden_test.go`, which verifies the three public files of the published release `v0.1.0-alpha.6` in `testdata/v0.1.0-alpha.6/` (`testdata/README.md` has their SHA-256 and the URLs they were downloaded from; `TestGoldenFilesMatchRecordedHashes` fails if a copy is edited). The bundle is not committed.
- Test releases are built in memory (`newRelease`, `helpers_test.go`): a manifest as a map that tests edit by path (`setPath`, `delPath`), a real tar.gz, `checksums.txt`, and a signature file of the minisign layout. A test that needs a signature for changed bytes builds it again.
- `signature_test.go` ports the cases of `scripts/test-signing.sh` (tampered file and signature, another key, replay, prehashed `ED`) and adds the container format, every byte of the 74-byte payload flipped, every strict prefix, CRLF and base64 variants. `release_test.go` has mix-and-match and replay. `tar_test.go` has one test per rejected entry type and path form, the size, hash, shape and ELF cases, a decompression bomb and a 24 MiB file with a measured allocation bound (`runtime.MemStats.TotalAlloc`).
- `version_test.go` holds the shared table of accepted and rejected version strings. `TestVersionSyntaxEqualsInstaller` extracts `valid_version` from `install/install.sh`, runs it with `sh` over the table and compares; the table lists exactly where `relverify` is narrower (the 64-character and `..` bounds of ADR 0010 U2). It is skipped if `sh` is missing.
- The fuzz targets run their seeds in `go test`. `go test -run '^$' -fuzz '^FuzzInspectTar$' -fuzztime 60s ./internal/relverify` (in `backend/`) fuzzes one of them. A crash input goes into `testdata/fuzz/<target>/` and stays as a regression test.
- `architecture_test.go` checks the imports (`go list -deps` for linux/arm64, linux/amd64 and the host, and the direct imports against a list), that the source has no goroutine, no package-level variable and no `panic`, that every error code is in the list of ADR 0010 U2 (it reads the ADR), and that every error detail is a string literal.
- `TestProducerCompatibility` needs `OARFISH_RELVERIFY_PRODUCER=1`; see `make test-relverify-producer` above.
- A new error code needs the ADR first. A new file in the bundle needs the allow-list in `layout.go`, `scripts/bundlegen` and the installer to change together.

## Update helper core and its tests (Gate G6, step G6.5)

The helper core ([`ARCHITECTURE.md`](ARCHITECTURE.md#the-update-helper-core-gate-g6-step-g65)) is privileged code and is tested harder than the rest. **At the time of this step none of it had run on a device (the G6.9 device run later observed it on one device, see [`UPDATE.md`](UPDATE.md#results-on-the-reference-device)).** From `backend/`:

- `go test -race -count=1 ./cmd/oarfish-update-helper` runs everything (about a minute under `-race`; the crash sweep is the longest part). `-short` skips the test that stages a bundle of more than 32 MiB. The Linux-only parts (`CLOCK_BOOTTIME`, `SO_PEERCRED`) run on the Linux CI job; on a Mac, `GOOS=linux GOARCH=arm64 go vet ./cmd/oarfish-update-helper` at least compiles them.
- Fuzz: `go test -run '^$' -fuzz '^FuzzDecodeRequest$' -fuzztime 60s ./cmd/oarfish-update-helper`, the same for `FuzzDecodeState`; `make fuzz FUZZTIME=60s` runs these and all other targets. Seeds are in `cmd/oarfish-update-helper/testdata/fuzz/`.
- The crash sweep (`crash_test.go`) runs a scenario once to count its mutating file system calls, then for every k twice: once with "error at k" (that call fails) and once with "stop at k" (nothing is executed from k on, which simulates power loss). After each run it checks that `current` is unchanged, `releases/<version>` is absent or the fully verified tree, `state.json` parses, nothing outside the write-target table changed, and that the staging recovery leaves a clean, usable system. `sweepCrashes` takes a `crashScenario`, so G6.6 can add the activation phases to it.
- The file system seam: `fsfake_test.go` has `testFS`, which records every mutating call and can fail or stop at the k-th, or run a hook before each call (to pause a request, to change a file at a chosen instant, to fail a call by predicate). `TestNoMutationOutsideFsops` scans the sources.
- `tenv` (`env_test.go`) builds a complete environment in temporary directories: an installed release `1.0.0` with a component set, a state directory, a cache, a throwaway key, and fake clocks (`uptime`, `mono`). Releases come from `fixture_test.go` (a small copy of the builder in `internal/relverify`'s tests: throwaway Ed25519 key, minisign legacy signature, both bundle shapes, hostile variants). Requests go to `tenv.request` or over a socket (`serveEnv`).
- Three seams exist in the code for tests and are `nil` in production: `handler.wrapFS` (replace the file system), `handler.onChunk` (run between two chunks of a copy, used to grow, shorten or replace a cache file at an exact point) and the package variable `hookAfterLstat` of `openreg.go` (run between the `Lstat` and the open of a file, used to swap a name for a FIFO, a link or another file at that instant). A swap test must not assume that a replacement has another inode: on ext4 a file created right after a removal usually gets the inode of the removed one, on APFS it usually does not, and the same-file check cannot see the difference. `TestCacheFileSwappedBetweenLstatAndOpen` therefore keeps the original alive (renamed to a side name) and checks that the identity differs before it expects `not_found`; `TestCacheFileSwappedForOtherContent` swaps by remove and create and asserts only that the other bytes are never staged (the verification of the bytes read is the integrity boundary, the `Lstat` and same-file checks are defence in depth); `TestCacheLinkToTheCheckedFileIsAccepted` documents that a link to the very file that was checked is accepted by design. Everything else the tests replace (clocks, ids, free space) is a field of `handler`.
- **The helper carries no release version** (D-C80, ADR 0010 A13). `stage` answers `installer_required` when the helper's bytes differ from the installed one, so nothing in the helper may depend on the release version or the commit: it does not import `internal/version` (`TestImports` forbids it), `helper_version` is the constant `helperID` (`"protocol-1"`, kept equal to `protocolMax` by `TestHelperID`), and `scripts/build-helper.sh` passes no `-X` flag. Tests use `helperID` and no release version for the helper. Check it with `scripts/check-helper-independent.sh` (part of `make check-helper`); `make check-reproducible` also compares the five files across two bundles of different versions. A change of the helper code, a package it links, the Go version or a unit text changes the bytes and needs the installer until G6.10; that is intended.
- A change to a privileged function should be tried against the tests by hand first: break it on purpose (drop `O_NONBLOCK`, skip the `Lstat`, skip a `fsync`, skip the lock release on one path, accept another uid) and check that a test fails. The fault-injection run of G6.5 (more than 30 deliberate faults, one at a time) was reported at the review and is not kept in the repository.

## Update helper activation and its tests (Gate G6, step G6.6)

The transaction, the health check, the rollback and the recovery ([`ARCHITECTURE.md`](ARCHITECTURE.md#the-release-transaction-and-the-recovery-gate-g6-step-g66)) are privileged code. **At the time of this step none of it had run on a device (observed on one device in G6.9, see [`UPDATE.md`](UPDATE.md#results-on-the-reference-device)); the tests use a fake `systemctl`, a fake health client and a fake clock, and what they prove is the order of the writes and calls and the invariant of U7, not the behaviour of systemd.**

- `txnenv_test.go` is the harness: `fakeSystemd` knows which release `current` names and behaves per version (`bOK`, `bCrash`, `bHang`, `bWrongVersion`, `bNoVersion`, `bLoop`, `bNeverUp`); its `sleep` advances the fake clocks, so the 45 s deadline costs nothing. `faultGate` numbers every mutating file system call and every `systemctl` call of a run.
- `crashsweep_test.go` runs a scenario once to count the numbered calls and then, for every k, once with "error at k" and once with "stop at k". After each run it checks the invariant of U7 on the disk (`current` names a complete release with the bytes of that version; no directory under a version name is incomplete; `state.json` parses), then runs the next start (`recover` after "stop", the start-up recovery of `serve` after "error") and checks it again: a clean tree, `current` on the new version only after a committed update, and that a second recovery changes nothing. Scenarios: activate with success, activate with a rollback, `recover` from `verifying`, `switching` and `rolling_back`, the live recovery, pruning, and a staged release at boot. `recover_test.go` has one test per row of the U7 table.
- `TestLinuxRecoverSubcommandAsAProcess` and its sibling re-execute the test binary (`os.Args[0]`) and run the real `recover` subcommand with real `flock`, links and renames; they exist only in `*_linux_test.go`, so the CI log (`go test -v`) names them.
- Fuzz: `FuzzDecodeActivate` and `FuzzDecodeAck` (and `FuzzDecodeRequest`, which now covers all five ops): `go test -run '^$' -fuzz '^FuzzDecodeActivate$' -fuzztime 60s ./cmd/oarfish-update-helper`. Both are in the helper list of `make fuzz` and of the CI fuzz step.
- Fault hooks: `go test -tags faulthooks -run '^TestFaultHooks' ./cmd/oarfish-update-helper` (part of `make test-helper`). See "The test build of the update helper" below.
- Cross-building the Linux tests on a Mac for a VM: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o ../bin/linux-tests/helper.test ./cmd/oarfish-update-helper`, then run it by its absolute path (the activation and recovery tests start the binary through `os.Args[0]`).
- A change to a privileged function should be tried against the tests by hand first (the 60 deliberate faults of G6.6 were reported at the review and are not kept in the repository).

## The unit lint and its tests (Gate G6, step G6.10a)

The unit lint (ADR 0010 U15 rule 3 and amendment A15, [`UPDATE.md`](UPDATE.md#what-is-checked-and-in-which-order)) is `unitlint.go` (the parser, the one function `lintUnit` and the call from `stage`) and `unitrules.go` (the compiled-in tables: the accepted forms of the three helper units and the baseline and extras of `oarfish.service`). **None of it has run on a device.** From `backend/`:

- `go test -race -count=1 -run 'Lint|Unit|Service' ./cmd/oarfish-update-helper` runs its tests (`unitlint_test.go`). They read the four unit files of `install/` from the repository (`../../../install/`), so they need the source tree.
- `TestInstallUnitsPassTheLint` and `TestInstallUnitsAreTheFirstForms`: every unit in `install/` passes, and each helper unit normalises to the first accepted form of its list. `TestServiceRulesMatchTheInstallUnit`: the baseline of the table is the set of directives of `install/oarfish.service` minus the named extras. A change of a unit in `install/` without the matching change of the tables fails here, and a change of the tables is the two-step rule of [`RELEASING.md`](RELEASING.md#release-procedure), with an ADR amendment or a decision.
- `TestLintParsing` (one rejected case per forbidden construct, for each of the four units), `TestLintHelperUnits` (every line of a form removed, changed, reordered; added directives; accepted comments, `Description=` and `Documentation=`), `TestLintServiceBaseline` (every baseline directive removed, changed and moved to another section; the weakened values; the `ExecStart` shapes), `TestLintServiceUnlistedDirectives`, `TestLintServiceExtras` (each extra at and outside its bounds), `TestLintReasonsAreClosed`.
- `TestStageUnitRejected`, `TestStageUnitLintOrder` and `TestLintJournalLineShape` run `stage` with signed bundles: a bad unit answers `unit_rejected`, nothing is left behind, the journal names the unit and the reason and nothing of the text. `TestRedaction` has a flow for it.
- The fixtures of the other helper tests (`fixture_test.go`: `baseComponents`, `baseServiceUnit`, `changedUnit`) are unit texts that pass the lint, built from the compiled-in forms and not from `install/`. A test that needs a release with a differing unit uses `changedUnit` (one more comment line), because any other difference of a helper unit is `unit_rejected` before the comparison.
- Fuzz: `go test -run '^$' -fuzz '^FuzzUnitLint$' -fuzztime 60s ./cmd/oarfish-update-helper`. The seeds are the units of `install/` and one mutation per rule; the target checks that the lint does not panic, is deterministic, gives only reasons of the closed set, and that an accepted text has the properties the rules promise. It is in the helper list of `make fuzz` and of the CI fuzz step.
- A new reason goes into the constants and into `lintReasons` in `unitlint.go` and into ADR 0010 A15.3. No reason and no part of a unit text may reach an answer.

## The component transaction and its tests (Gate G6, step G6.10b)

The component transaction (ADR 0010 U15 rule 4 and amendment A16, [`ARCHITECTURE.md`](ARCHITECTURE.md#the-component-transaction-gate-g6-step-g610b), [`UPDATE.md`](UPDATE.md#the-steps-of-an-update-with-components)) is `components.go` (the links, the set digest, the hand-over, the self-check, the rollback and the recovery of the component phases) plus the changes in `stage.go`, `compare.go`, `txn.go`, `recover.go`, `cleanup.go` and `main.go`. It is privileged code. **None of it has run on a device; the tests use the fake root and a fake systemd, and what they prove is the order of the writes and calls and the invariant of U7, not the behaviour of systemd.** From `backend/`:

- `go test -race -count=1 ./cmd/oarfish-update-helper` runs everything; with the sweeps of this step it takes about two minutes under `-race` on a Mac. For a higher `-count`, raise `-timeout` above the default of 10 minutes.
- **The process boundary.** The transaction runs in two processes of the helper: the one that receives `activate` ends with exit code 75, and systemd starts the binary that `components/active` names. `componentenv_test.go` has `supervisor`, which plays that part of systemd: `process(link)` is a new `handler` whose own executable image (`handler.selfImage`, the seam for `/proc/self/exe`) is the helper file that the link names at that instant; `settle` starts the service again while the last process ended with 75; `serveStart` runs the recover unit (`recover(true)`, the known-good image, `onFailure` set) when the active image is one of `crash`; `reboot` is the recover unit and then the first start of `serve`; `mutate` changes a new process (another key id, another protocol range, another image). `countRestart` fails a test at the third restart within 10 s of the fake uptime, the start limit of the helper service (A1). `tenv.newHandler` builds a process; the fakes, the fault gate and the file system wrapper are copied from the last process, so a fault outlives the process that hit it.
- `fakeSystemd` (`txnenv_test.go`) answers `daemon-reload` and `reset-failed oarfish-update-helper.service`; both are numbered calls of the fault gate, and `reloadFail` fails the n-th reload. `acting()` lists the calls without `show`.
- `components_test.go`: the success path with the exact order of the `systemctl` calls and of the journal lines (`TestComponentTransactionSuccess`), the write-ahead order and the write targets (`TestComponentWriteAheadOrder`), the retention, the confirmation (D6), `oarfish.service` alone, `stage` with a set and its refusals, the second lint at `activate`, the reference hash (E2), the self-check with one case per reason, the failed first start with its two restarts, a helper that does not start, the release that fails behind committed components, the socket flag (D4), one test per row c1 to c9, the hand-over on a real socket (`TestHandOverExitsAfterTheAnswer`) and the retry of a start-up recovery that found the lock held. [`UPDATE.md`](UPDATE.md#failure-matrix-f1-to-f32) maps the rows F24 and F26 to F32 to the test names.
- `componentsweep_test.go` is the crash sweep across the process boundary. A scenario runs once to count the numbered calls of every process that takes part; then, for every k, once with "error at k" and once with "stop at k". After each run `checkComponentDisk` checks the invariant of U7 with the component part (`current` names a complete release, both component links name complete sets, no release or component directory under a version name is incomplete). Then the next start runs (a reboot after "stop", the next start of `serve` after "error") and `checkSettled` requires a final state: both links on one set, `current` on the new version only after a committed update, the reference hash of the set that is known-good, nothing left behind; and a second recovery that changes nothing. Scenarios: the whole transaction, the rollback by the new helper, a release that fails behind committed components, a new helper that does not start, the recover unit from each component phase, and `stage` with a component set (through `sweepCrashes` of `crash_test.go`).
- `compat_test.go`: `status` for every `v`, and the state round trip. `oldDecodeState` is the decoder of the code before this step, with a **copy** of its member lists; it must read a state of this step, and this decoder must read `oldStateFile`. A new state member is added to `fullState` (`state_test.go`), to the list in `TestStateRoundTripWithThePreviousDecoder` and to ADR 0010; it must be additive, and it must not differ from an old name only in letter case.
- The service: `internal/updater/followups_test.go` (G6.10 step c, ADR 0010 A17) has the follow-ups F1 to F4: the installer view for each installer code of `stage` and none for the same code from `activate`; the discard after every refusal of `activate` that leaves a release staged, with a stateful fake (`stagingHelper`) whose `stage` answers `busy` while a release is staged; no discard after a failed call; the accepted and refused last results of a rebuilt state on the wire; and the bound of the hand-over on both sides with the fake clock (`handOverLimit`). `internal/updater/components_test.go` runs the whole flow against the fake helper (stage with components, the pause, the second confirmation, activate, the restart of the helper, the restart of the service, the finished result), the flow that ends in `components_rolled_back`, the one of F31, and a request that waits on the socket while no process accepts.
- A new reason of `components_rolled_back` goes into `componentRollbackReasons` (`state.go`) and into ADR 0010 A16.2; the set is closed.
- A change to this code should be tried against the tests by hand first: break it on purpose (skip the write of `c_switched`, switch `known-good` before `c_verified`, skip a directory `fsync`, hand over before the reload, drop one condition of the self-check, do not hand back) and check that a test fails. The two defects that the sweep found while this step was written (a release continued after its directory was removed by an interrupted abort; a staged set left behind by the staging recovery) are covered by `TestCrashSweepRecoverComponentPhases` and `TestCrashSweepStageWithAComponentSet`.

## The test build of the update helper (class T, Gate G6, step G6.9)

The evidence class T of ADR 0010 U13 (D-C56) uses a test build of the helper on the reference device. It differs from the release build in two ways only: the embedded public key (`backend/internal/trustkey/oarfish-release.pub` replaced by a throwaway key, in a copy of the tree, never in the repository) and the build tag `faulthooks`:

```bash
cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -tags faulthooks -ldflags "-s -w" -o ../bin/test-helper ./cmd/oarfish-update-helper
```

With the tag, `faults_on.go` numbers every mutating file system call (write, `fsync`, directory sync, link, rename, removal, mkdir, chmod) and every `systemctl` call of the process, counted from its start. Three environment variables select a fault; without `OARFISH_FAULT_AT` nothing happens:

| Variable | Meaning |
|---|---|
| `OARFISH_FAULT_AT` | the number of the call (from 1) |
| `OARFISH_FAULT_MODE` | `error`: the call fails and does nothing, every other call runs (also any unknown value). `stop`: the process kills itself with `SIGKILL` before the call, which is what a power loss looks like to the files. `hold`: the call never returns; one line on standard error (the journal) names the call (`fault hook: holding at call N (kind)`), the rest of the process keeps running, and power can be removed at a known point. A held helper keeps its transaction, so it does not exit on its idle timer; `RuntimeMaxSec=900` of the unit ends it after 15 minutes. |
| `OARFISH_FAULT_PROC` | (since G6.10 step b) the process the fault applies to, as `<subcommand>:<phase>`: the subcommand is `serve` or `recover`, the phase is the one `state.json` named when the process started (any phase name; `invalid` for an unusable file; for `serve` also `locked` when the lock was held at its start, and `unknown`). Every process counts its own calls from 1; a process that is not named counts and is left alone. Without the variable the fault applies to every process, as before. |

**The processes of a component transaction, and their call numbers.** A component transaction runs in two processes of the helper, and a failed one in a third:

| `OARFISH_FAULT_PROC` | The process |
|---|---|
| `serve:staged` | the helper that receives `activate`, when it was started for that request (the helper has exited on its 30 s idle timer since `stage`). A helper that received `stage` and `activate` in one process is `serve:idle`, and its counter includes the calls of `stage` |
| `serve:c_switched` | the new helper at its first start (the self-check, the commit of the components, the release phases) |
| `recover:c_switched` | the recover unit that finds the new set active: at boot after a power loss, or started by `OnFailure=` when the new helper failed |

A journal line `phase=<name>` is written when the state write of that phase is complete (five calls: create, write, `fsync`, rename, directory `fsync`), so the action that the phase announces starts with the next call. The numbers below are the calls of a process that started in the named phase, for a release whose helper binary differs, with a service that is healthy at every poll (each `systemctl show` is a numbered call, so a slow service moves the numbers after the first poll), without an older component set to prune and without a difference of `oarfish.service`. `TestFaultHooksCallNumbersOfTheComponentTransaction` (test build) fails when they change.

| Process | Calls | What they are |
|---|---|---|
| `serve:staged` | 1 to 2 | the pre-check (`systemctl show`, two polls) |
| | 3 to 7 | the write of `c_switching` (the journal line follows call 7) |
| | 8 to 10 | the switch of `components/active`: link, rename, directory `fsync` (`hold` or `stop` at 8: F29 "at `c_switching`"; at 10 or 11: "after the link switch") |
| | 11 to 15 | the write of `c_switched` |
| | 16 | `daemon-reload`; the process then ends with exit code 75 |
| `serve:c_switched` | (none) | the self-check reads only |
| | 1 to 5 | the write of `c_verified` (`hold` or `stop` at 1: F29 "in the new helper's first start") |
| | 6 to 8 | the switch of `components/known-good` (at 6: before it; at 9: after it, F29 "around the known-good switch") |
| | 9 | `daemon-reload` (D2) |
| | 10 to 14 | the write of `c_committed` |
| | 15 | `systemctl show` (MainPID and NRestarts, E3) |
| | 16 to 20 | the write of `switching` |
| | 21 to 23 | the switch of `current` |
| | 24 to 28 | the write of `verifying` |
| | 29 | `systemctl restart oarfish.service` |
| | 30 to 40 | the health polls |
| | 41 to 45 | the write of `committed`; 46 to 50 the write of `idle` |
| `recover:c_switched` | 1 to 5 | the write of `c_rolling_back` |
| | 6 to 8 | the switch of `components/active` back to the known-good set |
| | 9 | `daemon-reload`; when `OnFailure=` started the unit, 10 is `reset-failed oarfish-update-helper.service` and the following numbers are one higher |
| | 10 to 13 | the removal of the staged release and of the failed set (two calls each) |
| | 14 to 18 | the write of the result `components_rolled_back` |

On the device the variables are given to the helper service by a runtime drop-in below `/run/systemd/system/oarfish-update-helper.service.d/`, which is gone after a reboot or a power loss. A fault in the recover unit needs the same drop-in for `oarfish-update-recover.service`. The device session reads the helper's journal line and `state.json` to confirm the phase that was hit before power is removed.

**Why a release build must not contain the hooks.** An environment variable must never be able to make the production helper fail, stop or hang in the middle of a transaction, and the release helper's bytes are compared on every device (A13). `faults_off.go` (the default, no tag) turns every hook into a no-op that the compiler removes. `TestReleaseBuildHasNoFaultHooks` and `scripts/check-helper-hooks.sh` (part of `make check-helper` and CI) build the helper without and with the tag. They fail unless the release binary contains none of the hook strings (`OARFISH_FAULT_AT`, `OARFISH_FAULT_MODE`, `OARFISH_FAULT_PROC`, `injected fault`) and the hooked binary contains all of them, so that the check itself can fail. Test releases for class T are signed with a throwaway key and never published; a test copy of `install.sh` (`scripts/make-test-installer.sh` plus the device session's own changes) installs them. Class T is never presented as evidence about the production key path.

## Update page and its scenario mode (Gate G6, step G6.8)

The Update page ([`UI.md`](UI.md#update-page-step-g68)) is `frontend/src/pages/Update.svelte` with its client `lib/update.ts`, the code sentences `lib/updateErrors.ts` and the fixtures `lib/update.fixtures.ts`. `npm test` in `frontend/` runs its tests (`pages/Update.test.ts`, `lib/update.test.ts`); they use fake timers for the polling, the 60-second lock and the 3-minute restart limit, and a fake server with plain response objects. **At the time of this step nothing of the page had run against a real update service or on a device; the G6.9 device run exercised it through the GUI update and its GUI texts on one device** ([`UPDATE.md`](UPDATE.md#results-on-the-reference-device)).

**Scenario mode.** To see every state of the page without a device, start the Vite development server and add `?mock=<scenario>` before the `#`:

```bash
cd frontend && npm run dev     # or make dev
# then open http://localhost:5173/?mock=available#/update
```

With `?mock=…` a fake fetch layer answers `/api/v1/session` (signed in as the synthetic user `admin-test`, with a CSRF token) and the four update routes from the fixtures; every other request goes on unchanged (the Dashboard and `/healthz` still need the backend). It exists only in the development server: `main.ts` imports `src/dev/updateMock.ts` in a branch that is `false` in a production build, and `vite.config.ts` fails `npm run build` if the marker string `oarfish-dev-mock` appears in any output file. An unknown scenario prints the list in the browser console and changes nothing. The proxy problem of `make dev` is not touched by this mode.

Scenarios: `no-helper`, `never`, `up-to-date`, `available`, `available-stable`, `available-unverified`, `check-running`, `check-running-then-done`, `check-failed`, `check-rate-limited`, `throttled`, `apply-downloading`, `apply-activating`, `apply-flow`, `awaiting-confirmation`, `installer-required`, `apply-failed`, `result-success`, `result-rolled-back`, `result-components-rolled-back`, `result-rollback-failed`, `result-aborted`, `cooldown`, `needs-attention`, `staged`, `loading`, `load-error`, `restart-timeout`.

Added in G6.10 step c (ADR 0010 A17; synthetic data only): `awaiting-confirmation` now names the four helper files and `oarfish.service`; `awaiting-service-only` (D6, `oarfish.service` alone); `stage-expired` (the release expired while the dialog waited); `apply-components-phase` and `apply-handing-over` (F4: a component phase, and the helper between its two processes); `components-flow` (the dialog, then after "Bestätigen und aktualisieren" the component phases, the hand-over, the release phases, the restart and the result with components); `result-success-components` (with the socket notice), `result-rolled-back-components` (F31), `result-known-good-damaged` (E5), `result-rebuilt` (F3); `installer-key-changed`, `installer-unit-rejected`, `installer-requirements-not-met` (F1). The tests of step c are in the block "component update" of `pages/Update.test.ts`.

Some of them move on their own, two seconds per read: `check-running-then-done` ends in an available version; `apply-flow` (and `apply-downloading`, `apply-activating`) runs through the steps, then three reads without an answer (the restart), then a 401, so the login page appears; sign in with any password and the page shows the successful result; `restart-timeout` stays without an answer and shows the 3-minute failure. In the confirmation, the password `falsch` (or `wrong`) answers `wrong_password`, `viele` answers `rate_limited` with 8 seconds and `busy` answers `busy`; any other password starts the update. "Verwerfen" in `staged` and `awaiting-confirmation` removes the staged release.

## Known limitations at Gate G3

(Detection limits are in [`DETECTION.md`](DETECTION.md), "Verification status".)

- Real hardware: one Raspberry Pi Zero 2 W (image reported as Raspberry Pi OS Lite 64-bit) ran the installer and the service (single data point, see `INSTALL.md`, "Hardware test results"). The update helper layout and the stub's socket activation were observed once on one device with a test release (device observation, G6.3 owner stop, 2026-11-03; same section, not Verified). The GUI update, the update helper and the recover unit were observed on one device in the G6.9 run (class D and T, [`UPDATE.md`](UPDATE.md#results-on-the-reference-device); not Verified). Nothing has run on an Ubuntu Server image or another board. The release bundle's ARM64 binary is exercised under `qemu-user` (smoke test, installer container jobs), which is not a hardware test.
- The unit file was checked with `systemd-analyze verify` and started by a real systemd (257) on that one device, including a reboot (AT-007). The containers have no systemd. Other kernels and images, and the 512 MB memory budget under load, are untested.
- The installer container jobs use plain `debian:12` and `ubuntu:24.04` arm64 images. They are not Raspberry Pi OS Lite or Ubuntu Server images; the results (including which tools the minimal images lacked) say nothing certain about those.
- The embedded UI was verified by fetching the served HTML and assets, and the frontend by unit tests in jsdom. The redesigned UI (UI foundation task) was also opened in headless Chrome on a developer Mac for layout checks and screenshots ([`UI.md`](UI.md#verification-status)); it has not been opened in Firefox or Safari, nor on the device.
- Reproducibility is verified for the same commit, version string, Go and Node versions. Cross-toolchain-version reproducibility is not claimed.
- Manifest `requirements` list OS, CPU architecture and libc. Kernel and memory minimums are not stated because they have not been measured.
- Signature verification exists in the installer (system `openssl`) but not inside the Oarfish binary; the update path is Gate G6. The shared Go verification package exists since G6.4 and is not imported by any binary yet.
- Two installer runs are serialised by an exclusive `flock` on `/var/lib/oarfish-update` (30 s wait) when the release carries the update helper, and when that directory already exists; on one device, installer runs were observed one at a time, with the lock directory created as specified (device observation, G6.3 owner stop, 2026-11-03; see [`INSTALL.md`](INSTALL.md#results-on-the-reference-device-g63)); two runs at the same time were tested only against the fake root (G6.3b, ADR 0010 T14). The helper takes the same lock for `stage`, `discard` and the expiry cleanup since G6.5; that side is tested against temporary directories and `flock(2)` on the CI runners (and `flock(1)` where it exists), not on a device.
