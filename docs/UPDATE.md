# The update path

How a device updates Oarfish from the GUI, what is checked, what happens when something fails, and how the evidence on the reference device is produced. The design is [ADR 0010](adr/0010-update-helper.md) (decisions U1 to U15, amendments A1 to A17). This document describes the code at gate G6.9 and, since the steps a to c of G6.10, the unit lint, the replacement of the helper and its units by the helper (the component transaction), the second confirmation on the update page and its texts. **G6.10 is implemented with class C tests only (fake root, fake systemd, the fake helper, jsdom): it did not run on a device, and no release carries it yet.** The release that carries the helper of step b and the device evidence of F24 to F32 are later steps. **Device observations exist for the base update, each labelled with its evidence class (C, D or T); nothing is Verified.** Every device entry below means "observed as expected on one device" (one Raspberry Pi Zero 2 W, one image); class T is never production evidence. The section [Results on the reference device](#results-on-the-reference-device) holds the facts, what was not run and the open items.

## Contents

1. [What exists](#what-exists)
2. [The steps of one update](#the-steps-of-one-update), and [of an update with components](#the-steps-of-an-update-with-components)
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
4. **`stage`.** The helper takes the lock, writes the intent `staging`, copies the four files into `releases/.stage.<id>/` and hashes the bytes it writes, verifies them, extracts and checks the tree, lints the unit files of the release, compares the five component files with the installed ones, moves the tree to `releases/<version>` and writes `staged`. Its answer says whether one of the five files differs, and which.
5. **`activate`.** The helper checks that the installed version is healthy (pre-check), records the hash of the running binary, writes `switching`, switches `current` with one atomic rename, writes `verifying`, restarts `oarfish.service` and runs the health check.
6. **Commit or rollback.** If the new version is healthy, the helper writes `committed`, keeps the previous version and removes older releases. Otherwise it rolls back (below). The service restarts in both cases, so everybody is signed out.
7. **Finish.** At the next status read, the restarted service writes `update_finished` to the audit with the correlation ID of `update_started`, and then acknowledges the result (`ack`).

### The steps of an update with components

When `stage` finds that one of the five compared files differs, the update needs the second confirmation (U9, U15 rule 7, D6), and when one of the four helper files differs it is applied as one transaction in two processes of the helper (U15 rule 4, A16). Class C tests only; not run on a device.

1. **`stage`** as above, with three more checks when the helper files differ and before anything is moved: the manifest's `components.key_id` equals the key embedded in the installed helper (`key_changed`), `components.protocol` is the helper's protocol P or P+1 (`requirements_not_met`), and the known-good set equals its reference hash when one is recorded (`components_damaged`; without one it is recorded now). The staged helper files are moved to `components/<version>`, then the release to `releases/<version>`. The answer has `components_differ: true` and `differs` with the names.
2. **The pause and the second confirmation** (U9, U15 rule 7, A17). The job of the service waits at `awaiting_components_confirmation` (at most 30 minutes, the life of a staged release). The page shows the dialog "Neue Bestandteile bestätigen": the names of `job.differs` in plain words with their file names, that privileged components are replaced, that the service restarts and everybody is signed out, what happens on a failure (the release is rolled back; a new helper that passed its check stays), that a changed socket unit takes effect after the next restart of the device (D4), and the password again (D7). "Verwerfen" sends `discard`. When the staged release expires meanwhile, the job ends with `stage_expired` and the page says to check again and start once more. A first `apply` never carries the confirmation: with `components_confirmed` and no waiting job it is refused with `invalid_request`, and the job always pauses (A17.5, G1).
3. **`activate` with `components_confirmed`.** A refusal (for example `cooldown`, `confirmation_required`, `components_damaged`, `current_unhealthy`) ends the job, and the service sends `discard` for the staged version at once, so that a new attempt does not answer `busy` for up to 30 minutes; the cooldown of the helper still applies (F2). Otherwise the helper lints the staged unit files again and compares them, the staged helper and the known-good set with the recorded hashes. It writes `c_switching` with everything the next process needs, answers "accepted", switches `components/active` to the new set, writes `c_switched`, runs `daemon-reload` and ends with exit code 75 after the answer was written and the connection is closed. It runs nothing of the new set.
4. **The new helper starts** (systemd restarts the service; `RestartForceExitStatus=75`). Meanwhile a `status` call of the service waits on the socket or fails; within 60 seconds after the later of the acceptance of `activate` and the last successful `status`, the service reports that as `handing_over`, and the page shows "Die Bestandteile des Update Helpers werden ersetzt. Er startet dabei kurz neu." instead of an error (F4, A17 S4). The page shows the same text while the helper reports a component phase. Before it serves a request it proves itself: `active` names the new set, the helper file there and its own executable image have the recorded SHA-256, its embedded key id is the recorded one, its protocol range overlaps the old helper's. It writes `c_verified`, switches `components/known-good`, runs `daemon-reload`, writes `c_committed` with the new reference hash, and prunes older component sets.
5. **The release phases** of the base update (`switching`, `verifying`, `committed`), by the new helper, without a client request.
6. **If the new helper does not prove itself** it switches `active` back to `known-good`, runs `daemon-reload`, removes the staged release and the new set, writes the result `components_rolled_back` with a reason and ends with exit code 75; the old helper starts again. If it does not start at all, the recover unit does the same from the known-good binary. **If the release fails after `c_committed`**, only the release is rolled back; the new helper stays and the result is `rolled_back` with `components_updated`.

When only `oarfish.service` differs there is no component phase: the second confirmation is needed (the same dialog, with that name only, D6), and the helper runs `daemon-reload` after the switch of `current` and before the restart, and again after a switch back.

The journal of one successful component transaction (the lines of U8):

```text
phase=staging to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=staged to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=c_switching to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=c_switched to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=c_verified to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=c_committed to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=switching to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=verifying to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=committed to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
phase=idle to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok
```

The first four lines are written by the helper that received the requests, the others by the new helper.

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
| k2 | the unit lint (ADR 0010 U4 step 9, U15 rule 3, A15.3): `oarfish.service` of every release and, when the release carries the components, the three helper units, each against the rule tables compiled into the helper | `unit_rejected` |
| l | the bundle has components (the legacy shape is refused); the five files are compared with the installed ones, which must be readable | `requirements_not_met`, `components_damaged` |
| l2 | only when one of the four helper files differs: the key rule (U15 rule 5), the protocol rule (D5), the reference hash of the known-good set (E2) | `key_changed`, `requirements_not_met`, `components_damaged` |

The bundle hash (i) comes before the version rule (j). A changed or truncated copy of any real release therefore answers `checksum_mismatch` even when that version is installed or older. The version rule (j) comes before the component comparison (l). An older release with other helper bytes therefore answers `downgrade_refused`, not `installer_required`. The unit lint (k2) comes after the extraction and before the comparison (l). A release with a unit text that the rules refuse therefore answers `unit_rejected`, whether or not its files differ from the installed ones, and the staging tree is removed. The checks of l2 come before the component set or the release is moved into place, so a refusal leaves nothing behind. A difference that passes them is not an error: the release is staged, and the answer names the files (`differs`). The journal gets one line that names the unit and a reason from a closed set; no part of the unit text reaches the answer or the journal.

**The unit lint in short** (the complete rules, the accepted forms and the table are in ADR 0010 A15.3). A unit file is at most 16 KiB of valid UTF-8 without NUL or other control characters, without a line continuation, a specifier (`%`) or a duplicate key, with only the sections of its unit type, and with nothing but the expected `WantedBy=` in `[Install]`. The three helper units must equal a compiled-in accepted form after their comments, blank lines and the values of `Description=` and `Documentation=` are removed; the order of the lines counts. `oarfish.service` must contain the required baseline (every directive of `install/oarfish.service` except the extras, each with exactly its value, and `ExecStart` with a literal loopback address and port) and may add only the listed extras: `Description`, `Documentation`, `Restart` (`on-failure`, `always`), `RestartSec` (1 to 10 s), `TimeoutStopSec` (5 to 30 s) and `ProtectProc` (`invisible`, `noaccess`). Every other directive is rejected. The lint checks the release, not the installed units. It runs in `stage` and again in `activate`, on the staged files: each unit is read once, the same bytes are linted and hashed, and the hash must equal the one `stage` recorded (`unit_rejected`, `checksum_mismatch`; the staged release is then removed).

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
| `c_switching` | intent to switch `components/active` | `active` on the old set: the release is staged again (and removed at boot). On the new set: components rolled back |
| `c_switched` | `active` on the new set, the old helper ends | the new helper's own first start: self-check, forward. Any other process (the recover unit): components rolled back, `interrupted` |
| `c_verified` | the new helper proved itself | both links on the new set: committed. Otherwise as `c_switched` |
| `c_committed` | the components are committed | a running helper continues with the release phases; at boot the release is `aborted` and the components stay |
| `c_rolling_back` | rollback of the components running | finished; `rollback_failed` before any switch when the known-good set does not equal its reference hash or cannot be read (E5) |

Results: `success`, `rolled_back` (reasons `start_failed`, `health_timeout`, `restart_failed`, `interrupted`), `aborted`, `rollback_failed` (`rollback_target_damaged`, `rollback_unhealthy`, `known_good_damaged`, …), and `components_rolled_back` with one of eight reasons: `helper_mismatch`, `key_changed`, `protocol_mismatch`, `state_unreadable`, `interrupted`, `switch_failed`, `state_write_failed`, `reload_failed`. A result whose transaction replaced the components carries `components_updated: true`, whatever the outcome of its release part. The rollback first writes `rolling_back`. It then compares the hash of `releases/<from>/oarfish` with the hash recorded when `activate` started, switches `current` back, runs `reset-failed` and `restart`, and checks the old version with the same criteria. A damaged rollback target is never switched to. The rollback of the components compares the known-good set with its reference before it switches `active`, by the same rule (E5). The recover unit runs the same rule at every boot, before `oarfish.service` starts; when `OnFailure=` of the helper service starts it, it leaves the release phases to the next start of the helper, which rolls back with a restart (E6). Rollback does not restore data, so releases must stay N-1 compatible ([`RELEASING.md`](RELEASING.md)).

When `state.json` is missing or unreadable, the helper rebuilds it (U7). The last result is then `aborted` with the reason `state_rebuilt` and no versions, `components_rolled_back` with `state_unreadable` (P11), or `rollback_failed` with `known_good_damaged` (E5), and `needs_attention` is set. The service accepts these results (F3; the rule is in ADR 0010 A17.2) and the page has a sentence for each.

**What the page says for each result** (A17; the texts are in [`UI.md`](UI.md#state-and-label-texts)):

| Result | Page |
|---|---|
| `success` | the update is complete; with `components_updated` also that the helper and its units were replaced |
| `rolled_back`, `aborted` with `components_updated` (F31, row c6) | the release sentence, and that the new helper passed its check and stays installed |
| `components_rolled_back` | the new components did not work and the previous ones were restored, and one sentence per reason group: the new helper failed its check at its first start (`helper_mismatch`, `key_changed`, `protocol_mismatch`); interrupted (`interrupted`); the saved state could not be read (`state_unreadable`); a step of the switch failed (`switch_failed`, `state_write_failed`, `reload_failed`) |
| `rollback_failed` with `known_good_damaged` | the saved components are damaged and nothing was switched; a person with a shell is needed ([`INSTALL.md`](INSTALL.md#when-the-update-helper-needs-a-person)) |
| `aborted` with `state_rebuilt` | the helper rebuilt its state because the state file was missing or unreadable |
| `socket_restart_pending` (in `helper`) | a notice: the new socket unit takes effect after the next restart of the device |

The reason code is always shown as text under "Technische Einzelheiten".

## Cooldown and expiry

- After a rollback or a failure, also after `components_rolled_back`, the same version is refused for 15 minutes (`cooldown`). Any `activate` within 60 s of the previous one is refused as well. Both markers are stored in `state.json` with the time since boot (`CLOCK_BOOTTIME`), never the wall clock. A marker is void when the uptime is smaller than the uptime at which it was set (a reboot happened). After a reboot the cooldown may therefore end early, and this is accepted (U6).
- A staged release expires 30 minutes after `stage` (`stage_expired`). `status` and `activate` remove it, and so does every boot.

## When the installer is required

A helper that has the component transaction (G6.10 step b) never answers `installer_required` for a difference of the five compared files (A16, E1): a difference of the helper files selects the component transaction, a difference of `oarfish.service` alone the second confirmation. **No such helper has been released.** Every helper released so far (`v0.1.0-alpha.7` to `v0.1.0-alpha.10`) answers `installer_required` for the new bytes of the helper of step b, so **every device needs one installer run** to reach the first release that carries it ([`RELEASING.md`](RELEASING.md#release-notes-for-g610)).

After that, the installer is needed in these cases. The page shows the installer view for each of them: "Installer nötig", the sentence that this version cannot be installed from this page, a reason sentence of its own, and the fixed instruction of A12 (download `install.sh` from the release page of the version, read it, run it with `sudo sh install.sh`; settings and account are kept). No command is built from data of the API (F1, A17).

| Code of `stage` | Case | Reason sentence on the page |
|---|---|---|
| `installer_required` | a helper before step b and a release whose compared files differ (U9) | the version replaces privileged parts that the installed helper cannot replace by itself; the differing names are listed |
| `key_changed` | the release delivers a helper with another trust key (U15 rule 5, D-C60); never applied from the GUI | another trusted key; a change of the key is only possible with the installer |
| `unit_rejected` | a helper unit differs from the accepted forms of the installed helper in more than comments, blank lines, `Description=` or `Documentation=`, or `oarfish.service` is outside its rules. The installer does not lint, so an installer run still installs such a release. How a release changes a helper unit on purpose (the two steps) is in [`RELEASING.md`](RELEASING.md#release-procedure) | one of its unit files does not match the rules of the installed helper |
| `requirements_not_met` | the delivered helper implements a protocol other than P or P+1 (D5), or the release has the legacy bundle shape. The answer carries no cause, and the same code also means a minimum installed version, another architecture or a program that is not an arm64 ELF; by F1 the page shows the installer view for every `requirements_not_met` of `stage` | the version has requirements that this device does not meet with the installed helper |
| (no helper) | a device on `v0.1.0-alpha.6` or earlier: the bootstrap (U11) | the warning "helper not present" with the installation guide |

The bytes of the helper depend only on its code, the packages it links and the Go toolchain, never on the release version (A13, D-C80). A release that changes none of these and no unit text is a plain release update without a second confirmation.

The same codes from `activate` do not show the installer view: `unit_rejected` or `checksum_mismatch` at `activate` means that a staged file changed after `stage` (P14), and the page shows the failed job with the sentence of the code (A17 S1).

### What G6.10 still needs before G6 can close

Steps a to c are implemented; nothing of G6.10 ran on a device. Before the owner can decide whether G6 passes (U14):

1. **The first release with the helper of step b.** Its release notes say that every device needs one installer run ([`RELEASING.md`](RELEASING.md#release-notes-for-g610)). Before it is tagged: the observation on the device that the helper can read its own executable image under its sandbox (the assumption of D1, A15.1).
2. **Class D, F24:** a later real signed release that changes the helper bytes and a unit text within what the installed helper's lint accepts (A15.2 statement 2), applied from the GUI with both confirmations, with the timings, the journal and the audit entries.
3. **Class T, F25 to F31** with the test build and `OARFISH_FAULT_PROC` in the device test kit (not part of this repository), each row labelled as class T.
4. **The closeout list of U13** (below) for the component update: `write_bytes` and peak `VmHWM` of the service and of both helper processes, `systemd-analyze security` after the update, the redaction counts, the boot after a component update (D2: a changed recover unit takes effect at the next boot; D4: the socket flag is void after the reboot), and what the page showed during the hand-over (F4, its 60-second bound against the real gap).
5. **The owner's open questions:** C3 (the finishing audit entry), C4 (the password at the second confirmation, D7), the retention of failed releases, a release tooling check for the two-step rule, the wording of the U13 row F14 against U6, du versus Sie, specification 14.2 (G6.10 as one step), S8 (the steps after `known_good_damaged`).

## Evidence classes

U13 and D-C56 define three classes of evidence:

- **C**: an automated test in CI (fixtures, fake root, fake systemd, fake GitHub).
- **D**: on the reference device, with the production binaries and releases signed with the production key.
- **T**: on the reference device, with a test build of the helper (a throwaway public key and the build tag `faulthooks`) and releases signed with a throwaway key. Class T is never presented as evidence about the production key path.

The success row F19 and the genuine signed-release path must be class D.

## Failure matrix F1 to F32

The Device column says what was observed as expected on one device and in which class (D or T); "C only" means that the row, or the named part of it, is not planned on the device, and "not run, device step follows" means that the row belongs to G6.10 and has not run on a device: its tests are class C (fake root, fake systemd, the fake helper, jsdom), and its device evidence follows with the release after step c ([What G6.10 still needs](#what-g610-still-needs-before-g6-can-close)). See [Results on the reference device](#results-on-the-reference-device). Test names refer to the packages `cmd/oarfish-update-helper` (H), `internal/relverify` (R), `internal/updater` (U) and `internal/server` (S) below `backend/`, and the jsdom tests of the update page `frontend/src/pages/Update.test.ts` (P), cited by the start of their names.

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
| F12 | Release changes the helper or a unit, before G6.10 | C, D (from `v0.1.0-alpha.7` to `v0.1.0-alpha.8`, whose helpers differ, A13); T optional | U `TestInstallerRequiredCarriesTheFixedInstruction` (the service, for a helper before step b). The helper of step b no longer gives this answer: H `TestComponentComparison`, `TestStageComponentRefusals`; U `TestInstallerViewForEachInstallerCase`; P "F1, installer_required" | D (judged by hand, see [kit false stops](#kit-false-stops-and-observations)) |
| F13 | Hostile archive or sizes | C only | R `TestInspectRejectedEntryTypes`, `TestInspectRejectedPaths`, `TestInspectSizeMismatch`, `TestInspectDecompressionBomb`, `FuzzInspectTar` | C only |
| F14 | Damaged rollback target | C, T; injected after `activate` is accepted and before the rollback compares (the hash is recorded when `activate` starts) | H `TestDamagedRollbackTarget`, `TestRecoverRow10RollbackHash` | T |
| F15 | Wrong peer | C, D | H `TestPeerUIDMismatchClosesWithoutAnswer`, `TestRealPeerCheck` | D (judged by hand, see [kit false stops](#kit-false-stops-and-observations)) |
| F16 | Concurrent apply | C, D (helper `busy`); the second apply in the service job: C | H `TestConcurrentStageOneWins`, `TestStageInOtherPhasesIsBusy`; U `TestOneJobAtATimeAndTheStepsAreShown` | D |
| F17 | Helper not installed or stopped | C, D | U `TestStatusAnswerWithoutAHelper`; S `TestUpdateRoutesWithoutAnUpdater` | D (GUI text recorded by the owner) |
| F18 | Offline, rate limited, wrong clock | C, D (offline, wrong clock); the fake `403` with a reset header: C only | U `TestNetworkErrorsAreClassifiedByCodeOnly`, `TestAnUntrustedCertificateIsATLSFailure`, `TestRateLimitHandling` | D for offline and wrong clock (the fake `403` with a reset header stays C only) |
| F19 | Success | D | H `TestActivateSuccess`; U `TestApplyDownloadsVerifiesStagesAndActivates`, `TestReconcileWritesTheFinishingEntryOnceAndAcknowledges` | D |
| F20 | Cooldown survives the helper | C, T | H `TestCooldown`, `TestCooldownSameVersionBoundary` | T |
| F21 | Cooldown across a reboot | C, T | H `TestCooldownVoidAfterAReboot` | T |
| F22 | Staged release expires | C, D (the helper side); the GUI text "start again": C only (the GUI stages and activates in one job, except while the second confirmation waits, which step c covers in jsdom) | H `TestActivateStageExpired`, `TestStatusExpiresAStagedRelease`; U `TestActivateRefusalDiscardsTheStagedRelease` (F2, a refusal no longer leaves the release staged); P "the staged release expires while the dialog waits" | D (the "start again" text stays C only) |
| F23 | FIFO, link, directory, socket or growing file in the cache | C, D | H `TestCacheFileKinds`, `TestCacheFileGrowsDuringTheCopy`, `TestStageDeadline` | D (17 of 17 cases) |
| F24 | Component replacement, success | D (G6.10); the logic: C | H `TestComponentTransactionSuccess`, `TestComponentWriteAheadOrder`, `TestComponentRetention`, `TestHandOverExitsAfterTheAnswer`, `TestFirstStartHoldsTheLock`, `TestHelperRestartsAtMostTwice`, `TestCrashSweepComponentTransaction`; U `TestComponentUpdateFlowWithTheFakeHelper`; U `TestHandOverToleranceBound`, `TestNoHandOverWithoutAComponentSet` (F4); P "the second confirmation names every staged component", "F4: the course after the second confirmation", "success with components_updated" | not run, device step follows |
| F25 | Unit lint reject | C, T (G6.10) | H `TestStageUnitRejected`, `TestStageUnitLintOrder`, `TestLintParsing`, `TestLintHelperUnits`, `TestLintServiceBaseline`, `TestLintServiceUnlistedDirectives`, `TestLintServiceExtras`, `TestInstallUnitsPassTheLint`, `TestInstallUnitsAreTheFirstForms`, `TestServiceRulesMatchTheInstallUnit`, `FuzzUnitLint` (the lint at `stage`); `TestActivateChecksTheStagedFilesAgain` (the lint at `activate`); U `TestInstallerViewForEachInstallerCase` (F1); P "F1, unit_rejected" | not run, device step follows |
| F26 | Key rule | C, T (G6.10) | H `TestKeyRule`, `TestStageComponentRefusals`, `TestSelfCheck`, `TestCrashSweepComponentRollback`; U `TestComponentsRolledBackFlowWithTheFakeHelper`; U `TestInstallerViewForEachInstallerCase` (F1, the pre-check); P "F1, key_changed", "components_rolled_back, key_changed" (the post-check) | not run, device step follows |
| F27 | New helper does not start | C, T (G6.10) | H `TestNewHelperDoesNotStart`, `TestResetFailedOnlyWhenStartedByOnFailure`, `TestRecoverStartedByOnFailureLeavesReleasePhases` (E6), `TestCrashSweepNewHelperDoesNotStart`; P "components_rolled_back, interrupted" | not run, device step follows |
| F28 | New helper answers a wrong protocol | C, T (G6.10) | H `TestNewHelperWithAWrongProtocol`, `TestNewHelperWithOtherBytes`, `TestSelfCheck`, `TestStatusIsAnsweredForEveryVersion`; P "components_rolled_back, protocol_mismatch", "F1, requirements_not_met" (D5 at `stage`) | not run, device step follows |
| F29 | Power loss in each component phase | T (and C) (G6.10) | H `TestPowerLossInEachComponentPhase`, `TestCrashSweepComponentTransaction`, `TestCrashSweepRecoverComponentPhases`, `TestCrashSweepStageWithAComponentSet`, and one test per row: `TestComponentRowC1UnreferencedSet`, `TestComponentRowC2IntentBeforeTheSwitch`, `TestComponentRowsC3C4ActiveSwitched`, `TestComponentRowC5VerifiedNotCommitted`, `TestComponentRowC6CommittedBeforeTheRelease`, `TestSocketRestartPending` (c7), `TestComponentRowC8RollingBack`, `TestComponentRowC9Leftovers`; U `TestRebuiltStateResultsAreAccepted`, `TestRebuiltStateIsShownByTheService` (F3, P11); P "F3: a rebuilt state", "components_rolled_back, state_unreadable" | not run, device step follows |
| F30 | Damaged previous component copy | C, T (G6.10) | H `TestDamagedKnownGoodSet` (E5: the self-check, row c8, P11), `TestKnownGoodRecord`, `TestKnownGoodDigestEncoding`, `TestComponentsDamaged` (the comparison side); U `TestRebuiltStateResultsAreAccepted` (F3, E5); P "rollback_failed with known_good_damaged" | not run, device step follows |
| F31 | Components first, release then fails | T (G6.10); the logic: C | H `TestReleaseFailsAfterTheComponentsCommitted`, `TestCrashSweepComponentsThenReleaseFails`; U `TestReleaseRolledBackAfterTheComponentsFlowWithTheFakeHelper`; P "F31: rolled_back with components_updated", "aborted with components_updated" | not run, device step follows |
| F32 | Missing component confirmation | C (G6.10) | H `TestMissingComponentConfirmation`, `TestComponentsConfirmedRule`, `TestServiceUnitDifferenceAlone`; U `TestComponentUpdatePausesForTheSecondConfirmation`; U `TestActivateRefusalDiscardsTheStagedRelease` (`confirmation_required`, F2); P "the second confirmation sends components_confirmed", "oarfish.service alone (D6)", "\"Verwerfen\" in the second confirmation" | not run, device step follows |

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

The replacement of components (F24 to F32) belongs to G6.10. Step a added the unit lint, step b the component transaction and step c the dialog, the texts and the follow-ups F1 to F4 of the service, all with class C tests; nothing of them ran on a device. What the G6.10 closeout adds to this list is in [What G6.10 still needs before G6 can close](#what-g610-still-needs-before-g6-can-close). Before the first release that carries the code, the assumption of D1 must be observed on the device: the helper can read its own executable image under its sandbox (A15, A16.4).

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
- F24 to F32 belong to G6.10. The unit lint of step a, the component transaction of step b and the page, texts and service follow-ups of step c have class C tests only; they did not run on a device. The page of step c was checked in jsdom only, not in a browser. Not verified in particular: the read of the helper's own executable image under the unit sandbox, the restart of the real units after exit code 75, `daemon-reload` from the helper and from the recover unit, and the effect of a changed recover unit at the next boot.
- Not verified: screen reader use, other boards and models, Ubuntu, behaviour over many days, four simultaneous clients.
- Nothing is Verified for this device or for any feature. G6 stays open until the owner decides on G6.10 with its device evidence.
