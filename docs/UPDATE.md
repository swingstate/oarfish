# The update path

How a device updates Oarfish from the GUI, what is checked, what happens when something fails, and how the evidence on the reference device is produced. The design is [ADR 0010](adr/0010-update-helper.md) (decisions U1 to U15, amendments A1 to A14). This document describes the code at gate G6.9. **Device observations exist, each labelled with its evidence class (C, D or T); nothing is Verified.** Every device entry below means "observed as expected on one device" (one Raspberry Pi Zero 2 W, one image); class T is never production evidence. The section [Results on the reference device](#results-on-the-reference-device) holds the facts, what was not run and the open items.

## Contents

1. [What exists](#what-exists)
2. [The steps of one update](#the-steps-of-one-update)
3. [What is checked, and in which order](#what-is-checked-and-in-which-order)
4. [States, results and the rollback](#states-results-and-the-rollback)
5. [Cooldown and expiry](#cooldown-and-expiry)
6. [When the installer is required](#when-the-installer-is-required)
7. [Evidence classes](#evidence-classes)
8. [Failure matrix F1 to F32](#failure-matrix-f1-to-f32)
9. [Closeout evidence](#closeout-evidence)
10. [How the device evidence is produced](#how-the-device-evidence-is-produced)
11. [Results on the reference device](#results-on-the-reference-device)

## What exists

| Part | Where | Privilege |
|---|---|---|
| Update service | `backend/internal/updater`, routes in `backend/internal/server/update.go` | the unprivileged service user `oarfish` |
| Update page | `frontend/src/pages/Update.svelte` (System > Update) | browser |
| Update helper | `backend/cmd/oarfish-update-helper`, units `oarfish-update-helper.socket`, `.service`, `oarfish-update-recover.service` | root, socket activated, empty capability set, writes only below `/opt/oarfish` and `/var/lib/oarfish-update` |
| Shared verification | `backend/internal/relverify` | used by both |

The service does all network traffic and verifies only so that the page can show results and fail early (advisory). The helper copies the downloaded files into its own area and verifies them again. Only the helper's verification counts (ADR 0010 U3, U4). The helper accepts five typed requests (`status`, `stage`, `activate`, `discard`, `ack`). No request has an argument that names a path, a unit, a program or a URL (U2).

## The steps of one update

1. **Check** (user action: opening the page, at most once per 60 minutes, or "Check now", at most once per 60 s per device). The service reads the release list from the GitHub API, picks the highest eligible version (prerelease rule Q7) and verifies the small files of that release (advisory).
2. **Apply.** The administrator confirms with the fresh password (Q3). The password is checked before anything is downloaded. The service starts one job (a second request answers `busy`).
3. **Download** into `/var/cache/oarfish/update/<version>/` (directory 0755, files 0644), streamed with size caps and time limits.
4. **`stage`.** The helper takes the lock, writes the intent `staging`, copies the four files into `releases/.stage.<id>/` and hashes the bytes it writes, verifies them, extracts and checks the tree, compares the five component files with the installed ones, moves the tree to `releases/<version>` and writes `staged`.
5. **`activate`.** The helper checks that the installed version is healthy (pre-check), records the hash of the running binary, writes `switching`, switches `current` with one atomic rename, writes `verifying`, restarts `oarfish.service` and runs the health check.
6. **Commit or rollback.** If the new version is healthy, the helper writes `committed`, keeps the previous version and removes older releases. Otherwise it rolls back (below). The service restarts in both cases, so everybody is signed out.
7. **Finish.** At the next status read, the restarted service writes `update_finished` to the audit with the correlation ID of `update_started`, and then acknowledges the result (`ack`).

## What is checked, and in which order

What the helper checks in `stage`, in this order (`backend/cmd/oarfish-update-helper/stage.go`):

| Order | Check | Code when it fails |
|---|---|---|
| a | phase is `idle` (an expired staged release is cleaned first) | `busy` |
| b | the installed version is readable | `current_unhealthy` |
| c | the four cache files are regular files (no link, FIFO, directory, socket) within their caps | `not_found`, `checksum_mismatch` |
| d | free space: the cache sizes plus 64 MiB | `no_space` |
| g | signature of `checksums.txt` (embedded key, legacy `Ed`), the checksum list, the manifest hash and fields, the version against the request | `signature_invalid`, `checksum_mismatch`, `manifest_invalid` |
| h | free space: copied plus extracted bytes plus 64 MiB | `no_space` |
| i | bundle copy with the manifest size as limit; its hash and size against the signed values | `checksum_mismatch` |
| j | strictly newer than installed, minimum installed version, helper protocol | `downgrade_refused`, `already_installed`, `requirements_not_met`, `helper_too_old` |
| k | tarball inspection and extraction (allow-listed names, regular files, sizes, hashes, ELF) | `manifest_invalid`, `checksum_mismatch`, `requirements_not_met` |
| l | the bundle has components; the five compared files equal the installed ones | `requirements_not_met`, `installer_required` |

The bundle hash (i) comes before the version rule (j). A changed or truncated copy of any real release therefore answers `checksum_mismatch` even when that version is installed or older. The version rule (j) comes before the component comparison (l). An older release with other helper bytes therefore answers `downgrade_refused`, not `installer_required`.

The health check (U6, A11.2): `ActiveState=active`, `SubState=running`, a new MainPID, `NRestarts` not increased, `/healthz` with the installed version and, from `strictHealthFrom` (`0.1.0-alpha.7`) on, `pid` equal to MainPID and `/readyz` 200. All of this must hold on 3 polls in a row and for at least 10 s. A failed unit or a higher `NRestarts` ends the wait at once (fail fast). Otherwise the deadline is 45 s.

## States, results and the rollback

| Phase in `state.json` | Meaning | After a crash or power loss (U7) |
|---|---|---|
| `idle` | nothing runs | – |
| `staging` | copy and verify running | staging removed, `idle`, result `aborted` |
| `staged` | verified release waiting for `activate` | removed at boot or after 30 minutes |
| `switching` | intent to switch `current` | `current` on `from`: intent cleared |
| `verifying` | restarted, health check running | at boot `current` back to `from`, `rolled_back` `interrupted` |
| `committed` | success durable | pruning finished |
| `rolling_back` | rollback running | finished |
| `rollback_failed` | rollback failed, `needs_attention` | a shell is needed (U6) |

Results: `success`, `rolled_back` (reasons `start_failed`, `health_timeout`, `restart_failed`, `interrupted`), `aborted`, `rollback_failed` (`rollback_target_damaged`, `rollback_unhealthy`, …). The rollback first writes `rolling_back`. It then compares the hash of `releases/<from>/oarfish` with the hash recorded when `activate` started, switches `current` back, runs `reset-failed` and `restart`, and checks the old version with the same criteria. A damaged rollback target is never switched to. The recover unit runs the same rule at every boot, before `oarfish.service` starts. Rollback does not restore data, so releases must stay N-1 compatible ([`RELEASING.md`](RELEASING.md)).

## Cooldown and expiry

- After a rollback or a failure the same version is refused for 15 minutes (`cooldown`). Any `activate` within 60 s of the previous one is refused as well. Both markers are stored in `state.json` with the time since boot (`CLOCK_BOOTTIME`), never the wall clock. A marker is void when the uptime is smaller than the uptime at which it was set (a reboot happened). After a reboot the cooldown may therefore end early, and this is accepted (U6).
- A staged release expires 30 minutes after `stage` (`stage_expired`). `status` and `activate` remove it, and so does every boot.

## When the installer is required

`stage` compares the SHA-256 of `oarfish-update-helper`, its three units and `oarfish.service` with the installed files. When one differs it answers `installer_required` with the names. The page then shows the installer instruction instead of applying the update (U9). This applies until G6.10 replaces components from the GUI.

- The bytes of the helper depend only on its code, the packages it links and the Go toolchain, never on the release version (A13, D-C80). A release that changes none of these and no unit text needs no installer.
- A device on `v0.1.0-alpha.7` or earlier needs the installer once: up to alpha.6 there is no helper (bootstrap, U11), and the alpha.7 helper embeds its release version.
- A release that changes the helper code, a package it links, the Go version or a unit text needs the installer. Its release notes say so ([`RELEASING.md`](RELEASING.md)).
- A change of the trust key is never applied from the GUI (U15 rule 5).

## Evidence classes

U13 and D-C56 define three classes of evidence:

- **C**: an automated test in CI (fixtures, fake root, fake systemd, fake GitHub).
- **D**: on the reference device, with the production binaries and releases signed with the production key.
- **T**: on the reference device, with a test build of the helper (a throwaway public key and the build tag `faulthooks`) and releases signed with a throwaway key. Class T is never presented as evidence about the production key path.

The success row F19 and the genuine signed-release path must be class D.

## Failure matrix F1 to F32

The Device column says what was observed as expected on one device and in which class (D or T); "C only" means that the row, or the named part of it, is not planned on the device, and "not run" means that the row belongs to G6.10. See [Results on the reference device](#results-on-the-reference-device). Test names refer to the packages `cmd/oarfish-update-helper` (H), `internal/relverify` (R), `internal/updater` (U) and `internal/server` (S) below `backend/`.

| # | Case | Class (plan) | Covering CI tests | Device |
|---|---|---|---|---|
| F1 | Bad signature | C, D; the audit entry: C only (the GUI flow downloads genuine files, nothing can be injected there) | R `TestSignatureOtherKey`, `TestGoldenTamper`; H `TestStageErrorCodes`; U `TestAdvisoryVerificationRefusesTamperedReleases`, `TestAuditResultsOfTheHelperCodes` | D (the audit entry stays C only) |
| F2 | Mix-and-match | C, D | R `TestMixAndMatch` | D |
| F3 | Wrong checksum | C, D | R `TestVerifyMetadataTamper`; H `TestCleanupAfterFailuresDuringExtraction`; U `TestDownloadLimitsAndTamper` | D |
| F4 | Truncated download | C, D (placed truncated file); the cut download and the `.part` file: C only | U `TestDownloadRetriesOnceAndNeverResumes`, `TestDownloadStallAndAbsoluteLimit`; H `TestCacheFileShrinksDuringTheCopy` | D for the checksum part (the cut download stays C only) |
| F5 | No space | C, D | H `TestStageSpaceChecks` | D |
| F6 | Start failure | C, T | H `TestRollbackAfterAStartFailure`, `TestRollbackAfterARestartLoop` | T |
| F7 | Health failure | C, T | H `TestRollbackAfterTheDeadline`, `TestWaitHealthyCriteria` | T |
| F8 | Power loss while staging | T (and C) | H `TestCrashSweepStage`, `TestRecoverRow2Staging` | T |
| F9 | Power loss in the verification window | T (and C) | H `TestCrashSweepRecoverFromVerifying`, `TestRecoverRows6To8RollBack` | T |
| F10 | Replay of an old release | C, D | R `TestReplay`; U `TestAdvisoryDowngradeIsRefused` | D |
| F11 | Same version | C, D | R `TestCheckNewer`; H `TestStageErrorCodes` | D |
| F12 | Release changes the helper or a unit | C, D (from `v0.1.0-alpha.7` to `v0.1.0-alpha.8`, whose helpers differ, A13); T optional | H `TestComponentComparison`, `TestInstallerRequiredBodyCarriesOnlyDiffers`; U `TestInstallerRequiredCarriesTheFixedInstruction` | D (judged by hand, see [kit false stops](#kit-false-stops-and-observations)) |
| F13 | Hostile archive or sizes | C only | R `TestInspectRejectedEntryTypes`, `TestInspectRejectedPaths`, `TestInspectSizeMismatch`, `TestInspectDecompressionBomb`, `FuzzInspectTar` | C only |
| F14 | Damaged rollback target | C, T; injected after `activate` is accepted and before the rollback compares (the hash is recorded when `activate` starts) | H `TestDamagedRollbackTarget`, `TestRecoverRow10RollbackHash` | T |
| F15 | Wrong peer | C, D | H `TestPeerUIDMismatchClosesWithoutAnswer`, `TestRealPeerCheck` | D (judged by hand, see [kit false stops](#kit-false-stops-and-observations)) |
| F16 | Concurrent apply | C, D (helper `busy`); the second apply in the service job: C | H `TestConcurrentStageOneWins`, `TestStageInOtherPhasesIsBusy`; U `TestOneJobAtATimeAndTheStepsAreShown` | D |
| F17 | Helper not installed or stopped | C, D | U `TestStatusAnswerWithoutAHelper`; S `TestUpdateRoutesWithoutAnUpdater` | D (GUI text recorded by the owner) |
| F18 | Offline, rate limited, wrong clock | C, D (offline, wrong clock); the fake `403` with a reset header: C only | U `TestNetworkErrorsAreClassifiedByCodeOnly`, `TestAnUntrustedCertificateIsATLSFailure`, `TestRateLimitHandling` | D for offline and wrong clock (the fake `403` with a reset header stays C only) |
| F19 | Success | D | H `TestActivateSuccess`; U `TestApplyDownloadsVerifiesStagesAndActivates`, `TestReconcileWritesTheFinishingEntryOnceAndAcknowledges` | D |
| F20 | Cooldown survives the helper | C, T | H `TestCooldown`, `TestCooldownSameVersionBoundary` | T |
| F21 | Cooldown across a reboot | C, T | H `TestCooldownVoidAfterAReboot` | T |
| F22 | Staged release expires | C, D (the helper side); the GUI text "start again": C only (the GUI stages and activates in one job) | H `TestActivateStageExpired`, `TestStatusExpiresAStagedRelease` | D (the "start again" text stays C only) |
| F23 | FIFO, link, directory, socket or growing file in the cache | C, D | H `TestCacheFileKinds`, `TestCacheFileGrowsDuringTheCopy`, `TestStageDeadline` | D (17 of 17 cases) |
| F24 | Component replacement, success | D (G6.10) | not implemented (G6.10) | not run (G6.10) |
| F25 | Unit lint reject | C, T (G6.10) | not implemented (G6.10) | not run (G6.10) |
| F26 | Key rule | C, T (G6.10) | not implemented (G6.10) | not run (G6.10) |
| F27 | New helper does not start | C, T (G6.10) | not implemented (G6.10) | not run (G6.10) |
| F28 | New helper answers a wrong protocol | C, T (G6.10) | not implemented (G6.10) | not run (G6.10) |
| F29 | Power loss in each component phase | T (and C) (G6.10) | H `TestRecoverLeavesComponentPhasesAlone` (component phases are left alone today) | not run (G6.10) |
| F30 | Damaged previous component copy | C, T (G6.10) | H `TestComponentsDamaged` (the comparison side) | not run (G6.10) |
| F31 | Components first, release then fails | T (G6.10) | not implemented (G6.10) | not run (G6.10) |
| F32 | Missing component confirmation | C (G6.10) | H `TestComponentsConfirmedRule`; U `TestComponentUpdatePausesForTheSecondConfirmation` (the interlock exists; a difference cannot be staged before G6.10) | not run (G6.10) |

## Closeout evidence

What the G6 closeout must contain for the base update (U13, U12), in addition to the matrix:

- the success run F19 with its timings: stage, activate, restart, time to healthy;
- `write_bytes` (`/proc/<pid>/io`) of the service and of the helper, before and after one update;
- the peak `VmHWM` of the service and of the helper during one update (the helper sets a soft memory limit of 16 MiB itself, because `MemoryMax=64M` is not enforced on the reference image, A4);
- `systemd-analyze security` of `oarfish.service`, `oarfish-update-helper.service` and `oarfish-update-recover.service` on the device;
- counts that show no password, token, address or host name in the helper journal, the service journal, the audit and `state.json`;
- the boot time and the recover unit's timing;
- the check that `pid` of `/healthz` equals the systemd `MainPID` (A11.2);
- when the finishing audit entry is written (open condition C3);
- a list of what was not run.

The replacement of components (F24 to F32) belongs to G6.10.

## How the device evidence is produced

The owner runs the device session from the device test kit, not part of this repository, in phases. The steps are reviewed one at a time.

1. **Production helper, class D, with published releases.** The official installer of `v0.1.0-alpha.7` runs on a device that was on an earlier alpha. The GUI then offers `v0.1.0-alpha.8` and answers `installer_required` (F12). After that the official installer of `v0.1.0-alpha.8` runs. Then follow the layout, socket activation, hardening and status checks, and the rows that need no newer release. These rows place real or throwaway-signed files in the cache and send `stage` over the socket as the service user, or they act on the system around the service: a stopped socket, a runtime drop-in that denies the service network access, a wrong clock, a bounded and automatically removed fill of the file system.
2. **Production helper, class D, after the next real release.** The checksum rows use that release (F3, F4). A second request runs during a stage (F16). The staged release expires in real time (F22). Then follows the update from the GUI with measurements (F19).
3. **Test build, class T.** A test copy of the installer differs from the production script only in the trusted key, the allowed download protocol and the base URL. It installs a test release with the test helper. A fault hook `hold` stops the helper at a chosen call, and power is removed at a known point. Every block ends with the restore and the check that the production key id is reported again. These scripts ran in the device test kit, not part of this repository (see the results below).
4. **Restore and collection.** The official installer of the newest production release restores the device. One collector writes all closeout values into one text file without secrets.

No device values (host names, addresses, user names, serial numbers) are written into this repository.

## Results on the reference device

**Observed on one device, not Verified.** One Raspberry Pi Zero 2 W Rev 1.0, Debian 13 (trixie) arm64, kernel 6.18.50+rpt-rpi-v8, systemd 257, one image. The support status stays Compatible. The values come from the owner's observations of the G6.9 device run with the device test kit, not part of this repository; the repository holds no device logs. The classes are those of [Evidence classes](#evidence-classes): D is the production binaries and the production release key (key id `11f1c52777090bfc`), T is a test build with a throwaway key and the build tag `faulthooks` and is never production evidence. No host names, addresses, user names or serial numbers are recorded here.

### Phase 1, class D (installers of `v0.1.0-alpha.7` and `v0.1.0-alpha.8`)

Observed as expected: the layout, socket activation and the status answer; F12, F1, F15, F17, F2a, F2b, F10, F11, F5 and F23 (17 of 17 cases); F18 offline and with a wrong clock. The GUI texts were recorded by the owner. F12 and F15 were judged by hand (see [Kit false stops and observations](#kit-false-stops-and-observations)).

### Phase 2, class D (`v0.1.0-alpha.8`, then `v0.1.0-alpha.9`)

Observed as expected: F3, F4 (the checksum part), F16, F19 and F22 (an `activate` after 2474 s answered `stage_expired` and the staging directory was removed).

**F19, the GUI update from `v0.1.0-alpha.8` to `v0.1.0-alpha.9` without the installer:**

| Measure | Value |
|---|---|
| Stage | 4.4 s |
| Activate | 12.0 s |
| Restart | 0.1 s |
| Time to healthy | 11.9 s |
| Cache in use | 6.9 s, peak 5,022,803 bytes |

| Process | Peak `VmHWM` | `write_bytes` during the update |
|---|---|---|
| Helper, update process | 9,872 kB (soft limit 16 MiB) | +17,162,240 bytes |
| Helper, `stage` requests | 5,572 kB | – |
| Service, during the download | 33,316 kB | +5,042,176 bytes |
| Service, as the new process | 27,576 kB | – |

`pid` of `/healthz` equals the systemd `MainPID` on the real service (A11.2).

### The boot-order finding (ADR 0010 A14, D-C81)

On `v0.1.0-alpha.8` and `v0.1.0-alpha.9` the helper socket stayed inactive after a reboot; the device only worked after the socket was started by hand. The cause: the recover unit had `Before=` the socket but the default `After=basic.target`, so systemd found an ordering cycle and deleted the socket start job. The fix is PR #41, [ADR 0010](adr/0010-update-helper.md) amendment A14 and decision D-C81. The release notes of `v0.1.0-alpha.10` said that the corrected unit had not yet run on a device.

### `v0.1.0-alpha.10`, class D

Installed with the official installer: 14 s, signature and hashes OK, components shape, the five compared files equal to the release, `pid` equal to `MainPID`, `/readyz` 200, 0 failed units.

**Boot check, observed as expected:** an automatic reboot, SSH back 57 s after the request, and the socket active without a manual start (0 `sudo` lines in the boot journal). 0 lines "ordering cycle" and 0 lines "deleted to break". The recover unit ended with `Result=success`, exit 0 and its start and end timestamps set; it ended 14.67 s and the socket was active 18.81 s into the boot. The helper answered with key id `11f1c52777090bfc` and installed version `0.1.0-alpha.10`. Boot time 42.292 s (kernel 6.039 s, userspace 36.252 s); `v0.1.0-alpha.9` took 40.93 s.

**Recover unit timing, what is evidenced and what is not.** The only evidence for the recover duration is the boot check above (the end of the unit 14.67 s into the boot, with its timestamps set) and the earlier G6.3 figure of about 650 ms ([INSTALL.md](INSTALL.md#results-on-the-reference-device-g63)). In the collector run on `v0.1.0-alpha.10` the unit's `ExecMainStart` and `ExecMainExit` timestamps printed as 0 again after the uninstall and reinstall; the unit state was probably reset (not proven). No other duration is stated.

**Not finished:** the boot check counted 999 journal lines for that boot. Whether the kit caps the journal it reads is not verified, and a search of the whole journal of the controlled boot is still to be done.

### Phase 3, class T (`v0.1.0-alpha.10`, throwaway key)

Each block ended with the official installer and the report of the production key again. Observed as expected:

| Row | Observation |
|---|---|
| F6 start fails | Plain case: `rolled_back` / `start_failed` after 15.9 s with 1 automatic restart (start limit not reached). Loop case: 14.7 s, 2 restarts, start limit hit, rolled back, the drop-in removed. |
| F20 cooldown | A different release under 60 s and the same version after the idle end of the helper both answered `cooldown`. |
| F7 health fails | `rolled_back` / `health_timeout` after 58.7 s (not before the 45 s deadline); `/readyz` 503 in the window. |
| F21 cooldown across a reboot | After the reboot the cooldown mark was void (uptime 51 s below the mark 1840 s, as A13 expects), `activate` was accepted, then `rolled_back` / `start_failed`. |
| F8 power cut during `stage` | 3 cuts at helper calls 7, 94 and 173: each time `aborted` / `interrupted`, phase `idle`, no leftovers, `current` and `releases` unchanged. |
| F9 power cut during `activate` | 3 cuts at calls 16, 17 and 22: each time the recover unit ran about 15 s after boot, `rolled_back` / `interrupted`, `current` back on the old version, cooldown for the interrupted version, no leftovers. |
| F14 damaged rollback target | `rollback_failed` / `rollback_target_damaged`, `needs_attention` true, cooldown set, `current` not switched to the damaged tree; the official uninstall and installer worked from that state. |

The six power cuts were switched by a switchable power outlet controlled by the test kit: off confirmed after 0 to 1 s, held 10 s, on confirmed after 0 s, SSH back after 50 to 54 s, 0 manual fallbacks.

### Phase 4, class D (`v0.1.0-alpha.9`, and again `v0.1.0-alpha.10` after Phase 3)

- **Installer re-run:** idempotent (release and components already identical, no restart).
- **`systemd-analyze security`:** exposure 1.7 OK for `oarfish.service`, the helper service and the recover service. For the recover unit it prints `PrivateTmp`, `ProtectHome`, `ProtectSystem` and `RootDirectory` as "special boot phase, option not appropriate" (not scored) since A14; the score stays 1.7. The unit file in the repository ([`install/oarfish-update-recover.service`](../install/oarfish-update-recover.service)) still sets `ProtectSystem=strict`, `ProtectHome=yes` and `PrivateTmp=yes`; whether they take effect at run time is not evidenced.
- **Files:** the five compared files equal the `v0.1.0-alpha.10` release.
- **Redaction check:** password count 0 everywhere; host name, user name, IPv4, IPv6 and MAC counts 0 in the helper, recover and service journals and in the audit files. The whole system journal and the kit logs are informative only.
- **State directory:** after uninstall and reinstall it has no state file (a fresh idle state). This is consistent with the helper creating it only with the first update transaction.

### Open condition C3, the finishing audit entry

`update_finished` is written only on a later `GET /api/v1/update` poll. In the run it was written about 13.5 min after the commit, with the time of the write and the user field "-", not the time the helper finished. This is the behaviour designed in G6.7. A proposal exists (write the entry with the helper's `finished_at` and check once at service start). The owner has not decided it; nothing was changed.

### Kit false stops and observations

The device test kit stopped several times on its own checks. These are the "ABWEICHUNG" lines of its ledger. They are not product failures:

- F12 on `v0.1.0-alpha.7`: the first creation of the state file counted as a change, and the journal check reported a BrokenPipe. Judged as expected by hand.
- F15: `connection_reset` instead of `closed_without_answer`. ADR 0010 U13 allows "closed". Judged as expected by hand.
- Two aborted boot-check runs on `v0.1.0-alpha.10`: the check read the state file, which does not exist before the first update, instead of the helper's socket answer. The kit was fixed; nothing was rebooted by them.
- A stale-test counter counted leftovers of an earlier version.
- The kit driver marked a step as failed when the printed ledger history contained the word "ABWEICHUNG".
- One early class T run on `v0.1.0-alpha.9` had a gap of 15 minutes between `stage` and `activate`, caused by a sleep defect of the kit (a kit defect, not a product finding).

**Observations without a gate failure:**

1. After a power cut the displayed wall-clock times (`finished_at`, the recover log) were 1 to 2 minutes too early, probably because the device has no battery clock (unproven). The helper logic uses uptime, so only displayed times are affected. Known limitation; no product change in G6.9.
2. Failed test releases stay in `releases/` after a rollback. Whether this is intended retention is an open question.
3. For F14 the service state and `/healthz` during `rollback_failed` were not captured.
4. The official uninstall and installer after F14 needed about 100 s before its own log started. The cause is unknown.

### What was not run, and what is not verified

- F13 and the parts marked "C only" in the matrix: the audit entry of F1, the cut download and the `.part` file of F4, the fake `403` with a reset header of F18, and the GUI text "start again" of F22.
- F24 to F32 belong to G6.10.
- Not verified: screen reader use, other boards and models, Ubuntu, behaviour over many days, four simultaneous clients.
- Nothing is Verified for this device or for any feature. G6 stays open until G6.10.
