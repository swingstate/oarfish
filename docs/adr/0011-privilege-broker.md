# ADR 0011: Privilege Broker: a socket-activated root process with a closed, typed allowlist for systemd services, reboot and shutdown

- Status: **Accepted** (2026-11-23, accepted by the project owner in chat, recorded in `docs/DECISIONS.md` as D-C87, with the changes of [amendment A1](#amendment-a1-g70-owner-decisions); proposed in the design stop G7.0, PR #53)
- Amendment A1 (G7.0 owner decisions, written 2026-11-23, D-C87): the owner's decisions on the draft items, two changes (item 6, item 11), one open item (16) and one added task for the spike G7.1, listed in "Amendment A1 (G7.0 owner decisions)" below. It edits no sentence of the body; where the body speaks of itself as Proposed or as a proposal, the amendment governs.
- Amendment A2 (G7.1 device spike findings, written 2026-11-23): the results of the device spike G7.1 with throwaway units on the reference device, one verdict per assumption, the service list for amendment A1.5, and draft changes to B2, B3 and B8 marked "proposal, not applied", listed in "Amendment A2 (G7.1 device spike findings)" below. It edits no sentence of the body and no row of "Assumptions to verify" and takes no decision; the owner decides B2, B3 and B8 from it (stop point of G7.1 in B10).
- Amendment A3 (G7.1 owner decisions, written 2026-11-23, D-C88): the owner's decisions on the proposals of amendment A2.5, "wie empfohlen", with the full text of every rule they change (B2, B3, B8), the changed rows of the failure matrix and what stays open, listed in "Amendment A3 (G7.1 owner decisions)" below. It edits no sentence of the body; where the body and A3 differ, A3 governs. It is a decision, not evidence: nothing in it is verified.
- Amendment A4 (G7.2 broker core as implemented, written 2026-11-24, D-C89): the owner's decisions for G7.2 (D-C89 items 2 to 7 and four accepted choices) as the rules that now apply, the broker as built (files, functions, the environment of `systemctl`, the bound of `correlation_id`, how test seams stay out of the release binary), every choice the code made where this ADR did not fix one, and the class C tests per row of the failure matrix, listed in "Amendment A4 (G7.2 broker core as implemented)" below. There is no deviation from the body, A1 or A3. It edits no sentence of the body, A1, A2 or A3 except two pointer sentences in A3.7 and A3.8. It is class C evidence only: nothing in it is verified on a device.
- Amendment A5 (G7.4 service side and Activity page as implemented, written 2026-11-25): the service side of the broker as built (the read path R1, the broker client, the routes, the action job, the service values of ADR 0009 A7 in the audit package) and the page Aktivität, the files and functions, every choice the code made where this ADR did not fix one, the class C tests per row of the failure matrix, what is not verified and what stays open, listed in "Amendment A5 (G7.4 service side and Activity page as implemented)" below. There is no deviation from the body or from A1 to A4. It edits no sentence of the body or of A1 to A4 except one pointer sentence in A4.8. It is class C evidence only: nothing in it is verified on a device, in a real browser or against the real broker.
- Amendment A6 (G7.5 reboot and shutdown as implemented, written 2026-11-25): the two power operations in the broker client, the power routes with the password and the list of waiting actions, the power values of ADR 0009 A7 with the flush and `fsync` before the answer, and the card, dialog and status view on the page Aktivität, the files and functions, every choice the code made where this ADR did not fix one, the class C tests per row of the failure matrix, what is not verified and what stays open, listed in "Amendment A6 (G7.5 reboot and shutdown as implemented)" below. One deviation, in the texts: the "Bitte" sentence of the B6 table. It edits no sentence of the body or of A1 to A5 except one pointer sentence in A4.8 and one in A5.8. It is class C evidence only: nothing in it is verified on a device, in a real browser or against the real broker, and no real reboot or shutdown was made.
- Amendment A7 (G7.6 release and device evidence, written 2026-11-26): the first release that carries the broker (`v0.1.0-alpha.13`, the first real run of the two release checks), the installer run with the broker on the reference device and the device evidence per row of the failure matrix and per open device check of A4 to A6, each with its class (C, D or T), what was not observed, one finding (a unit name with an escape is answered `internal`), the open points and the deviations, listed in "Amendment A7 (G7.6 release and device evidence)" below. It takes no decision and edits no sentence of the body or of A1 to A6. Class D evidence is one device, one image, one moment: nothing is Verified, and the reference device stays Compatible. Whether G7 passes is the owner's decision (D-C87 item 15).
- Amendment A8 (G7-FIX1, quoted names in `show`, written 2026-11-26): the owner's decision that G7 passes (D-C100), systemd's quoting of the property `Names` in the `show` shape with its source, the fix of finding A7.5 in the broker (an entry of `Names` is unquoted by that rule before rule 1 is applied to it), the check of the service side, the tests and the mutations, and three page fixes (the wrong-password sentence of the power dialog, a count line of the page Aktivität that adds up, the tunnel sentence of the status view when the device is back), listed in "Amendment A8 (G7-FIX1, quoted names in show)" below. It edits no sentence of the body or of A1 to A7 except one pointer sentence in A7.5 and one in A7.7. It is class C evidence until the device check of G7-FIX1; the device result (the release `v0.1.0-alpha.14`, the GUI update that replaced the broker, the page with an escaped unit, one finding) is in A8.8, class D on one device, nothing Verified.
- Amendment A9 (UF1, the read path and the Ubuntu Wi-Fi unit, written 2026-11-27, D-C104): the unit-file states in their own store with their own deadline (findings U1 and U4 of [`UBUNTU_PROBE.md`](../UBUNTU_PROBE.md)), the rule that answers a list request from the last unit list while a unit-file read runs, the added fields of `GET /api/v1/services`, the family `netplan-wpa-` in rule 11, the tests and the mutations, listed in "Amendment A9 (UF1, the read path and the Ubuntu Wi-Fi unit)" below. It edits no sentence of the body or of A1 to A8 except one pointer sentence each in A3.2, A3.4 and A5.2. It is class C evidence; on Ubuntu it is not rechecked (D-C104 item 3).
- Date: 2026-11-23
- Gate: G7, **passed** by D-C100 (2026-11-26), steps G7.0 to G7.6d and the follow-up G7-FIX1 in the amendments A1 to A8; the fix task UF1 in A9. Task C1 (2026-11-27): the section [Current state](#current-state) and a risk added to [Risks](#risks) (the reboot loop)
- Written against: `main` at commit `4f1b5d13accc7373e5fee586c0defe45e9b7ae0c` (merge of PR #52)
- Decision numbers: this ADR uses draft numbers `D-DRAFT-G7-nn` only. They become `D-C87` and later when the owner accepts them (`D-C82` stays reserved). `docs/DECISIONS.md` is not changed by this ADR.
- Amends nothing. Where an accepted text (ADR 0009, ADR 0010, the specification) would need a change, this ADR lists it as a proposal in "Conflicts and gaps" and in "Amendments this ADR would need".

## Contents

1. [Current state](#current-state)
2. [How to read this ADR](#how-to-read-this-adr)
3. [Context](#context)
4. [Facts found](#facts-found)
5. [Decisions proposed](#decisions-proposed) (B1 to B10)
6. [Failure matrix](#failure-matrix)
7. [Threat model](#threat-model)
8. [Rejected alternatives (summary)](#rejected-alternatives-summary)
9. [Requirements and what G7.0 meets](#requirements-and-what-g70-meets)
10. [Not decided here](#not-decided-here)
11. [Conflicts and gaps](#conflicts-and-gaps)
12. [Amendments this ADR would need](#amendments-this-adr-would-need)
13. [Risks](#risks)
14. [Draft decisions for the owner](#draft-decisions-for-the-owner)
15. [Assumptions to verify](#assumptions-to-verify)
16. [Amendment A1 (G7.0 owner decisions)](#amendment-a1-g70-owner-decisions)
17. [Amendment A2 (G7.1 device spike findings)](#amendment-a2-g71-device-spike-findings)
18. [Amendment A3 (G7.1 owner decisions)](#amendment-a3-g71-owner-decisions)
19. [Amendment A4 (G7.2 broker core as implemented)](#amendment-a4-g72-broker-core-as-implemented)
20. [Amendment A5 (G7.4 service side and Activity page as implemented)](#amendment-a5-g74-service-side-and-activity-page-as-implemented)
21. [Amendment A6 (G7.5 reboot and shutdown as implemented)](#amendment-a6-g75-reboot-and-shutdown-as-implemented)
22. [Amendment A7 (G7.6 release and device evidence)](#amendment-a7-g76-release-and-device-evidence)
23. [Amendment A8 (G7-FIX1, quoted names in show)](#amendment-a8-g7-fix1-quoted-names-in-show)
24. [Amendment A9 (UF1, the read path and the Ubuntu Wi-Fi unit)](#amendment-a9-uf1-the-read-path-and-the-ubuntu-wi-fi-unit)

## Current state

What holds today (task C1, 2026-11-27), so that a reader does not have to read every amendment. The amendments stay as the history and govern where they say more. G7 passed (D-C100); the device evidence is in A7 and A8.8. Nothing is Verified; the reference device is Compatible.

- **Process.** `oarfish-broker` runs as root, socket-activated on `/run/oarfish-broker/broker.sock` (0660 `root:oarfish`, peer uid checked with `SO_PEERCRED`), keeps no persistent state and has no recover unit, an empty capability set, no writable path and no network; standard library only (B1, B8, A4). It exits 30 s after its rate window is empty.
- **Operations.** A closed list, protocol 1: `status`, `service.start`, `service.stop`, `service.restart` (with a unit name) and `system.reboot`, `system.poweroff` (A4.3). No argument is a path, a program, a verb or a property; the only program is `/usr/bin/systemctl` with fixed shapes.
- **Units.** The broker reads the unit itself and applies the rules of B3 as amended by A3, A8 and A9: syntax and canonical name (a quoted entry of `Names` is unquoted first, A8), a deny list, no one-shot, transient or template-creating starts, and services that can cut the access path are restart-only.
- **Reading.** The service lists the units itself with two read-only `systemctl` shapes (R1, A5); the unit-file states are kept apart from the unit list and read at most once per 30 minutes, at a list request (A9). The page Aktivität shows and filters them.
- **Authentication.** A session for every route, CSRF as ADR 0009; reboot and shutdown need the current password through the shared limiter (B4, A6); service actions need one confirmation click. The broker trusts the caller's uid, not the person.
- **Limits in the broker.** One mutating operation at a time, at most 10 service operations per 60 s, one accepted power operation per boot, and no reboot or shutdown while an update transaction holds the helper's lock or phase (B4, B6).
- **Packaging.** The broker and its two units are part of the helper's component set (L1, ADR 0010 A20 to A22), so a change of them needs the second confirmation of a component update; the first GUI update that replaced the broker ran in G7-FIX1 (A8.8).
- **Open.** The capability check for plugins is an interface only (B7, G8). D-C101 items 1 to 3 let the design block revise the broker's role: one privileged system service (or two with a common pattern) with a catalogue of typed operations that grows without an installer run, and the authentication rule for root actions.

## How to read this ADR

Every statement carries one of these labels, as in [ADR 0010](0010-update-helper.md#how-to-read-this-adr):

- **Fact** names the file (and where useful the line) it comes from. It was read in the tree of `main` named above while writing this ADR.
- **Device observation** is a result that a repository document records with its evidence label (ADR 0010 amendments, `docs/UPDATE.md`, `docs/DASHBOARD.md`, `docs/AUTH.md`). It is one reading of one device and is never called Verified. This ADR makes no device observation of its own; no device was touched for it.
- **Assumption** is a claim about systemd, Linux, the hardware or a browser that was recalled, not checked. Each one is listed again under [Assumptions to verify](#assumptions-to-verify) with the step that checks it. Statements about general systemd behaviour that are not tied to a repository file are assumptions even where they are not marked again.
- **Proposal** is a recommendation of this draft. It is not decided. It becomes decided design only when the owner accepts it, and then it is still not evidence that anything works. Numbers marked *estimate* are not measurements.

**Nothing in this ADR is verified, on a device or otherwise.** The design stop writes no code, no unit file, no installer change, no test and no workflow change. The unit text in B8 is documentation, not a file that is installed.

## Context

Gate G7 adds the service view, start, stop and restart of systemd services, reboot and shutdown, and the Privilege Broker (specification 8.6, ORB-SEC-002). The journal and the process view belong to G7 in the specification but are reserve behind the MVP acceptance (D-C69 item 5). The broker's capability check (ORB-SEC-003) is G8 (D-C28). G6 is closed (D-C86), so G7 may start (D-C63).

The specification fixes goals, not mechanisms. It says that privileged operations go through a narrowly bounded broker with typed arguments (ORB-SEC-002), that the broker has a fixed allowlist with examples such as `service.restart(unit)` and `system.reboot()` (8.6), and that there is no general shell (8.6, 10.1). It does not say how the broker is started, how the service reaches it, what exactly it may do to a unit, how a unit name is validated, who audits, what happens to a reboot during an update, or how the broker reaches a device. This ADR proposes the answers.

ADR 0010 U2 already sets the boundary from the other side: the update helper never becomes a broker; every privileged operation outside Oarfish's own release and component lifecycle goes to the broker, "a separate process with its own socket, unit and code"; the two processes share no request-handling code.

## Facts found

What exists today, with the file it was read from. Line numbers are those of the commit named in the header.

### The specification and the decisions

- **Fact** ([`PFLICHTENHEFT.md`](../PFLICHTENHEFT.md), 6.3 and 6.4): ORB-PWR-001 (MUSS): reboot and shutdown as GUI actions that need a confirmation, authorized and audited. ORB-PWR-002 (SOLL): a restart shows waiting actions and connected users; the dialog names possible effects. ORB-SVC-001 (MUSS): services listed with a filter, name, state, start mode and short description visible. ORB-SVC-002 (MUSS): start, stop and restart with a description of the effect and a confirmation; authorized, logged and verified. ORB-SVC-003 (SOLL): critical services marked; network, SSH, Oarfish and container engines show an additional warning. ORB-LOG-002 (MUSS): every display of logs **and system files** masks secrets.
- **Fact** (specification 6.11): ORB-SEC-001 (core and web process unprivileged, no blanket sudo), ORB-SEC-002 (narrow broker, only defined operations with typed arguments), ORB-SEC-003 (the broker checks board and plugin capabilities; operations not granted are refused technically), ORB-SEC-004 (CSRF and origin), ORB-SEC-006 (audit with time, user, plugin, action, target, result, correlation ID), ORB-SEC-007 (no secrets in audit and logs).
- **Fact** (specification 8.6): the broker provides no general shell and implements a fixed allowlist, for example `service.restart(unit)`, `system.reboot()`, `tailscale.login(flow_id)`, `container.restart(engine, container_id)`. The update helper is separate and may only do the listed things, among them "die eigene Binärdatei und die zugehörigen Unit-Dateien" replace.
- **Fact** (specification 10.1, items 6 and 7): a privileged change goes only through the broker or, for Oarfish releases and its own components, the helper; helper and web process are strictly separated. 10.2 lists example capabilities, among them `service.read` and `service.control:tailscaled`.
- **Fact** (specification 5.2, 5.3): the uniform action pattern reads the state, validates, previews, asks for confirmation, authorizes, executes and verifies the new state, and shows result, recovery hint and audit entry. A change of the network may be offered only when Oarfish has a traceable way back; otherwise it is read-only or not available.
- **Fact** (specification 7.2): the page "Activity" in the group System holds services, restart, reboot and shutdown, with journal and processes as reserve.
- **Fact** (specification 14.2, G7 row): delivery "Dienste, Journal, Prozesse, Reboot und Shutdown. Privilege Broker mit fester Allowlist (ORB-SEC-002)"; exit criterion "Sicherer Dienstneustart End-to-End". Section 16, item 12: dashboard, services (display, restart), reboot and shutdown work on real hardware.
- **Fact** (specification 11): ORB-NFR-001 idle RSS of the core at most 35 MB (45 MB tolerated), ORB-NFR-002 the whole Oarfish process set below 80 MB peak RSS, ORB-NFR-009 minimal microSD writes.
- **Fact** ([`DECISIONS.md`](../DECISIONS.md)): D-C28 (broker in G7, capability check in G8); D-C63 (G7 starts after G6.10); D-C69 item 2 (the installation script is the only shell action; everything after it runs in the interface) and item 5 (showing and restarting services, reboot and shutdown are mandatory; journal and process view are reserve); D-C70 (LAN direction; the installer budget is an open point of D-D10); D-C75 (Files read-only after G7; Tailscale installation needs its own typed broker action and design stop); D-C80 (the bytes of a privileged binary depend only on its code, never on the release version); D-C81 (boot order of the recover unit); D-C84 and D-C85 (component transaction, the two-file change of `v0.1.0-alpha.12`); D-C86 (G6 closed; G7 may start).
- **Fact** ([`ROADMAP.md`](../ROADMAP.md), line 36): the G7 row says G7 has not started. Line 51: the owner works one task at a time; G7 starts with this design stop. Lines 32, 34, 35 and 45 are the rows ST1, SET1, ONB1 and VER1; they are not part of G7 and are not touched.

### The service today

- **Fact** (`backend/internal/platform/capabilities/capabilities.go`, lines 84 to 86): the only contact with systemd is a probe for the directory `run/systemd/system` and the file `run/systemd/journal/socket`. **The service reads no unit, no unit state and no journal today, and starts no program to do so.** A search of `backend/internal` and `backend/cmd/oarfish` for `systemctl`, `dbus` and `/run/systemd` finds nothing else.
- **Fact** (`install/oarfish.service`): the service runs as the user `oarfish` (line 13), `NoNewPrivileges=yes` (33), empty `CapabilityBoundingSet=` (34), `ProtectSystem=strict` (36), `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` (50), `SystemCallFilter=@system-service` (52), `TimeoutStopSec=15` (18), `RuntimeDirectory=oarfish` (28), and listens on a literal loopback address (15).
- **Fact** (`backend/internal/server/api.go`, lines 76 to 84): only the account endpoints and the update routes accept a state-changing method; every route except session status, login and setup needs a session. `backend/internal/server/auth.go`: the Origin and `Sec-Fetch-Site` check (`originAllowed`, line 110), the per-session token in `X-Oarfish-CSRF` (lines 29, 139), JSON only.
- **Fact** (`backend/internal/auth/manager.go`, lines 263 to 275): `ConfirmPassword` checks the current password through the shared limiter (`m.allow`) and the single Argon2id hash. `backend/internal/server/update.go`, lines 149 to 209: the update route `apply` uses it for the first stage and again for the second confirmation (A17 D7), and refuses `components_confirmed` without a waiting job before the password (A17 G1, lines 167 to 173).
- **Fact** (`backend/internal/auth/session.go`): sessions live in memory, at most 8 (`MaxSessions`, line 19); `Sessions.Len` (line 166) returns their number. A restart signs everybody out.
- **Fact** (`backend/internal/audit/audit.go`): the audit file has fixed typed fields (`entry`, lines 180 to 189: `time`, `user`, `plugin`, `action`, `target`, `result`, `correlation_id`, `count`); actions, targets and results are closed sets (lines 42 to 153); `Event` has **no plugin member** (lines 170 to 176), so `plugin` is always empty today. Entries are buffered and written 30 s after the first one, on rotation and at `Close` (lines 6 to 8, 349, 441); the data is not `fsync`ed, only the directory at rotation (line 425). `backend/cmd/oarfish/main.go`, lines 74 and 87 to 89: SIGTERM ends `run`, and the deferred `Close` writes the buffer.
- **Fact** (`backend/internal/updater/helper.go`): the client of the helper socket (`SocketPath`, line 19) sends one request per connection with a per-request timeout (lines 24 to 34), retries once on `ECONNRESET` or `ENOENT` (lines 155 to 178), maps an unreachable socket to `unavailable`, and accepts only closed sets of phases, outcomes, names and codes (lines 125 to 139). `HelperAPI` (lines 85 to 91) is the interface the tests fake. Error codes are in `backend/internal/updater/errors.go` (lines 21 to 59).
- **Fact** (`frontend/src/routes.ts`, lines 28 to 33): the pages are Dashboard, Gerät, Update and the account page. There is no Activity page.
- **Device observation** (`docs/DASHBOARD.md`, the hardware results of 2026-10-27, one device, once): service idle `VmRSS` 8480 kB; with one browser tab on Home `VmHWM` 12924 kB. (`docs/UPDATE.md`, Phase 2): service during an update download `VmHWM` 33,316 kB.

### The helper today

- **Fact** (`backend/cmd/oarfish-update-helper/peercred_linux.go`, lines 11 to 23): the peer uid comes from `SO_PEERCRED`.
- **Fact** (`backend/cmd/oarfish-update-helper/systemctl.go`, lines 14 to 27, 73): the helper runs `/usr/bin/systemctl` only, without a shell, with a closed set of argument shapes.
- **Fact** (`backend/cmd/oarfish-update-helper/lock.go`, line 23; `txn.go`, lines 76 to 98 and 297 to 300): the helper takes `LOCK_EX|LOCK_NB` on the directory `/var/lib/oarfish-update` and, for `activate`, hands the lock to the transaction, which releases it only when the transaction ends. The installer waits on the same lock (ADR 0010 A9). Between the exit 75 of the old helper and the first start of the new one (component hand-over, ADR 0010 A16.2) no process holds it; the state then says `c_switched`.
- **Fact** (`backend/internal/relverify/layout.go`, lines 15 to 31): a bundle has exactly one of two shapes, four files or those plus the helper and its three units. Any other file name is refused (ADR 0010 U13 F13, "extra file").
- **Fact** (ADR 0010 U15 rule 8, A2): the helper cannot write below `/etc/systemd/system` or `/usr/libexec` (observed `EROFS`, A2); the links there are the installer's.
- **Fact** (`install/install.sh`, line 87 and lines 311, 544, 591 to 596, 701 to 712, 928 to 937): the installer names each helper file and unit explicitly: in the bundle check, the layout preflight, the unit links, the relative wants links and the uninstall.
- **Device observation** (ADR 0010 G6.1 V1): `systemctl restart`, `daemon-reload` and `reset-failed` worked from a uid-0 process with an empty capability set under the helper's sandbox (throwaway units, one device, one image, one moment, 2026-11-02).
- **Device observation** (ADR 0010 A4): `MemoryMax=` is not enforced on the reference image (`cgroup_disable=memory`); `TasksMax=` works.
- **Device observation** (ADR 0010 A6): the journal is volatile on Raspberry Pi OS Lite; it does not survive a reboot.
- **Device observation** (ADR 0010 A14, D-C81): an ordering cycle at a cold boot deleted the helper socket job until `DefaultDependencies=no` and `After=local-fs.target` were added to the recover unit. CI now verifies the boot transaction of `multi-user.target` (`scripts/verify-units.sh`).
- **Device observation** (`docs/UPDATE.md`, Phase 2): helper update process peak `VmHWM` 9,872 kB, `stage` requests 5,572 kB. ADR 0010, "Device observations at the G6.3 owner stop": `systemd-analyze security` 1.7 OK for the helper units and `oarfish.service`; the helper exited about 35 s after a query.

### The installer size budget

- **Fact** (`scripts/check-budgets.sh`, lines 18 and 26 to 28; `docs/DEVELOPMENT.md`, "Size budgets"): the `install.sh` next to the bundle must stay at or below 50,000 bytes (Gate G3: readable before it runs as root).
- **Fact, measured for this ADR** on the commit in the header: `install/install.sh` is 49,481 bytes; stamped with `scripts/stamp-installer.sh` it is **49,476 bytes** for an alpha version such as `0.1.0-alpha.13` and 49,467 bytes for `0.1.0`. **Headroom: 524 bytes** for an alpha release. Comment-only lines take 8,487 bytes (121 lines); the `usage` function 525 bytes.

## Decisions proposed

Each question B1 to B10 gives options, a recommendation and the rejected choices. Every recommendation is a **proposal**.

### B1. Process model

**Options.**

| Option | Shape | Verdict |
|---|---|---|
| P1 | A separate root daemon, socket-activated, idle exit, own binary, own socket and service unit | **Recommended** |
| P2 | The broker as a third subcommand of the helper binary | Rejected: ADR 0010 U2 rule 4 and 5 (own code, no shared request handling); every broker change would change the helper's bytes and therefore be a helper component update (D-C80); the helper's root attack surface grows by everything the broker does. |
| P3 | The broker inside the service binary, started as root by a second unit | Rejected: puts the HTTP server, the frontend, Argon2id and `golang.org/x/crypto` into a root process (the reason of ADR 0010 U1 for a separate helper binary). |
| P4 | An always-running root daemon | Rejected for the same reason as in ADR 0010 U1: resident memory and a permanently listening root process for an operation used rarely. |
| P5 | A polkit rule or a sudoers rule that lets the service call `systemctl` itself | Rejected: see [Rejected alternatives](#rejected-alternatives-summary). |

**Recommendation (P1).**

- One binary, `oarfish-broker` (name proposal), built from the same Go module, standard library only, with one subcommand `serve`. It does not import the helper's packages for request handling and does not import `backend/internal/server`, `auth`, `updater` or any third-party module. A test like the helper's `TestImports` pins that.
- Two units, `oarfish-broker.socket` and `oarfish-broker.service` (B8). No recover unit: the broker keeps no persistent state, so there is nothing to recover at boot.
- The broker's bytes depend only on its own code and the toolchain, never on the release version or the commit (the rule of D-C80, applied to the broker). Otherwise every release would differ in a privileged file.
- Socket `/run/oarfish-broker/broker.sock` in a root-owned directory, mode 0660 `root:oarfish` (B8).

**Where the broker's files live (the consequence for U15).** The broker runs as root, so its binary and units are privileged files that a release delivers. ADR 0010 U15 rule 2 says the five compared files are "the only files a release is allowed to change that run with more than the service's privileges". Any placement therefore changes an accepted rule.

| Layout | Shape | For | Against |
|---|---|---|---|
| L1 | The broker's three files join the helper's component set (`components/<id>/`, `active`, `known-good`); compared files grow from five to eight | Root code changes only through the component transaction: second confirmation, password, lint of the unit texts against accepted forms (D3), `known-good` kept, rollback. A release rollback never swaps root code silently. | The helper's closed lists (U2), the reference hash of E2 (over four files today), the lint forms and the bundle shape change: an ADR 0010 amendment and a change of the helper, the privileged code that G6 closed. Specification 8.6 says the helper replaces only "die eigene" files. |
| L2 | The broker's files travel in the release tree (`current/`), linked from `/usr/libexec` and `/etc/systemd/system` like `oarfish.service` | Service and broker of one release always belong together, so no protocol range is needed between them. | Every release switch and every automatic rollback (U6) changes root code; the health check of U6 does not cover the broker. The broker units would need a lint like the helper units, and a difference would need the second confirmation without a component phase (D6). A broken release could carry a broken root unit. |
| L3 | Installed and replaced by the installer only | No change of the helper. | Contradicts D-C69 item 2 (everything after the installation runs in the interface) and E1 (a helper with the component transaction never answers `installer_required`). A release that changes the broker would leave devices with a mismatched broker. |

**Recommendation: L1**, with the open owner decision **D-DRAFT-G7-02**. Reason: the component transaction is the only path in this project that already treats a change of root code with two confirmations, a lint and a kept known-good copy; L2 would let an ordinary update or rollback change root code without them.

**Consequences, whatever layout the owner chooses.**

1. **One installer run per device is unavoidable for the first broker release.** Every installed helper accepts only the two bundle shapes of `relverify.layout` (Fact) and refuses an extra file; and no helper can create the links in `/usr/libexec` and `/etc/systemd/system` or the wants link (Fact, U15 rule 8, A2). A GUI update therefore cannot introduce the broker. This is the same kind of bootstrap as ADR 0010 Q1. Release notes must say so; the update page shows the installer instruction (A17 F1) for the case.
2. **The installer must learn the broker**: the bundle check, the unit links, the relative wants link of the socket (A5), the start of the socket, the uninstall, and the tests in `scripts/test-installer.sh`. `scripts/check-units.sh` and `scripts/verify-units.sh` must cover the two broker units, including the boot transaction (A14).
3. **The installer cannot carry it today.** The headroom is 524 bytes (measured above). The helper's socket alone is named about twenty times in the installer (Fact, line list above); the same handling for a broker socket, service and binary is *estimated* at 1,500 to 3,000 bytes. The smallest options, **not decided here** (open decision **D-DRAFT-G7-16**): (I1) raise the budget with a decision that restates the readability goal of G3; (I2) move explanatory comments (8,487 bytes) into `docs/INSTALL.md`, at a cost to reading the script before running it as root; (I3) make the per-unit code data-driven over one list of units, a refactor of a root script that its 126 tests would have to carry; (I4) move `--uninstall` and `--purge` into a second script in the bundle. D-D10 needs installer bytes too (D-C70: an installer option for LAN access and the fingerprint output), so the owner should decide the budget once for both.
4. **Memory on the reference device.** Idle: no broker process (socket activation). Running: the broker plus one `systemctl` child, *estimate* below 12 MB together for the duration of one operation; for comparison the helper's update process peaked at 9,872 kB (device observation). The service's read path (B2) starts one `systemctl` child per list read, *estimate* a few MB for under a second. Service idle 8,480 kB (device observation) plus these stays far below the 80 MB set budget (ORB-NFR-002), *estimate*. `MemoryMax=` does not protect on the reference image (A4), so the broker sets a soft Go memory limit itself and bounds every output it reads.
5. **Release size.** One more static binary, *estimate* 2 to 3 MB uncompressed; the bundle budget is 35 MB (ORB-NFR-005).

### B2. The MVP allowlist

**The read path first.** The specification asks to prefer an unprivileged read. Reading the list and the state of services needs no root on a standard systemd (Assumption A-01: unprivileged clients may read unit properties). Options:

| Option | How the service reads | Verdict |
|---|---|---|
| R1 | The **service** runs `/usr/bin/systemctl` with a closed set of read-only argument shapes (below), no shell, cleared environment, output bounded and parsed as untrusted | **Recommended**, subject to the device spike (A-02: works under the service sandbox) |
| R2 | The service speaks D-Bus to systemd itself | Not for G7: a hand-written D-Bus client in the web process, or a new dependency (`docs/DEVELOPMENT.md` requires a reason and an owner decision). Kept as a later option if starting a program is not wanted. |
| R3 | The **broker** serves reads as root (`service.list`, `service.show`) | Fallback only, if R1 fails in the spike: root for a read, and a page refresh would start a root process. |
| R4 | Files only: cgroup directories, unit files, wants links | Rejected: misses `failed`, transient units, aliases, generators and masks, and re-implements systemd's resolution. |

R1 is the first program the service starts (ADR 0008 decision 1 and D-C19 say detection and metrics run no program; `docs/AGENT_RULES.md` 4.7 prefers files). That is a deliberate exception, named in **D-DRAFT-G7-03**.

**Read shapes (R1), closed list, proposal:**

| Shape | Purpose |
|---|---|
| `systemctl list-units --type=service --all --no-legend --no-pager --plain` | Name, load state, active state, sub state, description |
| `systemctl list-unit-files --type=service --no-legend --no-pager` | Start mode (`enabled`, `disabled`, `static`, `masked`, `indirect`, `generated`, `transient`, `alias`) |

A JSON output (`--output=json`) is used instead if the spike shows that both systemd versions in scope support it (A-03). Bounds: at most 256 KiB of output per call, at most 1,000 units, 5 s per call, one list read at a time, the result cached for 5 s device-wide. Each field is validated (the unit-name rule of B3, the state words against closed sets); a description is plain text, control characters removed, at most 200 characters, shown as text only. No background polling: the Activity page reads on open, on the button "Aktualisieren" and after each action.

**The operations.** The broker's request list is closed and compiled in. Each request is one JSON line: `{"v":1,"op":"…","unit":"…","correlation_id":"…"}`; `unit` only where the table says so. There is no other argument.

| `op` | Argument | Preconditions (checked by the broker, in order) | Effect (fixed `systemctl` shape) | Verification afterwards (ORB-SVC-002) |
|---|---|---|---|---|
| `status` | none | peer | Read-only: protocol range, broker build id, whether a power operation was accepted in this boot, busy flag | — |
| `service.start` | `unit` | peer, policy (B7), rate, not busy, unit rules of B3 (re-read by the broker itself with `show`), not `active` | `systemctl start --no-ask-password --job-mode=fail -- <unit>` | `show` again: `ActiveState=active`, else `state_unexpected` with the observed state |
| `service.stop` | `unit` | as above, plus: not on the restart-only list (B3), not `inactive` | `systemctl stop --no-ask-password --job-mode=fail -- <unit>` | `ActiveState` is `inactive` or `failed` |
| `service.restart` | `unit` | as above | `systemctl restart --no-ask-password --job-mode=fail -- <unit>` | `ActiveState=active` and a new `ExecMainStartTimestampMonotonic` (or `MainPID` changed) |
| `system.reboot` | none | peer, policy, not busy, no update transaction (B6), not already requested in this boot | `systemctl reboot --no-ask-password` | The answer is `accepted` once systemd queued the job; the evidence is the next boot (B6) |
| `system.poweroff` | none | as above | `systemctl poweroff --no-ask-password` | as above |

The `show` shape for the checks: `systemctl show --no-pager --property=Id,Names,LoadState,ActiveState,SubState,Result,Type,Transient,RefuseManualStart,RefuseManualStop,UnitFileState,ExecMainStartTimestampMonotonic,MainPID -- <unit>`. The `--` ends the options, so a unit name can never become an option (and B3 forbids a leading `-` anyway). Each mutating call has a deadline of 100 s (systemd's usual start timeout of 90 s plus a margin, A-04); at the deadline the broker ends its `systemctl` child and answers `timeout` with the state it reads then. The job may continue inside systemd; the page says so.

**Typed errors and user texts** (closed set; German in the Sie-form as in A17, English beside it; final wording may be improved at the UI step, the meaning not):

| Code | Source | Deutsch | English |
|---|---|---|---|
| `bad_request` | broker | Die Aktion ist fehlgeschlagen. Laden Sie die Seite neu und versuchen Sie es erneut. | The action failed. Reload the page and try again. |
| `busy` | broker | Eine andere Aktion läuft noch. Bitte warten Sie. | Another action is still running. Please wait. |
| `rate_limited` | broker | Zu viele Aktionen in kurzer Zeit. Bitte warten Sie {n} Sekunden. | Too many actions in a short time. Please wait {n} seconds. |
| `unit_invalid` | broker | Der Dienstname ist ungültig. | The service name is not valid. |
| `unit_unknown` | broker | Das System kennt diesen Dienst nicht mehr. Laden Sie die Liste neu. | The system no longer knows this service. Reload the list. |
| `unit_not_canonical` | broker | Dieser Name ist ein weiterer Name eines Dienstes. Wählen Sie den Dienst in der Liste. | This name is another name of a service. Choose the service in the list. |
| `unit_denied` | broker | Diesen Dienst verwaltet Oarfish nicht. Er wird für Oarfish selbst oder für den Systemstart gebraucht. | Oarfish does not manage this service. It is needed by Oarfish itself or by the system start. |
| `unit_masked` | broker | Dieser Dienst ist gesperrt (maskiert) und lässt sich nicht starten. | This service is masked and cannot be started. |
| `unit_transient` | broker | Dieser Dienst wurde nur vorübergehend angelegt. Oarfish verwaltet ihn nicht. | This service was created only temporarily. Oarfish does not manage it. |
| `unit_oneshot` | broker | Dieser Dienst führt eine einmalige Aufgabe aus. Oarfish startet solche Aufgaben nicht. | This service runs a one-time task. Oarfish does not start such tasks. |
| `unit_template` | broker | Diese Vorlage lässt sich nicht direkt starten. | This template cannot be started directly. |
| `refused_by_unit` | broker | Der Dienst lässt diese Aktion nicht zu. | The service does not allow this action. |
| `operation_not_offered` | broker | Diesen Dienst können Sie nur neu starten. Ein Stopp würde den Zugang zum Gerät trennen. | You can only restart this service. Stopping it would cut the access to the device. |
| `already_in_state` | broker | Der Dienst ist bereits in diesem Zustand. | The service is already in this state. |
| `job_failed` | broker | Die Aktion ist fehlgeschlagen. Zustand jetzt: {state}. | The action failed. State now: {state}. |
| `timeout` | broker | Die Aktion dauert zu lange. Das System führt sie vielleicht noch aus. Laden Sie die Liste gleich neu. | The action takes too long. The system may still be running it. Reload the list in a moment. |
| `state_unexpected` | broker | Die Aktion wurde ausgeführt, aber der Dienst hat nicht den erwarteten Zustand. Zustand jetzt: {state}. | The action ran, but the service is not in the expected state. State now: {state}. |
| `update_running` | broker | Gerade läuft ein Oarfish-Update. Neustart und Herunterfahren sind erst danach möglich. | An Oarfish update is running. Restart and shutdown are possible only after it. |
| `already_requested` | broker | Neustart oder Herunterfahren wurde bereits angefordert. | A restart or shutdown was already requested. |
| `internal` | broker | Die Aktion ist fehlgeschlagen. Versuchen Sie es erneut. | The action failed. Try again. |
| `unavailable` | service | Der Privilege Broker ist nicht erreichbar. Die Liste bleibt sichtbar, Aktionen sind nicht möglich. | The privilege broker cannot be reached. The list stays visible; actions are not possible. |
| `wrong_password`, `rate_limited` (password) | service | the existing texts of the account and update pages | the existing texts |

`{state}` is one of a closed set of state words, translated (for example `failed` = "fehlgeschlagen"), never a raw string from systemd. An error never echoes the request, a path or an operating-system error text.

**Explicitly not in the allowlist (G7):** no generic command and no `exec`; no argument that is a path, a file, a program, an environment variable, a property or a `systemctl` verb; no `enable`, `disable`, `mask`, `unmask`, `edit`, `set-property`, `daemon-reload`, `reload`, `kill`, `reset-failed`, `isolate`, `link`, `revert` or `preset`; no write of a unit file or drop-in; no unit type other than `service`; no new instance of a template; no `halt`, `kexec`, `soft-reboot`, `rescue`, `emergency` or reboot into firmware; no journal read, no process list, no signal to a process; no Tailscale, container, package or file operation. Each of these needs a later amendment of this ADR.

### B3. Validating a unit name

The broker trusts nothing the service sends and nothing systemd prints. Proposal, in the order the broker applies it:

1. **Syntax.** ASCII only; 1 to 255 bytes; matches `^[A-Za-z0-9][A-Za-z0-9:_.-]*(@[A-Za-z0-9:_.\\-]+)?\.service$`, where a backslash may appear only as `\x` followed by two lowercase hexadecimal digits (systemd's escaping). No `/`, no `..`, no space, no leading `-`. Else `unit_invalid`.
2. **Type.** Only the suffix `.service`. A template without an instance (`name@.service`) is `unit_template`.
3. **Known to systemd, by the broker's own read.** The broker runs the `show` shape of B2 for the name. `LoadState` must be `loaded` (else `unit_unknown`) or `masked` (B3.6).
4. **Canonical name.** The requested name must equal the `Id` that systemd reports. An alias (`Names` contains the request, `Id` differs) is `unit_not_canonical`: the page lists only canonical names, and the deny and critical lists are checked against `Id` **and every entry of `Names`**, so an alias cannot reach a denied unit.
5. **Template instances.** An instance (`name@inst.service`) is accepted only when systemd already has it loaded (rule 3); the broker never creates a new instance, because the instance string is an argument to a root unit chosen by the caller.
6. **Masked.** `LoadState=masked`: `service.start` and `service.restart` are refused with `unit_masked`; `service.stop` is allowed (it only ends something that runs).
7. **Transient.** `Transient=yes` (units made at run time, for example by `systemd-run`): refused for every operation, `unit_transient`. They are shown in the list, marked.
8. **One-shot tasks.** `Type=oneshot`: `service.start` and `service.restart` are refused with `unit_oneshot`; `service.stop` is allowed. Reason: starting a one-shot service runs a task, not a daemon; on Raspberry Pi OS such tasks include first-boot and key-generation jobs (Assumption A-05), which is exactly the kind of privileged action a service page must not offer by a click. This is a narrowing of ORB-SVC-002 and is an owner decision (**D-DRAFT-G7-05**).
9. **The unit's own refusal.** `RefuseManualStart=yes` or `RefuseManualStop=yes`: `refused_by_unit` (systemd would refuse too).
10. **Deny list** (compiled in, refused for every operation, `unit_denied`; the page shows these units without buttons):
    - Oarfish's own units: `oarfish.service`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, `oarfish-broker.service`. The helper's units belong to the helper's lifecycle (ADR 0010 U2); restarting the service from itself, or the broker from itself, has no defined result.
    - Every unit whose `Id` or alias begins with `systemd-` (among them `systemd-reboot.service`, `systemd-poweroff.service`, `systemd-halt.service`, `systemd-kexec.service`, `systemd-soft-reboot.service`, which would be a second way to reboot or power off past the checks of B6, and `systemd-journald`, `systemd-logind`, `systemd-udevd`).
    - `dbus.service`, `dbus-broker.service`, `polkit.service`; `emergency.service`, `rescue.service`, `debug-shell.service`; `user@*.service`, `user-runtime-dir@*.service`; `getty@*.service`, `serial-getty@*.service`, `autovt@*.service`, `console-getty.service`, `container-getty@*.service`.
    The list is closed and fail-closed only for these names; any other loaded service is allowed unless rules 1 to 9 refuse it. A list of allowed services instead (allowlist) is listed as an alternative in D-DRAFT-G7-05.
11. **Critical list** (ORB-SVC-003; allowed, with an additional warning in the dialog, and **restart only** for the units marked "restart only", **D-DRAFT-G7-06**):

    | Kind | Units (`Id` or alias) | Stop offered |
    |---|---|---|
    | Network | `NetworkManager.service`, `networking.service`, `dhcpcd.service`, `wpa_supplicant.service`, `wpa_supplicant@*.service`, `ModemManager.service`, `avahi-daemon.service` | No (restart only), except `avahi-daemon` and `ModemManager` |
    | SSH | `ssh.service`, `sshd.service` | No (restart only) |
    | Remote access | `tailscaled.service` | Yes, with warning |
    | Container engines | `docker.service`, `containerd.service`, `podman.service` | Yes, with warning |
    | Time | `chrony.service`, `ntp.service`, `ntpsec.service` | Yes, with warning |

    Reason for "restart only": until D-D10 is implemented, a person reaches Oarfish only through an SSH tunnel (D-C48). Stopping SSH or the only network path makes the device unreachable until a power cycle, and specification 5.3 allows a network change only with a traceable way back. A restart returns to the configured state by itself. A person with a shell can still stop such a unit.

### B4. Authentication and authorization chain

- **Person.** The service authenticates the person with the session of ADR 0009 (E2, E9). Every new route is behind a session and every state-changing one behind the three CSRF layers of ADR 0009 E3 (Origin or `Sec-Fetch-Site`, `X-Oarfish-CSRF`, JSON only), body at most 1 KiB, correlation ID by the server. Routes (proposal): `GET /api/v1/services`, `POST /api/v1/services/action` (`{"unit": …, "action": "start"|"stop"|"restart"}`), `GET /api/v1/power` (waiting actions and sessions, B6), `POST /api/v1/power` (`{"action": "reboot"|"poweroff", "current_password": …}`). No route takes a path, a command or a property.
- **Caller.** The broker authenticates the caller: socket mode 0660 `root:oarfish` and `SO_PEERCRED` uid equal to the uid of `oarfish`, looked up once at start; any other uid is closed without an answer (as the helper, ADR 0010 U2). No PID check.
- **Second confirmation by password.** Proposal (**D-DRAFT-G7-07**): `system.reboot` and `system.poweroff` require the current password, checked by `ConfirmPassword` through the shared limiter (Fact: `manager.go`), before the broker is called. Reason: a shutdown makes the reference device unreachable until a person removes and restores the power (B6), and a reboot ends every session and interrupts every service; a stolen or left-open session should not be enough (the same reasoning as ADR 0010 Q3). Start, stop and restart of a service need one confirmation click with the description of the effect and no password (ADR 0010 U9 already judged "one click without a password" adequate for a service restart). There is no two-stage flow as in A17 G1, because a power operation has one step only. Cost: one Argon2id hash (442.9 ms on the reference device, device observation, ADR 0009 A5).
- **Rate limits.** Service: one service action at a time device-wide (`busy`). Broker (the boundary): one mutating operation at a time; at most 10 mutating service operations per 60 s (`rate_limited` with the seconds to wait); one accepted power operation per boot (`already_requested`). The broker stays running while its window holds entries and exits 30 s after it is empty (B8), so the window survives the idle exit; a crash resets it (accepted, residual risk below).
- **What a compromised service can do (worst case of the allowlist).** The password and the confirmation are checked only in the service, as for the helper (ADR 0010 T1). A process that runs as `oarfish` can, without a click and without the password: start, stop or restart any loaded, non-transient, non-denied service that is not a one-shot task (stop only where allowed by B3.11), up to 10 times per minute; reboot or power off the device once per boot. Power-off is the worst: the reference device then needs a person at the power supply. It cannot: run a command, choose a program or a path, write a file or a unit, create a template instance, touch Oarfish's or the helper's units, enable, disable or mask a unit, read the journal, or reboot during an update transaction (B6, enforced in the broker). This residual risk is accepted by the design only if the owner accepts it (**D-DRAFT-G7-08**).

### B5. Audit

- **Writer: the service only.** The audit file belongs to the service (ADR 0009 E7) and a second writer would break its rotation and ownership (the reason of ADR 0010 U8). The broker writes one journal line per request, `op=<op> unit=<name|-> cid=<id> result=<code>`, which does not depend on the service; the journal is volatile on the reference image (A6), so the persistent record is the audit entry.
- **Fields** (ORB-SEC-006): time, user (the account name), plugin (empty in the core; Fact: the field exists and is always empty), action, target, result, correlation ID. Proposed additions to the closed sets (an amendment A7 of ADR 0009, as A6 was for the update):
  - actions: `service_start`, `service_stop`, `service_restart`, `system_reboot`, `system_poweroff`; none aggregated;
  - targets: the pattern `service/<unit>` (the syntax rule of B3, at most 255 bytes after the prefix) and the fixed target `system`;
  - results: `success` and `wrong_password`, `rate_limited`, `error`, `unavailable` (existing), plus the broker codes of B2 except `bad_request` and `internal`, which are recorded as `error`.
- **When.** A service action: one entry when the broker has answered (the operation is synchronous). A refusal by the service (password, CSRF) is audited as today. A power operation: one entry when the broker answered `accepted`, followed at once by a flush of the audit buffer **and an `fsync` of the audit file** (new; today the data is not `fsync`ed, Fact), because the stop of the service follows within seconds. The SIGTERM path still flushes again (Fact: `main.go`). After the boot nothing is added: the new boot is the evidence (B9).
- **Redaction** (ORB-SEC-007). The broker never sees the password; no request body, header or cookie is logged; error texts of `systemctl` are never logged or returned, only codes. Unit names are not secrets; they may contain an instance string that names a device or a configuration (for example `systemd-fsck@<escaped device>`), so they are shown and audited as they are, but bounded by the syntax rule. Descriptions are not audited.

### B6. Reboot and shutdown semantics

- **Refusal during an update transaction, enforced by the broker.** The service checks the helper's `status` first, for the message. The broker enforces it itself, because a compromised service would skip the service's check: it opens `/var/lib/oarfish-update` read-only and takes `LOCK_SH|LOCK_NB` on it (the helper holds `LOCK_EX` for a whole transaction, Fact). If the lock is held: `update_running`. If it gets the lock, it reads `phase` from `state.json` (read-only, bounded, schema-versioned); a phase other than `idle`, `staged`, `committed`, `rolled_back`, `rollback_failed` or a component phase that has ended is `update_running`, which covers the hand-over gap in which no helper holds the lock (`c_switched`, Fact). The broker holds the shared lock until `systemctl` has queued the reboot, so the helper's next `LOCK_EX|LOCK_NB` gets `busy` instead of starting a transaction that the reboot would cut. This couples the broker to the helper's lock and state format and needs an ADR 0010 amendment (**D-DRAFT-G7-10**); the broker writes nothing there. A missing directory (no helper installed) is not an update.
- **Waiting actions and connected users shown before the confirmation** (ORB-PWR-002, applied to shutdown as well): (1) an update job that is staged or waits for the component confirmation ("Ein vorbereitetes Update wird verworfen" / "A prepared update will be discarded"; Fact from ADR 0010 T19: a reboot removes staged trees); (2) a running service action; (3) the number of other Oarfish sessions (`Sessions.Len` minus one) and open live views; (4) proposal, **D-DRAFT-G7-11**: the number of login sessions on the device from the session files of systemd-logind, as a count only, never a name or an address (Assumption A-06: readable under the service sandbox). Shown as a list in the dialog; an empty list says "Keine wartenden Aktionen" / "No waiting actions".
- **Dialog texts (proposal).**

  | Key | Deutsch | English |
  |---|---|---|
  | reboot title | Gerät neu starten? | Restart the device? |
  | reboot body | Oarfish und alle Dienste werden beendet. Alle Sitzungen enden; Sie melden sich danach neu an. | Oarfish and all services stop. All sessions end; you sign in again afterwards. |
  | tunnel sentence (both) | Wenn Sie über einen SSH-Tunnel verbunden sind, wird er getrennt. Bauen Sie ihn danach neu auf. | If you are connected through an SSH tunnel, it is closed. Open it again afterwards. |
  | shutdown title | Gerät herunterfahren? | Shut down the device? |
  | shutdown body | Das Gerät schaltet sich aus und startet nicht von selbst wieder. Um es wieder einzuschalten, trennen Sie die Stromversorgung und schließen Sie sie wieder an. Bis dahin ist es nicht erreichbar. | The device switches off and does not start again by itself. To switch it on again, disconnect the power supply and connect it again. Until then it cannot be reached. |
  | during reboot | Das Gerät startet neu … | The device is restarting … |
  | after reboot | Das Gerät ist wieder erreichbar. Bitte melden Sie sich an. | The device can be reached again. Please sign in. |
  | reboot too long | Das Gerät antwortet noch nicht. Prüfen Sie die Stromversorgung und das Netzwerk. | The device does not answer yet. Check the power supply and the network. |
  | during shutdown | Das Gerät fährt herunter … | The device is shutting down … |
  | after shutdown | Das Gerät antwortet nicht mehr. Nach dem Herunterfahren ist das erwartet. | The device no longer answers. After a shutdown this is expected. |

  The shutdown sentence about the power supply is stated by the gate prompt for the reference device and is not in the repository as a device observation (A-07). Other boards may have a power button; until a hardware profile says so, the text is the same for every board (proposal).
- **The page during and after the action.** After `accepted` the page stops every other request and polls `/healthz` (no session, Fact) every 5 s. Reboot: the first failed poll switches to "startet neu"; the first answer after that shows "wieder erreichbar" with a link to the sign-in; after 5 minutes without an answer it shows "antwortet noch nicht". Shutdown: the page shows "fährt herunter" and then "antwortet nicht mehr" after the first failed poll. A polling browser cannot tell a shutdown from a lost network; the text says what is expected, not what happened.
- **Browser on the device itself, or through a tunnel.** Until D-D10, the browser is either on the device (a desktop image) or on another computer through an SSH tunnel (D-C48). In both cases the page loses its connection; the tunnel does not come back by itself (the tunnel sentence above). A browser on the device itself ends with the reboot. Nothing in the flow depends on the page being alive after `accepted`.
- **What survives.** Survives: the account, the audit file (flushed and `fsync`ed, B5), the helper's `state.json`, the configuration, every installed release. Does not survive: sessions (memory, ADR 0009 E2), the live views, the broker's rate window, the journal on the reference image (A6), a staged update (T19), the update cooldown possibly (ADR 0010 F21). After a shutdown the same, plus the clock may be wrong at the next start (no battery-backed clock, ADR 0010 A8), so audit times may be off until it is set.

### B7. The hook for G8 (capability check, interface only)

Not implemented in G7. The G7 allowlist must not make it impossible. Proposal:

- **Caller identity comes only from the socket.** The request has no field that names a caller or a plugin. In G7 the only accepted uid is that of `oarfish`; the caller is "core". In G8 a plugin process runs under another uid (ADR 0010 U2 already requires that plugin processes do not share the service's uid), and the broker maps the peer uid to a plugin id from a root-owned registry. Nothing a client sends can claim a plugin identity.
- **Each operation maps to exactly one capability**, in the vocabulary of specification 10.2: `service.start`, `service.stop` and `service.restart` of unit `u` need `service.control:<u>` (or a broader `service.control`); `system.reboot` and `system.poweroff` need `system.power` (a new name, proposal); the read shapes need `service.read`. Board capabilities enter the same check (for example a board without the power operation).
- **The check runs in the broker, before the unit lookup**, so a refused plugin learns nothing about units. In G7 it is a constant function. Interface shape (documentation only):

  ```go
  // Documentation only; not implemented in G7.
  type Caller struct {
      UID    uint32
      Plugin string // "" for the core; set only by the broker from the peer uid
  }

  type Operation string // "service.start", "service.stop", "service.restart", "system.reboot", "system.poweroff"

  type Policy interface {
      // Allow decides before any unit lookup. unit is "" for the power operations.
      Allow(c Caller, op Operation, unit string) (ok bool, code string)
  }

  // G7: corePolicy allows every Operation of the allowlist for the core caller
  // and nothing for any other caller.
  ```

- **Open for G8, not decided here:** whether plugins call the broker directly or through the core; who writes the audit entry for a plugin's direct call (B5 makes the service the only writer); whether `plugin` is passed back to the service for the audit.

### B8. Hardening, resource limits, recovery and boot order

**Unit texts (documentation of the intent, proposal):**

```ini
# oarfish-broker.socket
[Unit]
Description=Oarfish privilege broker socket
[Socket]
ListenStream=/run/oarfish-broker/broker.sock
SocketUser=root
SocketGroup=oarfish
SocketMode=0660
DirectoryMode=0755
Accept=no
Backlog=8
RemoveOnStop=yes
[Install]
WantedBy=sockets.target

# oarfish-broker.service
[Unit]
Description=Oarfish privilege broker
After=oarfish-update-recover.service
[Service]
Type=exec
ExecStart=/usr/libexec/oarfish-broker serve
Restart=no
RuntimeMaxSec=300
User=root
Group=root
UMask=0077
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
PrivateNetwork=yes
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
RestrictAddressFamilies=AF_UNIX
IPAddressDeny=any
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM
MemoryMax=32M
TasksMax=16
```

- **No writable path at all.** The broker writes no file: no `ReadWritePaths=`, no `StateDirectory=`. `/var/lib/oarfish-update` is readable under `ProtectSystem=strict`, and `flock` needs no write access (Assumption A-08).
- **Root with an empty capability set.** Talking to systemd is done by `systemctl` as uid 0. That this works for `start`, `stop`, `restart`, `reboot` and `poweroff` with an empty capability set, `PrivateNetwork=yes` and `ProtectProc=invisible` is **Assumption A-09**; G6.1 V1 observed it only for `restart`, `daemon-reload` and `reset-failed` under the helper's sandbox. If it fails, the work stops and the owner decides; the answer must not silently become `CAP_SYS_ADMIN` or `CAP_SYS_BOOT` (the rule of ADR 0010 U10).
- **`PrivateNetwork=yes` and `AF_UNIX` only.** The broker needs no network; `systemctl` reaches systemd through a socket in the file system, which works across network namespaces (Assumption A-10). This is tighter than the helper, which needs loopback for its health check.
- **`MemoryMax=32M`** stays in the text and does nothing on the reference image (A4). The program sets a soft memory limit of 16 MiB itself and bounds every output it reads (256 KiB per `systemctl` call). `TasksMax=16` works (A4) and bounds the child processes.
- **Idle exit.** The broker exits 30 s after the last connection when no operation runs and its rate window is empty; the socket stays. *Estimate:* no resident memory while idle.
- **Socket path and modes.** `/run/oarfish-broker/` is created by the socket unit, root-owned, 0755; the socket 0660 `root:oarfish`. `/run/oarfish` is not used, because the service owns it (the reason of ADR 0010 U2). A future plugin runtime must keep plugin processes away from this socket, as from the helper's (`InaccessiblePaths=`, another uid).
- **Crash or hang.** A crash ends the connection; the service answers `unavailable` and re-reads the state; the next connection starts a new broker. A hang ends at the 100 s operation deadline or at `RuntimeMaxSec=300`; systemd then kills the broker and its `systemctl` child, and a job already queued in systemd continues by itself. There is no persistent state and no recover unit. `Restart=no` because a socket-activated broker is started again by the next connection.
- **Boot order without a cycle.** The socket has the default dependencies and `WantedBy=sockets.target`, like the helper socket, and no ordering relation to the recover unit; this keeps the A14 situation out (a socket ordered after the recover unit would bring back a chain through `sockets.target` and `basic.target` if the recover unit ever gets its default dependencies again). The service has the default dependencies (so it starts after `basic.target`) and `After=oarfish-update-recover.service`, so under layout L1 it never runs a binary from a component set that the recovery is still switching. No `Requires=`, `Wants=`, `BindsTo=` or `Before=`; no relation to `oarfish.service` or the helper units. `scripts/verify-units.sh` must verify the boot transaction of `multi-user.target` with the two broker units and their wants link added, and fail on an ordering cycle (the CI check of A14). The cold-boot observation on the device is part of B9.
- **Hardening target.** `systemd-analyze security` of `oarfish-broker.service` at or below the helper's 1.7 OK (device observation for the helper, G6.3 owner stop), shown offline in CI and on the device.

### B9. Test and evidence plan

Evidence classes as in [`UPDATE.md`](../UPDATE.md#evidence-classes) and ADR 0010 U13, applied to the broker:

- **C**: automated tests in CI with a fake `systemctl` (a program that records its arguments and prints fixture output), a fake root for the helper's lock and state, a fake broker for the service, and the page tests. They prove the argument shapes, the validation, the order of checks, the error mapping and the texts, not systemd's behaviour.
- **D**: on the reference device with the production binaries of a release signed with the production key. Real services, real reboot, real shutdown.
- **T**: on the reference device with a test build of the helper (fault hooks) to hold an update transaction while a reboot is requested, and test units installed by the device kit (a throwaway service that fails to start, one that hangs, a one-shot unit, a masked unit, a transient unit made by the kit). The production broker is used; only the units around it and the held helper are test material. Class T is never presented as evidence about the production path.

Which rows each class covers is in the [failure matrix](#failure-matrix).

**Device spike first (step G7.1, no product code):** the assumptions A-01 to A-10 on the reference device, with throwaway units and a throwaway broker sandbox, as G6.1 did for the helper.

**Kit work for a real reboot and shutdown** (the device test kit is not part of this repository): (1) before the click, collect what does not survive (the broker's journal lines, A6) and the uptime; (2) the owner clicks in the GUI (the kit does not store or send the password); (3) the kit reconnects the tunnel in a loop with a bounded wait and polls `/healthz`; (4) after the boot it collects: a smaller uptime, the `system_reboot` entry in the audit file with the correlation ID of the request, no ordering cycle in the boot journal, the broker socket active; (5) shutdown: the kit waits until the owner reports the power restored, then collects the `system_poweroff` entry and the same boot checks; (6) the class T row "reboot during an update" holds the helper's transaction with a hook and expects `update_running`. No device value (host name, address, user name, serial number, boot id) is written into this repository.

### B10. Delivery split

Each step is its own session, pull request and stop point; no step starts before the previous one is accepted (as ADR 0010 U14).

| Step | Content | Stop point |
|---|---|---|
| G7.0 | This ADR, Proposed, and the G7 status line of `docs/ROADMAP.md`. Docs only. | The owner decides the draft decisions; a documentation PR records them (D-C87 and later), the needed amendments of ADR 0009 and ADR 0010, and the specification alignment. |
| G7.1 | Device spike, no product code: A-01 to A-10 with throwaway units and scripts; writes the findings into this ADR as an amendment. | Owner confirms or changes B2, B3 and B8 from the findings. |
| (decision) | The installer budget (D-DRAFT-G7-16), together with the needs of D-D10. | Owner decision before G7.3. |
| G7.2 | Broker core: binary, protocol, peer check, policy constant (B7), validation (B3), the `systemctl` shapes, rate limit, the update check of B6, the two unit files, unit checks in CI, class C tests. Not yet in the bundle. | Review of the privileged code. |
| G7.3 | Packaging: bundle shape, the component set or release tree (B1), the helper change and its lint forms if L1, `bundlegen`, the installer and its tests, `docs/INSTALL.md`, `docs/RELEASING.md`. | Review; dry-run release green; installer budget met. |
| G7.4 | Service side and Activity page: read path R1, routes, audit amendment in code, page with filter, dialogs and critical warnings, texts in `de` and `en`, accessibility. | Owner reviews the UI. |
| G7.5 | Reboot and shutdown: routes, password, waiting actions, the page during and after, audit flush and `fsync`. | Owner reviews the UI and the texts. |
| G7.6 | Release (one installer run per device, B1 consequence 1) and device evidence of the failure matrix, class D and T; documentation. (Pointer: split into G7.6a, documentation and release texts; G7.6b, the release; G7.6c, device evidence; G7.6d, the closing review; see `docs/ROADMAP.md`.) | Owner decides whether G7 passes. |

**Order relative to D-D10 and the installer budget.** G7 does not depend on D-D10: it works on loopback through a tunnel. Both need installer bytes, so the budget decision comes before G7.3 and should consider D-D10. The roadmap's working order (line 51) puts G7 before N0, N1, H1 and U2 and does not place D-D10; this ADR does not move D-D10.

## Failure matrix

**No row is run; this is the plan.** Classes as in B9.

| # | Case | How it is injected | Expected result | Class |
|---|---|---|---|---|
| BF1 | Wrong peer | Another uid connects | Closed without an answer; nothing runs | C, D |
| BF2 | Malformed request | Unknown field, unknown `op`, trailing data, more than 1 KiB, wrong `v` | `bad_request`; nothing runs | C |
| BF3 | Bad unit name | Leading `-`, `/`, `..`, space, non-ASCII, 256 bytes, `.socket`, `.timer`, backslash not in `\xHH` | `unit_invalid`; `systemctl` is not started | C |
| BF4 | Template without instance; new instance | `name@.service`; an instance that is not loaded | `unit_template`; `unit_unknown` | C, T |
| BF5 | Unknown unit | A name that is not loaded | `unit_unknown` | C, D |
| BF6 | Denied unit | Each entry of the deny list, and an alias of one | `unit_denied` | C, D |
| BF7 | Alias | An alias of an allowed unit | `unit_not_canonical` | C, T |
| BF8 | Masked unit | Start and restart; stop | `unit_masked`; stop allowed | C, T |
| BF9 | Transient unit | A unit made by `systemd-run` | `unit_transient` | C, T |
| BF10 | One-shot unit | Start, restart; stop | `unit_oneshot`; stop allowed | C, T |
| BF11 | Unit refuses manual start or stop | `RefuseManualStart=yes` | `refused_by_unit` | C, T |
| BF12 | Restart-only unit | Stop of `ssh.service` | `operation_not_offered` | C, D |
| BF13 | Start fails | A test unit whose program exits with an error | `job_failed` with the state word; audit entry | C, T |
| BF14 | Hang | A test unit that never becomes ready | `timeout` at the deadline; the state read afterwards is shown | C, T |
| BF15 | Wrong state afterwards | The fake `systemctl` reports success but `show` reports `inactive` | `state_unexpected` | C |
| BF16 | Restart success | A real non-critical service | `success`; new start timestamp; audit entry with user, action, target, result, correlation ID | C, D |
| BF17 | Busy | Two actions at once | One runs, the other `busy` | C, D |
| BF18 | Rate limit | 11 mutating requests within 60 s, directly on the socket as `oarfish` | The 11th `rate_limited`; survives an idle exit inside the window | C, D |
| BF19 | Broker absent or socket stopped | Socket unit stopped | `unavailable`; list still shown; no action button works | C, D |
| BF20 | Broker crash during an operation | Kill the broker while `systemctl` runs | `unavailable`; the systemd job continues; the page re-reads | C, T |
| BF21 | Hostile `systemctl` output | Fixture with 1 MiB, control characters, invalid UTF-8, a description with markup | Bounded, cleaned, shown as text; no crash | C |
| BF22 | Missing CSRF or Origin | Request without the header or with a foreign Origin | Refused by the existing layers; audit `csrf_failed` or `origin_rejected` | C |
| BF23 | Reboot, wrong password | Wrong password; repeated | `wrong_password`, then the limiter's `rate_limited`; nothing reaches the broker | C, D |
| BF24 | Reboot during an update transaction | Helper test build holds a transaction (hook) | `update_running`; the transaction continues | C, T |
| BF25 | Reboot in the hand-over gap | State `c_switched`, no lock held (fake root) | `update_running` | C |
| BF26 | Reboot success | GUI reboot with the password | Device restarts; `/healthz` answers again; audit entry `system_reboot` `success` present after the boot; broker socket active; no ordering cycle | D |
| BF27 | Shutdown success | GUI shutdown with the password; owner restores power | Device off; after power-on the audit entry `system_poweroff` `success` is present | D |
| BF28 | Second power request | Two power requests in one boot | The second `already_requested` | C, D |
| BF29 | Compromised service, bounded | Requests sent directly to the socket as `oarfish` without any password | Exactly the allowlist works; every denied case of B2 and B3 is refused | C, D |
| BF30 | Boot transaction | CI verify of `multi-user.target` with the broker units; cold boot on the device | No ordering cycle in CI and in the boot journal | C, D |
| BF31 | Memory and idle exit | One list read, one restart, then 30 s idle | Peak `VmHWM` of broker and service recorded; broker gone after the idle time | D |
| BF32 | Hardening | `systemd-analyze security` | At or below the helper's value; recorded | C (offline), D |

## Threat model

Assets: the root-owned control over systemd units and the power state, the availability of the device and of the person's access path, the audit trail.

| # | Attacker or fault | Threat | Mitigation in this design | Residual risk |
|---|---|---|---|---|
| TB1 | Code execution in the service (web-layer bug) | Use the broker to become root or to break the device | Closed allowlist, no path, program, verb or property argument; unit rules of B3 checked by the broker from its own read; deny list; one-shot tasks refused; no template creation; rate limit; reboot refused during an update by the broker | The password and the confirmation are checked only in the service: within the allowlist the attacker can stop or restart allowed services and reboot or power off once per boot without them (B4). Power-off needs a person at the device. Once per boot, every boot, is a reboot loop ([Risks](#risks), added by task C1). |
| TB2 | Option injection | A unit name that `systemctl` reads as an option | Syntax rule (no leading `-`), `--` before the unit, no shell | None known. |
| TB3 | Alias or instance trick | Reach a denied unit through an alias, or create an instance with a chosen string | Canonical `Id` required; deny list checked against `Id` and all `Names`; only loaded instances | A unit installed later by an administrator with a dangerous effect and a harmless name is allowed. |
| TB4 | Second path to a power operation | `service.start systemd-reboot.service` or similar | `systemd-*` denied; one-shot tasks refused | A third-party unit that reboots when started and is not a one-shot unit would pass; accepted, named. |
| TB5 | Hostile output of `systemctl` (descriptions from unit files) | Script injection, memory exhaustion | Bounded reads, closed state words, text rendering, length limit, control characters removed | Misleading descriptions are possible. |
| TB6 | Other local user | Talk to the broker | Socket mode, `SO_PEERCRED` | A future plugin under the uid `oarfish` would reach the socket (G8 must prevent it, B7, B8). |
| TB7 | Reboot during an update | Half-finished transaction | Lock and phase check in the broker (B6); the helper's crash safety (ADR 0010 U7) remains the last line | A reboot by another root process or a power cut is outside the broker. |
| TB8 | Lockout of the person | Stop of SSH or the network path | Restart only for those units (B3.11) | A restart that fails leaves the unit stopped; recovery is a reboot or a power cycle. |
| TB9 | Resource exhaustion | Many reads or actions | Cache of 5 s for reads, one action at a time, rate limit, bounded output, soft memory limit; `TasksMax=16` | `MemoryMax=` not enforced on the reference image (A4). |
| TB10 | Information leakage | Unit names or descriptions with identifying values in the audit or the repository | Descriptions not audited; no device value in the repository; codes instead of error strings | Instance strings in unit names are audited on the device as they are. |
| TB11 | Broker bug as root | Code execution as root | Empty capability set, no writable path, no network, `systemctl` only, standard library only, small code reviewed in G7.2 | A bug runs as uid 0 inside that sandbox. |
| TB12 | Signed but malicious broker in a release | Root code from a release | Under L1: the component transaction with two confirmations and the lint; the same key analysis as ADR 0010 T7 and T17 | The same as ADR 0010 T7 and T17. |

## Rejected alternatives (summary)

- **A generic `systemctl <verb> <unit>` request** or any request that carries a verb or a property: a generic executor in disguise (AGENT_RULES 5.1).
- **A polkit rule** that lets the user `oarfish` manage units itself: a privileged configuration surface outside the project's code and review, a JavaScript rule engine as the boundary, not present on every target (Assumption), and the audit would not be in the project's hands.
- **A sudoers rule or a setuid helper:** forbidden by ORB-SEC-001 ("ohne pauschale sudo-Rechte") and blocked by `NoNewPrivileges=yes` of the service.
- **Extending the update helper:** forbidden by ADR 0010 U2 ("the helper never becomes a broker").
- **The broker inside the service binary, or the broker as a subcommand of the helper:** B1, P2 and P3.
- **An always-running root daemon:** B1, P4.
- **Reads through the broker as root (R3), D-Bus in the service (R2), files only (R4):** B2.
- **Unit validation by the service's list only:** the broker must check what it runs, from its own read.
- **Allowing new template instances or transient units:** B3.
- **Stopping SSH and network units from the GUI:** B3.11 and specification 5.3.
- **No password for reboot and shutdown:** B4.
- **The broker writing the audit file:** B5.
- **The service alone refusing a reboot during an update:** a compromised service would skip it (B6).
- **`systemctl --no-block` and polling instead of a blocking call with a deadline:** more states for the same result; may be reconsidered if the spike shows that blocking calls are a problem.
- **A recover unit for the broker:** the broker keeps no state.

## Requirements and what G7.0 meets

G7.0 is a design document. **It meets no requirement by itself and claims none.** The table says which step is planned to meet each one.

| Requirement | Met by G7.0 | Planned in | How (proposal) |
|---|---|---|---|
| ORB-SEC-001 core and web process unprivileged | not met by G7.0 (already met for the service since G5.1; G7 keeps it) | G7.2 to G7.5 | The service gets no privilege; only a second socket connection and, with R1, read-only `systemctl` calls. |
| ORB-SEC-002 narrow broker with typed arguments | no | G7.2 | B1, B2, B3. |
| ORB-SEC-003 broker checks capabilities | no; not G7 | G8 | B7 keeps it possible. |
| ORB-SEC-004 CSRF and origin | no | G7.4, G7.5 | Existing layers of ADR 0009 E3 on the new routes. |
| ORB-SEC-006 audit fields | no | G7.4, G7.5 | B5, ADR 0009 amendment A7. |
| ORB-SEC-007 no secrets in audit and logs | no | G7.2 to G7.5 | B5; redaction tests. |
| ORB-SVC-001 list with filter | no | G7.4 | R1 and the Activity page. |
| ORB-SVC-002 start, stop, restart with effect description, confirmation, verification | no | G7.2, G7.4 | B2 (verification by re-reading the state), B3, B4. |
| ORB-SVC-003 critical services marked | no | G7.4 | B3.11. |
| ORB-PWR-001 reboot and shutdown | no | G7.2, G7.5 | B2, B4, B6. |
| ORB-PWR-002 waiting actions and connected users | no | G7.5 | B6. |
| ORB-LOG-002 masking in displays of system files | no | G7.4 | See conflict K6. |
| ORB-NFR-001, -002, -009 | no | G7.6 | Measured, BF31; no persistent writes except one audit entry per action. |
| AT-009 service action complete | no | G7.6 | BF16. |
| Specification 16, item 12 (services, reboot, shutdown on real hardware) | no | G7.6 | BF16, BF26, BF27. |

## Not decided here

- The journal view and the process view (ORB-LOG-001, ORB-PRC-001): reserve behind the MVP acceptance (D-C69 item 5). The broker gets no journal or process privilege in G7.
- The capability check (ORB-SEC-003, G8), plugin identities, and who audits a plugin's direct broker call (B7).
- Tailscale operations (`tailscale.login`, the installation through the GUI: own design stop, D-C75) and container operations (G10, D-C75).
- Files (design stop F1, after G7).
- LAN access and HTTPS (D-D10) and key rotation (D-D12). This ADR does not touch either.
- Reload of a service, enable and disable, masking, OS updates (ORB-OSU), and unit file editing.
- Whether other boards get a different shutdown text (needs a hardware profile field).
- The installer budget (D-DRAFT-G7-16 lists options; the owner decides).

## Conflicts and gaps

Each item names the sources, the affected requirement and the smallest formal correction. **None is resolved by this ADR.**

| # | Conflict or gap | Sources | Smallest formal correction (proposal) |
|---|---|---|---|
| K1 | The broker's files are root code that a release delivers, but the helper may replace only "die eigene Binärdatei und die zugehörigen Unit-Dateien" and U15 rule 2 calls the five compared files the only privileged files of a release. | Specification 8.6; ADR 0010 U2 (lists), U15 rule 2; ORB-UPD-007 | With L1: a decision (D-C87 or later), an ADR 0010 amendment (A19) that adds the broker files to the component set, the compared files and the lint forms, and one more bullet in specification 8.6 ("sowie Binärdatei und Units des Privilege Broker"). With L2: the same amendment for the release tree and the lint. |
| K2 | The first broker release needs one installer run on every device; D-C69 item 2 says everything after the installation runs in the interface. | D-C69 item 2; `relverify.layout`; ADR 0010 U15 rule 8, A2 | A decision that accepts a second bootstrap installer run, as ADR 0010 Q1 did for the helper, and says it in the release notes. |
| K3 | The installer has 524 bytes of headroom (measured); the broker needs an *estimated* 1,500 to 3,000; D-D10 needs bytes too. | `scripts/check-budgets.sh`; `docs/DEVELOPMENT.md`; D-C70 | Owner decision D-DRAFT-G7-16 before G7.3; if the budget changes, a decision and the line in `docs/DEVELOPMENT.md` and `scripts/check-budgets.sh`. |
| K4 | ORB-SVC-002 requires start, stop and restart (MUSS); D-C69 item 5 and specification 16 item 12 name only display and restart. | Specification 6.4, 16; D-C69 | None needed for the plan: the requirement is the higher source and G7 plans all three. If the owner wants restart only, the requirement must change by a decision. |
| K5 | Stopping a network or SSH unit is a network change without a way back (specification 5.3), but ORB-SVC-002 asks for stop of services in general. | Specification 5.3, 6.4 | Decision D-DRAFT-G7-06 (restart only for those units) and a sentence in the acceptance criterion of ORB-SVC-002 or ORB-SVC-003. |
| K6 | ORB-LOG-002 applies masking to "jede Anzeige von Logs und Systemdateien"; unit descriptions come from unit files, and no masking function exists yet (the journal is reserve). | Specification 6.4; D-C69 item 5 | Decision D-DRAFT-G7-17: either descriptions count as system-file content and G7.4 brings a minimal masker, or the scope of ORB-LOG-002 is stated as logs and file contents, not unit names and descriptions. |
| K7 | Running `systemctl` from the service (R1) is the first program the service starts; ADR 0008 decision 1 and D-C19 say detection and metrics run no program; AGENT_RULES 4.7 prefers files. | ADR 0008; D-C19; AGENT_RULES 4.7 | No accepted rule forbids it for services; a decision (D-DRAFT-G7-03) that records the exception and its bounds. |
| K8 | The specification's G7 exit criterion is "Sicherer Dienstneustart End-to-End"; reboot and shutdown on real hardware are required by specification 16 item 12 but not by the gate. | Specification 14.2, 16 | Decision D-DRAFT-G7-15 and an extension of the G7 exit criterion in specification 14.2 (with a change-log line). |
| K9 | The broker reads the helper's lock and state (B6); ADR 0010 U8 says the service reads the state only through `status` and says nothing about another reader. | ADR 0010 U7, U8 | An ADR 0010 amendment that names the broker as a read-only reader of the lock and of `phase`, with the state format as an interface (D-DRAFT-G7-10). |
| K10 | The audit sets are closed; the new actions, targets and results need an amendment. `Event` has no plugin member, so ORB-SEC-006's "Plugin" is always empty until G8. | ADR 0009 E7, A6; `audit.go` | ADR 0009 amendment A7 (as A6 was for the update). |
| K11 | "Verbundene Benutzer" in ORB-PWR-002 is not defined (Oarfish sessions, or login sessions on the device). | Specification 6.3 | Decision D-DRAFT-G7-11. |
| K12 | Specification 8.2 lists no broker paths (`/usr/libexec/oarfish-broker`, `/run/oarfish-broker/`, the unit links). | Specification 8.2 | Rows added when G7.3 changes the layout, with a decision and a change-log line. |
| K13 | ORB-PWR-002 names only the restart; the dialog of a shutdown has the same need. | Specification 6.3 | None needed; the design applies it to both (proposal). |

## Amendments this ADR would need

Listed as proposals; none is made by this ADR.

1. **ADR 0010, amendment A19** (if L1): the component set, the compared files, the lists of U2, the lint forms (D3) for the two broker units, the reference hash of E2 over the larger set, the bundle shape; the broker as a read-only reader of the lock and of `phase` (K9). If L2: the release-tree variant of the same.
2. **ADR 0009, amendment A7:** the audit actions, targets and results of B5, and the `fsync` of the audit file before a power operation.
3. **Specification** (after the owner's decisions, with change-log lines): 8.6 (K1), 8.2 (K12), 14.2 G7 exit criterion (K8), and possibly ORB-SVC-002 or -003 (K5) and ORB-LOG-002 (K6).
4. **`docs/DEVELOPMENT.md` and `scripts/check-budgets.sh`** only if the owner changes the installer budget (K3).

## Risks

- **Schedule.** G7 now includes a helper change (L1), an installer change against a full budget, and a device spike. D-C69 item 7 already names the MVP as larger than before.
- **Root code grows.** A second root program exists; it is small, standard library only and has no writable path, but it is root (TB11).
- **The compromised-service case** allows power-off without the password (TB1). Only the owner can accept that.
- **Reboot loop of a compromised service** (task C1, finding F-12 of the interim assessment; next to TB1). The limit is one accepted power operation per boot, but the service starts again at every boot, so a compromised service can reboot the device again after each start, without the password. The device then stays unreachable for the person, every boot writes to the SD card, and each reboot voids the update cooldown ([ADR 0010](0010-update-helper.md) U6, corrected by task C1). Not mitigated today; the design block of D-C101 decides it (for example a root-owned record of power operations with a minimum interval across boots, or the person's proof checked at the privileged side, D-C101 item 2).
- **systemd behaviour** behind A-01 to A-10 is not observed. If `systemctl` needs a capability for some verb, the design stops (B8).
- **A third-party unit with a dangerous start** that is not a one-shot unit passes the rules (TB4).
- **Two systemd versions** (257 on the reference image, an older one on Ubuntu Server LTS) may print or behave differently; the read shapes are parsed strictly and a mismatch fails closed.
- **The installer budget** may force a change of the G3 readability rule.

## Draft decisions for the owner

Each item is a proposal with a recommendation. Accepted items become `D-C87` and later; `D-C82` stays reserved.

| # | Subject | Recommendation |
|---|---|---|
| D-DRAFT-G7-01 | Process model | A separate root daemon `oarfish-broker`, socket-activated with idle exit, own binary (standard library only, bytes independent of the release version), own socket and service unit, no recover unit (B1, P1). |
| D-DRAFT-G7-02 | Where the broker's files live | L1, the helper's component set, with an ADR 0010 amendment and specification 8.6 (K1). Alternative L2 (release tree). |
| D-DRAFT-G7-03 | Read path | R1: the service runs `systemctl` with two closed read-only shapes; R3 (broker reads) only if the spike shows R1 does not work under the service sandbox (K7). |
| D-DRAFT-G7-04 | Allowlist | Exactly `status`, `service.start`, `service.stop`, `service.restart`, `system.reboot`, `system.poweroff`, with the shapes, checks and verification of B2, and the explicit exclusions of B2. |
| D-DRAFT-G7-05 | Unit rules | B3: syntax, `.service` only, broker's own read, canonical `Id`, loaded instances only, masked, transient refused, one-shot start refused, the unit's own refusal, the deny list. Alternative: a closed allowlist of manageable services instead of a deny list (safer, but every service the person installs later would need a release). |
| D-DRAFT-G7-06 | Critical units | Warnings for the kinds of B3.11; restart only for SSH and the network path (K5). |
| D-DRAFT-G7-07 | Confirmation | Password for reboot and shutdown; one confirmation click for service actions (B4). |
| D-DRAFT-G7-08 | Residual risk of a compromised service | Accept TB1 as stated, with the broker's rate limit and one power operation per boot. |
| D-DRAFT-G7-09 | Audit | The service writes; ADR 0009 amendment A7; flush and `fsync` before a power operation; the broker writes journal lines only (B5). |
| D-DRAFT-G7-10 | Reboot during an update | Refused by the broker through the helper's lock and `phase`, with an ADR 0010 amendment (K9); also checked in the service for the message. |
| D-DRAFT-G7-11 | Connected users | Other Oarfish sessions and live views; plus a count of login sessions on the device if the spike shows it is readable without names. |
| D-DRAFT-G7-12 | G8 hook | Caller identity from `SO_PEERCRED` only; one capability per operation in the vocabulary of specification 10.2 (`system.power` new); check before the unit lookup; a constant policy in G7 (B7). |
| D-DRAFT-G7-13 | Hardening | The unit texts of B8: no writable path, `PrivateNetwork=yes`, `AF_UNIX` only, empty capability set, soft memory limit in the program, idle exit 30 s, `RuntimeMaxSec=300`, `After=oarfish-update-recover.service` for the service only. |
| D-DRAFT-G7-14 | Delivery | The steps G7.1 to G7.6 of B10, starting with a device spike; one session, one pull request and one stop point each. |
| D-DRAFT-G7-15 | G7 exit | G7 passes only with BF16, BF26 and BF27 as class D on the reference device; specification 14.2 is aligned (K8). |
| D-DRAFT-G7-16 | Installer budget | Not decided here. Options I1 to I4 of B1; the owner decides once for G7 and D-D10, before G7.3 (K3). |
| D-DRAFT-G7-17 | Masking of unit descriptions | Not decided here: ORB-LOG-002 scope (K6). Recommendation: state that descriptions are shown as plain bounded text and are not "system file content", so that no masker is built before the journal view. |
| D-DRAFT-G7-18 | Bootstrap installer run | Accept one installer run per device for the first broker release, said in the release notes (K2). |

## Assumptions to verify

| # | Assumption | Used in | Checked by |
|---|---|---|---|
| A-01 | An unprivileged process may read unit lists and properties from systemd. | B2 | G7.1 spike |
| A-02 | `systemctl` with the read shapes of B2 works as `oarfish` under the unchanged service sandbox (`ProtectSystem=strict`, `@system-service`, `AF_UNIX` allowed). | B2, R1 | G7.1 spike |
| A-03 | `--output=json` for `list-units` and `list-unit-files` exists in systemd 257 and in the systemd of Ubuntu Server LTS. | B2 | G7.1 spike (Raspberry Pi OS part); Ubuntu later (G12) |
| A-04 | The default start timeout of services is 90 s, so a 100 s deadline covers a normal job. | B2 | G7.1 spike |
| A-05 | Raspberry Pi OS ships one-shot units for first-boot and key-generation tasks. | B3 rule 8 | G7.1 spike (read the unit list) |
| A-06 | The count of login sessions can be read from the session files of systemd-logind under the service sandbox without reading names or addresses. | B6 | G7.1 spike |
| A-07 | The reference device does not switch itself on after `poweroff`; restoring the power starts it. | B6 | Stated by the gate prompt; observed in G7.6 (BF27) |
| A-08 | `flock` with `LOCK_SH` works on a directory under `ProtectSystem=strict`, read-only, and conflicts with the helper's `LOCK_EX`. | B6, B8 | G7.1 spike and class C test |
| A-09 | `systemctl start`, `stop`, `restart`, `reboot` and `poweroff` work from uid 0 with an empty capability set, `PrivateNetwork=yes` and `ProtectProc=invisible`. | B8 | G7.1 spike; if not, stop and ask the owner |
| A-10 | `systemctl` reaches systemd through a file-system socket from a private network namespace. | B8 | G7.1 spike |

## Amendment A1 (G7.0 owner decisions)

**A1 (G7.0, 2026-11-23, `docs/DECISIONS.md` D-C87).** The project owner decided the draft items in chat on 2026-11-23. D-C87 records them as items 1 to 18, with the numbers of `D-DRAFT-G7-01` to `D-DRAFT-G7-18` in [Draft decisions for the owner](#draft-decisions-for-the-owner); `D-C82` stays reserved. This amendment edits no sentence of the body above. Where the body speaks of this ADR, a decision or a recommendation as Proposed, as a proposal or as "not decided here", the decisions below govern; where an item is changed, the change governs over the body.

### A1.1 Accepted as recommended

Items 1 to 5, 7 to 10, 12 to 15, 17 and 18 are accepted as recommended in the table of draft decisions, among them layout L1 (item 2), read path R1 with R3 as a fallback only (item 3), the deny list of B3 rule 10 (item 5), and the residual risk TB1 (item 8). The conflicts these items settle and the documents changed for them are named in D-C87.

### A1.2 Item 6, critical units: accepted with a change

`tailscaled.service` is restart only, like SSH and the network path. In the table of B3 rule 11, the row "Remote access" therefore reads "No (restart only)" instead of "Yes, with warning"; a stop of it is refused with `operation_not_offered` (B2) and BF12 covers it. The warnings for the kinds of B3 rule 11 stay as recommended. The rule in the owner's words: what can cut the person's access can only be restarted, not stopped. The acceptance criterion of ORB-SVC-003 says so (specification 0.4.3).

### A1.3 Item 11, connected users: changed

"Verbundene Benutzer" in ORB-PWR-002 means other Oarfish sessions and open live views only (B6, list items 1 to 3). Part (4) of that list, the count of login sessions on the device from systemd-logind, is dropped from G7, and assumption A-06 is dropped from the spike G7.1. ORB-PWR-002 carries the definition (specification 0.4.3).

### A1.4 Item 16, installer budget: not decided

The installer budget stays open (K3). The owner decides it before G7.3, together with the needs of D-D10, as B10 already plans. The owner's stated direction, not binding: raise the limit (option I1 of B1) rather than restructure the root script. No budget value is changed; `scripts/check-budgets.sh` and `docs/DEVELOPMENT.md` are not touched.

### A1.5 Added task for the spike G7.1

The spike reads the real service list on the reference device and reports further units that can cut the person's access, as candidates for restart only. **The list of B3 rule 11 has not been checked on a device.** A unit is added to the restart-only list only by a later owner decision on the spike's findings (the stop point of G7.1 in B10).

### A1.6 What is not done

The amendments named in [Amendments this ADR would need](#amendments-this-adr-would-need) are made as text only: ADR 0010 amendment A19, ADR 0009 amendment A7 and the specification in version 0.4.3 (8.6, 14.2, ORB-SVC-003, ORB-LOG-002, ORB-PWR-002). Specification 8.2 (K12) is aligned in G7.3. No code implements anything of this ADR yet: the broker is G7.2, its packaging G7.3, the service side and the audit in code G7.4, reboot and shutdown G7.5. **Nothing in this ADR is verified, on a device or otherwise**; the assumptions A-01 to A-05 and A-07 to A-10 stay open for the steps named in [Assumptions to verify](#assumptions-to-verify). G7.1 has not started. (G7.1 ran on 2026-11-23; see amendment A2 and amendment A3.)

## Amendment A2 (G7.1 device spike findings)

**A2 (G7.1, written 2026-11-23).** Step G7.1 ran on the reference device with throwaway units, a throwaway copy of the broker sandbox and throwaway scripts, as G6.1 did for the update helper. This amendment records what was observed. **It takes no decision.** It edits no sentence of the body above and no row of [Assumptions to verify](#assumptions-to-verify); A2.3 says which verdict belongs to which row. The draft text in A2.5 is marked "proposal, not applied" and changes nothing until the owner decides B2, B3 and B8 (the stop point of G7.1 in B10). `docs/DECISIONS.md` is not changed by it.

**Evidence label.** Every result below is a *Device observation (G7.1), throwaway units, one device, one image, one moment, 2026-11-23*, in the sense of [ADR 0010](0010-update-helper.md#how-to-read-this-adr) "Device observation (G6.1)". It is one reading, not the production path: no broker binary exists, and no real (non-throwaway) unit was started, stopped or restarted. It is never called Verified, and the support status of the reference device stays Compatible. The write-up of the spike and its raw outputs are evidence outside this repository and are not linked from here. Device values (host name, user name, address, serial number, boot id, machine id, partition identifiers) are left out; DEVICE, USER and ADDRESS stand for them where needed.

### A2.1 Refuted or surprising: read this first

| # | Finding (device observation, G7.1) | Affects | What it means |
|---|---|---|---|
| S1 | **Refuted.** `show` of a template instance that was never started (`<tpl>@b.service`) reports `LoadState=loaded`: systemd loads an instance from its template on demand. A following `start` of that name returns 0 and creates the instance. | B3 rules 3 and 5 | "Accepted only when systemd already has it loaded" cannot be decided with the `show` shape. As written, rule 5 would let a caller create a new instance of any template. |
| S2 | **Refuted.** Stopping a one-shot unit with `RemainAfterExit=yes` runs its `ExecStop=`. On this image: `rpi-setup-loop@var-swap.service` (active) has `ExecStop=/sbin/losetup -d …` for the swap file's loop device; `alsa-restore.service` (active) runs `alsactl … store`; `nftables.service` (inactive here) has `ExecStop=/usr/sbin/nft flush ruleset`. | B3 rules 6 and 8 | "`service.stop` is allowed (it only ends something that runs)" is not true for these units: a stop runs a privileged task. |
| S3 | **Surprise.** A unit with `RefuseManualStop=yes` refuses `restart` as well as `stop` (exit code 4, "Operation refused, unit … may be requested by dependency only"). | B3 rule 9 | Rule 9 should name restart for `RefuseManualStop=yes`. |
| S4 | **Partly refuted.** The manager's `DefaultTimeoutStartUSec` is 90 s, but `NetworkManager.service` has `TimeoutStartUSec=10min`, and `cloud-init-main.service` and `rc-local.service` have `infinity` (non-one-shot units). | B2 deadline, A-04 | The 100 s deadline covers the default, not every unit. A restart of NetworkManager, which B3.11 makes restart only, can outlast it and end in `timeout`. |
| S5 | **Surprise.** A `Type=exec` unit whose main process exits with 1 right after starting: `systemctl start` returned 0 after 162 ms; one second later `ActiveState=failed`, `Result=exit-code`. | B2 verification of `service.start` and `service.restart` | Only the `show` read after the call catches this, and only if the unit has already failed when it is read. |
| S6 | **Surprise.** `list-unit-files` takes 2.6 to 2.7 s on the reference device; `list-units` takes about 50 ms. | B2 bounds (5 s per call, 5 s cache) | Within the bound, with a margin below two. |
| S7 | **Surprise, parsing.** `show` prints properties in systemd's own order, not in the order of `--property=`. `show` of an unknown name returns 0 with `LoadState=not-found`. `list-units --all` lists 16 `not-found` entries (names referenced by other units, without a file). `list-unit-files` prints a third column (vendor preset) and the states `enabled-runtime`, `linked` and `alias`, beside the ones B2 names. | B2 read shapes, B3 rule 3 | Parse by key, treat `not-found` as unknown, and expect three columns. |
| S8 | **Surprise.** In the broker sandbox, `systemctl` cannot check whether it runs in a chroot (`/proc/1/root` is not accessible) and logs "Failed to check if we're running in chroot, assuming not: Permission denied". It then works normally. | B8, A-09 | The calls work because systemd 257 assumes "not in a chroot" on this error. If another systemd version decided the other way, `systemctl` would ignore the command and still exit 0 (*assumption*, not observed). |
| S9 | **Surprise.** The two sides reach systemd differently: as `oarfish` in the service sandbox, `systemctl` connects to the D-Bus system bus (`/run/dbus/system_bus_socket`); as uid 0 in the broker sandbox it connects to `/run/systemd/private`. | R1, B8 | R1 depends on `dbus.service` running; the broker does not. |
| S10 | **Not captured.** For `reboot` and `poweroff` the device went down within about 1 to 1.5 s of the start of the sandboxed unit, and the exit line of `systemctl` was lost with the link and the volatile journal. | A-09, B9 kit | The evidence for both is the device's behaviour, not an exit code. |
| S11 | **Table gaps.** Several units of the B3.11 table do not exist on this image; several units on the image that can cut access are not in it (A2.4). | B3 rule 11 | Input for the owner's decision; nothing is changed here. |

### A2.2 Device and method

- **Device.** One Raspberry Pi Zero 2 W Rev 1.0, revision code `902120`; Raspberry Pi OS Lite 64-bit (Debian 13 "trixie", Raspberry Pi reference image of 2026-10-06); kernel `6.18.50+rpt-rpi-v8` (aarch64); systemd 257 (`257.13-1~deb13u1`); kernel command line with `cgroup_disable=memory`, controllers `cpuset cpu io pids` (as ADR 0010 A4); journald `Storage=volatile` from a drop-in, `/var/log/journal` empty, one boot in the journal after each restart (as ADR 0010 A6); 425,116 kB `MemTotal`.
- **Production state.** Release `0.1.0-alpha.12`, read from the name of the release directory that `/opt/oarfish/current` points to (the release tree holds no manifest file; the binary was not run). `oarfish.service` active and running, `oarfish-update-helper.socket` active and listening, `oarfish-update-helper.service` and `oarfish-update-recover.service` inactive.
- **Method.** A private kit outside this repository, run from the development computer over SSH with `sudo`, as fixed named blocks (no command is passed in from outside). Every throwaway unit, directory and file on the device had the prefix `oarfish-spike71-`. Two sandbox units were made mechanically and checked with `diff` on the device:
  1. **Service sandbox copy:** the `[Service]` section of `install/oarfish.service` verbatim (`User=oarfish`, `Group=oarfish` and the whole hardening block); changed were only `ExecStart=`, the names in `StateDirectory=`, `CacheDirectory=` and `RuntimeDirectory=`, and `Description=`.
  2. **Broker sandbox copy:** the `oarfish-broker.service` text of B8 verbatim, including `After=oarfish-update-recover.service`, `PrivateNetwork=yes`, `ProtectProc=invisible`, `ProcSubset=pid`, the empty `CapabilityBoundingSet=`, `RestrictAddressFamilies=AF_UNIX`, `IPAddressDeny=any`, `SystemCallFilter=@system-service`, `MemoryMax=32M`, `TasksMax=16` and `User=root`; changed were only `ExecStart=` and `Description=`. One copy per case.

  Inside a sandbox, a POSIX shell script ran `/usr/bin/systemctl` with the exact shapes of B2 against throwaway target units. Durations were measured with nanosecond timestamps around each call. `VmHWM` of the `systemctl` child was polled from `/proc/<pid>/status` while it ran, so it is a lower bound. The transport was read from the debug log of `systemctl` (`SYSTEMD_LOG_LEVEL=debug`), as a diagnosis only, without `strace` and without installing a package. The throwaway target units were: a long-running `Type=exec` unit, a `Type=notify` unit that never becomes ready (`TimeoutStartSec=10`), a unit whose main process exits 1, a unit whose `ExecStartPre=` fails, a masked name (link to `/dev/null`), a transient unit made with `systemd-run --unit=…`, a `Type=oneshot` unit, a unit with `RefuseManualStop=yes`, a template with one started and one never-started instance, and an alias made as a link to the long-running unit.
- **Snapshots.** Before anything else, before the reboot, after the reboot, after the power-off and after the cleanup, the kit recorded `ActiveState`, `SubState`, `UnitFileState`, `FragmentPath` and `NRestarts` of `oarfish.service` and the three helper units, the SHA-256 of their unit files and of `/opt/oarfish/current/oarfish`, and the target of `/opt/oarfish/current`. **All five snapshots are identical.**
- **Incidents.** (1) For the two shortest lock cases, the first journal read returned nothing (a race with journald). The lines were read again by unit name; the kit then waited 1 s before reading. (2) The exit lines of `systemctl reboot` and `systemctl poweroff` were not captured (S10). (3) No production unit, file or lock was touched; the real `/var/lib/oarfish-update` was only read with `stat`.
- **Not checked: Ubuntu Server.** There is no Ubuntu device (G12).

### A2.3 Verdicts per assumption

Each row names the row of [Assumptions to verify](#assumptions-to-verify) it belongs to. The row itself is not edited.

| Row | Verdict (G7.1) | Evidence (device observation, G7.1) |
|---|---|---|
| A-01 | **Confirmed** | As `nobody` outside any sandbox: `list-units` exit 0, 128 lines; `list-unit-files` exit 0, 214 lines; `show` of `ssh.service` exit 0. As `oarfish` in the service sandbox: see A-02. |
| A-02 | **Confirmed** | As uid 999 (`oarfish`), `CapEff` and `CapBnd` 0, `NoNewPrivs` 1, `Seccomp` 2, under the verbatim service sandbox: `list-units --type=service --all --no-legend --no-pager --plain` exit 0, 55 ms (51 ms warm), 15,084 bytes, 129 lines (2 throwaway); `list-unit-files --type=service --no-legend --no-pager` exit 0, 2,701 ms (2,561 ms warm), 13,906 bytes, 214 lines (25 throwaway); the `show` shape of B2 for one unit exit 0, 63 ms, 299 bytes, 13 lines; `show` of the manager exit 0, 35 ms. No output on stderr and no denial line in the journal. Transport: the D-Bus system bus (S9). |
| A-03 | **Partly confirmed** (Raspberry Pi OS: confirmed; Ubuntu: not checked) | systemd 257: `list-units … --output=json` exit 0, 49 ms, 16,994 bytes (1.13 times the plain output), one line; members `unit`, `load`, `active`, `sub`, `description`. `list-unit-files … --output=json` exit 0, 2,559 ms, 16,164 bytes (1.16 times), members `unit_file`, `state`, `preset`. Together they cover name, load, active, sub, description and the unit file state. `--no-legend` makes no difference to the JSON output. The systemd of Ubuntu Server LTS was not checked (no device; G12). |
| A-04 | **Partly confirmed** | `systemctl show` of the manager: `DefaultTimeoutStartUSec=1min 30s`, `DefaultTimeoutStopUSec=1min 30s`. A 100 s deadline covers the default, but not every unit (S4). Throwaway `Type=notify` unit with `TimeoutStartSec=10`, started from the broker sandbox with `systemctl start --no-ask-password --job-mode=fail -- <unit>`: exit 1 after 10,078 ms, stderr "Job for … failed because a timeout was exceeded."; afterwards `ActiveState=failed`, `SubState=failed`, `Result=timeout`. |
| A-05 | **Confirmed** | 102 of 180 non-template unit files are `Type=oneshot` (one of them throwaway). First-boot and key-generation tasks: `regenerate_ssh_host_keys.service`, `sshd-keygen.service`, `rpi-resize.service` ("Grow and trim root filesystem on first boot"), `systemd-firstboot.service`, `cloud-init-local.service`, `cloud-init-network.service`, `cloud-config.service`, `cloud-final.service`, `systemd-growfs-root.service`, `systemd-machine-id-commit.service`. Similar privileged tasks: `rpi-eeprom-update.service` (EEPROM update check), `apt-daily.service`, `apt-daily-upgrade.service`, `sshswitch.service` ("Turn on SSH if /boot/ssh … is present"), `rpi-resize-swap-file.service`, `nftables.service`, `e2scrub_all.service`, `fstrim.service`, `dpkg-db-backup.service`, `logrotate.service`, `man-db.service`, `ldconfig.service`. Read only; nothing was started. |
| A-06 | **Dropped** by A1.3 | Not checked. |
| A-07 | **Not run** as BF27 evidence | Observed only alongside the power-off test (owner present and agreed in chat): after `systemctl poweroff` from the broker sandbox, SSH did not answer for 90 s (9 attempts), nor at any time before the owner restored the power about 7 minutes later. It answered 17 s after the owner reported the power restored. This is not BF27 evidence: there was no audit entry, no GUI and no production broker. |
| A-08 | **Confirmed** | Throwaway directory `root:root`, 0700, opened read-only by `flock(1)` from the broker sandbox (no `ReadWritePaths=`). (a) `LOCK_SH\|LOCK_NB` with nobody holding it: exit 0, 14 ms. (b) While another process held `LOCK_EX`: exit 1, 8 ms (the conflict exit code of `flock -n`; the errno itself was not printed). (c) While the sandboxed process held `LOCK_SH` for 12 s: `LOCK_EX\|LOCK_NB` from another process exit 1; `LOCK_SH\|LOCK_NB` from another process exit 0; after the release `LOCK_EX\|LOCK_NB` exit 0. The real `/var/lib/oarfish-update` is `root:root`, 0700, a directory (`stat` only; no lock taken). |
| A-09 | **Confirmed** for `start`, `stop` and `restart` on throwaway units; **confirmed by effect** for `reboot` and `poweroff` (exit code not captured, S10) | In the broker sandbox `/proc/self/status` shows `Uid` 0, `CapInh`, `CapPrm`, `CapEff`, `CapBnd` and `CapAmb` all 0, `NoNewPrivs` 1, `Seccomp` 2 (22 filters). Long-running unit: `show` exit 0, 108 ms; `start` exit 0, 115 ms, then `ActiveState=active`, `SubState=running`; `start` again exit 0, 53 ms; `restart` exit 0, 130 ms, then a new `MainPID` and a new `ExecMainStartTimestampMonotonic`; `stop` exit 0, 114 ms, then `inactive`/`dead`; `stop` again exit 0, 78 ms. No call needed a capability, and no polkit or D-Bus refusal appeared. `systemctl reboot --no-ask-password`: the sandboxed unit started (exit 0); SSH closed within 1 s; the device answered again after 52 s; at the check, uptime was 70.8 s. `systemctl poweroff --no-ask-password` (owner present): SSH closed about 1.5 s after the start; for the rest see A-07. After each boot: `oarfish.service` active, `oarfish-update-helper.socket` active, `systemctl --failed` empty, no line about an ordering cycle in the boot journal. |
| A-10 | **Confirmed** | In the broker sandbox only `lo` exists (`/sys/class/net`). The debug log of `systemctl` shows "starting bus by connecting to /run/systemd/private", and every shape of A-09 worked. |

**The cases B3 relies on**, observed through the `show` shape and the start, stop and restart shapes in the broker sandbox:

| Case | Observed (device observation, G7.1) | B3 rule |
|---|---|---|
| Masked name | `LoadState=masked`, `UnitFileState=masked`; `start` and `restart` exit 1, "Unit … is masked."; `stop` exit 0 (no effect) | Rule 6 as assumed |
| Transient unit (`systemd-run --unit=…`) | `Transient=yes`, `UnitFileState=transient`; `restart` exit 0 (systemd allows it; rule 7 is the broker's own refusal); after `stop` the unit is gone (`LoadState=not-found`) | Rule 7 as assumed |
| `Type=oneshot`, `RemainAfterExit=no` | `start` exit 0, 158 ms, ends `inactive`/`dead`; `restart` exit 0 | Rule 8; see S2 for one-shots with `RemainAfterExit=yes` |
| `RefuseManualStop=yes` | `stop` and `restart` exit 4, "Operation refused" | Rule 9; see S3 |
| Template | `show` of `<tpl>@.service` exit 1 ("neither a valid invocation ID nor unit name"); `start` of it exit 1 ("missing the instance name"); started instance `loaded`/`active`, `restart` exit 0; never-started instance `loaded`/`inactive`, `start` exit 0 creates it | Rule 2 as assumed; rules 3 and 5 refuted (S1) |
| Alias (link) | `show` of the alias: `Id=<target>`, `Names=<target> <alias>`; `start` and `stop` through the alias exit 0 and act on the target; the link makes the target's `UnitFileState=indirect` | Rule 4 as assumed |
| Unknown name | `show` exit 0, `LoadState=not-found`; `start` exit 5, "Unit … not found." | Rule 3 (S7) |
| Main process exits at once | see S5 | B2 verification |

**`systemd-analyze security`** (device observation, G7.1): the broker sandbox copy **0.9 SAFE**; the service sandbox copy 1.7 OK; `oarfish.service` 1.7 OK; `oarfish-update-helper.service` 1.7 OK. The broker sandbox meets the target of B8 (at or below the helper's 1.7). Remaining deductions of the broker copy: root user 0.4, `@resources` and `@privileged` in the system call filter 0.2 each, `PrivateUsers=` 0.2, host root directory 0.1, `AF_UNIX` 0.1, device ACL `char-rtc:r` 0.1.

**Memory** (one reading each, device observation, G7.1, not a budget): `VmHWM` of the `systemctl` child in the broker sandbox, polled, so a lower bound: the `show` shape 7,696 to 8,384 kB; `start` and `restart` 7,760 to 7,768 kB; `stop` up to 8,376 kB. The memory of the read shapes in the service sandbox was not measured.

### A2.4 The service list and the access path (A1.5)

Read only, as root, with the two read shapes and `show` per unit file; nothing was started, stopped or restarted. 127 entries in `list-units --all` and 189 unit files (without the throwaway units); 111 loaded, 41 active.

**Units of the B3.11 table present on the device:** `NetworkManager.service` (running), `wpa_supplicant.service` (running; alias `dbus-fi.w1.wpa_supplicant1.service`), `avahi-daemon.service` (running; alias `dbus-org.freedesktop.Avahi.service`), `ssh.service` (running; alias `sshd.service`), `tailscaled.service` (running). `wpa_supplicant@.service` is installed as a template with no loaded instance.

**Units of the table absent on the device:** `networking.service` (listed as `not-found`: referenced, no unit file), `dhcpcd.service`, `ModemManager.service`, `docker.service`, `containerd.service`, `podman.service`, `chrony.service` (`chronyd.service` is `not-found`), `ntp.service`, `ntpsec.service`. Time is kept by `systemd-timesyncd.service`, which the deny list of rule 10 already covers by the prefix `systemd-`.

**Already covered by the deny list (rule 10):** `dbus.service` (NetworkManager, wpa_supplicant and the D-Bus read path R1 depend on it); `systemd-networkd.service` (installed, inactive), `systemd-timesyncd.service`, `systemd-logind.service`, `systemd-rfkill.service` (radio switch state), `systemd-hostnamed.service` (all by the prefix `systemd-`); `getty@tty1.service`, `serial-getty@.service`, `user@1000.service`, `user-runtime-dir@1000.service`. `systemd-resolved.service` is `not-found`.

**Already covered by rule 8 for start and restart** (one-shot): `regenerate_ssh_host_keys.service` and `sshd-keygen.service` (new host keys would break every client's host key check), `sshswitch.service`, `NetworkManager-wait-online.service`, `cloud-init-network.service`, `tailscale-wait-online.service`, `nftables.service`. Stop stays allowed for them under rule 8 (S2).

**Candidates for restart only, or for the deny list, not in the tables** (one line each; nothing is decided here):

| Unit on the device | State | Reason it can cut access |
|---|---|---|
| `avahi-daemon.service` | running; in the table with "stop offered" | The mDNS responder: a person who reaches the device by its `.local` name loses that name when it stops. |
| `sshd@.service` (template; loaded instance `sshd@sshd-keygen.service`) and `sshd-unix-local@.service` (alias) | template `indirect`; alias | Per-connection SSH daemons: stopping or restarting an instance ends that SSH connection; the B3.11 entry `ssh.service` does not cover the template. Restart does not help here, so the deny list may fit better than restart only. |
| `wpa_supplicant-nl80211@.service`, `wpa_supplicant-wired@.service` | templates, disabled | Wireless and wired authentication; the pattern `wpa_supplicant@*.service` of the table does not match their names. |
| `nftables.service` | one-shot, disabled, inactive | The firewall: `start` (refused by rule 8) can load rules that cut access, and `stop` (allowed by rule 8) runs `nft flush ruleset` (S2). |
| `rpi-usb-gadget-ics.service` | disabled | USB gadget network sharing: on a Zero 2 W, the USB connection can be the person's network path when this is enabled. |
| `ppp@.service` | template, disabled | A PPP link can be the only network path when used. |
| `NetworkManager-dispatcher.service`, `nm-priv-helper.service` | D-Bus activated, static | Helpers of NetworkManager: a stop does not cut a running connection, but can break later network changes; low. |
| `bluetooth.service` | running | Can carry a network path (Bluetooth PAN); no such use was seen on this image; low. |

**Considered, not candidates:** `cron.service`, `udisks2.service`, `rsync.service`, `rc-local.service` (a person's own script; `infinity` start timeout, S4), `cloud-init-main.service` (`infinity`, S4), `rpi-setup-loop@var-swap.service` (not access, but S2).

### A2.5 Proposed changes to B2, B3 and B8 (proposal, not applied)

**Proposal, not applied.** Each item is draft text for the owner's decision at the stop point of G7.1. None changes the body of this ADR until the owner accepts it and a decision records it.

**B2.**

- *P-B2-1 (read shapes).* Keep the two plain shapes as the R1 contract for G7: they work under the service sandbox (A-02) and their output is 13 to 16 % smaller than JSON. Parse `list-units` as four words and the rest of the line as the description. Parse `list-unit-files` as three words: name, state and vendor preset. Accept the state words observed here (`enabled`, `enabled-runtime`, `disabled`, `static`, `masked`, `indirect`, `linked`, `alias`) plus `generated` and `transient`; anything else fails closed. Revisit `--output=json` when the Ubuntu systemd is checked (G12), because A-03 is only half answered.
- *P-B2-2 (`not-found`).* Entries with `LoadState=not-found` in `list-units --all` are not shown as services. A `show` that answers `LoadState=not-found` is `unit_unknown`.
- *P-B2-3 (`show` parsing).* The `show` output is parsed by key, never by position. Each of the 13 keys must appear exactly once, else `internal`.
- *P-B2-4 (timing).* The 5 s bound per call stays. The observed margin for `list-unit-files` is below two (S6), so the device-wide cache of 5 s stays as designed. If the margin proves too small in G7.6, the bound is raised for `list-unit-files` only.
- *P-B2-5 (deadline).* The 100 s deadline stays, and the sentence "covers a normal job" reads "covers the manager's default start timeout of 90 s (device observation, G7.1); units with a longer `TimeoutStartUSec`, among them `NetworkManager.service` (10 min) on the reference image, can end in `timeout` while the job continues" (S4).
- *P-B2-6 (verification after start and restart).* The verification reads the `show` shape once more about 1 s after `systemctl` returned, and accepts only `ActiveState=active`. A failure later than that is outside what the broker verifies, and the page says so in the success text (S5).

**B3.**

- *P-B3-1 (rule 5, instances; S1).* Replace "accepted only when systemd already has it loaded (rule 3)" by: an instance is accepted for `service.stop` and `service.restart` only while its `ActiveState` is not `inactive`, and `service.start` of an instance is refused with `unit_template` unless its `ActiveState` is `failed`. Alternative: the broker reads `list-units --all --type=service --no-legend --no-pager --plain -- <name>` before its `show` and accepts the instance only if it is listed. The name is literal, because rule 1 allows no `*`, `?` or `[`. This adds a fourth shape and may still race with the load that `show` itself causes (not observed).
- *P-B3-2 (rule 8, one-shot; S2).* Refuse `service.stop` of a one-shot unit as well, so that `unit_oneshot` covers all three operations. The text "Oarfish startet solche Aufgaben nicht" then reads "Oarfish verwaltet solche Aufgaben nicht" / "Oarfish does not manage such tasks". Alternative: allow stop only when `RemainAfterExit=no`, which needs one more property in the `show` shape.
- *P-B3-3 (rule 9; S3).* `RefuseManualStop=yes` refuses `service.stop` and `service.restart`. `RefuseManualStart=yes` refuses `service.start` and `service.restart`. The second sentence is an *assumption*: only `RefuseManualStop=` was observed.
- *P-B3-4 (rule 3; S7).* `LoadState=not-found` (exit code 0) is `unit_unknown`. Rule 3 already says so; this adds the observed form.
- *P-B3-5 (rules 10 and 11; A2.4).* The owner decides which candidates of A2.4 join the restart-only list or the deny list. The table of rule 11 keeps its absent units, because other images and Ubuntu may have them; the time row has no unit on this image.
- Rules 2, 4, 6 and 7 behaved as written (A2.3); no change is proposed for them.

**B8.**

- *P-B8-1.* No change of the unit text: start, stop, restart, reboot and poweroff worked with the text verbatim and an empty capability set (A-09, A-10), and the score is 0.9 (target at or below 1.7). No capability stop was needed, and no diagnostic variant was run.
- *P-B8-2 (S8).* Add a sentence: "`systemctl` cannot read `/proc/1/root` in this sandbox and assumes it does not run in a chroot (device observation, G7.1, systemd 257). The broker's verification of service operations by `show` would detect a silent refusal; for `system.reboot` and `system.poweroff` only the next boot does. G7.6 and the Ubuntu check (G12) observe it again."
- *P-B8-3 (memory).* The `systemctl` child alone peaked at about 7.7 to 8.4 MB (7,696 to 8,384 kB, lower bound, A2.3), so the *estimate* "below 12 MB together" in B1 consequence 4 leaves about 4 MB for the broker process. BF31 measures it.
- *P-B8-4 (journal).* For reboot and power-off the broker's own journal lines do not survive (S10, ADR 0010 A6). B5 already makes the audit entry the persistent record; the device kit of B9 must collect the broker's lines before the click, as B9 item 1 says.

### A2.6 What was not verified

- Ubuntu Server: A-03 and every other row (no device; G12).
- The production path: there is no broker binary, socket, `SO_PEERCRED` check, idle exit, rate window or audit entry yet (G7.2 to G7.6). No real unit was started, stopped or restarted, so BF16 and every class D row stay open.
- The exit codes of `systemctl reboot` and `systemctl poweroff` (S10). A-07 as BF27 evidence, and whether the device switches itself on after a longer time or after a power loss while running.
- `RefuseManualStart=yes` (only `RefuseManualStop=yes` was observed).
- Whether a `show` that loads an instance makes it appear in `list-units --all` (P-B3-1, alternative).
- The memory of the read shapes in the service sandbox; `MemoryMax=` stays not enforced on this image (ADR 0010 A4).
- The boot order with broker units (none were installed; BF30) and the cold-boot ordering of a broker socket.
- Hostile output (BF21), behaviour under load, and the read shapes while a job is running.
- A diagnostic variant without `PrivateNetwork=yes`: not needed, because nothing failed.

## Amendment A3 (G7.1 owner decisions)

**A3 (G7.1, 2026-11-23, `docs/DECISIONS.md` D-C88).** The project owner decided in chat on 2026-11-23 on the proposals of [A2.5](#a25-proposed-changes-to-b2-b3-and-b8-proposal-not-applied): "wie empfohlen", that is, every item as written in A2.5, with the choices named below. D-C88 records them as items 1 to 15. This amendment edits no sentence of the body above and no row of [Assumptions to verify](#assumptions-to-verify) or of the [failure matrix](#failure-matrix); it states each changed rule as it now applies, with the proposal id. **Where the body and A3 differ, A3 governs.** The text of A2 stays as written: its A2.5 items, marked "proposal, not applied", are decided by this amendment as stated in the sections below.

**A3 is a decision, not evidence.** It verifies nothing, on a device or otherwise. The findings it rests on are the device observations of A2 (throwaway units, one device, one image, one moment); the reference device stays Compatible, and nothing is called Verified. The installer budget (D-C87 item 16, K3) stays open and is not decided here. G7.2 has not started.

### A3.1 Overview

| D-C88 item | Proposal id | Applies to | Where in A3 |
|---|---|---|---|
| 1 | P-B2-1 | B2 read shapes and parsing | A3.2 |
| 2 | P-B2-2 | B2 read shapes, B3 rule 3 | A3.2 |
| 3 | P-B2-3 | B2 `show` parsing | A3.2 |
| 4 | P-B2-4 | B2 bounds | A3.2 |
| 5 | P-B2-5 | B2 deadline sentence, row A-04 | A3.3 |
| 6 | P-B2-6 | B2 operations table, verification | A3.3 |
| 7 | P-B3-1 | B3 rule 5 | A3.4 |
| 8 | P-B3-2 | B3 rule 8, `unit_oneshot` text | A3.4, A3.3 |
| 9 | P-B3-3 | B3 rule 9 | A3.4 |
| 10 | P-B3-4 | B3 rule 3 | A3.4 |
| 11 | P-B3-5 | B3 rules 10 and 11 | A3.4 |
| 12 | P-B8-1 | B8 unit text (no change) | A3.5 |
| 13 | P-B8-2 | B8 chroot sentence | A3.5 |
| 14 | P-B8-3, P-B8-4 | B8 memory note, journal note | A3.5 |
| 15 | (A2.3) | B3 rules 2, 4, 6 and 7 (no change) | A3.4 |

### A3.2 B2: read shapes and parsing (items 1 to 4)

The two read shapes of B2 stay the R1 contract for G7 (P-B2-1). The following replaces the sentence "A JSON output (`--output=json`) is used instead if the spike shows that both systemd versions in scope support it (A-03)" and adds to the bounds of B2:

- **`list-units` is parsed** as four words (name, load state, active state, sub state) and the rest of the line as the description.
- **`list-unit-files` is parsed** as three words: name, state and vendor preset.
- **Accepted state words** of `list-unit-files`: `enabled`, `enabled-runtime`, `disabled`, `static`, `masked`, `indirect`, `linked`, `alias`, `generated`, `transient`. Any other word fails closed.
- **`--output=json`** is not used in G7. It is revisited when the Ubuntu systemd is checked (G12), because A-03 is only half answered (A2.3).
- **`not-found`** (P-B2-2): entries with `LoadState=not-found` in `list-units --all` are not shown as services. A `show` answer with `LoadState=not-found` is `unit_unknown`.
- **`show` parsing** (P-B2-3): the output of the `show` shape is parsed by key, never by position. Each of the 13 keys of the shape must appear exactly once, else `internal`.
- **Timing** (P-B2-4): 5 s per call and the device-wide cache of 5 s stay as in B2. If the margin of `list-unit-files` (2.6 to 2.7 s observed, S6) proves too small in G7.6, the bound is raised for `list-unit-files` only. (Pointer, UF1: raised to 45 s for `list-unit-files` only, with its own store; see [A9.2](#a92-the-read-path-as-it-now-holds).)

### A3.3 B2: deadline, verification and the text of `unit_oneshot` (items 5, 6, 8)

**Deadline sentence (P-B2-5).** The sentence of B2 "Each mutating call has a deadline of 100 s (systemd's usual start timeout of 90 s plus a margin, A-04)" and the words "covers a normal job" of row A-04 are replaced by: the deadline of 100 s stays. It covers the manager's default start timeout of 90 s (device observation, G7.1). Units with a longer `TimeoutStartUSec`, among them `NetworkManager.service` (10 min) on the reference image, can end in `timeout` while the job continues. The rest of the sentence stands: at the deadline the broker ends its `systemctl` child and answers `timeout` with the state it reads then; the job may continue inside systemd, and the page says so.

**Verification after `service.start` and `service.restart` (P-B2-6).** In the operations table of B2 the column "Verification afterwards" now reads:

| `op` | Verification afterwards (as it now applies) |
|---|---|
| `service.start` | The broker reads the `show` shape once more about 1 s after `systemctl` returned and accepts only `ActiveState=active`; else `state_unexpected` with the observed state. |
| `service.restart` | The same read, about 1 s after `systemctl` returned: `ActiveState=active` is the only accepted state; the check of B2 for a new `ExecMainStartTimestampMonotonic` (or a changed `MainPID`) is not dropped by this amendment and applies to that read. |
| `service.stop` | Unchanged: `ActiveState` is `inactive` or `failed`. |

A failure later than that read is outside what the broker verifies, and the page says so in the success text. The wording of that text is set at the UI step (G7.4); its meaning is fixed here. `system.reboot` and `system.poweroff` are unchanged.

**Typed error `unit_oneshot` (item 8).** The row of the table of typed errors now reads:

| Code | Source | Deutsch | English |
|---|---|---|---|
| `unit_oneshot` | broker | Dieser Dienst führt eine einmalige Aufgabe aus. Oarfish verwaltet solche Aufgaben nicht. | This service runs a one-time task. Oarfish does not manage such tasks. |

### A3.4 B3: the rules as they now apply (items 7 to 11, 15)

Rules 1, 2, 4, 6 and 7 stay as written in B3 (A2.3; item 15). The order of B3 is unchanged. Rules 3, 5, 8, 9, 10 and 11 now read as follows.

3. **Known to systemd, by the broker's own read** (P-B3-4). The broker runs the `show` shape of B2 for the name. `LoadState` must be `loaded` or `masked` (B3.6). `LoadState=not-found`, which `show` answers with exit code 0, is `unit_unknown`; every other `LoadState` is `unit_unknown` too.

5. **Template instances** (P-B3-1, main variant). `show` reports `LoadState=loaded` for an instance that was never started (S1), so `loaded` is not proof that an instance exists. For a name `name@inst.service`:
   - `service.stop` and `service.restart` are accepted only while its `ActiveState` is not `inactive`;
   - `service.start` is refused with `unit_template` unless its `ActiveState` is `failed`;
   - the broker never creates a new instance.

   The alternative of P-B3-1 (a fourth read shape, `list-units` with the literal name) is rejected. The error code for `service.stop` or `service.restart` of an instance whose `ActiveState` is `inactive` is not fixed by P-B3-1 or by D-C88; see A3.7.

8. **One-shot tasks** (P-B3-2, main variant). `Type=oneshot`: `service.start`, `service.stop` and `service.restart` are all refused with `unit_oneshot`, whatever the unit's state and its `RemainAfterExit=`. Reason: starting a one-shot service runs a task, not a daemon (A-05), and stopping one with `RemainAfterExit=yes` runs its `ExecStop=` (S2). The alternative "stop only when `RemainAfterExit=no`" is rejected. This is still the narrowing of ORB-SVC-002 that B3 rule 8 names (D-C87 item 5).

9. **The unit's own refusal** (P-B3-3).
   - `RefuseManualStop=yes` refuses `service.stop` and `service.restart` with `refused_by_unit`. Observed: both exit 4 (S3).
   - `RefuseManualStart=yes` refuses `service.start` and `service.restart` with `refused_by_unit`. **This sentence rests on an assumption:** only `RefuseManualStop=` was observed (A2.6).

10. **Deny list** (P-B3-5; compiled in, refused for every operation with `unit_denied`; the page shows these units without buttons). The deny list is checked against `Id` and every entry of `Names` (rule 4). It now holds:
    - Oarfish's own units: `oarfish.service`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, `oarfish-broker.service`.
    - Every unit whose `Id` or alias begins with `systemd-` (among them `systemd-reboot.service`, `systemd-poweroff.service`, `systemd-halt.service`, `systemd-kexec.service`, `systemd-soft-reboot.service`, `systemd-journald`, `systemd-logind`, `systemd-udevd`).
    - `dbus.service`, `dbus-broker.service`, `polkit.service`; `emergency.service`, `rescue.service`, `debug-shell.service`; `user@*.service`, `user-runtime-dir@*.service`; `getty@*.service`, `serial-getty@*.service`, `autovt@*.service`, `console-getty.service`, `container-getty@*.service`.
    - **New:** `sshd@*.service` and `sshd-unix-local@*.service`. Reason: a restart of such an instance would also end the connection (A2.4).

    The list stays closed and fail-closed only for these names; any other loaded service is allowed unless rules 1 to 9 refuse it.

11. **Critical list** (ORB-SVC-003; allowed, with an additional warning in the dialog, and **restart only** for the units marked "No", P-B3-5). For a unit that is restart only, `service.stop` is refused with `operation_not_offered`; `service.restart` is accepted. The owner's rule: what can cut the person's access can only be restarted, not stopped.

    | Kind | Units (`Id` or alias) | Stop offered |
    |---|---|---|
    | Network | `NetworkManager.service`, `networking.service`, `dhcpcd.service`, `wpa_supplicant.service`, `wpa_supplicant@*.service`, `wpa_supplicant-nl80211@*.service`, `wpa_supplicant-wired@*.service`, `ModemManager.service`, `avahi-daemon.service`, `rpi-usb-gadget-ics.service`, `ppp@*.service` | No (restart only), except `ModemManager.service` |
    | SSH | `ssh.service`, `sshd.service` | No (restart only) |
    | Remote access | `tailscaled.service` | No (restart only; A1.2) |
    | Container engines | `docker.service`, `containerd.service`, `podman.service` | Yes, with warning |
    | Time | `chrony.service`, `ntp.service`, `ntpsec.service` | Yes, with warning |

    Changes against the body and A1.2: `avahi-daemon.service` moves from "stop offered" to restart only (`ModemManager.service` keeps "stop offered"); `wpa_supplicant-nl80211@*.service`, `wpa_supplicant-wired@*.service`, `rpi-usb-gadget-ics.service` and `ppp@*.service` are added as restart only. The rows for units that are absent on the reference image (A2.4) stay, because other images and Ubuntu may have them. For an instance such as `ppp@<name>.service`, rule 5 applies as well.

    (Pointer, UF1: the Network row and restart only gain the name family `netplan-wpa-`; see [A9.3](#a93-rule-11-with-the-family-netplan-wpa-).)

    **No list entry** for `NetworkManager-dispatcher.service`, `nm-priv-helper.service` and `bluetooth.service` (no change). `nftables.service` needs none either: it is `Type=oneshot`, so rule 8 refuses every operation on it.

### A3.5 B8: sentences added (items 12 to 14)

- **Unit text (P-B8-1).** No change of the unit texts of B8.
- **Chroot (P-B8-2).** Added to B8: "`systemctl` cannot read `/proc/1/root` in this sandbox and assumes it does not run in a chroot (device observation, G7.1, systemd 257). The broker's verification of service operations by `show` would detect a silent refusal; for `system.reboot` and `system.poweroff` only the next boot does. G7.6 and the Ubuntu check (G12) observe it again."
- **Memory (P-B8-3), a note on B1 consequence 4.** The `systemctl` child alone peaked at about 7.7 to 8.4 MB (7,696 to 8,384 kB, lower bound, A2.3), so the *estimate* "below 12 MB together" in B1 consequence 4 leaves about 4 MB for the broker process. BF31 measures it.
- **Journal (P-B8-4), a note for the device kit of B9.** For reboot and power-off the broker's own journal lines do not survive (S10, ADR 0010 A6). B5 already makes the audit entry the persistent record; the device kit of B9 must collect the broker's lines before the click, as B9 item 1 says.

### A3.6 Rules and texts that do not change

B3 rules 1, 2, 4, 6 and 7; the unit text of B8; the 100 s deadline, the 5 s bounds and the 5 s cache; the deny list apart from the two names above; every typed error apart from the text of `unit_oneshot`. `docs/DECISIONS.md` D-C87 stays as it is, including item 5 and item 6.

### A3.7 Failure matrix: rows whose expected result changes

The rows of the [failure matrix](#failure-matrix) are not edited. Where a row below differs from the body, this table governs. Classes stay as in the body.

**Rows whose expected result changes:**

| # | New expected result |
|---|---|
| BF4 | An instance that is `inactive`: `service.start` is refused with `unit_template`; `service.stop` and `service.restart` are refused (code open, see below). An instance that is `failed`: `service.start` is accepted; `service.stop` and `service.restart` are accepted. A running instance: `service.stop` and `service.restart` are accepted; `service.start` is `unit_template`, as P-B3-1 says "unless its `ActiveState` is `failed`" (whether `already_in_state` should come first is not decided). A template without instance (`name@.service`): `unit_template`, as before. No new instance is ever created (rule 5). The body's "an instance that is not loaded: `unit_unknown`" no longer describes the case, because `show` reports a never-started instance as loaded (S1). Class T as before; a throwaway template with a started, a never-started and a failed instance. |
| BF10 | A `Type=oneshot` unit: `service.start`, `service.restart` **and `service.stop`** are all `unit_oneshot`, with the new text of A3.3. The body's "stop allowed" is withdrawn. |
| BF11 | `RefuseManualStop=yes`: `service.stop` **and `service.restart`** are `refused_by_unit` (observed, S3). `RefuseManualStart=yes`: `service.start` **and `service.restart`** are `refused_by_unit` (an assumption, not observed; the class T row must observe it). |
| BF12 | Restart only: `service.stop` of `ssh.service` is `operation_not_offered`, as before; the row now covers every unit of the "No" rows of the table in A3.4 rule 11 (among them `tailscaled.service` since A1.2, `avahi-daemon.service`, `wpa_supplicant-nl80211@*.service`, `wpa_supplicant-wired@*.service`, `rpi-usb-gadget-ics.service`, `ppp@*.service`), and `service.restart` of them is accepted. `ModemManager.service` stays stoppable. |
| BF13 | Two cases. `systemctl` exits non-zero (for example a unit whose `ExecStartPre=` fails, or a start timeout): `job_failed` with the state word, audit entry, as before. `systemctl` exits 0 but the unit has already failed when the `show` read about 1 s later is taken (a `Type=exec` unit whose main process exits at once, S5): `state_unexpected` with the state word `failed`. A failure later than that read is not caught (A3.3). |

**Rows whose expected result is the same, with a changed or widened scope:**

| # | Change |
|---|---|
| BF5 | "Unknown unit" now includes the `show` answer `LoadState=not-found` with exit code 0 (rule 3, P-B3-4). Result `unit_unknown` as before. |
| BF6 | The set of entries grows by `sshd@*.service` and `sshd-unix-local@*.service` (rule 10). Result `unit_denied` as before. |
| BF14 | The deadline of 100 s stays; the row's unit with a start timeout longer than 100 s (the shape of the `NetworkManager.service` case, S4) also ends in `timeout`, and the job may continue. |
| BF15 | The state is read about 1 s after `systemctl` returned (item 6); result `state_unexpected` as before. |
| BF16 | The success verification is the read about 1 s after `systemctl` returned; for a restart, `ActiveState=active` and a new start timestamp or `MainPID`. The success text says that a later failure is not verified. |
| BF21 | Class C cases added: a `show` output with a missing or duplicated key is `internal`; a `list-unit-files` line with a state word outside the closed set, or with a number of words other than three, fails closed; `list-units --all` entries with `LoadState=not-found` are not shown. |
| BF29 | "Every denied case of B2 and B3" now includes the rows of this table (rules 5, 8, 9, 10 and 11 as they now read). |
| BF31 | Records `VmHWM` of the broker apart from its `systemctl` child, because the child alone was observed at 7.7 to 8.4 MB (P-B8-3). |

**Open point in BF4.** Neither P-B3-1 nor D-C88 names the error code for `service.stop` or `service.restart` of a template instance whose `ActiveState` is `inactive`. The body has no code for it either (`already_in_state` fits a stop only). A3 does not choose one. It is chosen from the closed set of B2 in G7.2 and shown to the owner at the review of the privileged code, or decided earlier by a further amendment. (Decided by D-C89 items 2 to 4 before the code was written; see [A4.1](#a41-owner-decisions-d-c89-as-they-now-apply).)

Rows not named above (BF1 to BF3, BF7 to BF9, BF17 to BF20, BF22 to BF28, BF30, BF32) are unchanged. BF8 is unchanged: masked units behave as in rule 6, and a masked unit that is also `Type=oneshot` is refused by rule 8 for `service.stop` as well.

### A3.8 What is not verified, and what stays open

- **Nothing is verified by A3.** It records decisions. The evidence behind them is A2, a set of device observations on throwaway units; no production path exists yet.
- **Assumption, from item 9:** `RefuseManualStart=yes` refuses `service.start` and `service.restart`. Not observed (A2.6).
- **Not observed and still open from A2.6:** Ubuntu Server (A-03 and the rest, G12); the exit codes of `systemctl reboot` and `systemctl poweroff` (S10); A-07 as BF27 evidence; the memory of the read shapes in the service sandbox; the boot order with broker units (BF30); hostile output (BF21); and the read shapes while a job runs.
- **Installer budget (D-C87 item 16, K3):** not decided. It is decided before G7.3, together with the needs of D-D10. `scripts/check-budgets.sh` and `docs/DEVELOPMENT.md` are not touched.
- **Error code for `service.stop` and `service.restart` of an inactive template instance:** open (A3.7, BF4).
- **Next:** the installer budget decision, then G7.2. G7.2 has not started and is not prepared by this amendment. (Both happened: D-C89 decides the budget and the open code of BF4, and G7.2 is recorded in [amendment A4](#amendment-a4-g72-broker-core-as-implemented).)

## Amendment A4 (G7.2 broker core as implemented)

**A4 (G7.2, 2026-11-24, `docs/DECISIONS.md` D-C89).** Step G7.2 implements the broker core of B10: the binary `oarfish-broker` with the subcommand `serve`, the protocol, the peer check, the constant G7 policy (B7), the unit rules (B3), the fixed `systemctl` shapes (B2), the rate limits (B4), the update check of the power operations (B6), the idle exit and the resource bounds (B8), the two unit files, a reproducible build, the unit checks in CI and the class C tests. The broker is **not** in the release bundle, the installer, `bundlegen`, `relverify`, the helper or the release workflow; that is G7.3. This amendment edits no sentence of the body, A1, A2 or A3, except a pointer sentence in A3.7 and one in A3.8.

**A4 is class C evidence, not device evidence.** The tests prove argument shapes, the validation, the order of the checks and the error mapping against a fake `systemctl` and a fake root, and the socket handling on a real Unix socket. They prove nothing about systemd's behaviour. No device was touched in G7.2; every class D and T row of the [failure matrix](#failure-matrix) stays open for G7.6. Nothing is Verified, and the reference device stays Compatible.

**Deviations from the body, A1 and A3: none.** Where this ADR left a choice to the code, A4.4 names it.

### A4.1 Owner decisions (D-C89) as they now apply

| D-C89 item | Rule as it now applies |
|---|---|
| 1 | The installer budget is raised from 50,000 to 80,000 bytes (option I1 of B1). Applied in G7.3; G7.2 changes no budget value. |
| 2 | A template instance whose `ActiveState` is `inactive`: `service.stop` is `already_in_state`, as for any other unit that is already stopped. |
| 3 | A template instance whose `ActiveState` is `inactive`: `service.restart` is `unit_template` (a restart would start it; rule 5). |
| 4 | A template instance whose `ActiveState` is `active`: `service.start` is `already_in_state`, not `unit_template`. `service.start` of an instance that is neither `active` nor `failed` stays `unit_template`. |
| 5 | The restart verification keeps the check for a new `ExecMainStartTimestampMonotonic` or a changed `MainPID` on the read about 1 s after `systemctl` returned (A3.3). |
| 6 | Power flag (option P1): after the broker answered `accepted` to `system.reboot` or `system.poweroff`, it does not idle-exit; it runs until the shutdown ends it or until `RuntimeMaxSec=300`. The flag is in memory only. |
| 7 | Rate counting: a slot of the window (10 mutating service operations per 60 s) is used as soon as a mutating service request passes the rate check, whatever the later result. Power operations use no slot. |

**BF4 as it now reads** (replaces the open point of A3.7): template without instance, every operation: `unit_template`. Instance `inactive` (never started, S1): start `unit_template`, stop `already_in_state`, restart `unit_template`. Instance `failed`: start, stop and restart accepted. Instance `active`: start `already_in_state`, stop and restart accepted. Instance in any other state: start `unit_template`; stop and restart accepted. No new instance is ever created.

**Residual risk of item 6, beside TB1.** If the device has not gone down within 300 s after `accepted`, systemd ends the broker at `RuntimeMaxSec=300`, and a new broker can accept one more power request in the same boot. A crash of the broker has the same effect at any time. TB1 therefore reads, for the power operations: "reboot or power off the device once per broker lifetime, which is once per boot unless the shutdown does not happen within 300 s of an accepted request or the broker crashes". The same holds for the rate window: it lives as long as the process, so a broker ended at `RuntimeMaxSec=300` starts with an empty window (B4 already accepts this for a crash).

**Four choices accepted by the owner with items 6 and 7:** `correlation_id` is exactly 16 lowercase hexadecimal digits; the build id in `status` is a constant compiled into the binary (`broker-1`); `success` answers a service operation (and `status`) and `accepted` a power operation; `service.restart` of an ordinary inactive unit is allowed and starts it, while the refusal `unit_template` of item 3 applies to template instances only.

### A4.2 Files and functions

All in `backend/cmd/oarfish-broker` (package `main`, standard library only, the Go version of the module):

| File | Content |
|---|---|
| `main.go` | constants of B2, B4 and B8; `options` (everything the tests replace) and `productionOptions`; `run` (exactly one argument, `serve`, else the usage text and exit 2); `serve`; `lookupUID` (`os/user`, once at start); `journal` (whole lines to standard error) |
| `activation.go` | `activatedListener`: exactly one socket from systemd (`LISTEN_PID` equal to the own pid, `LISTEN_FDS=1`, descriptor 3, a Unix stream listener), else exit 1 with a fixed message; the variables are unset first |
| `peercred_linux.go`, `peercred_other.go` | `peerUID` by `SO_PEERCRED`; on other systems every peer fails |
| `server.go` | `server.serve` (accept loop, at most 4 connections at once, idle exit), `server.handle` (peer check, one request line, one answer), `drain` |
| `protocol.go` | `readRequest`, `decodeRequest`, `requestMembers`, `validCorrelationID`, the codes, `answer`, `statusAnswer` |
| `policy.go` | `Caller`, `Operation`, `Policy` and the constant `corePolicy` of B7 |
| `unit.go` | `validUnitName` (rule 1), `splitTemplate`, `denyList` (rule 10), `restartOnly` (rule 11), `parseShow` and `unitFacts` (the show shape) |
| `systemctl.go` | the shapes `shapeShow`, `shapeUnitJob`, `shapePower`; `allowedShape`; `broker.systemctl`; `runProgram` (the only place that starts a process); `limitedBuffer` |
| `update.go` | `checkUpdate` (B6), `readHelperPhase`, `decodeHelperPhase`, `jsonObject` |
| `broker.go` | `broker.respond`, `logLine`, `status`, the rate window, `begin`/`end` (busy), `mayExit`, `service`, `checkUnit` (B3 in order), `runServiceJob` (call and verification), `powerOp` |

Unit files: `install/oarfish-broker.socket` and `install/oarfish-broker.service`, the text of B8 verbatim, each starting with the comment line that names it in the block of B8 (`TestUnitFilesMatchTheADR` compares them with this document). Scripts: `scripts/build-broker.sh`, `scripts/check-broker-independent.sh`; `scripts/check-units.sh` and `scripts/verify-units.sh` extended. Make targets `build-broker`, `test-broker`, `check-broker`; `make fuzz` runs four broker targets. CI job `broker`; the job `helper-units` runs the extended unit scripts unchanged in its steps.

### A4.3 The protocol as built

- **Request.** One JSON object on one line, at most 1,024 bytes including the newline, read within 5 s. Member sets: `status`, `system.reboot`, `system.poweroff`: exactly `v`, `op`, `correlation_id`; `service.start`, `service.stop`, `service.restart`: exactly `v`, `op`, `unit`, `correlation_id`. `v` is the JSON literal `1` (not `1.0`, `1e0` or `"1"`), for every operation including `status`. `correlation_id` is required for every operation and is exactly 16 lowercase hexadecimal digits. `unit` is a JSON string; rule 1 of B3 decides on it (`unit_invalid`), not the decoder. Unknown members, a member in another letter case, repeated members, `null`, a wrong type, data after the object or after the newline, a missing newline and a longer line are `bad_request`, and nothing runs.
- **Answer.** One JSON line: `{"v":1,"code":"…"}`, with `"state":"…"` (a word of the closed set of `ActiveState`: `active`, `reloading`, `inactive`, `failed`, `activating`, `deactivating`, `maintenance`, `refreshing`) on `success` of a service operation and on `job_failed`, `timeout` and `state_unexpected` when the state could be read, and `"retry_after_s":n` (whole seconds, at least 1) on `rate_limited`. `status` answers `{"v":1,"code":"success","protocol_min":1,"protocol_max":1,"build_id":"broker-1","power_requested":…,"busy":…}`. No answer carries the request, a path, an operating-system error text or any `systemctl` output.
- **Codes.** The closed set of B2 (as amended by A3) except the service's own `unavailable`, `wrong_password` and the password `rate_limited`, plus `success` and `accepted`: 22 codes (`allCodes`). `TestCodesMatchTheADR` compares them with the table of B2; `TestEveryCodeIsReached` reaches each.
- **Journal.** One line per answered request: `op=<op> unit=<name|-> cid=<id> result=<code>`. The unit is printed only when it passes rule 1 (otherwise `-`); `op` and `cid` are `-` when the request was not decoded. A refused peer gets no line (as the helper). Without the user `oarfish` the broker writes one line at start: `oarfish-broker: the user oarfish does not exist; every connection is refused`.

### A4.4 Choices the code made where this ADR did not fix one

1. **Rule 1 and rule 2.** The pattern of rule 1 requires a non-empty instance, while rule 2 answers `unit_template` for `name@.service`. The code's pattern lets the instance be empty, so that rule 2 can answer; every other part of rule 1 is as written (ASCII, 1 to 255 bytes, no `..`, a backslash only as `\x` and two lowercase hexadecimal digits).
2. **Order of rule 4 and rule 10 for aliases.** A request by an alias name is `unit_not_canonical` (rule 4 comes first), also when the alias or its unit is on the deny list. A request by the canonical name whose `Names` holds a denied name is `unit_denied`. Both refuse; BF6 is tested for both forms.
3. **Where `already_in_state` is decided.** It is the state precondition of B2 after rules 1 to 11: start of an `active` unit, stop of an `inactive` unit. Rule 5 refuses with `unit_template` only start of an instance that is neither `active` nor `failed`, and restart of an `inactive` instance. So D-C89 item 4 holds (an active instance is never `unit_template` for start), and a denied active instance (for example `getty@tty1.service`) is `unit_denied`, not `already_in_state`.
4. **Rule 3 before the values.** The output of `show` is first checked for its shape (13 keys, each exactly once, printable ASCII, no other line; else `internal`), then `LoadState` decides rule 3 (anything other than `loaded` or `masked` is `unit_unknown`), then the other values are checked, each fail-closed (`internal`): `Id` and every entry of `Names` pass rule 1, at most 64 names, single spaces; `ActiveState` and `Type` (`simple`, `exec`, `forking`, `oneshot`, `dbus`, `notify`, `notify-reload`, `idle`) from closed sets; `Transient`, `RefuseManualStart`, `RefuseManualStop` are `yes` or `no`; `ExecMainStartTimestampMonotonic` and `MainPID` plain decimals (64 and 32 bits); `SubState`, `Result`, `UnitFileState` match `[a-z0-9-]{0,64}`.
5. **`show` failures.** A non-zero exit, a timeout, output over 256 KiB or a wrong shape of the check read is `internal`. The `show` call has a deadline of 5 s (the bound B2 names for the read shapes).
6. **Reads after the call.** After a non-zero exit or the deadline, the state is read once; if that read fails, the answer is `job_failed` or `timeout` without a state word. If the verification read after exit 0 fails, the answer is `internal`. The read after `stop` is made at once (A3.3: stop unchanged); the read after `start` and `restart` waits `verifyDelay` (1 s).
7. **Power operations.** The deadline of 100 s applies to `reboot` and `poweroff` as well. A non-zero exit is `job_failed` and the deadline `timeout`, both without a state word; neither sets the power flag. Only `accepted` sets it. The order is B2's: policy, busy, update check, once per lifetime; so with the flag set and an update running the answer is `update_running`.
8. **Update check (B6).** The directory is opened with `O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC`; only its absence (`ENOENT`) means "no helper installed". Any other error, a held `LOCK_EX` and any other lock error are `update_running`. `state.json` is opened without following a link and must be a regular file of at most 64 KiB; a missing `state.json` (`ENOENT`) in an existing directory that the broker has opened and locked counts as no transaction, exactly as the update helper treats it (`loadState`: a missing file is a fresh idle state), and the power operation goes on (owner, D-C89 item 8). The reasons: (a) the helper writes `state.json` only when a transaction or a recovery first saves its state, so on a device that was installed but never staged an update the directory exists and the file does not; refusing then would block every reboot and shutdown with `update_running` until the first update; (b) the directory is `root:root 0700` and the service cannot create, delete or replace anything in it, so a missing file is not something a compromised service can arrange; (c) a running transaction is still refused, because the helper holds `LOCK_EX` for the whole transaction and `LOCK_SH|LOCK_NB` then fails. Every other case stays fail closed (`update_running`): a `state.json` that exists but is not a regular file, is a link (dangling or not), is unreadable, is too large, is not of schema 1, or has a malformed or missing `transaction` or `phase`, or names a phase outside the finished set; a directory that exists but cannot be opened or locked. The schema check: `schema_version` is the literal `1`, `transaction` an object and `phase` a string, each present exactly once and in its exact letter case; unknown members are ignored at both levels (the helper adds members additively, ADR 0010 U15 rule 6). The finished phases are `idle`, `staged`, `committed`, `rolled_back` and `rollback_failed`. **No component phase counts as ended:** `c_switching`, `c_switched`, `c_verified`, `c_committed` and `c_rolling_back` are each a phase inside a transaction (ADR 0010 A16.2), so all five are `update_running`, as are `staging`, `switching`, `verifying`, `rolling_back` and every unknown value. The shared lock is released when `systemctl` has returned, also after a failure.
9. **Policy refusal.** `corePolicy` refuses every caller other than the core uid with `internal`, because the closed set has no capability code yet. In G7 no such caller passes the peer check, so the refusal is not reachable through the socket (tested directly). G8 names the code.
10. **Status.** Answered while an operation runs; uses no rate slot and no policy decision (B2: peer only). `power_requested` reports the flag of item 6, which lives as long as the process.
11. **Connections and time limits.** At most 4 connections are served at once (others wait in the backlog of 8); 5 s to send the request, 5 s to write the answer, then a drain of at most 250 ms and 4 KiB of unread client input.
12. **Idle exit.** The broker exits when 30 s have passed since the last connection ended, no connection is open, no operation runs, the rate window is empty and no power operation was accepted. When the window empties later than 30 s after the last connection, the broker exits when it empties.
13. **`systemctl` environment.** Exactly `PATH=/usr/sbin:/usr/bin:/sbin:/bin` and `LC_ALL=C`; standard input `/dev/null`, standard error discarded, working directory `/`. At the deadline the child is killed (`SIGKILL`); the broker waits at most 2 s more for its output.
14. **Rule 11 in the broker.** The broker enforces only the restart-only part (the "No" rows). The warnings of the other rows are for the page (G7.4) and change nothing at the socket.
15. **Termination.** `SIGTERM` (shutdown, `RuntimeMaxSec=`) ends the process with Go's default behaviour; there is nothing to save. `LISTEN_FDNAMES` is unset and not used.

### A4.5 Test seams and the release binary

There is no build tag and no fault hook. Every seam (the path of `systemctl`, the runner, the update directory, the user lookup, the peer check, the clock, the policy, the time limits) is a field of `options`, which only tests construct; `main` always runs `productionOptions`. The production binary reads only `LISTEN_PID` and `LISTEN_FDS` from its environment and takes exactly the argument `serve`. Tests pin this: `TestProductionOptions` (the constants of this ADR, no runner, the constant policy), `TestNoEnvironmentSeams` (no `flag` package, no other environment read), `TestOnlySystemctlExecutes` (only `systemctl.go` imports `os/exec`, one `exec.CommandContext`, no shell named; no call that creates, writes, renames or removes a file anywhere in the broker) and `TestImports` (standard library only, for linux/arm64, linux/amd64 and the host). The re-executed child of the process tests uses variables named in `main_test.go` only, which is not compiled into the binary. Because no tag exists, `make check-broker` has no hook check; such a check is needed only where a build tag compiles a seam in.

### A4.6 Class C tests per row of the failure matrix

Expected results as amended by A3.7 and A4.1. All in `backend/cmd/oarfish-broker`.

| Row | Tests |
|---|---|
| BF1 | `TestWrongPeer` (root, another uid, an unreadable uid), `TestRefuseAll`, `TestServeProcess` (the real `SO_PEERCRED` path on Linux) |
| BF2 | `TestDecodeRejects`, `TestReadRequest`, `TestMalformedOverSocket`, `FuzzDecodeRequest` |
| BF3 | `TestRule1InvalidNames`, `FuzzUnitName` |
| BF4 | `TestRule5Templates` |
| BF5 | `TestRule3Unknown` |
| BF6 | `TestRule10Denied` |
| BF7 | `TestRule4Alias` |
| BF8 | `TestRule6Masked` |
| BF9 | `TestRule7Transient` |
| BF10 | `TestRule8Oneshot` |
| BF11 | `TestRule9Refusal` |
| BF12 | `TestRule11RestartOnly` |
| BF13 | `TestJobFailed` |
| BF14 | `TestTimeout`, `TestRunProgramDeadline`, `TestServiceThroughProcessFake` |
| BF15 | `TestStateUnexpected`, `TestVerificationDelay` |
| BF16 (class C part) | `TestServiceSuccess` |
| BF17 | `TestBusy`, `TestConcurrentConnections` |
| BF18 | `TestRateLimit`, `TestWindowKeepsBroker` |
| BF20 | `TestCrashDuringOperation` (the broker process is killed while the fake `systemctl` runs; no answer; the next start serves) |
| BF21 (broker side) | `TestHostileShowOutput`, `TestRunProgramBound`, `TestParseShow`, `TestShowValueSets`, `FuzzParseShow`; the `list-units` and `list-unit-files` cases of A3.7 belong to the service's read path (G7.4) |
| BF24 (class C part) | `TestUpdateLockHeld`, `TestUpdateLockHeldDuringCall` |
| BF25 | `TestUpdatePhases`, `TestUpdateStateFailsClosed`, `TestUpdateMissingState`, `TestUpdateLinks`, `FuzzHelperPhase` |
| BF28 | `TestPowerOnce`, `TestPowerKeepsBroker`, `TestPowerOrder` |
| BF29 | `TestCompromisedServiceBounded`, `TestEveryCodeIsReached` |
| BF30 (class C part) | `scripts/verify-units.sh` (boot transaction of `multi-user.target` with the broker socket's wants link) and `scripts/verify-units.sh --cycle-self-test` |
| BF32 (class C part) | `scripts/verify-units.sh` prints `systemd-analyze security --offline` of `oarfish-broker.service` (CI job `helper-units`; see A4.7) |

The fake `systemctl` exists twice: a Go stand-in for the runner (`fakeSys`, used for most rule tests) and a small test program (`newFakeSystemctl`, a shell script in a temporary directory) that records its arguments and environment, prints fixture output, exits with a chosen status or hangs; it runs through the production `runProgram`. The fixtures use the shapes observed in A2 (S7: systemd's own property order, `not-found` with exit code 0) and throwaway names only. The third column of `list-unit-files` (S7) is not read by the broker; it belongs to the service's read path (G7.4).

### A4.7 What is not verified

- Everything on a device: the production broker under the sandbox of B8, `systemctl` behind the shapes, the real `SO_PEERCRED` of the service user, the update check against a real helper, a real reboot and shutdown, the cold-boot order, memory (BF31). Class D and T rows stay open for G7.6.
- `systemd-analyze security --offline` is printed by CI in the job `helper-units` for information; it is a property of the unit text, not of a running process. Recorded from the CI run of the G7.2 pull request (#57, systemd 255 of the runner): `oarfish-broker.service` **0.9 SAFE**, at or below the target of B8 (the helper's 1.7); the helper and recover services 1.7 OK in the same run. G7.1 observed 0.9 SAFE on the device for the same text (A2.3).
- `RefuseManualStart=yes` (assumption of A3.4 rule 9, tested only against the fake).
- Ubuntu Server (G12).

### A4.8 What stays open

- G7.3 (packaging: bundle, component set, helper change, installer, budget value 80,000) has not started and is not prepared by G7.2. (Pointer, G7.3a: this bullet is stale. G7.3 is split into G7.3a and G7.3b. G7.3a implements the helper change, the bundle shape, `relverify`, `bundlegen`, the installer's store of the broker files below `components/<id>` and the budget value 80,000, stated in [ADR 0010 Amendment A20](0010-update-helper.md#amendment-a20-g73a); the broker's links, the wants link and start of its socket, its uninstall and its health check in the installer are G7.3b.) (Pointer, G7.3b: done, stated in [ADR 0010 Amendment A21](0010-update-helper.md#amendment-a21-g73b); D-C90 accepted G7.3a.)
- The service side (read path R1, routes, audit entries, the Activity page) is G7.4; reboot and shutdown in the UI are G7.5. (Pointer, G7.4: implemented as class C, stated in [amendment A5](#amendment-a5-g74-service-side-and-activity-page-as-implemented); G7.5 has not started.) (Pointer, G7.5: reboot and shutdown are implemented as class C, stated in [amendment A6](#amendment-a6-g75-reboot-and-shutdown-as-implemented).)

## Amendment A5 (G7.4 service side and Activity page as implemented)

**A5 (G7.4, written 2026-11-25).** Step G7.4 implements the service side of B10: the read path R1 of B2 as amended by A3.2, a client of the broker's socket for the three service operations of A4.3, the routes of B4 for services, the action job, the service values of ADR 0009 amendment A7 in the audit package, and the page Aktivität with the service list and start, stop and restart. This amendment edits no sentence of the body or of A1 to A4, except one pointer sentence in A4.8. **Deviations from the body and A1 to A4: none.** Where this ADR left a choice to the code, A5.6 names it. It decides nothing: every item of A5.6 that the owner wants as a rule needs a decision entry.

**A5 is class C evidence only.** The tests prove the argument shapes and environment of the two read shapes against a fake `systemctl`, the parsers against fixture output and hostile output, the classification against the broker's own lists, the protocol against a scripted server on a real Unix socket, the job, the routes, the audit mapping and the page in jsdom. They prove nothing about systemd, the broker or a browser. No device was touched in G7.4; the broker, the helper, `install/`, `scripts/` and the workflows are unchanged (the SHA-256 of the helper and the broker built at the base and at the head of the step are equal). Nothing is Verified, and the reference device stays Compatible.

### A5.1 Files and functions

| File | Content |
|---|---|
| `backend/internal/unitname/unitname.go` | `Valid` (rule 1 of B3, the broker's pattern of A4.4 item 1 as the constant `Pattern`), `IsTemplate`, `Template`; used by the route, the read path, the client and the audit target |
| `backend/internal/services/systemctl.go` | `SystemctlPath`, the two shapes `shapeListUnits` and `shapeListUnitFiles`, `allowedShape`, `systemctlEnv`, `runProgram` (the only place of the service that starts a process), `limitedBuffer` |
| `backend/internal/services/parse.go` | `parseListUnits`, `parseListUnitFiles`, the closed sets of load, active, sub and unit-file states, `cleanDescription` |
| `backend/internal/services/classify.go` | `denyList` and `restartOnly` (copies of the broker's lists of A3.4), `criticalKinds` (the table of rule 11), `classify`, `offeredActions`, `ValidAction` |
| `backend/internal/services/reader.go` | `reader` (single flight, the 5 s cache, `invalidate`), `readOnce`, `join`, `List`, `Row`, `Counts`, the codes `services_unavailable` and `services_read_failed` |
| `backend/internal/services/actions.go` | the job: `submit` (stat, busy, start), the broker call on its own context, one audit entry, `lastAction`, `close` |
| `backend/internal/services/services.go` | `Manager` (`List`, `BrokerAvailable`, `Submit`, `Last`, `Close`) |
| `backend/internal/brokerclient/client.go` | `SocketPath`, `Deadline` (110 s), `API`, `Client` (`Available`, `Do`), `request`, `decodeAnswer`, the closed sets `Codes` and `States` |
| `backend/internal/server/services.go` | `ServiceControl`, `WithServices`, the handlers of `GET /api/v1/services`, `POST /api/v1/services/action`, `GET /api/v1/services/action` |
| `backend/internal/audit/audit.go` | the actions `service_start`, `service_stop`, `service_restart`; the results of A7 for service operations; `ServiceResult`, `ServiceTarget`, `validServiceTarget` |
| `backend/cmd/oarfish/main.go` | `services.New` with `brokerclient.New(brokerclient.SocketPath)` and the audit log; `Close` runs before the audit log closes |
| `frontend/src/pages/Activity.svelte`, `lib/services.ts`, `lib/serviceErrors.ts`, `lib/services.fixtures.ts`, `dev/servicesMock.ts` | the page, its client with run-time shape checks, the sentences of the codes and the words of the closed sets, the fixtures and the development scenario mode ([`UI.md`](../UI.md#activity-page-step-g74)) |

### A5.2 The read path as built

- **Process.** `/usr/bin/systemctl`, no shell, exactly the two shapes of A3.2: `list-units --type=service --all --no-legend --no-pager --plain` and `list-unit-files --type=service --no-legend --no-pager`; environment exactly `PATH=/usr/sbin:/usr/bin:/sbin:/bin` and `LC_ALL=C`; standard input `/dev/null`; standard error discarded; working directory `/`; 5 s per call (then `SIGKILL`, and at most 1 s more for the output); at most 256 KiB of standard output kept. No `show`, no third shape, no `--output=json`. (Pointer, UF1: `list-unit-files` has its own deadline, store and single flight now; see [A9.2](#a92-the-read-path-as-it-now-holds).)
- **Bounds.** At most 1,000 lines per output and 1,000 rows; one read at a time (a caller that comes while one runs waits for it and gets its result); the result kept 5 s device-wide; the cache dropped when an action ends. No background work: the read runs only when a request asks for the list.
- **Parsing (A3.2).** `list-units`: four words separated by spaces and the rest of the line as the description. `list-unit-files`: exactly three words. Every name passes rule 1; every word is from its closed set (A5.6 items 2 to 4); a duplicated name, an empty line, a tab, a wrong word count or output over a bound fails the whole read (`services_read_failed`); nothing of a failed read is shown.
- **Rows.** The names of `list-units` without the `not-found` entries, joined with the start mode of `list-unit-files`; the names that only `list-unit-files` has, as inactive and not loaded; never an alias or a template. Sorted by name (byte order).
- **Descriptions.** Invalid UTF-8 replaced by U+FFFD, control characters and bidirectional format characters removed, trimmed, at most 200 characters; text only on the page.
- **Unavailable.** Without the directory `/run/systemd/system` or the file `/usr/bin/systemctl`, or when the program cannot be started, the answer is `services_unavailable` (503); every other failure is `services_read_failed` (503). Neither carries a path, an operating-system text or output. The service starts and runs without systemd (a development Mac, Generic mode).

### A5.3 The broker client as built

- One request per connection, `{"v":1,"op":"service.start|service.stop|service.restart","unit":"…","correlation_id":"<16 lowercase hex>"}` and a newline, at most 1,024 bytes; the member set of A4.3 and nothing else. No `status`, no power operation (G7.5).
- One answer line of at most 8 KiB, decoded strictly (A5.6 item 12). The 19 codes of B2 that a service operation can produce (`success`, `bad_request`, `busy`, `rate_limited`, `unit_invalid`, `unit_unknown`, `unit_not_canonical`, `unit_denied`, `unit_masked`, `unit_transient`, `unit_oneshot`, `unit_template`, `refused_by_unit`, `operation_not_offered`, `already_in_state`, `job_failed`, `timeout`, `state_unexpected`, `internal`) are kept; `accepted`, `update_running`, `already_requested` and any other word are `internal`. The service's own `unavailable`: the socket is missing, the connection is refused or reset, the broker closes without an answer, or no answer comes within 110 s.
- No retry: a request is sent at most once (a retry could run the operation twice).
- `Available` is a stat of `/run/oarfish-broker/broker.sock` and requires a socket; it never connects, so opening the page never starts the root process.

### A5.4 Routes and the job as built

- `GET /api/v1/services`: session, activity. `{"schema_version":1,"read_at":…,"broker_available":…,"counts":{"total","active","failed","inactive"},"services":[{"name","description","load_state","active_state","sub_state","start_mode","denied","restart_only","critical","actions"}]}`.
- `POST /api/v1/services/action`: session, activity, the three CSRF layers of ADR 0009 E3 unchanged, JSON only, body at most 1 KiB, exactly `unit` and `action`. Order: the body; rule 1 for `unit` and the closed set for `action` (both 400 `invalid_request`, not audited as a service action); the stat of the socket (503 `unavailable`, audited); one action at a time device-wide (409 `busy`, audited); else 202 `{"schema_version":1,"accepted":true}` and the job. No password (D-C87 item 7).
- `GET /api/v1/services/action`: session, activity, from memory: `{"schema_version":1,"action":null}` or the last action with `unit`, `action`, `step` (`running` or `done`), `started_at`, and when done `finished_at`, `code`, `state` and `retry_after_s` as the broker sent them. A restart of the service clears it.
- The job runs the broker call on its own context, not the request's: closing the page neither cancels nor un-audits it, and the server's 30 s write timeout is unchanged. The correlation ID is the server's value of the request, passed to the broker and into the audit entry. Without `WithServices` the three routes answer 503 `services_unavailable`.

### A5.5 Audit (ADR 0009 A7, the part of service actions)

Actions `service_start`, `service_stop`, `service_restart`, none aggregated; target `service/<unit>` where the unit passes rule 1 (so at most 255 bytes), else the event is dropped and counted; results `success`, `error`, `busy`, `rate_limited`, `unavailable` and the broker codes `unit_invalid`, `unit_unknown`, `unit_not_canonical`, `unit_denied`, `unit_masked`, `unit_transient`, `unit_oneshot`, `unit_template`, `refused_by_unit`, `operation_not_offered`, `already_in_state`, `job_failed`, `timeout`, `state_unexpected`; `bad_request` and `internal` are recorded as `error`. One entry per request that passed the syntax and closed-set checks, written when the answer is known (the job for an accepted request, the route for the service's own `busy` and `unavailable`), with the account name of the session at request time. The power part of A7 (`system_reboot`, `system_poweroff`, `update_running`, `already_requested`, the target `system`, the flush and `fsync`) is G7.5 and not in the code.

### A5.6 Choices the code made where this ADR did not fix one

1. **Rule 1 in the service.** The service uses the broker's pattern of A4.4 item 1 (the instance may be empty), so `name@.service` passes the route and the broker answers `unit_template`. Templates are never rows. `TestPatternMatchesTheBroker` keeps the two patterns equal.
2. **Sub states are a closed set.** The 27 sub states of a service unit in systemd up to version 257 (`dead`, `condition`, `start-pre`, `start`, `start-post`, `running`, `exited`, `reload`, `reload-signal`, `reload-notify`, `refresh-extensions`, `mounting`, `stop`, `stop-watchdog`, `stop-sigterm`, `stop-sigkill`, `stop-post`, `final-watchdog`, `final-sigterm`, `final-sigkill`, `failed`, `dead-before-auto-restart`, `failed-before-auto-restart`, `dead-resources-pinned`, `auto-restart`, `auto-restart-queued`, `cleaning`). The list is an *assumption* from systemd's documentation, not read from a device; an unknown sub state fails the whole read (A5.8).
3. **Load states** are the closed set `loaded`, `not-found`, `bad-setting`, `error`, `merged`, `masked`, `stub`; `not-found` rows are dropped.
4. **The vendor preset** (third column) must be `-` or 1 to 16 lowercase letters; it is not used.
5. **Rows only in `list-unit-files`** have the load state `not-loaded` (a word of the service, not of systemd), the active state `inactive`, the sub state `dead` and no description.
6. **Start mode of an instance** that `list-unit-files` does not name: the state of its template; otherwise `""` (the page says "Unbekannt"). `alias` is never a start mode.
7. **Restart-only rows offer restart only**, also when they are inactive. The broker would accept a start of them (it enforces only the stop part, A4.4 item 14); the page follows the owner's rule "only restarted" for every button. The rule for other rows: start when not `active`; stop when `active`, `activating` or `reloading`; restart always; nothing for a denied row.
8. **Critical kinds** are given for every row of the rule 11 table, also for the rows with "Stop offered" (`ModemManager.service`, containers, time).
9. **A failed read is cached** for 5 s like a successful one, so repeated clicks do not start more processes.
10. **Descriptions** lose bidirectional format characters (U+200E, U+200F, U+202A to U+202E, U+2066 to U+2069) besides control characters, so a description cannot reorder what is shown around it.
11. **systemd present** means the directory `/run/systemd/system` and the file `/usr/bin/systemctl` (stat); a program that cannot be started is `services_unavailable` too.
12. **Answer decoding.** `state` is accepted only with `success`, `job_failed`, `timeout` and `state_unexpected`; `retry_after_s` is required with `rate_limited` and refused with any other code, plain digits from 1 to 86,400; the answer must be exactly one line ending in a newline; an empty answer is `unavailable`; anything else is `internal`.
13. **The client refuses to build** a request for an operation outside the three, a unit that fails rule 1 or a correlation ID that is not 16 lowercase hexadecimal digits: `internal`, no connection.
14. **`Available` requires a socket** at the path, not only an entry.
15. **A refusal does not replace the last action**: `busy` and `unavailable` of the route leave the last action as it was.
16. **Shutdown.** When the service stops, the job's context ends; the client returns `unavailable` at once (the operation may continue inside systemd), the job writes its audit entry, and the audit log closes after that.
17. **HTTP status codes:** 202 accepted, 400 `invalid_request`, 409 `busy`, 413 `request_too_large`, 503 `unavailable`, `services_unavailable` and `services_read_failed`.
18. **The page** reads the last action once on opening and follows an action that runs; after an action it reads the list once; a refused request other than `busy` reads the list once as well (the socket may be gone).
19. **Texts.** The sentences of B2 are used as written, except two that the text rules of `docs/UI.md` forbid ("bitte"): `busy` reads "Eine andere Aktion läuft noch. Warten Sie, bis sie beendet ist." and `rate_limited` reads "Zu viele Aktionen in kurzer Zeit. Eine neue Aktion ist in {duration} möglich." The meaning is unchanged (B2 allows the wording to change at the UI step). The route's own `invalid_request` has the sentence of the update page's request refusal. Every user text of G7.4 is a proposal for the owner's review.

### A5.7 Class C tests per row of the failure matrix

Expected results as amended by A3.7 and A4.1. Service side only; the broker side stays as A4.6.

| Row | Tests (service side) |
|---|---|
| BF13 | `TestJobAuditMapping` (`job_failed` kept with its state and audited), page "says every result code as its own sentence" |
| BF14 | `TestJobAuditMapping` (`timeout`), `TestUnavailableAndNoRetry` (no answer within the client deadline is `unavailable`, sent once), page scenario `action-timeout` |
| BF15 | `TestJobAuditMapping` (`state_unexpected`), page "says every result code as its own sentence" |
| BF16 (class C part) | `TestJobSuccess`, `TestServicesActionIsAJobAuditedOnce` (one entry with user, action, target, result and the request's correlation ID), page "confirms with one click …" (the success sentence says that a later failure is not checked) |
| BF17 | `TestJobBusy`, `TestServicesActionIsAJobAuditedOnce`, `TestServicesActionSubmit`, page "answers of the action route itself" |
| BF19 | `TestJobUnavailable`, `TestUnavailableAndNoRetry`, `TestAvailableIsStatOnly`, `TestServicesActionSubmit`, page "state broker unavailable" |
| BF21 (service side) | `TestParseListUnits`, `TestParseListUnitsFailsClosed`, `TestParseListUnitFiles`, `TestParseListUnitFilesFailsClosed`, `TestCleanDescription`, `TestHostileListUnits`, `TestRunProgramBound`, `TestReadFailed`, `TestJoinBound`, `FuzzParseListUnits`, `FuzzParseListUnitFiles`, page "shows names, descriptions and unknown words as text only" |
| BF22 | `TestServicesRoutesNeedASessionAndTheCSRFLayers` (refused, nothing reaches the job, audited `csrf_failed` and `origin_rejected`) |
| BF29 (service side) | `TestRequestRefusedBeforeSending`, `TestNoOtherOperation`, `TestPowerOnlyAndUnknownCodesAreInternal`, `TestListsMatchTheBroker`, `TestServicesActionBody`; the broker side of BF29 is A4.6 |

Besides the matrix: `TestShapes`, `TestRunProgram`, `TestRunProgramDeadline`, `TestOnlySystemctlExecutes`, `TestImports`, `TestCache`, `TestInvalidate`, `TestSingleFlight`, `TestCloseEndsARead`, `TestReadUnavailable`, `TestDenyClassification`, `TestRestartOnlyClassification`, `TestRestartOnlyIsTheNoRowsOfRule11`, `TestCriticalKindsAndActions`, `TestJobOwnContextAndClose`, `TestManagerDropsCacheAfterAction`, `TestServicesLastAction`, `TestServicesRoutesAreActivity`, `TestServicesRoutesWithoutTheOption`, `TestWriteTimeoutUntouched`, `TestServicesRedaction`, `TestClosedSetsAfterA7ServicePart`, `TestServiceResultMapping`, `TestServiceTargetPattern`, `TestServiceEventsAreWrittenOrDropped`, `TestArchitectureNoProductCodeRunsAProgram` (extended by the one file of the read path), and the page tests named in [`UI.md`](../UI.md#activity-page-step-g74). A mutation run weakened, one at a time, the restart-only list, the deny list, the target pattern, the 1 KiB body limit, the busy slot, the mapping of power-only codes, the no-retry rule, the stat-only socket check and the single process-start file; each turned at least one of these tests red.

### A5.8 What is not verified, and what stays open

- **Not verified:** the read shapes under the service's sandbox on a device as part of the product (G7.1 observed them once with a throwaway copy, A2.3 A-02; the transport is the D-Bus system bus, S9); the time of `list-unit-files` against the 5 s bound on a device (2.6 to 2.7 s observed in G7.1, S6); the real broker behind the client; the memory of a list read; the page in any real browser, with a screen reader, in forced colours and at 320 CSS pixels; Ubuntu Server (G12). Every class D and T row stays open for G7.6.
- **Open: the unit type on the read path.** The two shapes do not tell a one-shot unit, and a transient unit only by its unit-file state; the page shows buttons for them and the broker refuses (`unit_oneshot`, `unit_transient`), with the sentence of the code. No change inside the two shapes can tell the type. A proposal outside R1 as decided: a third read shape `systemctl show --property=Id,Type,Transient --no-pager -- <names…>` for the loaded names (an owner decision, it changes R1 and D-C87 item 3); not implemented.
- **Open: the sub-state set.** An unknown sub state from a newer systemd fails the whole read (A5.6 item 2). If the owner prefers, the sub state could be checked by a pattern as the broker checks `SubState` (A4.4 item 4) and shown neutrally; not implemented.
- **Open: the page's texts and name.** Every user text of G7.4 and the name Aktivität / Activity are proposals for the owner's review (B10 stop point of G7.4).
- **Not started:** G7.5 (reboot, shutdown, the power routes, the password for them, the waiting actions and sessions, the power part of ADR 0009 A7). (Pointer, G7.5: implemented as class C, stated in [amendment A6](#amendment-a6-g75-reboot-and-shutdown-as-implemented).)

## Amendment A6 (G7.5 reboot and shutdown as implemented)

**A6 (G7.5, written 2026-11-25).** Step G7.5 implements reboot and shutdown of B10: the two power operations in the broker client, the routes `GET /api/v1/power` and `POST /api/v1/power` of B4 with the password through the shared limiter (B4, D-C87 item 7) and the list of waiting actions (B6, ORB-PWR-002 as D-C87 item 11 changed it), the power values of [ADR 0009 amendment A7](0009-local-access-control-and-audit.md) with the flush and `fsync` before the answer (B5), and on the page Aktivität the card, the confirmation dialog and the status view during and after the action (B6). This amendment edits no sentence of the body or of A1 to A5, except one pointer sentence in A4.8 and one in A5.8. Where this ADR left a choice to the code, A6.6 names it. It decides nothing: every item of A6.6 that the owner wants as a rule needs a decision entry.

**Deviations from the body and A1 to A5: one, in the texts.** The table of B6 has "Das Gerät ist wieder erreichbar. Bitte melden Sie sich an." and "Please sign in."; the text rules of [`docs/UI.md`](../UI.md#text-rules) and `i18n.test.ts` forbid "bitte" and "please". The page says "Das Gerät ist wieder erreichbar. Melden Sie sich an." and "The device can be reached again. Sign in." (A6.6 item 16). The meaning is unchanged, and B2 allows the wording to change at the UI step; it is listed here because the prompt of G7.5 names it a deviation.

**A6 is class C evidence only.** The tests prove the request shapes and the decoding against a scripted server on a real Unix socket, the order of the checks, the mapping of every code to an HTTP status, a sentence and an audit result, the order write, flush and `fsync` against a fake file, the waiting list against fakes of the update job and the helper, and the page, the dialog and the status view in jsdom. They prove nothing about the broker, systemd, a browser or a device. No device was touched in G7.5, no real reboot or shutdown was made (that is G7.6, class D); the broker, the helper, `install/`, `scripts/` and the workflows are unchanged (the SHA-256 of the helper and the broker built at the base and at the head of the step are equal). Nothing is Verified, and the reference device stays Compatible.

### A6.1 Files and functions

| File | Content |
|---|---|
| `backend/internal/brokerclient/client.go` | `PowerOp` (`OpReboot`, `OpPoweroff`), `PowerDeadline` (20 s), `PowerCodes`, `PowerAPI`, `Client.Power`, `powerRequest`, `decodePowerAnswer`; `exchange`, `answerMembers` and `codeIn` shared with the service operations |
| `backend/internal/audit/audit.go` | the actions `system_reboot`, `system_poweroff`; the target `system`; the results `update_running`, `already_requested`; `PowerResult`; `RecordDurable`, `flush(sync)`, `write(data, sync)`, the file seam `openFile` (tests only replace it) |
| `backend/internal/updater/power.go` | `Activity` (`Staged`, `Running`) and `Updater.Activity`: the in-memory job and the helper's `status`, without an audit entry or an acknowledgement |
| `backend/internal/services/services.go`, `actions.go` | `Manager.ActionRunning` |
| `backend/internal/server/power.go` | `PowerConfig`, `WithPower`, `UpdateActivity`, `ServiceActivity`, the handlers `waiting` (`GET /api/v1/power`) and `request` (`POST /api/v1/power`), `powerError` |
| `backend/cmd/oarfish/main.go` | one broker client for services and power; `WithPower` with the updater, the services manager and `Sampler.Clients` |
| `frontend/src/pages/ActivityPower.svelte`, `pages/PowerStatus.svelte`, `lib/power.ts`, `lib/powerErrors.ts`, `lib/power.fixtures.ts`, `dev/servicesMock.ts`, `App.svelte`, `lib/session.ts` | the card and the dialog, the status view, the client with run-time shape checks, the sentences, the fixtures, the power scenarios, the switch to the status view, and a timeout parameter of `postJSON` ([`UI.md`](../UI.md#restart-and-shutdown-step-g75)) |

### A6.2 The broker client as built

- One request per connection, `{"v":1,"op":"system.reboot|system.poweroff","correlation_id":"<16 lowercase hex>"}` and a newline: the member set of A4.3, no `unit`. An operation outside the two or a bad correlation ID is `internal` and nothing is sent.
- The answer is decoded strictly with exactly the members `v` (the literal 1) and `code`. The closed set `PowerCodes` is what `powerOp` and the decoder of the broker can answer (A4.4 item 7 and 9): `accepted`, `bad_request`, `busy`, `update_running`, `already_requested`, `job_failed`, `timeout`, `internal`. A service-only code (`success`, `rate_limited`, the `unit_*` codes, `refused_by_unit`, `operation_not_offered`, `already_in_state`, `state_unexpected`), a `state` or `retry_after_s` member and any other word are `internal`. The power-only codes stay `internal` for a service operation (G7.4).
- No retry. No answer within `PowerDeadline` (20 s), a reset, a refused connection, a missing socket or a cancelled context is the service's own `unavailable`. There is still no `status` operation of the broker, and `Available` is still a stat only.

### A6.3 The routes as built

- `GET /api/v1/power`: session, activity, no body. `{"schema_version":1,"broker_available":…,"waiting":[{"kind":…,"count":…}]}` in the fixed order `update_staged`, `update_running`, `service_action_running`, `other_sessions` (count), `other_views` (count); an empty list is `[]`. No broker connection, no `systemctl`, no password.
- `POST /api/v1/power`: session, activity, the three CSRF layers of ADR 0009 E3 unchanged, JSON only, body at most 1 KiB, members `action` and `current_password`. The order: the body (413 `request_too_large`, 400 `invalid_request`); the closed set `reboot`, `poweroff` (400 `invalid_request`, not audited as a power action, the password is not checked); `ConfirmPassword` through the shared limiter (403 `wrong_password`, 429 `rate_limited` with `retry_after_seconds` and `Retry-After`, both audited, exactly the handling of the update route's password step, `accountError`); the stat of the socket (503 `unavailable`, audited); the update (409 `update_running`, audited); a running service action (409 `busy`, audited); then the broker. A wrong or limited password never reaches any of the later steps.
- The broker's answer: `accepted` is one entry `success`, written with `RecordDurable` (flush and `fsync`), then 200 `{"schema_version":1,"accepted":true}`. Every other code is one entry with `PowerResult(code)` and a typed error: 409 `busy`, `update_running`, `already_requested`; 503 `unavailable`; 502 `job_failed`, `timeout`, `bad_request`, `internal` (an unknown code is `internal`). Without `WithPower` both routes answer 503 `power_unavailable` after the session check.

### A6.4 Audit (ADR 0009 A7, the power part)

Actions `system_reboot`, `system_poweroff`, none aggregated; target `system`; results `success` (for `accepted`), `wrong_password`, `rate_limited`, `update_running`, `already_requested`, `busy`, `unavailable`, `job_failed`, `timeout`, and `error` for `bad_request`, `internal` and anything else. One entry per request that passed the syntax and closed-set checks, with the account name of the session; never the password, a body or an error text. `RecordDurable` adds the entry, writes the buffer and fsyncs `audit.jsonl` before it returns; the directory sync at rotation and the flush on SIGTERM are unchanged. The action, target and result sets stay closed; `TestClosedSetsAfterA7` pins them, updated on purpose.

### A6.5 The page as built

The card, the dialog and the status view are described in [`UI.md`](../UI.md#restart-and-shutdown-step-g75), with every state and its test, and the scenarios in [`DEVELOPMENT.md`](../DEVELOPMENT.md#reboot-and-shutdown-scenarios-gate-g7-step-g75).

### A6.6 Choices the code made where this ADR did not fix one

1. **Deadline 20 s** for a power request (the prompt of G7.5). The broker's own deadline for `systemctl reboot` is 100 s (A4.4 item 7), so its `timeout` cannot reach the service: a `systemctl` that hangs becomes the service's `unavailable` after 20 s, audited as `unavailable`, while the broker may still hold its busy flag and the job may still be queued.
2. **The answer comes inside the request**, not from a job: the broker answers once systemd has queued the job, about a second (G7.1 observed the device going down 1 to 1.5 s after the start, A2.1 S10). The bound is the hash (about 0.5 s on the reference device), the update read (at most 3 s, item 4) and the broker (20 s), inside the server's write timeout of 30 s, which is unchanged. The page waits 28 s (`powerRequestTimeoutMs`; `postJSON` gains a timeout parameter, 20 s by default as before).
3. **Order of the checks** as in A6.3: everything that can refuse without the password is syntax only, so a password guess learns nothing about the device's state; the password comes before the socket, the update and the service action.
4. **The update source of the waiting list and of `update_running`** is `Updater.Activity`: the in-memory job (an apply job that waits for the second confirmation is `update_staged`; any other step of a running apply job, the download included, is `update_running`) and the helper's `status`, the same read that `GET /api/v1/update` makes, bounded to 3 s, without the reconcile, the audit entry `update_finished` or the acknowledgement that `State` does. Phase `staged` is `update_staged`; a phase outside `idle`, `staged`, `committed`, `rolled_back`, `rollback_failed` is `update_running` (the broker's set, A4.4 item 8); a helper that cannot be reached is `update_running` only within the hand-over rule of ADR 0010 A17 F4, otherwise it adds nothing and the broker decides. So opening the card and opening the dialog each make one connection to the helper's socket, which starts the root helper by socket activation, as opening the update page does.
5. **Other sessions** are `Sessions.Len` minus one, shown when at least 1. `Len` counts expired sessions until they are swept, so the number can be too high by the sessions that expired since the last sweep.
6. **Live views exist**: the event stream `GET /api/v1/events` of the Dashboard, counted with `Sampler.Clients`. A Dashboard tab that is hidden closes its stream and is not counted; a Dashboard tab of the caller's own session is counted, because the stream has no session tie in the sampler.
7. **HTTP status codes** of A6.3. 200, not 202, for `accepted`, because no job runs in the service; 502 for the broker's failures that the person cannot change.
8. **The broker call does not end with the request**: it runs on `context.WithoutCancel` of the request, bounded by `PowerDeadline`, so a browser that closes after the password cannot leave a queued operation without its audit entry.
9. **A failed write or `fsync`** of the accepted entry is counted in `Stats().Failures` (not in `Dropped`: the line may be on the disk without the sync) and reported to the journal as "audit: syncing the audit file failed (n failed writes since start)" without data; the answer stays `accepted`, because systemd has already queued the job. `RecordDurable` also opens and syncs the file when a concurrent flush has taken the entry, and refuses an aggregated action.
10. **No power flag in the service.** A repeated request after `accepted` goes through every check again and to the broker, which answers `already_requested` (BF28).
11. **The password** is overwritten in the request's variable right after the check. A Go string cannot be wiped, so its bytes stay in memory until the garbage collector reuses them (residual, as for login and the update route). A client that leaves during the hash gets no answer and no entry (the existing `accountError` path).
12. **The body decoder** is the one of the update and services routes: unknown member names are refused, but `encoding/json` matches a known name in another letter case and takes the last of two equal members. Not changed in this step (finding of the report).
13. **The dialog** is a native `dialog` opened with `showModal()` (the first modal of the project; `UI.md` had no rule). The focus goes to its title, back to the opening button on closing; Escape and the browser's `cancel` close it unless a request runs. Without `showModal` the `open` attribute is set (no focus trap then). The password is read from the input element at submit and kept in no variable.
14. **The status view** replaces the shell (`App.svelte`), not only the page content, so the navigation, the user menu and the version line are gone too. The first poll is 5 s after `accepted`, each poll gives up after 4 s. Restart: the polling stops at the first answer after a failure; "antwortet noch nicht" is measured from the last answer (or the start of the view) and the polling goes on after it. Shutdown: the polling stops at the first failure (nothing more to learn). The link to the sign-in is a new load of the start page with the address's query.
15. **The development mock** answers the power routes and `GET /healthz` in every scenario of `servicesMock.ts`, so a power scenario never sends a request to the development server's proxy.
16. **Texts.** The table of B6 is used as written except the "Bitte" sentence (the deviation above). `unavailable` on the power card reads "Der Privilege Broker ist nicht erreichbar. Neustart und Herunterfahren sind nicht möglich." instead of B2's "Die Liste bleibt sichtbar, Aktionen sind nicht möglich." (the list is not about power). `busy` uses the wording of G7.4 (A5.6 item 19). `job_failed` and `timeout` of B2 carry a state word that a power operation does not have, so they have their own sentences. The waiting list, the card, the status view titles and the link are new texts. Every user text of G7.5 is a proposal for the owner's review; the shutdown sentence about the power supply is the same for every board until a hardware profile says otherwise (A-07, an owner text decision).
17. **Icon.** The card has a new 24 by 24 outline icon `power` in `lib/icons.ts`.

### A6.7 Class C tests per row of the failure matrix

Expected results as amended by A3.7 and A4.1. Service side only; the broker side stays as A4.6.

| Row | Tests (service side) |
|---|---|
| BF22 (power routes) | `TestPowerRoutesNeedASessionAndTheCSRFLayers` |
| BF23 | `TestPowerWrongPasswordNeverReachesTheBroker` (five `wrong_password`, then `rate_limited`, also for the right password while limited; zero calls to the update check, the service check and the broker; each audited), page "every refusal is one sentence …", scenarios `power-wrong-password`, `power-rate-limited` |
| BF24 (service side) | `TestPowerOrderOfChecks` (update running from the service source: 409, audited, the broker not called), `TestPowerBrokerCodes` (`update_running` from the broker), `TestActivity` (the steps and the transaction phases), scenario `power-update-running` |
| BF25 (service side) | `TestActivity` (`c_switched` and `c_committed` are a running update; the helper unreachable within the hand-over rule), `TestPowerBrokerCodes` (`update_running` from the broker) |
| BF26, BF27 (class C part) | `TestPowerAcceptedIsWrittenBeforeTheAnswer`, `TestRecordDurableWritesThenSyncs`, `TestRecordDurableSyncsAnEmptyBuffer`, `TestRecordDurableSyncFailureIsCounted`, `TestRecordDurableRealFile`, `TestPowerAuditFailureKeepsAccepted`, `TestPowerBrokerCallOutlivesTheRequest`, the status view tests of `pages/ActivityPower.test.ts` |
| BF28 (service side) | `TestPowerRepeatedRequestGoesToTheBroker`, `TestPowerBrokerCodes` (`already_requested`), scenario `power-already-requested` |
| BF29 (service side) | `TestPowerRequestShape`, `TestPowerRequestRefusedBeforeSending`, `TestNoOtherOperation`, `TestEveryPowerCode`, `TestServiceOnlyAndUnknownCodesAreInternalForPower`, `TestMalformedPowerAnswersAreInternal`, `TestPowerBody` (the closed set of actions) |

Besides the matrix: `TestPowerUnavailableAndNoRetry`, `TestPowerWaiting`, `TestPowerOtherSessionsCount`, `TestPowerRoutesAreActivity`, `TestPowerRoutesWithoutTheOption`, `TestPowerRedaction` (a marker password in no answer, audit line or broker call in ten paths), `TestPowerResultMapping`, `TestPowerTargetVectors`, `TestRecordDurableWriteFailureIsCounted`, `TestRecordDurableRefusals`, `TestClosedSetsAfterA7`, `TestActivityWithoutHelper`, `TestActionRunning`, and the client, page and scenario tests named in [`UI.md`](../UI.md#restart-and-shutdown-step-g75). A mutation run weakened, one at a time: the password check removed, the password check moved after the broker call, a broker call on a wrong password, the `fsync` removed, the flush removed, the audit entry written before the broker answered, the 1 KiB body limit, the closed set of power codes (a service-only code accepted), the mapping of `update_running`, the other-sessions count (the caller not subtracted), the status view sending a request other than `/healthz`, and the dialog not clearing the password; each turned at least one of these tests red.

### A6.8 What is not verified, and what stays open

- **Not verified:** the real broker behind the client (its answers for the power operations are class C at the broker side, A4.6); a real reboot or shutdown and the page during and after it (BF26, BF27, G7.6, class D); the update check against a real helper during a transaction (BF24, class T); the `fsync` on the reference device's microSD; the timing of the 20 s deadline and the 28 s page timeout against a real device; the page in any real browser, the native dialog's focus trap and inert background, Safari, Firefox, a screen reader, forced colours; Ubuntu Server (G12).
- **Open: a request that systemd accepted but the answer did not reach.** When the broker answers later than 20 s, or the connection breaks after systemd queued the job, the service answers and audits `unavailable` while the device goes down; the page then shows the `unavailable` sentence in the dialog instead of the status view.
- **Open: an accepted restart that never happens.** If the device keeps answering after `accepted`, the status view says "Das Gerät startet neu …" without end; there is no limit for that case.
- **Open: the waiting list is a convenience, not exact** (A6.6 items 4 to 6): the helper read can fail, expired sessions count until swept, and the caller's own Dashboard stream counts as a live view.
- **Not started:** G7.6 (release with one installer run per device, and the device evidence of the failure matrix) and the release check before the first broker release (R-check). (Pointer, R-check: the release checks of D-C92 and of the two-step rule are built as class C and run in the release workflow before signing, [`RELEASING.md`](../RELEASING.md#release-checks-d-c92-and-the-two-step-rule); the choices of G7.4 and G7.5 are recorded as D-C93 and D-C94. G7.6 is split into G7.6a (release texts, text only, done in its pull request), G7.6b (the release), G7.6c (device evidence) and G7.6d (the closing review); the first run of the checks on a real tag is G7.6b, and G7.6b to G7.6d have not started.) (Pointer, G7.6: G7.6b and G7.6c are done; the release `v0.1.0-alpha.13` and the device evidence are recorded in [amendment A7](#amendment-a7-g76-release-and-device-evidence).)

## Amendment A7 (G7.6 release and device evidence)

**A7 (G7.6b and G7.6c, written 2026-11-26).** This amendment records the first release that carries the broker and what was observed on the reference device with it. **It takes no decision.** It edits no sentence of the body or of A1 to A6, except one pointer sentence in A6.8; where it names an expected result, the expected result is the one of the [failure matrix](#failure-matrix) as amended by A3.7 and A4.1. `docs/DECISIONS.md` is not changed by it. Whether G7 passes is the owner's decision (D-C87 item 15: BF16, BF26 and BF27 as class D).

**Evidence label.** Every device result below is a *device observation (G7.6c), production release `v0.1.0-alpha.13`, one device, one image, one moment, 2026-11-26*. Class D in the sense of B9: the production binaries of a release signed with the production key, real services, a real reboot and a real shutdown. No class T row was run: there was no test build and no test unit. Nothing is Verified, and the support status of the reference device stays Compatible. The raw outputs stay outside this repository. Device values (host name, user name, address, serial number, boot id, partition identifiers) are left out.

### A7.1 The release (G7.6b)

- **Tag and commit.** `v0.1.0-alpha.13`, an annotated tag on `7babf046074e5b09d268e2830e86bcfc9b2ba734` (the merge of PR #64 on `main`), created after the owner confirmed the name in chat. CI on that commit was green before the tag was pushed. Every job of the release workflow passed (CI checks, build, sign, publish).
- **The bundle.** The components shape with the broker: 12 tarball entries, that is the root directory and 11 files; the `contents` of `manifest.json` list the 11 files, among them `oarfish-broker` (0755, SHA-256 `f18712cd3a42a6a8ba10987a925cc31678954e07d1f6e469abb170326ba70551`), `oarfish-broker.service` (`cc2bf150…`) and `oarfish-broker.socket` (`e0061c37…`). `components.key_id` is `11f1c52777090bfc`, the key id of `release/oarfish-release.pub`; `components.protocol` is 1.
- **Compared with `v0.1.0-alpha.12`** (procedure item 3 of [`RELEASING.md`](../RELEASING.md#release-procedure)): `oarfish-update-helper` differs (`0c6f95d2…` against `059c73f0…`), the three broker files are new, and the three helper units and `oarfish.service` are identical. No unit of the component set changed, so the two-step rule (item 4) did not apply.
- **The first real run of the two release checks** (the job `build`, before signing). Both passed, as the local run before the tag did:
  - `component-set: v0.1.0-alpha.13: set sha256 887757cffa9e4046d39fc56e5c476eb898cb19ba4f7e89aded8585650590d219`; `v0.1.0-alpha.1` to `v0.1.0-alpha.6` skipped as legacy; `v0.1.0-alpha.7` to `v0.1.0-alpha.12` skipped as four-file sets; `directly preceding release: v0.1.0-alpha.12`; `PASS: no earlier release carries a seven-file set; nothing to compare`. The set hash of the published release equals the one of a local build of the same commit.
  - `unit-forms`: the four units that the helper of `v0.1.0-alpha.12` lints were each `accepted by the unit lint of the helper of v0.1.0-alpha.12`; `oarfish-broker.service` and `oarfish-broker.socket` were each `not linted by the helper of v0.1.0-alpha.12 (not a unit of its component set), not judged`; `PASS`.
- `release/release-exceptions.txt` held no active line; none was added.
- **Notes.** The section "Release notes for the first broker release" of `RELEASING.md` with the tag filled in, English and German, plus a paragraph per language that names the files that differ from `v0.1.0-alpha.12` (procedure item 3). Appended with `gh release edit`; no asset was touched.
- **Anonymous verification** (no token): `scripts/verify-release.sh` printed `VERIFIED`; the trusted comment is `oarfish v0.1.0-alpha.13 7babf046074e5b09d268e2830e86bcfc9b2ba734`; the stamp in `install.sh` is `0.1.0-alpha.13`; the commit in `manifest.json` is the tag's commit.

### A7.2 Device and method

- **Device.** The reference device of A2.2: one Raspberry Pi Zero 2 W, Debian 13 (trixie) aarch64, kernel `6.18.50+rpt-rpi-v8`, systemd 257 (`257.13-1~deb13u1`), 425,116 kB `MemTotal`, journald volatile, no real-time clock. Before the run it ran `v0.1.0-alpha.12` (installed and updated through the GUI in G6.10), 0 failed units, boot time 42.091 s.
- **Method.** The implementation agent ran every check over SSH with `sudo`, one step at a time, under the owner's approval in chat for this session. Each step that changed the device (installer run, socket stop and start, the restart of a service, reboot, shutdown) was announced and waited for the owner's "ok". Every step that needed the Oarfish password was made by the owner in a real browser (through an SSH tunnel to `127.0.0.1:8484`); the agent never handled the password. Before the reboot and the shutdown the broker's and the service's journal lines and the audit tail were saved, and a live `journalctl -f` recorded the broker's lines until the link dropped. The owner restored power with a smart plug after the shutdown. No Oarfish or system file was edited by hand.

### A7.3 Evidence per row of the failure matrix

| Row | Class reached | Result | Evidence (device observation, G7.6c) |
|---|---|---|---|
| BF16 Restart success | **D** | as expected | The owner restarted `cron.service` on the page Aktivität; the page said "Neustart für cron.service. Die Aktion wurde ausgeführt. Zustand jetzt: aktiv. Ein späterer Ausfall wird nicht geprüft." The broker journal: `op=service.restart unit=cron.service cid=87b4de8ca9e5e103 result=success`. `MainPID` changed and `ExecMainStartTimestamp` moved to the time of the request; `ActiveState=active`. The audit: one entry `service_restart`, target `service/cron.service`, result `success`, the account name, correlation ID `87b4de8ca9e5e103`. |
| BF26 Reboot success | **D** | as expected | The owner rebooted from the page with the password. The audit entry `system_reboot`, target `system`, result `success`, correlation ID `fcba7b6a8f4307dd`, written before the reboot, is present after the boot (the flush and `fsync` of D-C94). The boot id changed; `/healthz` answered `0.1.0-alpha.13`; `oarfish-broker.socket` active (listening), the broker service inactive; 0 failed units; 0 lines "ordering cycle" in the boot journal; boot time 41.000 s. The texts of the status view were not reported (the owner did not note them). |
| BF27 Shutdown success | **D** | as expected | The owner shut down from the page with the password. The status view showed "Herunterfahren angefordert", "Das Gerät antwortet nicht mehr. Nach dem Herunterfahren ist das erwartet." and the sentence about the SSH tunnel (screenshot of the owner). The SSH connections closed in the same second as the audit entry. The owner switched the smart plug off and on about 1 to 2 minutes later; the boot began about 2 min 52 s after the shutdown (from the uptime). After the boot: the audit entry `system_poweroff`, target `system`, result `success`, correlation ID `d6b980e9fc706d78`; new boot id; broker socket active; 0 failed units; 0 cycle lines; boot time 40.952 s; after a reload the page showed the sign-in. |
| BF12 Restart only | **D** (page) | as expected | The row `ssh.service` offered only "Neustart" under "Aktionen", with the note "Nur Neustart. Diesen Dienst können Sie nur neu starten. Ein Stopp würde den Zugang zum Gerät trennen." and, in the action panel, "Dieser Dienst ermöglicht den Zugang per SSH. Ein Neustart trennt bestehende SSH-Verbindungen, auch einen SSH-Tunnel zu Oarfish." No restart was made. The broker's answer `operation_not_offered` was not requested (no raw socket request); it stays class C. |
| BF19 Socket stopped | **D** | as expected | With `oarfish-broker.socket` stopped, the page showed "Der Privilege Broker ist nicht erreichbar. Die Liste bleibt sichtbar, Aktionen sind nicht möglich."; the list stayed visible, no action button could be used. The socket was started again and was active (listening). |
| BF23 Reboot, wrong password | **D** | as expected | Seven wrong passwords in the reboot dialog: seven audit entries `system_reboot` with result `wrong_password`, then one with `rate_limited`. The page said "Das aktuelle Passwort ist falsch." and then "Zu viele Versuche. Warten Sie 3 Sekunden und versuchen Sie es dann erneut." The broker journal shows no start and no request in that time. |
| BF30 Boot transaction | **D** | as expected | `systemd-analyze verify` of the two installed broker units: exit 0, no output. 0 lines "ordering cycle" in the journal of the boot after the reboot and of the boot after the power-on; 0 failed units each time. |
| BF31 Memory and idle exit | **D** | recorded | `VmHWM` of the broker process (read from its own `/proc` status, so without its `systemctl` child) 4,552 kB after the restart of BF16; the service at the same time 31,012 kB `VmHWM`, 15,572 kB `VmRSS`. Idle exit: the broker started at the request and exited with status 0 59 s later, when the 60 s rate window emptied (A4.4 item 12); the socket stayed active. The `systemctl` child was not measured. |
| BF32 Hardening | **D** | as expected | `systemd-analyze security`: `oarfish-broker.service` 0.9 SAFE, `oarfish-update-helper.service` 1.7 OK, `oarfish.service` 1.7 OK. The remaining deductions of the broker are those of A2.3. |
| BF28 Second power request | C only | not run | The device went down within about 1 s of `accepted`; a second request in the same boot could not be made safely by hand without a second reboot. |
| BF1, BF5, BF6, BF17, BF18, BF29 | C only | not run | Each needs requests sent directly to the socket, two parallel actions or eleven requests in 60 s; this session sent no raw socket request. |
| BF4, BF7 to BF11, BF13, BF14, BF20, BF24 | C only | not run | Class T: no test build and no test unit in this session. |
| BF2, BF3, BF15, BF21, BF22, BF25 | C | not applicable | Class C only by the matrix. |

### A7.4 The open device checks of A4 to A6

- **The installer run with the broker** (ADR 0010 A21, [`INSTALL.md`](../INSTALL.md#results-on-the-reference-device-g76c)): observed with the official installer of `v0.1.0-alpha.13` over a device of `v0.1.0-alpha.12`, class D.
- **The read path R1 under the service's real sandbox** (A5.8): the page Aktivität loaded the list, 156 services, no error. A root `list-units --all` at the same time listed 112 loaded units (41 active, 71 inactive); the rest are rows from `list-unit-files` only (A5.6 item 5). The time of `list-unit-files` under the sandbox was not measured.
- **The real broker behind the client** (A5.8, A6.8): BF16, BF19, BF26 and BF27 above.
- **The `fsync` of the power entry on the microSD** (A6.8): both power entries survived the reboot and the power cut.
- **The `SO_PEERCRED` of the service user** (A4.7): the service's requests were accepted (BF16, BF26, BF27); a request from another uid was not tried (BF1).
- **The idle exit** (A4.4 item 12): observed (BF31).
- **The `Names` rule on real services**: the aliases on the image read as A2.3 says (for example `ssh.service` with `Names=ssh.service sshd.service`). A unit whose name holds a `\x` escape is printed differently: see finding A7.5.
- **The answer for a masked unit**: seven unit files are masked on the image; none is loaded and none was requested (a raw request would be needed). Not observed.
- **One-shot units on the read path**: a known gap of R1 (A5.8); not tested as a failure.
- **The exit codes of `systemctl reboot` and `systemctl poweroff`** (S10): not captured. The live journal shows the start of `oarfish-broker.service` and then the connection closed (about 0.2 s after the start for the shutdown); the broker's result line was lost with the volatile journal. The evidence of both is the audit entry and the device's behaviour.
- **A-07** (the device stays off after a power-off): consistent with the observation, not independent of it. The device did not answer before the owner switched the plug, and it booted after that; nobody tried to reach it during the off time.
- **The update page of `v0.1.0-alpha.12` for this release** (the statement of `RELEASING.md` written in G7.6a): observed by the owner before the installer run: version `0.1.0-alpha.13`, "Nicht bestätigt", the manifest notice, no button to install, the release notes visible ([`UPDATE.md`](../UPDATE.md#v010-alpha13-class-d)).

### A7.5 Finding (defect, not fixed)

**A unit name with an escape is always answered `internal`.** For a unit whose name holds a `\xHH` escape, systemd 257 prints the property `Names` of the `show` shape in double quotes, with the backslash doubled; `Id` stays unquoted. Observed on the device with the shape of B2 for a `systemd-fsck@…\x2d….service` instance (read only, as root). `unitFacts` in `backend/cmd/oarfish-broker/unit.go` requires every word of `Names` to pass rule 1, so the quoted word fails and the broker answers `internal` before rules 4 to 11 run. Rule 1 allows such names, so a unit with an escaped name can never be started, stopped or restarted, and a denied one is answered `internal` instead of `unit_denied`. The broker fails closed: nothing runs. No request for such a unit was sent; the answer is read from the code. *Smallest fix proposal, not built:* unquote a `Names` value that systemd quoted (C-style escapes, as systemd writes them) before it is split, and add the observed output as a class C fixture. An owner decision; it changes the privileged code. *Pointer (G7-FIX1):* fixed by [amendment A8](#amendment-a8-g7-fix1-quoted-names-in-show) (class C until its device check).

### A7.6 What was not observed

- Every class T row (no test build): BF4, BF7 to BF11, BF13, BF14, BF20, BF24.
- The class D part of BF1, BF5, BF6, BF17, BF18, BF28 and BF29.
- The texts of the status view during and after the reboot (not reported).
- The exit codes of `systemctl reboot` and `systemctl poweroff`; the broker's own lines for the power operations.
- The `VmHWM` of the `systemctl` child, and the memory of a list read.
- The time of `list-unit-files` under the service's sandbox against the 5 s bound.
- The answer for a masked unit; the behaviour under load.
- The page in another browser, with a screen reader, in forced colours or at 320 CSS pixels; the native dialog's focus trap.
- An interrupted installer run (ADR 0010 A21), `--uninstall` with the broker, and a GUI update that replaces the broker (no later release exists).
- Ubuntu Server (G12), another board or image.

### A7.7 Open points for the owner

- **G7 exit (D-C87 item 15).** BF16, BF26 and BF27 were observed as class D. Whether G7 passes, and which of the rows of A7.6 are accepted as known and not blocking, is the owner's decision.
- **Finding A7.5** (escaped unit names): whether and when it is fixed. *Pointer (G7-FIX1):* G7 passed by D-C100; A7.5 is fixed by [amendment A8](#amendment-a8-g7-fix1-quoted-names-in-show).
- **Texts** (owner remarks during the run): the wrong-password sentence of the power dialog reuses the account page's "Das aktuelle Passwort ist falsch."; the owner suggests a wording that names the password just entered. The counts of the page Aktivität do not add up when a row is in another state than active, failed or inactive (one row of 156 in this run).
- **Wording in `RELEASING.md`**: "the manifest of this release lists twelve" counts the entries of the tarball (the root directory and 11 files); `contents` lists 11.
- The open points of A5.8 and A6.8 stay open.

### A7.8 Deviations

- The local release checks before the tag ran with the scripts that `make release-bundle` and `make check-release-history` call, with a fresh output directory instead of `dist/release`, which held bundles of earlier builds.
- The journal timestamps of the early boot are those of the last saved clock (no real-time clock) until the time is synchronized; times of the boots above are taken from the uptime, not from those lines.

## Amendment A8 (G7-FIX1, quoted names in show)

**A8 (G7-FIX1, written 2026-11-26, `docs/DECISIONS.md` D-C100).** The owner decided that G7 passes (D-C100) and that finding A7.5 is fixed in a follow-up step, G7-FIX1. This amendment records systemd's quoting of the `show` shape, the fix in the broker, the check of the service side and the tests. It edits no sentence of the body or of A1 to A7, except one pointer sentence in A7.5 and one in A7.7. No rule of B3 changes: rule 1 to rule 11 apply as A3.4 and A4.1 state them, now also to a unit whose `Id` or alias holds a `\xHH` escape.

**A8 is class C evidence until the device check of G7-FIX1.** The tests prove the parsing against fixtures in systemd's shape; they prove nothing about systemd's behaviour beyond what its source says. Nothing is Verified, and the reference device stays Compatible.

### A8.1 systemd's quoting rule, and its source

Read from the systemd source at the tags `v257` and `v257.13` (the reference device runs Debian's `257.13-1~deb13u1`; the files below are identical in both tags; whether Debian patches them was not checked, an assumption):

- `src/shared/bus-print-properties.c`, `bus_print_property()`: a property of D-Bus type `as` (an array of strings, such as `Names`) is printed as its entries separated by one space, each entry passed through `shell_maybe_quote(str, 0)`. A property of type `s` (such as `Id`) is printed as it is; a value that holds a newline is printed as `[unprintable]`.
- `src/systemctl/systemctl-show.c`, `print_property()`: systemctl's own printer handles neither `Id` nor `Names`, so the generic printer above applies.
- `src/basic/escape.c`, `shell_maybe_quote()` and `strcpy_backslash_escaped()`, with `src/basic/escape.h` and `src/basic/string-util.h`: an entry is printed unchanged unless it holds a control character, invalid UTF-8, a character of `WHITESPACE` (space, tab, newline, carriage return) or a character of `SHELL_NEED_QUOTES` (`"`, `\`, `` ` ``, `$`, `*`, `?`, `[`, `'`, `(`, `)`, `<`, `>`, `|`, `&`, `;`, `!`). Then it is enclosed in double quotes, and a backslash is put before each character of `SHELL_NEED_ESCAPE` (`"`, `\`, `` ` ``, `$`); a control character is written as a C escape; every other character stays as it is.

Of the 13 properties of the `show` shape of B2, only `Names` has type `as`; `Id`, `LoadState`, `ActiveState`, `SubState`, `Result`, `Type` and `UnitFileState` have type `s`; the others are booleans or numbers. So only `Names` can be quoted. A unit name allowed by rule 1 holds a character of `SHELL_NEED_QUOTES` only as the backslash of a `\xHH` escape, so `a@x\x2dy.service` is printed as `"a@x\\x2dy.service"` in `Names` and as `a@x\x2dy.service` in `Id`, which is what A7.5 observed.

`list-units` and `list-unit-files` print the name as a table cell of type `TABLE_STRING` (`src/systemctl/systemctl-list-units.c`, `src/systemctl/systemctl-list-unit-files.c`, `src/shared/format-table.c`), which is not quoted. The read path R1 therefore sees the name as it is.

### A8.2 The change in the broker

`unitFacts` in `backend/cmd/oarfish-broker/unit.go` reads `Names` with `splitNames`, the inverse of the rule of A8.1: 1 to 64 entries (`maxNames`, unchanged) separated by single spaces. Each entry is either a plain word without a character of `SHELL_NEED_QUOTES` and without white space, or a word in double quotes whose only escapes are a backslash before `"`, `\`, `` ` `` or `$`, in which none of `"`, `` ` `` and `$` stands without its backslash, and which systemd would have quoted (it holds one of the characters above). The entry comes back unquoted, and rule 1 is applied to it as before. Everything else is `errShowShape` and the answer `internal`, as before: an empty entry, a leading, trailing or double space, an unterminated quote, a backslash before any other character (a C escape included), a quoted word joined to the next one, a word quoted without need, and an unquoted word that holds a backslash (systemd 257 never prints one). `Id` is not unquoted, because systemd does not quote it; a quoted `Id` fails rule 1. The bounds of `parseShow` (13 keys, each once, printable ASCII) and of rule 1 (255 bytes) are unchanged.

The broker's bytes change: `oarfish-broker-linux-arm64` was `f18712cd3a42a6a8ba10987a925cc31678954e07d1f6e469abb170326ba70551` (`v0.1.0-alpha.13`) and is `7f7329e3692b1d72f27e76e86c2ade805aaf32db7ea5d6549eaad72a60a50109` after the fix (`make build-broker`, reproducible). The helper's bytes are unchanged (`0c6f95d25fbb44d25af6f21a8d96ee9d0d0fa46a99ad4600ec67c32bc3e6400e` before and after, `make build-helper`). No unit file, the installer and the bundle format do not change.

### A8.3 The service side

No change is needed. R1 reads the names from `list-units` and `list-unit-files`, which do not quote (A8.1), checks them with `internal/unitname` (rule 1, which allows the escape), and the page sends the name of the row unchanged in the JSON body of `POST /api/v1/services/action`. Tests that show it: `TestEscapedNamesOnReadPath` (`internal/services`: an escaped instance is listed under its exact name, takes the start mode of its template and is offered stop and restart; an escaped `systemd-fsck@` instance is listed as denied without actions), `TestServicesActionSubmit` (`internal/server`: the escaped name reaches the job byte for byte) and the test of `submitAction` in `frontend/src/lib/services.test.ts` (the page posts the name exactly as the list gave it).

### A8.4 Tests and mutations

- `TestShowEscapedNameFromDevice`: the shape A7.5 observed, for a `systemd-fsck@` instance with two `\x2d` escapes and a synthetic device path, is accepted and gives the unquoted name in `Id` and `Names`.
- `TestSplitNames`: plain and quoted entries, an alias with and without escapes, the edge cases of the rule (an escaped backslash, quote, `$` and `` ` ``, a quoted space, `*` and `'`), each also printed back by a separate test implementation of `shell_maybe_quote` and compared; the malformed shapes of A8.2; the bound of 64 entries.
- `TestShowNamesRule1AfterUnquoting`: a correctly quoted entry that is not a unit name (a space, a quote, `$`, `` ` ``, `*`, a double backslash, an upper-case escape and others) fails the read; a quoted `Id` fails it too.
- `TestEscapedNames` (through the broker against the fake `systemctl`): the device shape is answered `unit_oneshot` (rule 8 before rule 10); an escaped instance succeeds for restart and stop; rules 4 to 9 give their own codes (`unit_not_canonical` for an escaped alias, `unit_template`, `unit_masked`, `unit_transient`, `refused_by_unit`, `already_in_state`); escaped deny-list instances, as `Id` and as an alias, are `unit_denied`; escaped restart-only instances are `operation_not_offered` for stop and succeed for restart; malformed quoting is `internal`.
- `FuzzParseShow`: new seeds with quoted `Names`; for every accepted input each entry passes rule 1 and the test implementation of `shell_maybe_quote` prints the entries back to the exact value of `Names`.
- The test helper `showOut` now prints `Names` quoted as systemd does, so every broker test with an escaped name uses the real shape.

Mutations, each applied alone to `unit.go`; each builds, and each turns tests red:

| Mutation | Failing tests |
|---|---|
| `Names` split with `strings.Fields`, no unquoting | `TestShowEscapedNameFromDevice`, `TestParseShow`, `TestEscapedNames`, `TestHostileShowOutput` |
| A quoted word taken as it is up to the next quote, without unescaping and without the checks | `TestSplitNames` (among others the bad escape `"a@x\x2dy.service"`, the quoted `"a.service"` and `""` are accepted), `TestShowEscapedNameFromDevice`, `TestParseShow`, `TestEscapedNames` |
| Rule 1 not applied to the entries after unquoting | `TestShowNamesRule1AfterUnquoting` |

### A8.5 Three page fixes (texts and the count line)

Two remarks of the owner in A7.7 ("Texts") and one sentence of the status view, frontend and texts only; no route, no backend field and no rule changes.

1. **Wrong password in the power dialog.** It reused the account page's "Das aktuelle Passwort ist falsch." / "The current password is wrong.", although the dialog asks for the password, not the current one. It now has its own key `pwrErr.wrong_password`: "Das Passwort ist falsch." / "The password is wrong." (`lib/powerErrors.ts`). The account page keeps its sentence. The update page's password step reuses the same account sentence (`lib/updateErrors.ts`, `wrong_password`), where "aktuell" is just as wrong; it is not changed here (an open point for the owner). Service actions ask for no password (D-C87 item 7).
2. **The count line of the page Aktivität adds up.** The service counts only `active`, `failed` and `inactive` in their groups but every row in the total (`count` in `internal/services/reader.go`), so a row in another state of the closed set of A5 (`reloading`, `activating`, `deactivating`, `maintenance`, `refreshing`) was in no group: on the device the line read "156 Dienste, davon 41 aktiv, 0 fehlgeschlagen und 114 inaktiv" (A7.7). The page now derives that group as the total minus the three counts (no new field) and, when it is not zero, uses `actCountsTransition`: "… , {inactive} inaktiv und {transition} im Übergang." / "… , {inactive} inactive and {transition} in transition."; with no such row the old sentence `actCounts` stays. The state filter has a fifth choice, "Im Übergang" / "In transition", which shows exactly those rows; "Aktiv", "Fehlgeschlagen" and "Inaktiv" keep showing exactly their own state.
3. **The status view after a restart, state "wieder erreichbar" only.** The tunnel sentence there is now `pwrTunnelBack`: "Wenn Sie über einen SSH-Tunnel verbunden waren, bauen Sie ihn jetzt neu auf. Dann melden Sie sich an." / "If you were connected through an SSH tunnel, set it up again now. Then sign in." Before the device is back, after 5 minutes without an answer and in the shutdown view the sentence `pwrTunnel` stays.

Tests: `lib/power.test.ts` (the sentence and that the account sentence is unchanged), `pages/ActivityPower.test.ts` (every refusal sentence, the scenario `power-wrong-password`, the status view's tunnel sentence in each state, the scenario `power-reboot-flow`), `pages/Activity.test.ts` ("state list in transition …" with one activating row, "the count line adds up for every active state of the closed set", the scenario `list-in-transition`), and `docs.test.ts` for the text tables of [`UI.md`](../UI.md). Checked in jsdom only; not in a real browser or on a device.

### A8.6 What is not verified, and what stays open

- Every result above is class C. The device check of G7-FIX1 (a GUI update that replaces the broker, and the page with a unit whose name holds an escape) is still to come. *Pointer (G7-FIX1, phase 3):* the device result is in [A8.8](#a88-device-result-g7-fix1-phase-3).
- Whether Debian's systemd 257.13 package changes the printing code of A8.1 (assumption: it does not).
- Rule 1 allows a backslash only in the instance of a template (B3, unchanged). A unit or an alias with an escape outside the instance, for example `a\x2db.service`, still fails rule 1: as a request it is `unit_invalid`, as an alias in `Names` it makes the broker answer `internal`, and as a line of `list-units` or `list-unit-files` it makes the whole read of R1 fail. None was seen on the reference image; this is an open point for the owner, not part of this fix.
- The open points of A5.8, A6.8 and A7.7 stay open.

### A8.7 Deviations

None from the body or from A1 to A7.

### A8.8 Device result (G7-FIX1, phase 3)

**Evidence label.** *Device observation (G7-FIX1 phase 3), production release `v0.1.0-alpha.14`, one device, one image, one moment, 2026-11-27*; the reference device of A7.2. Class D in the sense of B9. Nothing is Verified, and the support status of the reference device stays Compatible. The raw outputs stay outside this repository; device values (host name, user name, address, partition identifiers) are left out.

**Method.** The implementation agent read the device over SSH with `sudo` for reading only, under the owner's approval in chat. The one step that changed the device, the GUI update, was made by the owner in a real browser through an SSH tunnel; the agent never handled the password. No request was sent to the broker's socket by the agent, and no file was edited by hand.

- **The release** (phase 2): `v0.1.0-alpha.14` on `49cd9fbea51f2e1627d986ed90a13834b3f51fc7` (the merge of PR #66). Only `oarfish-broker` differs from `v0.1.0-alpha.13` (`7f7329e3…` against `f18712cd…`). Both release checks passed in the real run (`component-set: PASS: v0.1.0-alpha.14 brings a set that no earlier release brought (1 compared)`; `unit-forms: PASS`, all six units accepted by the helper of `v0.1.0-alpha.13`). Anonymous verification printed `VERIFIED`, trusted comment `oarfish v0.1.0-alpha.14 49cd9fbea51f2e1627d986ed90a13834b3f51fc7`.
- **The GUI update that replaced the broker** (class D): from `v0.1.0-alpha.13` to `v0.1.0-alpha.14` on the update page, with the second confirmation, which named the privilege broker and asked for the password again (the owner's report; the wording was not copied). Afterwards the broker on disk equals the manifest, `active` and `known-good` name the new set, the broker socket is active, the helper is unchanged, 0 failed units, no installer run. The details and the journal are in [`UPDATE.md`](../UPDATE.md#v010-alpha14-class-d). This is the first observation of the component update for a broker file, the case of A20 that a device of `v0.1.0-alpha.13` takes from the GUI.
- **The `show` shape on the device** (class D, read only, as root, with the property list of B2): for the one service whose name holds an escape, a `systemd-fsck@` instance with two `\x2d` escapes, `systemctl show` printed `Id` unquoted and `Names` in double quotes with the backslashes doubled, the shape of A8.1 and of the fixture `fsckShow`.
- **The page with the escaped unit** (class D, page part): the page Aktivität listed that instance under its exact name, state "aktiv", with the note that Oarfish does not manage it and without buttons, as rule 10 says (the owner's report).
- **The broker's answer for an escaped unit: not observed.** The only escaped unit on the image is denied (rule 10) and one-shot (rule 8), so the page offers no action for it, and no raw socket request was sent. The parsing fix of A8.2 is therefore still class C; its input shape is now observed on the device.
- **The count line** (A8.5, class D, page part): after a hard reload the line read "156 Dienste, davon 41 aktiv, 0 fehlgeschlagen, 114 inaktiv und 1 im Übergang." (it adds up). At the agent's read some minutes later, a root `list-units --all` showed no service in another state than active, failed or inactive, so the row in transition was not identified; it had probably finished its transition. The wrong-password sentence and the tunnel sentence of A8.5 were not observed on the device.
- **Finding (not fixed): the page after a GUI update.** After the update the browser kept showing the page of `v0.1.0-alpha.13` (its count line without the new group) until the owner reloaded it hard (Cmd+Shift+R). A normal use of the page after an update can therefore show the old interface against the new service. Reported by the owner, recorded as a finding; whether and how it is fixed is the owner's decision. *Pointer (task N0):* the owner had the fix done in task N0: the service sends `index.html` with `Cache-Control: no-cache`, and the update page loads the new page itself once `/healthz` reports the new version ([`UI.md`](../UI.md#update-page-step-g68), "Reload after an update"; ADR 0008, [amendment "N0 as implemented"](0008-dashboard-metrics.md#amendment-n0-as-implemented), N0.7). Device check of N0 ([ADR 0008, N0.9](0008-dashboard-metrics.md#amendment-n0-as-implemented)): the cache headers are observed on the device; the reload was not observable, because the update to `v0.1.0-alpha.15` was started from the page of `v0.1.0-alpha.14`, which has no reload logic, so that page needed one normal reload after the login. The reload stays class C until an update that starts from `v0.1.0-alpha.15` or later.

**Not observed:** the broker started from the new binary by an action; the broker's answer for an escaped unit; the wording of the second confirmation; a boot after this update; any other browser.

## Amendment A9 (UF1, the read path and the Ubuntu Wi-Fi unit)

**A9 (step UF1 with N1 and ST1, written 2026-11-27, D-C104).** The fix task UF1 repairs two findings of the Ubuntu probe UBU0 ([`UBUNTU_PROBE.md`](../UBUNTU_PROBE.md)): U1, the read path of the services list, and U4, the Wi-Fi unit of Ubuntu in rule 11. D-C104 item 4 sets the frame: the unit-file states are kept apart from the unit list, the deadline is raised for `list-unit-files` only (A3.2 allows it), and the two read shapes of D-C87 stay. The rest of the design is an implementation choice of the step for the owner's review, except A9.2 item 6, which the owner chose in chat during the step. **Evidence: class C** (tests, CI). The device check on the Raspberry Pi OS card follows the release; **on Ubuntu nothing of A9 is rechecked** (D-C104 item 3). Nothing is Verified.

### A9.1 Why

- `list-unit-files` is computed by PID 1: 2.6 to 2.7 s on the Raspberry Pi OS image (A2, S6) and 17.0 s on the probed Ubuntu 24.04 image (about 400 unit files), every time (U1).
- PID 1 answers no other request meanwhile: `systemctl is-active` took 15.9 s, and socket activation of the update helper was delayed (U1, U2). Killing the client at its deadline does not stop the work in PID 1.
- Until UF1 the page read it on every load after the 5 s cache, and on Ubuntu every load failed at the 5 s deadline.

### A9.2 The read path as it now holds

1. **Shapes.** Unchanged: exactly the two shapes of A3.2 (`allowedShape`), no `show`, no third shape.
2. **The unit list.** `list-units` keeps its 5 s deadline, its single flight and its device-wide cache of 5 s (B2, A5.2); an action that ends drops that cache as before.
3. **The unit-file states** have their own store, their own single flight and a deadline of **45 s** (`list-unit-files` only). A read starts only at a list request, and only when (1) no states are known, (2) the known states are older than **30 minutes**, or (3) the store was invalidated by code; never while one runs, and never sooner than **60 s** after the previous one ended, also after a failure. There is no timer and no work without a request.
4. **Grace.** A list request waits at most **4 s** for a unit-file read. The read itself goes on and fills the store for later requests.
5. **The answer without states.** When no states are known in time, the list is answered from `list-units` alone: every start mode is unknown (`""`), and the units that only `list-unit-files` names (units that are not loaded) are missing. The list-level status is `pending` while a read runs and `unavailable` when the last read failed and none is known; the count line counts only the rows shown. Known states stay in use while a renewal runs and after a failed renewal. A failed or slow `list-unit-files` no longer fails the list; a failed `list-units` still does (`services_read_failed`).
6. **No list-units behind a unit-file read** (the owner's choice in chat during the step). While a unit-file read runs, a list request whose 5 s cache has expired does not start `list-units`, because PID 1 would hold that call behind the unit-file read and it would fail at its 5 s deadline on Ubuntu. It is answered from the last successful `list-units` result instead, which is at most as old as the start of that read (at most about 45 s); `read_at` gives its time. Outside a unit-file read the 5 s cache holds as before.
7. **Invalidation.** `Manager.InvalidateUnitFiles` marks the states out of date; it is the entry point for a later step that changes unit files (for example a package installation) and has no caller today. Start, stop and restart do not change unit-file states and do not drop them. A read that was running when the store was invalidated fills it but leaves it out of date.
8. **No offer depends on the start mode.** The buttons follow the deny list, rule 11 and the active state only (A5.6 item 7), so a row without a known start mode gets exactly the offer it has with one (`TestOffersWithoutStartModes`). The broker stays the authority for every action.
9. **API.** `GET /api/v1/services` gains two fields; nothing is removed or renamed, the schema version stays 1: `start_modes` (`known`, `pending` or `unavailable`) and `start_modes_read_at` (the time of the unit-file read the start modes come from, `null` unless `known`).
10. **Page.** While `pending`, the page says so and reads the list again 3 s after each answer, at most 10 times in a row; any other read starts that count again, and leaving the page stops it. While `unavailable`, it says so and shows "Unbekannt" as before. When `known`, it shows the time of the start modes under the time of the list ([`UI.md`](../UI.md#activity-page-step-g74)).
11. **What it still costs.** One unit-file read still occupies PID 1 once, at most once per 30 minutes and only at a list request: about 2.7 s on Raspberry Pi OS and 17 s on the probed Ubuntu image, during which other requests to systemd wait (among them the socket activation of the helper and of the broker). The measured alternatives of U1 are **not built**: `list-unit-files` with the unit names as patterns (5.3 s on Ubuntu; it changes the shape) and `systemctl show -p UnitFileState` (3.2 s; a third shape, which D-C87 and D-C104 item 4 exclude).

### A9.3 Rule 11 with the family netplan-wpa-

On Ubuntu the Wi-Fi link is held by `netplan-wpa-<interface>.service` (U4), which was on no list. The name prefix `netplan-wpa-` is added to the Network row of rule 11 and to restart only, in the service's lists (`backend/internal/services/classify.go`) and in the broker's (`backend/cmd/oarfish-broker/unit.go`). The row now reads:

| Kind | Units (`Id` or alias) | Stop offered |
|---|---|---|
| Network | `NetworkManager.service`, `networking.service`, `dhcpcd.service`, `wpa_supplicant.service`, `wpa_supplicant@*.service`, `wpa_supplicant-nl80211@*.service`, `wpa_supplicant-wired@*.service`, `ModemManager.service`, `avahi-daemon.service`, `rpi-usb-gadget-ics.service`, `ppp@*.service`, every name that begins with `netplan-wpa-` | No (restart only), except `ModemManager.service` |

So `netplan-wpa-wlan0.service` gets the network warning, restart is allowed and stop is refused with `operation_not_offered`. The prefix is matched at the start of the name; a name that only looks similar (`netplan-ovs-cleanup.service`, `netplan-wpa.service`, `my-netplan-wpa-wlan0.service`) stays an ordinary unit. `systemd-networkd`, `systemd-resolved` and `systemd-timesyncd` stay covered by the `systemd-` prefix of the deny list. The broker's bytes change (a component update with the second confirmation, ADR 0010 A20 to A22); the helper's do not.

### A9.4 Tests and mutations (class C)

- **Read path** (`backend/internal/services/unitfiles_test.go` and `reader_test.go`, with the process fake and a Go runner): a `list-unit-files` slower than the grace gives `pending` with the rows of `list-units` only, then the full list without a second process (`TestUnitFilesPendingThenKnown`, which also proves A9.2 item 6); one process for 30 page loads in 29 minutes and a renewal at 30 minutes while the old states are served (`TestUnitFilesOneReadFor30Minutes`); one read for eight concurrent requests (`TestUnitFilesSingleFlight`); the 60 s floor after a failure and after a failed renewal (`TestUnitFilesFloorAfterFailure`); a failed read does not fail the list (`TestUnitFilesFailedListStillAnswered`); a read that never ends is killed at its deadline and leaves the list usable (`TestUnitFilesHangingReadIsKilled`); the bounds and the deadlines passed to the runner (`TestUnitFilesDeadline`); `close` ends a running read (`TestCloseEndsAUnitFileRead`); invalidation (`TestInvalidateUnitFiles`); the offers without start modes (`TestOffersWithoutStartModes`). `TestOnlySystemctlExecutes` and the architecture test stay green.
- **Rule 11** on both sides: `TestRestartOnlyClassification`, `TestRestartOnlyIsTheNoRowsOfRule11` and `TestListsMatchTheBroker` (service), `TestLists` and `TestRule11RestartOnly` (broker), with the names that only look similar.
- **Routes and page:** `TestServicesList` (the two fields), `Activity.test.ts` ("activity start modes"), the scenarios `start-modes-pending`, `start-modes-pending-long` and `start-modes-unavailable`.
- **Mutations**, each compiled and turned a test red, then restored: the unit-file read started on every list request; the grace removed (the list waits); the 60 s floor removed; a failed unit-file read fails the list again; the prefix removed from the broker's list and, separately, from the service's; the prefix matched as a substring, in either list.

### A9.5 What is not verified, and what stays open

- **Not rechecked on Ubuntu** (D-C104 item 3): the time of the page on Ubuntu, the warning and the restart-only rule for `netplan-wpa-wlan0.service`, and U2 (the late `update_finished`); class C until a later Ubuntu run.
- **Open:** one unit-file read still occupies PID 1 (A9.2 item 11); the implementation choices of A9.2 (the 30 minutes, the 60 s floor, the 4 s grace, the 45 s deadline, the re-read of the page) are for the owner's review.
