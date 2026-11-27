# VER1: what counts as Verified (proposal)

**Status: proposal of task C1 for the owner, not a decision.** Nothing in this file changes the support status of any board. Until the owner decides, the rules of the specification (3.6, 12.4) and of the profile validator apply, and the reference device stays Compatible.

## What exists

- **Fact.** Specification 3.6: Verified means a concrete board and operating system combination was tested on physical hardware, with every listed core function tested. Specification 12.4: a combination that was not physically tested is never Verified. Specification 16, item 8, asks for the Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit to be documented as Verified for the MVP.
- **Fact.** The profile validator accepts `support_tier: "verified"` only with a `tested_combinations` entry that lists every core function (`install`, `dashboard`, `system_functions`, `self_update`, `rollback`, `plugins`) and an `evidence_ref` (`backend/internal/platform/profiles/validate.go`, `schema.go`).
- **Fact.** The evidence classes C, D and T are defined in [`UPDATE.md`](UPDATE.md#evidence-classes). Today every device result comes from one device, mostly updated in place over many alpha releases.

## Proposal

1. **Scope.** Verified is given to one combination: a board profile, an OS adapter and an OS version (`os_version_id`), for one Oarfish release. A later release keeps it only after the repeat run of item 6.
2. **Devices.** At least two physical units of the board, not only the development device. One of them is not used for development.
3. **Fresh image.** On each unit: a freshly written official image of the OS (name and date of the image recorded), the official installer of the release under test, no files left from earlier runs. One of the two runs also does the GUI update from the previous release, so that the update path is covered from an installed system.
4. **Checklist, class D only** (production key, production binaries; class T and C do not count for Verified):
   - `install`: AT-006, AT-007 (reboot, the service starts by itself and can be reached), the installer rows of [`INSTALL.md`](INSTALL.md).
   - `dashboard`: AT-002, AT-003, AT-008, AT-021, AT-022 with the measured values of ORB-NFR-001 to ORB-NFR-009.
   - `system_functions`: AT-009 and the broker rows BF16, BF26 and BF27 of [ADR 0011](adr/0011-privilege-broker.md#failure-matrix), plus every BF row that is class D in A7 today.
   - `self_update` and `rollback`: AT-010 to AT-013 and the rows F19 and F24 of [`UPDATE.md`](UPDATE.md#failure-matrix-f1-to-f32), with a deliberately broken release for AT-012 (a release signed with the production key that fails its health check, prepared for this purpose).
   - `plugins`: AT-014, AT-016, AT-018 and AT-020 (the core without plugins and with a broken plugin), once G8 exists.
   - Across all: AT-023 to AT-026 (secrets, GUI completeness, keyboard use, offline operation), and AT-028 and AT-029 once D-D10 and Files exist.
5. **Record.** One evidence file per combination under `docs/` (for example `docs/evidence/<profile>-<os>-<version>.md`) with the device model and revision, OS image, kernel, release and commit, the date, the method and the result of each item; no host names, addresses or serial numbers. The profile's `tested_combinations` entry points to it with `evidence_ref`, and `support_tier` changes in the same pull request. The owner runs the device steps and merges the pull request; the implementation agent never sets Verified.
6. **Repeatability.** Before each MVP release, and after any release that changes a component of the component set, the units, the OS adapter or the detection for that board, a shorter repeat run on one unit with a fresh image: install, reboot, dashboard values, one service restart, one GUI update and one rollback. If it fails, the profile goes back to Compatible in the same release.
7. **Who decides.** The owner, by a decision entry that names the evidence file.

## Open questions for the owner

- Two units, or is one unit with a fresh image enough for the alpha phase?
- Does the deliberately broken release for AT-012 need the production key, or is a class T rollback acceptable for this one row?
- Is `plugins` part of the core functions for Verified before G8 exists, or is Verified only possible after G8 (as the validator demands today)?
