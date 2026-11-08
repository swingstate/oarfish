# The update path

How a device updates Oarfish from the GUI, what is checked, what happens when something fails, and how the evidence on the reference device is produced. The design is [ADR 0010](adr/0010-update-helper.md) (decisions U1 to U15, amendments A1 to A13). This document describes the code at gate G6.9. **Nothing on this page is verified on a device.** Every device status below says "not run". The section [Results on the reference device](#results-on-the-reference-device) stays empty until the closeout task of G6.9 fills it in.

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

Device status of every row: **not run**. "C only" means that the row, or the named part of it, is not planned on the device. Test names refer to the packages `cmd/oarfish-update-helper` (H), `internal/relverify` (R), `internal/updater` (U) and `internal/server` (S) below `backend/`.

| # | Case | Class (plan) | Covering CI tests | Device |
|---|---|---|---|---|
| F1 | Bad signature | C, D; the audit entry: C only (the GUI flow downloads genuine files, nothing can be injected there) | R `TestSignatureOtherKey`, `TestGoldenTamper`; H `TestStageErrorCodes`; U `TestAdvisoryVerificationRefusesTamperedReleases`, `TestAuditResultsOfTheHelperCodes` | not run |
| F2 | Mix-and-match | C, D | R `TestMixAndMatch` | not run |
| F3 | Wrong checksum | C, D | R `TestVerifyMetadataTamper`; H `TestCleanupAfterFailuresDuringExtraction`; U `TestDownloadLimitsAndTamper` | not run |
| F4 | Truncated download | C, D (placed truncated file); the cut download and the `.part` file: C only | U `TestDownloadRetriesOnceAndNeverResumes`, `TestDownloadStallAndAbsoluteLimit`; H `TestCacheFileShrinksDuringTheCopy` | not run |
| F5 | No space | C, D | H `TestStageSpaceChecks` | not run |
| F6 | Start failure | C, T | H `TestRollbackAfterAStartFailure`, `TestRollbackAfterARestartLoop` | not run |
| F7 | Health failure | C, T | H `TestRollbackAfterTheDeadline`, `TestWaitHealthyCriteria` | not run |
| F8 | Power loss while staging | T (and C) | H `TestCrashSweepStage`, `TestRecoverRow2Staging` | not run |
| F9 | Power loss in the verification window | T (and C) | H `TestCrashSweepRecoverFromVerifying`, `TestRecoverRows6To8RollBack` | not run |
| F10 | Replay of an old release | C, D | R `TestReplay`; U `TestAdvisoryDowngradeIsRefused` | not run |
| F11 | Same version | C, D | R `TestCheckNewer`; H `TestStageErrorCodes` | not run |
| F12 | Release changes the helper or a unit | C, D (from `v0.1.0-alpha.7` to `v0.1.0-alpha.8`, whose helpers differ, A13); T optional | H `TestComponentComparison`, `TestInstallerRequiredBodyCarriesOnlyDiffers`; U `TestInstallerRequiredCarriesTheFixedInstruction` | not run |
| F13 | Hostile archive or sizes | C only | R `TestInspectRejectedEntryTypes`, `TestInspectRejectedPaths`, `TestInspectSizeMismatch`, `TestInspectDecompressionBomb`, `FuzzInspectTar` | not planned |
| F14 | Damaged rollback target | C, T; injected after `activate` is accepted and before the rollback compares (the hash is recorded when `activate` starts) | H `TestDamagedRollbackTarget`, `TestRecoverRow10RollbackHash` | not run |
| F15 | Wrong peer | C, D | H `TestPeerUIDMismatchClosesWithoutAnswer`, `TestRealPeerCheck` | not run |
| F16 | Concurrent apply | C, D (helper `busy`); the second apply in the service job: C | H `TestConcurrentStageOneWins`, `TestStageInOtherPhasesIsBusy`; U `TestOneJobAtATimeAndTheStepsAreShown` | not run |
| F17 | Helper not installed or stopped | C, D | U `TestStatusAnswerWithoutAHelper`; S `TestUpdateRoutesWithoutAnUpdater` | not run |
| F18 | Offline, rate limited, wrong clock | C, D (offline, wrong clock); the fake `403` with a reset header: C only | U `TestNetworkErrorsAreClassifiedByCodeOnly`, `TestAnUntrustedCertificateIsATLSFailure`, `TestRateLimitHandling` | not run |
| F19 | Success | D | H `TestActivateSuccess`; U `TestApplyDownloadsVerifiesStagesAndActivates`, `TestReconcileWritesTheFinishingEntryOnceAndAcknowledges` | not run |
| F20 | Cooldown survives the helper | C, T | H `TestCooldown`, `TestCooldownSameVersionBoundary` | not run |
| F21 | Cooldown across a reboot | C, T | H `TestCooldownVoidAfterAReboot` | not run |
| F22 | Staged release expires | C, D (the helper side); the GUI text "start again": C only (the GUI stages and activates in one job) | H `TestActivateStageExpired`, `TestStatusExpiresAStagedRelease` | not run |
| F23 | FIFO, link, directory, socket or growing file in the cache | C, D | H `TestCacheFileKinds`, `TestCacheFileGrowsDuringTheCopy`, `TestStageDeadline` | not run |
| F24 | Component replacement, success | D (G6.10) | not implemented (G6.10) | not run |
| F25 | Unit lint reject | C, T (G6.10) | not implemented (G6.10) | not run |
| F26 | Key rule | C, T (G6.10) | not implemented (G6.10) | not run |
| F27 | New helper does not start | C, T (G6.10) | not implemented (G6.10) | not run |
| F28 | New helper answers a wrong protocol | C, T (G6.10) | not implemented (G6.10) | not run |
| F29 | Power loss in each component phase | T (and C) (G6.10) | H `TestRecoverLeavesComponentPhasesAlone` (component phases are left alone today) | not run |
| F30 | Damaged previous component copy | C, T (G6.10) | H `TestComponentsDamaged` (the comparison side) | not run |
| F31 | Components first, release then fails | T (G6.10) | not implemented (G6.10) | not run |
| F32 | Missing component confirmation | C (G6.10) | H `TestComponentsConfirmedRule`; U `TestComponentUpdatePausesForTheSecondConfirmation` (the interlock exists; a difference cannot be staged before G6.10) | not run |

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

The owner runs the device session from a private test kit, outside the repository, in phases. The steps are reviewed one at a time.

1. **Production helper, class D, with published releases.** The official installer of `v0.1.0-alpha.7` runs on a device that was on an earlier alpha. The GUI then offers `v0.1.0-alpha.8` and answers `installer_required` (F12). After that the official installer of `v0.1.0-alpha.8` runs. Then follow the layout, socket activation, hardening and status checks, and the rows that need no newer release. These rows place real or throwaway-signed files in the cache and send `stage` over the socket as the service user, or they act on the system around the service: a stopped socket, a runtime drop-in that denies the service network access, a wrong clock, a bounded and automatically removed fill of the file system.
2. **Production helper, class D, after the next real release.** The checksum rows use that release (F3, F4). A second request runs during a stage (F16). The staged release expires in real time (F22). Then follows the update from the GUI with measurements (F19).
3. **Test build, class T.** A test copy of the installer differs from the production script only in the trusted key, the allowed download protocol and the base URL. It installs a test release with the test helper. A fault hook `hold` stops the helper at a chosen call, and power is removed at a known point. Every block ends with the restore and the check that the production key id is reported again. These scripts are a later step.
4. **Restore and collection.** The official installer of the newest production release restores the device. One collector writes all closeout values into one text file without secrets.

No device values (host names, addresses, user names, serial numbers) are written into this repository.

## Results on the reference device

Not run. This section is filled in by the closeout task of G6.9 (step C), with the label of the device observations and the list of what was not run.
