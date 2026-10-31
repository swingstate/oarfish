# Architecture (Gate G5.1 state)

This document describes what exists after Gate G5.1 and the boundaries later gates build on. The normative target architecture is section 8 of [`PFLICHTENHEFT.md`](PFLICHTENHEFT.md); where this document and the specification differ, the specification wins.

## Repository layout

```text
backend/                     Go module github.com/swingstate/oarfish/backend
  cmd/oarfish/                 process entry point: flags, listen guard, state directory, signal handling, graceful shutdown (audit flushed), runs detection once
  internal/server/           HTTP handler: /healthz, the account routes, GET /api/v1/device, /api/v1/metrics, /api/v1/events (SSE), Host/Origin/CSRF checks, correlation IDs, listen guard, static frontend, security headers
  internal/auth/             access control (ADR 0009): Argon2id hashes, account file, setup token, in-memory sessions, login limiter (docs/AUTH.md)
  internal/audit/            the audit file: typed JSON lines, buffered, rotated (docs/AUTH.md)
  internal/securefile/       0700/0600 checks and atomic writes under the state directory
  internal/clock/            injectable time for auth and audit (clocktest: a fake clock for tests)
  internal/metrics/          read-only live metrics: collector, sampler (only while watched), history ring, alert rules (docs/DASHBOARD.md)
  internal/platform/         read-only detection: source, hardware, operating_system, capabilities, profiles, detector (docs/DETECTION.md)
  internal/web/              go:embed of the built frontend (dist/)
  internal/version/          Version and Commit, injected with -ldflags
frontend/                    Svelte 5 + TypeScript + Vite
  src/                       App.svelte (session gate and navigation), pages/ (Home, Gerät, Einrichten, Anmelden, Konto), lib/ (metrics, stream, device and session clients, locales/de.ts + en.ts)
docs/                        specification, ADRs, roadmap, prompts, this file
install/                     install.sh (POSIX sh installer) and oarfish.service (unit, shipped in the bundle)
scripts/                     release packaging, signing, verification, installer tests, budget and smoke tests
  bundlegen/                 stdlib-only Go module: tar.gz, manifest.json, checksums.txt (not part of the product)
release/                     public signing key (oarfish-release.pub)
.github/workflows/           ci.yml, release.yml
Makefile
```

The Go module lives in `backend/` rather than the repository root so the Go toolchain never walks into `frontend/node_modules`, and the two toolchains stay clearly separated. All Go code is under `internal/`; nothing is importable from outside the module yet.

## Frontend delivery

```text
frontend/src ──vite build──▶ backend/internal/web/dist ──go:embed──▶ oarfish binary
```

- Vite writes straight into the Go embed directory (`backend/internal/web/dist`).
- Only `dist/.gitkeep` is tracked. It keeps `//go:embed all:dist` valid on a fresh checkout, so `go vet`, `go test` and `go build` work without Node.js. A binary built without a frontend build answers `503` for UI requests instead of a misleading `404`.
- The Vite config restores `.gitkeep` after each build, because `emptyOutDir` would delete it.
- Production assets are served by the Go binary; Node.js is not needed at runtime (ORB-CICD-003, ORB-NFR-008).

## HTTP surface

| Route | Behaviour |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}`, `Cache-Control: no-store`; public, exposes no device data |
| `GET /api/v1/session`, `POST /api/v1/session`, `DELETE /api/v1/session`, `POST /api/v1/setup`, `POST /api/v1/account/password` | access control (G5.1): session status, login, logout, first-start setup with the setup token, password change. The only state-changing routes. See [`AUTH.md`](AUTH.md) |
| `GET /api/v1/device` | detection result (G4): hardware, OS, kernel, profile, support tier, capabilities, CPU temperature, boot medium. Read-only: other methods `405`, a request body `400`, unknown `/api/` paths typed `404`; `Cache-Control: no-store`. See [`DETECTION.md`](DETECTION.md) |
| `GET /api/v1/metrics` | one snapshot (schema version 1): host name, uptime, CPU, memory and swap, temperatures, storage, network, the device summary for Home, the alerts and the history ring. Same method, body and header rules as the device endpoint. See [`DASHBOARD.md`](DASHBOARD.md) |
| `GET /api/v1/events` | Server-Sent Events: `history` once, `alerts` when they change, `metrics` about every 2 s, a heartbeat comment every 15 s. At most 4 clients (then a typed `503`). Sampling runs only while a client is connected |
| `GET /` and below | embedded frontend, public (the login page must load); unknown paths `404`; `503` if no frontend is embedded |

Since Gate G5.1 every `/api/v1` route except session status, login and setup needs a valid session (D-C46); while no account exists they answer `401 setup_required`. Every request is refused unless its Host header names the device by a loopback name or address (DNS rebinding); state-changing requests also need a same-origin `Sec-Fetch-Site` or a matching `Origin`, `Content-Type: application/json`, and with a session the `X-Oarfish-CSRF` token. Every response carries a server-made `X-Correlation-ID`; typed errors carry it as `correlation_id`. Details: [`AUTH.md`](AUTH.md).

All responses carry `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'`, `X-Content-Type-Options`, `X-Frame-Options` and `Referrer-Policy`. The server sets read-header, read, write and idle timeouts and a header size limit. The write timeout bounds a whole response and would cut every event stream, so the stream handler sets its own write deadline before each write and ends its streams when the server shuts down (ADR 0008).

