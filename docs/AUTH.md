# Access control and audit (Gate G5.1)

Gate G5.1 adds one local administrator account, server-side sessions, CSRF, origin and Host protection, a rate-limited login, a first-start setup with a one-time token, and an own audit file. The design is [ADR 0009](adr/0009-local-access-control-and-audit.md) (decisions E1 to E9, confirmed as D-C45 to D-C48); the requirements are specification 6.11 (ORB-AUTH-001 to -003, ORB-SEC-004 to -007, ORB-SEC-010), ORB-ONB-001 to -003, 8.1, 8.2, 8.7, 8.8, 10.5, ORB-STO-007, ORB-NFR-009 and AT-023. Where this document and the ADR or the specification differ, they win.

**Status: implemented and tested** with Go and jsdom tests and in CI (including an arm64 container under qemu). **Not verified on hardware**: no login has run on a Raspberry Pi yet, the Argon2id cost and the audit write load have not been measured there, and the cookie behaviour has not been seen in a real browser. Every number below that comes from the device side is an **estimate** until the owner reports the measurements of [Measurements and checks on the device](#measurements-and-checks-on-the-device). G5.1 is not PASS before that evidence is reviewed.

Oarfish still listens on loopback only (D-C48): nothing here opens the device to the network. Remote access stays an SSH tunnel until LAN access and HTTPS are designed (D-D10).

## Contents

1. [What changes for a device](#what-changes-for-a-device)
2. [First start and setup](#first-start-and-setup)
3. [Lost password](#lost-password)
4. [Routes](#routes)
5. [Error codes](#error-codes)
6. [Sessions and cookie](#sessions-and-cookie)
7. [CSRF, origin and Host](#csrf-origin-and-host)
8. [Rate limit](#rate-limit)
9. [Password and user name rules](#password-and-user-name-rules)
10. [Files and modes](#files-and-modes)
11. [Audit file](#audit-file)
12. [Password hash and the dependency](#password-hash-and-the-dependency)
13. [Measurements and checks on the device](#measurements-and-checks-on-the-device)
14. [Verification status](#verification-status)
15. [Requirement to test](#requirement-to-test)
16. [Amendments to ADR 0009 and open points](#amendments-to-adr-0009-and-open-points)

## What changes for a device

- **An updated device starts in setup mode.** A device updated from `v0.1.0-alpha.5` or earlier has no account. The GUI shows the setup page; the dashboard and the device page show data only after setup and login (D-C46). The release notes of the G5.1 release say so ([`RELEASING.md`](RELEASING.md)).
- **Every `/api/v1` route needs a session**, except `GET /api/v1/session`, `POST /api/v1/session` (login), `POST /api/v1/setup` and the public `/healthz` (which says only `{"status":"ok"}`). The frontend files stay public, so the login page loads.
- **A restart signs everybody out.** Sessions live in memory only (E2).
- **The service needs its state directory.** systemd passes it as `STATE_DIRECTORY` (`StateDirectory=oarfish`, `/var/lib/oarfish`). Without it, or when it is not writable, the process refuses to start with a message; for development `-state-dir DIR` sets it (`make dev` uses `.dev-state/`). The unit now sets `StateDirectoryMode=0700`.

## First start and setup

While no account exists, the service creates a 128-bit random setup token at every start, writes it as 32 lower-case hex digits to `/var/lib/oarfish/setup-token` (mode 0600, owner `oarfish`) and keeps it in memory. It is never logged; the journal only says where the file is. Every route except the session status, the setup, `/healthz` and the frontend answers `401` with the code `setup_required`.

On the device (or through SSH) read the token:

```bash
sudo cat /var/lib/oarfish/setup-token
```

Then open the GUI (through the SSH tunnel of [`INSTALL.md`](INSTALL.md#first-start-and-opening-the-gui)), paste the token, choose a user name and a password. The setup page also says, read-only, that Oarfish is reachable on this device only (loopback, ORB-ONB-001); there is no binding choice yet. After the setup the token file is deleted and the browser is signed in. The installer's final message names the same command while no account exists.

The token is compared in constant time, accepts surrounding white space and upper-case hex (it is copied by hand), and is replaced at every restart while no account exists. A stale token file is removed at start when an account exists. Losing the file is harmless: restart the service and a new one is written.

## Lost password

There is no reset by e-mail and no command-line tool. On the device, as root:

```bash
sudo systemctl stop oarfish
```

```bash
sudo rm /var/lib/oarfish/auth/account.json
```

```bash
sudo systemctl start oarfish
```

The service is in setup mode again and writes a new setup token. The audit file is not touched. A present account file that cannot be read or does not validate (broken JSON, unknown fields, an invalid hash, a directory instead of a file) is never treated as "no account": the service refuses to start and its message names the file and this recovery. Uninstalling without `--purge` keeps `/var/lib/oarfish` and therefore the account and the audit; `sudo sh install.sh --uninstall --purge` removes them together with the rest of `/var/lib/oarfish` ([`INSTALL.md`](INSTALL.md#uninstall)).

## Routes

All bodies are JSON. Every response carries `X-Correlation-ID` (64 random bits as 16 hex digits, made by the server; a value from the client is ignored) and the security headers of [`ARCHITECTURE.md`](ARCHITECTURE.md#http-surface). API answers carry `Cache-Control: no-store`. The examples are synthetic.

| Route | Session | What it does |
|---|---|---|
| `GET /api/v1/session` | no | The session state. Does **not** count as activity. Allowed in setup mode. |
| `POST /api/v1/setup` | no | `{setup_token, username, password}`: creates the account, deletes the token file, starts a session, answers like a login. Only in setup mode, otherwise `409 setup_not_available`. |
| `POST /api/v1/session` | no | Login `{username, password}`. Sets the session cookie, answers with the session state. An existing session of the same browser is ended first. |
| `DELETE /api/v1/session` | yes, with CSRF | Logout: ends the session and its event streams, clears the cookie. |
| `POST /api/v1/account/password` | yes, with CSRF | `{current_password, new_password}`. Ends every other session. |
| `GET /api/v1/device`, `GET /api/v1/metrics`, `GET /api/v1/events` | yes | As before ([`DETECTION.md`](DETECTION.md), [`DASHBOARD.md`](DASHBOARD.md)). GET only, no body. The event stream ends with its session. |
| `GET /healthz` | no | `{"status":"ok"}`, no device data. The installer's health check. |

Only the four account endpoints accept `POST` or `DELETE`; everything else under `/api/v1` is GET only (`405 method_not_allowed` with an `Allow` header). The request bodies of setup, login and password change are limited to 1 KiB (`413 request_too_large`), must be one JSON object with exactly the listed fields (`400 invalid_request`), and are never logged.

Session state (`GET /api/v1/session`, and the answer of setup, login and password change):

```json
{"schema_version":1,"state":"setup_required"}
{"schema_version":1,"state":"login_required"}
{"schema_version":1,"state":"authenticated","username":"admin","csrf_token":"<43 characters>",
 "expires_at":"2026-10-29T00:00:00Z","idle_expires_at":"2026-10-28T13:00:00Z","idle_timeout_seconds":3600}
```

`username`, `csrf_token` and the expiry hints are present only when authenticated. `expires_at` is the absolute end (12 h after login), `idle_expires_at` the end if nothing else happens (1 h after the last ordinary request). Logout answers `{"schema_version":1,"state":"login_required"}`.

Login request and answer, as the browser sends it:

```http
POST /api/v1/session HTTP/1.1
Host: 127.0.0.1:8484
Origin: http://127.0.0.1:8484
Sec-Fetch-Site: same-origin
Content-Type: application/json

{"username":"admin","password":"<password>"}
```

```http
HTTP/1.1 200 OK
Set-Cookie: oarfish_session=<43 characters>; Path=/; HttpOnly; SameSite=Strict
X-Correlation-ID: 3f9c2a7d10b4e865
Cache-Control: no-store
Content-Type: application/json

{"schema_version":1,"state":"authenticated","username":"admin","csrf_token":"<43 characters>", ...}
```

A typed error (specification 8.7, now with the correlation ID; `retry_after_seconds` only on `rate_limited`):

```json
{"error":{"code":"rate_limited","message":"Too many attempts.","recovery":"Wait and try again.",
          "correlation_id":"3f9c2a7d10b4e865","retry_after_seconds":8}}
```

The event stream sends one more event when its session ends (logout, expiry, eviction, a password change in another session) and then closes:

```text
event: session
data: {"schema_version":1,"state":"ended"}
```

## Error codes

| Status | Code | When |
|---|---|---|
| 401 | `setup_required` | No account exists; every route except session status, setup, `/healthz` and the frontend. |
| 401 | `unauthenticated` | No session cookie, an unknown, expired or ended session. A stale cookie is cleared. |
| 401 | `invalid_credentials` | Login: wrong user name **or** wrong password (one code, one message, one amount of work). |
| 403 | `invalid_setup_token` | Setup with a wrong token. |
| 403 | `wrong_password` | Password change with a wrong current password. Not 401, so the UI does not sign out. |
| 400 | `username_invalid`, `password_too_short`, `password_too_long`, `password_too_common`, `password_invalid` | A rule is broken ([rules](#password-and-user-name-rules)). In setup only after the token was right. |
| 400 | `invalid_request` | Not one JSON object with exactly the known fields. |
| 413 | `request_too_large` | Body above 1 KiB. |
| 409 | `setup_not_available` | Setup although an account exists. |
| 429 | `rate_limited` | The limiter refused the attempt before any hash; `Retry-After` and `retry_after_seconds`. |
| 403 | `host_rejected` | The Host header is not a loopback name or address (any path). |
| 403 | `origin_rejected` | A state-changing request without `Sec-Fetch-Site: same-origin` or without a matching `Origin`. |
| 415 | `unsupported_media_type` | A state-changing request that is not `Content-Type: application/json`. |
| 403 | `csrf_failed` | A state-changing request with a session but without the right `X-Oarfish-CSRF`. |
| 405 | `method_not_allowed` | A method the route does not take. |
| 400 | `request_body_not_allowed` | A body on a GET route. |
| 404 | `not_found` | Unknown `/api/` path. |
| 503 | `account_unavailable` | The account file could not be written, or the hash failed. |
| 503 | `auth_unavailable` | A build without access control (tests only). |

The order of the checks for a request: Host, route, method, then for state-changing methods Origin / `Sec-Fetch-Site` and Content-Type, then setup mode, then the session, then the CSRF token, then the body.

## Sessions and cookie

- **In memory only** (E2, spec 8.8). The 256-bit token is random (`crypto/rand`); the store keys sessions by its SHA-256 and never holds the token itself. A memory dump does not give a usable cookie.
- **Lifetimes.** Idle timeout 1 hour, absolute lifetime 12 hours, at most 8 sessions; the ninth login evicts the oldest. Only ordinary requests count as activity; the event stream and the session status request do not.
- **Expiry** is checked at every access. A sweeper timer, armed for the earliest deadline and only while at least one session exists, ends expired sessions on time, so their event streams close without waiting for a request. With no session there is no timer (idle CPU budget, ORB-NFR-003).
- **Ending a session** (logout, expiry, eviction, a password change in another session) cancels its context: its event streams send the `session` event and close at once. The sampler stops when the last stream ends.
- **Cookie.** Name `oarfish_session`, `HttpOnly`, `SameSite=Strict`, `Path=/`, no `Domain`, no `Max-Age` or `Expires` (a browser session cookie; the server enforces the lifetimes). On a TLS connection (`r.TLS != nil`, only in tests today) the name is `__Host-oarfish_session` and the cookie is `Secure`. `Secure` is never derived from `X-Forwarded-Proto` or any other header. Safari's handling of `Secure` cookies on `http://127.0.0.1`, the reason for this rule, is a design-review assumption that has not been checked yet.
- **The UI** keeps the CSRF token in a module variable of `frontend/src/lib/session.ts`, never in `localStorage`, `sessionStorage`, a readable cookie, the address or a log. Any 401 `unauthenticated`, the stream's `session` event, or a refused stream whose session is gone takes the UI back to the login page with "Die Sitzung ist beendet."; it does not reconnect in a loop ([`UI.md`](UI.md#access-states-gate-g51)).

## CSRF, origin and Host

Every request: the host part of `Host` (port ignored, so `ssh -L 9000:127.0.0.1:8484` works) must be `localhost`, a literal loopback address (`127.0.0.1`, `[::1]`, the rest of `127.0.0.0/8`) or a name from the allow-list (`server.WithAllowedHosts`, empty in G5.1, for D-D10; D-C50). Otherwise `403 host_rejected`. This stops DNS rebinding. Note that the listen guard still refuses `localhost` as a *listen* address (D-C29); the Host check is about what the browser sends.

Every state-changing method (anything but `GET`, `HEAD`, `OPTIONS`) passes three layers (E3):

1. **Origin.** If `Sec-Fetch-Site` is sent, it must be `same-origin`. Otherwise `Origin` must be sent, be `http` or `https`, and its host (port ignored) must equal the host of `Host`. If both are sent, both must pass. Neither header: refused. `403 origin_rejected`.
2. **Content type** `application/json` (parameters such as `charset` are allowed), so a cross-site HTML form cannot make a valid request. `415 unsupported_media_type`.
3. **CSRF token** for requests with a session (logout, password change): `X-Oarfish-CSRF` must equal the session's token, compared in constant time. `403 csrf_failed`. Login and setup have no session yet; they get the Host, Origin and Content-Type checks and the `SameSite=Strict` cookie.

The CSRF token is created with the session and returned by login, setup and `GET /api/v1/session`. Cross-origin pages cannot read those answers (same-origin policy, and DNS rebinding is stopped by the Host check).

## Rate limit

One global limiter for login, setup-token attempts and the current-password check of a password change (E6; per-address limits are useless behind an SSH tunnel). It is checked **before** any hash.

- From the fifth consecutive failure the next attempt is refused (`429 rate_limited`, `Retry-After`) for 1 s, after the sixth for 2 s, doubling up to 15 minutes.
- In addition at most 10 attempts per minute are processed overall, failed or not.
- A refused attempt is refused even with the right password, and it neither counts as a failure nor extends the wait.
- A successful login resets the consecutive count. There is no hard lockout.
- State: a counter, a deadline and 10 timestamps, in memory.

A wrong user name and a wrong password give the same answer after the same work: for an unknown user the hash is computed against a fixed dummy hash with the current parameters.

## Password and user name rules

- **User name:** 1 to 32 characters from `a-z`, `0-9`, `.`, `_`, `-`.
- **Password:** 12 to 128 Unicode code points, no composition rules, valid UTF-8 without NUL, and not on a short list of common passwords (compared without case). The UI checks length and the repetition before sending; the server checks everything again.
- **The list** (`backend/internal/auth/common_passwords.txt`, 400 entries) was written for this project: keyboard rows, digit runs, "password"/"passwort"/"kennwort" and default device and distribution words (`raspberry`, `ubuntu`, `admin`, `changeme`, ...) with common suffixes, every entry lower case and 12 to 128 characters long. It is Apache-2.0 like the rest of the repository; no third-party list ships. A filtered public list (SecLists, MIT) was considered and not used, because its longer entries include strings that look like personal names and handles.
- **Body limit 1 KiB.** A password of 128 four-byte characters (512 bytes) still fits a login or setup body; a password change from one such password to another does not (two passwords above 1 KiB) and gets `413`.

## Files and modes

Everything the service writes is below its state directory (D-C47), because `ProtectSystem=strict` makes `/etc` read-only for it. `/etc/oarfish/secrets/` stays for operator-provided, read-only secrets.

| Path | Mode | Content |
|---|---|---|
| `/var/lib/oarfish/` | 0700 (`StateDirectoryMode=0700`; the installer creates it as 0750 and systemd sets the unit's mode when the service starts, expected, to be confirmed with `stat` on the device; open item D-D11) | state directory, owner `oarfish` |
| `/var/lib/oarfish/auth/` | 0700 | created at every start if missing |
| `/var/lib/oarfish/auth/account.json` | 0600 | `{"schema_version":1,"username":...,"password_hash":"$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>","created_at":...}`; written atomically (temporary file in the same directory, `fsync`, rename, `fsync` of the directory) |
| `/var/lib/oarfish/setup-token` | 0600 | only while no account exists |
| `/var/lib/oarfish/audit/` | 0700 | |
| `/var/lib/oarfish/audit/audit.jsonl` and `.1` to `.3` | 0600 | the audit file and its rotations |

At start every one of these that exists is checked (ORB-SEC-005): a directory or file of the service user that is more open is set to 0700 / 0600; one that belongs to another user, a symbolic link, or a file where a directory belongs (or the other way round) stops the start with a message naming the path. The writability check of the state directory uses `access(2)` and writes nothing.

## Audit file

`/var/lib/oarfish/audit/audit.jsonl`, one JSON object per line, fixed fields only (ORB-SEC-006, E7):

```json
{"time":"2026-10-28T12:00:00Z","user":"admin","plugin":"","action":"login_succeeded","target":"session","result":"success","correlation_id":"3f9c2a7d10b4e865","count":1}
{"time":"2026-10-28T12:03:10Z","user":"-","plugin":"","action":"login_failed","target":"session","result":"invalid_credentials","correlation_id":"8e1d04b9c2a5f377","count":6}
```

| Field | Values |
|---|---|
| `time` | UTC, RFC 3339, seconds; for an aggregated entry the time of its first event |
| `user` | the account name, or `-`. A failed login or a rejected request is always `-`, never the name that was typed (people type their password into the user name field). |
| `plugin` | empty in the core |
| `action` | `setup_completed`, `login_succeeded`, `login_failed`, `logout`, `password_changed`, `password_change_failed`, `request_rejected` |
| `target` | `setup`, `session`, `account/password`, `device`, `metrics`, `events`, `-` |
| `result` | `success`, `invalid_credentials`, `invalid_setup_token`, `rate_limited`, `wrong_password`, `policy_rejected`, `error`, `host_rejected`, `origin_rejected`, `csrf_failed` |
| `correlation_id` | the server's ID of the request (of the first request for an aggregated entry) |
| `count` | 1, or the number of events in an aggregated entry |

Every field is checked against its closed set or pattern before it is written; there is no free-form text and no map, so request data cannot reach the file. An event outside the sets is dropped and counted.

- **What is recorded.** Setup, every successful login, every logout, every password change and failed change. Failed logins and failed setup-token attempts (`login_failed`, also when rate limited) and rejected requests (`request_rejected`: Host, Origin or CSRF failures on API routes) are **aggregated**: the first event of a kind opens a one-minute window, and the window is written as one entry with its count. A kind is the combination of action, target and result, all from the closed sets, so at most a few entries per minute exist however many attempts there are. A policy error on the setup form is not recorded (it is a typo, not an attempt).
- **Writing.** Entries are buffered in memory (at most 64 KiB) and written 30 s after the first unwritten entry, before a rotation and when the service stops (`SIGTERM`, `SIGINT`: the audit log is closed after the HTTP server). There is no `fsync` per entry: on a power failure up to 30 s of ordinary entries and up to about 90 s of an aggregated entry (60 s window plus 30 s flush) can be lost (accepted in E7 and D-C52). The flush timer and the aggregation timer exist only while there is something to write.
- **Rotation.** Before a line would make the file larger than 1 MiB, `audit.jsonl` becomes `.1`, `.1` becomes `.2`, `.2` becomes `.3` and the old `.3` is gone: at most 4 files, 4 MiB (ORB-STO-007, 10.5).
- **Failure.** A failed write does not stop the service and does not block a request; it is counted and the journal gets the plain message `audit: writing the audit file failed (N failed writes since start)`, without any entry data.
- **Write load (estimate, not measured):** about 200 to 250 bytes per entry; a few logins a day write well below 10 KB per day. Measured on the device with the AT-022 method below.
- **No UI** reads the audit in G5.1. Read it on the device with `sudo cat /var/lib/oarfish/audit/audit.jsonl`.

Request logs: the service writes no per-request log at all. Its journal lines are the start, the setup-mode note with the token file path, audit write failures and shutdown; none contains a body, a cookie, a query string or a secret.

## Password hash and the dependency

- **Argon2id** from `golang.org/x/crypto/argon2` (E1), PHC string `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>` with a 16-byte random salt and a 32-byte key. The parameters are constants in `backend/internal/auth/password.go`. A stored hash with other parameters verifies and is re-hashed with the current ones at the next login (atomic write).
- **Parser bounds.** The PHC parser accepts exactly the canonical form, version 19, `m <= 64 MiB`, `1 <= t <= 10`, `1 <= p <= 4`, salt 8 to 64 bytes, key 16 to 64 bytes, at most 256 characters; a tampered account file cannot make a login allocate more. It is fuzzed (`FuzzParsePHC`, `make fuzz`, 5 s in CI). The fuzzer found that Go's base64 decoder skips `\r` and `\n` even in strict mode; the parser now accepts only the base64 alphabet, and the input is kept as a seed.
- **One hash at a time** (a semaphore of size 1); waiting requests are bounded by the rate limit. After each hash `runtime/debug.FreeOSMemory()` returns the 19 MiB block to the operating system. Effect measured on a developer Mac (Apple M5, not the target): heap kept by the idle process after the last hash 0.05 MiB with it, 19.26 MiB without; about 33 ms per verification there. The Mac's resident size does not show the release (macOS keeps released pages counted until it needs them: the dev binary went from 13 MB idle to 35 MB after the first hash and stayed there). On Linux Go releases with `MADV_DONTNEED`, so the resident size should fall back; `TestHashMemoryIsReturnedToTheOS` checks that on the Linux CI runner (no race detector) and logs the numbers. First CI run (GitHub-hosted ubuntu-24.04, linux/amd64, not the target): `VmRSS` 5,372 kB before, 6,396 kB after four hashes with the production parameters, `VmHWM` 25,620 kB. This matters for the 35 MB idle budget (ORB-NFR-001) and is checked on the device in step 5 below. The cost on the Pi is unknown until the benchmark below runs (ADR estimate 100 to 400 ms, target below about 500 ms).
- **The dependency (D-C49).** `golang.org/x/crypto v0.58.0` (released tag) is the first Go module; `golang.org/x/sys v0.49.0` comes with it. Only the package `argon2` is imported. It uses `golang.org/x/crypto/blake2b` (BLAKE2b is part of the Argon2 definition) and, on amd64 only, `golang.org/x/sys/cpu`; the linux/arm64 release binary contains `argon2` and `blake2b` and not `x/sys/cpu`. `TestArchitectureOnlyArgon2FromXCrypto` fails on any other third-party import and walks argon2's own imports in the module cache; CI also lists the non-standard packages of the linux/arm64 build and runs `go mod verify`. No vendoring: `go.sum` pins the module hashes, the release build already uses `GOFLAGS=-mod=readonly`, and the reproducibility check builds twice with the module cache. The licence (BSD 3-Clause, identical for both modules) is in `THIRD_PARTY_NOTICES.txt`, copied from the module's `LICENSE`.

## Measurements and checks on the device

Run these on the Raspberry Pi with the G5.1 release installed, one command at a time. `USER` and `DEVICE` are your login and the device's name or address, as in [`INSTALL.md`](INSTALL.md). Do not paste the output of commands that show tokens; the commands below are written so that they do not print them, except the one that reads the setup token for the setup itself. Record device, revision, OS, kernel, release, commit and method with every number (spec 12.4).

### 1. Upgrade from `v0.1.0-alpha.5` and setup mode

Upgrade with the stamped installer of the G5.1 release as for earlier releases ([`INSTALL.md`](INSTALL.md#install)). The final message must name `sudo cat /var/lib/oarfish/setup-token`. Then:

```bash
systemctl status oarfish --no-pager
```

```bash
curl -s http://127.0.0.1:8484/api/v1/session
```

Expected: `{"schema_version":1,"state":"setup_required"}`.

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8484/api/v1/device
```

Expected: `401`.

```bash
curl -s http://127.0.0.1:8484/healthz
```

Expected: `{"status":"ok"}`.

### 2. File modes

```bash
sudo stat -c '%a %U:%G %n' /var/lib/oarfish /var/lib/oarfish/auth /var/lib/oarfish/audit /var/lib/oarfish/setup-token
```

Expected: `700 oarfish:oarfish` for the three directories and `600 oarfish:oarfish` for `setup-token`. After the setup (step 4):

```bash
sudo stat -c '%a %U:%G %n' /var/lib/oarfish/auth/account.json /var/lib/oarfish/audit/audit.jsonl
```

Expected: `600 oarfish:oarfish` for both (`audit.jsonl` appears at most 30 s after the first audited event). And the token file is gone:

```bash
sudo ls -la /var/lib/oarfish
```

### 3. Idle memory before a login

```bash
grep -E 'VmRSS|VmHWM' /proc/$(systemctl show -p MainPID --value oarfish)/status
```

### 4. Setup in the browser

Open the SSH tunnel on your computer:

```bash
ssh -L 8484:127.0.0.1:8484 USER@DEVICE
```

On the device, read the token (this output is the secret; do not share it):

```bash
sudo cat /var/lib/oarfish/setup-token
```

Open <http://127.0.0.1:8484> on your computer, enter the token, a user name and a password (use a test password you can share in the report, for example a sentinel string such as `Sentinel-Pw-` followed by a few random letters). Expected: the Home page with live values; the user menu shows the user name.

### 5. Memory after a login and the hash cost

```bash
grep -E 'VmRSS|VmHWM' /proc/$(systemctl show -p MainPID --value oarfish)/status
```

Wait about a minute with no browser tab open on Home (close it or switch to the Gerät page), then once more:

```bash
grep -E 'VmRSS|VmHWM' /proc/$(systemctl show -p MainPID --value oarfish)/status
```

`VmHWM` shows the peak including the hash; `VmRSS` should come back near the idle value of step 3, because the hash memory is returned at once.

Benchmark (one login-equivalent verification with the production parameters). On your computer, in the repository at the release tag:

```bash
make bench-arm64
```

```bash
scp build/bench/oarfish-auth-bench.test USER@DEVICE:/tmp/oarfish-auth-bench.test
```

On the device:

```bash
/tmp/oarfish-auth-bench.test -test.run '^$' -test.bench '^BenchmarkLoginVerify$' -test.benchtime 10x
```

```bash
/tmp/oarfish-auth-bench.test -test.run '^$' -test.bench '^BenchmarkLoginVerifyWithoutFreeOSMemory$' -test.benchtime 10x
```

```bash
rm /tmp/oarfish-auth-bench.test
```

Report `ns/op` (target below about 500,000,000 ns), `VmHWM-kB`, `VmRSS-kB` and `retained-MiB` of both lines. The binary is not part of any release and is built only on your computer.

### 6. Login, logout, wrong password, rate limit, restart

In the browser through the tunnel:

1. **Logout** with the user menu, "Abmelden". Expected: the login page with "Sie sind abgemeldet."
2. **Wrong password** once. Expected: "Benutzername oder Passwort ist falsch." The same sentence for a wrong user name.
3. **Rate limit**: five wrong passwords in a row, then the right one at once. Expected: from the sixth attempt "Zu viele Versuche. Warten Sie eine Sekunde …" (the wait doubles with every further failure), also for the right password; after the wait the right password works.
4. **Login**, then **password change** on Konto: a wrong current password gives "Das aktuelle Passwort ist falsch."; a correct change gives "Das Passwort ist geändert. Alle anderen Sitzungen sind beendet." A second browser signed in before is sent to the login page.
5. **Session ends at restart**: with Home open, on the device:

```bash
sudo systemctl restart oarfish
```

Expected: the page goes to the login page with "Die Sitzung ist beendet. Melden Sie sich erneut an." and does not reconnect in a loop.

### 7. Cookie behaviour in three browsers

Repeat the login in **Safari, Chrome and Firefox** against `http://127.0.0.1:8484` through the tunnel. Each must stay signed in across page loads (Home, Gerät, Konto). In the developer tools, the cookie `oarfish_session` must show HttpOnly, SameSite Strict, Path `/`, no Secure, and "Session" as expiry. Report per browser whether login worked and the attributes shown. This checks the design-review assumption about `Secure` cookies on `http://127.0.0.1` (E3).

### 8. No secret in the audit file or the journal

With the test password of step 4 (replace `SENTINEL` by it):

```bash
sudo grep -c 'SENTINEL' /var/lib/oarfish/audit/audit.jsonl
```

```bash
sudo journalctl -u oarfish --no-pager | grep -c 'SENTINEL'
```

Expected: `0` for both (grep prints 0 and exits with status 1). Also check that no token is in the journal:

```bash
sudo journalctl -u oarfish --no-pager | grep -cE '[0-9a-f]{32}'
```

Expected: `0`. To see what was audited:

```bash
sudo cat /var/lib/oarfish/audit/audit.jsonl
```

Expected: `setup_completed`, `login_succeeded`, `logout`, `login_failed` with `"user":"-"` and a `count`, `password_change_failed`, `password_changed`. No line contains a password or token.

### 9. Write load over 24 hours (AT-022, E7)

Right after a restart (so the counter starts at 0), and again after 24 hours of normal use (a few logins):

```bash
sudo grep write_bytes /proc/$(systemctl show -p MainPID --value oarfish)/io
```

```bash
sudo ls -l /var/lib/oarfish/audit
```

Report both readings and the time between them. The estimate is below 10 KB per day.

### 10. Lost-password recovery

Follow [Lost password](#lost-password). Expected: after the start the GUI shows the setup page again, a new token is in `/var/lib/oarfish/setup-token`, and `audit.jsonl` is still there.

### 11. Idle CPU (unchanged budget)

With no browser tab open and nobody signed in for at least an hour (no session, so no sweeper timer), the method of [`DASHBOARD.md`](DASHBOARD.md#checking-a-device-acceptance-by-hand) (five minutes of CPU ticks) should still show almost 0 ticks.

## Verification status

**Verified by tests (no network, no root, no hardware):** everything in the table below. Backend: Go tests with `-race`, a fake clock for every timer (sessions, limiter, audit), synthetic credentials, temporary state directories, `httptest` servers (also TLS). Process level: the real binary started by the test with a temporary `STATE_DIRECTORY`, set up over HTTP, stopped with `SIGTERM`. Frontend: jsdom with a fake `fetch` and `EventSource`. CI also runs the release binary under qemu-user (smoke test: setup mode, 401 without a session, the token not in the log) and the installer in arm64 Debian 12 and Ubuntu 24.04 containers (modes of `setup-token`, `auth/` and `audit/`, 401 without an account, the installer's hint).

**Not verified:**

- **Anything on the Raspberry Pi**: login, logout, password change, the setup in a browser, the Argon2id cost, memory before and after a login, the audit write load, the file modes under the real systemd (`StateDirectoryMode`), the restart behaviour.
- **Real browsers**: the cookie in Safari, Chrome and Firefox over `http://127.0.0.1` (E3 design-review assumption), the forms' layout, keyboard use and screen readers on the new pages.
- **The OWASP parameter recommendation** (m = 19 MiB, t = 2, p = 1) was not re-read; it is a design-review assumption of E1.
- **TLS** exists only in tests (D-D10).
- **Long running**: many hours of sessions and audit rotation on a real card.

## Requirement to test

`TestAuthTraceabilityTableIsComplete` parses this table: every requirement in scope must be listed, and every named Go test or frontend test must exist.

| Requirement | Tests |
|---|---|
| ORB-ONB-002 first start creates a local administrator; no normal access without it | `TestSetupModeBlocksEverythingElse`, `TestSetupNeedsTheToken`, `TestSetupTokenLifecycle`, `TestMainSetupThenSIGTERMFlushesTheAudit`, `Auth.test.ts "names the command for the token, says that Oarfish listens on this device only"`, `Auth.test.ts "shows the server answer for a wrong token, then sets up and opens the app"`, `Auth.test.ts "shows nothing of the dashboard before the session state is known"` |
| ORB-AUTH-001 login, logout and password change | `TestLoginLogoutAndPasswordChange`, `TestChangePassword`, `TestHashRoundTrip`, `TestLoginUpgradesAnOldHash`, `Auth.test.ts "has a user menu with the account page and Abmelden, and logs out"`, `Auth.test.ts "changes the password on the account page"`, `Auth.test.ts "signs in, keeps the token out of browser storage"` |
| ORB-AUTH-002 server-side invalidatable sessions, secure cookies, sensible lifetime | `TestSessionIdleTimeout`, `TestSessionAbsoluteTimeout`, `TestSessionSweeperEndsExpiredSessionsAndOnlyRunsWhileSessionsExist`, `TestSessionEvictionAtNine`, `TestSessionTokenIsNotStoredRaw`, `TestDeleteAndDeleteOthersCloseSessions`, `TestRestartEndsAllSessions`, `TestSessionCookieAttributesPlainHTTP`, `TestSessionCookieAttributesTLS`, `TestSessionStatusAndIdleExtension`, `TestEveryAPIRouteNeedsASession`, `TestEventStreamEndsWithItsSession`, `TestEventStreamDoesNotExtendTheSession`, `Auth.test.ts "returns to the login page with"`, `Auth.test.ts "returns to the login page when the server ends the stream with the session event"`, `stream.test.ts "stops for good and reports it when the server ends the stream with the session event"` |
| ORB-AUTH-003 rate-limited login | `TestBackoffDoublesFromOneSecondToFifteenMinutes`, `TestLimiterBackoff`, `TestLimiterHasNoHardLockout`, `TestLimiterTenAttemptsPerMinute`, `TestLoginRateLimitRefusesEvenTheRightPassword`, `TestSetupTokenAttemptsAreRateLimited`, `TestLoginRateLimitOverHTTP`, `Auth.test.ts "shows the wait time when rate limited"` |
| ORB-SEC-004 CSRF and origin protection | `TestStateChangingRequestsMatrix`, `TestHostHeaderGuard`, `TestAuthRequestBodies`, `session.test.ts "sends JSON and the CSRF header with every state-changing request"` |
| ORB-SEC-005 restrictive file modes | `TestAccountFileModesAndContent`, `TestStartTightensTooOpenModes`, `TestForeignOwnerIsRefused`, `TestModesAreTightenedAndCreatedRestrictive`, `TestSymlinksAreRefused`, `TestOpenTightensExistingFiles`, `TestCorruptAccountFileRefusesToStart`, `TestMissingOrReadOnlyStateDirectoryRefusesToStart`, `TestMainRefusesToStartWithoutAStateDirectory`, `TestMainRefusesACorruptAccountFile` |
| ORB-SEC-006 administrative actions audited with time, user, plugin, action, target, result, correlation ID | `TestEntryHasTheFixedFields`, `TestAccountEventsAreAudited`, `TestFailedLoginsAndRejectionsAreAggregated`, `TestCorrelationIDAndNoStore` |
| ORB-SEC-007 no secrets in audit and logs | `TestRedactionOfSecrets`, `TestInvalidEventsAreDropped`, `TestFailedWriteIsCountedAndReportedWithoutData`, `TestMainSetupThenSIGTERMFlushesTheAudit`, `TestCorruptAccountFileRefusesToStart` |
| AT-023 secrets not in UI, browser storage, journal or logs | `TestRedactionOfSecrets`, `TestMainSetupThenSIGTERMFlushesTheAudit`, `Auth.test.ts "signs in, keeps the token out of browser storage and the address"`, `session.test.ts "reads the session state and keeps the CSRF token in memory only"` |
| ORB-NFR-009 minimal microSD write load | `TestFlushTiming`, `TestRotationAndCap`, `TestCloseWritesEverything`, `TestSessionSweeperEndsExpiredSessionsAndOnlyRunsWhileSessionsExist`, `TestRestartEndsAllSessions`, `TestCheckWritableDir` |
| ORB-STO-007 own logs rotated and bounded | `TestRotationAndCap`, `TestFailedLoginsAndRejectionsAreAggregated` |
| ORB-NFR-013 atomic writes | `TestWriteAtomic`, `TestAccountFileModesAndContent` |
| ORB-SEC-010, ORB-ONB-003, D-C48 loopback only | `TestValidateListen`, `TestValidateListenMessageNamesTheOpenDesign`, `TestMainRefusesANonLoopbackListenAddress` |
| E1 Argon2id parameters, parser bounds, one hash at a time, memory returned | `TestProductionParametersAreTheADRValues`, `TestParsePHCRejectsMalformedAndOutOfBoundsHashes`, `FuzzParsePHC`, `TestVerifyReportsAnOldParameterSet`, `TestHasherRunsOneHashAtATime`, `TestHasherReturnsMemoryAfterEveryHash`, `TestHashMemoryIsReturnedToTheOS`, `TestArchitectureOnlyArgon2FromXCrypto` |
| E4 password and user name rules | `TestUsernamePolicy`, `TestPasswordPolicy`, `TestCommonPasswordListIsSmallAndWellFormed`, `Auth.test.ts "checks the rules before sending and names the broken one"`, `session.test.ts "counts password length in code points, as the server does"` |
| E6 same answer and work for a wrong user name and a wrong password | `TestLoginFailuresLookTheSame`, `TestDummyHashHasTheCurrentCost`, `Auth.test.ts "gives the same sentence for a wrong user name and a wrong password"` |
| Layering: auth and audit below the server, no program | `TestArchitectureAuthAndAuditAreBelowTheServer`, `TestArchitectureNoProductCodeRunsAProgram`, `TestArchitectureTestHelpersAreOnlyImportedByTests` |

## Amendments to ADR 0009 and open points

The project owner confirmed four deviations of the implementation from ADR 0009. They are recorded as decisions D-C49 to D-C52 in `docs/DECISIONS.md` and as amendments A1 to A4 in the ADR, whose original decisions stay as written. One point is still open (D-D11).

- **Third-party code in the binary (D-C49, ADR 0009 A1).** E1 said that only `argon2` and `x/sys/cpu` are used. The rule is now `golang.org/x/crypto/argon2` and what argon2 itself needs: `golang.org/x/crypto/blake2b` and, on amd64 only, `golang.org/x/sys/cpu`. The check allows exactly that closure.
- **Host check (D-C50, ADR 0009 A2).** E3 names `127.0.0.1`, `[::1]` and `localhost`. The check accepts `localhost`, every literal loopback address (`127.0.0.0/8` and `[::1]`) and the allow-list, which is empty in G5.1. This matches the listen guard (D-C29), and a literal loopback address cannot be rebound.
- **The password change goes through the login limiter (D-C51, ADR 0009 A3).** E6 names login and setup; the current-password check of a password change costs the same hash, so it shares the limiter with the same count, backoff and attempts per minute.
- **Aggregation key (D-C52, ADR 0009 A4).** The one-minute aggregation is kept per kind (action, target, result), so a minute with both wrong passwords and rate-limited attempts gives two entries. The accepted loss on a power failure is up to 30 s for ordinary entries and up to about 90 s for an aggregated one.
- **The state directory mode (open item D-D11, not a decision).** The installer still creates `/var/lib/oarfish` as 0750; `StateDirectoryMode=0700` in the unit is expected to decide the mode at every service start under a real systemd. Whether systemd tightens the existing directory on an update from `v0.1.0-alpha.5` is to be confirmed with `stat` on the device (step 2); whether the installer then aligns to 0700 is decided after that evidence.
