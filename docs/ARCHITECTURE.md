# Architecture (Gate G5.1 state)

This document describes what exists after Gate G5.1 and the boundaries later gates build on. The normative target architecture is section 8 of [`PFLICHTENHEFT.md`](PFLICHTENHEFT.md); where this document and the specification differ, the specification wins.

## Repository layout

```text
backend/                     Go module github.com/swingstate/oarfish/backend
  cmd/oarfish/                 process entry point: flags, listen guard, state directory, signal handling, graceful shutdown (audit flushed), runs detection once
  cmd/oarfish-update-helper/   the update helper (ADR 0010): a separate root program; the stub of G6.3a plus the core of G6.5 (`status`, `stage`, `discard`, state file, lock). Shipped in the default bundle since G6.3b; the service does not talk to it yet
  internal/server/           HTTP handler: /healthz, the account routes, GET /api/v1/device, /api/v1/metrics, /api/v1/events (SSE), Host/Origin/CSRF checks, correlation IDs, listen guard, static frontend, security headers
  internal/auth/             access control (ADR 0009): Argon2id hashes, account file, setup token, in-memory sessions, login limiter (docs/AUTH.md)
  internal/audit/            the audit file: typed JSON lines, buffered, rotated (docs/AUTH.md)
  internal/securefile/       0700/0600 checks and atomic writes under the state directory
  internal/clock/            injectable time for auth and audit (clocktest: a fake clock for tests)
  internal/metrics/          read-only live metrics: collector, sampler (only while watched), history ring, alert rules (docs/DASHBOARD.md)
  internal/platform/         read-only detection: source, hardware, operating_system, capabilities, profiles, detector (docs/DETECTION.md)
  internal/web/              go:embed of the built frontend (dist/)
  internal/trustkey/        the release public key (a go:embed copy of release/oarfish-release.pub) and the minisign key parser; imported by the update helper and relverify
  internal/updater/          the service side of the GUI update (ADR 0010, G6.7): GitHub client, check, download, advisory verification, helper client, the one job; docs/ARCHITECTURE.md "The update service"
  internal/relverify/       release verification (ADR 0010 U4, U5, G6.4): signature, checksums, manifest, versions, tar inspection. Standard library plus trustkey; not imported by any binary yet (below)
  internal/version/          Version and Commit, injected with -ldflags
frontend/                    Svelte 5 + TypeScript + Vite
  src/                       App.svelte (session gate and navigation), pages/ (Home, Gerät, Einrichten, Anmelden, Konto), lib/ (metrics, stream, device and session clients, locales/de.ts + en.ts)
docs/                        specification, ADRs, roadmap, this file
install/                     install.sh (POSIX sh installer), oarfish.service (unit, shipped in the bundle) and the three update-helper units (shipped in the default release bundle since G6.3b)
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
| `GET /healthz` | `200 {"status":"ok","version":"...","pid":n}`, `Cache-Control: no-store`; public, exposes no device data (the version is public, the process ID is for the helper's health check, A11.2) |
| `GET /readyz` | `200 {"status":"ok"}` when the access store and the audit log were opened, the detection result names a profile and the frontend has its `index.html` (memory only, no disk access); else `503 {"status":"not_ready"}`. Public |
| `GET /api/v1/update`, `GET /api/v1/update/notice`, `POST /api/v1/update/check`, `/apply`, `/discard` | the update (G6.7), see "The update service" |
| `GET /api/v1/session`, `POST /api/v1/session`, `DELETE /api/v1/session`, `POST /api/v1/setup`, `POST /api/v1/account/password` | access control (G5.1): session status, login, logout, first-start setup with the setup token, password change. The only state-changing routes. See [`AUTH.md`](AUTH.md) |
| `GET /api/v1/device` | detection result (G4): hardware, OS, kernel, profile, support tier, capabilities, CPU temperature, boot medium. Read-only: other methods `405`, a request body `400`, unknown `/api/` paths typed `404`; `Cache-Control: no-store`. See [`DETECTION.md`](DETECTION.md) |
| `GET /api/v1/metrics` | one snapshot (schema version 1): host name, uptime, CPU, memory and swap, temperatures, storage, network, the device summary for Home, the alerts and the history ring. Same method, body and header rules as the device endpoint. See [`DASHBOARD.md`](DASHBOARD.md) |
| `GET /api/v1/events` | Server-Sent Events: `history` once, `alerts` when they change, `metrics` about every 2 s, a heartbeat comment every 15 s. At most 4 clients (then a typed `503`). Sampling runs only while a client is connected |
| `GET /` and below | embedded frontend, public (the login page must load); unknown paths `404`; `503` if no frontend is embedded |

Since Gate G5.1 every `/api/v1` route except session status, login and setup needs a valid session (D-C46); while no account exists they answer `401 setup_required`. Every request is refused unless its Host header names the device by a loopback name or address (DNS rebinding); state-changing requests also need a same-origin `Sec-Fetch-Site` or a matching `Origin`, `Content-Type: application/json`, and with a session the `X-Oarfish-CSRF` token. Every response carries a server-made `X-Correlation-ID`; typed errors carry it as `correlation_id`. Details: [`AUTH.md`](AUTH.md).

All responses carry `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'`, `X-Content-Type-Options`, `X-Frame-Options` and `Referrer-Policy`. The server sets read-header, read, write and idle timeouts and a header size limit. The write timeout bounds a whole response and would cut every event stream, so the stream handler sets its own write deadline before each write and ends its streams when the server shuts down (ADR 0008).

The process listens on `127.0.0.1:8484` by default (loopback only, ORB-ONB-003). The port is a G1 default, not a specification value. `-listen` must be a literal loopback IP address with a port (`127.0.0.0/8` or `[::1]`); anything else, including an empty host, the unspecified addresses and host names such as `localhost`, stops the process before it listens, with a message and exit status 1. There is no override (D-C29, `server.ValidateListen`). The guard stays after G5.1 until LAN access and HTTPS are designed (D-C48, open item D-D10).

`/readyz` (section 13.1) exists since G6.7. Authentication and audit exist since Gate G5.1 ([ADR 0009](adr/0009-local-access-control-and-audit.md), D-C45 to D-C48, [`AUTH.md`](AUTH.md)); the service still stays on loopback (D-C11, D-C48) until LAN access and HTTPS are designed (D-D10).

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

Not implemented, only reserved by the layout and by the project rules:

