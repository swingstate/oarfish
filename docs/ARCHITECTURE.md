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

Not implemented, only reserved by the layout and by the project rules:

- Hardware detection and OS detection are separate concerns (ADR 0002, spec 3.4, 8.3) and implemented in G4 (below). Package, network and service adapters (`packages/`, `network/`, `services/` in spec 8.3) do not exist yet.
- The only privileged code that is installed is the stub of the Update Helper, and only by an installer run of a release that carries it (G6.3b; run once on one device at the G6.3 owner stop with a test release, see [INSTALL.md](INSTALL.md#results-on-the-reference-device-g63); not Verified). Privilege Broker and Update Helper are separate components (spec 8.6): the Update Helper arrives in Gate G6 (its stub is in the repository since step G6.3a and in the release bundle since G6.3b, see "The update helper stub" below), the Privilege Broker in Gate G7, its capability check in Gate G8 (D-C28).
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
{"v":1,"ok":true,"protocol_min":1,"protocol_max":1,"helper_version":"0.0.0-dev.abc1234","key_id":"11f1c52777090bfc","installed_version":"0.1.0-alpha.8","phase":"idle","free_bytes":1234567890}
```

| Member | Meaning |
|---|---|
| `v` | protocol version of this answer |
| `ok` | `true`; an error answer is `{"v":1,"ok":false,"error":"<code>","message":"<fixed text>"}` |
| `protocol_min`, `protocol_max` | the protocol range the helper implements |
| `helper_version` | the version injected at build time (`dev` for an unstamped build) |
| `key_id` | the id of the embedded release key, 16 lowercase hexadecimal digits |
| `installed_version` | the version in the name of the target of `/opt/oarfish/current`, which must be `releases/<version>` with a version that matches the pattern of U2; `""` if the link is missing or anything else |
| `phase` | the transaction phase; always `"idle"` in the stub |
| `free_bytes` | bytes available to the helper on the file system of `/opt/oarfish` (`statfs`, available blocks times block size) |

The names follow `state.json` and the manifest of ADR 0010. Members may only be added (U15 rule 6). The stub answered a subset of U2's `status`; G6.5 added the staged and previous version, the last result and `needs_attention`, and the component state is G6.10. `phase` is now read from the state file. If `statfs` fails, the answer is `io_error`.

**Key.** `backend/internal/trustkey` embeds a copy of `release/oarfish-release.pub` (`go:embed` cannot leave the module; `scripts/check-helper-key.sh` and a Go test require the copy to be identical) and parses the minisign public key. Its `ID()` is the key id as the manifest field `components.key_id` and `bundlegen check -expect-key-id` expect it: minisign stores the 8 id bytes in the key file in the order `fc0b097727c5f111` and prints them reversed, `11F1C52777090BFC`, in the comment line and in `install.sh`; `ID()` returns that reversed form in lower case, `11f1c52777090bfc`. The helper reports it in `status`. Since G6.5 the helper verifies signatures with it, through the shared package `internal/relverify` (G6.4).

**Resources.** The helper sets a soft Go memory limit of 16 MiB itself, because `MemoryMax=64M` is not enforced on the reference image (ADR 0010 A4) and the unit has no `Environment=` line. Buffers are fixed: a 4 KiB request buffer, 32 KiB copy buffers (G6.5), answers well below 1 KiB.

**What it must not import.** The helper is standard library only, plus `internal/version`, `internal/trustkey` and (since G6.5) `internal/relverify`. It must not import `net/http`, `os/exec`, a third-party module or the service's `web`, `auth`, `audit`, `clock`, `securefile` or `server` packages. A test compares `go list -deps` for linux/arm64, linux/amd64 and the host with an allow-list. The helper is a root process and everything compiled into it is attack surface (U1).

**The units** (`install/oarfish-update-helper.socket`, `.service` and `oarfish-update-recover.service`) have the texts of ADR 0010 U10 as amended by A1, plus a `[Unit]` description, `Documentation=` and an `[Install]` section for the socket (`WantedBy=sockets.target`) and the recover unit (`WantedBy=multi-user.target`), which the ADR text implies without printing. The helper has no `Requires=`, `BindsTo=`, `PartOf=` or ordering against `oarfish.service`, and no `Environment=`. The recover unit alone is ordered `Before=` the socket and `oarfish.service`. `scripts/check-units.sh` pins the required lines and forbids the rest; CI also runs `systemd-analyze verify` against a temporary root and prints `systemd-analyze security --offline` for information, with no threshold.

**Build.** `scripts/build-helper.sh` builds the helper for linux/arm64 with the flags of the main binary and can check that two builds from an empty cache are identical. CI reports its size next to the bundle sizes; it has no budget of its own; since G6.3b its bytes count in the bundle budget.

## The update helper core (Gate G6, step G6.5)

Step G6.5 turns the stub into the helper core of ADR 0010: a strict decoder per request, the peer check on every connection, a real `status` from a real state file, and the requests `stage` and `discard`. It is the first privileged code that writes below `/opt/oarfish`, and it is reviewed before anything is built on it. **Nothing of this step has been run on a device, and the sandbox of the units (`ProtectSystem=strict`, `ReadWritePaths=/opt/oarfish`, `StateDirectory=`, `SystemCallFilter=@system-service`) has not been exercised with this code.** It is tested on CI runners and on a development machine, against temporary directories and a fake peer check. Linux-only parts (`CLOCK_BOOTTIME`, `SO_PEERCRED`) run on the Linux CI job; the development machine for this step was macOS.

**Not in this step:** `activate` and `ack` (still `bad_request`), the health check, rollback, the boot-time `recover` subcommand (still a no-op), retention and pruning of old releases, cooldown enforcement, `systemctl` and `os/exec`, fault hooks, component replacement, `key_changed`, the unit lint.

**Requests.** `status` accepts exactly `v` and `op`. `stage` accepts exactly `v`, `op`, `version` and `correlation_id` (16 lowercase hexadecimal digits). `discard` accepts exactly `v`, `op` and `version`. `version` must match the pattern of U2 (at most 64 characters, no `..`). Every rule of the stub decoder stays: unknown members, repeated members, other letter cases, trailing data, `null`, wrong types, 4 KiB, a missing newline. The peer check (`SO_PEERCRED`, uid of `oarfish` looked up once) still runs before the first byte of the body is looked at; a wrong peer is closed without an answer and the file system is not even opened.

**Answers.** All answers stay below 8 KiB and never echo the request, a path or an operating-system error string. `stage` answers `{"v":1,"ok":true,"version":"…","commit":"…","components_differ":false}`; `components_differ` is always `false` in this step (a difference is `installer_required`) and exists for G6.10. `discard` answers `{"v":1,"ok":true}`. `installer_required` carries one extra member, `"differs":["oarfish-update-helper", …]`, the differing names from the closed list of U2, in the fixed order `oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, `oarfish.service`. `status` has these additive members after the ones of the stub: `phase`, `staged_version` (empty unless a staged release exists), `previous_version`, `last_result` (`outcome`, `reason_code`, `to`, `from`, `correlation_id`, `transaction_id`, `acknowledged`, `finished_at`; `null` when there is none), `needs_attention`.

**State file** (`/var/lib/oarfish-update/state.json`, mode 0600, `schema_version` 1). It has the field set of U8: `helper_version`; `previous_version` (added: U8 lists no member for it and `status` has to report it); `transaction` (`id`, `phase`, `to`, `from`, `from_binary_sha256`, `correlation_id`, `staged_at_uptime_s`, `components_from`, `components_to`, `known_good_sha256`, `socket_restart_pending`); `cooldown` (`refuse_version`, `refuse_until_uptime_s`, `refuse_set_at_uptime_s`, `last_activate_uptime_s`, `last_activate_set_at_uptime_s`); `last_result`. G6.5 writes the phases `idle`, `staging` and `staged` and the last result `aborted` with the reasons `interrupted` and `stage_expired`; the other fields are declared and encoded so that the file of a later gate can be read. The decoder knows all phase names of U7 and U15, ignores unknown members (an older helper reads a newer file) and is strict about the known ones: types, ranges, version and id patterns, hexadecimal hashes, phase and outcome names, uptimes not negative, a repeated key or a key that differs only in letter case is invalid. The file holds no host name, address, URL, user name or serial number. It is replaced by: temporary file `.state.tmp.<id>` (`O_EXCL`, 0600), write, `fsync`, `rename`, `fsync` of the directory; a leftover temporary file is never read. A missing file is a fresh `idle` state without a last result. A file that exists and is not valid is reported by `status` as phase `idle` with `needs_attention: true`; `stage` and `discard` answer `io_error`; nothing is repaired in this step.

**Time since boot.** `CLOCK_BOOTTIME` through `clock_gettime` in the standard library `syscall` package, in whole seconds (`/proc/uptime` is hidden by `ProcSubset=pid`, V12); the wall clock is used only for `finished_at`. The marker type of U6 (`set_at_uptime_s`, `until_uptime_s`) and its predicate exist and are tested (void when the uptime is smaller than `set_at_uptime_s`; the end is exclusive); enforcement of the cooldown is G6.6. A staged release expires at 1800 s of uptime since `staged_at_uptime_s`, or when the uptime is smaller (a reboot).

**The lock.** For `stage`, `discard` and the expiry cleanup of `status` the helper opens the directory `/var/lib/oarfish-update` (`O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC`) and takes `flock(LOCK_EX|LOCK_NB)` on that descriptor, the same object the installer locks with `flock -x -w` on descriptor 9. It holds the lock for the whole request and releases it on every path (`defer`). A held lock is `busy` and nothing changes. A `status` request without cleanup work never touches the lock; with work it does not wait either and answers from what it read.

**Cache access** follows U3 step 1: `os.OpenRoot` on `/var/cache/oarfish`, `OpenRoot("update")`, then `<version>/<name>` for the four names, `O_RDONLY|O_NONBLOCK|O_NOFOLLOW|O_CLOEXEC`, `fstat` must say regular file. Missing, link, directory, FIFO, socket, device and a version directory that is not a directory are `not_found`; a size above 64 KiB (the three small files) or 40,000,000 bytes (the bundle) is `checksum_mismatch`; anything else is `io_error`. All four are opened and checked before anything is written. **`os.Root` differs from the assumption of U3 in one place (V5):** `Root.OpenFile` does not honour `O_NOFOLLOW` for the last path element. It resolves the element itself and follows a link that stays inside the root; only a link that leaves the root is refused (its error text, not its type, says "path escapes from parent"). The same holds for the directory levels `update` and `<version>`. The helper therefore calls `Lstat` first (it does not follow the last element) on `update`, on `<version>` and on each file, refuses everything that is not a directory or a regular file, and requires the opened descriptor to be the same file (`os.SameFile`). A name swapped for a link after that check can only lead to another file inside the cache, whose bytes are hashed against the signed values.

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

**Staging recovery, expiry, discard.** The staging recovery (U7 table rows 2 to 4 only) removes every `releases/.stage.*` and `releases/.trash.*`, removes `releases/<to>` unless it is the installed or the recorded previous version, and sets `idle` with the last result `aborted`, reason `interrupted`. `status` removes an expired staged release when it can take the lock without waiting (same guards), with the last result `aborted`, reason `stage_expired`. The refusal of `activate` with `stage_expired` is G6.6. `discard` removes the staged release of exactly that version and sets `idle` without a last result; anything else is `not_found`; it never touches the installed or the recorded previous version. Removing a version directory is a rename to `.trash.<id>` followed by the deletion.

**The file system seam** (`fsops.go`). Every mutating file system call of `stage`, `discard`, the state file and the cleanup goes through the interface `fsops`; a test scans the sources and fails on any other mutating call of `os`, `syscall`, `os.Root` or a file opened for writing. The production implementation uses `os.Root` for both trees (`/opt/oarfish` and `/var/lib/oarfish-update`). What `os.Root` could not do, and how it is done: it has no `fchmod` by descriptor and documents a race for `Root.Chmod` on Unix, so the mode is set by opening the name and calling `fchmod` on the descriptor; `Mkdir` takes a mode that the umask reduces, so directories are followed by an explicit chmod; a file is created with `O_EXCL|O_NOFOLLOW` at 0600 and then set to its final mode with `fchmod`; `statfs` is `Fstatfs` on a directory opened through the root; `Root.OpenFile` does not honour `O_NOFOLLOW` for the last element (above); `fsync` of a directory is `Root.Open` plus `File.Sync`. The recording and the failing implementation are test helpers (`fsfake_test.go`). The closed write-target table of U2 is tested: in this step only `releases/<version>`, `releases/.stage.<id>`, `releases/.trash.<id>` and the state directory are mutated, and nothing under `components/` and not `current`.

**Journal.** One line per phase change on stderr, exactly `phase=<name> to=<version> from=<version> cid=<id> result=<code>`; the values are validated versions, ids and fixed codes, never a path or an operating-system error string.

**Memory.** The soft memory limit of the stub stays. Copies and hashes use 32 KiB buffers; a test stages a bundle of more than 32 MiB and requires the total allocation to stay far below its size.

**What it may import.** The standard library plus `internal/version`, `internal/trustkey` and `internal/relverify`. Still forbidden: `net/http`, `os/exec`, `plugin`, any third-party module (`go list -deps` must show no `golang.org/x` package) and the service's `web`, `auth`, `audit`, `clock`, `securefile` and `server` packages.

**Gap rules set by the prompt of G6.5** (provisional; to be recorded as a decision after the review): G1 a release of the legacy shape while the helper runs is `requirements_not_met`; G2 an existing `releases/<version>` of a newer version is moved aside and replaced; G3 a synchronous error writes no last result; G4 oversize cache files are `checksum_mismatch`; G5 a corrupt state file is `needs_attention` and `io_error` for `stage` and `discard`; G6 only the staging-phase recovery is included; G7 `status` removes an expired staged release; G8 an unreadable `current` is `current_unhealthy`. Further gaps found while implementing, each with the most conservative choice: the state has an explicit `previous_version` member; `discard` in phase `staging` is `not_found` (the recovery belongs to `stage`); the staging recovery also removes `.trash.*`; when the installed version is unknown nothing is removed and the answer is `current_unhealthy`; `discard` answers `{"v":1,"ok":true}`; a `stage` for the recorded previous version with an existing directory of that name is `io_error`; the legacy-shape check comes after all verification; the journal result for a successful phase is `ok`.

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
