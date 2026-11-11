# ADR 0010: Update helper: a socket-activated root process with a closed list of typed requests, helper-owned verification, a persistent state machine and a guarded replacement of its own components

- Status: **Accepted** (2026-11-02, accepted by the project owner in chat after the review of the Proposed draft of PR #19; revised by the owner decisions Q1 to Q7 and the review findings R1 to R6 before acceptance)
- Date: proposed 2026-11-02 (commit `c3f13e8126a052fdf1a8bf5dd77ffbd13b91a2ec`), revised and accepted 2026-11-02
- Gate: G6, step G6.0 (design stop; G6.1 and later are not started)
- Written against: `main` at commit `1cc1e022d769725284d0a7b162d14f73d75c5e3e` (merge of PR #18)
- Recorded in `docs/DECISIONS.md` as D-C53 to D-C63 and the open decision D-D12; amends ADR 0009 by amendment A6; aligns the specification in version 0.3.5
- Amended after the G6.1 device spike (applied 2026-11-03, `docs/DECISIONS.md` D-C64): amendments A1 to A8 and one residual-risk sentence in T1, listed in "Amendments after the G6.1 spike" below; the specification follows in version 0.3.6. The status above and the decisions D-C53 to D-C63 are unchanged.
- Amended after the G6.3a stub (applied 2026-11-03, `docs/DECISIONS.md` D-C66): amendment A9, listed in "Amendment after G6.3a" below. It changes the recover unit text, one sentence about the socket, U11 item 4 and the T14 row; the specification is not changed.
- Amended after G6.5 (applied 2026-11-05, `docs/DECISIONS.md` D-C67): amendment A10, listed in "Amendment after G6.5" below. It corrects the assumption V5 about `O_NOFOLLOW` through `os.Root` and records the limits of `os.Root`, the `previous_version` member of the state and the one mutation seam; the specification is not changed.
- Amended after G6.6 (applied 2026-11-06, `docs/DECISIONS.md` D-C68): amendment A11, listed in "Amendment after G6.6" below. It adds the update notice route, the `pid` member of `/healthz` and `/readyz`, the call timeouts and the job model of the service, and records a known property of the state write; the specification is not changed.
- Amendment A12 (G6.8, written 2026-11-07, for the owner's review of the update page): facts of the implementation of the update page, listed in "Amendment A12 (G6.8)" below. It records what the page does, the polling rules, the fix C2 and what is not verified. It makes no new decision; the specification is not changed.
- Amendment A13 (task HB, written 2026-11-07, `docs/DECISIONS.md` D-C80): the bytes of the helper do not depend on the release version, listed in "Amendment A13 (task HB)" below. It records the finding, the fix and its consequence for devices on `v0.1.0-alpha.7`, and adds pointers in U5, U11 and U15; the specification is not changed.
- Amendment A14 (G6.9 follow-up, applied 2026-11-10, `docs/DECISIONS.md` D-C81): the boot order of the recover unit, listed in "Amendment A14 (G6.9 follow-up)" below. It changes the recover unit text in U10 and the sentence about the recover unit in U1, and closes the note "not covered by G6.1" about its order at boot; the specification is not changed.

- Amendment A15 (G6.10 step a, written 2026-11-10, `docs/DECISIONS.md` D-C83): the owner decisions D1 to D7 for the replacement of the helper and its units, and the unit lint of U15 rule 3 as implemented, listed in "Amendment A15 (G6.10, step a)" below. It adds pointers in U4 and U15 rule 3 and refines the sentences its change table names; the specification is not changed.

- Amendment A16 (G6.10 step b, written 2026-11-11, `docs/DECISIONS.md` D-C84): the owner decisions E1 to E4 and the component transaction of U15 rules 1, 2, 4, 5 and 6 as implemented, listed in "Amendment A16 (G6.10, step b)" below. It edits no accepted sentence in place; its change table names the rule that now applies; the specification is not changed.

## How to read this ADR

Every statement carries one of these labels, so that a reader can tell what is known from what is hoped:

- **Fact** names the file it comes from. It was read while writing this ADR.
- **Assumption** is a claim about third-party behaviour or about the device that was recalled, not re-checked, and not measured. Each assumption is listed again under "Assumptions to verify" and is checked by a named step of the G6 plan.
- **Owner decision** is a decision the project owner made in chat on 2026-11-02 (Q1 to Q7 below). It is binding.
- **Device observation (G6.1)** is a result of the G6.1 device spike, applied by the amendments below. It is a reading of throwaway units on one device, one image and one moment, not the production path: *device observation, throwaway units, one device, one image, one moment, 2026-11-02*. It is never called verified, and it does not change the support status of the reference device (Compatible).
- **Proposal** was a proposal of the draft. **Since the acceptance of this ADR a rule labelled Proposal is decided design that is not yet implemented.** It is binding as design, it is not evidence that anything works, and a step of the plan may change it only through the stop point of that step. Numbers marked *estimate* are never measurements.

Rows of tables and of the threat model are proposals in this sense unless a cell says Fact, Assumption or Owner decision. Statements about general Linux or systemd behaviour that are not tied to a file of this repository are assumptions, even where they are not marked again.

Nothing in this ADR is verified on hardware as the production path. Statements labelled Device observation (G6.1) rest on throwaway units on one device (amendments A1 to A8); an assumption that G6.1 checked keeps its label in the text, and its verdict is in the table "Assumptions to verify". The design stop writes no code, no unit file, no installer change and no workflow change. The unit text in U10 is documentation, not a file that is installed.

## Context

Gate G6 adds the self-update: the GUI detects a newer release on GitHub, the administrator confirms, the release is downloaded, verified, installed beside the running one, activated atomically, health-checked and, on failure, rolled back (spec 6.12: ORB-UPD-001..008, 9.5, 13.4). The specification says that a separate, privileged Update Helper does the privileged part and that it may do nothing else (spec 8.6, ORB-UPD-007). It does not say how that process is started, how the unprivileged service talks to it, who downloads what, where trust starts, what happens at a power failure, how the result is audited, or how the helper itself is updated. This ADR records the answers.

Facts that shape the design:

- **Fact** (`install/oarfish.service`): the service runs as the unprivileged user `oarfish` with `NoNewPrivileges=yes`, an empty `CapabilityBoundingSet`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, `SystemCallFilter=@system-service`, `Restart=on-failure`, `RestartSec=2`, `TimeoutStopSec=15`, `UMask=0077`. Its only writable places are `/var/lib/oarfish` (mode 0700), `/var/cache/oarfish` and `/run/oarfish`.
- **Fact** (`install/install.sh`): the installer verifies `checksums.txt.sig` with the system `openssl`, checks the SHA-256 of manifest and bundle against the signed `checksums.txt`, inspects the tarball entries (exactly five, regular files only), checks the ELF header, installs into `/opt/oarfish/releases/<version>` through a `.stage.XXXXXX` directory and `mv`, **copies** `oarfish.service` to `/etc/systemd/system/oarfish.service` (`install_unit`, a regular file), switches `/opt/oarfish/current` (a relative symlink) with `mv -T`, restarts the service, polls `/healthz`, and rolls back with `reset-failed` plus `restart` when the new version is not healthy. It requires 64 MiB free (`MIN_FREE_KB=65536`). Its downloads use `--max-filesize 100000000`, `--retry 3`, `--connect-timeout 20`, HTTPS only.
- **Fact** (`docs/RELEASING.md`, `docs/adr/0005-release-signing.md`): the signature is a minisign legacy-mode (`Ed`) Ed25519 signature over the bytes of `checksums.txt`; `checksums.txt` lists the SHA-256 of bundle, `install.sh`, `manifest.json` and the SBOMs. The manifest has `schema_version`, `name`, `version`, `tag`, `prerelease`, `commit`, `build_date`, `os`, `architecture`, `requirements` (`os`, `architecture`, `cpu`, `libc`, `runtime_dependencies`), `bundle` (`file`, `sha256`, `size`) and `contents` (path, mode, size, SHA-256 of every file). Kernel and memory minimums are not in the manifest. The public key is `release/oarfish-release.pub`; the updater is expected to embed it with `go:embed`. Key rotation and revocation are not designed. A tag with a hyphen is a prerelease.
- **Fact** (`docs/adr/0006-installer-verification.md`): the root of trust on a device is `install.sh` itself, which the administrator downloads over HTTPS, can read, and then runs as root; the public key is a constant inside it.
- **Fact** (`backend/internal/server/server.go`, `docs/AUTH.md`, `docs/ARCHITECTURE.md`): `/healthz` answers `{"status":"ok"}` without a session and without device data; `/readyz` is specified (spec 13.1) and does not exist.
- **Fact** (`docs/DECISIONS.md` D-C47, `docs/AUTH.md`): the service writes its account, setup token and audit file below `/var/lib/oarfish`; sessions live in memory; a restart signs everybody out.
- **Fact** (`backend/internal/audit/audit.go`, ADR 0009 E7): audit entries have fixed typed fields; action, target and result are closed sets or patterns; failed logins are aggregated; the file is flushed every 30 s.
- **Fact** (`docs/AUTH.md`, ADR 0009 A5): on the reference device one Argon2id verification took 442.9 ms and, with `FreeOSMemory`, the process retained 0 MiB afterwards (`VmHWM` 25020 kB). One device, one reading.
- **Fact** (`docs/DECISIONS.md` D-C13, D-C28, spec 6.12 ORB-UPD-008): releases are pulled anonymously over HTTPS, no token, no Tailscale; the Update Helper belongs to G6, the Privilege Broker (ORB-SEC-002) to G7, its capability check to G8.
- **Fact** (local development build of 2026-11-01, not a repository file and not a release): the manifest of that build shows a bundle of 3,008,386 bytes with a 7,274,656 byte binary. Treat both as an *estimate* of release size; the budget is 35 MB (ORB-NFR-005).

### Owner decisions of 2026-11-02

These were made in chat after the review of the Proposed draft. They are recorded in `docs/DECISIONS.md` (D-C54 to D-C60) and shape the text below.

| # | Owner decision |
|---|---|
| Q1 | **Helper replacement.** The helper and its units can be replaced from the GUI as part of a GUI update, with their own explicit confirmation. This replaces the draft's "installer only". The one-time bootstrap from a version without a helper (for example `v0.1.0-alpha.6`) stays an installer run. The replacement is implemented in the separate step G6.10, not in G6.1 to G6.9. Until G6.10 is accepted, a release that changes the helper or any unit needs an installer run, and the GUI shows exact instructions instead of a dead end. |
| Q2 | **Unit changes from the GUI** are allowed, for allow-listed unit files only (U15). The draft's error code `unit_changed` is gone; its replacement is the transitional state `installer_required` of Q1. |
| Q3 | **Fresh password for `apply`:** yes, through the shared limiter (ADR 0009 E6, A3). |
| Q4 | **Fault evidence.** A test build of the helper with a throwaway key and fault hooks is accepted for the failure rows (class T). The success path (F19) and at least one genuine signed-release path must still be shown with the production key (class D). Class T is never presented as evidence about the production key path. |
| Q5 | **`/healthz` may report the Oarfish version:** yes. |
| Q6 | **Update state in a root-owned `/var/lib/oarfish-update`:** yes. |
| Q7 | **Prerelease rule:** a device on a prerelease sees prereleases and stable releases, a device on a stable release sees stable releases only: yes. |
| (key) | **Key rotation is not decided here.** A change of the embedded trust key through a GUI update is refused until the owner decides key rotation (U15 rule 5, open decision D-D12). |

## Decisions

### U1. Process model and trust boundary

- **Proposal: the helper is a separate systemd unit that runs as root, is started by socket activation, serves requests, and exits after 30 s without a connection and without a running transaction.** One binary, `/usr/libexec/oarfish-update-helper` (the path of spec 8.2), with exactly two subcommands: `serve` (the socket-activated service) and `recover` (recovery, U7). A second unit, `oarfish-update-recover.service` (`Type=oneshot`, `TimeoutStartSec=120` (A9), no `RemainAfterExit`, `DefaultDependencies=no` and `After=local-fs.target` (A14), `Before=oarfish-update-helper.socket oarfish.service`, `WantedBy=multi-user.target`), runs `recover` at every boot before the socket and the main service start, and is also started by `OnFailure=` of the helper service (U15 rule 4). It is not `Requires=` of anything, so a failing recovery never keeps Oarfish from starting; since A9 a hanging recovery cannot either, because `TimeoutStartSec=120` ends it (a oneshot unit has no start timeout by default).
- **Proposal: the helper has no ordering or dependency relation to `oarfish.service`** (no `Requires=`, `BindsTo=`, `PartOf=`). A restart of the main service therefore never stops the helper, and the helper keeps its transaction running while the service restarts. The helper lives in its own control group, so `systemctl restart oarfish.service` cannot kill it.
- **Proposal: the unprivileged service stays unprivileged** (ORB-SEC-001). It gets no capability, no `ReadWritePaths=` for `/opt/oarfish`, no sudo rule, no polkit rule and no setuid helper. The only new thing it may do is connect to one Unix socket.
- **Proposal: the helper is a separate binary built from the same Go module, standard library only.** It does not contain the HTTP server, the frontend, Argon2 or any third-party module. Reason: everything compiled into a root process is attack surface. The shared verification code (U4) is a small package in the same module that both binaries import.
- **Proposal: the helper binary and its three units are not part of a release tree.** They live in a separate component tree that `current` does not control (U15 rule 3), so that a broken release can never make recovery impossible. The path `/usr/libexec/oarfish-update-helper` is a symbolic link into that tree, created by the installer.
- **Assumption:** a socket-activated service with `Type=exec` and no resident process costs nothing while idle; a Go static binary of this size has an idle RSS of roughly 3 to 6 MB (*estimate*, U12).

Alternatives considered:

- *Always-running root daemon:* simplest to reason about, but 3 to 6 MB resident for a function used a few times a year (*estimate*), on a device with a 35 MB idle budget for the core (ORB-NFR-001), and root code listens on a socket all the time. Rejected.
- *On-demand oneshot unit started by the service:* the unprivileged service cannot start a system unit without a sudoers or polkit rule. Both are extra privileged configuration surfaces outside the helper's own code and review. Passing arguments through unit instance names or files is awkward and loses peer authentication. Rejected.
- *Setuid helper executed by the service:* `NoNewPrivileges=yes` in the service unit forbids it, and weakening that is not acceptable (`docs/ARCHITECTURE.md`, hardening paragraph). Rejected.
- *systemd `.path` unit watching a request file the service writes:* no socket code, but there is no peer authentication, no synchronous error, and the request is a file the unprivileged side can change while it is read. Kept only as a documented fallback if socket activation turns out not to work on the target (verification V2).
- *D-Bus service:* needs bus policy files and a D-Bus library or a hand-written wire protocol. More code and more configuration for no gain. Rejected.

### U2. Interface between the service and the helper, and the difference to the Privilege Broker

- **Proposal: transport is an `AF_UNIX` stream socket at `/run/oarfish-update/helper.sock`.** The socket unit creates it (`SocketUser=root`, `SocketGroup=oarfish`, `SocketMode=0660`, `DirectoryMode=0755`, `Accept=no`). The directory is owned by root, so the service cannot replace or delete the socket. (`/run/oarfish` is not used because the service owns it.)
- **Proposal: peer authentication has two layers.** The file mode lets only root and the group `oarfish` connect. In addition the helper reads `SO_PEERCRED` of every connection (`syscall.GetsockoptUcred`, standard library) and requires the peer uid to equal the uid of the user `oarfish`, looked up once at start. A different uid is closed without an answer. PID-based checks are not used: reading `/proc/<pid>/exe` of another user's process needs a capability and PIDs are reused (**Assumption**, Linux semantics).
- **Proposal: one request per connection, newline-terminated JSON, at most 4 KiB, 5 s to send it.** The decoder rejects unknown fields (`DisallowUnknownFields`), trailing data and a protocol version it does not implement (U15 rule 6). Responses are bounded as well (at most 8 KiB). At most 4 connections are served; the others wait in the socket backlog.
- **Proposal (A2): a client retries once.** The service must retry a request once when the connection fails with `ECONNRESET` or `ENOENT`. **Device observation (G6.1, V13):** when the socket unit is restarted, a connection that waits in the backlog or is made during the restart gets `ECONNRESET` or `ENOENT`, depending on the instant; a connection that was already accepted is not affected. The helper does not restart its own socket (U15 rule 4 step 6); a restart comes from an installer run or from an administrator.
- **Proposal: the list of requests is closed and compiled in:**

  | `op` | Arguments | Effect |
  |---|---|---|
  | `status` | none | Read-only: protocol range and helper version, embedded key id, installed version, previous healthy version, component state, transaction phase, last result, free space of the release file system. |
  | `stage` | `version`, `correlation_id` | Copy the four downloaded files from the cache into the helper's private area, verify them (U4), extract into a staging directory, check the units against the compiled-in rules (U15), move the release into `releases/<version>` (inactive). Reports whether the helper or a unit differs from the installed one. |
  | `activate` | `version`, `correlation_id`, `components_confirmed` | Run the transaction of U5/U6, and of U15 when components differ: switch, restart, check health, commit or roll back. Returns at once with "accepted"; the transaction continues without the caller. `components_confirmed` must be `true` when `stage` reported a component difference (else `confirmation_required`). It is a protocol interlock that makes the second confirmation explicit and testable; it is not a security boundary, because the helper cannot see the click. |
  | `discard` | `version` | Remove a staged but not activated release and its staging files. |
  | `ack` | `transaction_id` | Mark the last result as seen by the service (U8). |

  Arguments are typed: `version` must match `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$` (the installer's `valid_version`, **Fact**: `install/install.sh`), at most 64 characters and no `..`; `correlation_id` is 16 hexadecimal digits; `transaction_id` is 16 hexadecimal digits made by the helper; `components_confirmed` is a boolean. **There is no argument that is a path, a URL, a command line, an environment variable, a unit name, a program or a package.** The helper builds every path from fixed templates and the validated version.
- **Proposal: errors are typed with a closed set of codes** with fixed texts: `bad_request`, `busy`, `not_found`, `signature_invalid`, `checksum_mismatch`, `manifest_invalid`, `downgrade_refused`, `already_installed`, `requirements_not_met`, `helper_too_old`, `installer_required`, `confirmation_required`, `key_changed`, `unit_rejected`, `components_damaged`, `stage_expired`, `no_space`, `current_unhealthy`, `cooldown`, `io_error`, `internal`. An error never echoes the request, a path or an operating-system error string. Outcomes of a finished transaction are the closed set `success`, `rolled_back`, `components_rolled_back`, `rollback_failed`, `aborted`.
- **Proposal: the fixed lists.** The following lists are compiled into the helper. They are the closed lists; this ADR is the amendment that opens the first list of paths and units to the component replacement of U15, and a test compares every list with this table and fails on any difference.

  | List | Content |
  |---|---|
  | Requests | `status`, `stage`, `activate`, `discard`, `ack` |
  | Programs executed | `/usr/bin/systemctl` only (absolute path, cleared environment, no shell), with exactly these argument shapes: `restart oarfish.service`; `reset-failed oarfish.service`; `show oarfish.service --property=ActiveState,SubState,MainPID,NRestarts,ExecStart --no-pager`; `daemon-reload`; `reset-failed oarfish-update-helper.service` (A2: the shape `restart oarfish-update-helper.socket` of the accepted text is removed) |
  | Unit names | `oarfish.service`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service` |
  | Replaceable component files | `oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service` (the first is a program, the others are units); `oarfish.service` travels with the release (U15 rule 2) |
  | Write targets (and nothing else) | below `/opt/oarfish`: `releases/<version>`, `releases/.stage.<id>`, `releases/.trash.<id>`, `current`, `.current.new.<id>`, `components/<id>`, `components/active`, `components/known-good` and their `.new.<id>` temporaries; `/var/lib/oarfish-update/` (state file, lock, temporaries) |

  No argument selects an element of any list except `version`, which selects only the name of a directory below `releases/`.
- **How this differs from the Privilege Broker (G7).** The broker (spec 8.6, ORB-SEC-002, ORB-SEC-003) is a general mechanism: an allowlist of operations such as `service.restart(unit)`, with arguments that name things on the system, and a capability check per caller (G8). The update helper is not general: it has one subject (Oarfish releases and its own components below `/opt/oarfish`), four units it may name in code, no capability model and no plugin-facing API. **The helper never becomes a broker.** Rules: (1) a new request type, program, argument shape, unit name or write target needs an amendment of this ADR; (2) the test above; (3) the helper has no operation whose argument names a unit, a path, a package or a program; (4) every privileged operation outside Oarfish's own release and component lifecycle goes to the broker, which is a separate process with its own socket, unit and code; (5) the two processes share no request-handling code. A future plugin runtime (G8) must keep plugin processes away from the helper socket (for example `InaccessiblePaths=/run/oarfish-update` and a different uid), because the socket accepts every process of the user `oarfish`.
- **Consequence for a compromised service:** the helper cannot tell whether the administrator clicked. See U9 and the threat model.

Alternatives considered: *HTTP over a loopback port* (needs its own authentication, is reachable by every local user, and mixes with the public API), *named pipes or files* (no peer identity), *passing the request on the command line of a started process* (needs a way to start it, see U1), *a generic RPC with `exec(cmd, args)`* (forbidden by the project rules and ORB-UPD-007), *a separate request type for component replacement* (a second path to the same files; the transaction and its write-ahead state would be duplicated), *a `components` request that takes file names* (an argument that names a file; rejected by rule 3).

### U3. Who downloads what, and how the files reach a place the helper trusts

- **Proposal: the unprivileged service does all network traffic; the helper has no network access beyond loopback for the health check** (U6, U10). A root process that parses TLS and HTTP is a larger risk than the same code running as `oarfish` under the existing sandbox.
- **Proposal: discovery.** The service asks the GitHub REST API for the release list once per user-initiated check, with an `If-None-Match` ETag kept in memory, and reads only `tag_name`, `prerelease`, `draft` and the release notes `body`. It does **not** use `browser_download_url` or any other URL from the response. It builds the download URLs itself from the strictly validated tag: `https://github.com/<repo>/releases/download/<tag>/<file>` for `checksums.txt`, `checksums.txt.sig`, `manifest.json` and `oarfish_<version>_linux_arm64.tar.gz` (the same scheme as the installer; `<repo>` is the constant the installer already carries). Reason: the API response is unsigned data.
- **Proposal: bounds on every request.** HTTPS only, TLS 1.2 or newer, system root certificates, no proxy from the environment, at most 3 redirects, each redirect must stay on `https://` and on an allow-listed host (`github.com` and `*.githubusercontent.com`), and the response host is never logged beyond a code. **Device observation (G6.1, V3, A3):** a release asset is served by `github.com` (a redirect) and then by `release-assets.githubusercontent.com`; this allow-list covers both hosts. Connect 15 s, TLS handshake 15 s, response headers 20 s. API response at most 1 MiB; `checksums.txt`, `checksums.txt.sig` and `manifest.json` at most 64 KiB each, 30 s each. The bundle: at most the size announced by the (advisory-verified) manifest and never more than 40,000,000 bytes (ORB-NFR-005 is 35 MB), a stall timeout of 30 s without bytes and an absolute limit of 15 min. The body is streamed to a `.part` file with a 32 KiB buffer, never held in memory. An interrupted download is discarded; there is no resume (**Assumption:** range requests on the release CDN are not relied on). One automatic retry after 5 s, then the GUI shows a typed error and the administrator can try again (R-05, ORB-NFR-014).
- **Proposal: rate limit and interval.** No background polling and no timer (ORB-UPD-002: no automatic upgrade; spec 10.5: no hidden traffic). The service checks when the administrator opens the update page, at most once per 60 minutes (the result is cached in memory, nothing is written), and on the button "Check now", at most once per 60 s device-wide. On `403` or `429`, or `X-RateLimit-Remaining: 0`, the service waits until `Retry-After` or `X-RateLimit-Reset` and shows the reason; it never loops. The request carries the user agent `oarfish/<version>` and nothing else that identifies the device. **Device observation (G6.1, V3, A3):** the anonymous limit is 60 API requests per hour and per source address (observed `x-ratelimit-limit: 60`); an answer `304` to a matching ETag **counts** against that limit like any other request, so the ETag saves bandwidth, not quota; a request to `/rate_limit` is free. **Assumption:** the limit is shared by every device behind the same address (recalled from the GitHub documentation, not tested).
- **Proposal: the cache.** The service writes into `/var/cache/oarfish/update/<version>/` (directory 0755, files 0644 after an explicit `chmod`, because the unit's `UMask=0077` would otherwise make them unreadable for the helper without a capability). **Device observation (G6.1, A7):** the installer creates `/var/cache/oarfish` with mode 0750 and the unit's `CacheDirectory=` makes it 0755 when the service starts (observed `755`); the helper, as uid 0 with an empty capability set, can read a 0644 file in the cache and cannot read a 0600 file (`EACCES`), so the cache modes above are needed and are enough. The files are public release artifacts, so the mode is not a confidentiality problem; ORB-SEC-005 concerns configuration and secret files. The cache is "recoverable" (spec 8.2): the service deletes `.part` files and stale directories at every start and after a successful `stage`.
- **Proposal: the service also verifies, but only for display and to fail early.** It checks the signature (embedded key), the hashes and the manifest of the small files before it downloads the bundle, so the update page can show "signature checked" (spec 9.5) and a bad release costs no bundle download. **The helper never trusts this result.** The two binaries import the same verification package.
- **Proposal: closing the time-of-check to time-of-use gap.** The request `stage` names only a version. The helper then:
  1. opens `/var/cache/oarfish/update` as an `os.Root` (confined traversal, no symlink can lead out; **Assumption:** `os.OpenRoot` exists in the Go version in `backend/go.mod`, recalled from the Go 1.24 release notes), and opens each of the four expected file names **with `O_NONBLOCK|O_NOFOLLOW|O_CLOEXEC`**, so that opening a FIFO or a symbolic link cannot block or lead elsewhere; it then requires a regular file with `fstat` (a FIFO, directory, socket or device is `not_found`) and a size within the caps above;
  2. copies the bytes into its own private directory `/opt/oarfish/releases/.stage.<id>/` (root-owned, mode 0700, same file system as the final location), computing SHA-256 over **the bytes it writes**, with a limit of cap plus one byte;
  3. verifies the signature of its private copy of `checksums.txt` first, then compares the hashes it computed with the signed list, and from then on reads only its private copies. Whatever the service does to the cache after the copy cannot change what was verified. A change during the copy changes the computed hash and fails the comparison.
  The whole `stage` request has a deadline of 180 s (measured with a monotonic clock, checked between chunks); exceeding it removes the staging directory and answers `io_error`. A read on a regular file can only be slow because of the card, not because of the service. **Tests** (G6.5): a FIFO, a symbolic link, a directory and a socket in place of each of the four files, a file that grows during the copy, and a file that is replaced during the copy; every one must end in a typed error, no hang, and nothing left behind.
- **Cost (*estimate*):** the bundle is written twice (cache, then private copy), about 3 MB more per update for a release the size of the local build, up to 40 MB at the budget limit. The private copy is deleted after extraction.

Alternatives considered:

- *The service streams the bundle into the helper over the socket:* one write less, but the helper must stay alive during a download that can take minutes, the protocol needs a binary streaming phase, and a slow client holds a root process. Rejected for G6; possible later.
- *File descriptor passing with a sealed `memfd`:* no SD write for the handoff, but the whole bundle must sit in RAM (up to 40 MB on a 512 MB device with an 80 MB peak budget, ORB-NFR-002), and the code is subtle. Rejected.
- *The helper downloads:* puts the network stack in root. Rejected.
- *The helper verifies in place in the cache directory:* the service owns that directory and can change a file after the check. This is exactly the problem to avoid. Rejected.
- *Make the cache read-only for the service after download (`chattr +i`, mount tricks):* needs privileges the service must not have, or a root helper just to protect the helper. Rejected.
- *Open the cache files without `O_NONBLOCK` and rely on `fstat` afterwards:* opening a FIFO for reading blocks until a writer appears, so the check would never run. Rejected (R3).
- *The GitHub `latest` release endpoint:* it ignores prereleases (**Device observation**, G6.1, V3: the API answered `404` while every release was a prerelease; was an assumption), and every alpha tag is a prerelease (**Fact**, `docs/RELEASING.md`).

### U4. Verification: key, manifest, architecture, downgrade, prereleases

- **Proposal: the helper verifies everything itself, in this order, on its private copies:** (1) `checksums.txt.sig` against the embedded key; (2) `checksums.txt` lists the expected bundle name and `manifest.json` exactly once each, with 64 hexadecimal digits; (3) SHA-256 of the manifest and bundle copies equal the listed values; (4) the manifest is decoded strictly for the fields below; (5) `manifest.bundle.sha256` equals the listed bundle hash and `manifest.bundle.size` equals the file size; (6) version, architecture, key and protocol rules; (7) the tarball is inspected and extracted (U5); (8) every extracted file matches `contents` in size, mode and SHA-256; (9) the units are linted (U15 rule 3). Step 9 is implemented in `stage` since G6.10 step a (A15.3).
- **Proposal: trusted key.** The helper embeds `release/oarfish-release.pub` with `go:embed` and reads no key file at run time; an on-disk key would be a mutable trust root. The same file is embedded in the service for the advisory check. `go:embed` cannot reach outside the Go module, so the implementation copies the file into the module at build time and a CI step, like `scripts/check-installer-key.sh` for the installer, requires the copy to equal `release/oarfish-release.pub` (**Fact**: `docs/RELEASING.md` describes the installer check). The key is a list of one entry in G6; rotation is not designed here (D-D12).
- **Proposal: signature format exactly as ADR 0005 and ADR 0006.** Line 2 of the signature file base64-decodes to 74 bytes: algorithm `Ed`, the 8-byte key id of the embedded key, a 64-byte Ed25519 signature over the exact bytes of `checksums.txt`, checked with `crypto/ed25519`. Algorithm `ED` (prehashed) is refused, as `scripts/verify-release.sh` and the installer refuse it. The trusted comment (lines 3 and 4) is ignored, like the installer ignores it. The standard library is enough, so **no third-party module is added to the helper**.
- **Proposal: manifest fields used** (ORB-REL-002): `schema_version` must be 1; `name` must be `oarfish`; `version` must equal the requested version, the tag `v<version>` and the version in the bundle file name; `tag`; `prerelease`, which must agree with the presence of a hyphen in `version`; `commit` (40 lowercase hexadecimal digits, shown in the GUI and the audit); `os` = `linux`; `architecture` = `arm64` and equal to the architecture the helper was built for (`runtime.GOARCH`); `requirements.os`, `requirements.architecture` (same checks), `requirements.libc` and `runtime_dependencies` (shown, not enforced); `bundle`; `contents`. Unknown fields are ignored so that additive changes stay compatible; unknown or missing known fields fail. **Fact** (`docs/RELEASING.md`): ORB-REL-002 is met "for architecture, OS and libc only"; kernel and memory minimums are not in the manifest. **Proposal:** three additive, optional fields, added by the release tooling in plan step G6.2: `requirements.min_installed_version` (a version; the helper refuses an update from anything older, which gives spec 9.5 step 2 "Mindestversion" a meaning); `requirements.helper_protocol` (an integer: the lowest helper protocol the release's service needs; the helper refuses a release that needs a newer protocol than the helper it will have after the transaction, `helper_too_old`); and the object `components` with `key_id` (the 16-digit key id embedded in the helper the bundle delivers) and `protocol` (the highest protocol that helper implements). The `components` object is present only when the bundle delivers helper files. Kernel and memory minimums stay out until they are measured.
- **Proposal: downgrade and replay protection is "strictly newer than the installed version".** The helper compares the candidate with the version in the target of `/opt/oarfish/current` (written only by root) using SemVer 2.0 precedence, implemented in about 40 lines of standard library code (numeric identifiers compare as numbers, a release is newer than its own prereleases, no build metadata). Equal or older is refused (`already_installed` or `downgrade_refused`). Reason: the signed content has neither an expiry nor a counter, so a validly signed old release can be replayed by anyone who can serve files; the installed version is a counter the attacker cannot reset. Rollback to the previous healthy version (U6) is not an update and is allowed. Spec 9.6 allows a downgrade only through "einen klaren Recovery-Workflow": that is the installer with `--version` run by an administrator with a shell, unchanged. A freeze attack (hiding new releases) cannot be detected; that is accepted.
- **Proposal: mix-and-match is closed by construction.** A signed `checksums.txt` of version A with a bundle of version B fails step 3, because the listed bundle name contains the version and the hash is that of A's bundle.
- **Owner decision (Q7): prereleases.** The release list is not filtered by GitHub's flag, which is unsigned. A device whose installed version has a prerelease part offers the highest eligible version among prereleases and stable releases; a device on a stable version offers stable releases only, until an explicit beta setting exists (spec 9.6, aligned in version 0.3.5). The helper enforces the manifest's own `prerelease` field and the hyphen rule, not the API flag.
- **Proposal: what must be decided about key rotation before the MVP release, not designed here (open decision D-D12).** (1) Whether a device may trust more than one key and how a second key is introduced; (2) how a device that only knows the old key obtains a new key, including the case of a lost private key (ADR 0005: "devices that embed only this public key cannot accept new releases without a new installer"); (3) how a compromised key is revoked when signed content carries no expiry; (4) how the embedded key of the helper changes, given that U15 rule 5 refuses a key change through a GUI update; (5) where the private key lives and who can use it (today an unencrypted GitHub Actions secret, ADR 0005 decision 3); (6) what happens to the alpha key (the replacement of the key used for the alpha releases). Until then a device that loses the key trust needs the installer.

Alternatives considered: *`minisign` or `openssl` on the device* (the installer's choice for a shell script, ADR 0006; the helper is Go and has `crypto/ed25519`), *a third-party minisign or TUF library* (a large dependency in a root process for a 74-byte signature check), *the trusted comment as version binding* (not needed, the version is bound through the signed file names and hashes), *a monotonic counter in the manifest* (a release tooling change for a property the installed version already gives), *trusting the API `prerelease` flag* (unsigned).

### U5. On-disk layout and activation

- **Fact** (`docs/PFLICHTENHEFT.md` 8.2, `install/install.sh`): releases are `/opt/oarfish/releases/<version>` (root-owned, immutable, mode 0755), the active one is the relative symlink `/opt/oarfish/current -> releases/<version>`.
- **Proposal: the layout, with U15.**

  ```text
  /opt/oarfish/releases/<version>/{oarfish, oarfish.service, LICENSE, THIRD_PARTY_NOTICES.txt}
  /opt/oarfish/current                 -> releases/<version>
  /opt/oarfish/components/<id>/{oarfish-update-helper, oarfish-update-helper.socket,
                               oarfish-update-helper.service, oarfish-update-recover.service}
  /opt/oarfish/components/active       -> <id>      (helper binary and socket and service units)
  /opt/oarfish/components/known-good   -> <id>      (the recover unit and its binary)
  /usr/libexec/oarfish-update-helper   -> /opt/oarfish/components/active/oarfish-update-helper
  /etc/systemd/system/oarfish.service                 -> /opt/oarfish/current/oarfish.service
  /etc/systemd/system/oarfish-update-helper.socket    -> /opt/oarfish/components/active/...
  /etc/systemd/system/oarfish-update-helper.service   -> /opt/oarfish/components/active/...
  /etc/systemd/system/oarfish-update-recover.service  -> /opt/oarfish/components/known-good/...
  ```

  `<id>` is the version of the release that first delivered those exact bytes. The links in `/usr` and `/etc` are created once by the installer and never changed by the helper; the helper only moves links below `/opt/oarfish`. This is the layout of U15 rule 3 and it depends on verification V10; if V10 fails, U15 rule 3 names the fallback. **Device observation (G6.1, V10): V10 held, so this layout (Option L) stays and Option C stays the unused fallback.**
- **Device observation (G6.1, V10, A5): wants links.** `systemctl enable`, and `systemctl link` followed by `enable`, write a wants link that points to the **resolved release directory** (`.../releases/<version>/<unit>`), not to the path through `current` or to `/etc/systemd/system`. The installer therefore creates each wants link itself, as a **relative link to the unit in `/etc/systemd/system`** (for example `../oarfish.service`), which does not depend on a release directory. A wants link into a removed release directory did not stop the unit from loading, from being reported as enabled, or from starting at boot (observed), so the retention rule below is not blocked by it.
- **Proposal: staging.** Private working area `/opt/oarfish/releases/.stage.<id>/` (`<id>` 16 random hexadecimal digits), mode 0700, root-owned, on the same file system as the final directories so that the final `rename(2)` is atomic. It holds the four copied files and the extracted tree. The installer already uses names of the form `.stage.XXXXXX` there (**Fact**: `install/install.sh`).
- **Proposal: extraction rules.** The helper reads the tarball with `archive/tar` and `compress/gzip` from its private copy. It accepts only regular files and the single top-level directory `oarfish_<version>_linux_arm64/`; every other entry type (symlink, hard link, device, FIFO, absolute or `..` path, duplicate) fails the release. The accepted names are a fixed allow-list: `oarfish`, `oarfish.service`, `LICENSE`, `THIRD_PARTY_NOTICES.txt` and the four component files of U2. Each file must be listed in `contents` with the same size and SHA-256 and a mode of exactly 0644 or 0755; the helper creates files with `O_EXCL`, an explicit mode and root ownership and ignores owner, group and mode in the tar header. At most 128 MiB are extracted in total and `io.LimitReader` stops any file at its manifest size plus one byte (decompression bomb). The `oarfish` binary and the helper binary must be ELF64 little-endian executables for machine 0xB7, as the installer checks for `oarfish`. The four component files are extracted into `.stage.<id>/components/` and moved to `components/<id>` only when they differ from the active set (U15).
- **Proposal: durability rules.** Each extracted file is written, `fsync`ed and closed; the directory is `fsync`ed; the directory is root-owned with mode 0755, so the service cannot change it; `rename(.stage.<id>/release, releases/<version>)`; then `fsync` of `releases/`. The state file is replaced by write to a temporary file in the same directory, `fsync`, `rename`, `fsync` of the directory (the same recipe as `account.json`, ADR 0009 E4). A switch is a symlink `/opt/oarfish/.current.new.<id>` (or `components/.new.<id>`) created with `os.Symlink`, then `rename` over the old link (a single atomic replacement, like the installer's `mv -T`), then `fsync` of the parent directory. **Assumption:** the root file system is ext4 in ordered mode, where a rename is atomic with respect to a crash (old or new, never a mixture).
- **Proposal: free space.** Before `stage` the helper requires at least *copied bytes + extracted bytes + 64 MiB* free on the release file system (`statfs`); the 64 MiB reserve is the installer's `MIN_FREE_KB` (**Fact**) and keeps the state-file writes and the rollback possible. The service checks *bundle size + 16 MiB* on the cache file system before it downloads. Failure is `no_space` before anything is changed.
- **Proposal: retention.** After a successful commit the helper keeps the active release and the previous healthy one (the rollback target) and removes every other directory in `releases/` whose name matches the version pattern, plus stale `.stage.*` and `.trash.*`. Component directories are kept the same way: the ones `active` and `known-good` point to, and the one they replaced. A directory is removed by first renaming it to `.trash.<id>` (atomic) and then deleting it, so an interrupted delete never leaves a half-removed directory under a version name. Names that do not match the pattern are never touched. Nothing is pruned after a rollback or a failure. Two releases at about 7 MB each (*estimate*) cost little space; this replaces the installer's "old release directories are kept" (**Fact**: `docs/INSTALL.md`) and the documentation must say so when it is implemented.
- **Proposal: a staged release expires after 30 minutes.** `stage` records the time since boot (U6, R1) in the state file; `activate` refuses a staged release older than 30 minutes with `stage_expired`, and `status` and `recover` remove it. Reason: a staged tree should not wait indefinitely between verification and use, and with the second confirmation of U15 rule 7 the flow can pause for a human. The GUI shows "the verification expired, start again" and the service downloads again (the cache is deleted after a successful `stage`).
- **Proposal: releases that change the helper or a unit.** In G6.5 to G6.9 `stage` compares the helper files and `oarfish.service` of the new release with the installed ones and, when any differs, answers `installer_required` with the list of differing names; the GUI shows the installer instruction (U9, U11). From G6.10 on the same comparison selects the component transaction of U15. The comparison is of SHA-256 digests. A release whose helper code and unit texts are unchanged compares equal only because the helper's bytes depend on its code, the packages it links and the toolchain, never on the release version or the commit (A13, D-C80).
- **Estimate of microSD writes per update:** about *2 × bundle + extracted size* plus well under 100 KiB of state, journal and audit data. For the local build that is roughly 2 × 3 MB + 7.3 MB ≈ 13 MB (*estimate*, not measured); a release at the 35 MB budget would be several times that. With a changed helper (about 3 to 5 MB more, *estimate*) the figure rises accordingly. At one update a month this is a few hundred megabytes a year (*estimate*), small against the wear the R-04 risk is about. Measured in G6.9 with `write_bytes` of `/proc/<pid>/io` for both processes, as AT-022 does.

Alternatives considered: *extract directly into `releases/<version>`* (a power failure leaves a half-filled directory under a version name; the staging-and-rename recipe is what the installer already does), *hard-linking or deduplicating files between releases* (one binary dominates the size, nothing to gain), *keeping all old releases* (the SD card fills; the installer's remark that they are small holds only for a few releases), *a separate `rollback` symlink* (a second pointer that can disagree with `current`; the previous version is state of the transaction), *an unlimited stage lifetime* (a verified tree of unknown age).

### U6. Health check, rollback and the case where the new binary does not start

- **Proposal: the helper decides, nobody else.** The GUI only displays; the service may be the thing that is broken.
- **Proposal: before a transaction starts**, the helper requires the current release to be healthy (same criteria as below, 10 s) and records the SHA-256 of the current release's binary, so that it knows a healthy rollback target exists. When components will change it records the hashes of the known-good component set as well and refuses with `components_damaged` if the set on disk does not match them (U15 rule 4). Otherwise `current_unhealthy`.
- **Proposal: what counts as healthy after activation.** All of these, polled every second:
  1. `systemctl show` reports `ActiveState=active`, `SubState=running`, and a `MainPID` that differs from the one before the restart;
  2. `NRestarts` has not increased since the restart (a start-then-crash loop with `Restart=on-failure` is therefore caught, not mistaken for a slow start);
  3. `GET /healthz` on the loopback address from the unit's `-listen` argument (read from the `ExecStart` property and accepted only if it is a literal loopback IP address with a port, else the default `127.0.0.1:8484`) answers `200` with `"status":"ok"`;
  4. **the response reports the version that was installed** (see next point);
  5. conditions 1 to 4 hold for 3 consecutive polls and for at least 10 s ("stability window").
  Fail fast: `ActiveState=failed` or an increased `NRestarts` ends the wait at once. Otherwise the deadline is 45 s after the restart returned.
- **Device observation (G6.1, V6, A6): the stock start limit does not end a crash loop of `oarfish.service`.** The stock limit (5 starts in 10 s) is not reached by `Restart=on-failure` with `RestartSec=2`: a throwaway unit with those lines and a command that exits at once restarted 20 times in 45 s and never reached `start-limit-hit`. The health criteria therefore rely on `NRestarts` (criterion 2), not on a start limit.
- **Owner decision (Q5): reported version.** `/healthz` gains a `"version"` member (`{"status":"ok","version":"0.1.0-alpha.8"}`). The installer's check only globs for `"status":"ok"` (**Fact**: `install/install.sh`), so it keeps working. This touches D-C46 / ADR 0009 E9 ("a minimal health check that exposes no device data"); the Oarfish version is "Öffentlich" in the data classes of spec 10.3, so it is not device data (spec 13.1 says so since version 0.3.5; `docs/AUTH.md` is updated when the change is implemented). Alternatives: reading `/proc/<pid>/exe` (needs `CAP_SYS_PTRACE` for a process of another user, **Assumption**; rejected) or executing the new binary with `-version` (never execute downloaded code as root). **Proposal:** the specified `/readyz` (spec 13.1; **Fact**: it does not exist) should be implemented in plan step G6.7 and the helper should then require it as well, because `/healthz` only says that the process answers.
- **Proposal: `daemon-reload`.** When the `oarfish.service` of the new release differs from the one of the old release, the helper runs `systemctl daemon-reload` after the `current` switch and before the restart, and again after a switch back. `oarfish.service` is a link to `current/oarfish.service` (U5), so its text moves with `current`. This applies from G6.10; before that such a release is `installer_required`.
- **Proposal: automatic rollback** (ORB-UPD-006) when conditions fail or the deadline passes: write the intent `rolling_back`, verify that the SHA-256 of `releases/<from>/oarfish` equals the hash recorded before the transaction (a damaged rollback target on a microSD card is detected instead of activated), switch `current` back, `systemctl reset-failed oarfish.service` (the failed starts count against the start limit; the installer does the same, **Fact**), `restart`, and apply the same health criteria to the old version. Result `rolled_back` with a reason code. If the old version does not become healthy either, the result is `rollback_failed`: the helper stops, leaves `current` on `from`, logs it, and the state says `needs_attention`. The GUI is probably unreachable then; the administrator needs a shell and `journalctl`, exactly like the installer's last message today. This is the one case where a GUI-only path is not promised, and it is stated as such.
- **Proposal: the new binary does not start at all.** It is the same path: `ActiveState` goes `failed` or `NRestarts` rises within seconds, the helper rolls back without waiting for the full deadline. Worst case duration for one update that fails and rolls back is below 2 minutes (*estimate*).
- **Proposal: cooldown that survives the helper (R1).** After a rollback or a failure the helper refuses `activate` for the same version for 15 minutes, and refuses any `activate` within 60 s of the previous one (`cooldown`). A compromised service cannot use the helper to restart `oarfish.service` in a loop. Because the helper exits after 30 s idle, the markers are stored in `state.json` (U8) and use **time since boot**, never the wall clock (the device has no battery-backed clock, **Assumption**, and the clock can jump when it is set; **Device observation (G6.1, V8, A8):** the reference device has no RTC device and no `fake-hwclock`; systemd restores the clock from its timestamp file (`/var/lib/systemd/timesync/clock`) at boot and `systemd-timesyncd` synchronises it later, seen after a soft reboot; a cold start was not tested): `CLOCK_BOOTTIME`, read with `clock_gettime` through the standard library's `syscall` package, in whole seconds. Each marker stores `set_at_uptime_s` and `until_uptime_s`. **After a reboot the cooldown may reset, and this is stated as accepted:** a marker is void when the current uptime is smaller than `set_at_uptime_s` (a reboot happened); when the machine has been up long enough that this test does not notice the reboot, the marker can block for at most its original length again (15 minutes at the worst) and then expires. The service cannot reboot the device, so a reboot is an act of the administrator or of the power supply, not a lever of an attacker. The wall-clock `finished_at` of a result is for display only.
- **Proposal: rollback does not restore data, so releases must be N-1 compatible.** A release must not change the format of anything below `/var/lib/oarfish` in a way that the previous release cannot read. A migration is allowed only in two steps (first a release that reads both formats, later one that writes the new one). The same rule holds for `state.json` of the helper (U15 rule 6). This is a rule for release authors and belongs in `docs/RELEASING.md` when G6 is implemented.
- **Proposal: a future TLS design (D-D10) must keep a loopback-reachable plain health route, or this ADR is amended**, because the helper's health check depends on it.

Alternatives considered: *the service reports success to the helper* (a broken service cannot report; a compromised one lies), *only `systemctl is-active`* (a crashing binary is "active" for a moment), *a watchdog (`WatchdogSec=`) in the unit* (needs the Go binary to implement `sd_notify`; reasonable later, not required), *the GUI decides* (the GUI is the thing that may be dead), *the wall clock for the cooldown* (can jump, no battery-backed clock), *keeping the cooldown in memory* (lost when the helper exits after 30 s idle, which is the case of F20).

### U7. Crash and power-loss safety

- **Proposal: a persistent state machine in `state.json`** (U8) with these phases of the release transaction: `idle`, `staging`, `staged`, `switching`, `verifying`, `committed`, `rolling_back`, and the results `rolled_back` and `rollback_failed`; the component phases of U15 rule 4 sit in front of them. A phase is written (atomic replace, `fsync`) **before** the action it announces (write-ahead intent), so that a crash always leaves a state that names what might be half done. At most ten small writes per update.
- **Proposal: recovery looks at the disk, not only at the file.** The truth about what is active is the target of `current` and of the two component links; the state file says why. If `state.json` is missing or unreadable (SD fault), the helper rebuilds the minimum from the disk (installed version = target of `current`, component set = the targets of `active` and `known-good`, no previous version known, no transaction), removes `.stage.*` and `.trash.*`, and reports `needs_attention` once.
- **Proposal: one rule for every unfinished transaction.** `recover` (at boot, before `oarfish.service` and the helper socket start) and `serve` (when it finds a transaction that no process owns, found with an exclusive `flock` on the state directory) both apply the same conservative rule: **an update that has not reached `committed` is rolled back.** The components have their own commit (U15 rule 4) and are not rolled back by a failure of the later release phases. At boot the service is not yet running, so rolling back is only a link switch; later the full rollback procedure of U6 runs. Reason: it needs no health decision at boot and has one code path. Cost: a power failure in the short window after the new version proved healthy and before `committed` is durable rolls back a good update; the administrator repeats it.
- **Table: where power can fail in a release update.** "State" is what is on the disk afterwards; "Recovery" is what the next start does. The component steps have their own table in U15 rule 4.

  | # | Step | Interrupted here | State on disk | Recovery at the next start |
  |---|---|---|---|---|
  | 1 | Service downloads files into the cache | during the download | `.part` file in the cache, helper state `idle` | The service deletes `.part` files and stale cache directories at start; nothing privileged happened. |
  | 2 | Helper copies and verifies (`staging` written) | during copy, verify or extraction | `.stage.<id>` partly filled, `releases/` unchanged, `current` unchanged | `recover` deletes every `.stage.*`, sets `idle`, result `aborted`. The administrator starts again. |
  | 3 | `rename` of the staged tree into `releases/<version>` | after the rename, before `staged` is durable | `releases/<version>` complete, state still `staging` | The state names `to`; `recover` removes `releases/<to>` (it is not `current` and not the recorded previous), sets `idle`. |
  | 4 | `staged` written (with the time since boot) | waiting for `activate`, or for the second confirmation | verified release inactive, `current` unchanged | After a reboot `recover` removes the staged directory (uptime is smaller than the recorded one). Without a reboot, `activate` refuses a staged release older than 30 minutes (`stage_expired`) and `status` removes it; a staged release never outlives the window between verification and use. |
  | 5 | `switching` written (intent, `from`, `to`, hash of the old binary) | before the symlink rename | `current -> from` | `current` still names `from`: clear the intent, remove `releases/<to>`, `idle`. |
  | 6 | Symlink `rename` | at the rename or just after it, directory not yet synced | `current` names `from` or `to` (never anything else) | If `from`: as step 5. If `to` while the state is `switching` or `verifying`: roll back (below). |
  | 7 | `systemctl restart`, `verifying` written, polling | any time, including a helper crash or the kernel killing it | `current -> to`, state `verifying` | At boot: switch `current` back to `from` before the service starts, result `rolled_back` reason `interrupted`. Live (helper restarted by a new connection): full rollback procedure of U6. |
  | 8 | Health proven, `committed` not yet durable | in the window between | `current -> to`, state `verifying` | Same as step 7 (accepted cost). |
  | 9 | `committed` written | before pruning is complete | new `current`, `from` recorded as previous, some old releases or `.trash.*` left | `recover` finishes the pruning; no functional effect. |
  | 10 | Rollback (`rolling_back` written) | during the switch back or the health check of the old version | `current -> from` or `to` | Ensure `current -> from`, compare the hash of the old binary with the recorded one; equal: result `rolled_back`; different: `rollback_failed`, `needs_attention`. |
  | 11 | Any state-file write | during the temporary file or the rename | previous state file intact (temporary file is ignored and deleted) | The previous phase applies; the rows above cover it. |
  | 12 | The SD card itself fails or remounts read-only | any | not designed | The helper reports `io_error` and stops. Not verified, not covered by this design. |

- **Proposal: the invariant the tests must prove.** After a crash at any point, `current` names a complete release that was healthy when it was last activated, the two component links name complete component sets, and no release or component directory under a version name is incomplete.
- **Assumption:** the above relies on rename atomicity and on `fsync` doing what it says on the microSD card's controller. Power-cut behaviour of cheap cards is not predictable from software; the design reduces exposure, it cannot prove durability (U13).

Alternatives considered:

- *Resume forward at boot instead of rolling back:* needs a health decision before the service runs, and a second code path with its own bugs. Rejected for the same reason as the draft's single rule.
- *No intent record, deduce everything from the disk:* the disk cannot say whether a link switch was intended or an accident, nor which hash the old binary had. The state file is cheap (under ten writes).
- *A/B partitions or boot-loader based rollback:* the specification forbids automatic changes of boot loader or partition table (spec 13.3) and it would tie the update to one board family.
- *`syncfs` or `sync` after every phase:* writes more and flushes unrelated data; the targeted `fsync` of files and directories is enough on ext4 (V7).
- *A per-file journal for the component files:* replaced by a single link switch for the whole set (U15 rule 3), which is atomic by `rename`.

### U8. State, audit and privacy

- **Owner decision (Q6): the helper's state lives in `/var/lib/oarfish-update/`** (`StateDirectory=oarfish-update`, root-owned, mode 0700; `state.json` 0600). **This deviates from the text of spec 8.2 and D-C47, which place update state in `/var/lib/oarfish/`**; the specification is corrected in version 0.3.5 (C1). Reason: `/var/lib/oarfish` is owned by the unprivileged service (**Fact**: `install/oarfish.service`, `docs/AUTH.md`; mode 0700, user `oarfish`); the owner of a directory can rename or remove entries in it whatever their owner and mode (**Assumption**, Unix semantics), so state the root helper trusts for rollback decisions would be modifiable by the service it protects against. The service reads the state only through `status`.
- **Proposal: `state.json` content** (schema-versioned, additive fields only, so that an older helper can read what a newer one wrote, U15 rule 6): `schema_version`, `helper_version`; the transaction (`id`, `phase`, `to`, `from`, `from_binary_sha256`, `correlation_id`, `staged_at_uptime_s`) with its component part (`components_from`, `components_to`, `known_good_sha256`, `socket_restart_pending`, a status flag only, A2); the cooldown markers (`refuse_version`, `refuse_until_uptime_s`, `refuse_set_at_uptime_s`, `last_activate_uptime_s`, `last_activate_set_at_uptime_s`); and the last result (`transaction_id`, `to`, `from`, `outcome`, `reason_code`, `correlation_id`, `finished_at`, `acknowledged`). Times with the suffix `_uptime_s` are seconds since boot (U6); `finished_at` is wall-clock time for display only. Only the last result is kept; the history is the audit. No host name, no address, no URL, no user name, no serial number.
- **Proposal: audit.** The existing audit file belongs to the service (ADR 0009 E7), and a second writer would break its rotation and its ownership, so the helper does not write it. Instead: (1) the service writes `update_started` when the administrator confirmed (user = the account name, correlation ID = the server's), and a finishing entry `update_finished` when it reads an unacknowledged last result from `status` (after the restart, at the next start), with the **same correlation ID** and user `-` (the service keeps nothing across a restart; with a single administrator account the correlation ID links the two); then it sends `ack`. (2) The helper writes its own phase changes to the journal, one line per phase, as `phase=<name> to=<version> from=<version> cid=<id> result=<code>`; this record does not depend on the service. **Fact** (ADR 0009 E7): action, target and result are closed sets or patterns; the update actions are not in them. The sets are extended by **ADR 0009 amendment A6** (accepted together with this ADR, D-C61): the actions `update_started` and `update_finished`, the target pattern `update/<version>` (the version regex of U2), and the results listed in A6 (the helper's error codes and outcomes plus `unavailable`). The code change is part of plan step G6.7.
- **Proposal: nothing secret and nothing identifying in logs.** The helper handles no secret: it has no password, no token, no cookie (the request carries none). Network errors are mapped to codes (`dns_failure`, `tls_failure`, `timeout`, `unreachable`, `http_<status>`, `rate_limited`) and never logged as Go error strings, because those contain server addresses. No log line contains a request body, a cookie or a header value. This extends ORB-SEC-007 and AT-023 to the update path; the redaction test covers it.
- **Estimate:** about 750 bytes of audit data and about 2 KiB of journal data per update.
- **Device observation (G6.1, V6, A6):** `journald` is volatile on Raspberry Pi OS Lite (`Storage=volatile`, set by a drop-in of the image; the current journal is under `/run/log/journal`). The helper's journal lines do not survive a reboot, and the persistent record is `state.json` plus the service's audit entry. (This was an assumption of the accepted text: "whether `journald` writes to disk or only to memory is not known".)

### U9. Authorization in the GUI

- **Proposal: routes** (all behind a valid session, ADR 0009 E9 / D-C46; every state-changing one with the CSRF layers of ADR 0009 E3 unchanged, JSON only, body at most 1 KiB, correlation ID by the server): `GET /api/v1/update` (installed version, available version, state, last result, helper presence, staged release and whether components differ), `POST /api/v1/update/check`, `POST /api/v1/update/apply`, `POST /api/v1/update/discard`. There is no route that takes a path, a URL or a command.
- **Proposal: administrator confirmation (ORB-UPD-002).** The GUI shows installed and available version, channel, release notes (plain text, labelled "from GitHub, not signed"), download size, signature status, migrations (none exist yet) and rollback ability (spec 9.5), and states that the service restarts and everybody is signed out. `apply` takes `{"version": ..., "current_password": ...}` and, in the second stage below, `{"components_confirmed": true}`.
- **Owner decision (Q3): a fresh password confirmation is required for `apply`.** It is checked through the same global limiter and the same single Argon2id hash as login and password change (ADR 0009 E6, amendment A3). Reason: an update replaces the code that runs on the device, it is the most consequential action the GUI offers so far, and a stolen or left-open session should not be enough. Cost: one hash (442.9 ms on the reference device, **Fact**: `docs/AUTH.md`). The hash is computed **before** the download starts and its memory is returned to the system, so the 19 MiB hash and the download never overlap (U12). `version` must equal the version of the last check; the helper validates it again.
- **Owner decision (Q1): a component update has its own explicit confirmation in addition to the password** (U15 rule 7). When `stage` reports that the helper or a unit differs, the flow pauses after `stage` (the staged release waits at most 30 minutes, U5) and the GUI shows a second dialog that names what will be replaced. Only then does the service send `activate` with `components_confirmed: true`. There is no automatic component update.
- **Proposal: no automatic upgrade in the MVP.** No timer, no policy setting, no background download. The finishing entry in the audit is the only thing that happens without a click, and it is not an action.
- **Proposal: the transaction survives the end of the session.** After the confirmation the flow does not depend on the browser: a session that expires does not stop it. A restart of the service before `stage` ends it harmlessly (nothing privileged happened); after `activate` the helper finishes alone. The page polls `GET /api/v1/update` every 2 s while an update is running, and shows the result after the administrator has logged in again.
- **Transitional rule until G6.10 is accepted.** When `stage` answers `installer_required`, the GUI does not show a dead end: it names the differing components, says that this release replaces privileged parts and needs one run of the installer on the device, and shows the exact command for the release (`sudo sh install.sh` from the release page, the installer of that release, as `docs/INSTALL.md` describes it). The update is not applied from the GUI.

Alternatives considered: *one click without a password* (adequate for a service restart, not for replacing the program), *a separate "activate now" confirmation after every staging* (more clicks; the pause exists in the protocol and is used for components only), *letting the helper check the password* (would put Argon2id and `account.json` into a root process), *one combined dialog for password and components* (the second dialog exists so that a rare, privileged change is not clicked through as part of the routine one).

### U10. Hardening

- **Proposal: the helper units** (documentation of the intent; the real files are written in plan step G6.3, then checked with `systemd-analyze verify` in CI and `systemd-analyze security` on the device):

  ```ini
  # oarfish-update-helper.socket
  [Socket]
  ListenStream=/run/oarfish-update/helper.sock
  SocketUser=root
  SocketGroup=oarfish
  SocketMode=0660
  DirectoryMode=0755
  Accept=no
  Backlog=8
  RemoveOnStop=yes
  [Install]
  WantedBy=sockets.target

  # oarfish-update-helper.service
  [Unit]
  OnFailure=oarfish-update-recover.service
  StartLimitIntervalSec=10
  StartLimitBurst=3
  [Service]
  Type=exec
  ExecStart=/usr/libexec/oarfish-update-helper serve
  Restart=on-failure
  RestartSec=1
  SuccessExitStatus=75
  RestartForceExitStatus=75
  RuntimeMaxSec=900
  User=root
  Group=root
  UMask=0077
  StateDirectory=oarfish-update
  StateDirectoryMode=0700
  ReadWritePaths=/opt/oarfish
  NoNewPrivileges=yes
  CapabilityBoundingSet=
  AmbientCapabilities=
  ProtectSystem=strict
  ProtectHome=yes
  PrivateTmp=yes
  PrivateDevices=yes
  ProtectKernelTunables=yes
  ProtectKernelModules=yes
  ProtectKernelLogs=yes
  ProtectControlGroups=yes
  ProtectClock=yes
  ProtectHostname=yes
  ProtectProc=invisible
  ProcSubset=pid
  RestrictNamespaces=yes
  RestrictRealtime=yes
  RestrictSUIDSGID=yes
  LockPersonality=yes
  MemoryDenyWriteExecute=yes
  RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
  IPAddressDeny=any
  IPAddressAllow=localhost
  SystemCallArchitectures=native
  SystemCallFilter=@system-service
  SystemCallErrorNumber=EPERM
  MemoryMax=64M
  TasksMax=16
  Nice=10
  ```

  `oarfish-update-recover.service` uses the same sandbox with `Type=oneshot`, `TimeoutStartSec=120` (A9), `ExecStart=/opt/oarfish/components/known-good/oarfish-update-helper recover`, `DefaultDependencies=no`, `After=local-fs.target` (A14), `Before=oarfish-update-helper.socket oarfish.service`, `WantedBy=multi-user.target`, and no socket. Its own lines, in the text the repository ships:

  ```ini
  # oarfish-update-recover.service (the sandbox lines are those of the helper service)
  [Unit]
  DefaultDependencies=no
  After=local-fs.target
  Before=oarfish-update-helper.socket oarfish.service
  [Service]
  Type=oneshot
  TimeoutStartSec=120
  ExecStart=/opt/oarfish/components/known-good/oarfish-update-helper recover
  [Install]
  WantedBy=multi-user.target
  ```

  `TimeoutStartSec=120` (A9) exists because a oneshot unit has no start timeout by default and this unit is ordered `Before=oarfish.service`: without it a hanging recovery could hold up the start of Oarfish at boot. After the timeout systemd marks the unit failed and `oarfish.service` still starts, because the ordering is not a requirement. The lint canonical form of this unit (U15 rule 3) gets the line in G6.10. A normal idle exit is code 0. **Device observation (G6.1, V11, A1):** `Restart=on-failure` alone does **not** make the helper's exit code 75 ("restart me with the new binary", U15 rule 4) work: 75 is a failed exit, so `OnFailure=` starts the recovery unit, and the recovery switches `active` back before the restarted helper runs. `SuccessExitStatus=75` declares 75 a clean exit, so `OnFailure=` is not started, and `RestartForceExitStatus=75` still forces the restart. Both lines are part of the canonical form of this unit (U15 rule 3). What `OnFailure=`, the start limit and the recovery do with a helper that fails is in U15 rule 4, failure handling.
- **Every place where hardening is relaxed, why, how narrowly, and the risk that stays:**

  | Relaxation | Why it is needed | How narrow | Risk left |
  |---|---|---|---|
  | The helper runs as root (`User=root`) | It writes root-owned release and component directories and the links below `/opt/oarfish`, and asks systemd to reload, restart and reset units. | Empty capability set (`CapabilityBoundingSet=`): a uid-0 process without capabilities has no `CAP_DAC_OVERRIDE`, `CAP_CHOWN` or `CAP_SYS_ADMIN`; it can only write what it owns (**Assumption**, Linux semantics). It reads the cache because those files are world-readable (U3). **Assumption, verification V1:** `systemctl restart`, `daemon-reload` and `reset-failed` may need more than uid 0 when talking to systemd. If an empty set does not work, the work stops and the owner decides; the answer must not silently become `CAP_SYS_ADMIN`, which would make the sandbox mostly cosmetic. | A bug in the helper runs as uid 0 inside the sandbox below. |
  | `ReadWritePaths=/opt/oarfish` | Releases, component sets and all links the helper moves live there. | **One directory** (and the state directory that `StateDirectory=` makes writable). `/etc`, `/usr` (including the helper's own link) and everything else stay read-only, so the helper cannot rewrite a unit file or the links in `/etc` and `/usr`; it can only change where the links below `/opt/oarfish` point. | Anything below `/opt/oarfish` can be damaged by a helper bug. What protects it: the compiled-in allow-list of write targets and file names (U2), the unit lint (U15 rule 3), the hash checks of the rollback targets, and immutable root-owned directories. This is a residual risk, not a guarantee. |
  | **Fallback only, if V10 fails:** `ReadWritePaths=/opt/oarfish /usr/libexec /etc/systemd/system` | The helper would copy the binary and the unit files to their final places instead of moving links. | Two more directories, each shared with unrelated files; the helper may write any file name there as far as the sandbox is concerned. | A helper bug could drop an arbitrary unit file into `/etc/systemd/system`, which defines what runs as root. Only the compiled-in name list and the lint stand in the way. This is clearly weaker, and is the reason the link layout is preferred. |
  | `AF_INET`, `AF_INET6` | The health check connects to `/healthz` on loopback. | `IPAddressDeny=any` and `IPAddressAllow=localhost` limit traffic to the loopback addresses (**Assumption, V2:** the Raspberry Pi OS kernel supports the cgroup BPF address filter). The unit has no outbound network. | The helper could reach any other service on loopback (for example an SSH tunnel endpoint) if a bug let an attacker choose the destination; the destination is a constant or a validated loopback literal. |
  | No `PrivateNetwork=yes` | A private network namespace would hide the loopback listener of the main service. | See the line above. | Same. |
  | `RuntimeMaxSec=900`, `Restart=on-failure`, `SuccessExitStatus=75`, `RestartForceExitStatus=75`, `OnFailure=` | A hanging or crashing new helper must end in a rollback (U15 rule 4); a deliberate hand-over by exit code 75 must not. | The same sandbox applies to the recovery unit; neither unit is `Requires=` of anything. | **Device observation (G6.1, V11, A1):** the interplay works with the two exit-status lines and does not work without them (see the text after the unit block and U15 rule 4). Every restart counts against `StartLimitBurst=3` per 10 s. One device, throwaway units; never tried with the real helper. |
  | Not a relaxation, stated for clarity: no write access to `/etc/systemd/system` (primary layout) | A unit file in a release would define what runs as root. | Units are linked, not copied; the lint checks every unit text before any link can point to it (U15 rule 3). | A hardening change in a unit passes only if the lint rules allow it (two-step rule). |

- **Proposal: the main unit needs no relaxation for G6.** Outbound HTTPS needs `AF_INET`/`AF_INET6` (already allowed, **Fact**: `install/oarfish.service`), the system root certificates and resolver files under `/etc` (readable under `ProtectSystem=strict`), writing the cache (`CacheDirectory=oarfish`, already writable), and connecting to the helper socket (`AF_UNIX` is allowed, `/run` is readable). **Assumption, V4:** the Go resolver and TLS stack work under `SystemCallFilter=@system-service` and `ProtectSystem=strict`, and `ca-certificates` is installed on Raspberry Pi OS Lite and Ubuntu Server (the installer already depends on HTTPS to GitHub through `curl`, which is not proof of the file the Go client reads). What must **not** happen is stated here so that G6 does not add it: no `ReadWritePaths=/opt/oarfish`, no capability, no `User=root`, no `NoNewPrivileges=no`, and no `SupplementaryGroups=` for the service. `docs/ARCHITECTURE.md` already requires a narrow, commented relaxation for any gate that needs one; for G6 there is none. The only change of `oarfish.service` is that, in the primary layout, it becomes a link into the release (U5), which is the installer's job.
- **Proposal: the cache modes of U3 (0755 directory, 0644 files) are chosen so that the helper does not need `CAP_DAC_READ_SEARCH` or `SupplementaryGroups=oarfish`.** **Device observation (G6.1, A7):** the helper sandbox reads a 0644 file and not a 0600 file of the service user (see U3), so no `CAP_DAC_OVERRIDE` exists in it.
- **Device observation (G6.1, A4): `MemoryMax=64M` is not enforced on the reference image.** The unit text keeps `MemoryMax=64M`, and `TasksMax=16` works through the `pids` controller. Where the memory cgroup controller is absent the limit does nothing; the Raspberry Pi OS kernel command line has `cgroup_disable=memory` and `cgroup.controllers` lists `cpuset cpu io pids` only (systemd reports `MemoryMax=67108864` and says nothing else; no allocation test was run). The memory protection of the helper must therefore come from the program: streaming with 32 KiB buffers and a soft Go memory limit (`GOMEMLIMIT` semantics, set by the program itself so that the unit text needs no `Environment=` line). The helper peak is measured with `VmHWM` in G6.9. The kernel command line is not changed (specification 13.3).
- **Proposal: the clock.** `ProcSubset=pid` hides `/proc/uptime`; the helper reads the time since boot with `clock_gettime(CLOCK_BOOTTIME)` instead (V12), so the sandbox stays as tight as it is.

Alternatives considered: *`PrivateNetwork=yes` and reading the version from `/proc`* (needs `CAP_SYS_PTRACE`, worse than the loopback filter), *granting `CAP_DAC_OVERRIDE`* (a blanket override of file permissions), *`DynamicUser=` for the helper* (it must own root files), *running the helper as the `oarfish` user with `ReadWritePaths=`* (puts releases under the control of the unprivileged service, defeating the point), *dropping `ProcSubset=pid` to read `/proc/uptime`* (widens the sandbox for a value a system call gives).

### U11. Bootstrap and migration

- **Fact** (`docs/ARCHITECTURE.md`, `docs/INSTALL.md`, `install/install.sh`): no helper exists; `/usr/libexec/oarfish-update-helper` is not created by the installer; a re-run of the installer with another version installs it beside the old one, switches `current`, restarts the service and checks health; the unit file is copied to `/etc/systemd/system` and replaced when it differs.
- **Owner decision (Q1): the one-time bootstrap from a version without a helper stays an installer run.** The first helper-capable release (the "bootstrap release") is installed by running its `install.sh` on the device. A device that runs `v0.1.0-alpha.6` has no update page, so there is no GUI path to the bootstrap release; this one-time step is stated plainly in the release notes (as the G5.1 notes did for the setup mode, **Fact**: `docs/RELEASING.md`). From then on updates come through the GUI, and, from G6.10, so do the helper and its units.
- **Proposal: what the installer must change later (described, not changed here), in plan step G6.3:** (1) extend the bundle contract: `check_bundle_entries` today requires exactly five entries (**Fact**: `install/install.sh`); the bundle gets `oarfish-update-helper` and the three helper unit files, and the manifest `contents`, `bundlegen check` and `docs/RELEASING.md` follow; (2) create the layout of U5: `components/<id>`, the two component links, `/usr/libexec/oarfish-update-helper` as a link, and the four unit links in `/etc/systemd/system`; replace today's regular file `/etc/systemd/system/oarfish.service` by a link to `current/oarfish.service` (drop-ins of `systemctl edit` keep working, **Assumption**); this step follows the result of G6.1 (V10); (3) run `daemon-reload`, enable `oarfish.service`, the socket and the recover unit by creating their wants links itself as relative links to the unit in `/etc/systemd/system` (for example `../oarfish.service`), not with `systemctl enable`, which writes the resolved release path (A5); when the helper bytes or the socket text changed, or the socket is not active, stop `oarfish-update-helper.service` and then restart `oarfish-update-helper.socket`, because `daemon-reload` does not apply a changed socket text to a running socket (A9); (4) create `/var/lib/oarfish-update` (root:root, mode 0700) because the installer takes the lock there before the helper has ever run (A9; `StateDirectory=` and `StateDirectoryMode=0700` in the helper service stay and agree with it); (5) `--uninstall` stops and removes them, `--purge` also removes `/var/lib/oarfish-update`; (6) `--dry-run` lists the new steps; (7) take the same lock as the helper so that an installer run and a GUI update cannot overlap (today "two installers running at the same time are not guarded against", **Fact**: `docs/INSTALL.md`); (8) tests in `scripts/test-installer.sh` for each. The installer's own rollback stays as the safety net for installer runs.
- **Proposal: the helper and its units are replaced by a GUI update only under U15, from G6.10 on.** Until G6.10 is accepted, a release that changes the helper or any unit needs an installer run, and the GUI says so with exact instructions (U9). "Changes" means changes the bytes (U5); a release that does not touch the helper code, the packages it links, the toolchain or a unit text needs none (A13). A device on `v0.1.0-alpha.7` needs one installer run, once, because that helper embeds its release version (A13). The manifest field `requirements.helper_protocol` (U4) makes the service and the helper say that instead of failing obscurely.
- **Why the draft said "installer only", and what U15 changes (R5).** The draft argued that a helper replaced by a release would turn a leaked release key into root code. That argument was incomplete and is withdrawn in this form. The installer also runs as root and also trusts the release key (its embedded copy of it; **Fact**: ADR 0006), so a validly signed helper can already be put on a device as root code by an administrator who runs the installer of a release. What is true is narrower: (1) the root of trust of the installer path is a script the administrator can read before running it (**Fact**: ADR 0006, "download, inspect, run"), while the GUI path asks the administrator to confirm a replacement of binaries that cannot be read in the browser; (2) a key alone does not reach a device in either path, the attacker also needs the download channel (the release assets on GitHub), because both paths fetch from there; (3) the GUI path adds a route to root code that needs two confirmations and a password instead of a shell session. U15 does not claim to remove that difference. It reduces what a valid signature can do to a device (rules 3 and 5, the two confirmations of rule 7, the lint) and keeps the leaked-key analysis in T7. The real delta of U15 is given in rule 10 of U15 and in T7 and T17.
- **Proposal: compatibility.** The release layout, the `current` symlink form, the state directory and the unit `oarfish.service` stay as they are for the service; an installation made by the installer of any earlier alpha works unchanged until the bootstrap release is installed (the helper bootstraps its state from `current`, U7). Protocol and state compatibility between consecutive helpers is U15 rule 6. The service tolerates a missing socket (no helper installed) by showing the installed version and the installer instruction instead of an update button.

### U12. Resource budget on the Raspberry Pi Zero 2 W

All values are *estimates* until G6.9 measures them.

| Item | Proposal / estimate |
|---|---|
| Helper memory | Idle process: none (socket-activated, exits after 30 s). Running: below 16 MB peak. `MemoryMax=64M` stays in the unit text but is not enforced on the reference image, where the memory controller is absent (A4, U10); the protection comes from the program: streaming, 32 KiB copy buffer, gzip window 32 KiB, a soft Go memory limit. The peak is measured with `VmHWM` in G6.9. |
| Service memory during an update | No body buffering (32 KiB buffer); TLS and HTTP client add a few MB while a download runs. The Argon2id hash (19 MiB, **Fact**) runs before the download and its memory is returned (`FreeOSMemory`, **Fact**: ADR 0009 A5), so the two peaks do not add. Core idle target stays 35 MB (ORB-NFR-001); service peak during an update below 45 MB; service plus helper below the 80 MB set budget (ORB-NFR-002). |
| CPU | SHA-256 and gunzip over a few MB, one Ed25519 verification, one unit lint: well under 2 CPU-seconds in total on a Cortex-A53; `Nice=10` for the helper. Fsync waits dominate (seconds). |
| Time | Download: 3 MB at 1 Mbit/s is about 25 s. Stage: 5 to 15 s. Restart and stability window: 15 to 25 s. A component transaction adds one helper restart and its self-check, a few seconds (device observation, A1: a `daemon-reload` alone took about 3 s and a recovery run about 3.5 s on the reference device). Failure plus rollback: below 2 minutes. |
| Download | Streamed to a file, never held in memory; 40,000,000 byte cap; 30 s stall timeout; 15 min absolute limit; one retry; no resume (U3). |
| Check interval | Only on user action: opening the update page (cached 60 min) or "Check now" (60 s apart). At most a handful of API requests a day against the anonymous limit of 60 per hour per address (device observation, A3: a `304` counts like any other request, so the ETag does not save quota; the budget is unaffected). |
| Slow or interrupted network | A stalled or cut download is discarded; a partial bundle is never handed to the helper; a message with a code is shown (ORB-NFR-014, R-05). A wrong clock (the Zero 2 W has no battery-backed clock, **Assumption**) makes TLS fail; the message says to check the date. The helper never uses the wall clock for a security decision or for a cooldown. |
| microSD writes | About 2 × bundle + extracted size per update (U5) plus under 100 KiB for state, journal and audit; more when components change. |
| Disk | Two releases and two component sets kept; free-space rules of U5. |

Alternatives considered: *a hard CI gate on memory and time* (the numbers depend on the device and the card; they are measured on the device in G6.9 and reported, not asserted in CI), *buffering the bundle in memory* (up to 40 MB against an 80 MB budget), *a periodic background check* (violates ORB-UPD-002's spirit, spec 10.5 and the rate limit), *persisting the ETag on disk* (an SD write for a saved download, not for a saved request: a `304` counts against the limit, A3), *a larger `MemoryMax`* (hides a leak; where the controller exists the guard should trip early, and it does not exist on the reference image, A4).

### U13. Failure matrix and acceptance on the Pi

"Success and failure update verified on the Pi" (spec 14.2, G6; AT-011, AT-012, AT-013) will be shown in plan steps G6.9 and G6.10 with three classes of evidence. **No row below is verified; this is the plan.**

- **C** = automated test in CI (fixtures, fake root, fake systemd, fake GitHub).
- **D** = on the reference device with the production binaries and real releases signed with the production key.
- **T** = on the reference device with a test build of the helper that embeds a throwaway public key and offers fault hooks. The state machine code is identical; only the embedded key file and a build tag differ. A release build must not contain the hooks (CI checks the binary). **Owner decision (Q4):** class T is accepted for the failure rows. Evidence of class T is labelled as such and is **never** presented as evidence about the production key path. The success path (F19) and at least one genuine signed-release path per feature (F24 for components) must be class D.

| # | Case | How it is injected | Expected result | Class |
|---|---|---|---|---|
| F1 | Bad signature | Cache files signed with a throwaway key | `signature_invalid`; `current`, `releases/`, state unchanged; audit result `signature_invalid` | C, D |
| F2 | Signature valid, wrong checksum list (mix-and-match) | `checksums.txt` and signature of version A with the bundle of version B (both real) | `checksum_mismatch`; nothing changed | C, D |
| F3 | Wrong checksum | One byte of the bundle changed in the cache | `checksum_mismatch`; staging removed | C, D |
| F4 | Truncated download | Network cut or service killed during the download; truncated file placed in the cache | Service discards `.part`; helper `checksum_mismatch` for a placed file; GUI error; nothing changed | C, D |
| F5 | No space | Free space on the root file system reduced below the requirement | `no_space` before any change | C, D |
| F6 | Start failure | Signed release whose binary exits at once | Rollback within seconds (fail fast), old version healthy, result `rolled_back` | C (fake systemd), T |
| F7 | Health failure | Signed release whose binary runs but never answers `/healthz` correctly | Rollback after the 45 s deadline | C, T |
| F8 | Power loss while staging | Fault hook holds the phase, power removed; at least 3 repetitions | At boot `.stage.*` removed, state `idle`, old version running | T (and C for the logic) |
| F9 | Power loss in the verification window | Hold in `verifying`, power removed; at least 3 repetitions | At boot `current` back on `from`, result `rolled_back` reason `interrupted` | T (and C) |
| F10 | Replay of an old release | Real files of an earlier release placed in the cache | `downgrade_refused` | C, D |
| F11 | Same version | Files of the installed version | `already_installed` | C, D |
| F12 | Release changes the helper or a unit, before G6.10 | Real or throwaway-signed release with different helper bytes or unit text | `installer_required` with the list of names; GUI shows the installer instruction; nothing changed | C, D |
| F13 | Hostile archive or sizes | Signed bundle with a symlink, `..` path, extra file, oversized member, size lie | `manifest_invalid`; nothing left behind | C (throwaway key) |
| F14 | Damaged rollback target | Binary of the previous release altered after `activate` is accepted and before the rollback compares (the hash is recorded when `activate` starts) | `rollback_failed`, `needs_attention`, no switch to the damaged tree | C, T |
| F15 | Wrong peer | A different uid connects to the socket | Connection refused or closed, no state change | C, D |
| F16 | Concurrent apply | Second `apply` during an update | `busy`; one transaction | C, D |
| F17 | Helper not installed or stopped | Socket unit stopped | GUI shows the installer instruction; no state change | C, D |
| F18 | Offline, rate limited, wrong clock | Network off; fake `403` with reset header; wrong date | Typed GUI message; no loop; installed version still shown | C, D |
| F19 | Success | Real release N+1 offered, applied from the GUI | N+1 active, healthy, audit entries, previous version kept, `current` atomic | D |
| F20 | Cooldown survives the helper | Failed update and rollback; wait until the helper has exited (over 30 s); `activate` the same version, and any version within 60 s | `cooldown` both times, although the process is new | C (fake uptime), T |
| F21 | Cooldown across a reboot | Failed update, reboot, `activate` again | The marker is void or lasts at most its original 15 minutes; the observed behaviour is written down, not assumed | C, T |
| F22 | Staged release expires | `stage`, wait over 30 minutes (a fake uptime in C, once in real time on the device), `activate` | `stage_expired`; staged directory removed by `status`; GUI asks to start again | C, D |
| F23 | FIFO, symlink, directory, socket or growing file in the cache | Placed in the cache in place of each expected file | Typed error within the stage deadline, no hang, nothing left behind | C, D |
| F24 | Component replacement, success | Real signed release with a different helper binary and unit text, applied with both confirmations | New helper proven, `known-good` moved, release active and healthy, audit entries; units equal the release's | D |
| F25 | Unit lint reject | Units with each forbidden case of U15 rule 3 in a signed bundle | `unit_rejected` at `stage`; nothing changed | C, T |
| F26 | Key rule | Manifest declares a different `components.key_id` (pre-check); a helper that reports a different key id after the switch (post-check) | `key_changed`, GUI says to run the installer; the post-check case ends in `components_rolled_back` | C, T |
| F27 | New helper does not start | Signed helper binary that exits at once | `OnFailure=` runs the recovery from `known-good` at the first failed exit (no restart loop, no start-limit hit needed; A1), `components_rolled_back`; the old helper answers again | C (fake systemd), T |
| F28 | New helper answers a wrong protocol | Helper whose `status` reports a protocol outside the manifest's `components.protocol` | The new helper's self-check rolls the components back, `components_rolled_back` | C, T |
| F29 | Power loss in each component phase | Hold hooks at `c_switching`, after the link switch, in the new helper's first start, around the `known-good` switch; at least 3 repetitions per phase | Per the component table of U15 rule 4; no state with a link to an incomplete set | T (and C for the logic) |
| F30 | Damaged previous component copy | Alter a file of `known-good` before an update | `components_damaged`, no component transaction; at recovery `needs_attention` | C, T |
| F31 | Components first, release then fails | Release with a new helper and a binary that fails its health check | The release is rolled back, the new helper stays (it committed), the old service works with it; result `rolled_back` with `components_updated` noted | T |
| F32 | Missing component confirmation | `activate` without `components_confirmed` when components differ | `confirmation_required`; nothing changed | C |

**What the G6 closeout evidence must contain, in addition to the table:** the success runs F19 and F24 with timings; per-process `write_bytes` and peak `VmHWM` of service and helper during one update; `systemd-analyze security` of the helper units on the device; proof that the helper log and the audit contain no password, token, address or host name; and a list of what was not run. Non-verified combinations are not called Verified (spec 12.4).

Alternatives considered: *all failures with the production key* (needs a deliberately broken signed release in the public channel, which burns a version and is offered to every device; the owner chose class T, Q4), *CI only* (no proof on the hardware the specification names), *random power cuts* (not reproducible; the hold hooks make a phase hit on purpose, and a few unscripted cuts can be added), *`kill -9` instead of power loss* (proves the state machine, not durability; both are used and labelled).

### U14. Proposed split of the G6 implementation

Each step is its own session and pull request, with a stop point at which the owner reviews. No step starts before the previous one is accepted. **Gate G6 closes only after G6.10 is accepted, and G7 does not start before.**

| Step | Content | Stop point |
|---|---|---|
| G6.0 | This step: the owner accepted ADR 0010 with the owner decisions; the documentation PR records D-C53 to D-C63 and D-D12, aligns the specification (version 0.3.5) and adds ADR 0009 amendment A6. Docs only. | The owner says "merge" for the documentation PR. |
| G6.1 | Spike on the device, no product code: V1 to V4 (Raspberry Pi OS part), V6, V8, V10 to V13 on the device; V5, V7 and V9 are checked in G6.5, G6.9 and G6.2 (throwaway units and scripts, including the linked-unit layout (V10) and the restart interplay (V11)); writes the findings into the docs. | Owner confirms or changes U10 and U15 rule 3 from the findings. |
| G6.2 | Release tooling: bundle contract (helper binary and unit files, additive manifest fields), `bundlegen` checks and tests. No behaviour change on a device. | Dry-run release is green. |
| G6.3 | Installer: installs and removes the helper, its units and the link layout, lock, tests with the fake root, `systemd-analyze verify` in CI. The helper is still a stub that answers `status`. Delivered in two pull requests (A9): G6.3a the stub and the units, G6.3b the installer, the lock and the helper in the default bundle. Test item (A5): every wants link the installer creates is a relative link that points to the unit in `/etc/systemd/system` (fake root test), and, once on the device, a unit whose release directory was removed after a switch of `current` is still enabled, loads and starts at boot. | Owner runs the installer on the device: socket activation and hardening score observed. |
| G6.4 | Shared verification package (signature, checksums, manifest, SemVer, tar rules) with tamper, fuzz and replay tests ported from `scripts/test-signing.sh`; imported by helper and service. | CI green. |
| G6.5 | Helper core: protocol, peer check, `status`, `stage`, `discard`, staging, state file with the uptime markers, free space, the FIFO and symlink tests, the component comparison that answers `installer_required`. | Review of the privileged code. |
| G6.6 | Helper activation: `activate`, `ack`, health, rollback, boot `recover`, retention, cooldown, expiry, fault hooks (test build), crash tests for every row of the U7 table. | Review of the privileged code and of the tests. |
| G6.7 | Service side: GitHub client, check, download, advisory verification, helper client, routes, audit amendment A6 in code, `/healthz` version and `/readyz`, the transitional installer instruction. Fake GitHub and fake helper tests. | Review. |
| G6.8 | Update page (Svelte): states, texts in `de` and `en`, accessibility, errors, the transitional text. | Owner reviews the UI. |
| G6.9 | Hardware evidence of U13 (rows that do not need G6.10) on the reference device; documentation (`docs/UPDATE.md`, `INSTALL.md`, `RELEASING.md`, `ARCHITECTURE.md`). | Owner reviews the evidence; G6 stays open. |
| G6.10 | Replacement of the helper and its units by a GUI update (U15): unit lint, component transaction, known-good copy and recovery, key rule, protocol rule, second confirmation in the UI. Needs from G6.5 and G6.6: the staging with a component subtree, the extensible write-ahead state machine, the single `systemctl` function with its argument-shape test, the fault hooks and the crash-test harness. | Evidence: F24 with the production key (class D); the other new rows F25 to F32 with class C and T as marked; the closeout evidence list of U13. **Owner decides whether G6 passes; G7 waits for this.** |

Alternatives considered: *one G6 pull request* (privileged code that cannot be reviewed in one sitting), *helper after the service* (the service cannot be tested against a real peer), *UI first against a mock helper* (the states the UI must show come from the helper), *folding G6.10 into G6.6* (the owner separated it so that the base update can be reviewed and evidenced first), *skipping the spike* (V1, V10 and V11 can change the design; finding that out after the code is the expensive way).

### U15. Replacement of the helper and its units by a GUI update (step G6.10)

**Owner decision (Q1, Q2).** The helper and its allow-listed units can be replaced from the GUI as part of a release update, with their own confirmation. This section designs it. It is implemented in G6.10; until G6.10 is accepted the transitional limit of U5 and U9 applies (`installer_required`). Rules 1 to 10 each give a recommendation (decided design), the reason and the rejected alternatives.

**Rule 1. The closed list stays closed (extension of `activate`, no new request).**
- *Recommendation:* the component update is part of the `activate` transaction. `stage` reports whether the helper files or `oarfish.service` differ (by SHA-256, U5, A13); `activate` takes the boolean `components_confirmed` (U2). No new request type, no argument that names a path, unit, program or package. The compiled-in lists of requests, programs and argument shapes, unit names, component file names and write targets are the table in U2, and the test that compares them with that table stays and grows by the new rows. Because this changes the closed list of U2 (it adds `daemon-reload`, `reset-failed oarfish-update-helper.service`, the component write targets and the four unit names; A2 removed the third `systemctl` shape, `restart oarfish-update-helper.socket`, that the accepted text listed here), **this ADR is the amendment**; any further change needs another amendment.
- *Reason:* one transaction, one write-ahead state, one audit trail. A second code path to the same files would double the places where a path can be wrong.
- *Rejected:* a new request `components_update`; a request that takes a list of file names; a generic "install file" operation (all create an argument that names a file, which is what makes a broker).

**Rule 2. What may be replaced.**
- *Recommendation:* exactly five files, confirmed against the list of what the installer will install (U11): the helper binary `oarfish-update-helper` (reached as `/usr/libexec/oarfish-update-helper`) and the unit files `oarfish.service`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`. `oarfish.service` travels with the release tree and `current`; the other four form the component set. Nothing outside `/opt/oarfish` is written; the links in `/usr` and `/etc` are the installer's.
- *Reason:* the smallest set that makes the helper and the service definition updatable, and the only files a release is allowed to change that run with more than the service's privileges.
- *Rejected:* replacing arbitrary units (a unit is code that runs as root); replacing files the installer creates for other purposes (users, directories); letting a release add files to the component set.

**Rule 3. Unit lint, and where the units live.**
- *Recommendation (lint):* a unit text inside a signed release is checked against a compiled-in rule set before any link can point to it, at `stage` and again at `activate`. The form is two-fold, because the units differ in what they may change:
  - the **three helper units** (socket, service, recover) must equal a compiled-in **canonical form** after removing comments, blank lines and the values of `Description=` and `Documentation=` (printable, at most 200 characters). Any other difference is `unit_rejected`. Since A15 each unit has an ordered list of at most two accepted forms (D3), and A15.3 prints the forms, the complete table for `oarfish.service` and the parsing rules as they are implemented. The canonical form of `oarfish-update-helper.service` contains the lines `SuccessExitStatus=75` and `RestartForceExitStatus=75` (A1, U10);
  - **`oarfish.service`** must contain a compiled-in **required baseline** (each of these exactly: `User=oarfish`, `Group=oarfish`, `ExecStart` equal to the exact path `/opt/oarfish/current/oarfish` with the exact argument shape `-listen` followed by a literal loopback IP address and port, `NoNewPrivileges=yes`, empty `CapabilityBoundingSet=` and `AmbientCapabilities=`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, the kernel and control-group protections, `RestrictNamespaces=yes`, `RestrictSUIDSGID=yes`, `RestrictAddressFamilies` limited to `AF_UNIX AF_INET AF_INET6`, `SystemCallFilter=@system-service`, `SystemCallArchitectures=native`, the state, cache and runtime directory directives, `UMask=0077`; the baseline is the set of directives that `install/oarfish.service` of G6.2 sets, taken over verbatim) plus **allowed extras** from a closed list with a value validator for each (for example `Description`, `Documentation`, `Restart` limited to `on-failure` and `always`, `RestartSec` and `TimeoutStopSec` within bounds, further `Protect*` and `Restrict*` directives with restricting values only). **Every directive that is not listed is rejected** (fail closed), in particular any `Exec*` other than the one `ExecStart`, any `+`, `!`, `@` or `:` prefix on a command, `Environment*`, `User`/`Group` other than expected, `SupplementaryGroups`, `ReadWritePaths`, `BindPaths`, `Capability*` other than empty, `PrivateUsers`, `AppArmor*`, `Conflicts`, `OnFailure` and every dependency directive not on the list.
  - **Parsing:** UTF-8, at most 16 KiB, no NUL, no line continuation (a trailing backslash is rejected), only the sections `[Unit]`, `[Service]`, `[Socket]` and `[Install]` as the unit type allows, `key=value` lines and comment lines, no specifiers (`%`), a duplicate key is rejected unless the rule table marks it as repeatable (none is). The `[Install]` section may contain only `WantedBy=` with the expected target.
  - **A new legitimate directive** (a future release needs one the rules do not know): ship a release that updates the helper first (with the new rule tables, and units that still pass the old rules), then a release with the unit. The rules in force are those of the helper that stages. This two-step rule is accepted.
  - **Tests (G6.10):** every unit file in the repository's `install/` directory passes the lint, so the rules and the shipped units cannot drift apart (CI); a table test with one rejected case per forbidden construct above, per removed baseline directive and per weakened value (`ProtectSystem=full`, `NoNewPrivileges=no`, a capability, `User=root`, another `ExecStart` path, `ExecStartPre`, a `+` prefix, a continuation line, a duplicate key, an unknown section, a specifier, a 17 KiB file, a NUL byte); a fuzz target for the parser.
- *Recommendation (layout):* **the unit files stay inside the trees and are linked into systemd** (Option L), not copied to `/etc/systemd/system` (Option C). `oarfish.service` is a link to `/opt/oarfish/current/oarfish.service`, so a switch of `current` and a rollback move the unit atomically with the code. The helper's own units cannot depend on `current`, because a broken release must not break recovery at boot: they live in the **component tree** `/opt/oarfish/components/<id>/`, switched by two links that `current` does not control. `active` serves the socket and service units and the helper binary; **`known-good`** serves the recover unit and the binary it runs, and moves to the new set only after the new helper has proven itself (rule 4). The previous set is kept as well. So at boot the recover unit runs a helper that was proven healthy, whatever `current` and `active` point to.
- *Reason:* one link switch moves a whole set atomically (no per-file journal, no half-replaced set); the helper needs write access to one directory (`/opt/oarfish`) instead of three, and no write access to `/etc` or `/usr`, which is the narrowest hardening available (rule 8); a rollback is a link switch plus `daemon-reload`.
- *Rejected:* **Option C, copying units to `/etc/systemd/system` and the binary to `/usr/libexec`:** needs write access to two directories shared with unrelated files, a per-file journal for a set of five files and a rename per file; a half-copied set is possible after a power failure. It stays the documented **fallback** if verification V10 (systemd follows links from `/etc/systemd/system` into `/opt`, `daemon-reload` picks up a changed link target, linked units start at boot) fails; its hardening and residual risk are in the U10 table. **V10 did not fail on the reference device (G6.1, device observation), so Option L stays and Option C stays the unused fallback (owner decision after the spike).** *Also rejected:* putting the recover unit under `current` (a broken release would break boot recovery); a single component link (the recover unit would run an unproven helper after a half-finished switch); exact-match lint for `oarfish.service` (every harmless tuning would need a helper release); an allow-list only for the helper units (they are the units that run as root).
- *Residual risk (honest):* the lint is an allow-list and fails closed on what it does not know, but it can only judge text. It does not bound what a validly signed helper **binary** does when it runs as root, and it cannot foresee every interaction of systemd directives; the baseline, the exact paths and the closed list of extras are what reduce that. A drop-in in `/etc/systemd/system/<unit>.d/` belongs to the administrator and is not checked or touched.

**Rule 4. Order, atomicity and rollback of a transaction that also changes components.**
- *Recommendation:* components **first**, then the release; the components have their own commit point and the release is rolled back alone if it fails. The new service may need the new helper (`requirements.helper_protocol`), and the old service must keep working with the new helper (rule 6), so the helper goes first and stays.
- *The steps* (state phases in brackets; every phase is written and `fsync`ed before its action):
  1. `activate` accepted, preflight as in U6, plus the hashes of the known-good set compared with the ones recorded at its commit (`components_damaged` if they differ). [`c_switching`, with `components_from`, `components_to`, the recorded hashes and the flag `socket_restart_pending` when the socket unit text differs.]
  2. The old helper switches the link `components/active` to the new set (temporary link and `rename`, `fsync` of the directory) [`c_switched`], runs `systemctl daemon-reload`, and exits with code 75. It does not execute the new binary and does not restart itself.
  3. systemd restarts the service because of `RestartForceExitStatus=75`; `SuccessExitStatus=75` makes the exit clean, so `OnFailure=` does not start the recovery (A1: with `Restart=on-failure` alone the recovery would start and switch `active` back before the restarted helper runs). The restart starts the **new binary**: socket activation alone would only start it at the next connection, and the new helper must prove itself now. The running instance never replaces itself: it finishes its part and exits; the next process is the new one.
  4. The **new helper proves itself** at its first start, before it serves anything: its own version equals `components_to`'s (after A13 the helper carries no release version; how this check is made is an open point of A13 for G6.10), it implements a protocol inside the manifest's `components.protocol` and the protocol range of the old helper's requests, its embedded key id equals the key id the old helper recorded (rule 5), it reads `state.json`, and it answers `status`. [`c_verified`] It then switches `components/known-good` to the new set and writes [`c_committed`]. If the socket unit text changed it records `socket_restart_pending`.
  5. The new helper continues the same transaction with the release phases of U5 and U6 (`switching`, `verifying`, `committed`) under the **new** helper's code. If the release fails, only the release is rolled back; the components stay, because they committed and work with the old service.
  6. **The helper does not restart its own socket (A2).** `socket_restart_pending` stays a status flag in `state.json`: `status` reports it until the next boot or installer run, because a changed socket unit text takes effect only then: `daemon-reload` does not apply a changed socket text to a running socket, the socket keeps its text until it is restarted, and a socket cannot be started or restarted while its service is active (device observation, G6.1, A9). The installer therefore stops `oarfish-update-helper.service` first and then restarts `oarfish-update-helper.socket`. The release notes of a release that changes the socket unit text say so. Device observation (G6.1, V13): `systemctl restart oarfish-update-helper.socket` from inside the running service fails and leaves the socket stopped, and `restart --no-block` does not work either; a restart from a separate oneshot unit started by `OnSuccess=` worked, but is rejected because it adds a unit name and the helper cannot create unit links (`EROFS` measured). A client retries once on `ECONNRESET` or `ENOENT` (U2).
  7. Retention: the old component set is kept as the previous one; sets older than that are removed through `.trash.<id>`.
- *Failure handling:* a new helper that **fails its self-check** rolls itself back: it switches `active` back to `known-good`, runs `daemon-reload`, writes `components_rolled_back` with the reason and exits with code 75; the old helper starts again. A new helper that **does not start** or **crashes** is handled by the recovery at its first failed exit: `OnFailure=oarfish-update-recover.service` runs the recovery from `known-good`, which sees an uncommitted component transaction, switches `active` back, runs `daemon-reload`, runs `reset-failed oarfish-update-helper.service` and writes the result. A new helper that **hangs** is ended by `RuntimeMaxSec=900` and takes the same path. **Device observation (G6.1, V11, A1)** for how these units behave together, replacing the accepted sentence that a non-starting helper ends in a restart loop that `StartLimitBurst=3` stops:
  - `OnFailure=` starts the recovery at the **first failed exit** of the helper (a start failure, a crash or `RuntimeMaxSec`), **not** after a start-limit hit. Nothing loops, because the recovery acts after the first failure.
  - Exit code 75 is a **clean exit that forces the restart** (`SuccessExitStatus=75`, `RestartForceExitStatus=75`), so a deliberate hand-over does not start the recovery.
  - **Every restart counts against `StartLimitBurst=3` per 10 s**, so one transaction may restart the helper at most twice within 10 s (the hand-over to the new helper and, after a failed self-check, the hand-back to the old one); a third restart within the window ends in `start-limit-hit` and the recovery.
  - `daemon-reload` takes about 3 s and a recovery run about 3.5 s on the reference device; the restart after a failed exit waits for the recovery run.
  - `serve` and `recover` are **serialised by the exclusive `flock` on the state directory** (U7, U8) and not by timing or by unit ordering: the recovery can start while the restart of the helper is already pending, and the lock decides who goes first. The recover unit stays as in U1, without any ordering against the helper service.
  - A **damaged previous copy** is detected before the switch by the recorded hashes; at recovery it is `rollback_failed` with `needs_attention`.
- *Table: where power can fail in the component steps* (the rows of U7 apply to the release steps that follow):

  | # | Step | Interrupted here | State on disk | Recovery at the next start |
  |---|---|---|---|---|
  | c1 | New component set moved into `components/<id>`, no state yet | after the rename | `components/<id>` complete, not referenced | `recover` removes component directories that neither link nor the state names. |
  | c2 | `c_switching` written | before the link switch | `active` = `known-good` | Clear the intent; the staged release follows the rules of U7 row 4. |
  | c3 | `active` switched | at or just after the rename | `active` names the old or the new set | Old: as c2. New with an uncommitted state: `recover` switches `active` back to `known-good`, runs `daemon-reload`, result `components_rolled_back` reason `interrupted`. |
  | c4 | `c_switched`, `daemon-reload`, exit 75, new helper starting | any time | `active` = new, `known-good` = old, state `c_switched` | Same as c3. The units loaded at boot come from `active`; recovery sorts that out with the `daemon-reload` after its switch, before the socket starts (`Before=`). |
  | c5 | New helper verified, `known-good` switched, `c_committed` not yet durable | between the switch and the write | `active` = `known-good` = new, state `c_verified` | Both links name the new set: recovery treats it as committed and writes `c_committed` (the disk is the truth). If the `known-good` switch did not persist: as c3. |
  | c6 | `c_committed` written, release phase not started | any time | new components committed, old release | The release transaction is `aborted`, the components stay; the administrator can apply the update again. |
  | c7 | `socket_restart_pending` set (A2: no socket restart exists) | any time | everything committed, flag set | Nothing to do; the socket is created from the new text at the next boot or installer run (the installer stops the helper service and restarts the socket, A9), and `status` reports the flag until then. |
  | c8 | Components rollback (`c_rolling_back`) | during the switch back | `active` old or new | Ensure `active` = `known-good`, compare the recorded hashes; equal: `components_rolled_back`; different: `rollback_failed`, `needs_attention`. |
  | c9 | Pruning old component sets | during deletion | `.trash.*` left | `recover` removes them. |

- *Reason:* the whole set moves with one `rename`; the proof of the new helper happens in the new helper itself before it is made the known-good one; the recovery path never depends on anything that is not yet proven; the helper stays within the closed list because it executes nothing new beyond two more fixed `systemctl` shapes (A2 removed the third).
- *Rejected:* running the new binary as a child of the old helper for a self-test (a second executable path for downloaded code; the real start is the better test); replacing the helper after the release (a new service could meet an old helper); rolling the components back when the release fails (more link switches and restarts for no gain, the compatibility rule makes it unnecessary); restarting the socket unit during the transaction (it would stop the service that holds the transaction); keeping one copy only (no way back).

**Rule 5. Trust key rule: a GUI update does not change the trust key in G6.**
- *Recommendation:* the signed manifest declares `components.key_id`, the key id embedded in the helper it delivers (U4). The helper refuses a component update whose declared key id differs from its own with `key_changed` (GUI text: run the installer, key rotation is not available in the GUI yet). The new helper repeats the check at its first start against the key id recorded by the old one and rolls back with reason `key_changed` if they differ. **This is a deliberate limit until the key-rotation decision D-D12 is made, and it can be lifted only by a later ADR.**
- *Reason:* a key change is a change of the root of trust; it must not happen as a side effect of an update, and its design (U4, list of questions) is not done.
- *Rejected:* allowing a key change signed by the old key (that is a rotation design); comparing key files on disk (the key is embedded, not a file); trusting the declaration alone (the post-check in the new helper exists because a declaration can be wrong). Note: the key rule protects against an accident or an unplanned rotation, not against the holder of the trusted key, who is trusted by definition.

**Rule 6. Protocol and state compatibility during the transition.**
- *Recommendation:* (1) a helper implementing protocol P accepts requests with `v` in {P−1, P}; (2) `status` is the one request that stays answerable for every `v` ever released and reports the helper's accepted range, so the service learns the range first and then speaks a version both support; (3) the manifest carries `requirements.helper_protocol` (the lowest protocol the release's service needs) and `components.protocol` (what the delivered helper implements), and the helper refuses before the transaction a release whose requirement the helper it will have cannot meet (`helper_too_old`); (4) `state.json` changes are additive and the older helper ignores fields it does not know (N−1 rule of U6), so a rolled-back helper can read what the new one wrote. In G6, P is 1 and the range is {1}.
- *Reason:* in one transaction the old service meets the new helper (after step 2) and the new service meets the new helper (after the release switch); a rolled-back release meets a newer helper. Accepting the neighbouring protocol covers all of these.
- *Rejected:* an exact-match protocol (breaks the old service in the middle of a transaction); negotiation inside every request (more code in the privileged decoder); no versioning (the failure would be an obscure `bad_request`).

**Rule 7. GUI: explicit confirmation of the component update.**
- *Recommendation:* in addition to the fresh password (Q3), the component update has its own confirmation dialog (U9). The text says that **privileged components will be replaced**, names which ones from the list `stage` returned (the update helper, its units, the system service definition of Oarfish), says that the service restarts and everybody is signed out, and says what happens if the update fails (rollback, and that a new helper stays). While G6.10 is not accepted, the same place shows the transitional rule: this release needs one installer run, with the exact command. There is no automatic component update and no setting for one.
- *Reason:* a rare, privileged change should not be clicked through as part of the routine one, and the administrator should read what is replaced.
- *Rejected:* one combined confirmation; a checkbox "also update components" (a default that invites a click); a silent update of the helper.

**Rule 8. Hardening for the component update.**
- *Recommendation:* the helper needs write access to **`/opt/oarfish` and its state directory only**. The link layout of rule 3 makes that sufficient, and the table of U10 states it, the fallback (three directories) and the residual risk: whatever is protected below `/opt/oarfish` is protected by the compiled-in name list, the lint and the hash checks, not by the sandbox. `/etc/systemd/system` and `/usr/libexec` stay read-only for the helper in the primary layout. The additional privileges are two fixed `systemctl` argument shapes (`daemon-reload`, `reset-failed oarfish-update-helper.service`; A2 removed `restart oarfish-update-helper.socket`); the architecture test of U2 lists them.
- *Reason:* the narrowest systemd settings that still allow the transaction.
- *Rejected:* `ReadWritePaths=/etc/systemd/system` (fallback only); `CAP_DAC_OVERRIDE`; `CAP_SYS_ADMIN` for the systemd calls (V1 decides; it is not granted silently).

**Rule 9. Failure matrix rows.** F12 and F24 to F32 in U13 cover: component replacement success, unit lint reject, `key_changed` reject, new helper does not start, new helper answers a wrong protocol, power loss in each new phase, damaged previous helper copy, the release failing after the components committed, and the missing confirmation.

**Rule 10. Plan step G6.10, and what the GUI path adds and removes.**
- *Scope:* the unit lint with its rule tables, the component transaction and the recovery of rule 4, the `known-good` and `active` links, the key and protocol rules, the second confirmation in the UI and the tests above; the installer layout it relies on comes from G6.3. *Needs from G6.5 and G6.6:* staging with a component subtree, an extensible write-ahead state machine, the single `systemctl` function with its argument-shape test, fault hooks and the crash-test harness. *Stop point and evidence:* U14 and U13; class D for F24 (a real signed release with the production key that changes the helper bytes and the unit text), class T for the injected faults, each row labelled. Gate G6 closes only after G6.10 is accepted, and G7 does not start before.
- *What the GUI path adds, honestly:* a route by which a validly signed helper binary can become root code on a device after a password and two confirmations, without the administrator reading a script. *What it removes:* the need for a shell session for every helper or unit change, and the drift between service, helper and units on devices that never rerun the installer. *What it does not change:* the same signing key is the root of trust for the installer path and the GUI path, and an attacker needs the download channel in both. The leaked-key analysis is T7.

## Traceability

| Requirement | Decision | How it is met (proposal) |
|---|---|---|
| ORB-UPD-001 detect releases through GitHub Releases, GUI shows installed and available version | U3, U9 | User-initiated check against the release list; the update page shows both versions. |
| ORB-UPD-002 administrator confirms in the GUI, no automatic upgrade | U9, U15 rule 7 | Confirmation dialog plus fresh password; a second confirmation when components change; no timer, no policy, no background download. |
| ORB-UPD-003 staging, then versioned release directory; active version unchanged until activation | U5 | `.stage.<id>` then `rename` to `releases/<version>`; `current` untouched until `activate`. |
| ORB-UPD-004 atomic activation through `current` | U5, U7 | Temporary symlink and `rename` over `current`, directory `fsync`; table row 6. |
| ORB-UPD-005 service and health endpoints are checked after activation | U6 | Four-part health criteria with stability window and fail-fast. |
| ORB-UPD-006 automatic rollback to the previous healthy version | U6, U7, U15 rule 4 | Helper-owned rollback with hash-checked target; boot recovery; cooldown that survives the helper; component rollback through `known-good`. |
| ORB-UPD-007 helper manages Oarfish releases only | U1, U2, U10, U15 | Closed request list, no paths or unit names as arguments, fixed program and argument shapes, fixed write targets below `/opt/oarfish`, unit lint, `ReadWritePaths=/opt/oarfish`. The helper and its units are part of the signed Oarfish release (spec 8.6, version 0.3.5); the wording of the requirement is unchanged. |
| ORB-UPD-008 no Tailscale needed | U3 | Anonymous HTTPS to GitHub only; test F18 and the install/update test without Tailscale. |
| ORB-SEC-001 core and web process unprivileged | U1, U10 | The service gets no privilege and no relaxation; only a socket connection. |
| ORB-SEC-002 privileged operations through a narrow broker | U2 | **Not met by G6 and not claimed.** The broker is G7 (D-C28). The helper is a separate, narrower mechanism and sets the boundary. |
| ORB-SEC-005 restrictive file modes | U3, U8 | `state.json` 0600 in a root-only 0700 directory; the world-readable cache files are public artifacts, stated as such. |
| ORB-SEC-006 administrative actions are audited | U8, U9 | `update_started` and `update_finished` in the audit file (A6), helper phases in the journal, shared correlation ID. |
| ORB-SEC-007 no secrets in audit and logs | U8 | The helper handles no secret; error codes instead of error strings; redaction test. |
| ORB-REL-002 commit, version, architecture, minimum requirements, SHA-256 in the manifest | U4 | Fields used and enforced; three additive fields (`requirements.min_installed_version`, `requirements.helper_protocol`, `components`); kernel and memory minimums still absent (conflict C3). |
| ORB-REL-003, ORB-INS-004 (supporting) | U4 | Signature verified before use, ADR 0005 / 0006 format. |
| AT-011, AT-012, AT-013 (supporting) | U13 | F19, F6/F7, F1/F2/F3. |
| R-05 unstable Wi-Fi during update (supporting) | U3, U7 | Full download before activation, atomic link, rollback. |

## Threat model

Assets: the root-owned code path (helper, release trees, unit files), the integrity of the running version, the availability of the GUI, the administrator's trust in the update page.

| # | Attacker or fault | Threat | Mitigation in this design | Residual risk |
|---|---|---|---|---|
| T1 | Code execution in the unprivileged service (a web-layer bug) | Use the helper to become root | Closed requests, no path or program arguments, helper builds paths itself, content must be validly signed, units linted, helper never executes release code, `os.Root` confinement, copy-then-verify, size caps | The attacker can trigger an update to a genuine newer release without a click (the helper cannot see the click; `components_confirmed` is an interlock, not a boundary), and can cause restarts at the rate the cooldown allows. A newer release is signed and N-1 compatible. If that release replaces components, they are the project's signed ones. **Residual risk (G6.1 amendment):** `components_confirmed` is set by the unprivileged service, and the fresh password (Q3) and both confirmations (U9, U15 rule 7) are checked only there. A compromised service therefore skips them and can ask the helper to activate any genuinely signed, strictly newer release, including one that replaces the helper and its units, without a password and without the second confirmation. The two confirmations protect against a stolen or left-open session, not against a compromised service. The design is unchanged: the helper still verifies everything itself. See T7 and T17. |
| T2 | Same, tampering with files between check and use | Swap the bundle after the check | The helper copies and hashes the bytes it then uses; verification applies to its private copy | None known. |
| T3 | Same, symlink, FIFO or path tricks in the cache | Make the helper read or write elsewhere, or block it | Fixed templates, validated version, `os.Root`, `O_NONBLOCK`/`O_NOFOLLOW`, regular-file check, stage deadline, `O_EXCL` | Depends on `os.Root` behaving as documented (V5). |
| T4 | Same, hostile archive | Path traversal, links, devices, bombs | Allow-listed names, regular files only, manifest sizes and hashes, extraction caps | None known. |
| T5 | Network attacker (MITM, DNS, captive portal) | Serve a bad release | HTTPS, allow-listed hosts, signature and hash checks in the helper, strictly-newer rule | Can block updates or hide new ones (freeze); cannot install anything. |
| T6 | Replay of an old, validly signed, vulnerable release | Downgrade | Candidate must be strictly newer than the installed version, which only root can change | A downgrade by an administrator with a shell remains possible by design (recovery workflow). |
| T7 | Stolen or leaked release signing key | Ship a malicious signed release (R5: honest analysis) | **Without control of the download channel a key alone reaches no device**: both the installer and the updater pull from the release page, and the updater needs a user-initiated check (no automatic update). **With the channel**: a malicious release runs as the sandboxed `oarfish` user after an ordinary update (health check and rollback do not detect malice). It becomes **root code** only in two cases: the administrator runs a malicious installer (which carries the same key and is the existing path; the script can be read first), or the administrator confirms the component update of U15 after the password and a dialog that names privileged components. U15 narrows what a signature can do (unit lint, exact paths, `key_changed`, two confirmations, no automatic component update) but cannot bound a signed helper binary. | The GUI path is a way to root code that the old "installer only" design did not have; the difference to the installer path is that the administrator does not read the new binaries (U11, U15 rule 10). Rotation is not designed (D-D12). The mitigation that remains is the administrator's attention at the second dialog and the channel's integrity. The dialogs and the password are checked only in the unprivileged service, so they do not stop a compromised service (residual risk of T1). |
| T8 | Compromised GitHub account or release page | Replace assets | Cannot sign; same as T5 | Release notes are unsigned text (next row). If the attacker also holds the key: T7. |
| T9 | Hostile release notes from the API | Script injection in the GUI | Notes are shown as plain text only, length limited, control characters removed, labelled "from GitHub, not signed" | Misleading but harmless text. |
| T10 | Other local user | Talk to the helper, read state | Socket mode and `SO_PEERCRED`; state directory 0700 root | Another process running as `oarfish` (a future plugin) can reach the socket (U2, G8). |
| T11 | Power loss, SD fault | Half-installed release or component set, wrong pointer | U7 and U15 tables, write-ahead state, conservative rollback, hash-checked rollback targets | Cheap cards may lose data despite `fsync`; not designed (row 12). |
| T12 | Bad release (crash, hang, data format) | Brick the GUI | Fail-fast health, rollback, N-1 compatibility rule | `rollback_failed` needs a shell (U6). |
| T13 | Resource exhaustion on the Pi | Memory or CPU starvation of the dashboard | Streaming with 32 KiB buffers, a soft Go memory limit, `Nice=10`, hash before download, caps; `MemoryMax=64M` is in the unit text but not enforced on the reference image (A4) | Unmeasured until G6.9 (`VmHWM` of the helper); no kernel-enforced memory limit on the reference image. |
| T14 | Installer and GUI update at the same time | Inconsistent tree | Shared lock (U7, U8, U11): the installer takes an exclusive `flock` on `/var/lib/oarfish-update` and waits at most 30 s (implemented in G6.3b); the helper takes the same lock (G6.5) | Until the helper takes the lock (G6.5, G6.6), an installer run and a GUI update are not serialised from the helper side; no GUI update exists before G6.7. Installer-against-installer runs are guarded from G6.3b on. Not yet verified on a device. |
| T15 | Wrong clock (no battery-backed clock, **Assumption**) | TLS failures; misleading audit times | TLS failure is a typed message; the helper never uses wall-clock time for security or for cooldowns | Audit times may be wrong until the clock is set. |
| T16 | Information leakage | Host names, addresses, serial numbers in logs, state, audit or this repository | No such value is read, stored or logged; errors are codes | Journal lines of the system and of GitHub's servers are outside Oarfish. |
| T17 | A validly signed but malicious or broken helper binary or unit text | Root code execution, or a helper that cannot recover | Unit lint (fail closed) for the text; for the binary: signature, hashes, ELF check, `key_changed`, the new helper's self-check, `known-good` kept until the new one proved itself, `OnFailure` rollback at the first failed exit (A1), hash-checked previous set | The lint does not bound a signed binary. A malicious signed helper runs as root inside the sandbox of its units (empty capability set, `ReadWritePaths=/opt/oarfish`) until the administrator notices; it is the same trust as T7. The password and both confirmations are checked only in the unprivileged service, so a compromised service can request such a release without them (residual risk of T1). |
| T18 | Lint bypass through systemd semantics the rules did not foresee | A unit that passes the lint and still widens privilege | Allow-list, fail closed on unknown directives, exact paths, baseline from the shipped unit, two-step rule for new directives, tests per forbidden construct, fuzzing | New systemd versions add directives; unknown ones are rejected, but an allowed directive could interact in a way nobody tested. |
| T19 | A compromised service abuses the cooldown or the staged release | Restart loop; activation of an old staged tree | Cooldown in `state.json` on a boot-time clock, 60 s gap, 15 min per version, 30 min expiry, reboot removes staged trees | The cooldown may reset after a reboot (U6, accepted). |
| T20 | A hostile file type in the cache | Block the helper (FIFO), read elsewhere (link) | `O_NONBLOCK`, `O_NOFOLLOW`, `os.Root`, regular-file check, 180 s stage deadline | None known. |

## Alternatives considered (summary of rejected approaches)

Always-running daemon, on-demand oneshot, setuid binary, `.path` request files, D-Bus (U1); loopback HTTP, command-line requests, generic RPC, a separate component request (U2); helper downloads, in-place verification in the cache, `memfd` handoff, streaming into the helper, opening cache files without `O_NONBLOCK` (U3); `minisign`/`openssl`/TUF library, trusted-comment binding, manifest counter, API prerelease flag (U4); extracting in place, keeping all releases, second rollback pointer, unlimited stage lifetime (U5); service-reported health, watchdog-only, GUI decides, wall-clock cooldown (U6); resume at boot instead of rollback, no intent record, A/B partitions, `syncfs`, per-file journal (U7); state under `/var/lib/oarfish`, audit written by the helper (U8); no password, extra activate click per staging, one combined dialog (U9); `PrivateNetwork=yes`, broad capabilities, `DynamicUser=`, dropping `ProcSubset=pid` (U10); installer-only helper replacement as a permanent rule, a replaced helper without lint (U11 and U15); hard CI gates for budgets, buffering, background checks (U12); production-key-only failure tests, CI only, random power cuts, `kill -9` only (U13); one big pull request, helper after service, UI first, folding G6.10 into G6.6, no spike (U14); copying units to `/etc` as the primary layout, a single component link, exact-match lint for `oarfish.service`, a child process self-test, releasing components only after the release, restarting the socket mid-transaction (U15).

## Consequences

- The release bundle, the manifest, `bundlegen`, the installer, `docs/INSTALL.md` (old releases are pruned, a helper exists, the unit becomes a link), `docs/RELEASING.md` (bundle contents, N-1 rule, the new manifest fields) and `docs/ARCHITECTURE.md` (the "No privileged code exists" paragraph) change when G6 is implemented. This ADR changes none of them.
- A new root-owned program exists for the first time. Its code is small, standard library only and reviewed in steps (G6.5, G6.6, G6.10); the helper and its units can be replaced by a signed release after two confirmations (U15), which adds the route to root code described in T7 and T17.
- The update state is not in `/var/lib/oarfish`; the specification text and D-C47 are corrected in version 0.3.5 (C1).
- The layout of `/opt/oarfish` gains a component tree and the installer creates links in `/usr/libexec` and `/etc/systemd/system`; the specification's file layout (8.2) was **not** extended for this in version 0.3.5, because verification V10 could still change it; V10 held in G6.1 (device observation) and version 0.3.6 aligns it (C10).
- A power failure in the verification window can roll back a good update (U7). A release that changes the helper or a unit cannot be installed from the GUI until G6.10 is accepted, and never when it changes the trust key (U15 rule 5). A new unit directive takes two releases (U15 rule 3).
- After a failed update the helper may be newer than the release (U15 rule 4); the protocol rule makes this safe.
- `/healthz` gains a version member and `/readyz` finally exists (U6).
- `rollback_failed` is the one state that needs a shell. A manual rollback button, key rotation (D-D12), LAN access and HTTPS (D-D10), plugin isolation from the helper socket (G8) and OS package updates (ORB-OSU) are outside this ADR.
- Gate G6 now has the sub-steps G6.0 to G6.10 and closes only after G6.10; G7 waits (`docs/ROADMAP.md`, D-C63).

## Amendments after the G6.1 spike

Step G6.1 ran on the reference device (one Raspberry Pi Zero 2 W, Raspberry Pi OS Lite 64-bit, Debian 13, systemd 257, kernel 6.18) with throwaway units; the production service was not changed. The project owner reviewed the result at the stop point of G6.1 and confirmed the amendments below in chat. They are recorded as D-C64 in `docs/DECISIONS.md`. The decisions D-C53 to D-C63, the status of this ADR and the owner decisions Q1 to Q7 are not changed.

**Evidence label.** Every measured value below is a *device observation, throwaway units, one device, one image, one moment, 2026-11-02*. It is one reading, not the production path, and it is not a verification of the production path. The support status of the reference device stays Compatible; nothing here changes it. The write-up of the spike is evidence, not a normative document, and is not linked from here.

**Convention (as in ADR 0009).** The accepted decisions are not rewritten wholesale. Each amendment states the accepted wording, the new rule and where the text was changed. Where an accepted statement was wrong, the text was changed in place and carries the marker "A1" to "A8" or "Device observation (G6.1)"; the table "Changes made in place" lists every such passage.

**A1. Exit code 75, `OnFailure=` and the start limit (amends U10, U15 rules 3 and 4, V11; owner decision).** Accepted wording: `Restart=on-failure` is what makes the helper's exit code 75 work, and a new helper that does not start ends in a restart loop that `StartLimitBurst=3` within 10 s stops, after which `OnFailure=` runs the recovery. New rule: the helper service gets `SuccessExitStatus=75` and `RestartForceExitStatus=75`; `Restart=on-failure`, `RestartSec=1`, `StartLimitIntervalSec=10`, `StartLimitBurst=3` and `OnFailure=` stay. Both new lines are part of the lint canonical form of the helper service (U15 rule 3). `OnFailure=` starts the recovery at the first failed exit (start failure, crash, `RuntimeMaxSec`), not after a start-limit hit; exit 75 is a clean exit that forces the restart; every restart counts against `StartLimitBurst=3` per 10 s, so one transaction may restart the helper at most twice within 10 s; `daemon-reload` takes about 3 s and a recovery run about 3.5 s on the reference device. `serve` and `recover` are serialised by the exclusive `flock` on the state directory (U7, U8) and not by timing or unit ordering. The recover unit stays as in U1. Observed: with `Restart=on-failure` alone, a deliberate exit 75 started `OnFailure=`, and the recovery switched `active` back before the restarted helper ran, so the new binary never ran. The combination with the two new lines was observed with throwaway units that had the helper's restart settings and sandbox; it has not been tried with the real helper or the real recover unit.

**A2. The helper does not restart its own socket (amends U15 rule 4 step 6 and the closed list of U2, V13; owner decision, variant a).** Accepted wording: as its last action the helper runs `systemctl restart oarfish-update-helper.socket`, and the next connection starts the helper again. New rule: the helper does not restart the socket; the argument shape is removed from the closed list of U2, so the shapes that U15 adds to the closed list are two (`daemon-reload` and `reset-failed oarfish-update-helper.service`), not three. `socket_restart_pending` stays a status flag in `state.json`. A changed socket unit text takes effect at the next boot or installer run (A9 corrects how: `daemon-reload` alone does not apply it to a running socket), and the release notes of a release that changes the socket text say so. `systemctl restart --no-block` does not work; an `OnSuccess=` oneshot unit worked but is rejected because it adds a unit name and the helper cannot create unit links (`EROFS` measured). A client retries once on `ECONNRESET` or `ENOENT`; an accepted connection is not affected. Observed: a restart of the helper's own socket from inside the running service fails (`Socket service ... already active, refusing`) and leaves the socket stopped, so the next connection gets `ENOENT`; a connection waiting in the backlog gets `ECONNRESET`. How `socket_restart_pending` is cleared after the next boot or installer run is not decided here and belongs to G6.10.

**A3. GitHub rate limit and downloads (amends U3, U12, V3; owner decision).** Accepted wording (assumption): answers `304` to a matching ETag do not count against the anonymous limit. New rule: a `304` counts against the limit of 60 requests per hour per source address; the ETag saves bandwidth, not quota. A request to `/rate_limit` is free. Asset downloads go from `github.com` to `release-assets.githubusercontent.com`; the allow-list of U3 (`github.com` and `*.githubusercontent.com`) covers both. The request budget of U12 is unaffected. Also observed: `releases/latest` answered `404` while every release was a prerelease. Not tested: whether the limit is shared by every device behind one address.

**A4. `MemoryMax=64M` is not enforced on the reference image (amends U10, U12, T13; owner decision).** Accepted wording: `MemoryMax=64M` as a hard guard. New rule: `MemoryMax=64M` stays in the unit text and is not enforced where the memory cgroup controller is absent; the Raspberry Pi OS kernel command line has `cgroup_disable=memory`. `TasksMax=16` works. Memory protection must come from the program (streaming, 32 KiB buffers, `GOMEMLIMIT`), and the helper peak is measured with `VmHWM` in G6.9. The kernel command line is not changed (specification 13.3). An allocation test of the limit was not run; the statement rests on the controller list and the command line.

**A5. Enabling linked units (amends U5, U11 item 3, U15 rule 3, V10; owner decision).** Option L (links from `/etc/systemd/system` into `/opt/oarfish`) stays; Option C stays the unused fallback. `systemctl enable` writes a wants link to the resolved release directory, so the installer creates the wants link itself as a relative link (`../oarfish.service`). A dangling wants link did not break loading or boot (observed). G6.3 gets a test item for this (U14). The on-device test item was observed at the G6.3 owner stop, see "Device observations at the G6.3 owner stop" below.

**A6. Start limit and journal (amends U6, U8, V6; owner decision).** The stock start limit is not reached by `Restart=on-failure` with `RestartSec=2` (20 restarts in 45 s, no `start-limit-hit`), so the health criteria rely on `NRestarts`. `journald` is volatile on Raspberry Pi OS Lite, so the persistent record is `state.json` and the service audit entry.

**A7. Cache directory modes (amends U3, U10; owner decision).** The installer creates `/var/cache/oarfish` with 0750 and `CacheDirectory=` makes it 0755. The helper reads the cache as uid 0 without `CAP_DAC_OVERRIDE`: a 0644 file is readable, a 0600 file is not.

**A8. Clock (amends U6, T15, V8; owner decision).** The reference device has no RTC and no `fake-hwclock`; systemd restores the clock from its timestamp file. A cold start was not tested.

**T1. Residual risk of a compromised service (clarifies T1, T7 and T17; no design change; owner decision).** `components_confirmed` is set by the unprivileged service, and the fresh password and both confirmations are checked only there. A compromised service can therefore request any genuinely signed, strictly newer release, including one that replaces the helper and its units. The helper still verifies everything itself; T7 and T17 point to this sentence.

**Layout and plan wording (owner decisions).** U10 and U15 rule 3 keep Option L (A5). The V table of this ADR carries the G6.1 verdict and the evidence label for V1, V2, V3, V4, V6, V8, V10, V11, V12 and V13; V5, V7 and V9 are unchanged and V4 stays "not verified on Ubuntu". The G6.1 row (U14, `docs/ROADMAP.md`) says: V1 to V4 (Raspberry Pi OS part), V6, V8, V10 to V13 on the device; V5, V7 and V9 are checked in G6.5, G6.9 and G6.2. The specification follows in version 0.3.6: section 8.2 lists the component tree and notes that the journal may be volatile (conflict C10 of this ADR is resolved by it).

**Not covered by G6.1 and not decided here.** Ubuntu Server (V4 and everything else). A cold power cycle for the clock. The order `Before=oarfish-update-helper.socket oarfish.service` of the recover unit at boot (the spike had no such socket and no production ordering was allowed): observed at the G6.3 owner stop, see below; the G6.9 evidence run then showed an ordering cycle on a cold boot, corrected by A14. Socket activation together with `Restart=` and `OnFailure=` on one helper unit: observed at the G6.3 owner stop, see below. V5, V7 and V9.

### Changes made in place

| Section | Accepted wording | Now | Amendment |
|---|---|---|---|
| Header, How to read, intro | The ADR carried no G6.1 results; "Nothing in this ADR is verified on hardware" | One header line, the label "Device observation (G6.1)", and "as the production path" added; status line and Gate line unchanged | all |
| U2, list of programs executed | Three added `systemctl` shapes, including `restart oarfish-update-helper.socket` | The socket restart shape is removed | A2 |
| U2, after the request-format bullet | No client retry rule | A client retries once on `ECONNRESET` or `ENOENT` | A2 |
| U3, rate limit | Assumption: `304` does not count | Device observation: `304` counts; ETag saves bandwidth only; `/rate_limit` free; sharing between devices stays an assumption | A3 |
| U3, bounds on every request | Allow-list only | Added the observed redirect chain `github.com` then `release-assets.githubusercontent.com` | A3 |
| U3, cache; U10, cache modes | Modes chosen without observation | Added installer 0750 versus `CacheDirectory=` 0755 and the 0644 versus 0600 reading | A7 |
| U3, alternatives (`latest`) | Assumption, recalled | Device observation: `404` while all releases are prereleases | A3 |
| U5, layout | V10 could still change the layout | V10 held, Option L stays; new bullet on wants links | A5 |
| U6, health criteria; cooldown | No statement on the stock start limit; clock was an assumption | Added the start-limit observation; added the RTC and `fake-hwclock` observation | A6, A8 |
| U8, `state.json`; journal assumption | `socket_restart_pending` without a role; journald persistence unknown | Flag is status only; journald is volatile | A2, A6 |
| U10, unit text | `Restart=on-failure` and `RestartSec=1` | Plus `SuccessExitStatus=75` and `RestartForceExitStatus=75` | A1 |
| U10, text after the unit block; relaxation row | `Restart=on-failure` makes exit 75 work; V11 an assumption | Refuted and replaced; observation for the row | A1 |
| U10, new bullet; U12, helper memory; T13 | `MemoryMax=64M` as a hard guard | Not enforced on the reference image; protection from the program | A4 |
| U11, item 3 | Enable the socket and the recover unit | The installer creates relative wants links | A5 |
| U12, check interval, time, alternatives | Assumed limit; "a few seconds"; guard trips early | Observed limit with `304` counted; `daemon-reload` about 3 s, recovery about 3.5 s; guard absent on the reference image | A3, A1, A4 |
| U13, F27 | Restart loop ends at the start limit, then `OnFailure=` | `OnFailure=` runs at the first failed exit | A1 |
| U14, G6.1 row; G6.3 row | G6.1 "verifies V1 to V13"; no wants-link test | The split of V1 to V13 between G6.1, G6.2, G6.5, G6.9; wants-link test item | owner decision 10, A5 |
| U15 rule 1; rule 8; rule 4 reason | Three more `systemctl` shapes | Two | A2 |
| U15 rule 3, canonical form; Option C note | Canonical form without exit-status lines; Option C is the fallback if V10 fails | The two lines are in the form; V10 held | A1, A5 |
| U15 rule 4 step 3, step 6, failure handling, row c7 | Code 75 works through `Restart=on-failure`; socket restarted as last action; restart loop ended by `StartLimitBurst`; flag cleared live | Rewritten as A1 and A2 describe | A1, A2 |
| T1, T7, T17 | Residual risk did not say who sets `components_confirmed` | Residual-risk sentence in T1; pointers in T7 and T17 | T1 |
| T13 | `MemoryMax=64M` as a mitigation | Marked not enforced on the reference image | A4 |
| Consequences; C10 | Spec 8.2 aligned after G6.1 | Version 0.3.6 aligns it | A5 |
| Assumptions to verify | Four columns | Fifth column with the G6.1 verdict; V11 and V13 marked as refuted original wording | owner decision 9 |
| U1, U10, recover unit text | The recover unit had no start timeout; U1 said a failing recovery never keeps Oarfish from starting | `TimeoutStartSec=120`, shown in the unit text; a hanging recovery cannot hold up the start either | A9 |
| U10, socket unit text | The printed socket text had no `[Install]` section | `[Install]` with `WantedBy=sockets.target` | A9 |
| U15 rule 4 step 6, row c7; A2; U11 item 3 | A changed socket text takes effect at the next boot or installer run, `daemon-reload` suggested to apply it | `daemon-reload` does not apply a changed socket text to a running socket; the installer stops the helper service and restarts the socket (device observation) | A9 |
| U11 item 4 | Create nothing for the state, `StateDirectory=` does it | The installer creates `/var/lib/oarfish-update` (root:root, 0700) because it takes the lock there first | A9 |
| T14; U14 G6.3 row | Shared lock proposed; G6.3 as one step | Installer side of the lock implemented in G6.3b, helper side with G6.5; G6.3 delivered in two pull requests | A9 |

## Amendment after G6.3a

Step G6.3a (the helper stub and the three units, merged) showed four places where the accepted text and the units disagree or are silent, and step G6.3b (the installer) needs the answers before it writes code. The project owner decided them in the prompt of G6.3b. They are recorded as D-C66 in `docs/DECISIONS.md`. The decisions D-C53 to D-C65, the status of this ADR and the owner decisions Q1 to Q7 are not changed; the specification is not changed, because none of its normative statements changes (section 8.2 names the state directory and the links, not the ownership steps or the unit options).

**Evidence label.** The two statements about systemd below that rest on a device reading carry the label of the G6.1 amendments: *device observation (G6.1), throwaway units, one device, one image, one moment, 2026-11-02*. They are not Verified. The fake-root tests of G6.3b prove the installer's order of calls, not systemd behaviour.

**A9. Recover unit timeout, socket restart by the installer, the state directory, the socket `[Install]` section (amends U1, U10, U11 item 4, U15 rule 3 and rule 4 step 6, the A2 sentence, T14; owner decision D-C66).**

(a) *Recover unit timeout.* Accepted wording: `oarfish-update-recover.service` is `Type=oneshot` and ordered `Before=oarfish.service`; a failing recovery never keeps Oarfish from starting. New rule: the unit gets `TimeoutStartSec=120`. Reason: a oneshot unit has no start timeout by default, so a hanging recovery could hold up the start of Oarfish at boot, which contradicts the statement. After the timeout systemd marks the unit failed, and `oarfish.service` still starts, because `Before=` is an ordering and not a requirement. The line is shown in the unit text in U10. The lint canonical form of the recover unit (U15 rule 3) gets this line in G6.10; until then the repository check `scripts/check-units.sh` requires it.

(b) *Socket text and `daemon-reload`.* Accepted wording (U15 rule 4 step 6, repeated in A2, row c7 and by U11 as the installer's `daemon-reload`): a changed socket unit text takes effect at the next boot or installer run, the installer running `daemon-reload`. New rule: `daemon-reload` does not apply a changed socket text to a running socket; the socket keeps its text until it is restarted, and a socket cannot be started or restarted while its service is active. The installer therefore stops `oarfish-update-helper.service` first and then restarts `oarfish-update-helper.socket` (or starts it when it was not active). It does so when the helper bytes or the socket unit changed, or when the socket is not active. **Device observation (G6.1), not Verified:** a restart of the socket while its service was active was refused and left the socket stopped (see A2); that a changed text stays unapplied after `daemon-reload` is the owner's reading of the same G6.1 session, given with this amendment; the spike write-up is not linked from here and was not re-read for it. The helper still never restarts its own socket (A2).

(c) *State directory.* Accepted wording (U11 item 4): "create nothing for the state, `StateDirectory=` does it". New rule: the installer creates `/var/lib/oarfish-update` (owner root:root, mode 0700), because it takes the shared lock there (U7, U8, T14) before the helper has ever run. `StateDirectory=oarfish-update` and `StateDirectoryMode=0700` in the helper service and the recover unit stay and agree with it. An existing directory with another owner or mode is security relevant and makes the installer stop.

(d) *Socket `[Install]` section.* The socket unit has an `[Install]` section with `WantedBy=sockets.target` (G6.3a added it; U10 did not print it). The installer enables the socket by creating the relative wants link itself (A5). The helper service and the recover unit are unchanged in this respect (the helper service has no `[Install]` section; the recover unit is wanted by `multi-user.target`).

(e) *Delivery.* G6.3 is delivered in two pull requests: G6.3a (the stub and the units, merged) and G6.3b (the installer, the lock, the helper in the default release bundle). The owner stop of G6.3 (the owner runs the installer on the device: socket activation and hardening score observed) comes after G6.3b. The installer side of the lock is implemented in G6.3b; the helper side (the `flock` of `serve` and `recover`, U7) comes with G6.5. Nothing in G6.3b was verified on a device when this amendment was written; the observations of the owner stop are recorded in the subsection below.

### Device observations at the G6.3 owner stop

Label for every statement here: **device observation (G6.3 owner stop, 2026-11-03): one Raspberry Pi Zero 2 W, Debian 13 (trixie) aarch64, kernel 6.18.50, systemd 257, one image, one moment.** The owner ran the steps and reported the results. What was installed: a test release `0.1.0-alpha.6.1` (components shape, update helper stub) built from the merge commit of pull request 25, signed with a throwaway key and installed with a test copy of `install.sh` that differs from the production installer only in the key, the allowed download protocol and the base-URL override. It was never published. The production key path with the real installer was not exercised there. The support status stays Compatible; nothing is Verified. No decision of this ADR is changed by this subsection. The same observations, with more detail, are in `docs/INSTALL.md` ("Results on the reference device (G6.3)").

1. *Fresh components install over a `v0.1.0-alpha.6` device (regular unit file).* The layout came out as designed: `components/<id>` with relative links active and known-good, the `/usr/libexec` link, four unit links, `oarfish.service` turned from a regular file into a link into `current`, three relative wants links, `/var/lib/oarfish-update` `root:root` 0700, modes and owners as specified. No `enable` call. The hash of the account file was unchanged; the GUI login with the old password worked after the install and after a reboot.
2. *Socket activation.* The socket was active (listening) with mode 0660 `root:oarfish` in a root-owned 0755 directory; the helper service was inactive until the first query. A query as the user `oarfish` returned the `status` answer with `key_id` `11f1c52777090bfc` and the installed version. The helper was active right after, inactive after 35 s (idle exit) and started again by the next query. The same query as an ordinary user got `Permission denied`; as root the connection was closed without an answer.
3. *Hardening.* `systemd-analyze security` on the device: both helper units 1.7 OK, equal to the offline value in CI; `oarfish.service` 1.7 OK as well.
4. *A5, wants links.* After `current` moved from `0.1.0-alpha.6` to `0.1.0-alpha.6.1` and the old release directory was removed, `daemon-reload` and a reboot left all three units enabled, loaded and started. Older release directories (`alpha.2` to `alpha.5`) were still there: the installer keeps old releases.
5. *Boot order.* After that reboot (monotonic timestamps) the recover unit exited about 42 ms before `oarfish.service` started and about 54 ms before the socket became active; it needed about 650 ms; result `success`. Total boot 40.9 s against 41.3 s before, no noticeable delay.
6. *Same installer again.* Nothing changed and nothing was restarted (same PID, same activation timestamps).
7. *`SIGKILL` of the running helper (extra observation).* The service failed with result `signal`; `OnFailure=` started the recover unit, which finished within the same second; one second later `Restart=on-failure` started the helper again without a connection (restart counter 1). The socket stayed active and the next query was answered.
8. *Uninstall without `--purge`.* It removed the `/opt/oarfish` tree, the cache, the helper link, the units and the wants links, and kept `/etc/oarfish`, `/var/lib/oarfish`, `/var/lib/oarfish-update` and the user; no failed units; the account hash was unchanged. The official `v0.1.0-alpha.6` installer then restored the original state.

**Not exercised on the device:** `--purge`; a first installation without any previous release; the installer rollback after a failed health check; the production-key path; Ubuntu; another Pi model; a cold power cycle; a crash loop up to the start limit and a helper that does not start; two installer runs at the same time (tested only against the fake root); a helper that holds the lock (the stub takes none).

## Amendment after G6.5

Step G6.5 (the helper core: `stage`, `discard`, `status`, the lock, the cache access, the staging recovery) showed places where the accepted text and the behaviour of the Go standard library differ, and places where the ADR is silent. The project owner accepted the gap rules and the implementation decisions after the review of PR #28. They are recorded as D-C67 in `docs/DECISIONS.md`. The decisions D-C53 to D-C66, the status of this ADR and the owner decisions Q1 to Q7 are not changed; the specification is not changed. The statements below describe the code at the merge commit of PR #28.

**Evidence label.** Everything in A10 about the Go standard library and the file system was observed on the development machine (macOS) and on the GitHub-hosted Linux runner by the tests of the helper. Nothing is verified on a device, and `os.Root` is not verified on the kernel of the reference device.

**A10. Opening cache files, the limits of `os.Root`, the state member for the previous version, the one mutation seam (amends U3, V5, U8; owner decision D-C67).**

*A10.1 (U3, V5 corrected).* Accepted wording (assumption V5): opening a cache file with `O_NOFOLLOW` through `os.Root` refuses a link. New rule: on Linux the standard library's `Root.OpenFile` follows a link in the last path element that stays inside the root, even with `O_NOFOLLOW` (a link that leads out of the root is refused). The helper therefore opens each cache file with `openRegular`: `Lstat` (refuse a link, a FIFO, a directory, a device or a socket), open with `O_RDONLY|O_NONBLOCK|O_NOFOLLOW|O_CLOEXEC`, then check on the descriptor that it is a regular file and the same file as `Lstat` saw. These checks are defence in depth: `os.SameFile` compares device and inode and cannot detect inode reuse, and a link to the very file that was checked is accepted. The integrity boundary is the verification of the bytes read from the one opened descriptor, which is never opened again by name: other bytes fail the signed hashes. The same function reads `state.json`.

*A10.2 (U3).* `update/` and `update/<version>/` below the cache are checked with `Lstat`; a link there is refused (`not_found`) whatever it points to. The cache directory itself is opened with `os.OpenRoot`, which follows a link in the directory name; `/var/cache/oarfish` is created by the installer and `CacheDirectory=` as a directory.

*A10.3 (other `os.Root` limits).* (a) `Root` has no `fchmod` by descriptor and the standard library documents a race for `Root.Chmod` on Unix (the target replaced by a link while the call runs), so the helper opens the name and calls `fchmod` on the descriptor. (b) `Root.Mkdir` is reduced by the umask, so each directory gets an explicit `chmod` after it. (c) Files are created with `O_CREATE|O_EXCL` at 0600 and set to the final mode by `fchmod`, whatever the umask. (d) `O_NOFOLLOW` on the last element has no effect through `Root` (A10.1); the helper's trees are root-owned, so no unprivileged process can place a link there. (e) The free space that gates `stage` comes from `fstatfs` on a directory opened through the root (`releases/`); `status` reports the free bytes with `statfs` on the path of the tree, for display only. (f) A directory sync is `Root.Open` plus `Sync`. (g) An escape from the root is recognised by the text `path escapes from parent` of the error and mapped to `not_found`; the text is compared and never returned. **Test coverage:** at this commit no test produces that error (the `Lstat` check refuses a link before the root could report an escape, so the branch is reachable only by a swap between `Lstat` and open), and the mapping test lists no escape entry; see the finding in the pull request.

*A10.4 (U8).* U8 lists no member for the previous healthy version. `state.json` carries an explicit `previous_version` member (empty or a version matching the pattern of U2). The decoder is strict for the members it knows (type, range, pattern, phase and outcome names, no duplicate key, no key that differs from a known name only in letter case, at most 64 KiB, `schema_version` 1); unknown members are ignored at every level, as U15 rule 6 requires, and a missing member keeps its zero value.

*A10.5.* All mutating file operations of the helper go through the `fsops` interface (`Mkdir`, `Chmod`, `Create`, `Rename`, `RemoveAll`, `SyncDir`). `TestNoMutationOutsideFsops` parses the non-test sources and fails on any other call of a mutating function of `os` or `syscall`, of a mutating method of `os.Root` or `os.File`, and on an open for writing. The helper takes the lock with `open` and `flock` on the state directory; that creates and changes nothing on disk.

### Changes made in place

| Section | Accepted wording | Now | Amendment |
|---|---|---|---|
| U3; V5 | `O_NOFOLLOW` through `os.Root` refuses a link in the last element | It does not; `Lstat`, open, descriptor checks, and verification of the bytes read are the boundary | A10.1 |
| U3 | Links are refused | `update/` and `update/<version>/` checked with `Lstat` | A10.2 |
| U8 | The state members listed in U8 | `previous_version` added; strict decoder, unknown members ignored | A10.4 |
| U2, closed write targets | Writes only to the listed targets | Every write goes through one interface, checked by a source scan | A10.5 |

## Amendment after G6.6

Step G6.6 (activation, acknowledgement, health check, rollback, boot recovery, retention, cooldown, fault hooks) was merged as pull request 30. The review of that pull request produced five findings, and the owner adjusted the update notice before the service side (G6.7) starts. The project owner accepted the rules below on 2026-11-06 in the prompt of G6.7. They are recorded as D-C68 in `docs/DECISIONS.md`. The decisions D-C53 to D-C67, the status of this ADR and the owner decisions Q1 to Q7 are not changed. The specification is not changed: section 13.1 says that `/healthz` may report the version and describes `/readyz` in one sentence, and neither statement is made false or extended by A11 (the points where a reader could see a tension are listed under "Open points" below, for the owner).

**Evidence label.** This step is documentation only. No code, test, unit, installer or workflow was changed, and nothing in A11 is verified on a device. Statements about the helper's present behaviour are **Fact** and name the file they come from (`backend/cmd/oarfish-update-helper/`). Everything else is an **owner decision** (the wording of the prompt of G6.7, as the amendment lists them), a **proposal** (decided design, not implemented) or an **assumption**. Behaviour of the real GitHub service, of systemd and of the target kernel is not verified here.

**A11. Update notice, `/healthz` and `/readyz`, call timeouts and the job model, a known property of the state write, and two small items (refines U2, U3, U6, U9, U12; amends ADR 0009 E9 for the `pid` member; owner decision D-C68).**

*A11.1 Update notice (owner adjustment A4 of 2026-11-06; refines U3 and U9).*

- **Owner decision: a new route `GET /api/v1/update/notice`.** It is behind a valid session (ADR 0009 E9, D-C46). It triggers a check against GitHub only when the in-memory result is older than 24 hours; otherwise it answers from memory.
- **Owner decision: no timer and no goroutine that runs without a request, and nothing is written to disk.** The result lives in RAM only, so a restart of the service allows one new check.
- **Owner decision: offline or any other network error is silent in the notice answer.** The notice shows no error. The typed error stays available on the update page (`GET /api/v1/update`).
- **Owner decision: one shared in-memory result cache.** The notice and the update page check use the same cache. The U3 rules stay unchanged: the update page checks at most once per 60 minutes, "Check now" at most once per 60 s device-wide, and the rate-limit handling of U3 applies (no attempt before `Retry-After` or `X-RateLimit-Reset`, never a loop). The notice adds only the 24-hour limit for the page-view trigger.
- **Owner decision: the switch.** There is no settings store yet. G6.7 provides the route only. **The UI (G6.8) must not call the notice route and must not show a notice until a user-visible off switch exists** (a settings store, a later step). This is a known open point and is stated here plainly. **Note for G6.8:** once the UI uses the route, the user must be able to see that a request ran (a check against GitHub is outgoing traffic, spec 10.5).
- **Owner decision (D-C68, accepted 2026-11-06; the answer shape stays a G6.7 detail, proposal):** a failed attempt is kept in memory as the last attempt with its typed error. A notice request never starts an attempt less than 60 minutes after the last attempt of any outcome, and never while a rate-limit wait is running, so an offline device makes at most one attempt per hour from the notice, not one per page view. At most one check is in flight; a request that arrives meanwhile answers from memory. The notice answer carries the installed version and, when the cache holds a successful result, the available version and its channel (the data the page already shows); it has no error member. The exact shape is fixed in G6.7 and tested.
- **Owner decision (D-C68, accepted 2026-11-06):** a check that the notice starts is the one job of A11.3; when a job is running the notice answers from memory (it never answers `busy`).

*A11.2 `/healthz` and `/readyz` (refines U6, D-C57 and ADR 0009 E9).*

- **Owner decision: `/healthz` without a session answers `{"status":"ok","version":"<installed version>","pid":<number>}`**, where `pid` is the process ID of the answering service. Reason: the helper compares `pid` with the systemd `MainPID`, so that another process that listens on the loopback port cannot pass the health check (finding 1 of the G6.6 review). Justification for E9: the version is public (spec 10.3, D-C57); a process ID on the loopback interface is not device data in the sense of E9 (**assumption**: it is readable by any local user in `/proc` anyway; it is not a secret and gives no capability). The route stays public and exposes nothing else.
- **Fact (installer):** the installer's check only globs for `"status":"ok"` (`install/install.sh`, `HEALTH_URL`; ADR 0010 U6). It keeps working with the new body. **The installer is not changed.** G6.7 adds a test that feeds the new body to the same glob.
- **Owner decision: `/readyz` exists, needs no session and has the same minimal content style** (`{"status":"ok"}` when ready; when not ready, status 503 and a body without the member `"status":"ok"`, so the installer glob cannot match it by mistake, **proposal**). Its meaning follows spec 13.1: "Konfiguration, Profile und erforderliche lokale Ressourcen sind bereit".
- **Owner decision (D-C68, accepted 2026-11-06): what the three words map to.** Specification 13.1 gives categories, not a list, and the code has one counterpart for each: *Konfiguration*, the access store and the audit log were opened (the process does not listen otherwise, `backend/cmd/oarfish/main.go`); *Profile*, the embedded profile registry was loaded and the detection result exists; *erforderliche lokale Ressourcen*, the embedded frontend has its `index.html`. The checks are made in memory at request time and do no disk access. The owner accepted this reading of 13.1 on 2026-11-06.
- **Owner decision (D-C68, accepted 2026-11-06): the helper health check** (U6 criteria 3 and 4) additionally requires `pid == MainPID` from the same `systemctl show` reading, and a `200` from `/readyz` on the same loopback address.
- **Conflict found while writing this amendment, decided by the owner on 2026-11-06 (D-C68).** U6 applies "the same health criteria to the old version" after a rollback, and N-1 compatibility (U6) keeps the previous release as the rollback target. A previous release built before G6.7 does not report `pid` and has no `/readyz`. A helper that requires both unconditionally would therefore end every rollback to such a release in `rollback_failed`, and could not activate it either. **Owner decision:** the helper requires `pid` and `/readyz` only for a version that is at least a version constant compiled into the helper (the first release that carries them); older versions are checked with criteria 1 to 4 of U6 as they are today. The constant is set when that release is cut. `docs/RELEASING.md` has no checklist place for it yet, so it is listed here as an item for G6.7 (the constant, with a test that an older target gets the weaker check and a newer one the stricter) and for the release step (set the constant to the version being cut before tagging). The weaker check for an older rollback target is accepted because the impersonation the `pid` check closes needs another process on the loopback port at that moment, and the target is an older release the administrator already ran. The helper that requires `pid` is delivered by the installer until G6.10 (D-C54), together with the release that reports it. G6.7 implements this rule.
- **Owner decision (D-C68, accepted 2026-11-06):** `docs/AUTH.md` states the E9 exception ("the public `/healthz`, which says only `{"status":"ok"}`"). It is updated in the code pull request of G6.7, together with the code, because it describes what the service does and must not say more than the code does (D-C57 already says this).
- **Assumption, for G6.9:** the `pid` reported by `/healthz` equals the systemd `MainPID` of `oarfish.service`. It is checked on the device in G6.9 and is not measured until then.
- U6 keeps its sentence that a future TLS design (D-D10) must keep a loopback-reachable plain health route; it applies to `/readyz` as well.

*A11.3 Call timeouts and the job model (refines U2 and U9; finding 2 of the G6.6 review).*

- **Fact (`backend/cmd/oarfish-update-helper/txn.go`):** `activate` runs its pre-check (the installed version must be healthy, U6) synchronously while the helper holds its lock, for up to 10 s, before it answers "accepted". **Fact (U3):** `stage` has a request deadline of 180 s.
- **Owner decision: client timeouts of the service when it calls the helper:** `status`, `discard` and `ack` 10 s; `activate` 30 s; `stage` 200 s (the 180 s deadline plus 20 s for the connect and the answer).
- **Owner decision: HTTP handlers for `apply` and `check` never wait for the helper beyond a short bound.** `apply` starts a job in the service and returns at once; the page polls `GET /api/v1/update` every 2 s (U9). **Owner decision (D-C68, accepted 2026-11-06):** the short bound for `check` is 3 s; a check that is not finished by then answers "running" and the page polls.
- **Owner decision: at most one job at a time.** A second request answers `busy`. The job survives the end of the browser session (U9). A restart of the service before `stage` ends it harmlessly (nothing privileged happened); after `activate` the helper finishes alone.
- **Proposal:** the job has the steps check, download, advisory verification, `stage`, optional pause for the second confirmation (components, G6.10), `activate`, and the state is shown through `GET /api/v1/update`. Steps are named by a closed set; no Go error text reaches the answer or the log.

*A11.4 A known property of the state write (finding 4 of the G6.6 review).*

- **Fact (`backend/cmd/oarfish-update-helper/txn.go`, `run`):** `activate` first switches `current` to the new release and then saves the phase `verifying`. If that save fails, the helper returns without a restart: the running service is still the old process, the link `current` already names the new release, and the state still says `switching`. The helper logs the phase `verifying` with the result `io_error`.
- **Known property.** In that case `current` stays on the new version until the helper starts again and its recovery acts. **Recovery path (Fact, `recover.go`, `recoverSwitch`):** a transaction in the phase `switching` or `verifying` whose `current` names the new version is rolled back by the recovery that runs when the helper next starts, either as `serve` finds the unowned transaction (the helper exits after 30 s idle, so this is the next request), or as `oarfish-update-recover.service` at boot, before `oarfish.service` starts. Until then, a restart of `oarfish.service` by anyone would start the new binary without the health check of U6. The window is bounded by the helper's idle exit (30 s) and by the next boot, and it needs a failing write of the state file (a full or failing card) at that exact step. This is accepted as a property of the design (it is the "accepted cost" family of U7); it is not changed in G6.7, and it is a test-and-document item for G6.9. **Not verified on a device.**

*A11.5 Cosmetic and CI items (findings 3 and 5 of the G6.6 review).* Part of the scope of G6.7: (a) a constant instead of the string literal `"rolled_back"` in the phase switch of `recoverPhase` (`recover.go`); the value is the same as `outcomeRolledBack`, no behaviour changes; (b) the fuzz targets `FuzzDecodeActivate` and `FuzzDecodeAck` are added to the fuzz step of the CI workflow. The workflow is edited for this and for nothing else.

### Open points of A11

Decided by the owner on 2026-11-06 (D-C68), listed so that nothing is lost:

2. **`/readyz` reading: decided.** The mapping of the three words of spec 13.1 to checks (A11.2) is accepted; the checks are made in memory at request time, with no disk access.
3. **`pid` and spec 13.1: decided.** `pid` is an additional member; the specification is not changed and its version stays. If the specification should list the member later, that is a separate step with a new version.
4. **Rollback to a release without `pid`: decided.** The stricter check applies from a compiled-in minimum version (A11.2). The constant is an item for G6.7 and for the release step.
5. **Notice details: decided.** The 60-minute floor after a failed attempt, the notice never answers `busy`, and the 3 s bound for `check` handlers (A11.1, A11.3). The exact shape of the notice answer remains a G6.7 detail.
6. **`docs/AUTH.md`: decided.** It is updated in the code pull request of G6.7 together with the code.

Still open:

1. **Notice switch (owner decision, open).** The notice route exists in G6.7 but must stay unused by the UI until a user-visible off switch exists. No settings store is planned yet in `docs/ROADMAP.md`.

### Changes made to the accepted text

The accepted text of the sections is not rewritten; each row names the rule that now applies.

| Section | Accepted wording | Now | Amendment |
|---|---|---|---|
| U3 | Check on the update page at most once per 60 minutes and on "Check now" | Unchanged, plus the route `GET /api/v1/update/notice` with the 24-hour limit and the shared cache | A11.1 |
| U9 | Four routes | Five routes, the fifth is the notice route; the UI does not use it yet | A11.1 |
| U6; D-C57; ADR 0009 E9 | `/healthz` reports `status` and `version`; `/readyz` should be implemented | `/healthz` reports `pid` as well; `/readyz` is implemented; the helper requires `pid == MainPID` and `/readyz` | A11.2 |
| U2; U9 | No client timeouts, the transaction survives the session | Timeouts 10 s, 30 s, 200 s; one job at a time; handlers return at once | A11.3 |
| U7 | Row for a failed save of the phase | Documented as a known property with its recovery path | A11.4 |

## Amendment A12 (G6.8)

Step G6.8 built the update page on the routes of G6.7. Everything below is **Fact** from the code of the pull request of G6.8 (`frontend/src/pages/Update.svelte`, `frontend/src/lib/update.ts`, `frontend/src/lib/updateErrors.ts`, `backend/internal/server/update.go`) and its tests. Nothing in it is verified on a device or against the real GitHub service. The decisions D-C53 to D-C78 and the rules of U3, U9 and A11 are unchanged.

**A12. The update page (refines nothing; records the implementation of U9 and A11.1 to A11.3).**

- **What the page does.** System > Update (`#/update`, the first entry of the group System) shows the installed version, the state of the last check, its time and the earliest next page check; the available version with its channel, download size, the advisory signature status (with the sentence that the update helper checks again), the rollback sentence of specification 9.5 ("Die bisherige Version bleibt erhalten und wird automatisch wiederhergestellt, wenn die neue nicht funktioniert.") and the release notes as one plain text node labelled "Von GitHub, nicht signiert". There is no line for migrations: none exist (U9). "Aktualisieren" opens an inline confirmation with these facts, the sentence that the service restarts and everybody is signed out, and a password field; it sends `apply` with the version of the last check and the password. The password is only in the input and in the request and is cleared after every attempt.
- **Checks (U3).** The page sends `POST check {}` on opening only when `check.state` is `never` or `next_check_at` has passed. "Jetzt prüfen" sends `{"force":true}` and is locked in the browser for 60 seconds, and for `retry_after_seconds` after a rate limit; the service's device-wide rules of U3 apply unchanged behind it.
- **Polling (U9, A11.3).** `GET /api/v1/update` every 2 seconds, only while `job.running` is true or while the page waits for the restart after `activating`, and not after the page is left. Failed reads during or after `activating` show "Der Dienst startet neu" and are repeated for at most 3 minutes; then the page advises to open it again. A 401 goes to the login page through the existing session-loss path; after login the page shows `last_result`.
- **Transitional rule (U9, U15 rule 7).** On `installer_required` the page shows its own German or English fixed text: the release replaces privileged parts and cannot be applied from this page; download `install.sh` from the release page of the version, read it, run it on the device with sudo (`sudo sh install.sh`, a fixed text); settings and account are kept; the installation guide. It lists `installer.differs` by name. No command is built from API data, and the English `instruction` of the service is not shown.
- **Second confirmation (U9, U15 rule 7).** A minimal screen for the step `awaiting_components_confirmation`: a generic sentence (the State does not name the components), the restart sentence, what happens on failure, the password again, and `apply` with `components_confirmed: true`; "Verwerfen" sends `discard`. It is covered by a test with a fake and **cannot occur before G6.10**, because `stage` answers `installer_required` today. Whether the password is asked again at this point (open point C4) stays open for G6.10; the page keeps the present behaviour of the service, which asks for it.
- **Error texts.** Every error code of the closed set of `backend/internal/updater/errors.go` has its own German and English sentence, `http_<three digits>` shares one, anything else gets the generic sentence; `tls_failure` says to check the date and time of the device (U12). The server's English message is never shown.
- **Notice route (A11.1, D-C68).** The page does not call `GET /api/v1/update/notice`, and no badge or banner exists elsewhere. `frontend/src/shell.test.ts` fails if a frontend source names the route.
- **C2 fixed.** `apply` and `discard` read their body with a decoder of the update routes (`decodeUpdateBody`, sharing the code of `decodeOptionalBody`), so a body over 1 KiB answers `request_too_large` with the recovery "Send a smaller request." instead of the text of the login and setup routes. The access routes keep their text. Test: `TestApplyAndDiscardOversizeBodyRecovery`.
- **Development scenario mode.** With `?mock=<scenario>` in the Vite development server a fake fetch layer answers the session and update routes from fixtures, so every state can be reviewed without a device. The production build fails if the mode's marker is in the output (`frontend/vite.config.ts`).

**Not verified:** the page against a real service, helper or device; the real restart, its duration and the session loss it causes; Safari and Firefox; a screen reader; forced colours in a real browser. These belong to G6.9 or a later browser check. The page was checked in jsdom tests and in headless Chrome on one Mac against the scenario mode.

## Amendment A13 (task HB)

Everything below is **Fact** from the code and from builds made on one Mac with Go 1.27.3 (`backend/cmd/oarfish-update-helper/`, `scripts/build-helper.sh`, `scripts/release-bundle.sh`), unless it is labelled otherwise. The owner decision is D-C80. The decisions D-C53 to D-C79, the protocol, the requests, the state machine, the update service and the update page are not changed. Nothing in A13 is verified on a device.

**A13. The bytes of the helper do not depend on the release version (refines U5, U11 and U15; owner decision D-C80).**

- **Finding.** `stage` compares the SHA-256 of five files of the staged release with the installed ones: `oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service` (through `components/active`) and `oarfish.service` (through `current`); any difference answers `installer_required` with the names (`compareComponents`, `compare.go`). Up to this amendment `scripts/release-bundle.sh` built the helper with `scripts/build-helper.sh` and the release `VERSION` and `COMMIT`, which passed them as `-X` flags for `backend/internal/version`, and `main.go` set `helper_version` from `version.Version`. The helper therefore differed from release to release, and every real update would have answered `installer_required` for `oarfish-update-helper`. The unit tests did not show it because they stage fixed files. Measured before the fix (same commit, same toolchain): the helper built with `VERSION=0.1.0-alpha.8` had SHA-256 `849f98a8…49586c`, with `VERSION=0.1.0-alpha.9` `7e56f90d…6324`; in two complete bundles of these two versions the helper differed and the four unit files were identical.
- **Fix.** `scripts/build-helper.sh` passes no `-X` flag and reads neither `VERSION` nor `COMMIT`; the helper no longer imports `backend/internal/version` (`go list -deps` lists only `internal/relverify` and `internal/trustkey` besides the standard library, and `TestImports` forbids the version package). `helper_version` in `status` and in `state.json` is the constant `helperID`, `"protocol-1"`: it names the highest protocol the helper implements (`protocolMax`, the number the manifest states as `components.protocol`), and `TestHelperID` keeps the two equal. It is not a release version and does not have the form of one; it passes the existing validator of `helper_version` (printable ASCII, at most 64 characters), which is unchanged, so a `state.json` written by an earlier helper is still read and the next write replaces the value. `installed_version` in `status` still comes from the target of `current` (`installedVersion`, `handler.go`), not from the build. The main binary keeps its `-X` flags. The manifest, the bundle contents, the key id embedding (`components.key_id`) and the installer are unchanged.
- **Checks.** `scripts/check-helper-independent.sh` builds the helper with two different `VERSION` and `COMMIT` values and fails unless both files are identical; `scripts/check-reproducible.sh` runs it and then builds a third bundle with another `VERSION` and fails unless the five compared files equal those of the first bundle. CI runs both in the job "Reproducible bundle", and `make check-helper` runs the first. Both checks failed against the code before the fix.
- **What the unit files contain.** `oarfish.service` and the three helper units are copied from `install/` unchanged and contain no version; their bytes were identical across two bundles of different versions before and after the fix.
- **Consequence for devices on `v0.1.0-alpha.7`.** The helper published with `v0.1.0-alpha.7` embeds its release version (the string `0.1.0-alpha.7` is in the binary of the published bundle). A device on `v0.1.0-alpha.7` therefore gets `installer_required` for `oarfish-update-helper` for the next release and changes to it by the installer once.
- **Consequence from the first release built with this change.** An unchanged helper and unchanged units give byte-identical files, so `stage` finds no difference and no installer is needed. A change of the helper code, of a package the helper links (`relverify`, `trustkey` with its embedded key, the standard library, and so the Go version in `go.mod`) or of a unit text changes the bytes and needs the installer until G6.10 (U5, U11); that is intended. *Assumption:* the same Go version gives the same bytes on the Mac and on the GitHub-hosted runner, as `scripts/check-reproducible.sh` already assumes for the bundle.
- **Open point for the owner (G6.10, not decided here).** U15 rule 4 step 4 lets the new helper prove itself by checking that "its own version equals `components_to`'s". After A13 the helper no longer knows a release version, so this check cannot be made with `helper_version`. The other checks of step 4 (protocol, key id, state, `status`) and the hashes the old helper records are not affected. How the identity check of step 4 is made is a question for the design of G6.10.

### Changes made to the accepted text

| Where | Before | After | Source |
|---|---|---|---|
| U5, "releases that change the helper or a unit" | the files are compared, nothing says why an unchanged helper compares equal | adds that the comparison is of bytes and why an unchanged helper has the same bytes | A13 |
| U11, "replaced by a GUI update only under U15" | "a release that changes the helper or any unit needs an installer run" | adds that "changes" means the bytes, and the one installer run from `v0.1.0-alpha.7` | A13 |
| U15 rule 1 and rule 4 step 4 | "differ"; "its own version equals `components_to`'s" | "differ" by SHA-256; a pointer to the open point of A13 | A13 |

## Amendment A14 (G6.9 follow-up)

The device observation below was made in the G6.9 evidence run on the reference device (one Raspberry Pi, Raspberry Pi OS, systemd 257, one image, cold boot); it is a **device observation, not Verified**, and no host name or address is part of it. The CI observation was made on the GitHub-hosted runner (Ubuntu 24.04, systemd 255) on 2026-11-10. The owner decision is D-C81. The decisions D-C53 to D-C80, the helper, the socket unit and the helper service are not changed.

**A14. The boot order of the recover unit (amends U1 and U10; closes a note of "Not covered by G6.1"; owner decision D-C81).**

- **Device observation.** After a cold boot the journal shows `sockets.target: Found ordering cycle on oarfish-update-helper.socket/start`, the dependency chain `oarfish-update-recover.service/start -> basic.target/start -> sockets.target/start`, and `Job oarfish-update-helper.socket/start deleted to break ordering cycle`. The socket stayed inactive until it was started by hand, so the helper was unreachable after every reboot. `oarfish.service` itself started normally. The unit texts of `v0.1.0-alpha.8` and `v0.1.0-alpha.9` are identical, so both releases are affected.
- **Cause (Fact from the unit text and the systemd documentation).** The recover unit has `Before=oarfish-update-helper.socket oarfish.service` and, as a service, the default dependencies, which include `After=basic.target`. `basic.target` is ordered after `sockets.target` and `sockets.target` after the socket. `socket` before `sockets.target` before `basic.target` before the recover unit before the `socket` cannot be satisfied; systemd deletes a job to break the cycle and chose the socket.
- **New rule.** The recover unit gets `DefaultDependencies=no` and `After=local-fs.target` in `[Unit]`. `Before=oarfish-update-helper.socket oarfish.service`, `Type=oneshot`, `TimeoutStartSec=120`, `WantedBy=multi-user.target` and the whole sandbox are unchanged. `After=local-fs.target` is there because the unit needs `/opt/oarfish` and `/var/lib/oarfish-update`. No `Requires=`, `Wants=` or `Conflicts=` is added; the forbidden keys of `scripts/check-units.sh` are unchanged. The unit file carries a short comment that gives the reason.
- **What the unit loses without the default dependencies (analysis, not device-verified).** `Requires=` and `After=sysinit.target` and `After=basic.target`: the unit may now start before the rest of early boot has finished (swap, tmpfiles, `sysinit.target` services). It reads and writes only files below `/opt/oarfish` and `/var/lib/oarfish-update`, needs no network, no device node and no wall clock (the helper never uses the wall clock for security or cooldowns, T15), and local file systems are covered by `After=local-fs.target`; the mount dependencies that systemd derives from `StateDirectory=`, `ReadWritePaths=` and `PrivateTmp=` stay. Logging to the journal brings its own dependency on the journal socket. `Conflicts=` and `Before=shutdown.target`: at shutdown systemd no longer stops the unit by a conflict. For a `Type=oneshot` unit without `RemainAfterExit=` this does not matter in the normal case, because the unit is inactive again as soon as `recover` exits (about 650 ms in the G6.3 observation), so there is nothing to stop. A recovery that is still running at shutdown is bounded by `TimeoutStartSec=120`, by the stop of its mounts (the derived mount dependencies) and by the final kill; an interrupted recovery is the power-loss case that U7 and its crash tests already cover. Nothing else is needed, and nothing was added.
- **Detection in CI.** `scripts/verify-units.sh` now also runs `systemd-analyze verify --root=<temporary root> multi-user.target` in a root that holds the three units, `oarfish.service` and the wants links the installer creates, and fails on an ordering cycle in the output as well as on the exit status. Observed on the runner before the fix (the check alone, on the unchanged unit): `basic.target: Found ordering cycle on sockets.target/start`, `Found dependency on oarfish-update-helper.socket/start`, `Found dependency on oarfish-update-recover.service/start`, `Found dependency on basic.target/start`, `Job sockets.target/start deleted to break ordering cycle starting with basic.target/start`. `scripts/check-units.sh` requires the two new lines, and its mutation test removes and changes each of them. The single-unit `verify` that was in place did not see the cycle, which is why the boot transaction is verified.
- **Delivery.** The new unit text takes effect at the next boot after a `daemon-reload`, whichever way it reaches the device (the installer runs the reload). A release that changes this unit changes one of the five compared files (A13), so until G6.10 a device on an earlier release answers `installer_required` and needs one installer run. How a GUI update delivers a changed recover unit is a G6.10 question and is not decided here. The installer needs no change for A14.

### Changes made to the accepted text (A14)

| Where | Before | After | Source |
|---|---|---|---|
| U1, proposal sentence on the recover unit | `Type=oneshot`, `TimeoutStartSec=120`, no `RemainAfterExit`, `Before=oarfish-update-helper.socket oarfish.service`, `WantedBy=multi-user.target` | adds `DefaultDependencies=no` and `After=local-fs.target` | A14 |
| U10, recover unit text | `[Unit]` held only `Before=oarfish-update-helper.socket oarfish.service` | `[Unit]` holds `DefaultDependencies=no`, `After=local-fs.target` and the `Before=` line | A14 |
| "Not covered by G6.1", order of the recover unit at boot | observed at the G6.3 owner stop | adds that the G6.9 evidence run found an ordering cycle on a cold boot | A14 |

## Amendment A15 (G6.10, step a)

Step G6.10 is delivered in three implementation steps (A15.2, statement 3). This amendment belongs to step a: it records the owner decisions that the replacement of the helper and its units needs before the privileged transaction is written, and the unit lint of U15 rule 3 as it is implemented. The owner decisions D1 to D7 were given by the project owner with the prompt of step a on 2026-11-10 and are recorded as D-C83 in `docs/DECISIONS.md`. The decisions D-C53 to D-C81, the owner decisions Q1 to Q7 and the status of this ADR are unchanged; where an accepted sentence is refined, the table at the end names it. The specification is not edited: section 14.2 names for G6.10 the confirmation, the check of the unit files against compiled-in rules, the rollback of the helper and the key rule, and none of these statements changes.

**Evidence label.** Every statement below carries one of four labels. **Owner decision** is one of D1 to D7 or the split of the step, in the owner's wording as far as a table allows. **Fact** names the file it was read from, in the tree of the pull request of step a (`backend/cmd/oarfish-update-helper/`, `install/`). **Proposal** is a choice that the implementation made where U15, A1 to A14 and the owner decisions name a rule and leave its detail open; it is implemented and tested, and the owner's review of step a can change it. **Assumption** is not checked. **Nothing in A15 ran on a device, and nothing is Verified.** Step a changes no file in `install/`, no installer, no release and no behaviour of an update except the new answer `unit_rejected` of `stage`.

### A15.1 Owner decisions D1 to D7 (D-C83)

Each of D1 to D7 is an **owner decision**. Only D3, as far as the lint needs it, is implemented in step a; everything else in this table is implemented in step b or step c.

| # | Subject | Owner decision |
|---|---|---|
| D1 | Proof of the new helper without a release version (closes the open point of A13; replaces "its own version equals `components_to`'s" in U15 rule 4 step 4) | The proof is by bytes. The old helper records, with `c_switching`, the SHA-256 that the signed `contents` give for `oarfish-update-helper`. The new helper, started as `serve` while the state says `c_switched`, requires all of: the target of `components/active` is `components_to`; the SHA-256 of `components/active/oarfish-update-helper` equals the recorded one; the SHA-256 of its own executable image equals the recorded one. Any failure, including an own image that cannot be read, rolls the components back. No unit is widened for it and no release version enters the binary. |
| D2 | Delivery of a changed recover unit (closes the open point of A14) | No new mechanism. The four component files travel as one set. The recover unit link names `components/known-good`, so its text changes when `known-good` moves, after the new helper has proven itself. The helper runs `daemon-reload` after that switch; a changed ordering takes effect at the next boot. |
| D3 | Accepted forms (refines U15 rule 3, where "a compiled-in canonical form" is singular) | For each of the three helper units the helper carries a short ordered list of accepted canonical forms: the current one and, during a two-step change, the next one. Reason: in step one of the two-step rule the new helper must still accept the unit text that the repository ships, and every later release that keeps that text. The list has at most two entries per unit, and an entry is removed only by a later amendment or decision. In step a every list has exactly one entry. |
| D4 | `socket_restart_pending` (open since A2) | The flag is set when the normalised text of the socket unit changes (not for a change of comments, `Description=` or `Documentation=`). It is stored with the time since boot at which it was set, reported by `status`, and void when the current uptime is smaller than that value (a reboot happened). The installer is not changed (size budget); after an installer run without a reboot the flag stays until the next boot. |
| D5 | Protocol reading (U15 rule 4 step 4, rule 6) | A helper of protocol P stages a component set only if its `components.protocol` is P or P+1; otherwise `requirements_not_met`. `helper_too_old` stays for `requirements.helper_protocol`. `status` must be answered for every `v` and report the accepted range. |
| D6 | A difference of `oarfish.service` alone (U2, U15 rule 7) | It needs the second confirmation as well, but no component set and no component phase. The state therefore needs its own additive record of "a difference is staged, and which names", independent of `components_to`. The answer of `stage` gains an additive list of the differing names (the closed list) so that the dialog can name them. |
| D7 | Second confirmation (the open point C4 that A12 names) | The present behaviour stays: the password is asked again at the second stage. Whether that is wanted stays an open owner question. |

Statements that belong to the decisions and are not part of them:

- **Assumption (D1), to be observed on the device before the first release that carries the code:** the helper sandbox (`ProtectProc=invisible`, `ProcSubset=pid`) lets the process read its own executable image. It is not checked.
- **Fact (D1, `main.go`, `handler.go`):** the helper has no release version; `helper_version` is the constant `protocol-1` (A13). Nothing in step a reads the helper's own image.
- **Fact (D2, `install/install.sh`, `move_known_good`):** the installer moves `known-good` after its health check and runs `daemon-reload` when the recover unit text differs. D2 gives the helper the same order.
- **Fact (D4, `state.go`):** the state has the boolean `socket_restart_pending` and no member for the time since boot at which it was set; `resetTxn` keeps the flag; nothing sets it and `status` does not report it. The new member and the report are step b.
- **Fact (D4, `unitlint.go`, `normalisedUnit`):** the normalised text of D4 is the text that the lint compares with the accepted forms (A15.3), so the two rules use one function.
- **Fact (D5, `protocol.go`, `decodeRequest`):** today the decoder refuses every `v` outside `protocolMin..protocolMax` before it looks at `op`, so `status` with another `v` answers `bad_request`. `relverify.CheckHelperProtocol` (`backend/internal/relverify/policy.go`) compares `requirements.helper_protocol` with `components.protocol` and answers `helper_too_old`; no code compares `components.protocol` with the staging helper's protocol. Both changes are step b.
- **Fact (D6, `txn.go`, `admit`; `protocol.go`, `stageResponse`):** `activate` treats a non-empty `components_to` as "a difference is staged"; the answer of `stage` has `components_differ` (always false) and no list of names. Both changes are step b.
- **Fact (D7, ADR 0010 A12):** the page asks for the password again at the second confirmation, and the service checks it. Nothing changes in step a.

### A15.2 Three statements that follow

**1. When `installer_required` can still occur after step b.**

- **Fact (`compare.go`, `stage.go`):** every helper built before step b (the helpers of `v0.1.0-alpha.7` to `v0.1.0-alpha.10`, and the helper of step a) answers `installer_required` for any difference of the five compared files. No GUI update can change that on such a device, because the first release that carries the helper of step b differs from them in the helper's bytes. Every device therefore needs one installer run to reach that release. After it, the installed helper is the one of step b.
- **Proposal (for the review of step b, not decided here):** a helper that has the component transaction does not answer `installer_required` for a difference: a difference of the helper files selects the component transaction, and a difference of `oarfish.service` alone the second confirmation (D6). The code stays in the closed list of U2. The cases in which such a device still needs the installer have their own codes: a changed trust key (`key_changed`, U15 rule 5); a helper unit text that equals none of the accepted forms of the installed helper (`unit_rejected`, statement 2); a `components.protocol` outside P and P+1 (`requirements_not_met`, D5); a release in the legacy bundle shape (`requirements_not_met`, D-C67). The one-time bootstrap from a version without a helper stays an installer run (U11).

**2. What a release may change in the units.**

- **Fact (`unitlint.go`, `unitrules.go`; D3):** the helper of release N lints the units of release N+1 with the tables compiled into N. A release N+1 that is applied by the helper of release N may therefore differ from N in the three helper units only in comment lines, blank lines and the values of `Description=` and `Documentation=`, unless the helper of N lists the new form. In `oarfish.service` it may also differ in the allowed extras and in the order of its lines (A15.3).
- **Consequence for the class D row F24 of U13 (the row is not edited):** its "different unit text" is such a difference, or an allowed extra of `oarfish.service`. A release that changes a directive of a helper unit cannot be the F24 release unless the release before it carried the new form in its helper.
- **Fact:** a helper unit that differs in a directive answers `unit_rejected` at `stage`, also in step a, where the same release without the lint would have answered `installer_required`. The installer does not lint (`install/install.sh`), so an installer run still installs such a release. The update page shows the sentence of `unit_rejected`, which does not name the installer; the texts are step c.

**3. The split of G6.10.** **Owner decision:** G6.10 is implemented in three steps, each with its own session and pull request: (a) the decision record and the unit lint; (b) the component transaction and the recovery, with the links, exit code 75, the first start of the new helper, the key rule, the protocol change, the state members and the answer shape of `stage`; (c) the service, the page, the texts and the closing documentation. A release and the device evidence of U13 (F24 class D, the other rows as marked) follow after step c. The stop point of U14 for G6.10 is unchanged: the owner decides whether G6 passes.

### A15.3 The unit lint as implemented in step a

**Fact (`unitlint.go`, `unitrules.go`, `stage.go`).** The lint is part of the helper's `package main`, has no package of its own and adds no dependency (`TestImports` is unchanged). One function, `lintUnit(name, text)`, returns a typed result: accepted, or rejected with one reason from a closed set. The rule tables are constants of the program; no file, argument or manifest member changes them.

**Parsing.** U15 rule 3 gives the rules; where it is silent, the implementation takes the stricter reading (**proposal**), so that the lint never accepts a text that systemd reads differently from the lint.

| Rule | Source | Reason when it fails |
|---|---|---|
| At most 16 KiB (16,384 bytes); a larger file is not read | U15 rule 3 | `too_large` |
| No NUL byte | U15 rule 3 | `nul_byte` |
| Valid UTF-8 | U15 rule 3 | `not_utf8` |
| No control character other than the newline (no carriage return, no tab, no DEL) | proposal; **assumption** (recalled, not checked): systemd also ends a line at a carriage return | `control_character` |
| No line ends in a backslash, spaces after it ignored, comment lines included | U15 rule 3; proposal for the comment lines | `continuation` |
| No percent sign outside a comment line | U15 rule 3 | `specifier` |
| A blank line is empty or holds spaces only; a comment line starts with `#` or `;` in the first column; every other line starts in the first column and is `[Name]` or `Key=value`, without a space around `=` or at the end; a key is ASCII letters and digits; outside comments only printable ASCII | U15 rule 3 ("`key=value` lines and comment lines"); proposal for the spaces and for ASCII | `syntax` |
| Only `[Unit]`, `[Service]` and `[Install]` in a service unit, `[Unit]`, `[Socket]` and `[Install]` in the socket unit; each section at most once; no `Key=value` line before the first section | U15 rule 3; proposal for "at most once" | `section`, `syntax` |
| No key twice in one section | U15 rule 3 | `duplicate_key` |
| `[Install]` holds only `WantedBy=` with the expected target: `multi-user.target` for `oarfish.service` and the recover unit, `sockets.target` for the socket unit; the helper service has no `[Install]` content | U15 rule 3; **fact** `install/` | `install_section` |
| `Description=` and `Documentation=`: printable ASCII, at most 200 characters | U15 rule 3; proposal for ASCII | `text_value`, `syntax` |

**The closed set of reasons** (17): `unknown_unit`, `too_large`, `nul_byte`, `not_utf8`, `control_character`, `continuation`, `specifier`, `syntax`, `section`, `duplicate_key`, `install_section`, `text_value`, `form_mismatch`, `directive_not_allowed`, `baseline_missing`, `baseline_value`, `extra_value`. A reason is a fixed word. It is written to the journal and is not part of an answer.

**Helper units: the accepted forms (D3).** The text of a helper unit is normalised: comment lines and blank lines are removed, the values of `Description=` and `Documentation=` are removed (the two lines stay, as `Description=` and `Documentation=`), and the remaining lines are joined by newlines. The result must equal one entry of the unit's list, else `form_mismatch`. Each list has one entry, the text that `install/` ships (**fact**, `TestInstallUnitsAreTheFirstForms`). The forms contain `SuccessExitStatus=75` and `RestartForceExitStatus=75` (A1), `TimeoutStartSec=120` (A9), and `DefaultDependencies=no` and `After=local-fs.target` (A14).

**Proposal: the order of the sections and of the lines inside a section is part of the form.** Reasons: (1) U15 rule 3 says "must equal", and one comparison of two texts is the smallest code that implements it in a root process; (2) the form can be compared with the repository file by eye and by one test, with no rule about which reorderings are harmless; (3) nothing needs a reordering: a release that only reorders lines gains nothing, and a deliberate change of a helper unit is a two-step change in any case (D3). Cost: a pure reordering of a helper unit in `install/` is a unit change and needs the two steps. The lines `Description=` and `Documentation=` are part of the form, so a helper unit without one of them is rejected. For `oarfish.service` the order is not part of the rule, because rule 3 defines it as a set (a baseline that must be contained, plus extras).

```ini
# oarfish-update-helper.socket, accepted form 1
[Unit]
Description=
Documentation=
[Socket]
ListenStream=/run/oarfish-update/helper.sock
SocketUser=root
SocketGroup=oarfish
SocketMode=0660
DirectoryMode=0755
Accept=no
Backlog=8
RemoveOnStop=yes
[Install]
WantedBy=sockets.target
```

```ini
# oarfish-update-helper.service, accepted form 1
[Unit]
Description=
Documentation=
OnFailure=oarfish-update-recover.service
StartLimitIntervalSec=10
StartLimitBurst=3
[Service]
Type=exec
ExecStart=/usr/libexec/oarfish-update-helper serve
Restart=on-failure
RestartSec=1
SuccessExitStatus=75
RestartForceExitStatus=75
RuntimeMaxSec=900
User=root
Group=root
UMask=0077
StateDirectory=oarfish-update
StateDirectoryMode=0700
ReadWritePaths=/opt/oarfish
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
IPAddressDeny=any
IPAddressAllow=localhost
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM
MemoryMax=64M
TasksMax=16
Nice=10
```

```ini
# oarfish-update-recover.service, accepted form 1
[Unit]
Description=
Documentation=
DefaultDependencies=no
After=local-fs.target
Before=oarfish-update-helper.socket oarfish.service
[Service]
Type=oneshot
TimeoutStartSec=120
ExecStart=/opt/oarfish/components/known-good/oarfish-update-helper recover
(the 35 sandbox lines of the helper service, from User=root to Nice=10, in the same order)
[Install]
WantedBy=multi-user.target
```

**`oarfish.service`: the required baseline and the allowed extras.** Every directive must have a row in the table below, in the section the row names, with a value the row accepts; every baseline row must be present. A directive without a row is `directive_not_allowed`, a missing baseline row `baseline_missing`, another value `baseline_value` or `extra_value`.

**Proposal: which directive is baseline.** U15 rule 3 says that the baseline is the set of directives that `install/oarfish.service` sets, taken over verbatim, and names `Description`, `Documentation`, `Restart`, `RestartSec` and `TimeoutStopSec` as extras. The table follows both sentences: these five are extras, and every other directive of the file is baseline with exactly the value of the file. That decides the directives that rule 3 does not name: `Type=simple`, `StateDirectoryMode=0700`, `RuntimeDirectoryMode=0750`, `ProtectClock=yes`, `ProtectHostname=yes`, `RestrictRealtime=yes`, `LockPersonality=yes`, `SystemCallErrorNumber=EPERM` and `WantedBy=multi-user.target` are baseline. `TestServiceRulesMatchTheInstallUnit` reads `install/oarfish.service` and fails when the file and the table differ.

| Section | Directive | Kind | Accepted value |
|---|---|---|---|
| `[Unit]` | `Description` | extra | printable ASCII, at most 200 characters, no `%` |
| `[Unit]` | `Documentation` | extra | printable ASCII, at most 200 characters, no `%` |
| `[Service]` | `Type` | baseline | `simple` |
| `[Service]` | `User` | baseline | `oarfish` |
| `[Service]` | `Group` | baseline | `oarfish` |
| `[Service]` | `ExecStart` | baseline | exactly `/opt/oarfish/current/oarfish -listen ` followed by a literal loopback IP address and port in its canonical spelling: `127.0.0.0/8` or `[::1]`, a port from 1 to 65535, no zone, no IPv4-mapped IPv6 address, nothing after it. A prefix character (`+`, `!`, `!!`, `@`, `:`, `-`), a quote, a second argument or a second command fails the comparison |
| `[Service]` | `Restart` | extra | `on-failure` or `always` |
| `[Service]` | `RestartSec` | extra | a whole number of seconds from 1 to 10, digits only (no unit, no sign, no leading zero, not `infinity`) |
| `[Service]` | `TimeoutStopSec` | extra | a whole number of seconds from 5 to 30, digits only |
| `[Service]` | `StateDirectory` | baseline | `oarfish` |
| `[Service]` | `StateDirectoryMode` | baseline | `0700` |
| `[Service]` | `CacheDirectory` | baseline | `oarfish` |
| `[Service]` | `RuntimeDirectory` | baseline | `oarfish` |
| `[Service]` | `RuntimeDirectoryMode` | baseline | `0750` |
| `[Service]` | `UMask` | baseline | `0077` |
| `[Service]` | `NoNewPrivileges` | baseline | `yes` |
| `[Service]` | `CapabilityBoundingSet` | baseline | empty |
| `[Service]` | `AmbientCapabilities` | baseline | empty |
| `[Service]` | `ProtectSystem` | baseline | `strict` |
| `[Service]` | `ProtectHome` | baseline | `yes` |
| `[Service]` | `PrivateTmp` | baseline | `yes` |
| `[Service]` | `PrivateDevices` | baseline | `yes` |
| `[Service]` | `ProtectKernelTunables` | baseline | `yes` |
| `[Service]` | `ProtectKernelModules` | baseline | `yes` |
| `[Service]` | `ProtectKernelLogs` | baseline | `yes` |
| `[Service]` | `ProtectControlGroups` | baseline | `yes` |
| `[Service]` | `ProtectClock` | baseline | `yes` |
| `[Service]` | `ProtectHostname` | baseline | `yes` |
| `[Service]` | `ProtectProc` | extra | `invisible` or `noaccess` |
| `[Service]` | `RestrictNamespaces` | baseline | `yes` |
| `[Service]` | `RestrictRealtime` | baseline | `yes` |
| `[Service]` | `RestrictSUIDSGID` | baseline | `yes` |
| `[Service]` | `LockPersonality` | baseline | `yes` |
| `[Service]` | `RestrictAddressFamilies` | baseline | `AF_UNIX AF_INET AF_INET6` |
| `[Service]` | `SystemCallArchitectures` | baseline | `native` |
| `[Service]` | `SystemCallFilter` | baseline | `@system-service` |
| `[Service]` | `SystemCallErrorNumber` | baseline | `EPERM` |
| `[Install]` | `WantedBy` | baseline | `multi-user.target` |

The table has 38 rows: 32 baseline and 6 extras.

**Proposals in the table, with their reasons.**

- *The bounds.* `RestartSec` from 1 to 10 s: with 0 a crashing service restarts without a pause; above 10 s a crash loop raises `NRestarts` too slowly for the fail-fast criterion inside the 45 s health deadline (U6). `TimeoutStopSec` from 5 to 30 s: below 5 s the service has little time to end cleanly; above 30 s a stop and a start no longer fit into the 60 s limit of one `systemctl` call (`systemctl.go`, `systemctlTimeout`). The values of `install/oarfish.service` (2 and 15) are inside.
- *"Further `Protect*` and `Restrict*` directives with restricting values only."* Every directive of these two families that `install/oarfish.service` sets is baseline. The other directives of these families are `ProtectProc`, `RestrictFileSystems` and `RestrictNetworkInterfaces` (**assumption**: recalled from the systemd documentation, not checked against a systemd version). The table lists one of them, `ProtectProc`, with its two restricting values `invisible` and `noaccess`. `RestrictFileSystems` and `RestrictNetworkInterfaces` are not listed: their values are lists, an inverted list (`~`) changes the meaning of the same words, and no single-line validator can say which list is "restricting only" without also deciding what the service needs. A release that wants one of them takes the two steps.
- *A baseline value is exact.* A stronger value of a baseline directive (for example a narrower `RestrictAddressFamilies`) is rejected as well; rule 3 says "each of these exactly".
- *A directive belongs to its section.* `User=` in `[Unit]` is rejected (**assumption**, recalled: systemd ignores a directive in a section it does not belong to, so the baseline would be missing in effect).

**Wiring in `stage` (fact, `stage.go`, `unitlint.go`).** The lint runs after the extraction (step k of the sequence in `stage.go`) and before the component comparison (step l), on the unit files of the staged tree: `oarfish.service` for every release, and the three helper units when the release carries the components (a legacy bundle has none; it is linted for `oarfish.service` and then refused as before with `requirements_not_met`). A file of more than 16 KiB is rejected without being read. The first rejection ends the request with `unit_rejected` and the usual cleanup: the staging tree is removed, the state is `idle`, no last result is written. A unit file that cannot be read is `io_error`. The installed units are not linted. Every earlier check keeps its answer, and a difference that the lint accepts still answers `installer_required`. The lint at `activate` (U15 rule 3, "and again at `activate`") belongs to step b.

**The journal (fact, `unitlint.go`, `logLint`; refines U8).** A rejection writes one additional line before the line of the cleanup: the fields of U8 for the phase `staging` with the result `unit_rejected`, followed by `unit=<name>` and `reason=<reason>`. The name is one of the four unit names of U2 and the reason one of the closed set. No byte of the unit text, no path and no parser message is part of the answer or of the journal (`TestStageUnitRejected`, `TestLintJournalLineShape`, `TestRedaction`).

**Tests (fact, `unitlint_test.go`).** Every unit in `install/` passes and each helper unit normalises to the first form of its list; one rejected case per forbidden construct of rule 3, per removed baseline directive, per baseline directive with another value and per directive in the wrong section; the weakened values that rule 3 names; accepted cases for changed comments, `Description=` and `Documentation=` and for each extra at its bounds; `stage` with a signed bundle that holds a bad unit; the fuzz target `FuzzUnitLint` with the units of `install/` as seeds, added to `make fuzz` and to the fuzz step of CI. These are class C evidence for row F25 of U13. The class T part of F25 was not run.

**Residual risk.** Unchanged from U15 rule 3 and T18: the lint judges text and does not bound what a signed helper binary does. A new property of step a: the lint in the installed helper is as strict as its tables, so an error in a table that rejects a legitimate unit is corrected by a release whose helper carries the corrected table, and that release must itself pass the old table or be installed with the installer.

### Open points of A15

1. **Password at the second confirmation (C4, D7): open owner question.** The present behaviour stays until the owner decides.
2. **A release tooling check for the two-step rule: not decided.** Nothing in `scripts/` or in CI compares the helper units of a release with the accepted forms of the helper of the release before it. `TestInstallUnitsPassTheLint` only proves that the units and the tables of the same commit agree. `docs/RELEASING.md` has a checklist item; whether a tool should enforce it is an owner decision.
3. **The wording of the U13 row F14 against the U6 sentence about the hash recorded "before a transaction starts": pending owner check.** Neither text is edited by A15.
4. **The assumption of D1** (the helper can read its own executable image under its sandbox) is to be observed on the device before the first release that carries the code of step b.
5. **Statement 1 of A15.2** contains a proposal about `installer_required` after step b; it is confirmed or changed at the review of step b.

### Changes made to the accepted text (A15)

The accepted text is not rewritten. Two sentences got a pointer in place (U4 step 9 and U15 rule 3); for every other row the rule in the column "Now" applies, and the text of the section stays as it is.

| Where | Accepted wording | Now | Source |
|---|---|---|---|
| U4, verification step 9 | "the units are linted (U15 rule 3)" | pointer added: implemented in `stage` by G6.10 step a | A15.3 |
| U15 rule 3, the three helper units | "must equal a compiled-in **canonical form**" (singular) | an ordered list of at most two accepted forms per unit; the order of the lines is part of a form; pointer added in place | D3, A15.3 |
| U15 rule 3, `oarfish.service` | a baseline in prose and extras "for example" | the complete table of A15.3 with one validator per row and the bounds | A15.3 |
| U15 rule 3, parsing | the rules of the sentence "Parsing" | those rules and the stricter readings of the parsing table | A15.3 |
| U15 rule 3, "at `stage` and again at `activate`" | both planned for G6.10 | at `stage` since step a; at `activate` in step b | A15.3 |
| U15 rule 3, tests | "Tests (G6.10)" | the tests exist (class C) | A15.3 |
| A9 (a), U10 | "The lint canonical form of the recover unit gets this line in G6.10" | the form has `TimeoutStartSec=120` | A15.3 |
| U15 rule 4 step 4; A13, open point | "its own version equals `components_to`'s"; how the check is made is open | the proof by bytes of D1 | D1 |
| U15 rule 4 step 1 | "the recorded hashes" | they include the SHA-256 that the signed contents give for the helper | D1 |
| A14, "Delivery"; D-C81 | how a GUI update delivers a changed recover unit is a G6.10 question | the recover unit travels with the set and changes when `known-good` moves; `daemon-reload` after that switch | D2 |
| U8; U15 rule 4 steps 1, 4 and 6, row c7; A2 | `socket_restart_pending` is a status flag; how it is cleared is not decided | set for a change of the normalised socket text, stored with the time since boot, reported by `status`, void after a reboot | D4 |
| U2, request format; U15 rule 6 | the decoder rejects a protocol version it does not implement; `status` stays answerable for every `v` | `status` is answered for every `v` (step b); a component set is staged only for `components.protocol` P or P+1 | D5 |
| U2, `activate`; U9; U15 rule 7; U8 | `components_confirmed` when `stage` reported a component difference; the state members of U8 | a difference of `oarfish.service` alone needs the second confirmation without a component set; an additive state record and an additive list of names in the answer of `stage` (step b) | D6 |
| A12, second confirmation | whether the password is asked again stays open for G6.10 | the present behaviour stays; still an open owner question | D7 |
| U8, journal | one line per phase | plus one line for a rejected unit, with the unit name and the reason | A15.3 |
| U13, row F24 (not edited) | "a different helper binary and unit text" | the unit text differs within what the installed helper's lint accepts | A15.2 |
| U13, row F25 (not edited) | class C and T, planned | class C tests exist; class T not run | A15.3 |
| U14, row G6.10 (not edited) | one step | three implementation steps a, b and c, then a release and the device evidence | A15.2 |

## Amendment A16 (G6.10, step b)

This amendment belongs to step b of G6.10 (A15.2, statement 3): the component transaction, the first start of the new helper and the recovery of the component phases. It records the owner decisions E1 to E4, which the project owner gave with the prompt of step b on 2026-11-11 (D-C84 in `docs/DECISIONS.md`), and every choice the implementation made where U15, A1 to A15 and E1 to E4 name a rule and leave its detail open. The decisions D-C53 to D-C83, the owner decisions Q1 to Q7 and D1 to D7 and the status of this ADR are unchanged. No accepted sentence is edited in place: the table at the end names, for each sentence that is refined, the rule that now applies. The specification is not edited: section 14.2 names for G6.10 the confirmation, the check of the unit files, the rollback of the helper and the key rule, and none of these statements changes.

**Evidence label.** **Owner decision** is one of E1 to E4, in the owner's wording as far as a table allows. **Fact** names the file it was read from, in the tree of the pull request of step b (`backend/cmd/oarfish-update-helper/`, `backend/internal/updater/`). **Proposal** is a choice of the implementation; it is implemented and tested, and the owner's review of step b can change it. **Assumption** is not checked. **Nothing in A16 ran on a device, and nothing is Verified.** The tests are class C (fake root, fake systemd): they prove the order of the helper's writes and calls and the invariant of U7, not the behaviour of systemd or of the kernel. Step b changes no file in `install/`, no installer, no workflow and no release.

### A16.1 Owner decisions E1 to E4 (D-C84)

| # | Subject | Owner decision |
|---|---|---|
| E1 | `installer_required` after step b (accepts the proposal of A15.2, statement 1) | A helper that has the component transaction never answers `installer_required` for a difference. The code stays in the closed list of U2 and in the service. The remaining installer cases are `key_changed`, `unit_rejected`, `requirements_not_met` (D5, and the legacy bundle shape) and the bootstrap. |
| E2 | Reference hash of the known-good set (U6, U15 rule 4 step 1, row F30) | `known_good_sha256` is one SHA-256 over the four files of the set in the fixed order of U2. The helper writes it when it moves `known-good` (`c_committed`, and the recovery that completes row c5). When the state holds none (the installer moved `known-good` last, or the state was rebuilt), `stage` records it from `known-good` the first time it finds a component difference. `activate` compares `known-good` with the recorded value before `c_switching` and answers `components_damaged` when they differ; recovery row c8 does the same. **Residual risk:** damage to `known-good` before that first recording is not detected. |
| E3 | What the new helper takes over | The new helper performs the release phases itself, so it reads `MainPID` and `NRestarts` of `oarfish.service` immediately before its own switch of `current`; nothing of that kind is carried across the hand-over in memory. Everything else it needs (`from`, `to`, `correlation_id`, `from_binary_sha256`, the expected helper hash of D1, the key id of the old helper for rule 5, the protocol range of the old helper, the names of D6) is written to `state.json` before `c_switching`, as additive members. |
| E4 | Cooldown | A transaction that ends in `components_rolled_back` sets the same cooldown markers as a release rollback (U6): the version for 15 minutes, and the 60 s marker. |

### A16.2 The component transaction as implemented

**Fact (`stage.go`, `compare.go`, `components.go`, `txn.go`, `recover.go`, `cleanup.go`, `main.go`).** The numbers P1 to P24 point to the proposals of A16.3.

**`stage`.** The checks a to k2 are unchanged. Step l compares the SHA-256 of the five files with the installed ones (the four helper files through `components/active`, `oarfish.service` through `current`). Then, when one of the **four helper files** differs (the release brings a component set), and before anything is moved into place: `components.key_id` of the signed manifest must equal the key id embedded in this helper, else `key_changed` (rule 5); `components.protocol` must be P or P+1, else `requirements_not_met` (D5); the reference hash of the `known-good` set is recorded when the state holds none for that set, or compared when it holds one, else `components_damaged` (E2, P1). The staged set is then moved from `releases/.stage.<id>/components` to `components/<version>` (mode 0755, one rename, `fsync` of `components/`) **before** the release is moved to `releases/<version>` (row c1). An existing `components/<version>` that a link names, or that is the recorded previous set, is never touched and makes the request fail with `io_error`; any other is moved to `releases/.trash.<id>` first and deleted last, as an existing `releases/<version>` is (D-C67). The staged state records the names that differ (D6), the SHA-256 of the staged `oarfish.service`, and, with a component set, `components_from` (the target of `active`), `components_to` (the version), the set digest of the staged set and the SHA-256 of the staged helper (the expected hash of D1; the file was verified against the signed `contents` by the extraction). A difference of `oarfish.service` alone records the names and no component set (D6). The answer has `components_differ` true for any difference and the additive member `differs` with the names. `discard`, the expiry of a staged release and the staging recovery remove a staged set that no link names. `stage` never answers `installer_required` (E1).

**`activate`, the helper that hands over.** `components_confirmed` must be true when the staged state records any name or a component set (D6), else `confirmation_required`; a true flag when nothing differs is `bad_request`, as before. The staged unit files are linted again: `oarfish.service` of every release and the three helper units of a staged set. Each unit file is read once, and the same bytes are linted and hashed; the hashes must equal those of the staged state (P14). With a component set the target of `active` must still be `components_from`, and the `known-good` set must equal its reference hash, else `components_damaged` (E2). The pre-check of U6 follows as before. Then, with a component set:

1. `c_switching` is written with the members of E3: the key id and the protocol range of this helper, and `socket_text_changed` when the normalised text of the staged socket unit differs from the installed one (the intent of D4; the comparison uses the function of the lint). The answer "accepted" is given.
2. `components/active` is switched to the new set: a link `components/.new.<id>`, a rename over `active`, `fsync` of `components/`.
3. `c_switched` is written; `systemctl daemon-reload` runs.
4. The process ends with exit code 75. It first stops accepting connections, waits until every accepted connection has been answered and closed and the lock is free, and only then returns the status (`serveLoop`, `main.go`). The socket is systemd's and stays; a connection that arrives meanwhile waits for the next process. The old helper executes nothing of the new set and restarts neither itself nor the socket.

A call that fails in steps 2 to 4 ends in a rollback by the same process (P12). Without a component set but with a differing `oarfish.service` there is no component phase: `daemon-reload` runs after the switch of `current` and before the restart, and again after a switch back (U6, P20).

**The first start of the new helper.** `serve` runs its start-up recovery under the lock before it accepts a connection. When the state says `c_switched` and `active` names `components_to`, the process runs the self-check, and every request except `status` answers `busy` until the transaction has ended (the lock). The self-check requires all of: the target of `components/active` is `components_to`; the SHA-256 of `components/active/oarfish-update-helper` equals the expected hash; the SHA-256 of the process's own executable image equals it (read from `/proc/self/exe`, streamed with the 32 KiB buffer, behind a seam that the tests replace); the embedded key id equals the recorded key id of the old helper (rule 5); the protocol range overlaps the recorded range of the old helper (rule 6, D5). That `state.json` is readable is part of the proof by construction: without it the process does not know the transaction (P11). **Pass:** `c_verified` is written; `components/known-good` is switched to the new set; `daemon-reload` runs (D2); one state write records `known_good_sha256` for the new set, `c_committed`, the previous set, `components_updated` and, when the socket text changed, `socket_restart_pending` with the time since boot (D4); the retention runs; the helper reads `MainPID` and `NRestarts` (E3), writes `switching` and continues with the release phases of U5 and U6, without a client request and without an idle exit. **Fail:** the rollback below, with the reason, and exit code 75 (P7).

**Rollback of the components** (failure handling of rule 4, row c8). `c_rolling_back` is written with the reason; `active` is made to name what `known-good` names; `daemon-reload` runs; `reset-failed oarfish-update-helper.service` runs when the process is the recover subcommand started by `OnFailure=` (P8); the `known-good` set is compared with its reference hash. Equal: the never-activated release directory and the failed set are removed (P3), the result is `components_rolled_back` with the reason, both cooldown markers are set (E4), and the state is `idle`. Different, or unreadable: `rollback_failed` with the reason `known_good_damaged` and `needs_attention` (P10).

**The closed set of reasons of `components_rolled_back`** (8): `helper_mismatch`, `key_changed`, `protocol_mismatch`, `state_unreadable`, `interrupted` (a self-check or a recovery); `switch_failed`, `state_write_failed`, `reload_failed` (a call that failed in the helper that handed over). `rollback_failed` after a component rollback has the reason `known_good_damaged`. A reason is a fixed word; it is in the last result and in the journal.

**Recovery of the component phases.** `recover` (at boot, and started by `OnFailure=`; it runs the binary of `known-good`) and `serve` (at its start, under the lock) apply the table of rule 4. "Old" means that `active` does not name `components_to`.

| Row | State and disk | `recover` | `serve` at its start |
|---|---|---|---|
| c1 | a set in `components/<id>` that neither link nor the state names | removed through `releases/.trash.<id>` | the same |
| c2 | `c_switching`, `active` old | the intent is cleared: the phase is `staged` again; then U7 row 4 (removed) | the same; U7 row 4 keeps it until it expires (P4) |
| c3, c4 | `c_switching` or `c_switched`, `active` new | rollback, reason `interrupted` | `c_switched`: the self-check, forward on a pass, rollback with its reason on a failure. `c_switching`: rollback (P5) |
| c5 | `c_verified`, both links new | `c_committed` is written (with `daemon-reload` and the reference hash), then row c6 | the same, then the release phases |
| c5 | `c_verified`, `known-good` old | rollback, reason `interrupted` | the self-check; forward on a pass (P6), else rollback |
| c6 | `c_committed` | the release is aborted (`aborted`, reason `interrupted`, `components_updated` true), the components stay | the release phases continue (P9) |
| c7 | `socket_restart_pending` set | nothing | nothing; `status` reports the flag until a reboot |
| c8 | `c_rolling_back` | the rollback is finished with the recorded reason | the same |
| c9 | `releases/.trash.*`, `components/.new.<id>` | removed | removed |

A release that fails after `c_committed` is rolled back alone by the rules of U6 and U7; its result is `rolled_back` (or `aborted`) with the additive member `components_updated` true (F31).

**Retention.** After `c_committed` the helper keeps the sets that `active` and `known-good` name and the set they replaced (`components_previous` in the state) and removes every other directory below `components/` whose name is a version, through `releases/.trash.<id>`. After a rollback only the failed set is removed; nothing else is pruned. Names that are not a version are never touched.

**Protocol and status.** The decoder answers `status` for every positive integer `v`, with the helper's own `v` and the accepted range; every other request keeps the range check before anything else of it is used. `status` reports three additive members: `staged_differs` (the names of a staged release), `socket_restart_pending` (void when the uptime is smaller than the uptime at which it was set, D4) and, in `last_result`, `components_updated`.

**State members added by step b** (all additive; `TestStateRoundTripWithThePreviousDecoder` holds a copy of the member lists of the code before this step and shows that its decoder ignores them, and that this decoder reads a state of that code):

| Member | Where | Written | Meaning |
|---|---|---|---|
| `differs` | `transaction` | `stage` | the record of D6: names of the closed list, in its order |
| `service_unit_sha256` | `transaction` | `stage` | SHA-256 of the staged `oarfish.service` |
| `components_set_sha256` | `transaction` | `stage` | set digest of the staged set |
| `components_helper_sha256` | `transaction` | `stage` | the expected helper hash of D1 |
| `components_key_id` | `transaction` | `c_switching` | key id of the helper that hands over (rule 5) |
| `components_protocol_min`, `components_protocol_max` | `transaction` | `c_switching` | its protocol range (rule 6) |
| `socket_text_changed` | `transaction` | `c_switching` | the intent of D4 |
| `components_updated` | `transaction`, `last_result` | `c_committed`, every result after it | the components were replaced (F31) |
| `rollback_reason` | `transaction` | `c_rolling_back` | the reason a finished rollback reports |
| `known_good_id` | `transaction` | with `known_good_sha256` | the set the reference hash was computed from (P1) |
| `socket_restart_set_at_uptime_s` | `transaction` | `c_committed` | D4 |
| `components_previous` | top level | `c_committed` | the set the last commit replaced (retention) |

`known_good_sha256`, `known_good_id`, `socket_restart_pending` and its time outlive a transaction; the others end with it. **The set digest (E2):** the SHA-256 over the four file digests, each 32 bytes, concatenated in the order `oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, written as 64 lowercase hexadecimal digits (`TestKnownGoodDigestEncoding`).

**The journal (refines U8).** The component phases write the line of U8 with their names (`c_switching`, `c_switched`, `c_verified`, `c_committed`, `c_rolling_back`); the end of a component rollback writes `phase=components_rolled_back` with the reason as the result.

**Fault hooks (U13, class T).** The call counter stays per process. Because the transaction runs in two processes, the test build reads a third variable, `OARFISH_FAULT_PROC`, with the value `<subcommand>:<phase>`: the fault applies only to the process whose subcommand is that one and whose `state.json` named that phase when it started (`serve:staged`, `serve:c_switched`, `recover:c_switched`). Without the variable the fault applies to every process, as before. The release build contains none of the three names (`scripts/check-helper-hooks.sh`). The call numbers per phase are in `docs/DEVELOPMENT.md`, and a test of the test build fails when they change.

**The service (`backend/internal/updater`).** It accepts the five component phases in a `status` answer, keeps only names of the closed list from the `differs` of `stage` and the `staged_differs` of `status`, keeps `components_updated` of the last result, and shows the names in the job while it waits for the second confirmation. The job ends when `activate` is accepted, so the hand-over of the helper cannot fail it; a `status` request made during the hand-over waits on the socket or, when it fails, is shown as a helper that is not reachable at that moment. No text was added; the dialog and its texts are step c.

### A16.3 Choices of the implementation

Each of these is a **proposal** in the sense of the evidence label.

- **P1. The reference hash counts only for the set it was computed from.** The state records, with `known_good_sha256`, the id of the set (`known_good_id`). When `known-good` names another set, the record does not apply and is taken again (at `stage`, or at `activate`), and at row c8 there is nothing to compare with. *Reason:* E2 says that the state "holds none" when the installer moved `known-good` last. That is true only if the helper never recorded one: the installer does not write `state.json` (**fact**, `install/install.sh`), so after one component update from the GUI and a later installer run the state would hold the hash of a set that `known-good` no longer names, and every later component update would answer `components_damaged`. The id makes the owner's sentence true in that case.
- **P2. The set digest** as printed in A16.2. *Reason:* one fixed-length input per file, no separator and no length field to get wrong.
- **P3. After `components_rolled_back` the staged release and the failed set are removed.** *Reason:* the release was never activated, which is the case in which the release transaction already removes it (`aborted`, U7 row 5); the failed set is named by nothing afterwards and would be removed by row c1 at the next recovery anyway. Nothing else is pruned.
- **P4. Row c2 sets the phase back to `staged`.** The row says "clear the intent; the staged release follows the rules of U7 row 4". At boot row 4 removes it; a running helper keeps it until it expires, and it can be activated again after the 60 s marker.
- **P5. Only `c_switched` starts a self-check.** A `serve` process that finds `c_switching` with the new set active rolls back, also when it is the new binary: `c_switched` was not durable and `daemon-reload` may not have run. D1 says "while the state says `c_switched`".
- **P6. `c_verified` with `known-good` not yet switched:** a `serve` process runs the self-check again and moves forward on a pass; `recover` rolls back (row c5, "as c3").
- **P7. The hand-back.** After a rollback a `serve` process ends with exit code 75 exactly when its own image is not the helper that `active` names afterwards, or cannot be read. So the new helper hands back after a failed self-check, and the old helper that rolled back after a failed call keeps running. One transaction therefore restarts the helper at most twice (the hand-over and the hand-back).
- **P8. How `recover` knows that `OnFailure=` started it.** It reads the environment variable `MONITOR_UNIT` and compares it with `oarfish-update-helper.service`. No argument and no unit is added. **Assumption** (recalled from the systemd documentation, version 251 and later; not observed): systemd sets this variable for a unit it starts through `OnFailure=`. Without the variable `reset-failed` is not run.
- **P9. Row c6.** `recover` (also when `OnFailure=` started it) aborts the release: `aborted`, reason `interrupted`, `components_updated` true, the staged release removed, no cooldown. A `serve` start continues. Before the new helper writes `switching` it requires the staged binary to exist; when it is gone, the release is aborted the same way.
- **P10. `rollback_failed` of the components** uses the existing phase `rollback_failed`. Row c8 is followed in its order: `active` is switched to `known-good` first, then the set is compared. The next boot ends the transaction and keeps `needs_attention`, as it does for a release whose rollback failed.
- **P11. `state.json` unreadable.** The rebuild of U7 additionally looks at the links: when `active` and `known-good` name different sets, `active` is switched back to `known-good` and the result is `components_rolled_back` with the reason `state_unreadable`, the former target of `active` as `to`, and the cooldown. *Reason:* an unproven set is active and nothing says why.
- **P12. A failing call in the helper that hands over** (the link switch, the write of `c_switched`, `daemon-reload`) ends in the rollback above with `switch_failed`, `state_write_failed` or `reload_failed`, by the same process, without a restart.
- **P13. `daemon-reload` after the `known-good` switch (D2) may fail without undoing the commit:** both links name the new set, and the disk is the truth (row c5). systemd then keeps the loaded text until its next reload or the next boot.
- **P14. A staged file that changed.** At `activate`, a staged unit that the lint rejects answers `unit_rejected`, and a staged unit or helper whose hash differs from the staged state answers `checksum_mismatch`. In both cases the staged release ends (`aborted`, reason `unit_rejected` or `staged_changed`). A release staged against another `active` set answers `stage_expired`.
- **P15. The names in answers:** `differs` in the answer of `stage`, `staged_differs` and `socket_restart_pending` in `status`, `components_updated` in `last_result`.
- **P16. The retention is one rule:** a directory below `components/` whose name is a version and that neither link, nor `components_previous`, nor a transaction in the state names is removed. It runs after `c_committed`, at every start-up recovery of `serve` and in `recover` (rows c1, c9).
- **P17. E4, the 60 s marker,** is set at the time of the rollback, not only at `activate`.
- **P18. A start-up recovery that could not take the lock** (the recover unit or the installer holds it) is tried again with the next request. Until then `status` answers with the phase on disk and the other requests answer `busy`.
- **P19. E3, the pre-check.** The new helper does not repeat the 10 s health pre-check; the old helper made it before `c_switching`. When `systemctl show` fails, the health check of the new version runs without the comparison of the process id and of the restart counter, as a rollback does today.
- **P20. `oarfish.service` alone.** A `daemon-reload` that fails before the restart rolls the release back (`aborted`, reason `reload_failed`: the new version never ran). After a switch back the reload also runs at boot, because the unit that systemd loaded is the one `current` named.
- **P21. `socket_restart_pending` without a time** (a state of earlier code) is reported until a commit sets it again.
- **P22. The installed helper no longer builds an `installer_required` answer.** The code stays in the closed list, in the fixed texts and in the service (E1).
- **P23. `components.key_id` and `components.protocol` are checked only when the four helper files differ** (the scope the owner gave for step b): without a set nothing of the delivered helper is installed.
- **P24. The new helper proves its protocol against the range the old helper recorded** (rule 6, D5), not against the manifest's `components.protocol`, which `stage` checked. U13 F28 describes the case with the manifest; the row is not edited.

### A16.4 Not verified, and assumptions

- Nothing ran on a device. In particular: the read of `/proc/self/exe` by the helper under its sandbox (the assumption of D1; the owner's one-command indication of 2026-11-11 is about another process and is not evidence about the helper); the restart of the real units after exit code 75 and the start limit (A1 observed throwaway units); `daemon-reload` from the helper and from the recover unit, also early at boot, and the effect of a changed recover unit at the next boot (D2); `MONITOR_UNIT` (P8); that a connection made during the hand-over waits on the socket of the real socket unit.
- The class T rows of U13 (F26 to F31) and the class D row F24 are not run.

### Open points of A16

1. **Password at the second confirmation (C4, D7):** open owner question, unchanged.
2. **The finishing audit entry (C3), the retention of failed releases, a release tooling check for the two-step rule, and the wording of the U13 row F14 against U6:** not decided, unchanged.
3. **A recover unit started by `OnFailure=` treats the release phases as at boot** (a link switch without a restart of `oarfish.service`, U7): **fact**, `recover.go`, since G6.6. With P8 the process could tell the two cases apart. Not changed here.
4. **After a refused `activate` the release stays staged** (cooldown, `current_unhealthy`, `components_damaged`), and a new `stage` answers `busy` until it expires or is discarded: **fact**, since G6.6. Not changed here.
5. **A rebuilt state has a last result without `to`,** which the service's check of a `status` answer refuses: **fact**, `recover.go` and `backend/internal/updater/helper.go`, since G6.7. Not changed here.

### Changes made to the accepted text (A16)

The accepted text is not rewritten; for every row the rule in the column "Now" applies.

| Where | Accepted wording | Now | Source |
|---|---|---|---|
| U5, "releases that change the helper or a unit"; U9, transitional rule; U11; A15.2 statement 1 | `installer_required` for a difference until G6.10 | a helper with the component transaction never answers it for a difference; the remaining installer cases are `key_changed`, `unit_rejected`, `requirements_not_met` and the bootstrap | E1 |
| U6, "records the hashes of the known-good component set"; U15 rule 4 step 1; U8 | "the recorded hashes" | one set digest, `known_good_sha256`, with the set it names; recorded at the commit, or by `stage` at the first component difference | E2, P1, P2 |
| U15 rule 4 step 4 and 5 | the new helper continues the transaction | everything it needs is in `state.json`; it reads `MainPID` and `NRestarts` itself | E3 |
| U6, cooldown | after a rollback or a failure of a release | also after `components_rolled_back` | E4 |
| U2, `stage` and `status` rows; U8 | the members listed there | the additive members of A16.2 | A16.2 |
| U15 rule 3, "at `stage` and again at `activate`"; A15.3 | at `activate` in step b | implemented; the same bytes are linted and hashed | A16.2, P14 |
| U15 rule 4, failure handling | "writes `components_rolled_back` with the reason" | the closed set of reasons of A16.2 | A16.2 |
| U15 rule 4, table rows c1 to c9 | the rows | the table of A16.2 says which process does what; rows c2, c5 and c6 as P4, P6 and P9 | A16.2 |
| U15 rule 4 step 7; U5, retention | sets older than the previous one are removed | the rule of P16 | P16 |
| U2, request format; U15 rule 6 | `status` answerable for every `v` (step b) | implemented | D5 |
| U8, journal | one line per phase | the component phases and `components_rolled_back` | A16.2 |
| U13, rows F24 and F26 to F32 (not edited) | planned | class C tests exist (`docs/UPDATE.md` names them); class D and T not run | A16.2 |
| U13, row F28 (not edited) | "a protocol outside the manifest's `components.protocol`" | the new helper compares with the range the old helper recorded | P24 |
| Evidence and test plan, item 3 and U13 class T | two fault variables | a third, `OARFISH_FAULT_PROC` | A16.2 |

## Evidence and test plan for the later implementation

1. **Unit and fixture tests (CI).** SemVer precedence table with prerelease cases; `checksums.txt` parser (duplicates, bad digits, extra whitespace); signature verification against the vectors in `scripts/bundlegen/testdata/` and a port of every case of `scripts/test-signing.sh` (tampered file, signature, other key, replay, prehashed `ED`); manifest validation per field, including the new fields; tar rules (traversal, links, devices, duplicates, oversize, mode); request decoder (unknown fields, size, version); peer check with a fake credential source; cache files of the wrong type (FIFO, symbolic link, directory, socket, growing, replaced); cooldown and expiry with a fake uptime; unit lint (the table of U15 rule 3 and every unit in `install/`).
2. **Fuzzing.** Native Go fuzz targets for the request decoder, the checksums parser, the manifest decoder, the version parser, the tar inspector and the unit parser, added to `make fuzz` like the existing parsers.
3. **Architecture tests.** The helper imports only the standard library; only one file imports `os/exec` and only with the argument shapes of the table in U2; the compiled-in lists (requests, programs, unit names, component names, write targets) equal that table; the helper binary has no network client beyond the health check; a release build contains no fault hook.
4. **Crash tests.** A file system abstraction that fails or stops after the k-th mutating call; for every k, the recovery invariant of U7 must hold; every row of the power-loss tables (U7 and U15 rule 4) has a test.
5. **Fake systemd and fake GitHub.** A stub `systemctl` (as in the installer tests, **Fact**: `docs/INSTALL.md` says the installer's stub proves logic, not systemd behaviour) for start failure, crash loop, restart-limit and `OnFailure`; a TLS test server for the service with rate-limit headers, redirects, slow, truncated and oversized bodies.
6. **Unit files.** `systemd-analyze verify` in CI for the three helper units (the same job that checks the main unit, **Fact**: `.github/workflows/ci.yml`); `systemd-analyze security` score recorded on the device, not asserted in CI.
7. **Redaction.** A test that runs the whole flow with a marker password and checks that no log line, state file or audit entry contains it, any address, or any host name.
8. **Hardware (G6.9 and G6.10).** Table U13; measurements of U12; every number from this ADR that is an estimate is replaced by a measurement or left labelled.

## Decisions recorded, and the mapping from the draft numbers

The draft listed 17 proposed decisions under the prefix `D-DRAFT-G6-nn`. Their final numbers are in `docs/DECISIONS.md`:

| Draft | Subject | Final number |
|---|---|---|
| D-DRAFT-G6-01 | Root, socket-activated helper unit with a recover unit (U1) | D-C53 |
| D-DRAFT-G6-02 | Unix socket, `SO_PEERCRED`, five typed requests, never a broker (U2) | D-C53 |
| D-DRAFT-G6-03 | The service downloads, the helper copies and verifies (U3) | D-C53 |
| D-DRAFT-G6-04 | Verification, strictly-newer rule, additive manifest fields (U4) | D-C53; prerelease rule (Q7): D-C59 |
| D-DRAFT-G6-05 | Staging, atomic rename, durability, retention (U5) | D-C53 |
| D-DRAFT-G6-06 | Health criteria, rollback, cooldown, N-1 rule (U6) | D-C53; `/healthz` version (Q5): D-C57 |
| D-DRAFT-G6-07 | Write-ahead state machine, unfinished updates roll back (U7) | D-C53 |
| D-DRAFT-G6-08 | State in root-owned `/var/lib/oarfish-update`, audit split (U8) | D-C53; location (Q6): D-C58 |
| D-DRAFT-G6-09 | Fresh password for `apply`, no automatic upgrade (U9) | D-C55 (Q3) |
| D-DRAFT-G6-10 | Hardening, no relaxation of the main unit (U10) | D-C53 |
| D-DRAFT-G6-11 | Bootstrap by installer, helper replaced only by the installer | **Replaced** by D-C54 (Q1) and D-C60 (key rule) |
| D-DRAFT-G6-12 | Resource budget as targets (U12) | D-C53 |
| D-DRAFT-G6-13 | Evidence classes and the failure matrix (U13) | D-C56 (Q4) |
| D-DRAFT-G6-14 | The split G6.0 to G6.9 (U14) | D-C63 (now G6.0 to G6.10) |
| D-DRAFT-G6-15 | ADR 0009 amendment A6 | D-C61 |
| D-DRAFT-G6-16 | `/healthz` version and `/readyz` | D-C57 |
| D-DRAFT-G6-17 | A release that changes `oarfish.service` needs the installer | **Replaced** by D-C54 (Q2): transitional `installer_required`, then lint (U15) |

New final decisions without a draft number: the acceptance of this ADR itself (D-C53), the specification alignment in version 0.3.5 (D-C62), and the open decision on key rotation and revocation (D-D12). An earlier project note refers to a `D-DRAFT-04` about replacing the alpha signing key; no normative document contained it, and it is carried by D-D12.

## Questions to the owner: all decided

The seven open questions of the draft were decided by the owner on 2026-11-02. Nothing is open for the owner from this ADR except the key-rotation decision D-D12, which is due before the MVP release gate.

| # | Question | Decision |
|---|---|---|
| 1 | How is the helper replaced after G6? | Can be replaced from the GUI with its own confirmation (U15, G6.10); the one-time bootstrap stays an installer run. Draft recommendation "installer only" is replaced. |
| 2 | May a release that changes `oarfish.service` be installed from the GUI? | Yes, for allow-listed unit files only (U15). Transitional until G6.10: `installer_required` with exact instructions. |
| 3 | Fresh password for `apply`? | Yes (U9). |
| 4 | How are start and health failures shown on the Pi? | Test build with a throwaway key and fault hooks, class T, accepted; the success path and one genuine signed-release path with the production key, class D (U13). |
| 5 | May `/healthz` report the version? | Yes (U6). |
| 6 | Update state in root-owned `/var/lib/oarfish-update`? | Yes (U8). |
| 7 | Prerelease rule? | Yes (U4). |

## Source conflicts found

No conflict blocks a decision. Each item names the affected text, the smallest correction proposed, and where it was resolved.

| # | Affected | Conflict or gap | Smallest formal correction | Resolved in |
|---|---|---|---|---|
| C1 | Spec 8.2 row `/var/lib/oarfish/` ("Updatezustand"), D-C47 | Update state in a directory owned by the unprivileged service cannot be trusted by a root helper (U8). | Spec 8.2: add a row `/var/lib/oarfish-update/` "Root-eigener Zustand des Update Helpers" and remove "Updatezustand" from the `/var/lib/oarfish/` row; D-C47: add "except the state of the update helper, which lives in `/var/lib/oarfish-update`". | Spec 0.3.5; D-C58 |
| C2 | Spec 10.1 item 6 ("Privilegierte Änderung erfolgt ausschließlich über den Broker") against item 7, spec 8.6 and D-C28 | The update helper makes privileged changes and is not the broker. | Item 6: "…ausschließlich über den Broker oder, für Oarfish-Releases und die eigenen Komponenten, über den Update Helper". | Spec 0.3.5 |
| C3 | ORB-REL-002 ("Mindestanforderungen"), spec 9.5 step 2 ("Mindestversion") against `docs/RELEASING.md` | The manifest has no kernel or memory minimum and no minimum installed version; the release docs already say ORB-REL-002 is met "for architecture, OS and libc only". | Add the optional fields of U4 and state in the ORB-REL-002 acceptance column what the MVP checks; kernel and memory minimums after a measurement. | Spec 0.3.5 (acceptance column); fields in G6.2 |
| C4 | Spec 8.1 (Update Manager does "Verifikation") and 9.5 steps against spec 8.6 (helper checks "ein bereits heruntergeladenes, signiertes Bundle") | Two places verify; the actor of each step in 9.5 is not named. Spec 8.6 also does not mention the helper's own state, reading the service state, or the replacement of its own components. | 8.1: "Update Manager (unprivilegiert): Erkennung, Download, Vorabprüfung"; 8.6: add "den eigenen Zustand führen", "den Zustand von `oarfish.service` lesen" and the replacement of its own components under ADR 0010. | Spec 0.3.5 |
| C5 | D-C46 / ADR 0009 E9 / `docs/AUTH.md` ("minimal health check that exposes no device data", body `{"status":"ok"}`) | The health decision needs the running version (U6). | State that the Oarfish version is not device data (spec 10.3 class "Öffentlich"); add the `version` member to `docs/AUTH.md` when implemented. | Spec 0.3.5 (13.1); D-C57; `docs/AUTH.md` in G6.7 |
| C6 | ADR 0009 E7 closed audit sets (`audit.go`) | The update actions are not in the sets. | Amendment A6, accepted together with this ADR. | ADR 0009 A6; D-C61; code in G6.7 |
| C7 | `docs/INSTALL.md` ("Old release directories are kept"), installer `check_bundle_entries` (exactly five entries), spec 8.2 (the helper path is in the layout), `docs/ARCHITECTURE.md` ("No privileged code exists") | The implementation changes behaviour these texts describe. | Update the texts in the steps G6.2, G6.3, G6.9 and G6.10; no change now. | Later steps |
| C8 | Spec 9.6 ("stable ist der Defaultkanal") against the alpha phase (every tag is a prerelease, `docs/RELEASING.md`) and the installer note that `releases/latest/download/` is untested (`docs/INSTALL.md`) | A "stable default" channel offers nothing while only prereleases exist. | Adopt the prerelease rule of U4 (Q7) and revisit it with the first stable release. | Spec 0.3.5; D-C59 |
| C9 | The task text of the draft mentions open `D-DRAFT` items | **None found** in normative documents (ADR 0008 says its drafts became D-C31 to D-C41). The `D-DRAFT-04` about replacing the alpha signing key, named in an external project note, exists in no normative document. Open `D-D` items are D-D05 (G10), D-D06 (G12) and D-D10, none of them a G6 matter; D-D10 touches this ADR only through the health route (U6). | Record the key replacement under the open decision D-D12. | D-D12 |
| C10 | Spec 8.2 (file layout) and ADR 0005 ("Open point for G3 and G6, not decided here") | After this ADR the layout has a component tree and links, which spec 8.2 does not list; ADR 0005's open point on key rotation is not resolved in G3 or G6 but moves to D-D12. | Align spec 8.2 with the component tree after G6.1 has confirmed or changed it (V10); leave ADR 0005 unchanged and read it together with D-D12. | Spec 0.3.6 (after G6.1, D-C64); D-D12 |

## Assumptions to verify

| # | Assumption | Where used | Verified by | G6.1 verdict |
|---|---|---|---|---|
| V1 | `systemctl restart`, `daemon-reload` and `reset-failed` work from a uid-0 process with an empty capability set under the helper's sandbox. | U10, U15 | G6.1 on the device; if not, stop and ask the owner. | **Confirmed**; device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V2 | Socket activation, `SocketGroup=`, `DirectoryMode=` and the `IPAddressDeny=any` / `IPAddressAllow=localhost` filter work on the Raspberry Pi OS kernel and systemd. | U1, U2, U10 | G6.1. | **Confirmed**; device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V3 | GitHub: anonymous API limit and `304` handling; hosts of the redirect target for release assets; behaviour of `latest` for prereleases. | U3 | G6.1 against the real service, results written down. | **Partly confirmed**: the limit of 60 per hour per source address, the prerelease behaviour of `latest` and the redirect hosts of assets confirmed; `304` is not free (refuted, A3); device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V4 | The Go TLS client and resolver work under the unchanged service unit; root certificates are present on Raspberry Pi OS Lite and Ubuntu Server. | U10 | G6.1 and G6.7. | **Partly confirmed**: Raspberry Pi OS part confirmed; not verified on Ubuntu (no Ubuntu device; G6.7 and G12); device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V5 | `os.OpenRoot` exists in the Go toolchain of `backend/go.mod` and confines symlinks as documented. | U3 | G6.5 (compile and test). | not part of G6.1 |
| V6 | Whether `journald` is persistent on the reference image; whether the stock start limit of the unit (default `StartLimitBurst`) is hit by a crash loop within the 45 s window. | U6, U8 | G6.1. | **Confirmed** (both questions answered: journald is volatile, the stock start limit is not reached, A6); device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V7 | ext4 ordered-mode rename and `fsync` behaviour on the device's microSD card; memory, CPU, duration and write-load numbers of U12. | U5, U7, U12 | G6.9 (partial: durability cannot be proven by software tests). | not part of G6.1 |
| V8 | The Zero 2 W has no battery-backed clock. | U6, U12, T15 | G6.1 (`timedatectl` on a cold boot). | **Partly confirmed**: no RTC device and no `fake-hwclock`, cold start not tested (A8); device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V9 | Release sizes (the local build is not a release). | U3, U5, U12 | G6.2 on the first dry-run bundle. | not part of G6.1 |
| V10 | systemd follows a unit link from `/etc/systemd/system` into `/opt/oarfish`, `daemon-reload` picks up a changed link target, linked units start at boot with `/opt` on the root file system, drop-ins keep working on a linked unit, and `/usr/libexec/oarfish-update-helper` as a link works as `ExecStart`. | U5, U10, U15 rule 3 | G6.1. If it fails, the copy layout (Option C) with its hardening applies and U15 rule 3 is revised at that stop point. | **Confirmed**; Option L stays, Option C stays the unused fallback (A5 for the wants links); device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V11 | (Original wording of the accepted text, refuted by G6.1.) A helper exit with code 75 and `Restart=on-failure` restarts the service at once; `StartLimitBurst`, `OnFailure=` and `RuntimeMaxSec=` end a crash-looping or hanging new helper in a recovery run from `known-good`; a oneshot recover unit can be started by `OnFailure=` repeatedly. | U10, U15 rule 4 | G6.1. | **Refuted** as written (the exit-75 and start-limit parts; that a oneshot recover unit can be started by `OnFailure=` repeatedly was confirmed); replaced by A1; device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V12 | `clock_gettime(CLOCK_BOOTTIME)` works through the standard library's `syscall` package under `SystemCallFilter=@system-service`. | U6, U10 | G6.1 / G6.5. | **Confirmed**; device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
| V13 | (Original wording of the accepted text, refuted by G6.1.) The restart of the socket unit as the last action (`socket_restart_pending`) behaves as described: the next connection starts the helper. | U15 rule 4 | G6.1. | **Refuted** as written; replaced by A2 (variant a: no restart of the socket); device observation, throwaway units, one device, one image, one moment, 2026-11-02. |