The process listens on `127.0.0.1:8484` by default (loopback only, ORB-ONB-003). The port is a G1 default, not a specification value. `-listen` must be a literal loopback IP address with a port (`127.0.0.0/8` or `[::1]`); anything else, including an empty host, the unspecified addresses and host names such as `localhost`, stops the process before it listens, with a message and exit status 1. There is no override (D-C29, `server.ValidateListen`). The guard stays after G5.1 until LAN access and HTTPS are designed (D-C48, open item D-D10).

`/readyz` is specified (section 13.1) but does not exist yet. Authentication and audit exist since Gate G5.1 ([ADR 0009](adr/0009-local-access-control-and-audit.md), D-C45 to D-C48, [`AUTH.md`](AUTH.md)); the service still stays on loopback (D-C11, D-C48) until LAN access and HTTPS are designed (D-D10).

## Frontend

- Svelte 5 (runes), TypeScript strict mode, no router, no UI framework.
- UI strings come from `src/lib/i18n.ts` (German default, ORB-GEN-006); an English dictionary has the same keys. The texts are two named groups: UI texts (labels and states) and API texts (sentences chosen by a key from the API).
- The code is split into `styles/` (one token file with a light and a dark set, element defaults), `components/` (app shell, card, badge, meter, sparkline, key-value list, alert list, connection indicator, empty state, summary strip, page header, icon), `pages/` (Home, Gerät) and `routes.ts`, the one list of pages from which the navigation, the router and the page title are built. A new page adds one entry there. Fonts are the system fonts, icons are an own inline-SVG set, nothing is requested from outside. Details, tokens and rules: [`UI.md`](UI.md).
- The app first asks `GET /api/v1/session` and shows nothing of the dashboard until it knows the state: then the setup page, the login page or the app (G5.1, [`AUTH.md`](AUTH.md)). The CSRF token is kept in a module variable only. Any 401 or the end of the stream's session returns to the login page.
- Pages, switched by the address hash (no router): "Home" (default), the live dashboard backed by `/api/v1/metrics` and `/api/v1/events`, the "Gerät" page backed by `/api/v1/device`, and "Konto" (`#/account`, password change), reached from the user menu. Nothing else is shown, in line with the "no fake functions" rule (section 7.1). All UI text is in `src/lib/locales/de.ts` (default) and `en.ts` (prepared). The old status view and its `/healthz` check were replaced by Home, whose connection state says the same and more.
- Home keeps an `EventSource` open only while the tab is visible, shows lost connections and stale values, computes transfer rates from counters in the browser, and draws sparklines as inline SVG (no chart library, no new dependency). Details: [`DASHBOARD.md`](DASHBOARD.md).

## Access control and audit (Gate G5.1)

```text
internal/auth/      password.go policy.go account.go setuptoken.go session.go limiter.go manager.go
internal/audit/     audit.go
internal/securefile/ internal/clock/
```

