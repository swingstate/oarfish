# ADR 0010: Update helper: a socket-activated root process with a closed list of typed requests, helper-owned verification and a persistent state machine

- Status: **Proposed** (2026-11-02, waiting for the project owner; nothing here is decided until the owner accepts it)
- Date: 2026-11-02
- Gate: G6 (design stop, D-C28; G6 itself is not started)
- Written against: `main` at commit `1cc1e022d769725284d0a7b162d14f73d75c5e3e` (merge of PR #18)

## How to read this ADR

Every statement carries one of three labels, so that a reader can tell what is known from what is hoped:

- **Fact** names the file it comes from. It was read while writing this ADR.
- **Assumption** is a claim about third-party behaviour or about the device that was recalled, not re-checked, and not measured. Each assumption is listed again under "Assumptions to verify" and is checked by a named step of the G6 plan.
- **Proposal** is what this ADR asks the owner to accept. Numbers marked *estimate* are proposals or assumptions, never measurements.

Rows of tables and of the threat model are proposals unless a cell says Fact or Assumption. Statements about general Linux or systemd behaviour that are not tied to a file of this repository are assumptions, even where they are not marked again.

Nothing in this ADR is verified on hardware. The design stop writes no code, no unit file, no installer change and no workflow change. The unit text in U10 is documentation, not a file that is installed.

## Context

Gate G6 adds the self-update: the GUI detects a newer release on GitHub, the administrator confirms, the release is downloaded, verified, installed beside the running one, activated atomically, health-checked and, on failure, rolled back (spec 6.12: ORB-UPD-001..008, 9.5, 13.4). The specification says that a separate, privileged Update Helper does the privileged part and that it may do nothing else (spec 8.6, ORB-UPD-007). It does not say how that process is started, how the unprivileged service talks to it, who downloads what, where trust starts, what happens at a power failure, or how the result is audited. This ADR proposes answers.

Facts that shape the design:

- **Fact** (`install/oarfish.service`): the service runs as the unprivileged user `oarfish` with `NoNewPrivileges=yes`, an empty `CapabilityBoundingSet`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, `SystemCallFilter=@system-service`, `Restart=on-failure`, `RestartSec=2`, `TimeoutStopSec=15`, `UMask=0077`. Its only writable places are `/var/lib/oarfish` (mode 0700), `/var/cache/oarfish` and `/run/oarfish`.
- **Fact** (`install/install.sh`): the installer verifies `checksums.txt.sig` with the system `openssl`, checks the SHA-256 of manifest and bundle against the signed `checksums.txt`, inspects the tarball entries (exactly five, regular files only), checks the ELF header, installs into `/opt/oarfish/releases/<version>` through a `.stage.XXXXXX` directory and `mv`, switches `/opt/oarfish/current` (a relative symlink) with `mv -T`, restarts the service, polls `/healthz`, and rolls back with `reset-failed` plus `restart` when the new version is not healthy. It requires 64 MiB free (`MIN_FREE_KB=65536`). Its downloads use `--max-filesize 100000000`, `--retry 3`, `--connect-timeout 20`, HTTPS only.
- **Fact** (`docs/RELEASING.md`, `docs/adr/0005-release-signing.md`): the signature is a minisign legacy-mode (`Ed`) Ed25519 signature over the bytes of `checksums.txt`; `checksums.txt` lists the SHA-256 of bundle, `install.sh`, `manifest.json` and the SBOMs. The manifest has `schema_version`, `name`, `version`, `tag`, `prerelease`, `commit`, `build_date`, `os`, `architecture`, `requirements` (`os`, `architecture`, `cpu`, `libc`, `runtime_dependencies`), `bundle` (`file`, `sha256`, `size`) and `contents` (path, mode, size, SHA-256 of every file). Kernel and memory minimums are not in the manifest. The public key is `release/oarfish-release.pub`; the updater is expected to embed it with `go:embed`. Key rotation and revocation are not designed. A tag with a hyphen is a prerelease.
- **Fact** (`backend/internal/server/server.go`, `docs/AUTH.md`, `docs/ARCHITECTURE.md`): `/healthz` answers `{"status":"ok"}` without a session and without device data; `/readyz` is specified (spec 13.1) and does not exist.
- **Fact** (`docs/DECISIONS.md` D-C47, `docs/AUTH.md`): the service writes its account, setup token and audit file below `/var/lib/oarfish`; sessions live in memory; a restart signs everybody out.
- **Fact** (`backend/internal/audit/audit.go`, ADR 0009 E7): audit entries have fixed typed fields; action, target and result are closed sets or patterns; failed logins are aggregated; the file is flushed every 30 s.
- **Fact** (`docs/AUTH.md`, ADR 0009 A5): on the reference device one Argon2id verification took 442.9 ms and, with `FreeOSMemory`, the process retained 0 MiB afterwards (`VmHWM` 25020 kB). One device, one reading.
- **Fact** (`docs/DECISIONS.md` D-C13, D-C28, spec 6.12 ORB-UPD-008): releases are pulled anonymously over HTTPS, no token, no Tailscale; the Update Helper belongs to G6, the Privilege Broker (ORB-SEC-002) to G7, its capability check to G8.
- **Fact** (local development build of 2026-11-01, not a repository file and not a release): the manifest of that build shows a bundle of 3,008,386 bytes with a 7,274,656 byte binary. Treat both as an *estimate* of release size; the budget is 35 MB (ORB-NFR-005).

## Decisions

### U1. Process model and trust boundary

- **Proposal: the helper is a separate systemd unit that runs as root, is started by socket activation, serves requests, and exits after 30 s without a connection and without a running transaction.** One binary, `/usr/libexec/oarfish-update-helper` (the path of spec 8.2), with exactly two subcommands: `serve` (the socket-activated service) and `recover` (boot-time recovery, U7). A second unit, `oarfish-update-recover.service` (`Type=oneshot`, `Before=oarfish.service`, `WantedBy=multi-user.target`), runs `recover` at every boot before the main service starts. It does not use `Requires=`, so a failing recovery never keeps Oarfish from starting.
- **Proposal: the helper has no ordering or dependency relation to `oarfish.service`** (no `Requires=`, `BindsTo=`, `PartOf=`). A restart of the main service therefore never stops the helper, and the helper keeps its transaction running while the service restarts. The helper lives in its own control group, so `systemctl restart oarfish.service` cannot kill it.
- **Proposal: the unprivileged service stays unprivileged** (ORB-SEC-001). It gets no capability, no `ReadWritePaths=` for `/opt/oarfish`, no sudo rule, no polkit rule and no setuid helper. The only new thing it may do is connect to one Unix socket.
- **Proposal: the helper is a separate binary built from the same Go module, standard library only.** It does not contain the HTTP server, the frontend, Argon2 or any third-party module. Reason: everything compiled into a root process is attack surface. The shared verification code (U4) is a small package in the same module that both binaries import.
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
- **Proposal: one request per connection, newline-terminated JSON, at most 4 KiB, 5 s to send it.** The decoder rejects unknown fields (`DisallowUnknownFields`), trailing data and a protocol version other than `"v":1`. Responses are bounded as well (at most 8 KiB). At most 4 connections are served; the others wait in the socket backlog.
- **Proposal: the list of requests is closed and compiled in:**

  | `op` | Arguments | Effect |
  |---|---|---|
  | `status` | none | Read-only: protocol and helper version, installed version, previous healthy version, transaction phase, last result, free space of the release file system. |
  | `stage` | `version`, `correlation_id` | Copy the four downloaded files from the cache into the helper's private area, verify them (U4), extract into a staging directory, move it into `releases/<version>` (inactive). |
  | `activate` | `version`, `correlation_id` | Run the transaction of U5/U6: switch `current`, restart the service, check health, commit or roll back. Returns at once with "accepted"; the transaction continues without the caller. |
  | `discard` | `version` | Remove a staged but not activated release and its staging files. |
  | `ack` | `transaction_id` | Mark the last result as seen by the service (U8). |

  Arguments are typed: `version` must match `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$` (the installer's `valid_version`, **Fact**: `install/install.sh`), at most 64 characters and no `..`; `correlation_id` is 16 hexadecimal digits; `transaction_id` is 16 hexadecimal digits made by the helper. **There is no argument that is a path, a URL, a command line, an environment variable or a unit name.** The helper builds every path from fixed templates and the validated version.
- **Proposal: errors are typed with a closed set of codes** (`bad_request`, `busy`, `not_found`, `signature_invalid`, `checksum_mismatch`, `manifest_invalid`, `downgrade_refused`, `already_installed`, `requirements_not_met`, `helper_too_old`, `unit_changed`, `no_space`, `current_unhealthy`, `cooldown`, `io_error`, `internal`) with fixed texts. An error never echoes the request, a path or an operating-system error string.
- **Proposal: the only program the helper ever executes is `systemctl`**, by absolute path, with a cleared environment and exactly three argument shapes, all with the constant unit name: `restart oarfish.service`, `reset-failed oarfish.service`, and `show oarfish.service --property=ActiveState,SubState,MainPID,NRestarts,ExecStart --no-pager`. No shell, no `PATH` lookup, no package manager (ORB-UPD-007). One function does this; a test asserts the three shapes and that no other file imports `os/exec`.
- **How this differs from the Privilege Broker (G7).** The broker (spec 8.6, ORB-SEC-002, ORB-SEC-003) is a general mechanism: an allowlist of operations such as `service.restart(unit)`, with arguments that name things on the system, and a capability check per caller (G8). The update helper is not general: it has one subject (Oarfish releases below `/opt/oarfish`), one unit it may touch (`oarfish.service`, never as a parameter), no capability model and no plugin-facing API. **The helper never becomes a broker.** Proposed rules: (1) a new request type needs an amendment of this ADR; (2) a test compares the compiled-in list of `op` values with the table above and fails on any difference; (3) the helper has no operation whose argument names a unit, a path, a package or a program; (4) every privileged operation outside Oarfish's own release lifecycle goes to the broker, which is a separate process with its own socket, unit and code; (5) the two processes share no request-handling code. A future plugin runtime (G8) must keep plugin processes away from the helper socket (for example `InaccessiblePaths=/run/oarfish-update` and a different uid), because the socket accepts every process of the user `oarfish`.
- **Consequence for the audit of a compromised service:** the helper cannot tell whether the administrator clicked. See U9 and the threat model.

Alternatives considered: *HTTP over a loopback port* (needs its own authentication, is reachable by every local user, and mixes with the public API), *named pipes or files* (no peer identity), *passing the request on the command line of a started process* (needs a way to start it, see U1), *a generic RPC with `exec(cmd, args)`* (forbidden by CLAUDE.md and ORB-UPD-007).

### U3. Who downloads what, and how the files reach a place the helper trusts

- **Proposal: the unprivileged service does all network traffic; the helper has no network access beyond loopback for the health check** (U6, U10). A root process that parses TLS and HTTP is a larger risk than the same code running as `oarfish` under the existing sandbox.
- **Proposal: discovery.** The service asks the GitHub REST API for the release list once per user-initiated check, with an `If-None-Match` ETag kept in memory, and reads only `tag_name`, `prerelease`, `draft` and the release notes `body`. It does **not** use `browser_download_url` or any other URL from the response. It builds the download URLs itself from the strictly validated tag: `https://github.com/<repo>/releases/download/<tag>/<file>` for `checksums.txt`, `checksums.txt.sig`, `manifest.json` and `oarfish_<version>_linux_arm64.tar.gz` (the same scheme as the installer; `<repo>` is the constant the installer already carries). Reason: the API response is unsigned data.
- **Proposal: bounds on every request.** HTTPS only, TLS 1.2 or newer, system root certificates, no proxy from the environment, at most 3 redirects, each redirect must stay on `https://` and on an allow-listed host (`github.com` and `*.githubusercontent.com`), and the response host is never logged beyond a code. Connect 15 s, TLS handshake 15 s, response headers 20 s. API response at most 1 MiB; `checksums.txt`, `checksums.txt.sig` and `manifest.json` at most 64 KiB each, 30 s each. The bundle: at most the size announced by the (advisory-verified) manifest and never more than 40,000,000 bytes (ORB-NFR-005 is 35 MB), a stall timeout of 30 s without bytes and an absolute limit of 15 min. The body is streamed to a `.part` file with a 32 KiB buffer, never held in memory. An interrupted download is discarded; there is no resume (**Assumption:** range requests on the release CDN are not relied on). One automatic retry after 5 s, then the GUI shows a typed error and the administrator can try again (R-05, ORB-NFR-014).
- **Proposal: rate limit and interval.** No background polling and no timer (ORB-UPD-002: no automatic upgrade; spec 10.5: no hidden traffic). The service checks when the administrator opens the update page, at most once per 60 minutes (the result is cached in memory, nothing is written), and on the button "Check now", at most once per 60 s device-wide. On `403` or `429`, or `X-RateLimit-Remaining: 0`, the service waits until `Retry-After` or `X-RateLimit-Reset` and shows the reason; it never loops. The request carries the user agent `oarfish/<version>` and nothing else that identifies the device. **Assumption:** the anonymous limit is 60 API requests per hour and per source address, shared by every device behind the same address, and answers with `304` to a matching ETag do not count; both are recalled from the GitHub documentation and must be re-read in G6.1 (verification V3).
- **Proposal: the cache.** The service writes into `/var/cache/oarfish/update/<version>/` (directory 0755, files 0644 after an explicit `chmod`, because the unit's `UMask=0077` would otherwise make them unreadable for the helper without a capability). The files are public release artifacts, so the mode is not a confidentiality problem; ORB-SEC-005 concerns configuration and secret files. The cache is "recoverable" (spec 8.2): the service deletes `.part` files and stale directories at every start and after a successful `stage`.
- **Proposal: the service also verifies, but only for display and to fail early.** It checks the signature (embedded key), the hashes and the manifest of the small files before it downloads the bundle, so the update page can show "signature checked" (spec 9.5) and a bad release costs no bundle download. **The helper never trusts this result.** The two binaries import the same verification package.
- **Proposal: closing the time-of-check to time-of-use gap.** The request `stage` names only a version. The helper then:
  1. opens `/var/cache/oarfish/update` as an `os.Root` (confined traversal, no symlink can lead out; **Assumption:** `os.OpenRoot` exists in the Go version in `backend/go.mod`, recalled from the Go 1.24 release notes), opens each of the four expected file names, requires a regular file with `fstat`, and requires its size to be within the caps above;
  2. copies the bytes into its own private directory `/opt/oarfish/releases/.stage.<id>/` (root-owned, mode 0700, same file system as the final location), computing SHA-256 over **the bytes it writes**, with a limit of cap plus one byte;
  3. verifies the signature of its private copy of `checksums.txt` first, then compares the hashes it computed with the signed list, and from then on reads only its private copies. Whatever the service does to the cache after the copy cannot change what was verified. A change during the copy changes the computed hash and fails the comparison.
- **Cost (*estimate*):** the bundle is written twice (cache, then private copy), about 3 MB more per update for a release the size of the local build, up to 40 MB at the budget limit. The private copy is deleted after extraction.

Alternatives considered:

- *The service streams the bundle into the helper over the socket:* one write less, but the helper must stay alive during a download that can take minutes, the protocol needs a binary streaming phase, and a slow client holds a root process. Rejected for G6; possible later.
- *File descriptor passing with a sealed `memfd`:* no SD write for the handoff, but the whole bundle must sit in RAM (up to 40 MB on a 512 MB device with an 80 MB peak budget, ORB-NFR-002), and the code is subtle. Rejected.
- *The helper downloads:* puts the network stack in root. Rejected.
- *The helper verifies in place in the cache directory:* the service owns that directory and can change a file after the check. This is exactly the problem to avoid. Rejected.
- *Make the cache read-only for the service after download (`chattr +i`, mount tricks):* needs privileges the service must not have, or a root helper just to protect the helper. Rejected.
- *The GitHub `latest` release endpoint:* it ignores prereleases (**Assumption**, recalled), and every alpha tag is a prerelease (**Fact**, `docs/RELEASING.md`).

### U4. Verification: key, manifest, architecture, downgrade, prereleases

- **Proposal: the helper verifies everything itself, in this order, on its private copies:** (1) `checksums.txt.sig` against the embedded key; (2) `checksums.txt` lists the expected bundle name and `manifest.json` exactly once each, with 64 hexadecimal digits; (3) SHA-256 of the manifest and bundle copies equal the listed values; (4) the manifest is decoded strictly for the fields below; (5) `manifest.bundle.sha256` equals the listed bundle hash and `manifest.bundle.size` equals the file size; (6) version and architecture rules; (7) the tarball is inspected and extracted (U5); (8) every extracted file matches `contents` in size, mode and SHA-256.
- **Proposal: trusted key.** The helper embeds `release/oarfish-release.pub` with `go:embed` and reads no key file at run time; an on-disk key would be a mutable trust root. The same file is embedded in the service for the advisory check. `go:embed` cannot reach outside the Go module, so the implementation copies the file into the module at build time and a CI step, like `scripts/check-installer-key.sh` for the installer, requires the copy to equal `release/oarfish-release.pub` (**Fact**: `docs/RELEASING.md` describes the installer check). The key is a list of one entry in G6; rotation is not designed here.
- **Proposal: signature format exactly as ADR 0005 and ADR 0006.** Line 2 of the signature file base64-decodes to 74 bytes: algorithm `Ed`, the 8-byte key id of the embedded key, a 64-byte Ed25519 signature over the exact bytes of `checksums.txt`, checked with `crypto/ed25519`. Algorithm `ED` (prehashed) is refused, as `scripts/verify-release.sh` and the installer refuse it. The trusted comment (lines 3 and 4) is ignored, like the installer ignores it. The standard library is enough, so **no third-party module is added to the helper**.
- **Proposal: manifest fields used** (ORB-REL-002): `schema_version` must be 1; `name` must be `oarfish`; `version` must equal the requested version, the tag `v<version>` and the version in the bundle file name; `tag`; `prerelease`, which must agree with the presence of a hyphen in `version`; `commit` (40 lowercase hexadecimal digits, shown in the GUI and the audit); `os` = `linux`; `architecture` = `arm64` and equal to the architecture the helper was built for (`runtime.GOARCH`); `requirements.os`, `requirements.architecture` (same checks), `requirements.libc` and `runtime_dependencies` (shown, not enforced); `bundle`; `contents`. Unknown fields are ignored so that additive changes stay compatible; unknown or missing known fields fail. **Fact** (`docs/RELEASING.md`): ORB-REL-002 is met "for architecture, OS and libc only"; kernel and memory minimums are not in the manifest. **Proposal:** two additive, optional fields, added by the release tooling in plan step G6.2: `requirements.min_installed_version` (a version; the helper refuses an update from anything older, which gives spec 9.5 step 2 "Mindestversion" a meaning) and `requirements.helper_protocol` (an integer; the helper refuses a release that needs a newer protocol than it implements, `helper_too_old`, see U11). Kernel and memory minimums stay out until they are measured.
- **Proposal: downgrade and replay protection is "strictly newer than the installed version".** The helper compares the candidate with the version in the target of `/opt/oarfish/current` (written only by root) using SemVer 2.0 precedence, implemented in about 40 lines of standard library code (numeric identifiers compare as numbers, a release is newer than its own prereleases, no build metadata). Equal or older is refused (`already_installed` or `downgrade_refused`). Reason: the signed content has neither an expiry nor a counter, so a validly signed old release can be replayed by anyone who can serve files; the installed version is a counter the attacker cannot reset. Rollback to the previous healthy version (U6) is not an update and is allowed. Spec 9.6 allows a downgrade only through "einen klaren Recovery-Workflow": that is the installer with `--version` run by an administrator with a shell, unchanged. A freeze attack (hiding new releases) cannot be detected; that is accepted.
- **Proposal: mix-and-match is closed by construction.** A signed `checksums.txt` of version A with a bundle of version B fails step 3, because the listed bundle name contains the version and the hash is that of A's bundle.
- **Proposal: prereleases.** The release list is not filtered by GitHub's flag, which is unsigned. A device whose installed version has a prerelease part offers the highest eligible version among prereleases and stable releases; a device on a stable version offers stable releases only, until an explicit beta setting exists (spec 9.6). The helper enforces the manifest's own `prerelease` field and the hyphen rule, not the API flag.
- **Proposal: what must be decided about key rotation before the MVP release (G13), not designed here.** (1) Whether a device may trust more than one key and how a second key is introduced; (2) how a device that only knows the old key obtains a new key, including the case of a lost private key (ADR 0005: "devices that embed only this public key cannot accept new releases without a new installer"); (3) how a compromised key is revoked when signed content carries no expiry; (4) whether the helper itself can be replaced through the GUI (U11, open question 1), because a key change inside the helper binary needs that; (5) where the private key lives and who can use it (today an unencrypted GitHub Actions secret, ADR 0005 decision 3). Until then a device that loses the key trust needs the installer.

Alternatives considered: *`minisign` or `openssl` on the device* (the installer's choice for a shell script, ADR 0006; the helper is Go and has `crypto/ed25519`), *a third-party minisign or TUF library* (a large dependency in a root process for a 74-byte signature check), *the trusted comment as version binding* (not needed, the version is bound through the signed file names and hashes), *a monotonic counter in the manifest* (a release tooling change for a property the installed version already gives), *trusting the API `prerelease` flag* (unsigned).

### U5. On-disk layout and activation

- **Fact** (`docs/PFLICHTENHEFT.md` 8.2, `install/install.sh`): releases are `/opt/oarfish/releases/<version>` (root-owned, immutable, mode 0755), the active one is the relative symlink `/opt/oarfish/current -> releases/<version>`.
- **Proposal: staging.** Private working area `/opt/oarfish/releases/.stage.<id>/` (`<id>` 16 random hexadecimal digits), mode 0700, root-owned, on the same file system as the final directory so that the final `rename(2)` is atomic. It holds the four copied files and the extracted tree. The installer already uses names of the form `.stage.XXXXXX` there (**Fact**: `install/install.sh`).
- **Proposal: extraction rules.** The helper reads the tarball with `archive/tar` and `compress/gzip` from its private copy. It accepts only regular files and the single top-level directory `oarfish_<version>_linux_arm64/`; every other entry type (symlink, hard link, device, FIFO, absolute or `..` path, duplicate) fails the release. The accepted names are a fixed allow-list: `oarfish`, `oarfish.service`, `LICENSE`, `THIRD_PARTY_NOTICES.txt` and, from plan step G6.2, the helper and its unit files. Each file must be listed in `contents` with the same size and SHA-256 and a mode of exactly 0644 or 0755; the helper creates files with `O_EXCL`, an explicit mode and root ownership and ignores owner, group and mode in the tar header. At most 128 MiB are extracted in total and `io.LimitReader` stops any file at its manifest size plus one byte (decompression bomb). The `oarfish` binary must be an ELF64 little-endian executable for machine 0xB7, as the installer checks.
- **Proposal: durability rules.** Each extracted file is written, `fsync`ed and closed; the release directory is `fsync`ed; the directory is root-owned with mode 0755, so the service cannot change it; `rename(.stage.<id>/release, releases/<version>)`; then `fsync` of `releases/`. The state file is replaced by write to a temporary file in the same directory, `fsync`, `rename`, `fsync` of the directory (the same recipe as `account.json`, ADR 0009 E4). The switch is a symlink `/opt/oarfish/.current.new.<id>` created with `os.Symlink`, then `rename` over `current` (a single atomic replacement, like the installer's `mv -T`), then `fsync` of `/opt/oarfish`. **Assumption:** the root file system is ext4 in ordered mode, where a rename is atomic with respect to a crash (old or new, never a mixture).
- **Proposal: free space.** Before `stage` the helper requires at least *copied bytes + extracted bytes + 64 MiB* free on the release file system (`statfs`); the 64 MiB reserve is the installer's `MIN_FREE_KB` (**Fact**) and keeps the state-file writes and the rollback possible. The service checks *bundle size + 16 MiB* on the cache file system before it downloads. Failure is `no_space` before anything is changed.
- **Proposal: retention.** After a successful commit the helper keeps the active release and the previous healthy one (the rollback target) and removes every other directory in `releases/` whose name matches the version pattern, plus stale `.stage.*` and `.trash.*`. A directory is removed by first renaming it to `.trash.<id>` (atomic) and then deleting it, so an interrupted delete never leaves a half-removed directory under a version name. Names that do not match the pattern are never touched. Nothing is pruned after a rollback or a failure. Two releases at about 7 MB each (*estimate*) cost little space; this replaces the installer's "old release directories are kept" (**Fact**: `docs/INSTALL.md`) and the documentation must say so when it is implemented.
- **Proposal: the helper does not install systemd unit files** (see U10, "unit changes", and open question 2). If the new release's `oarfish.service` differs byte for byte from the one in the directory of the version being replaced, `stage` fails with `unit_changed` and the GUI tells the administrator to run the installer of that release. Spec 8.6 lists exactly what the helper may do and unit installation is not on the list (**Fact**: `docs/PFLICHTENHEFT.md` 8.6).
- **Estimate of microSD writes per update:** about *2 × bundle + extracted size* plus well under 100 KiB of state, journal and audit data. For the local build that is roughly 2 × 3 MB + 7.3 MB ≈ 13 MB (*estimate*, not measured); a release at the 35 MB budget would be several times that. At one update a month this is a few hundred megabytes a year (*estimate*), small against the wear the R-04 risk is about. Measured in G6.9 with `write_bytes` of `/proc/<pid>/io` for both processes, as AT-022 does.

Alternatives considered: *extract directly into `releases/<version>`* (a power failure leaves a half-filled directory under a version name; the staging-and-rename recipe is what the installer already does), *hard-linking or deduplicating files between releases* (one binary dominates the size, nothing to gain), *keeping all old releases* (the SD card fills; the installer's remark that they are small holds only for a few releases), *a separate `rollback` symlink* (a second pointer that can disagree with `current`; the previous version is state of the transaction).

### U6. Health check, rollback and the case where the new binary does not start

- **Proposal: the helper decides, nobody else.** The GUI only displays; the service may be the thing that is broken.
- **Proposal: before a transaction starts**, the helper requires the current release to be healthy (same criteria as below, 10 s) and records the binary hash of the current release, so that it knows a healthy rollback target exists. Otherwise `current_unhealthy`.
- **Proposal: what counts as healthy after activation.** All of these, polled every second:
  1. `systemctl show` reports `ActiveState=active`, `SubState=running`, and a `MainPID` that differs from the one before the restart;
  2. `NRestarts` has not increased since the restart (a start-then-crash loop with `Restart=on-failure` is therefore caught, not mistaken for a slow start);
  3. `GET /healthz` on the loopback address from the unit's `-listen` argument (read from the `ExecStart` property and accepted only if it is a literal loopback IP address with a port, else the default `127.0.0.1:8484`) answers `200` with `"status":"ok"`;
  4. **the response reports the version that was installed** (see next point);
  5. conditions 1 to 4 hold for 3 consecutive polls and for at least 10 s ("stability window").
  Fail fast: `ActiveState=failed` or an increased `NRestarts` ends the wait at once. Otherwise the deadline is 45 s after the restart returned.
- **Proposal: reported version.** `/healthz` gains a `"version"` member (`{"status":"ok","version":"0.1.0-alpha.8"}`). The installer's check only globs for `"status":"ok"` (**Fact**: `install/install.sh`), so it keeps working. This touches D-C46 / ADR 0009 E9 ("a minimal health check that exposes no device data"); the Oarfish version is "Öffentlich" in the data classes of spec 10.3, so it is not device data, but the owner should confirm (open question 5). Alternatives: reading `/proc/<pid>/exe` (needs `CAP_SYS_PTRACE` for a process of another user, **Assumption**; rejected) or executing the new binary with `-version` (never execute downloaded code as root). **Proposal:** the specified `/readyz` (spec 13.1; **Fact**: it does not exist) should be implemented in plan step G6.7 and the helper should then require it as well, because `/healthz` only says that the process answers.
- **Proposal: automatic rollback** (ORB-UPD-006) when conditions fail or the deadline passes: write the intent `rolling_back`, verify that the SHA-256 of `releases/<from>/oarfish` equals the hash recorded before the transaction (a damaged rollback target on a microSD card is detected instead of activated), switch `current` back, `systemctl reset-failed oarfish.service` (the failed starts count against the start limit; the installer does the same, **Fact**), `restart`, and apply the same health criteria to the old version. Result `rolled_back` with a reason code. If the old version does not become healthy either, the result is `rollback_failed`: the helper stops, leaves `current` on `from`, logs it, and the state says `needs_attention`. The GUI is probably unreachable then; the administrator needs a shell and `journalctl`, exactly like the installer's last message today. This is the one case where a GUI-only path is not promised, and it is stated as such.
- **Proposal: the new binary does not start at all.** It is the same path: `ActiveState` goes `failed` or `NRestarts` rises within seconds, the helper rolls back without waiting for the full deadline. Worst case duration for one update that fails and rolls back is below 2 minutes (*estimate*).
- **Proposal: cooldown.** After a rollback or a failure the helper refuses `activate` for the same version for 15 minutes and refuses any `activate` within 60 s of the previous one (`cooldown`). A compromised service cannot use the helper to restart `oarfish.service` in a loop.
- **Proposal: rollback does not restore data, so releases must be N-1 compatible.** A release must not change the format of anything below `/var/lib/oarfish` in a way that the previous release cannot read. A migration is allowed only in two steps (first a release that reads both formats, later one that writes the new one). This is a rule for release authors and belongs in `docs/RELEASING.md` when G6 is implemented.
- **Proposal: a future TLS design (D-D10) must keep a loopback-reachable plain health route, or this ADR is amended**, because the helper's health check depends on it.

Alternatives considered: *the service reports success to the helper* (a broken service cannot report; a compromised one lies), *only `systemctl is-active`* (a crashing binary is "active" for a moment), *a watchdog (`WatchdogSec=`) in the unit* (needs the Go binary to implement `sd_notify`; reasonable later, not required), *the GUI decides* (the GUI is the thing that may be dead).

### U7. Crash and power-loss safety

- **Proposal: a persistent state machine in `state.json`** (U8) with these phases: `idle`, `staging`, `staged`, `switching`, `verifying`, `committed`, `rolling_back`, and the results `rolled_back` and `rollback_failed`. A phase is written (atomic replace, `fsync`) **before** the action it announces (write-ahead intent), so that a crash always leaves a state that names what might be half done. At most seven small writes per update.
- **Proposal: recovery looks at the disk, not only at the file.** The truth about what is active is the target of `current`; the state file says why. If `state.json` is missing or unreadable (SD fault), the helper rebuilds the minimum from the disk (installed version = target of `current`, no previous version known, no transaction), removes `.stage.*` and `.trash.*`, and reports `needs_attention` once.
- **Proposal: one rule for every unfinished transaction.** `recover` (at boot, before `oarfish.service` starts) and `serve` (when it finds a transaction that no process owns, found with an exclusive `flock` on the state directory) both apply the same conservative rule: **an update that has not reached `committed` is rolled back.** At boot the service is not yet running, so rolling back is only a `current` switch; later the full rollback procedure of U6 runs. Reason: it needs no health decision at boot and has one code path. Cost: a power failure in the short window after the new version proved healthy and before `committed` is durable rolls back a good update; the administrator repeats it.
- **Table: where power can fail.** "State" is what is on the disk afterwards; "Recovery" is what the next start does.

  | # | Step | Interrupted here | State on disk | Recovery at the next start |
  |---|---|---|---|---|
  | 1 | Service downloads files into the cache | during the download | `.part` file in the cache, helper state `idle` | The service deletes `.part` files and stale cache directories at start; nothing privileged happened. |
  | 2 | Helper copies and verifies (`staging` written) | during copy, verify or extraction | `.stage.<id>` partly filled, `releases/` unchanged, `current` unchanged | `recover` deletes every `.stage.*`, sets `idle`, result `aborted`. The administrator starts again. |
  | 3 | `rename` of the staged tree into `releases/<version>` | after the rename, before `staged` is durable | `releases/<version>` complete, state still `staging` | The state names `to`; `recover` removes `releases/<to>` (it is not `current` and not the recorded previous), sets `idle`. |
  | 4 | `staged` written | waiting for `activate` | verified release inactive, `current` unchanged | `recover` removes the staged directory at boot (a staged release does not outlive a reboot, so `activate` never runs on a tree older than the verification). |
  | 5 | `switching` written (intent, `from`, `to`, hash of the old binary) | before the symlink rename | `current -> from` | `current` still names `from`: clear the intent, remove `releases/<to>`, `idle`. |
  | 6 | Symlink `rename` | at the rename or just after it, directory not yet synced | `current` names `from` or `to` (never anything else) | If `from`: as step 5. If `to` while the state is `switching` or `verifying`: roll back (below). |
  | 7 | `systemctl restart`, `verifying` written, polling | any time, including a helper crash or the kernel killing it | `current -> to`, state `verifying` | At boot: switch `current` back to `from` before the service starts, result `rolled_back` reason `interrupted`. Live (helper restarted by a new connection): full rollback procedure of U6. |
  | 8 | Health proven, `committed` not yet durable | in the window between | `current -> to`, state `verifying` | Same as step 7 (accepted cost). |
  | 9 | `committed` written | before pruning is complete | new `current`, `from` recorded as previous, some old releases or `.trash.*` left | `recover` finishes the pruning; no functional effect. |
  | 10 | Rollback (`rolling_back` written) | during the switch back or the health check of the old version | `current -> from` or `to` | Ensure `current -> from`, compare the hash of the old binary with the recorded one; equal: result `rolled_back`; different: `rollback_failed`, `needs_attention`. |
  | 11 | Any state-file write | during the temporary file or the rename | previous state file intact (temporary file is ignored and deleted) | The previous phase applies; the rows above cover it. |
  | 12 | The SD card itself fails or remounts read-only | any | not designed | The helper reports `io_error` and stops. Not verified, not covered by this design. |

- **Proposal: the invariant the tests must prove.** After a crash at any point, `current` names a complete release that was healthy when it was last activated, and no release directory under a version name is incomplete.
- **Assumption:** the above relies on rename atomicity and on `fsync` doing what it says on the microSD card's controller. Power-cut behaviour of cheap cards is not predictable from software; the design reduces exposure, it cannot prove durability (U13).

### U8. State, audit and privacy

- **Proposal: the helper's state lives in `/var/lib/oarfish-update/`** (`StateDirectory=oarfish-update`, root-owned, mode 0700; `state.json` 0600). **This deviates from spec 8.2 and D-C47, which place update state in `/var/lib/oarfish/`** (see "Source conflicts found", C1). Reason: `/var/lib/oarfish` is owned by the unprivileged service (**Fact**: `install/oarfish.service`, `docs/AUTH.md`; mode 0700, user `oarfish`); the owner of a directory can rename or remove entries in it whatever their owner and mode (**Assumption**, Unix semantics), so state the root helper trusts for rollback decisions would be modifiable by the service it protects against. The service reads the state only through `status`.
- **Proposal: `state.json` content** (schema-versioned): `schema_version`, `helper_version`, the transaction (`id`, `phase`, `to`, `from`, `from_binary_sha256`, `correlation_id`), and the last result (`transaction_id`, `to`, `from`, `outcome`, `reason_code`, `correlation_id`, `finished_at`, `acknowledged`). Only the last result is kept; the history is the audit. No host name, no address, no URL, no user name, no serial number.
- **Proposal: audit.** The existing audit file belongs to the service (ADR 0009 E7), and a second writer would break its rotation and its ownership, so the helper does not write it. Instead: (1) the service writes `update_started` when the administrator confirmed (user = the account name, correlation ID = the server's), and a finishing entry when it reads an unacknowledged last result from `status` (after the restart, at the next start), with the **same correlation ID** and user `-` (the service keeps nothing across a restart; with a single administrator account the correlation ID links the two); then it sends `ack`. (2) The helper writes its own phase changes to the journal, one line per phase, as `phase=<name> to=<version> from=<version> cid=<id> result=<code>`; this record does not depend on the service. **Fact** (ADR 0009 E7): action, target and result are closed sets or patterns; the update actions are not in them. **Proposal:** amendment A6 to ADR 0009 adds the actions `update_started` and `update_finished`, the target pattern `update/<version>` (the version regex of U2), and the results `rolled_back`, `signature_invalid`, `checksum_mismatch`, `downgrade_refused`, `no_space`, `health_failed`, `unit_changed`, `unavailable` next to the existing `success`, `wrong_password`, `rate_limited` and `error`. The amendment is part of plan step G6.7 and needs the owner's acceptance like A1 to A5 did.
- **Proposal: nothing secret and nothing identifying in logs.** The helper handles no secret: it has no password, no token, no cookie (the request carries none). Network errors are mapped to codes (`dns_failure`, `tls_failure`, `timeout`, `unreachable`, `http_<status>`, `rate_limited`) and never logged as Go error strings, because those contain server addresses. No log line contains a request body, a cookie or a header value. This extends ORB-SEC-007 and AT-023 to the update path; the redaction test covers it.
- **Estimate:** about 750 bytes of audit data and about 2 KiB of journal data per update.
- **Assumption:** whether `journald` on Raspberry Pi OS Lite writes to disk (`/var/log/journal`) or only to memory is not known. If it is volatile, the helper's journal lines are lost at reboot, and the persistent record is `state.json` plus the service's audit entry. Checked in G6.1 (V6).

### U9. Authorization in the GUI

- **Proposal: routes** (all behind a valid session, ADR 0009 E9 / D-C46; every state-changing one with the CSRF layers of ADR 0009 E3 unchanged, JSON only, body at most 1 KiB, correlation ID by the server): `GET /api/v1/update` (installed version, available version, state, last result, helper presence), `POST /api/v1/update/check`, `POST /api/v1/update/apply`, `POST /api/v1/update/discard`. There is no route that takes a path, a URL or a command.
- **Proposal: administrator confirmation (ORB-UPD-002).** The GUI shows installed and available version, channel, release notes, download size, signature status, migrations (none exist yet) and rollback ability (spec 9.5), and states that the service restarts and everybody is signed out. `apply` takes `{"version": ..., "current_password": ...}`.
- **Proposal: a fresh password confirmation is required for `apply`.** It is checked through the same global limiter and the same single Argon2id hash as login and password change (ADR 0009 E6, amendment A3). Reason: an update replaces the code that runs on the device, it is the most consequential action the GUI offers so far, and a stolen or left-open session should not be enough. Cost: one hash (442.9 ms on the reference device, **Fact**: `docs/AUTH.md`). The hash is computed **before** the download starts and its memory is returned to the system, so the 19 MiB hash and the download never overlap (U12). `version` must equal the version of the last check; the helper validates it again.
- **Proposal: no automatic upgrade in the MVP.** No timer, no policy setting, no background download. The finishing entry in the audit is the only thing that happens without a click, and it is not an action.
- **Proposal: the transaction survives the end of the session.** After the confirmation the flow does not depend on the browser: a session that expires does not stop it. A restart of the service before `stage` ends it harmlessly (nothing privileged happened); after `activate` the helper finishes alone. The page polls `GET /api/v1/update` every 2 s while an update is running, and shows the result after the administrator has logged in again.

Alternatives considered: *one click without a password* (adequate for a service restart, not for replacing the program), *a separate "activate now" confirmation after staging* (more clicks; the stop point exists in the protocol anyway and can be exposed later), *letting the helper check the password* (would put Argon2id and `account.json` into a root process).

### U10. Hardening

- **Proposal: the helper unit** (documentation of the intent; the real files are written in plan step G6.3, then checked with `systemd-analyze verify` in CI and `systemd-analyze security` on the device):

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

  # oarfish-update-helper.service
  [Service]
  Type=exec
  ExecStart=/usr/libexec/oarfish-update-helper serve
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

  `oarfish-update-recover.service` uses the same sandbox with `Type=oneshot`, `ExecStart=… recover`, `Before=oarfish.service`, `WantedBy=multi-user.target`, and no socket.
- **Every place where hardening is relaxed, why, how narrowly, and the risk that stays:**

  | Relaxation | Why it is needed | How narrow | Risk left |
  |---|---|---|---|
  | The helper runs as root (`User=root`) | It writes root-owned release directories and the `current` link and asks systemd to restart a system unit. | Empty capability set (`CapabilityBoundingSet=`): a uid-0 process without capabilities has no `CAP_DAC_OVERRIDE`, `CAP_CHOWN` or `CAP_SYS_ADMIN`; it can only write what it owns (**Assumption**, Linux semantics). It reads the cache because those files are world-readable (U3). **Assumption, verification V1:** `systemctl restart oarfish.service` may need more than uid 0 when talking to systemd. If an empty set does not work, the work stops and the owner decides; the answer must not silently become `CAP_SYS_ADMIN`, which would make the sandbox mostly cosmetic. | A bug in the helper runs as uid 0 inside the sandbox below. |
  | `ReadWritePaths=/opt/oarfish` | Releases and `current` live there. | One directory. `/etc`, `/usr` (including the helper itself) and everything else stay read-only, so the helper cannot rewrite itself or any unit. | Whatever is below `/opt/oarfish` can be damaged by a helper bug (the rollback target is hash-checked, U6). |
  | `AF_INET`, `AF_INET6` | The health check connects to `/healthz` on loopback. | `IPAddressDeny=any` and `IPAddressAllow=localhost` limit traffic to the loopback addresses (**Assumption, V2:** the Raspberry Pi OS kernel supports the cgroup BPF address filter). The unit has no outbound network. | The helper could reach any other service on loopback (for example an SSH tunnel endpoint) if a bug let an attacker choose the destination; the destination is a constant or a validated loopback literal. |
  | No `PrivateNetwork=yes` | A private network namespace would hide the loopback listener of the main service. | See the line above. | Same. |
  | Not a relaxation, stated for clarity: no write access to `/etc/systemd/system` | A unit file in a release would define what runs as root. | U5 refuses releases that change `oarfish.service`. | A hardening change in a release needs one installer run. |

- **Proposal: the main unit needs no relaxation for G6.** Outbound HTTPS needs `AF_INET`/`AF_INET6` (already allowed, **Fact**: `install/oarfish.service`), the system root certificates and resolver files under `/etc` (readable under `ProtectSystem=strict`), writing the cache (`CacheDirectory=oarfish`, already writable), and connecting to the helper socket (`AF_UNIX` is allowed, `/run` is readable). **Assumption, V4:** the Go resolver and TLS stack work under `SystemCallFilter=@system-service` and `ProtectSystem=strict`, and `ca-certificates` is installed on Raspberry Pi OS Lite and Ubuntu Server (the installer already depends on HTTPS to GitHub through `curl`, which is not proof of the file the Go client reads). What must **not** happen is stated here so that G6 does not add it: no `ReadWritePaths=/opt/oarfish`, no capability, no `User=root`, no `NoNewPrivileges=no`, and no `SupplementaryGroups=` for the service. `docs/ARCHITECTURE.md` already requires a narrow, commented relaxation for any gate that needs one; for G6 there is none.
- **Proposal: the cache modes of U3 (0755 directory, 0644 files) are chosen so that the helper does not need `CAP_DAC_READ_SEARCH` or `SupplementaryGroups=oarfish`.**

Alternatives considered: *`PrivateNetwork=yes` and reading the version from `/proc`* (needs `CAP_SYS_PTRACE`, worse than the loopback filter), *granting `CAP_DAC_OVERRIDE`* (a blanket override of file permissions), *`DynamicUser=` for the helper* (it must own root files), *running the helper as the `oarfish` user with `ReadWritePaths=`* (puts releases under the control of the unprivileged service, defeating the point).

### U11. Bootstrap and migration

- **Fact** (`docs/ARCHITECTURE.md`, `docs/INSTALL.md`): no helper exists; `/usr/libexec/oarfish-update-helper` is not created by the installer; a re-run of the installer with another version installs it beside the old one, switches `current`, restarts the service and checks health; the unit file is replaced when it differs.
- **Proposal: the first helper-capable release (the "bootstrap release") is installed by running its `install.sh` on the device.** A device that runs `v0.1.0-alpha.6` has no update page, so there is no GUI path to the bootstrap release; this one-time step is stated plainly in the release notes (as the G5.1 notes did for the setup mode, **Fact**: `docs/RELEASING.md`). From then on updates come through the GUI.
- **Proposal: what the installer must change later (described, not changed here), in plan step G6.3:** (1) extend the bundle contract: `check_bundle_entries` today requires exactly five entries (**Fact**: `install/install.sh`); the bundle gets `oarfish-update-helper` and the three unit files, and the manifest `contents`, `bundlegen check` and `docs/RELEASING.md` follow; (2) install the helper to `/usr/libexec/oarfish-update-helper` (root-owned, 0755) with the same compare-then-replace behaviour as `install_unit`; (3) install `oarfish-update-helper.socket`, `oarfish-update-helper.service` and `oarfish-update-recover.service`, run `daemon-reload`, enable the socket and the recover unit; (4) create nothing for the state (`StateDirectory=` does it); (5) `--uninstall` stops and removes them, `--purge` also removes `/var/lib/oarfish-update`; (6) `--dry-run` lists the new steps; (7) take the same lock as the helper so that an installer run and a GUI update cannot overlap (today "two installers running at the same time are not guarded against", **Fact**: `docs/INSTALL.md`); (8) tests in `scripts/test-installer.sh` for each. The installer's own rollback stays as the safety net for installer runs.
- **Proposal: the helper is replaced only by the installer in G6.** The bundle carries the helper so that the installer can install it, but a GUI update never overwrites `/usr/libexec/oarfish-update-helper` and never touches units. Reason: a helper that the release replaces would turn a leaked release key into root code on every device; without it the same leak yields code running as the sandboxed `oarfish` user. The price is that a helper bug fix or a new embedded key needs one installer run. The manifest field `requirements.helper_protocol` (U4) makes the service and the helper say so instead of failing obscurely. A self-replacement design is the open question 1 and is tied to the key-rotation decisions of U4.
- **Proposal: compatibility.** The release layout, the `current` symlink form, the state directory and the unit `oarfish.service` stay as they are, so an installation made by the installer of any earlier alpha works unchanged (the helper bootstraps its state from `current`, U7). Protocol `v` is 1; the service refuses a helper that answers a different protocol and shows "run the installer again"; the helper refuses a different `v` the same way. The service tolerates a missing socket (no helper installed) by showing the installed version and the installer instruction instead of an update button.

### U12. Resource budget on the Raspberry Pi Zero 2 W

All values are *estimates* until G6.9 measures them.

| Item | Proposal / estimate |
|---|---|
| Helper memory | Idle process: none (socket-activated, exits after 30 s). Running: below 16 MB peak, `MemoryMax=64M` as a hard guard. 32 KiB copy buffer, gzip window 32 KiB. |
| Service memory during an update | No body buffering (32 KiB buffer); TLS and HTTP client add a few MB while a download runs. The Argon2id hash (19 MiB, **Fact**) runs before the download and its memory is returned (`FreeOSMemory`, **Fact**: ADR 0009 A5), so the two peaks do not add. Core idle target stays 35 MB (ORB-NFR-001); service peak during an update below 45 MB; service plus helper below the 80 MB set budget (ORB-NFR-002). |
| CPU | SHA-256 and gunzip over a few MB, one Ed25519 verification: well under 2 CPU-seconds in total on a Cortex-A53; `Nice=10` for the helper. Fsync waits dominate (seconds). |
| Time | Download: 3 MB at 1 Mbit/s is about 25 s. Stage: 5 to 15 s. Restart and stability window: 15 to 25 s. Failure plus rollback: below 2 minutes. |
| Download | Streamed to a file, never held in memory; 40,000,000 byte cap; 30 s stall timeout; 15 min absolute limit; one retry; no resume (U3). |
| Check interval | Only on user action: opening the update page (cached 60 min) or "Check now" (60 s apart). At most a handful of API requests a day against an assumed anonymous limit of 60 per hour per address. |
| Slow or interrupted network | A stalled or cut download is discarded; a partial bundle is never handed to the helper; a message with a code is shown (ORB-NFR-014, R-05). A wrong clock (the Zero 2 W has no battery-backed clock, **Assumption**) makes TLS fail; the message says to check the date. The helper never uses the wall clock for a security decision. |
| microSD writes | About 2 × bundle + extracted size per update (U5) plus under 100 KiB for state, journal and audit. |
| Disk | Two releases kept; free-space rules of U5. |

### U13. Failure matrix and acceptance on the Pi

"Success and failure update verified on the Pi" (spec 14.2, G6; AT-011, AT-012, AT-013) will be shown in plan step G6.9 with three classes of evidence. **No row below is verified; this is the plan.**

- **C** = automated test in CI (fixtures, fake root, fake systemd, fake GitHub).
- **D** = on the reference device with the production binaries and real releases.
- **T** = on the reference device with a test build of the helper that embeds a throwaway public key and offers fault hooks. The state machine code is identical; only the embedded key file and a build tag differ. A release build must not contain the hooks (CI checks the binary). Evidence of class T is labelled as such and is **not** evidence about the production key path.

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
| F12 | Unit changed | Signed release with a different `oarfish.service` | `unit_changed`; GUI points to the installer | C |
| F13 | Hostile archive or sizes | Signed bundle with a symlink, `..` path, extra file, oversized member, size lie | `manifest_invalid`; nothing left behind | C (throwaway key) |
| F14 | Damaged rollback target | Binary of the previous release altered before a failing update | `rollback_failed`, `needs_attention`, no switch to the damaged tree | C, T |
| F15 | Wrong peer | A different uid connects to the socket | Connection refused or closed, no state change | C, D |
| F16 | Concurrent apply | Second `apply` during an update | `busy`; one transaction | C, D |
| F17 | Helper not installed or stopped | Socket unit stopped | GUI shows the installer instruction; no state change | C, D |
| F18 | Offline, rate limited, wrong clock | Network off; fake `403` with reset header; wrong date | Typed GUI message; no loop; installed version still shown | C, D |
| F19 | Success | Real release N+1 offered, applied from the GUI | N+1 active, healthy, audit entries, previous version kept, `current` atomic | D |

**What the G6 closeout evidence must contain, in addition to the table:** the success run F19 with timings; per-process `write_bytes` and peak `VmHWM` of service and helper during one update; `systemd-analyze security` of the helper unit on the device; proof that the helper log and the audit contain no password, token, address or host name; and a list of what was not run. Non-verified combinations are not called Verified (spec 12.4).

### U14. Proposed split of the G6 implementation

Each step is its own session and pull request, with a stop point at which the owner reviews. No step starts before the previous one is accepted.

| Step | Content | Stop point |
|---|---|---|
| G6.0 | Owner accepts or changes this ADR; the follow-up documentation PR adds the D-C entries, the spec corrections of "Source conflicts found" and ADR 0009 amendment A6 text. Docs only. | Owner says accept. |
| G6.1 | Spike on the device, no product code: verifies V1 to V7 (below) with throwaway units and scripts; writes the findings into the docs. | Owner confirms or changes U10 from the findings. |
| G6.2 | Release tooling: bundle contract (helper binary and unit files, additive manifest fields), `bundlegen` checks and tests. No behaviour change on a device. | Dry-run release is green. |
| G6.3 | Installer: installs and removes the helper and its units, lock, tests with the fake root, `systemd-analyze verify` in CI. The helper is still a stub that answers `status`. | Owner runs the installer on the device: socket activation and hardening score observed. |
| G6.4 | Shared verification package (signature, checksums, manifest, SemVer, tar rules) with tamper, fuzz and replay tests ported from `scripts/test-signing.sh`; imported by helper and service. | CI green. |
| G6.5 | Helper core: protocol, peer check, `status`, `stage`, `discard`, staging, state file, free space. | Review of the privileged code. |
| G6.6 | Helper activation: `activate`, `ack`, health, rollback, boot `recover`, retention, fault hooks (test build), crash tests for every row of the U7 table. | Review of the privileged code and of the tests. |
| G6.7 | Service side: GitHub client, check, download, advisory verification, helper client, routes, audit amendment A6, `/healthz` version and `/readyz`. Fake GitHub and fake helper tests. | Review. |
| G6.8 | Update page (Svelte): states, texts in `de` and `en`, accessibility, errors. | Owner reviews the UI. |
| G6.9 | Hardware evidence of U13 on the reference device; documentation (`docs/UPDATE.md`, `INSTALL.md`, `RELEASING.md`, `ARCHITECTURE.md`). | Owner decides whether G6 passes. |

A helper self-replacement path and key rotation are not steps of G6; they follow the owner's answer to open question 1.

## Traceability

| Requirement | Decision | How it is met (proposal) |
|---|---|---|
| ORB-UPD-001 detect releases through GitHub Releases, GUI shows installed and available version | U3, U9 | User-initiated check against the release list; the update page shows both versions. |
| ORB-UPD-002 administrator confirms in the GUI, no automatic upgrade | U9 | Confirmation dialog plus fresh password; no timer, no policy, no background download. |
| ORB-UPD-003 staging, then versioned release directory; active version unchanged until activation | U5 | `.stage.<id>` then `rename` to `releases/<version>`; `current` untouched until `activate`. |
| ORB-UPD-004 atomic activation through `current` | U5, U7 | Temporary symlink and `rename` over `current`, directory `fsync`; table row 6. |
| ORB-UPD-005 service and health endpoints are checked after activation | U6 | Four-part health criteria with stability window and fail-fast. |
| ORB-UPD-006 automatic rollback to the previous healthy version | U6, U7 | Helper-owned rollback with hash-checked target; boot recovery; cooldown. |
| ORB-UPD-007 helper manages Oarfish releases only | U1, U2, U10 | Closed request list, no paths or unit names, one `systemctl` shape set, no unit installation, `ReadWritePaths=/opt/oarfish`. |
| ORB-UPD-008 no Tailscale needed | U3 | Anonymous HTTPS to GitHub only; test F18 and the install/update test without Tailscale. |
| ORB-SEC-001 core and web process unprivileged | U1, U10 | The service gets no privilege and no relaxation; only a socket connection. |
| ORB-SEC-002 privileged operations through a narrow broker | U2 | **Not met by G6 and not claimed.** The broker is G7 (D-C28). The helper is a separate, narrower mechanism and sets the boundary. |
| ORB-SEC-005 restrictive file modes | U3, U8 | `state.json` 0600 in a root-only 0700 directory; the world-readable cache files are public artifacts, stated as such. |
| ORB-SEC-006 administrative actions are audited | U8, U9 | `update_started` and `update_finished` in the audit file, helper phases in the journal, shared correlation ID. |
| ORB-SEC-007 no secrets in audit and logs | U8 | The helper handles no secret; error codes instead of error strings; redaction test. |
| ORB-REL-002 commit, version, architecture, minimum requirements, SHA-256 in the manifest | U4 | Fields used and enforced; two additive fields proposed; kernel and memory minimums still absent (conflict C3). |
| ORB-REL-003, ORB-INS-004 (supporting) | U4 | Signature verified before use, ADR 0005 / 0006 format. |
| AT-011, AT-012, AT-013 (supporting) | U13 | F19, F6/F7, F1/F2/F3. |
| R-05 unstable Wi-Fi during update (supporting) | U3, U7 | Full download before activation, atomic link, rollback. |

## Threat model

Assets: the root-owned code path (helper, release trees, unit files), the integrity of the running version, the availability of the GUI, the administrator's trust in the update page.

| # | Attacker or fault | Threat | Mitigation in this design | Residual risk |
|---|---|---|---|---|
| T1 | Code execution in the unprivileged service (a web-layer bug) | Use the helper to become root | Closed requests, no path or program arguments, helper builds paths itself, content must be validly signed, no unit installation, helper never executes release code, `os.Root` confinement, copy-then-verify, size caps | The attacker can trigger an update to a genuine newer release without a click (the helper cannot see the click), and can cause restarts at the rate the cooldown allows. A newer release is signed and N-1 compatible. |
| T2 | Same, tampering with files between check and use | Swap the bundle after the check | The helper copies and hashes the bytes it then uses; verification applies to its private copy | None known. |
| T3 | Same, symlink or path tricks in the cache | Make the helper read or write elsewhere | Fixed templates, validated version, `os.Root`, `O_EXCL`, regular-file checks | Depends on `os.Root` behaving as documented (V5). |
| T4 | Same, hostile archive | Path traversal, links, devices, bombs | Allow-listed names, regular files only, manifest sizes and hashes, extraction caps | None known. |
| T5 | Network attacker (MITM, DNS, captive portal) | Serve a bad release | HTTPS, allow-listed hosts, signature and hash checks in the helper, strictly-newer rule | Can block updates or hide new ones (freeze); cannot install anything. |
| T6 | Replay of an old, validly signed, vulnerable release | Downgrade | Candidate must be strictly newer than the installed version, which only root can change | A downgrade by an administrator with a shell remains possible by design (recovery workflow). |
| T7 | Stolen or leaked release signing key | Ship a malicious signed release | Release code runs only as the sandboxed `oarfish` user; the helper and units are not replaced by a GUI update (U11); health check and rollback cover a broken release | The malicious release runs as `oarfish` and can read the account hash and audit. Rotation is not designed (U4 list). |
| T8 | Compromised GitHub account or release page | Replace assets | Cannot sign; same as T5 | Release notes are unsigned text (next row). |
| T9 | Hostile release notes from the API | Script injection in the GUI | Notes are shown as plain text only, length limited, control characters removed, labelled "from GitHub, not signed" | Misleading but harmless text. |
| T10 | Other local user | Talk to the helper, read state | Socket mode and `SO_PEERCRED`; state directory 0700 root | Another process running as `oarfish` (a future plugin) can reach the socket (U2, G8). |
| T11 | Power loss, SD fault | Half-installed release, wrong pointer | U7 table, write-ahead state, conservative rollback, hash-checked rollback target | Cheap cards may lose data despite `fsync`; not designed (row 12). |
| T12 | Bad release (crash, hang, data format) | Brick the GUI | Fail-fast health, rollback, N-1 compatibility rule | `rollback_failed` needs a shell (U6). |
| T13 | Resource exhaustion on the Pi | Memory or CPU starvation of the dashboard | Streaming, `MemoryMax=64M`, `Nice=10`, hash before download, caps | Unmeasured until G6.9. |
| T14 | Installer and GUI update at the same time | Inconsistent tree | Shared lock proposed for the installer (U11) | Until the installer takes the lock, concurrent runs are not guarded. |
| T15 | Wrong clock (no battery-backed clock, **Assumption**) | TLS failures; misleading audit times | TLS failure is a typed message; the helper never uses time for security | Audit times may be wrong until the clock is set. |
| T16 | Information leakage | Host names, addresses, serial numbers in logs, state, audit or this repository | No such value is read, stored or logged; errors are codes | Journal lines of the system and of GitHub's servers are outside Oarfish. |

## Alternatives considered (summary of rejected approaches)

Always-running daemon, on-demand oneshot, setuid binary, `.path` request files, D-Bus (U1); loopback HTTP, command-line requests, generic RPC (U2); helper downloads, in-place verification in the cache, `memfd` handoff, streaming into the helper (U3); `minisign`/`openssl`/TUF library, trusted-comment binding, manifest counter, API prerelease flag (U4); extracting in place, keeping all releases, second rollback pointer (U5); service-reported health, watchdog-only, GUI decides (U6); resume at boot instead of rollback (U7); state under `/var/lib/oarfish`, audit written by the helper (U8); no password, extra activate click (U9); `PrivateNetwork=yes`, broad capabilities, `DynamicUser=` (U10); helper replaced by every release, installing units from the bundle (U11).

## Consequences

- The release bundle, the manifest, `bundlegen`, the installer, `docs/INSTALL.md` (old releases are pruned, a helper exists), `docs/RELEASING.md` (bundle contents, N-1 rule) and `docs/ARCHITECTURE.md` (the "No privileged code exists" paragraph) change when G6 is implemented. This ADR changes none of them.
- A new root-owned program exists for the first time. Its code is small, standard library only and reviewed in two steps (G6.5, G6.6); every release that changes it needs an installer run (U11).
- The update state is not in `/var/lib/oarfish`; the spec and D-C47 text need a correction (C1).
- A power failure in the verification window can roll back a good update (U7). A release that changes `oarfish.service` cannot be installed from the GUI (U5, question 2).
- `/healthz` gains a version member and `/readyz` finally exists (U6).
- `rollback_failed` is the one state that needs a shell. A manual rollback button, a GUI path for helper replacement, key rotation, LAN access and HTTPS (D-D10), plugin isolation from the helper socket (G8) and OS package updates (ORB-OSU) are outside this ADR.

## Evidence and test plan for the later implementation

1. **Unit and fixture tests (CI).** SemVer precedence table with prerelease cases; `checksums.txt` parser (duplicates, bad digits, extra whitespace); signature verification against the vectors in `scripts/bundlegen/testdata/` and a port of every case of `scripts/test-signing.sh` (tampered file, signature, other key, replay, prehashed `ED`); manifest validation per field; tar rules (traversal, links, devices, duplicates, oversize, mode); request decoder (unknown fields, size, version); peer check with a fake credential source.
2. **Fuzzing.** Native Go fuzz targets for the request decoder, the checksums parser, the manifest decoder, the version parser and the tar inspector, added to `make fuzz` like the existing parsers.
3. **Architecture tests.** The helper imports only the standard library; only one file imports `os/exec` and only with the three argument shapes; the compiled-in `op` list equals the table of U2; the helper binary has no network client beyond the health check; a release build contains no fault hook.
4. **Crash tests.** A file system abstraction that fails or stops after the k-th mutating call; for every k, the recovery invariant of U7 must hold; every row of the power-loss table has a test.
5. **Fake systemd and fake GitHub.** A stub `systemctl` (as in the installer tests, **Fact**: `docs/INSTALL.md` says the installer's stub proves logic, not systemd behaviour) for start failure, crash loop and slow start; a TLS test server for the service with rate-limit headers, redirects, slow, truncated and oversized bodies.
6. **Unit files.** `systemd-analyze verify` in CI for the three helper units (the same job that checks the main unit, **Fact**: `.github/workflows/ci.yml`); `systemd-analyze security` score recorded on the device, not asserted in CI.
7. **Redaction.** A test that runs the whole flow with a marker password and checks that no log line, state file or audit entry contains it, any address, or any host name.
8. **Hardware (G6.9).** Table U13; measurements of U12; every number from this ADR that is an estimate is replaced by a measurement or left labelled.

## Proposed decisions (draft numbers, for the owner to accept, change or drop)

These are not entries of `docs/DECISIONS.md`. The prefix follows the convention ADR 0008 used for its drafts.

| Draft | Proposal | Depends on |
|---|---|---|
| D-DRAFT-G6-01 | Helper is a root, socket-activated unit with a boot-time recover unit, own binary, no dependency on `oarfish.service` (U1). | |
| D-DRAFT-G6-02 | Unix socket, `SO_PEERCRED`, closed list of five typed requests, one `systemctl` shape set; the helper never becomes a broker (U2). | |
| D-DRAFT-G6-03 | The service downloads and caches; the helper copies, hashes and verifies its own copy (U3). | |
| D-DRAFT-G6-04 | Verification in the helper with the embedded key, strictly-newer-than-installed rule, prerelease rule, additive manifest fields (U4). | Q7 |
| D-DRAFT-G6-05 | Staging, atomic rename, durability recipe, free-space rule, keep current plus previous (U5). | |
| D-DRAFT-G6-06 | Helper-decided health criteria, hash-checked rollback, cooldown, N-1 data compatibility rule (U6). | Q5 |
| D-DRAFT-G6-07 | Write-ahead state machine; unfinished transactions roll back (U7). | |
| D-DRAFT-G6-08 | Helper state in root-owned `/var/lib/oarfish-update`; audit by the service plus helper journal (U8). | Q6 |
| D-DRAFT-G6-09 | Fresh password for `apply`; no automatic upgrades (U9). | Q3 |
| D-DRAFT-G6-10 | The hardening of U10; no relaxation of the main unit. | V1, V2 |
| D-DRAFT-G6-11 | Bootstrap by installer re-run; helper and units replaced only by the installer in G6 (U11). | Q1 |
| D-DRAFT-G6-12 | The resource budget of U12 as targets to be measured. | |
| D-DRAFT-G6-13 | Evidence classes C, D, T and the failure matrix (U13). | Q4 |
| D-DRAFT-G6-14 | The split G6.0 to G6.9 (U14). | |
| D-DRAFT-G6-15 | ADR 0009 amendment A6: update actions, target pattern and results in the audit sets (U8). | |
| D-DRAFT-G6-16 | `/healthz` reports the version; `/readyz` is implemented (U6). | Q5 |
| D-DRAFT-G6-17 | A release that changes `oarfish.service` needs the installer (U5). | Q2 |

## Open questions for the owner

Each has a recommended answer; the design above assumes the recommendation.

1. **How is the helper itself replaced after G6?** Recommended: only by the installer in G6; decide a GUI path together with key rotation before the MVP (U4, U11). The alternative, letting each release replace it, makes a leaked release key equal to root on all devices.
2. **May a release that changes `oarfish.service` be installed from the GUI?** Recommended: no, refuse with `unit_changed` and ask for an installer run until the unit has settled (U5, U10).
3. **Is a fresh password required to apply an update?** Recommended: yes (U9).
4. **How are start and health failures shown on the Pi?** Recommended: a test build of the helper with a throwaway key and fault hooks, labelled class T. The alternative is to publish a deliberately broken signed prerelease, which burns a version number and is offered to every device (U13).
5. **May `/healthz` report the Oarfish version?** Recommended: yes; it is public data (spec 10.3) and the installer is unaffected. It amends the wording of D-C46 (U6).
6. **Is update state allowed to live in a root-owned `/var/lib/oarfish-update` instead of `/var/lib/oarfish`?** Recommended: yes, and correct spec 8.2 and D-C47 accordingly (U8, C1).
7. **Prerelease rule:** a device on a prerelease offers prereleases and stable releases, a device on a stable release only stable ones, until a beta setting exists. Recommended: yes (U4).

## Source conflicts found

No conflict blocks a decision. Each item names the affected text and the smallest correction proposed.

| # | Affected | Conflict or gap | Smallest formal correction |
|---|---|---|---|
| C1 | Spec 8.2 row `/var/lib/oarfish/` ("Updatezustand"), D-C47 | Update state in a directory owned by the unprivileged service cannot be trusted by a root helper (U8). | Spec 8.2: add a row `/var/lib/oarfish-update/` "Root-eigener Zustand des Update Helpers" and remove "Updatezustand" from the `/var/lib/oarfish/` row; D-C47: add "except the state of the update helper, which lives in `/var/lib/oarfish-update`". |
| C2 | Spec 10.1 item 6 ("Privilegierte Änderung erfolgt ausschließlich über den Broker") against item 7, spec 8.6 and D-C28 | The update helper makes privileged changes and is not the broker. | Item 6: "…ausschließlich über den Broker oder, für Oarfish-Releases, über den Update Helper". |
| C3 | ORB-REL-002 ("Mindestanforderungen"), spec 9.5 step 2 ("Mindestversion") against `docs/RELEASING.md` | The manifest has no kernel or memory minimum and no minimum installed version; the release docs already say ORB-REL-002 is met "for architecture, OS and libc only". | Add the optional fields of U4 (`requirements.min_installed_version`, `requirements.helper_protocol`) and state in the ORB-REL-002 acceptance column what the MVP checks; kernel and memory minimums after a measurement. |
| C4 | Spec 8.1 (Update Manager does "Verifikation") and 9.5 steps against spec 8.6 (helper checks "ein bereits heruntergeladenes, signiertes Bundle") | Two places verify; the actor of each step in 9.5 is not named. Spec 8.6 also does not mention the helper's own state, `reset-failed` or reading the service state. | 8.1: "Update Manager (unprivilegiert): Erkennung, Download, Vorabprüfung"; 8.6: add "den eigenen Zustand führen" and "den Zustand von `oarfish.service` lesen". |
| C5 | D-C46 / ADR 0009 E9 / `docs/AUTH.md` ("minimal health check that exposes no device data", body `{"status":"ok"}`) | The health decision needs the running version (U6). | State that the Oarfish version is not device data (spec 10.3 class "Öffentlich") and add the `version` member to `docs/AUTH.md` when implemented. |
| C6 | ADR 0009 E7 closed audit sets (`audit.go`) | The update actions are not in the sets. | Amendment A6 (U8), accepted by the owner like A1 to A5. |
| C7 | `docs/INSTALL.md` ("Old release directories are kept"), installer `check_bundle_entries` (exactly five entries), spec 8.2 (the helper path is in the layout), `docs/ARCHITECTURE.md` ("No privileged code exists") | The implementation changes behaviour these texts describe. | Update the texts in the steps G6.2, G6.3 and G6.9; no change now. |
| C8 | Spec 9.6 ("stable ist der Defaultkanal") against the alpha phase (every tag is a prerelease, `docs/RELEASING.md`) and the installer note that `releases/latest/download/` is untested (`docs/INSTALL.md`) | A "stable default" channel offers nothing while only prereleases exist. | Adopt the prerelease rule of U4 (question 7) as a decision and revisit it with the first stable release. |
| C9 | The task text mentions open `D-DRAFT` items | **None found:** `docs/DECISIONS.md` has no open `D-DRAFT` entries at this commit (ADR 0008 says its drafts became D-C31 to D-C41). Open `D-D` items are D-D05 (G10), D-D06 (G12) and D-D10, none of them a G6 matter; D-D10 (LAN and HTTPS) touches this ADR only through the health route (U6). | None. |

## Assumptions to verify

| # | Assumption | Where used | Verified by |
|---|---|---|---|
| V1 | `systemctl restart oarfish.service` works from a uid-0 process with an empty capability set under the helper's sandbox. | U10 | G6.1 on the device; if not, stop and ask the owner. |
| V2 | Socket activation, `SocketGroup=`, `DirectoryMode=` and the `IPAddressDeny=any` / `IPAddressAllow=localhost` filter work on the Raspberry Pi OS kernel and systemd. | U1, U2, U10 | G6.1. |
| V3 | GitHub: anonymous API limit and `304` handling; hosts of the redirect target for release assets; behaviour of `latest` for prereleases. | U3 | G6.1 against the real service, results written down. |
| V4 | The Go TLS client and resolver work under the unchanged service unit; root certificates are present on Raspberry Pi OS Lite and Ubuntu Server. | U10 | G6.1 and G6.7. |
| V5 | `os.OpenRoot` exists in the Go toolchain of `backend/go.mod` and confines symlinks as documented. | U3 | G6.5 (compile and test). |
| V6 | Whether `journald` is persistent on the reference image; whether the stock start limit of the unit (default `StartLimitBurst`) is hit by a crash loop within the 45 s window. | U6, U8 | G6.1. |
| V7 | ext4 ordered-mode rename and `fsync` behaviour on the device's microSD card; memory, CPU, duration and write-load numbers of U12. | U5, U7, U12 | G6.9 (partial: durability cannot be proven by software tests). |
| V8 | The Zero 2 W has no battery-backed clock. | U12, T15 | G6.1 (`timedatectl` on a cold boot). |
| V9 | Release sizes (the local build is not a release). | U3, U5, U12 | G6.2 on the first dry-run bundle. |
