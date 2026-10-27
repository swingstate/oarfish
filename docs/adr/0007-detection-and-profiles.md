# ADR 0007: Detection reads files only; JSON profiles embedded in the core

- Status: **Accepted**
- Date: 2026-10-27
- Gate: G4

## Context

Gate G4 adds detection of hardware, operating system and capabilities (spec 3.4, 3.5, 6.2, 8.3), a board and SoC profile registry (3.7, 8.4) and the support tiers (3.6). ADR 0002 already fixes the principle that hardware and OS detection are separate and that standard Linux interfaces come first. Four questions were left open by the specification and are settled here for the implementation.

## Decision

### 1. Detection reads files only; it executes nothing (in G4)

All detection goes through a read-only, size-limited file source (`fs.FS`; rooted at `/` in production, at a fixture directory in tests). It reads `/proc`, `/sys`, `/run/systemd`, `/etc/os-release` and a few marker files, and runs no program: not `vcgencmd`, `raspi-config`, `lsblk`, `systemctl` or `uname`. A test fails if a detection package imports `os/exec`. A second test lists every file detection may open and fails on any other, which also keeps identifying files (machine id, host name, network addresses, environment, home directories, SSH) out of reach.

Why: it is the strongest simple guarantee that detection is side-effect free and cannot be steered by file content (ORB-HW-006, "no generic privileged executor"); it is testable with plain fixture directories; it needs no tool on the target and no change to the service sandbox beyond reading.

Cost: some information is harder to obtain from files (the boot medium needs `/proc/self/mountinfo` and `/sys/dev/block`, not `lsblk`). Raspberry Pi tools may later be added as *optional enhancement providers* (spec 3.5), each by its own decision; none is needed for G4.

### 2. Profiles are JSON, decoded with the standard library; no YAML dependency

Profiles are JSON files, decoded with `encoding/json` using `DisallowUnknownFields`, then checked by an explicit validator. The standard library has no YAML parser; a YAML dependency would add a third-party parser to a binary that runs as a service, and YAML's features (anchors, tags, implicit typing) are exactly what a "data only, no commands" schema does not want. JSON with strict decoding gives the schema check, the no-commands guarantee (an unknown `command` field is an error) and no new dependency (CLAUDE.md: standard library first).

Profiles may reference only named, versioned handlers (`sensor.thermal_zone.v1`) from a list compiled into the core. G4 ships no handler that writes anything.

Cost: JSON has no comments. Documentation lives in `docs/DETECTION.md`.

### 3. Profiles are embedded in the core binary; no signed profile packages yet

Profiles ship inside the signed release, via `go:embed`. A profile update is a core release. The spec allows signed profile packages (8.4, ORB-NFR-016); that needs a package format, signature verification inside the binary (not available before G6) and a compatibility check, and is left to a later decision. The schema carries `schema_version` so that decision can add a loader without changing profile files.

### 4. Tier rules

- **No shipped profile claims Verified.** The validator rejects `verified` unless a tested combination lists all core functions (`install`, `dashboard`, `system_functions`, `self_update`, `rollback`, `plugins`) and an evidence reference (spec 12.4). At runtime Verified applies only to the exact tested combination; another combination of the same board is Compatible. Only the G3 install test exists, so the Raspberry Pi Zero 2 W profile is `compatible` and records that partial evidence.
- **Raspberry Pi Zero 2 W + Raspberry Pi OS = Compatible**; the same board + Ubuntu = Compatible with the limitation "OS adapter not qualified".
- **Board known, OS not listed in the profile, or kernel too old = Generic** (Generic Mode, no board-specific actions).
- **No matching board profile = Generic**, including a recognised Raspberry Pi of a model without a profile.
- **Not Linux, not ARM64, or `procfs`/`sysfs`/`systemd` missing = Unsupported**, with a reason and a diagnostic hint, never a crash.
- `board_actions_allowed` is true only for a matched board profile at Compatible or above; every board-specific action must check it.

### 5. Raspberry Pi OS is told from Debian by `/etc/rpi-issue`

The 64-bit Raspberry Pi OS reports `ID=debian` in `/etc/os-release`, identical to Debian. The adapter therefore also requires `/etc/rpi-issue` (written by pi-gen) or `ID=raspbian`. This is an OS-side fact; hardware detection does not use it, so a Raspberry Pi running Ubuntu or Debian is still a Raspberry Pi.

## Consequences

- Detection and API tests need no hardware, network or root.
- Adding a board is a data change plus a fixture and two translation entries, not a code change. A new handler is a code change.
- `/etc/rpi-issue` and the device-tree strings were confirmed on one real device (see Hardware confirmation). On other images they may differ; the effect is then a lower tier, not an unsafe one.
- Spec 3.2 listed "other Raspberry Pi models" as Compatible; this decision gives them Generic until a board or family profile exists (rule 4). Spec 3.2 was aligned in version 0.3.2. A Raspberry Pi family profile would make them Compatible; that is a data change.
- Not decided here: authentication of the API (it stays on loopback, D-C11), signed profile packages, optional Raspberry Pi enhancement providers, Ubuntu qualification (G12).

## Hardware confirmation

Confirmed on 2026-10-27 on one Raspberry Pi Zero 2 W Rev 1.0 (revision code 902120) with Raspberry Pi OS Lite 64-bit (Debian GNU/Linux 13, kernel `6.18.50+rpt-rpi-v8`), release `v0.1.0-alpha.3`, commit `b793b1069659f9ed85412b01523cc1f87e61df4f`. The board profile matched by its compatible string; the Raspberry Pi OS adapter matched through `/etc/rpi-issue`; the tier was Compatible with the reason `profile_not_verified`; all seven capabilities were available under the unchanged service unit. Record and limits: [INSTALL.md](../INSTALL.md#gate-g4-detection) and [DETECTION.md](../DETECTION.md#verification-status). This is one device and one image. Ubuntu, other boards and other Raspberry Pi models are not confirmed.