- **Below the server.** `auth` and `audit` do not import the HTTP layer, `log`, `os/exec`, detection or metrics; `audit` does not import `auth` (it takes typed values). The server wires both in with `WithAuth` and `WithAudit`. Time is injectable everywhere. An architecture test enforces this.
- **One third-party module.** `golang.org/x/crypto/argon2` (and its `blake2b`; `x/sys/cpu` on amd64 only) is the only code from outside the standard library ([`AUTH.md`](AUTH.md#password-hash-and-the-dependency), [`DEVELOPMENT.md`](DEVELOPMENT.md#dependencies)).
- **State on disk** only under the state directory (`$STATE_DIRECTORY`, `/var/lib/oarfish`): `auth/account.json`, `setup-token` while no account exists, `audit/audit.jsonl` with three rotations. Files 0600, directories 0700, checked at every start. Sessions are in memory only.
- **No timers while idle.** The session sweeper exists only while sessions exist, the audit flush timer only while entries wait.

## Boundaries prepared for later gates

Not implemented, only reserved by the layout and by CLAUDE.md rules:

- Hardware detection and OS detection are separate concerns (ADR 0002, spec 3.4, 8.3) and implemented in G4 (below). Package, network and service adapters (`packages/`, `network/`, `services/` in spec 8.3) do not exist yet.
- No privileged code exists. Privilege Broker and Update Helper are separate future components (spec 8.6): the Update Helper arrives in Gate G6, the Privilege Broker in Gate G7, its capability check in Gate G8 (D-C28).
- Plugins and self-update are out of scope until their gates (ADR 0003, ADR 0004). Release signing exists (below); the installer verifies signatures with the system `openssl`, but nothing in the Oarfish binary verifies signatures yet.

## Build and CI

- Builds use `CGO_ENABLED=0` for the Linux ARM64 cross-build, producing a static binary.
- CI runs on GitHub-hosted runners only and never connects to a device (ADR 0003, ORB-CICD-005). Third-party actions are pinned to commit SHAs (ORB-SEC-008) and workflows default to `contents: read`.
- CI jobs: Go checks, frontend checks, release bundle with size budgets and an ARM64 smoke test under qemu-user, reproducibility check, signing and tamper tests, dependency scan and SBOM trial.

## Release pipeline

```text
tag vX.Y.Z ──▶ ci (reused) ──▶ build ──▶ sign ──▶ publish ──▶ GitHub Release
                               no secret  secret   contents: write
                                          only here only here
```

- `scripts/release-bundle.sh` builds the bundle; `scripts/bundlegen` writes the tarball (sorted, commit-time mtimes, numeric owner 0, no gzip name or time), `manifest.json` and `checksums.txt`. The same script runs locally (`make release-bundle`) and in both workflows.
- `checksums.txt` is signed with minisign (Ed25519), `checksums.txt.sig` is published next to it (ADR 0005). The private key exists only as a GitHub Actions secret; the public key is committed as `release/oarfish-release.pub`.
- A tag push publishes; a manual run is a dry run that signs with a throwaway key and publishes nothing.
- A release holds the bundle, `manifest.json`, `install.sh` (the repository's installer with the release version written into one fixed line), `checksums.txt` (covers all of them), `checksums.txt.sig` and the SBOMs. The updater (Gate G6) does not exist yet.

Details: [`RELEASING.md`](RELEASING.md) and [`DEVELOPMENT.md`](DEVELOPMENT.md).

## Platform detection (Gate G4)

```text
internal/platform/
  source/  hardware/  operating_system/  capabilities/  profiles/  detector/
```

- **Read-only, files only.** Detection reads `/proc`, `/sys`, `/run/systemd` and os-release files through an injectable `fs.FS` (`os.DirFS("/")` in production, a fixture directory in tests) with bounded reads. It runs no program and uses no cgo. Which files may be read is fixed and tested (privacy, [`DETECTION.md`](DETECTION.md)).
- **Three separate concepts.** `hardware` (board, SoC, architecture), `operating_system` (os-release, kernel, OS adapter) and `capabilities` (what probes found). Hardware and OS packages do not import each other or the server; only `detector` combines them. An architecture test enforces this.
- **Ubuntu boundary.** `operating_system/ubuntu.go` recognises Ubuntu and reports `not-yet-qualified`. It has no Ubuntu-specific behaviour; the same detector runs on Ubuntu fixtures unchanged.
- **Profiles** are embedded JSON (`go:embed`), strictly decoded and validated: no commands, only allow-listed named handlers, no Verified tier without evidence for all core functions. Decided in [ADR 0007](adr/0007-detection-and-profiles.md).
- **Support tier** (Verified, Compatible, Community, Experimental, Generic, Unsupported) is computed from hardware, OS adapter, capabilities and profile. Unknown ARM64 starts in Generic Mode with no board-specific actions.
- **Run once, cache, read the temperature per request.** Detection runs at startup. Only the CPU temperature is read again per request.
- The unit file is unchanged. Reading `/proc` and `/sys` works under the current sandbox; this was confirmed under a real systemd on one Raspberry Pi Zero 2 W (see "Verification status" in [`DETECTION.md`](DETECTION.md)).

## Metrics (Gate G5)

```text
internal/metrics/
  parse/    pure parsers of /proc and /sys text (fuzzed); no file access, no clock
  collector reads files through platform/source; statfs is the only system call
  sampler   runs only while a stream client is connected; ring of 60 points; client cap
  alerts    rule engine with named thresholds; alerts are translation keys with parameters
  metricstest   fixtures, a changeable file source, a fake clock (tests only)
```

- **Files only, no program, no netlink.** The same rule as detection (ADR 0007, D-C19), extended to the live values. Which files may be opened is fixed and tested (`TestMetricsOpenOnlyAllowListedFiles`); the hardware address of an interface, the machine id, the environment and process data are not on the list.
- **Detection and metrics are separate.** `internal/metrics` imports only `platform/source`; it does not import the hardware, operating-system, capability, profile or detector packages, the HTTP server or `net`. The server composes the device summary from the detection result. An architecture test enforces this, and that no product code imports `os/exec`.
- **Sampling follows the watchers.** No client, no goroutine, no timer, no read. The first client starts a loop (baseline, then a sample every 2 s; slow data every 30 s); the last client stops it and clears the ring. A client holds one frame at most and is dropped when it stops reading. See [`DASHBOARD.md`](DASHBOARD.md) for the bounds.
- **Decided in [ADR 0008](adr/0008-dashboard-metrics.md)** (Accepted, D-C31 to D-C41): files only, Server-Sent Events, sampling only while watched, the ring, the thresholds, the client cap.

## Installer and the device layout (Gate G3)

```text
download (HTTPS, anonymous)         verify (before any change)               install
checksums.txt + .sig  ──▶  Ed25519 with system openssl 3.x and the  ──▶  user, dirs, release dir,
manifest.json, bundle      embedded key; then SHA-256 against the        current symlink, unit,
                           signed checksums.txt; bundle entry checks     start, /healthz, rollback
```

- **Trust model (ADR 0006).** The root of trust on the device is `install.sh`, which the user downloads over HTTPS from GitHub and can read before running it. The release public key is a constant in it (CI checks that it equals `release/oarfish-release.pub`). Everything else is authenticated against that key: `checksums.txt` by its Ed25519 signature, the manifest and the bundle by the SHA-256 values in it. The installer proves first that its `openssl` rejects a modified file (negative control), refuses OpenSSL older than 3.0 and LibreSSL, and accepts only legacy (`Ed`) minisign signatures with the expected key id. It does not protect against someone who controls the repository and replaces `install.sh` together with the key; the documentation says so and prints the key id for an independent comparison.
- **No execution before verification.** Nothing downloaded is run before the signature and hashes are verified. The first execution of the new binary is the health check after installation. The tools the installer runs are those of the distribution.
- **Layout (spec 8.2).** `/opt/oarfish/releases/<version>/` (immutable, root-owned), `/opt/oarfish/current` (relative symlink, switched with `mv -T`, an atomic rename), `/etc/oarfish` (`root:oarfish` 0750, `secrets/` 0700), `/var/lib/oarfish`, `/var/cache/oarfish`, `/run/oarfish` (created by systemd), unit in `/etc/systemd/system/oarfish.service`, user and group `oarfish`. `/usr/libexec/oarfish-update-helper` is not created; the update helper belongs to Gate G6.
- **Unit.** `install/oarfish.service` travels in the bundle, so it always matches its binary. It runs as `oarfish`, binds `127.0.0.1:8484` (D-C11), uses `StateDirectory` (mode 0700 since G5.1, because it holds the account, the setup token and the audit), `CacheDirectory` and `RuntimeDirectory`, and sandboxes the process (`NoNewPrivileges`, empty capability set, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, kernel and cgroup protections, namespace and realtime restrictions, `@system-service` system call filter). It has never been started by a real systemd yet. Hardening that later gates need relaxed (journal access, network management, a privilege broker) is added by those gates. Gate G5 reads more of `/proc` and `/sys` under the unchanged unit; that is expected to work and has not yet been seen on the device (`DASHBOARD.md`, "Verification status").
- **Idempotence and failure.** A re-run with the same version compares files byte for byte and changes nothing (no restart). A release directory that exists but differs is an error. All downloads and checks happen in a temporary directory; the first change to the system happens after everything verified. If a newly activated version fails its health check, the previous version and its unit are restored (a minimal safety net for the installer, not the update logic of Gate G6).
- **Privilege.** The installer runs once as root. It is not a general executor: its commands are fixed. The service is unprivileged. No privileged component is created.
- **No network exposure.** The installer never opens a port or changes a firewall.
- **Out of scope here:** authentication and onboarding (G5.1, [`AUTH.md`](AUTH.md)), platform and hardware detection, metrics, self-update, plugins, reverse proxy and TLS.

- **The hardening is tuned to what the service does today.** `ProtectSystem=strict` makes `/etc` (and everything but the three directories above) read-only for the service, `PrivateDevices=yes` hides physical devices, and `RestrictAddressFamilies` allows only `AF_UNIX`, `AF_INET` and `AF_INET6`, without `AF_NETLINK`. Later gates that need more (G4/G5 reading hardware, sensors and network state, G6 writing releases through the update helper, G7 journal and service control, plugins, Tailscale, containers) must revisit the unit: add a narrow, documented relaxation for the specific access that gate needs (for example one `ReadWritePaths=` entry, `DeviceAllow=` for one device, or `AF_NETLINK`, each with a comment saying which requirement needs it) and test it. Removing hardening wholesale, or setting the unit to run unconfined because something did not work, is not an acceptable fix. Privileged operations belong in the broker and update helper (spec 8.6), not in a weaker service unit.

User instructions: [`INSTALL.md`](INSTALL.md).
