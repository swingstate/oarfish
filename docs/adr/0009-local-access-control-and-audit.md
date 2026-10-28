# ADR 0009: Local access control and audit: Argon2id, in-memory sessions, strict cookies, own audit file, loopback only

- Status: **Accepted** (2026-10-28, accepted by the project owner)
- Date: 2026-10-28
- Gate: G5.1 (design stop; G5.1 itself is not started)

## Context

Gate G5.1 adds the local administrator account and the audit foundation (spec 6.11: ORB-AUTH-001..003, ORB-SEC-004..007, ORB-SEC-010; 6.1: ORB-ONB-002, ORB-ONB-003; 8.7, 8.8; 10.5; ORB-STO-007; ORB-NFR-009; AT-023). The device is a Raspberry Pi Zero 2 W with 512 MB of RAM and a microSD card. The service runs as the unprivileged user `oarfish` under `ProtectSystem=strict`, so `StateDirectory=oarfish` (`/var/lib/oarfish`) is the only place where it can persist anything (`install/oarfish.service`). Until now the API has no authentication and the binary refuses any listen address that is not a literal loopback IP address (D-C25, D-C29, `server.ValidateListen`).

The specification fixes goals, not mechanisms. It does not say how a password is stored, where a session lives, how a first account is created without a console, where the audit goes, or how remote access is secured. This ADR records those decisions. Each numbered decision is confirmed in `docs/DECISIONS.md` (D-C45 to D-C48).

Three kinds of statement appear below and are kept apart:

- **Decisions** are what G5.1 implements.
- **Estimates** are numbers nobody has measured on the device. They are labelled as estimates and are checked by a measurement during G5.1.
- **Design-review assumptions** are claims about third-party behaviour (the OWASP guidance, browser behaviour) that were recalled during the design review and not re-checked while writing this ADR. G5.1 verifies them.

Nothing in this ADR is verified on hardware.

## Decisions

**E1. Password hash: Argon2id, stored as a PHC string.** The hash is Argon2id from `golang.org/x/crypto/argon2`. This is the first third-party Go dependency of the project (`docs/DEVELOPMENT.md` currently says "standard library only"). The module version is pinned in `go.mod` and locked in `go.sum` by the G5.1 change. Only the packages `argon2` and, transitively, `golang.org/x/sys/cpu` are used; `golang.org/x/crypto` is not otherwise imported, and a test or a documented check in G5.1 keeps it that way. `THIRD_PARTY_NOTICES.txt` gets the licence text of the module, since the code ends up in the binary.

