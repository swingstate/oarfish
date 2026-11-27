# What counts as Verified (VER1)

**Status: accepted by the owner (D-C103, 2026-11-27).** The task VER1 applies these rules; the owner sets the status by a decision entry that names the evidence file. Nothing is Verified today, and the reference device stays Compatible.

## Rules

1. **Scope.** Verified is given to one combination: a board profile, an OS adapter and an OS version (`os_version_id`), for one Oarfish release (specification 3.6, 12.4). A later release keeps it only after the repeat run of item 6.
2. **Device.** One physical unit is enough, but the run is on a freshly written second SD card: the official image of the OS (name and date recorded), the official installer of the release under test, no files left from earlier runs. The development device does not count alone. The same card also gets one GUI update from the previous release, so that the update path is covered from an installed system.
3. **Checklist, class D only** (production key, production binaries; classes C and T do not count; classes in [`UPDATE.md`](UPDATE.md#evidence-classes)):
   - `install`: AT-006, AT-007, the installer rows of [`INSTALL.md`](INSTALL.md).
   - `dashboard`: AT-002, AT-003, AT-008, AT-021, AT-022 with the measured values of ORB-NFR-001 to ORB-NFR-009.
   - `system_functions`: AT-009 and the broker rows BF16, BF26 and BF27 of [ADR 0011](adr/0011-privilege-broker.md#failure-matrix), plus every BF row that is class D today.
   - `self_update` and `rollback`: AT-010 to AT-013 and the rows F19 and F24 of [`UPDATE.md`](UPDATE.md#failure-matrix-f1-to-f32); AT-012 uses the production key (a release signed with it that fails its health check, prepared for this purpose).
   - `plugins`: AT-014, AT-016, AT-018 and AT-020. This row applies only after G8.
   - Across all: AT-023 to AT-026, and AT-028 and AT-029 once D-D10 and Files exist.
4. **Record.** One evidence file per combination, `docs/evidence/<profile>-<os>-<version>.md`: device model and revision, OS image, kernel, release and commit, date, method and the result of each item; no host names, addresses or serial numbers. The profile's `tested_combinations` entry points to it with `evidence_ref`, and `support_tier` changes in the same pull request. The owner runs the device steps and merges; the implementation agent never sets Verified.
5. **Validator.** `support_tier: "verified"` needs a `tested_combinations` entry with every core function (`install`, `dashboard`, `system_functions`, `self_update`, `rollback`, `plugins`) and an `evidence_ref` (`backend/internal/platform/profiles/validate.go`). Because the `plugins` row applies only after G8, Verified is not set before G8.
6. **Repeat run.** Before each MVP release, and after any release that changes the component set, the units, the OS adapter or the detection for that board: on a fresh card, install, reboot, dashboard values, one service restart, one GUI update and one rollback. If it fails, the profile goes back to Compatible in the same release.