- Hardware detection and OS detection are separate concerns (ADR 0002, spec 3.4, 8.3) and implemented in G4 (below). Package, network and service adapters (`packages/`, `network/`, `services/` in spec 8.3) do not exist yet.
- The only privileged code that is installed is the Update Helper, and only by an installer run of a release that carries it (a stub in G6.3b, run once on one device at the G6.3 owner stop with a test release, see [INSTALL.md](INSTALL.md#results-on-the-reference-device-g63); the real helper since G6.5 and G6.6, not run on a device; not Verified). The update path as a whole is described below and in [`UPDATE.md`](UPDATE.md). Privilege Broker and Update Helper are separate components (spec 8.6): the Update Helper arrives in Gate G6 (its stub is in the repository since step G6.3a and in the release bundle since G6.3b, see "The update helper stub" below), the Privilege Broker in Gate G7, its capability check in Gate G8 (D-C28).
- Plugins and self-update are out of scope until their gates (ADR 0003, ADR 0004). Release signing exists (below); the installer verifies signatures with the system `openssl`, but nothing in the Oarfish binary verifies signatures yet.

## The update helper stub (Gate G6, step G6.3a)

*Step G6.5 extended the stub to the helper core; the section after this one describes what G6.5 added and replaces the statements below that say the stub answers only `status`, that nothing verifies a signature and that the helper takes no lock.*

ADR 0010 splits the update into an unprivileged service and a small, separate, privileged helper. Step G6.3a puts the first stub of that helper and its three units into the repository. In G6.3a nothing of it was shipped. **Since step G6.3b the release bundle carries the helper and the units by default, and `install/install.sh` installs them** (below); no code in the service talks to the helper. Everything below describes files in the repository and the installer, tested on CI runners and against a fake root. The installer, the layout and socket activation with the stub were also observed on one device: device observation (G6.3 owner stop, 2026-11-03: one Raspberry Pi Zero 2 W, Debian 13 aarch64, kernel 6.18.50, systemd 257, one image, one moment); a test release with a throwaway key, never published, installed with a test copy of `install.sh`. Details, and what was not exercised (among it the production key path), are in [INSTALL.md](INSTALL.md#results-on-the-reference-device-g63). That is not Verified. Not observed on any device: everything from G6.5 on, among it the helper's side of the lock.

**The program** (`backend/cmd/oarfish-update-helper`) has exactly two subcommands; anything else prints one usage line to stderr and exits 2.

- `recover` touches nothing and exits 0. Recovery is G6.6 (the staging recovery of G6.5 runs inside `stage`).
- `serve` runs only when started by socket activation (`LISTEN_PID` equal to its own pid, `LISTEN_FDS=1`, descriptor 3, a Unix stream listener). Otherwise it exits 1 with a short fixed message and binds nothing itself. The socket, its directory and its modes belong to the socket unit.

**Protocol P is 1, accepted range {1}.** One request per connection: one line of JSON ending in a newline, at most 4 KiB including the newline, 5 s to send it; one answer of at most 8 KiB; at most 4 connections are served at once (the others wait in the socket backlog); exit 0 after 30 s with no connection and none being served. The peer must have the uid of the user `oarfish` (`SO_PEERCRED`, looked up once at start); any other peer is closed without an answer. The decoder is strict: `DisallowUnknownFields`, no trailing data, `v` must be 1, and keys must be exactly `v` and `op` (no other letter case, no duplicate: `encoding/json` would accept both). Errors use only the closed codes of ADR 0010 U2, each with a fixed text; an error never contains the request, a path or an operating-system error string. A request that is too long, not terminated in time, or not valid is `bad_request`.

**Requests in the stub (G6.3a).** Only `status` was answered; since G6.5 `stage` and `discard` are as well, `activate` and `ack` are `bad_request` until G6.6, and so is an unknown `op`. The status request is `{"v":1,"op":"status"}`. The stub's answer was one line (the members added in G6.5 are in the next section):

```json
{"v":1,"ok":true,"protocol_min":1,"protocol_max":1,"helper_version":"protocol-1","key_id":"11f1c52777090bfc","installed_version":"0.1.0-alpha.8","phase":"idle","free_bytes":1234567890}
```

| Member | Meaning |
|---|---|
| `v` | protocol version of this answer |
| `ok` | `true`; an error answer is `{"v":1,"ok":false,"error":"<code>","message":"<fixed text>"}` |
| `protocol_min`, `protocol_max` | the protocol range the helper implements |
| `helper_version` | the constant `protocol-1`, named after the highest protocol the helper implements; not a release version, because the helper's bytes must not depend on the release (D-C80, ADR 0010 A13). The stub of G6.3a reported the version injected at build time, and the helper of `v0.1.0-alpha.7` reports `0.1.0-alpha.7` |
| `key_id` | the id of the embedded release key, 16 lowercase hexadecimal digits |
| `installed_version` | the version in the name of the target of `/opt/oarfish/current`, which must be `releases/<version>` with a version that matches the pattern of U2; `""` if the link is missing or anything else |
| `phase` | the transaction phase; always `"idle"` in the stub |
| `free_bytes` | bytes available to the helper on the file system of `/opt/oarfish` (`statfs`, available blocks times block size) |

The names follow `state.json` and the manifest of ADR 0010. Members may only be added (U15 rule 6). The stub answered a subset of U2's `status`; G6.5 added the staged and previous version, the last result and `needs_attention`, and the component state is G6.10. `phase` is now read from the state file. If `statfs` fails, the answer is `io_error`.

**Key.** `backend/internal/trustkey` embeds a copy of `release/oarfish-release.pub` (`go:embed` cannot leave the module; `scripts/check-helper-key.sh` and a Go test require the copy to be identical) and parses the minisign public key. Its `ID()` is the key id as the manifest field `components.key_id` and `bundlegen check -expect-key-id` expect it: minisign stores the 8 id bytes in the key file in the order `fc0b097727c5f111` and prints them reversed, `11F1C52777090BFC`, in the comment line and in `install.sh`; `ID()` returns that reversed form in lower case, `11f1c52777090bfc`. The helper reports it in `status`. Since G6.5 the helper verifies signatures with it, through the shared package `internal/relverify` (G6.4).

**Resources.** The helper sets a soft Go memory limit of 16 MiB itself, because `MemoryMax=64M` is not enforced on the reference image (ADR 0010 A4) and the unit has no `Environment=` line. Buffers are fixed: a 4 KiB request buffer, 32 KiB copy buffers (G6.5), answers well below 1 KiB.

**What it must not import.** The helper is standard library only, plus `internal/version`, `internal/trustkey` and (since G6.5) `internal/relverify`. It must not import `net/http`, `os/exec` (since G6.6 only `systemctl.go` may), a third-party module or the service's `web`, `auth`, `audit`, `clock`, `securefile` or `server` packages. A test compares `go list -deps` for linux/arm64, linux/amd64 and the host with an allow-list. The helper is a root process and everything compiled into it is attack surface (U1).

**The units** (`install/oarfish-update-helper.socket`, `.service` and `oarfish-update-recover.service`) have the texts of ADR 0010 U10 as amended by A1, plus a `[Unit]` description, `Documentation=` and an `[Install]` section for the socket (`WantedBy=sockets.target`) and the recover unit (`WantedBy=multi-user.target`), which the ADR text implies without printing. The helper has no `Requires=`, `BindsTo=`, `PartOf=` or ordering against `oarfish.service`, and no `Environment=`. The recover unit alone is ordered `Before=` the socket and `oarfish.service`, and it has `DefaultDependencies=no` and `After=local-fs.target`: the default `After=basic.target` of a service, with `Before=` the socket, was an ordering cycle through `sockets.target` that made systemd drop the socket job at boot (A14, D-C81). `scripts/check-units.sh` pins the required lines and forbids the rest; CI also runs `systemd-analyze verify` against a temporary root, verifies the boot transaction of `multi-user.target` there (it fails on an ordering cycle), and prints `systemd-analyze security --offline` for information, with no threshold.

**Build.** `scripts/build-helper.sh` builds the helper for linux/arm64 with the flags of the main binary except its `-X` flags, so the helper's bytes do not depend on the release version or the commit (D-C80, ADR 0010 A13; `scripts/check-helper-independent.sh`), and can check that two builds from an empty cache are identical. CI reports its size next to the bundle sizes; it has no budget of its own; since G6.3b its bytes count in the bundle budget.

## The update helper core (Gate G6, step G6.5)

Step G6.5 turns the stub into the helper core of ADR 0010: a strict decoder per request, the peer check on every connection, a real `status` from a real state file, and the requests `stage` and `discard`. It is the first privileged code that writes below `/opt/oarfish`, and it is reviewed before anything is built on it. **Nothing of this step has been run on a device, and the sandbox of the units (`ProtectSystem=strict`, `ReadWritePaths=/opt/oarfish`, `StateDirectory=`, `SystemCallFilter=@system-service`) has not been exercised with this code.** It is tested on CI runners and on a development machine, against temporary directories and a fake peer check. Linux-only parts (`CLOCK_BOOTTIME`, `SO_PEERCRED`) run on the Linux CI job; the development machine for this step was macOS.

**Not in this step** (G6.6 added `activate`, `ack`, the health check, rollback, the `recover` subcommand, retention, cooldown enforcement, `systemctl` and the fault hooks, see the next section): component replacement, `key_changed`, the unit lint (G6.10).

**Requests.** `status` accepts exactly `v` and `op`. `stage` accepts exactly `v`, `op`, `version` and `correlation_id` (16 lowercase hexadecimal digits). `discard` accepts exactly `v`, `op` and `version`. `version` must match the pattern of U2 (at most 64 characters, no `..`). Every rule of the stub decoder stays: unknown members, repeated members, other letter cases, trailing data, `null`, wrong types, 4 KiB, a missing newline. The peer check (`SO_PEERCRED`, uid of `oarfish` looked up once) still runs before the first byte of the body is looked at; a wrong peer is closed without an answer and the file system is not even opened.

**Answers.** All answers stay below 8 KiB and never echo the request, a path or an operating-system error string. `stage` answers `{"v":1,"ok":true,"version":"…","commit":"…","components_differ":false}`; `components_differ` is always `false` in this step (a difference is `installer_required`) and exists for G6.10. `discard` answers `{"v":1,"ok":true}`. `installer_required` carries one extra member, `"differs":["oarfish-update-helper", …]`, the differing names from the closed list of U2, in the fixed order `oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, `oarfish.service`. `status` has these additive members after the ones of the stub: `phase`, `staged_version` (empty unless a staged release exists), `previous_version`, `last_result` (`outcome`, `reason_code`, `to`, `from`, `correlation_id`, `transaction_id`, `acknowledged`, `finished_at`; `null` when there is none), `needs_attention`.

**State file** (`/var/lib/oarfish-update/state.json`, mode 0600, `schema_version` 1). It has the field set of U8: `helper_version`; `previous_version` (added: U8 lists no member for it and `status` has to report it); `transaction` (`id`, `phase`, `to`, `from`, `from_binary_sha256`, `correlation_id`, `staged_at_uptime_s`, `components_from`, `components_to`, `known_good_sha256`, `socket_restart_pending`); `cooldown` (`refuse_version`, `refuse_until_uptime_s`, `refuse_set_at_uptime_s`, `last_activate_uptime_s`, `last_activate_set_at_uptime_s`); `last_result`. G6.5 writes the phases `idle`, `staging` and `staged` and the last result `aborted` with the reasons `interrupted` and `stage_expired`; the other fields are declared and encoded so that the file of a later gate can be read. The decoder knows all phase names of U7 and U15, ignores unknown members (an older helper reads a newer file) and is strict about the known ones: types, ranges, version and id patterns, hexadecimal hashes, phase and outcome names, uptimes not negative, a repeated key or a key that differs only in letter case is invalid. The file holds no host name, address, URL, user name or serial number. It is replaced by: temporary file `.state.tmp.<id>` (`O_EXCL`, 0600), write, `fsync`, `rename`, `fsync` of the directory; a leftover temporary file is never read. A missing file is a fresh `idle` state without a last result. A file that exists and is not valid is reported by `status` as phase `idle` with `needs_attention: true`; `stage` and `discard` answer `io_error`; nothing is repaired in this step.

**Time since boot.** `CLOCK_BOOTTIME` through `clock_gettime` in the standard library `syscall` package, in whole seconds (`/proc/uptime` is hidden by `ProcSubset=pid`, V12); the wall clock is used only for `finished_at`. The marker type of U6 (`set_at_uptime_s`, `until_uptime_s`) and its predicate exist and are tested (void when the uptime is smaller than `set_at_uptime_s`; the end is exclusive); the cooldown is enforced since G6.6. A staged release expires at 1800 s of uptime since `staged_at_uptime_s`, or when the uptime is smaller (a reboot).

**The lock.** For `stage`, `discard` and the expiry cleanup of `status` the helper opens the directory `/var/lib/oarfish-update` (`O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC`) and takes `flock(LOCK_EX|LOCK_NB)` on that descriptor, the same object the installer locks with `flock -x -w` on descriptor 9. It holds the lock for the whole request and releases it on every path (`defer`). A held lock is `busy` and nothing changes. A `status` request without cleanup work never touches the lock; with work it does not wait either and answers from what it read.

**Cache access** follows U3 step 1: `os.OpenRoot` on `/var/cache/oarfish`, `OpenRoot("update")`, then `<version>/<name>` for the four names, `O_RDONLY|O_NONBLOCK|O_NOFOLLOW|O_CLOEXEC`, `fstat` must say regular file. Missing, link, directory, FIFO, socket, device and a version directory that is not a directory are `not_found`; a size above 64 KiB (the three small files) or 40,000,000 bytes (the bundle) is `checksum_mismatch`; anything else is `io_error`. All four are opened and checked before anything is written. **`os.Root` differs from the assumption of U3 in one place (V5):** `Root.OpenFile` does not honour `O_NOFOLLOW` for the last path element. It resolves the element itself and follows a link that stays inside the root; only a link that leaves the root is refused (its error text, not its type, says "path escapes from parent"). The same holds for the directory levels `update` and `<version>`. The helper therefore calls `Lstat` first (it does not follow the last element) on `update`, on `<version>` and on each file, refuses everything that is not a directory or a regular file, and requires the opened descriptor to be the same file (`os.SameFile`, device and inode). **These checks are defence in depth, not the integrity boundary.** The same-file check cannot detect inode reuse: a file system such as ext4 may give a file created right after a removal the inode of the removed file, so a replacement made by remove and create can compare equal. A link inside the cache that leads to the very file that was checked is accepted, by design, because the bytes read are the checked file's. The boundary is the verification of the bytes read from the opened descriptor: the helper never opens a cache file a second time. Each of the four files is read once, through the descriptor opened in `openCache`, into a private copy (the three small files also into memory); the signature, the checksums, the manifest and the bundle hash and size are verified over those copies, and `Inspect` and the extraction read the private copy below `releases/.stage.<id>`, never the cache. A name swapped after the open changes nothing for the descriptor; a name swapped before it can only lead to other bytes, and those fail the verification against the signed values. (The `Lstat` refusal and the same-file check overlap: removing the `Lstat` refusal alone is not detected by a test, because the same-file check still refuses a link; removing both is. The type check on the opened descriptor matters on its own only where a FIFO was created with the inode of the removed file.)

**The stage sequence** (`stage.go`):

1. take the lock, load the state; only `idle` proceeds. An expired `staged` is cleaned first. A `staging` found while holding the lock is a crash (nobody owns it): the staging recovery runs first. Any other situation, a valid staged release included, is `busy`;
2. the installed version is the target of `/opt/oarfish/current`; missing or invalid is `current_unhealthy`;
3. the four cache files are opened and checked;
4. free-space lower bound (`fstatfs` of `releases/`): the four sizes plus 64 MiB, else `no_space`;
5. write the state `staging` (`to`, `from`, `correlation_id`) before anything is created;
6. create `releases/.stage.<id>` (16 random hexadecimal digits from `crypto/rand`), mode 0700 by an explicit chmod (the unit has `UMask=0077`, no mode relies on the umask);
7. copy the three small files (32 KiB buffer, SHA-256 over the bytes written, limit cap plus one byte, `fsync`), then `relverify.VerifyMetadata` with the embedded release key, the requested version and `runtime.GOARCH`;
8. exact free-space check: copied plus extracted bytes plus 64 MiB;
9. copy the bundle (limit manifest size plus one byte), `Release.CheckBundle(hash, size)`; from here on only the private copy is read;
10. `relverify.CheckUpgrade` (strictly newer, minimum version, helper protocol);
11. `relverify.Inspect` twice on the private copy, first with a nil sink, then with the staging sink (which writes `release/` and `components/` below the staging directory, `O_EXCL`, mode by `fchmod`, by a fixed table of names);
12. the component comparison (below);
13. `fsync` of the files and directories, delete the private copies of the four downloaded files, rename `release/` to `releases/<version>` (a directory of that name that is not the installed or the recorded previous version is moved to `releases/.trash.<id>` first and deleted last), `fsync` of `releases/`, remove the staging directory;
14. write the state `staged` with `staged_at_uptime_s`, answer ok.

The request has a deadline of 180 s on a monotonic clock, checked between chunks and steps; exceeding it cleans up and answers `io_error`. Errors of `relverify` pass through with their code unchanged. Any error after step 5 removes the whole staging tree, removes `releases/<version>` if it was already renamed and the state does not yet say `staged`, and sets the state back to `idle` without a last result (a synchronous error is answered to the caller). If the cleanup itself fails the answer is `io_error` and the state stays `staging` with `to`, so that the recovery finds it.

**Component comparison.** The staged copies of `oarfish-update-helper`, `.socket`, `.service` and `oarfish-update-recover.service` are compared by SHA-256 (streamed, 32 KiB buffer) with the files read through `/opt/oarfish/components/active`, and the staged `oarfish.service` with `/opt/oarfish/current/oarfish.service`. Any difference is `installer_required` with the names; nothing has been moved into `releases/` then. An unreadable or incomplete active set is `components_damaged`, reported before a difference. A bundle without a `components` object (the legacy shape) is `requirements_not_met` (gap rule G1).

**Staging recovery, expiry, discard.** The staging recovery (U7 table rows 2 to 4 only) removes every `releases/.stage.*` and `releases/.trash.*`, removes `releases/<to>` unless it is the installed or the recorded previous version, and sets `idle` with the last result `aborted`, reason `interrupted`. `status` removes an expired staged release when it can take the lock without waiting (same guards), with the last result `aborted`, reason `stage_expired`. `activate` refuses an expired staged release with `stage_expired` (G6.6). `discard` removes the staged release of exactly that version and sets `idle` without a last result; anything else is `not_found`; it never touches the installed or the recorded previous version. Removing a version directory is a rename to `.trash.<id>` followed by the deletion.

**The file system seam** (`fsops.go`). Every mutating file system call of `stage`, `discard`, the state file and the cleanup goes through the interface `fsops`; a test scans the sources and fails on any other mutating call of `os`, `syscall`, `os.Root` or a file opened for writing. The production implementation uses `os.Root` for both trees (`/opt/oarfish` and `/var/lib/oarfish-update`). What `os.Root` could not do, and how it is done: it has no `fchmod` by descriptor and documents a race for `Root.Chmod` on Unix, so the mode is set by opening the name and calling `fchmod` on the descriptor; `Mkdir` takes a mode that the umask reduces, so directories are followed by an explicit chmod; a file is created with `O_EXCL|O_NOFOLLOW` at 0600 and then set to its final mode with `fchmod`; `statfs` is `Fstatfs` on a directory opened through the root; `Root.OpenFile` does not honour `O_NOFOLLOW` for the last element (above); `fsync` of a directory is `Root.Open` plus `File.Sync`. The recording and the failing implementation are test helpers (`fsfake_test.go`). The closed write-target table of U2 is tested: in this step only `releases/<version>`, `releases/.stage.<id>`, `releases/.trash.<id>` and the state directory are mutated, and nothing under `components/` and not `current`.

**Journal.** One line per phase change on stderr, exactly `phase=<name> to=<version> from=<version> cid=<id> result=<code>`; the values are validated versions, ids and fixed codes, never a path or an operating-system error string.

**Memory.** The soft memory limit of the stub stays. Copies and hashes use 32 KiB buffers; a test stages a bundle of more than 32 MiB and requires the total allocation to stay far below its size.

**What it may import.** The standard library plus `internal/version`, `internal/trustkey` and `internal/relverify`. Still forbidden: `net/http`, `plugin`, and (since G6.6) `os/exec` anywhere but in `systemctl.go`, any third-party module (`go list -deps` must show no `golang.org/x` package) and the service's `web`, `auth`, `audit`, `clock`, `securefile` and `server` packages.

**Gap rules set by the prompt of G6.5** (provisional; to be recorded as a decision after the review): G1 a release of the legacy shape while the helper runs is `requirements_not_met`; G2 an existing `releases/<version>` of a newer version is moved aside and replaced; G3 a synchronous error writes no last result; G4 oversize cache files are `checksum_mismatch`; G5 a corrupt state file is `needs_attention` and `io_error` for `stage` and `discard`; G6 only the staging-phase recovery is included; G7 `status` removes an expired staged release; G8 an unreadable `current` is `current_unhealthy`. Further gaps found while implementing, each with the most conservative choice: the state has an explicit `previous_version` member; `discard` in phase `staging` is `not_found` (the recovery belongs to `stage`); the staging recovery also removes `.trash.*`; when the installed version is unknown nothing is removed and the answer is `current_unhealthy`; `discard` answers `{"v":1,"ok":true}`; a `stage` for the recorded previous version with an existing directory of that name is `io_error`; the legacy-shape check comes after all verification; the journal result for a successful phase is `ok`.

## The release transaction and the recovery (Gate G6, step G6.6)

Step G6.6 adds the requests `activate` and `ack`, the transaction of ADR 0010 U5, U6 and U7, the health check, the rollback, the boot-time `recover` subcommand, retention and pruning, the cooldown, and the fault hooks of the test build. It is privileged code and is reviewed before the service side (G6.7) is built on it. **Nothing of it has run on a device. Neither the sandbox of the units (in particular whether `ProtectSystem=strict`, `NoNewPrivileges`, `SystemCallFilter=@system-service` and the empty capability set allow `exec` of `/usr/bin/systemctl` and its connection to systemd), nor a real systemd, a real `oarfish.service` or a real `/healthz` was involved.** The tests use a fake `systemctl`, a fake health client and a fake clock.

**Requests.** `activate` accepts exactly `v`, `op`, `version`, `correlation_id` and `components_confirmed` (a JSON boolean; `null`, a string or a number is `bad_request`); `ack` accepts exactly `v`, `op` and `transaction_id` (16 lowercase hexadecimal digits). `activate` answers `{"v":1,"ok":true,"status":"accepted","transaction_id":"…"}` at once; the transaction continues in the helper without the caller. `ack` answers `{"v":1,"ok":true}`, or `not_found` when the id is not the id of the last result; a second `ack` of the same id is accepted and changes nothing.

**The transaction** (`txn.go`). `activate` takes the lock (`busy` when held) and checks, in this order: the phase is `staged` (`idle` is `not_found`, any other phase is `busy`); the staged version is the requested one (`not_found`); `current` is readable (`current_unhealthy`); the release has not expired and was staged against the version that is installed now (`stage_expired`, the staged release is removed); the `components_confirmed` rule; the cooldown (`cooldown`); the pre-check (below); the hash of the running binary is computed and `releases/<to>/oarfish` is a regular file. Then it writes the phase `switching` (with `from`, `to`, `from_binary_sha256`, the correlation id and `last_activate_*_uptime_s`) and answers. The goroutine that runs the transaction owns the lock and the trees until it ends, and the server does not exit on its idle timer while it runs. The steps, each preceded by its write-ahead state:

1. `switching` is durable; `current` is switched with one atomic replacement (a link `.current.new.<id>` to `releases/<to>`, rename over `current`, `fsync` of `/opt/oarfish`);
2. `verifying` is durable; `systemctl restart oarfish.service`; the health check (below) with a deadline of 45 s from the return of the restart;
3. success: `committed` is durable with `previous_version` = `from` and the last result `success`; pruning; `idle`;
4. failure: `rolling_back` is durable; the SHA-256 of `releases/<from>/oarfish` must equal the recorded one (else `rollback_failed`, and **no** switch to the damaged tree); `current` is switched back; `reset-failed oarfish.service`, `restart`, the health check of the old version; the last result `rolled_back` with the reason (`start_failed`, `health_timeout`, `restart_failed`, `interrupted`), the cooldown is set (15 minutes for that version) and the phase is `idle`. A switch that fails before the restart ends as `aborted` with `switch_failed` and removes `releases/<to>`, which never ran. A failed rollback is `rollback_failed` with `needs_attention` (`rollback_target_damaged`, `rollback_unhealthy`, `restart_failed`, `switch_failed`, `current_unknown`); the helper stops and a person needs a shell. Nothing is pruned after a rollback or a failure.

**The pre-check** (U6): the installed version must be healthy before anything changes. ActiveState `active`, SubState `running`, `/healthz` answering 200, `"status":"ok"` and the installed version, on two polls in a row within 10 s (`current_unhealthy` otherwise). It records MainPID and NRestarts for the check after the restart.

**The health check** (`health.go`). Polled every second: `systemctl show oarfish.service --property=ActiveState,SubState,MainPID,NRestarts,ExecStart --no-pager` must report `active`, `running`, a MainPID that is not the old one and not 0, NRestarts not above the value before the restart; `GET /healthz` on the address after `-listen` in the `ExecStart` property (accepted only as a literal loopback IP address with a non-zero port, else `127.0.0.1:8484`) must answer 200 with `"status":"ok"` and the version that was installed; all of it on 3 polls in a row and for at least 10 s. `failed` or a higher NRestarts ends the wait at once; so does the deadline. The HTTP request is written out on a TCP connection (HTTP/1.0, 2 s, 8 KiB) because `net/http` is not allowed in the helper. **Since G6.7 (A11.2, D-C68):** for a target version that is at least `strictHealthFrom` (a constant of `health.go`, the first release that reports them) the check also requires that the `pid` member of `/healthz` equals the MainPID of the same `systemctl show` reading, so that another process on the loopback port cannot pass, and a 200 with `"status":"ok"` from `/readyz`. An older version, for example a rollback target, is checked with criteria 1 to 4 as before. The constant is set when that release is cut (an item for the release step). Criterion 4 and the `pid` rule are tested against a fake health client.

**`systemctl`** (`systemctl.go`). One function, `handler.systemctl`, is the only place that starts a process: `/usr/bin/systemctl`, no shell, a cleared environment, no standard input, 60 s, 16 KiB of output, a fixed error. It refuses every argument list that is not one of the five shapes of the table in U2 (`restart oarfish.service`; `reset-failed oarfish.service`; `show oarfish.service --property=… --no-pager`; `daemon-reload`; `reset-failed oarfish-update-helper.service`). G6.6 calls the first three; the last two are compiled in for G6.10 and are not called. A test compares the list with the table in the ADR; another allows `os/exec` only in this file.

**Recovery** (`recover.go`). One rule for every unfinished transaction: an update that has not reached `committed` is rolled back. The `recover` subcommand (`oarfish-update-recover.service`, before `oarfish.service`) takes the lock and applies it with only a link switch (`modeBoot`, no `systemctl`, no health check); `serve` applies it at its start, before it accepts a connection, when the state is not `idle` or `staged`, is unreadable, or something is left over (`modeLive`: the full procedure of U6, in the background, requests meanwhile are `busy`). Per phase: `staging` the staging recovery of G6.5; `staged` removed at boot (a reboot ends it) and, in a running helper, only when expired; `switching` or `verifying` with `current` on `from` clears the intent (and removes `releases/<to>` in `switching`), with `current` on `to` rolls back with the reason `interrupted`; `committed` finishes the pruning; `rolling_back` ensures `current` names `from`, compares the hash and ends `rolled_back` or `rollback_failed`; `rollback_failed` is retried when its reason was a failed switch or restart, and otherwise left to a person (at boot it ends the transaction when `current` names `from` and the hash matches, `needs_attention` stays until the result is acknowledged); the component phases of G6.10 are not touched and set `needs_attention`. Afterwards `.stage.*`, `.trash.*`, `.current.new.*` and temporary state files are removed. A state file that exists and is not valid is rebuilt from the disk (installed version = target of `current`, no previous version, no transaction) with `needs_attention` and the last result `aborted`, reason `state_rebuilt`, until the service acknowledges it; a missing file is a fresh idle state. Exit status of `recover`: 0 when the disk is consistent afterwards (also after a rollback), 1 when it could not finish, when the lock is held, or when a rollback failed; the unit is ordered `Before=oarfish.service` and not required by it, so a failure never keeps Oarfish from starting (A9).

**Retention** (U5). After a commit the helper keeps `current` and the previous healthy version and removes every other directory whose name matches the version pattern (rename to `.trash.<id>`, then delete); names that do not match and anything that is not a directory are never touched.

**Cooldown** (U6, R1). After a rollback or a failure the same version is refused for 900 s; any `activate` within 60 s of the previous one is refused. Both markers are in `state.json` and use `CLOCK_BOOTTIME`; a marker is void when the uptime is smaller than the uptime at which it was set. `status` reports `cooldown_version` and `cooldown_remaining_s` (the longer of the two), `previous_version`, the last result and `needs_attention`.

**Fault hooks** (`faults_on.go`, build tag `faulthooks`, off by default). In the test build every write, `fsync`, directory sync, link, rename, removal and `systemctl` call is numbered; the call with the number in `OARFISH_FAULT_AT` fails (`OARFISH_FAULT_MODE=error`) or kills the process with `SIGKILL` before it runs (`stop`, which looks like a power loss). The release build (`faults_off.go`) has no such code: `TestReleaseBuildHasNoFaultHooks` and `scripts/check-helper-hooks.sh` build both and look for the names of the hooks in the binaries. A signed test bundle for the device uses a throwaway key (D-C56, class T), never the production key.

**Gap decisions of G6.6** (the most conservative choice where the ADR is silent; the list is also in the pull request): `components_confirmed` must be `false` while no component difference can be staged (G6.10), a `true` is `bad_request`, and with a staged difference a `false` would be `confirmation_required`; the pre-check is 2 polls within 10 s; a rollback after a damaged hash does not switch; a failed rollback whose cause was a call that failed is retried at the next start; results are in `last_result` and only `rollback_failed` stays a phase; `needs_attention` is a state member cleared by `ack`; a missing `state.json` is not an alarm; `os/exec` is allowed in `systemctl.go` only (the architecture test of `internal/platform`, which forbids `os/exec` in product code, names that file as its one exception); a new `cooldown_version` and `cooldown_remaining_s` in `status`. Unit hardening against `exec` of `systemctl` is a **risk, not changed**: it can only be shown on the device (G6.9).

## The update service (Gate G6, step G6.7)

Source: ADR 0010 U3, U9, U12 and A11 (decision D-C68). Package `internal/updater`; the routes are in `internal/server/update.go`. The service is unprivileged, has no timer and no goroutine that runs without a request, and takes no path, URL or command from the user. **Nothing here is verified on a device or against the real GitHub service**; the tests use a fake GitHub server (TLS, own certificate) and a fake helper.

**Routes** (all behind a session, CSRF layers as everywhere, JSON only, body at most 1 KiB):

| Route | Behaviour |
|---|---|
| `GET /api/v1/update` | The state: installed version, the helper (present, phase, staged version, cooldown), the shared check result and its error, the available release (version, channel, plain-text notes, download size, whether the advisory verification passed), the job (kind, step, error), the helper's last result, and the transitional installer instruction. It asks the helper (at most 10 s), writes `update_finished` for a last result nobody has seen and acknowledges it. It starts nothing. |
| `POST /api/v1/update/check` | `{}` or `{"force":true}`. Without `force` (opening the page): GitHub is asked only when the last attempt, of any outcome, is older than 60 minutes. With `force` ("Check now"): older than 60 s, device-wide. Otherwise the state comes back from memory with `check.throttled`. During a rate-limit wait no request is made. The check is the one job; the call waits for it at most 3 s and the page polls. |
| `POST /api/v1/update/apply` | `{version, current_password}`, and `components_confirmed` for the second stage. Order: the slot of the one job is reserved (`busy`), the version must equal the version of the last check and its advisory verification must have passed, then the fresh password goes through the shared limiter and the one Argon2id hash (the hasher returns its memory to the system), then the job starts and the call returns `202` at once. Nothing is downloaded before the hash is done. |
| `POST /api/v1/update/discard` | `{version}`: removes a staged release; a job that waits for the second confirmation of that version ends. |
| `GET /api/v1/update/notice` | A11.1: answers from the shared in-memory result; asks GitHub only when that result is older than 24 hours (and the last attempt is older than 60 minutes, no rate-limit wait, no job). Never `busy`, never an error. It does not extend the session. **The UI does not call it (G6.8) until a user-visible off switch exists.** |

**The GitHub client** (`github.go`, `download.go`). HTTPS only, TLS 1.2 or newer, the system roots, no proxy from the environment, no keep-alive (so no connection and no goroutine outlives a request), no transparent decompression. At most 3 redirects, each on `https` and on `github.com` or a subdomain of `githubusercontent.com` (default port, no user info). Connect 15 s, TLS 15 s, response headers 20 s, API answer at most 1 MiB, the three small files at most 64 KiB and 30 s each, the bundle at most the size the signed manifest announces and never more than 40,000,000 bytes, a stall timeout of 30 s, an absolute limit of 15 minutes, a 32 KiB buffer, written to a `.part` file that gets its name (and mode 0644) only after the hash and the size match. One retry after 5 s for a temporary failure, no resume. The user agent is `oarfish/<version>`. The URLs are built from the constant repository and the validated tag; nothing else of the API answer is used. On `429`, or `403` with a used-up limit, or `X-RateLimit-Remaining: 0`, the service waits until `Retry-After` or `X-RateLimit-Reset` (capped at one hour) and never loops.

**Verification.** The service verifies the three small files with `internal/relverify` (signature with the embedded key, checksums, manifest, strictly newer, minimum installed version, helper protocol, size cap) at check time and again at apply time on the files it wrote to the cache; the bundle's hash and size are checked as it is written. This is advisory: the helper copies the four files into its own area and verifies them again.

**The job** (one at a time, A11.3). Steps: `checking`; for an apply `fetching_metadata`, `downloading`, `staging`, `awaiting_components_confirmation` (only when the helper reports that components differ; G6.10), `activating`; then `done` or `failed`. A second request answers `busy`. The job does not depend on the browser session. A restart before `stage` ends it harmlessly; after `activate` the helper finishes alone. The helper client timeouts are `status`, `discard`, `ack` 10 s, `activate` 30 s, `stage` 200 s; it retries once on `ECONNRESET` or `ENOENT`. The closed error codes of the helper are mapped to typed errors; no Go error string and no helper message is passed on. `installer_required` carries the names of the differing components from the closed list and the fixed instruction text (`InstallerInstruction`); no command is built.

**Cache.** `<cache>/update/<version>/` (directory 0755, files 0644), removed after `stage`, after a failure and at every start (`CleanCache`). The cache directory comes from `$CACHE_DIRECTORY` (systemd) or `-cache-dir`.

**Audit** (ADR 0009 A6): `update_started` when the administrator confirmed (user = the account, correlation ID = the request's), `update_finished` with user `-` and the same correlation ID, written by the service for a failure before `activate` and for a discard (`aborted`), and for the helper's unacknowledged last result. Targets are `update/<version>`; the results are the codes and outcomes of the helper plus `unavailable`. Check and notice are not audited (A6 has no action for them).

## The update path as implemented, and its verification status (Gate G6, step G6.9)

The update path is complete for the base update (ADR 0010 U1 to U14). The replacement of the helper and its units from the GUI is G6.10. The sections above describe each part; [`UPDATE.md`](UPDATE.md) describes the whole path, the order of the checks, the states, the rollback, the cooldown, when the installer is required, the evidence classes and the failure matrix F1 to F32.

| Part | Implemented in | Tested in CI | On the device |
|---|---|---|---|
| Installer with the helper layout and the lock | G6.3b | fake root, arm64 containers | layout observed once with a test release (G6.3 owner stop); production key path not run |
| Helper: `status`, `stage`, `discard`, lock, staging recovery | G6.5 | yes | not run |
| Helper: `activate`, `ack`, health check, rollback, boot recovery, retention, cooldown | G6.6 | yes (fake systemd, fake health client) | not run |
| Fault hooks of the test build (`error`, `stop`, `hold`) | G6.6, G6.9 | faulthooks test build | not run |
| Update service, routes, audit entries | G6.7 | yes (fake GitHub, fake helper) | not run |
| Update page | G6.8 | jsdom, headless Chrome on one Mac | not run |

**Not verified:** every row of the failure matrix on a device, the real systemd interplay of the helper sandbox with `systemctl`, the timings, the write load and the memory of a real update. G6.9 prepares the device evidence. Its results are recorded by a later closeout step and are not claimed here.

## The release verification package (Gate G6, step G6.4)

`backend/internal/relverify` verifies a release the way ADR 0010 U4 and U5 describe. The update helper (G6.5) and the service (G6.7) will both import it, so that the advisory check in the service and the binding check in the helper cannot drift apart. **In G6.4 nothing imported it; since G6.5 the update helper does (the service does not yet).** Nothing in it is verified on hardware; it is tested on CI runners and on a development machine only.

What it does, in the order a caller uses it:

| Step | Function | Rule |
|---|---|---|
| 1 | `VerifySignature` | line 2 of `checksums.txt.sig` is exactly 74 bytes: `Ed`, the 8-byte id of the embedded key, a 64-byte Ed25519 signature over the exact bytes of `checksums.txt`, checked with `crypto/ed25519`; the prehashed `ED` and a different key id are refused; the trusted comment is ignored; the container must be exactly four lines, at most 64 KiB |
| 2 | `ParseChecksums` | the `sha256sum` format that `bundlegen checksums` writes, strictly: 64 lowercase hexadecimal digits, two spaces, a plain file name, every name once, the bundle and `manifest.json` each exactly once |
| 3 | `ParseManifest`, `Manifest.Check` | the manifest fields of `bundlegen`, the constants and formats of U4, the optional `min_installed_version`, `helper_protocol` and `components`, `contents` of exactly the legacy or the components shape with the modes 0644 and 0755; unknown fields are ignored (the consumer on the device), a missing or invalid known field fails |
| 1 to 3 | `VerifyMetadata`, `Release.CheckBundle` | the above in the order of U4, plus the manifest hash against the signed list, the bundle hash and size against the signed values |
| 4 | `ParseVersion`, `Compare`, `CheckNewer`, `Eligible`, `Latest`, `CheckMinInstalled`, `CheckHelperProtocol`, `CheckUpgrade` | the version syntax of `valid_version` in the installer plus the 64-character and no-`..` bounds of the protocol; SemVer 2.0 precedence; strictly newer (`already_installed`, `downgrade_refused`); the prerelease rule Q7; `requirements_not_met`; `helper_too_old` |
| 5 | `Inspect` | streams the gzip tarball: one top-level directory, regular files from the allow-list only, each in `contents` with the same size and SHA-256, ELF64 little-endian machine 0xB7 for the two programs, 128 MiB in total, a decompression limit, one gzip member and nothing after it; hands every verified file to a `Sink` and never writes a file itself |

**Properties.** Standard library and `internal/trustkey` only (a test checks the imports for linux/arm64, linux/amd64 and the host, and the direct imports against a list). Every function is pure: no network, clock, environment or file access. Input is bounded, nothing panics on malformed input (five fuzz targets), there is no goroutine and no package-level variable (tests check the source). Errors are one type with a code from the closed list of ADR 0010 U2 and a fixed text; the detail for the log is a string literal, so it never contains file content or a name taken from a file (a test checks every call).

**Where it differs from `bundlegen check`.** `bundlegen check` is the producer side and decodes the manifest strictly, unknown fields included. `relverify` is the consumer and ignores unknown fields, so additive manifest changes stay compatible (U4). It is stricter in two places: it refuses a repeated manifest key and a key that differs from a known field only in letter case, which `encoding/json` would accept.

**What the Sink contract means for the helper (G6.5).** `Inspect` can only decide about a file after its last byte and about the bundle after its last entry. `Finish` is called only for a file whose size and hash matched, never after a failure. After any error from `Inspect` the caller must treat everything the sink wrote as unusable and remove the whole staging directory (U7, row 2 does the same after a crash). A caller that wants nothing written for a bad bundle calls `Inspect` twice, first with a nil sink.

**Error code gaps.** U2 has no code for a malformed bundle. A violation of the extraction rules is `manifest_invalid`, a size or hash that differs is `checksum_mismatch`, a program that is not an arm64 ELF executable is `requirements_not_met`, a failing reader or sink is `io_error`.

Tests: `docs/DEVELOPMENT.md`, "Release verification code and its tests (Gate G6, step G6.4)".

## Build and CI

- Builds use `CGO_ENABLED=0` for the Linux ARM64 cross-build, producing a static binary.
- CI runs on GitHub-hosted runners only and never connects to a device (ADR 0003, ORB-CICD-005). Third-party actions are pinned to commit SHAs (ORB-SEC-008) and workflows default to `contents: read`.
- CI jobs: Go checks, frontend checks, release bundle with size budgets and an ARM64 smoke test under qemu-user (which also runs the update helper stub), reproducibility check, signing and tamper tests, installer checks, update helper units and key, dependency scan and SBOM trial.

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
- **Unit.** `install/oarfish.service` travels in the bundle, so it always matches its binary. It runs as `oarfish`, binds `127.0.0.1:8484` (D-C11), uses `StateDirectory` (mode 0700 since G5.1, because it holds the account, the setup token and the audit), `CacheDirectory` and `RuntimeDirectory`, and sandboxes the process (`NoNewPrivileges`, empty capability set, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, kernel and cgroup protections, namespace and realtime restrictions, `@system-service` system call filter). It runs under a real systemd on the reference device (one Raspberry Pi Zero 2 W, see [`INSTALL.md`](INSTALL.md#hardware-test-results)). Hardening that later gates need relaxed (journal access, network management, a privilege broker) is added by those gates. Gate G5 reads more of `/proc` and `/sys` under the unchanged unit; that is expected to work and has not yet been seen on the device (`DASHBOARD.md`, "Verification status").
- **Idempotence and failure.** A re-run with the same version compares files byte for byte and changes nothing (no restart). A release directory that exists but differs is an error. All downloads and checks happen in a temporary directory; the first change to the system happens after everything verified. If a newly activated version fails its health check, the previous version and its unit are restored (a minimal safety net for the installer, not the update logic of Gate G6).
- **Privilege.** The installer runs once as root. It is not a general executor: its commands are fixed. The service is unprivileged. No privileged component is created.
- **No network exposure.** The installer never opens a port or changes a firewall.
- **Out of scope here:** authentication and onboarding (G5.1, [`AUTH.md`](AUTH.md)), platform and hardware detection, metrics, self-update, plugins, reverse proxy and TLS.

- **The hardening is tuned to what the service does today.** `ProtectSystem=strict` makes `/etc` (and everything but the three directories above) read-only for the service, `PrivateDevices=yes` hides physical devices, and `RestrictAddressFamilies` allows only `AF_UNIX`, `AF_INET` and `AF_INET6`, without `AF_NETLINK`. Later gates that need more (G4/G5 reading hardware, sensors and network state, G6 writing releases through the update helper, G7 journal and service control, plugins, Tailscale, containers) must revisit the unit: add a narrow, documented relaxation for the specific access that gate needs (for example one `ReadWritePaths=` entry, `DeviceAllow=` for one device, or `AF_NETLINK`, each with a comment saying which requirement needs it) and test it. Removing hardening wholesale, or setting the unit to run unconfined because something did not work, is not an acceptable fix. Privileged operations belong in the broker and update helper (spec 8.6), not in a weaker service unit.

User instructions: [`INSTALL.md`](INSTALL.md).

## The installer and the update helper layout (Gate G6, step G6.3b)

The installer (`install/install.sh`, POSIX `sh`) accepts exactly two bundle shapes, **legacy** (five entries) and **components** (those plus the helper binary and the helper service, helper socket and recover units), and the shape in the tarball listing must agree with the manifest's `components` object. The manifest's key id is not compared with the installer's own: the release build checks it (`bundlegen check -expect-key-id`, [`RELEASING.md`](RELEASING.md)). The helper binary is checked like the main binary (ELF64, little-endian, machine `0xB7`) and is only copied, never executed, by the installer.

For the components shape the installer builds the link layout of ADR 0010 U5 and nothing else is privileged about it: component sets are immutable directories `/opt/oarfish/components/<id>`, switched by the relative links `active` and `known-good`; `/usr/libexec/oarfish-update-helper` and the units in `/etc/systemd/system` are links into `/opt/oarfish`; the wants links are relative links created by the installer, not by `systemctl enable` (A5). Every link is created or replaced with one `rename` (the recipe of `switch_current`). A regular file or a foreign link at a path the layout needs makes the installer stop before it changes anything.

An exclusive `flock` on the directory `/var/lib/oarfish-update` (created root:root 0700 by the installer) serialises installer runs (30 s wait); the helper takes the same lock for `stage`, `discard` and the expiry cleanup since G6.5 (T14). The order of a run and the `systemctl` calls are fixed (take the lock, create, install, link, one `daemon-reload`, stop the helper service and restart its socket when the helper or the socket unit changed, restart `oarfish.service` when something changed, health check including the socket, then move `known-good`) and tested against a stub; ADR 0010 A9 gives the reasons for the socket handling and for the recover unit's `TimeoutStartSec=120`. Retention of old releases and component sets and everything the helper does beyond `status`, `stage` and `discard` are later steps; the helper takes the lock for `stage`, `discard` and the expiry cleanup since G6.5. No generic privileged executor is introduced: the installer runs as root exactly as before, with a closed list of paths and `systemctl` calls.