The stored value is a PHC string (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`), so parameters can change later without a migration: a login with an old parameter set verifies and may re-hash with the current one.

Start parameters: **m = 19 MiB, t = 2, p = 1.** This is the OWASP minimum for Argon2id as recalled by the design review; the OWASP page was not re-read while writing this ADR (design-review assumption). G5.1 confirms or adjusts the parameters with an ARM64 benchmark on the Raspberry Pi Zero 2 W. The target is **below about 500 ms per login**; the **estimate** is 100 to 400 ms, not measured. If the benchmark is above target, the memory or time parameter is lowered only as far as needed and the decision is recorded in the G5.1 documentation.

At most **one hash computation runs at a time** (a semaphore of size 1). Argon2id at 19 MiB on a device with 512 MB RAM and a 35 MB idle budget must not be started in parallel by an attacker or by a burst of requests; requests that wait are bounded by the rate limit of E6.

Rejected alternative: **PBKDF2 from the standard library.** It needs no dependency, but it is weaker against GPU attacks because it needs almost no memory, and to reach a comparable cost on the Pi it would need an estimated 1 to 2 s per login (estimate, not measured). The dependency is small, reviewed and pinned; that is worth the security gain.

**E2. Sessions: memory only.** A session lives in the process, as spec 8.8 allows. The token is 256 bits from `crypto/rand`. The server stores only the SHA-256 of the token, so a memory dump does not yield a usable cookie. Idle timeout **1 h**, absolute lifetime **12 h**, at most **8** sessions at the same time (the oldest is evicted when a ninth is created). A restart invalidates every session; that is accepted, a login takes seconds and nothing is written to the microSD card (ORB-NFR-009).

Server-Sent Events streams (`/api/v1/events`) do not extend a session: only an ordinary request counts as activity. A stream is closed immediately when its session expires or is logged out, and the UI shows a clear "session expired" state instead of reconnecting in a loop.

**E3. Cookie and CSRF.** The session cookie is `HttpOnly`, `SameSite=Strict`, `Path=/`. It is `Secure` **only when the connection itself is TLS** (`r.TLS != nil`), never derived from `X-Forwarded-Proto` or any other client-controlled header. Reason: the service is reached over plain `http://127.0.0.1` (directly or through an SSH tunnel), and Safari does not reliably accept `Secure` cookies on that origin (design-review assumption). G5.1 verifies the behaviour in Safari, Chrome and Firefox against `http://127.0.0.1`. When TLS exists later (D-D10), the cookie name uses the `__Host-` prefix, which requires `Secure`, `Path=/` and no `Domain`.

CSRF protection has three layers, applied to every state-changing method (anything but `GET`, `HEAD`, `OPTIONS`):

1. **Origin check.** `Origin` must match the request host; where the browser sends `Sec-Fetch-Site`, only `same-origin` is accepted. A state-changing request with neither header is refused.
2. **Per-session token** in the header `X-Oarfish-CSRF`. The server creates it with the session and returns it from login and session-status responses. The UI keeps it in memory only, never in `localStorage`, `sessionStorage` or a cookie readable by script.
3. **`Content-Type: application/json` only**, so a cross-site HTML form cannot produce a valid request.

A **Host header check** protects against DNS rebinding: the host part of `Host` must be a loopback name or address (`127.0.0.1`, `[::1]`, `localhost`) or a name the administrator configured. The port is ignored, because an SSH tunnel may use another local port (INSTALL.md describes `-L 9000:127.0.0.1:8484`) and that must keep working. Note that the listen guard still refuses `localhost` as a *listen* host (D-C29); the Host check concerns what a browser sends, not what the process binds.

**E4. Account storage.** One local administrator: a username and the PHC hash, in `/var/lib/oarfish/auth/account.json`. The directory is `0700`, the file `0600`. A write is atomic: write a temporary file in the same directory, `fsync`, rename over the target, `fsync` the directory. Password rules: **minimum 12 and maximum 128 characters**, no composition rules, optionally a small embedded list of common passwords to refuse; the request body for login, setup and password change is limited to **1 KiB**. A password change requires the current password, invalidates every other session, and is audited.

**E5. First start.** While no account exists, the service creates a **128-bit random setup token** and writes it to `/var/lib/oarfish/setup-token` (`0600`, in practice readable by root and the service user only). It is **never written to the journal** or any log (AT-023, ORB-SEC-007). The token is regenerated on every service start, deleted after a successful setup, compared in constant time, and the setup route has the same rate limit as login (E6). While no account exists, every other endpoint is refused with a typed error that tells the UI to show the setup page.

The installer's next-step text will name the command `sudo cat /var/lib/oarfish/setup-token`. That installer change belongs to G5.1, not to this ADR.

**Lost password:** stop the service, delete `/var/lib/oarfish/auth/account.json` as root, start the service; it is in setup mode again. There is no new command-line interface for it. Deleting the account file does not touch the audit file.

**E6. Rate limiting.** One global limiter, because per-IP limits are useless here: behind an SSH tunnel or a reverse proxy every client appears as loopback. After the **5th consecutive failure** the delay grows exponentially up to **15 minutes**; in addition at most **10 login attempts per minute** are processed overall. There is **no hard account lockout**, because it would let anyone who reaches the port lock the administrator out (denial of service). A wrong user name and a wrong password produce the same response and the same timing class (the hash is computed against a fixed dummy hash when the user does not exist).

**E7. Audit.** An own append-only JSON-lines file, `/var/lib/oarfish/audit/audit.jsonl`, mode `0600`. Rotation at **1 MiB** per file, **4 files**, so the hard cap is **4 MiB**. An entry has fixed, typed fields only: time, user, plugin (empty in the core), action, target, result, correlation ID (spec 6.11, ORB-SEC-006). There are no free-form maps, so a secret cannot be put into an entry by accident.

Failed logins are aggregated: at most one entry per minute with a counter. The file is flushed every **30 s** and on rotation, not on every entry. The accepted trade-off: the last seconds of audit entries may be lost on a power failure.

The server creates a **correlation ID** for each request (64 bits, hexadecimal) and ignores any value from the client. It is returned in the header `X-Correlation-ID` and in typed errors (spec 8.7). Request logs never contain bodies, cookies or query strings. Redaction tests are mandatory (AT-023, ORB-SEC-007).

The write load is an **estimate**: about 250 bytes per entry and below 10 KB per day in normal use. G5.1 measures it on the Pi (`write_bytes` in `/proc/<pid>/io` over 24 hours, as AT-022 does for the dashboard) and records the result.

**E8. The listen guard stays; remote access is a separate design.** G5.1 stays loopback-only. The listen guard of D-C29 and D-C40 is unchanged: the binary refuses any `-listen` that is not a literal loopback IP address with a port. Until HTTPS and LAN access are designed, remote access happens through an SSH tunnel.

This opens item **D-D10**: no gate owns LAN access and HTTPS/TLS. It needs its own design stop before the MVP release, not before G6. Constraints for that design (not decisions): low friction for users, secure by default, no mandatory cloud service or Tailscale, and the authentication of this ADR must keep working unchanged on top of TLS. Candidate directions, listed neutrally and **not chosen here**: a self-signed certificate whose fingerprint the UI shows; a certificate provided by an optional plugin; a reverse proxy configured by the administrator.

**E9. Scope of authentication.** After G5.1 every `/api/v1/*` route requires a valid session, except: login, setup (protected by the setup token), session status, and a minimal health check that exposes no device data. This tightens D-C25, which said the API has no authentication; D-C25 is marked as amended by D-C46.

## Alternatives considered

- **PBKDF2 (standard library):** see E1.
- **bcrypt or scrypt:** bcrypt truncates at 72 bytes and is also outside the standard library; scrypt is in the same module as Argon2 but is not the current first recommendation. Not pursued.
- **Sessions persisted to disk:** survive a restart, but cost microSD writes (ORB-NFR-009) and put session material on disk. A restart is rare and a login is cheap.
- **Signed stateless tokens (JWT-style):** cannot be invalidated server-side, which spec 6.11 (ORB-AUTH-002) requires.
- **Per-IP rate limits:** useless behind an SSH tunnel or proxy, see E6.
- **Hard account lockout:** a denial-of-service lever, see E6.
- **Audit to the journal only:** the unprivileged service cannot read the journal without extra privileges, so the UI could not show the audit; retention and rotation would not be in the project's hands.
- **Audit to the journal and to a file:** doubles the write load on a microSD card for no gain.
- **Setup token printed to the journal:** the journal is readable by more than the owner and may end up in diagnostic exports; a secret there violates AT-023 and ORB-SEC-007.
- **Setup by whoever connects first:** on a device that later gets LAN access, the first visitor would own it. The token proves access to the device.
- **Deriving `Secure` from `X-Forwarded-Proto`:** a client-controlled header must not decide a security attribute.

## Consequences

- **Existing devices.** After an update from `v0.1.0-alpha.5` (or any release before G5.1) a device has no account and starts in setup mode. Dashboard data is visible only after setup and login. This is a visible behaviour change and the release notes of the G5.1 release must say so.
- **Dependency.** `golang.org/x/crypto/argon2` and `golang.org/x/sys/cpu` become the first Go modules. The G5.1 change updates `docs/DEVELOPMENT.md`, `THIRD_PARTY_NOTICES.txt` and the CI vulnerability check, and justifies the dependency there (as DEVELOPMENT.md requires).
- **Path correction (D-C47).** Spec 8.2 places secrets under `/etc/oarfish/secrets`. The service cannot write below `/etc` (`ProtectSystem=strict`), so everything the service writes lives under `/var/lib/oarfish`: `auth/account.json`, `setup-token`, `audit/audit.jsonl`. `/etc/oarfish/secrets/` stays for operator-provided, read-only secrets. Spec 8.2 is corrected in version 0.3.4.
- **Audit location versus spec 8.1.** Spec 8.1 allows "journald oder append-only Datei" for audit; this ADR chooses the file. The journald row of spec 8.2 still describes runtime logs and is not changed.
- **G5.1 scope change.** The G5.1 row of spec 14.2 lists "explizite Konfiguration für nicht-lokale Bindung (ORB-SEC-010)" as part of its delivery. With E8 and D-C48, G5.1 delivers no non-loopback listening. ORB-ONB-003 (default loopback) and ORB-SEC-010 (no automatic public port) are met by staying on loopback; the explicit-configuration part moves to D-D10. The wording of spec 14.2 is not changed by this ADR's pull request (see the notes of D-C48).
- **Exit criteria of G5.1 stay as in spec 14.2:** state-changing endpoints reject requests without a valid session and without CSRF protection; audit entries contain no secrets; login verified on the Raspberry Pi. G5.1 adds two measurements: the Argon2id benchmark on the Pi (E1) and the audit write load over 24 hours (E7).
- **The audit file can lose the last seconds** on a power failure (E7).
- **Loss of the setup token file** is harmless: the service creates a new token at the next start.
- **Not decided here:** HTTPS and LAN access (D-D10), multiple users and roles, two-factor authentication, password reset by e-mail, the privilege broker (G7), the update helper (G6).

## Forward compatibility with HTTPS

Nothing in this ADR depends on plain HTTP except the cookie attributes, and those already switch on `r.TLS != nil`. When TLS is added: the cookie becomes `Secure` and takes the `__Host-` name prefix; the Host check gains the configured name; `Sec-Fetch-Site` and `Origin` checks, the CSRF header, the session store, the rate limiter and the audit are unchanged. Because the service does not derive `Secure` from forwarded headers, a TLS-terminating reverse proxy in front of a loopback listener would produce cookies without `Secure`; D-D10 must decide how that case is handled (for example by terminating TLS in Oarfish itself). That decision is not made here.
