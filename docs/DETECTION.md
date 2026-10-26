# Device detection (Gate G4)

Oarfish finds out, read-only, what it runs on: the **hardware** (board, SoC family, architecture, memory), the **operating system** (from `/etc/os-release`, plus the kernel) and the **capabilities** of the running system. The three are separate. The result is cached at startup, served at `GET /api/v1/device` and shown on the "Gerät" (Device) page.

Normative sources: Pflichtenheft 3.4 to 3.7, 6.2, 7.2, 8.3, 8.4, 12.4. Decisions: [ADR 0002](adr/0002-hardware-and-os-separation.md) (accepted) and [ADR 0007](adr/0007-detection-and-profiles.md) (**proposed**, not yet accepted).

**Status.** Everything here is tested against synthetic fixtures. **Nothing has been compared with a real Raspberry Pi yet** (see [Verification status](#verification-status)).

## Scope

In: read-only detection, the profile registry, the support tier, the read-only API and the Gerät page.

Not in G4: dashboard, metrics and charts (G5); authentication, accounts, audit and a privilege broker (not assigned to a gate yet); self-update (G6); systemd and journal management (G7); plugins; package, network and service adapters; signed profile packages; any Ubuntu-specific behaviour; running any external program. The API stays on loopback (`127.0.0.1:8484`, D-C11); there is no login yet, so it must not be exposed.

## Architecture

```text
backend/internal/platform/
  source/            read-only, bounded file access; the only package that touches the host
  hardware/          generic_board.go, raspberry_pi.go   (device tree, cpuinfo, meminfo, DMI)
  operating_system/  debian_family.go, raspberry_pi_os.go, ubuntu.go   (os-release, kernel)
  capabilities/      procfs, sysfs, systemd, journald, cgroup v2, CPU temperature, boot medium
  profiles/          registry, schema validation, embedded data/ (JSON)
  detector/          combines the four above and computes the support tier
  platformtest/      test helpers (fixtures, read recorder); only imported by tests
  testdata/          synthetic fixture trees (see testdata/README.md)
internal/server/     GET /api/v1/device (wire format and errors)
```

```text
              ┌─ hardware ──────┐
 file source ─┼─ operating_system ├─▶ detector ─▶ Result (cached) ─▶ server ─▶ /api/v1/device ─▶ GUI
 (fs.FS)      ├─ capabilities ──┤        ▲
              └─ profiles ──────┘        │ CPU temperature only: read again per request
```

Rules, each enforced by a test (`architecture_test.go`):

- `hardware` and `operating_system` never import each other, directly or through another package, and neither imports the HTTP server (ORB-HW-002, ORB-HW-010). Only `detector` knows both.
- No detection package imports `os/exec`, `net/http` or `syscall`. Detection reads files and nothing else (ORB-HW-006).
- Only `source` imports `os`. Everything else reads through an `fs.FS`: `os.DirFS("/")` in production, a fixture directory in tests. File names are slash paths without leading slash (`proc/cpuinfo`). `source.Host()` works with `CGO_ENABLED=0`.
- No detection file name ends in a platform suffix (`_arm64`, `_linux`, ...): Go would read it as a build constraint and leave the file out of builds on other platforms. That is why spec 8.3's example name `generic_arm64.go` is `generic_board.go` here (`TestArchitectureNoPlatformConstrainedFileNames`).
- Detection code contains no `pi` user name, no `/home`, no `/boot/config.txt`, no `raspi-config`, no `vcgencmd`.

An OS adapter (`operating_system.Adapter`) in G4 only *recognises* its system and reports a status. It carries no behaviour. `ubuntu.go` is that boundary for Ubuntu Server LTS ARM64: recognised, status `not-yet-qualified`. Qualification is Gate G12.

## What is read

Every file detection may open, and why. A test (`TestDetectionOpensOnlyAllowListedFiles`) records every access on every fixture and fails on anything else.

| File | Used for | Limit |
|---|---|---|
| `/proc/device-tree/model`, `/proc/device-tree/compatible` | board model; compatible strings (NUL-separated, most specific first) | 8 KiB |
| `/proc/cpuinfo` | the fields `Hardware`, `Revision`, `Model` only | 1 MiB |
| `/proc/meminfo` | `MemTotal` | 64 KiB |
| `/sys/class/dmi/id/sys_vendor`, `product_name` | vendor and model of ACPI systems without a device tree | 512 B |
| `/etc/os-release`, else `/usr/lib/os-release` | OS identity | 64 KiB |
| `/etc/rpi-issue` | tells Raspberry Pi OS from plain Debian | 1 KiB |
| `/proc/sys/kernel/osrelease` | kernel release | 256 B |
| `/run/systemd/system`, `/run/systemd/journal/socket`, `/sys/fs/cgroup/cgroup.controllers`, `/proc/meminfo`, `/sys/class` | existence probes for the capabilities | exists only |
| `/sys/class/thermal/thermal_zone*/{type,temp}`, `/sys/class/hwmon/hwmon*/{name,temp1_input}` | CPU temperature sensor | 64 B / 32 B |
| `/proc/self/mountinfo`, `/sys/dev/block/<major>:<minor>/uevent` (and its link) | boot medium | 1 MiB / 1 KiB |

### Raspberry Pi OS and `/etc/rpi-issue`

`/etc/os-release` alone cannot tell Raspberry Pi OS from Debian: the 64-bit images report `ID=debian` (older 32-bit images `ID=raspbian`). The Raspberry Pi OS adapter therefore matches `ID=raspbian`, or `ID=debian` together with an `/etc/rpi-issue` file that starts with `Raspberry Pi `. This is an OS fact, not hardware knowledge: a Raspberry Pi running Ubuntu or plain Debian has no such file and is still recognised as a Raspberry Pi by the hardware detector. *Not yet confirmed on a real image*; see [Verification status](#verification-status).

## Privacy

The API, logs, tests, fixtures and documentation must not contain or expose: CPU or board serial number, MAC addresses, machine-id, host name, user names, IP addresses, SSH data, environment variables.

- **Never read:** `/etc/machine-id`, `/etc/hostname`, `/sys/class/net/*/address`, `/proc/*/environ`, `/home`, anything SSH, `/etc/passwd`. The `Serial` line of `/proc/cpuinfo` is inside a file that must be read, so the parser keeps only three named fields and skips every other line; nothing in the result type could hold it. Tests plant such values in memory and check that they neither reach the API nor are opened (`TestDeviceEndpointNeverExposesIdentifyingData`).
- **Fixtures** are synthetic and are scanned for these patterns (`TestFixturesContainNoPrivateData`). Values needed to prove non-leakage are created in memory by the tests.
- **Untrusted input.** All file content is untrusted: every read is bounded, text is cleaned (invalid UTF-8 replaced, control characters removed, length capped), identifiers are validated against strict patterns, and file content is never used as a path or command. Paths for the boot medium are built from two integers (major, minor), and sensor paths from directory names that match `thermal_zone<digits>` or `hwmon<digits>`.
- The host name belongs to the dashboard (G5, ORB-DASH-001) and is not shown now.

## Hardware detection

`hardware.Detect(fsys, env)` returns `Info`: class, architecture, vendor, model, SoC family, compatible strings, revision code, total memory.

- Architecture comes from `runtime.GOARCH`/`GOOS` (injectable as `Env` for tests). Board knowledge is applied to ARM64 only.
- **Raspberry Pi** (`raspberry_pi.go`): recognised by a `raspberrypi,*` compatible string, or a model starting with `Raspberry Pi `. Vendor is "Raspberry Pi", model is the device-tree model, SoC family comes from the `brcm,bcmNNNN` compatible string (`/proc/cpuinfo` `Hardware` says BCM2835 on most models and is not used), and the revision code is the `Revision` field of cpuinfo. All of this from files; no Raspberry Pi tool is run.
- **Generic ARM64** (`generic_board.go`): model from the device tree, or from DMI on ACPI systems; no revision code, no SoC family.
- The same Raspberry Pi gives an identical `Info` under Raspberry Pi OS and under Ubuntu (`TestSameHardwareUnderRaspberryPiOSAndUbuntu`).

## Operating system detection

`operating_system.Detect(fsys)` returns `Info`: ID, ID_LIKE, VERSION_ID, PRETTY_NAME, adapter, adapter status, kernel release.

The `os-release` parser is strict about quoting and tolerant about everything else: a line that is not a well-formed `KEY=value` assignment is ignored as a whole; unknown keys are skipped; double-quoted values accept only the escapes `\\ \" \$ \``; single-quoted values take no escapes; bare values may not contain blanks, quotes, backslash, `$` or backtick; `$` and backticks in double quotes, NUL, control characters and invalid UTF-8 reject the value; lines over 4096 bytes and files over 64 KiB are ignored; `ID` and `ID_LIKE` must be lowercase identifiers. The fallback to `/usr/lib/os-release` happens when the primary file is missing, too large or has no usable `ID`.

| Adapter | Matches | Status |
|---|---|---|
| `raspberry_pi_os` | `ID=raspbian`, or `ID=debian` plus `/etc/rpi-issue` | `initial-target` (the first target, **not** verified) |
| `ubuntu` | `ID=ubuntu` or `ID_LIKE` contains `ubuntu` | `not-yet-qualified` |
| `debian_family` | `ID=debian` or `ID_LIKE` contains `debian` | `generic` |
| `generic_linux` | anything else, including no usable os-release | `unrecognised` |

## Capabilities

Found by probing standard interfaces, never by looking at the distribution name. Each has an `id`, `available`, `source` (the path or pattern that was probed, never file content) and, when unavailable, a machine-readable `reason`.

| id | Available when | Reasons |
|---|---|---|
| `procfs` | `/proc/meminfo` exists | `not_found` |
| `sysfs` | `/sys/class` is a directory | `not_found` |
| `systemd` | `/run/systemd/system` is a directory (the test of `sd_booted(3)`) | `not_found` |
| `journald` | `/run/systemd/journal/socket` exists | `not_found` |
| `cgroup_v2` | `/sys/fs/cgroup/cgroup.controllers` exists | `not_found` |
| `cpu_temperature` | a thermal zone or hwmon device with a plausible reading (−40 to 150 °C); a CPU-named sensor is preferred, then the lowest-numbered one | `no_sensor` |
| `boot_medium` | the root device is recognised (see below) | `no_root_mount`, `unrecognised_device` |

**Boot medium** is the device under `/`. `/proc/mounts` is not enough (Raspberry Pi OS often shows `/dev/root`), so the topmost mount at `/` is read from `/proc/self/mountinfo`, its device name from `/sys/dev/block/<major>:<minor>/uevent` (or from the mount source for file systems without a block major, such as btrfs). `mmcblk*` is `mmc`, `nvme*` is `nvme`, `sd*` is `usb` if its sysfs link has a `usbN` component, anything else is `unknown`. The API reports `unknown` as an unavailable capability.

**Minimum capabilities** are `procfs`, `sysfs` and `systemd` (systemd because the installer and the service model require it, spec 3.3). Without them the tier is Unsupported.

**Sensors.** The CPU temperature is the only value read per request. Everything else is read once at startup.

## Profiles

A profile is a JSON file under `backend/internal/platform/profiles/data/{soc,boards,generic}/`, embedded with `go:embed`. JSON is decoded strictly (`DisallowUnknownFields`, no trailing data, 64 KiB limit) and then checked by `Profile.Validate`. The registry loads at startup; an invalid embedded profile fails startup, and `TestShippedProfilesValidate` makes that a CI failure.

**Profiles contain no commands (ORB-HW-008).** An unknown field such as `command` or `script` is a decode error. Text fields may not contain shell metacharacters. Sensor bindings name a **handler** from a compiled-in allow-list (`sensor.thermal_zone.v1`, `sensor.hwmon.v1`); anything else is rejected. G4 ships no handler that writes anything. The bindings are validated and carried, but the CPU sensor itself is found generically; consuming bindings starts with the dashboard (G5).

| Field | Kinds | Meaning |
|---|---|---|
| `schema_version` | all | must be `1` |
| `kind` | all | `soc_family`, `board` or `generic` |
| `profile_id`, `version` | all | id (`[a-z0-9._-]`, unique) and `major.minor.patch` |
| `vendor` | all | plain text |
| `model` | board, generic | the model name; also matches the device-tree model with an optional ` Rev x.y` suffix (secondary match) |
| `soc_family` | soc_family, board | display name for a SoC family; a board names the SoC profile it belongs to |
| `compatible_patterns` | soc_family, board | device-tree compatible strings, exact or with one trailing `*`; the primary match |
| `os_constraints.adapters` | board | OS adapters the profile applies to; on any other the board is recognised but runs in Generic Mode |
| `kernel_constraints.min_release` | board (optional) | `major.minor`; an older kernel means Generic Mode |
| `support_tier` | board, generic | `verified`, `compatible`, `community`, `experimental` (board) or `generic` (generic profile); never `unsupported` |
| `maintainers` | all | plain text |
| `tested_combinations` | board | `os_adapter`, `os_version_id` (optional), `oarfish_version`, `functions_tested`, `evidence_ref` |
| `capabilities` | board (optional) | capability ids the board is expected to have; a missing one adds the limitation `expected_capability_missing` |
| `sensor_bindings` | soc_family, board (optional) | `{sensor, handler}` with a known sensor and an allow-listed handler |
| `storage_hints.boot_media` | board (optional) | usual boot media (`mmc`, `nvme`, `usb`) |
| `known_limitations` | all (optional) | limitation **keys** (`memory_512mb`), not text; the GUI translates them |
| `documentation_links` | all (optional) | a path under `docs/` or a `swingstate/oarfish` URL |

**The Verified rule (spec 12.4).** The validator rejects `support_tier: "verified"` unless a tested combination lists *every* core function (`install`, `dashboard`, `system_functions`, `self_update`, `rollback`, `plugins`) and carries an evidence reference. At runtime Verified is only returned for the exact tested combination (OS adapter and, if the entry names one, OS version); any other combination of the same board is Compatible. **No shipped profile claims Verified**, and a test fails if one does.

**Matching.** `compatible_patterns` are tried against the device-tree compatible list (most specific first); only if no board matches is the model string tried. A SoC profile is attached by compatible string. No board match means the built-in generic profile.

Shipped: `soc-bcm2837` (`brcm,bcm2837`), `raspberry-pi-zero-2-w` (`raspberrypi,model-zero-2-w`, tier `compatible`, partial evidence: the G3 install test only), `generic-arm64`.

### How to add a board profile

1. Capture the device's `compatible` strings from the board itself: `tr '\0' '\n' < /proc/device-tree/compatible`. Do not guess them. They contain no personal data.
2. Add `data/boards/<id>.json` (and a `data/soc/<family>.json` if the SoC family is new). Start from `raspberry-pi-zero-2-w.json`. Use only fields from the table above; keep text plain; list limitations as keys.
3. Pick the tier honestly: `compatible` unless every core function was tested on that hardware, in which case add the `tested_combinations` entry with `evidence_ref` pointing to a document that holds the evidence, then `verified`. Add a `community` profile only with a named maintainer.
4. Add the limitation keys you used to `frontend/src/lib/locales/de.ts` and `en.ts` (`limitation.<key>`). `TestEveryAPIValueHasATranslation` fails until you do.
5. Add a fixture under `testdata/` for the board (synthetic, no identifiers) and a case to the detector tests. Run `make test lint`.
6. A new handler is code, not data: it needs its own review and an entry in `KnownHandlers`.

## Support tier

| Tier | When | Reason (machine-readable) |
|---|---|---|
| Unsupported | not Linux | `not_linux` |
| Unsupported | not ARM64 | `not_arm64` |
| Unsupported | `procfs`, `sysfs` or `systemd` missing | `missing_minimum_capabilities` (limitations name the missing ones) |
| Generic | no board profile matched | `no_board_profile` |
| Generic | board matched, but its profile does not list this OS adapter | `os_not_covered_by_profile` |
| Generic | board matched, kernel older than `min_release` | `kernel_too_old` |
| Compatible | board profile matched, declared `compatible` | `profile_not_verified` |
| Compatible | profile `verified`, but this OS adapter/version was not the tested one | `verified_combination_not_matched` |
| Community / Experimental | profile declares it | `profile_community` / `profile_experimental` |
| Verified | profile `verified` and this exact combination was tested | `verified_combination` |

The user-facing explanation key is `support.<tier>.<reason>`; the translation files have a German and an English text for each. Additional limitations: `os_adapter_not_qualified` (Ubuntu), `os_unrecognised`, `generic_mode`, `raspberry_pi_no_board_profile`, `expected_capability_missing`, and the profile's own.

`support.board_actions_allowed` is true only when a board profile matched and the tier is not Generic or Unsupported. **Every future board-specific action must check it** (ORB-HW-007, ORB-NFR-015). G4 has no such action.

Results for the fixtures: Pi Zero 2 W + Raspberry Pi OS → Compatible; the same hardware + Ubuntu → Compatible with the limitation `os_adapter_not_qualified`; Pi Zero 2 W + plain Debian → Generic; Pi 4 + Raspberry Pi OS → Generic (hardware recognised as a Raspberry Pi, no board profile ships for it); unknown ARM64 → Generic; x86_64 → Unsupported.

## API: `GET /api/v1/device`

Read-only: only `GET`; any other method (including `HEAD`) is `405` with `Allow: GET`; a request body is `400`; `Cache-Control: no-store`. Unknown paths under `/api/` are a typed `404`. Errors have the form `{"error":{"code","message","recovery"}}` (spec 8.7). Unknown values are `null`, lists are never `null`. `schema_version` changes only when a field is changed or removed, not when one is added.

| Field | Meaning |
|---|---|
| `schema_version` | `1` |
| `hardware` | `vendor`, `model`, `soc_family`, `arch`, `revision_code`, `memory_total_bytes` |
| `os` | `id`, `id_like[]`, `version_id`, `pretty_name`, `adapter`, `adapter_status` |
| `kernel.release` | kernel release string |
| `profile` | `id`, `version`, `matched_by` (`compatible`, `model`, `generic_fallback`) |
| `support` | `tier`, `reason`, `explanation_key`, `limitations[]`, `board_actions_allowed` |
| `capabilities[]` | `id`, `available`, `source`, `reason` (only when unavailable) |
| `sensors.cpu_temperature_celsius` | number (0.1 °C) or `null`; read on every request |
| `boot_medium.type` | `mmc`, `nvme`, `usb`, `unknown` |

Example for the synthetic fixture `pi-zero-2w-raspios` (not a real device):

```json
{
  "schema_version": 1,
  "hardware": {
    "vendor": "Raspberry Pi",
    "model": "Raspberry Pi Zero 2 W Rev 1.0",
    "soc_family": "BCM2837",
    "arch": "arm64",
    "revision_code": "902120",
    "memory_total_bytes": 425373696
  },
  "os": {
    "id": "debian",
    "id_like": [],
    "version_id": "13",
    "pretty_name": "Debian GNU/Linux 13 (trixie)",
    "adapter": "raspberry_pi_os",
    "adapter_status": "initial-target"
  },
  "kernel": { "release": "6.18.50+rpt-rpi-v8" },
  "profile": { "id": "raspberry-pi-zero-2-w", "version": "1.0.0", "matched_by": "compatible" },
  "support": {
    "tier": "compatible",
    "reason": "profile_not_verified",
    "explanation_key": "support.compatible.profile_not_verified",
    "limitations": ["memory_512mb", "evidence_install_only"],
    "board_actions_allowed": true
  },
  "capabilities": [
    { "id": "procfs", "available": true, "source": "/proc/meminfo" },
    { "id": "sysfs", "available": true, "source": "/sys/class" },
    { "id": "systemd", "available": true, "source": "/run/systemd/system" },
    { "id": "journald", "available": true, "source": "/run/systemd/journal/socket" },
    { "id": "cgroup_v2", "available": true, "source": "/sys/fs/cgroup/cgroup.controllers" },
    { "id": "cpu_temperature", "available": true, "source": "/sys/class/thermal/thermal_zone0/temp" },
    { "id": "boot_medium", "available": true, "source": "/proc/self/mountinfo, /sys/dev/block" }
  ],
  "sensors": { "cpu_temperature_celsius": 43.3 },
  "boot_medium": { "type": "mmc" }
}
```

And the relevant part for the unknown-ARM64 fixture `generic-arm64-debian`:

```json
{
  "hardware": { "vendor": null, "model": "Synthetic Board A1", "soc_family": null, "arch": "arm64", "revision_code": null, "memory_total_bytes": 8356864000 },
  "os": { "id": "debian", "id_like": [], "version_id": "12", "pretty_name": "Debian GNU/Linux 12 (bookworm)", "adapter": "debian_family", "adapter_status": "generic" },
  "profile": { "id": "generic-arm64", "version": "1.0.0", "matched_by": "generic_fallback" },
  "support": {
    "tier": "generic",
    "reason": "no_board_profile",
    "explanation_key": "support.generic.no_board_profile",
    "limitations": ["generic_mode"],
    "board_actions_allowed": false
  },
  "sensors": { "cpu_temperature_celsius": 45.3 },
  "boot_medium": { "type": "nvme" }
}
```

## The Gerät page

Navigation has two entries, "Status" (the existing health view) and "Gerät" (`#/device`). The page shows the support status first (tier, explanation, profile, limitations), then hardware, operating system, sensors and boot medium, and the capability list with the probe source behind a disclosure. All texts come from `frontend/src/lib/locales/de.ts` (default) and `en.ts` (prepared, same keys).

| State | What the page shows |
|---|---|
| Compatible (Pi Zero 2 W + Raspberry Pi OS) | tier "Kompatibel" in the positive colour, its explanation (not fully tested on real hardware), limitations (512 MB, only the install was tested) |
| Generic | tier "Generisch" in the warning colour and the notice "Generischer Modus" with the explanation; unknown values labelled "Unbekannt"; no board-specific action |
| Unsupported | tier "Nicht unterstützt" in the error colour and a notice with a diagnostic hint (for example, install a 64-bit ARM system) |
| API unreachable | an error message and a "reload" button |

The only control on the page is "reload". There is nothing to change on the device in G4.

## Resource use

Detection reads about a dozen small files once at startup; the profile data is three small JSON files in the binary. The only per-request work is one sysfs read. ORB-NFR-001 asks for at most 35 MB idle RSS on the Zero 2 W: **no RSS measurement exists for G4**; measuring it on the device is a manual step. The binary-size change is reported in the pull request.

## Requirement to test

Each MUST requirement in scope maps to at least one test (spec 12.4). `TestTraceabilityTableIsComplete` parses this table: every in-scope requirement must be listed, and every named Go test or frontend test must exist.

| Requirement | Tests |
|---|---|
| ORB-HW-001 board, vendor, SoC, architecture, OS, kernel in API and GUI | `TestDetectPiZero2W`, `TestDetectPi4`, `TestPiZero2WWithRaspberryPiOSIsCompatible`, `TestDeviceEndpointPiZero2W`, `Device.test.ts "shows model, SoC, OS, kernel"` |
| ORB-HW-002 hardware and OS detection separate; same Pi under both OS | `TestSameHardwareUnderRaspberryPiOSAndUbuntu`, `TestSameHardwareUnderUbuntu`, `TestHardwareDetectionNeverReadsOSFiles`, `TestOSDetectionNeverReadsHardwareFiles`, `TestOSDetectionIsIndependentOfHardware`, `TestArchitectureHardwareAndOperatingSystemAreSeparate` |
| ORB-HW-003 OS from `/etc/os-release` (ID, ID_LIKE, VERSION_ID, PRETTY_NAME) | `TestParseOSReleaseFields`, `TestParseOSReleaseQuoting`, `TestParseOSReleaseBounds`, `TestDetectRaspberryPiOS`, `TestDetectFallsBackToUsrLibOSRelease`, `FuzzParseOSRelease` |
| ORB-HW-004 functions follow real capabilities | `TestEveryCapabilityIsReportedWithSourceAndReason`, `TestPiZero2WCapabilities`, `TestMinimalSystem`, `TestEmptyFileSource`, `TestExpectedCapabilityMissingIsALimitation` |
| ORB-HW-005 no `pi` user, no `/home/pi`, no fixed network backend | `TestNoUserNameOrHomeDirectoryDependency`, `TestArchitectureNoHardcodedUserHomeOrPiTools` |
| ORB-HW-006 standard interfaces first, no Pi tool | `TestArchitectureDetectionRunsNoProgramsAndOnlySourceTouchesTheHost`, `TestDetectionOpensOnlyAllowListedFiles`, `TestDetectPiZero2W` |
| ORB-HW-007 unknown ARM64 starts in Generic Mode | `TestUnknownARM64IsGenericMode`, `TestDeviceEndpointGenericARM64`, `TestDetectUnknownARM64Minimal`, `Device.test.ts "shows Generic Mode"` |
| ORB-HW-008 schema-validated, data-only profiles | `TestShippedProfilesValidate`, `TestParseRejects`, `TestKindSpecificRules`, `TestRegistryRelationChecks`, `TestMatch` |
| ORB-HW-009 support status and limitations visible; no unearned Verified | `TestNoShippedProfileClaimsVerified`, `TestVerifiedRule`, `TestVerifiedOnlyForTestedCombination`, `TestShippedProfilesNeverProduceVerified`, `TestUnsupportedSystems`, `TestEveryAPIValueHasATranslation`, `Device.test.ts "shows model, SoC, OS, kernel"` |
| ORB-HW-010 Ubuntu as its own OS adapter, proven by architecture test | `TestDetectUbuntuAdapterBoundary`, `TestAdapterNamesIncludeUbuntu`, `TestSameHardwareUnderUbuntu`, `TestArchitectureHardwareAndOperatingSystemAreSeparate`, `TestArchitectureDetectionDoesNotImportTheServer` |
| ORB-GEN-004 offer functions by detected capabilities | `TestUnknownARM64IsGenericMode`, `TestRaspberryPi4HasNoBoardProfile`, `TestBoardOnUncoveredOSFallsBackToGenericMode`, `Device.test.ts "lists every capability"` |
| ORB-GEN-006 UI text from translation resources | `i18n.test.ts "English has exactly the keys of German"`, `TestEveryAPIValueHasATranslation` |
| ORB-NFR-015 Generic Mode is safe on unknown ARM64 | `TestUnknownARM64IsGenericMode`, `TestDeviceEndpointGenericARM64`, `TestUnsupportedSystems`, `Device.test.ts "shows Generic Mode"` |
| Privacy rules | `TestFixturesContainNoPrivateData`, `TestDetectionOpensOnlyAllowListedFiles`, `TestDeviceEndpointNeverExposesIdentifyingData`, `TestDeviceResponsesContainNoPrivatePatterns`, `TestCPUInfoSerialIsNeverKept` |
| API is read-only | `TestDeviceEndpointAcceptsOnlyGet`, `TestDeviceEndpointRejectsARequestBody`, `TestUnknownAPIPathsGetATypedNotFound` |

Acceptance tests: AT-002 `TestDetectRaspberryPiOS`; AT-003 `TestDetectPiZero2W`, `TestPiZero2WCapabilities`; AT-004 `TestSameHardwareUnderRaspberryPiOSAndUbuntu`; AT-005 `TestUnknownARM64IsGenericMode`; AT-027 `TestArchitectureHardwareAndOperatingSystemAreSeparate`, `TestSameHardwareUnderUbuntu`. These are fixture tests; AT-002 and AT-003 on the real device are still to be run (below).

## Verification status

**Verified by tests (no hardware, no network, no root):** everything above, against synthetic fixtures; the fuzz targets for the `os-release` parser and the compatible parser run for a few seconds in CI (`make fuzz`).

`TestDetectOnTheRealHost` additionally runs detection against the real `/proc` and `/sys` of the machine running the tests (skipped when it is not Linux), so real files are parsed in CI on a generic x86_64 runner. It asserts completeness and cleanliness only, nothing about what the machine is.

**Not verified:**

- **No comparison with a real Raspberry Pi Zero 2 W.** The fixture values (compatible strings, cpuinfo layout, revision code, `/etc/rpi-issue`, kernel release, boot device) are written from knowledge of the kernel sources and of the G3 test report. If the real `compatible` strings differ from `raspberrypi,model-zero-2-w` and `brcm,bcm2837`, or the image has no `/etc/rpi-issue`, the Pi would be reported as Generic or as `debian_family`.
- **The service sandbox.** The unit file was not changed. `ProtectSystem=strict`, `ProtectKernelTunables`, `ProtectControlGroups`, `PrivateDevices` and `ProtectHome` should leave `/proc`, `/sys` and `/run/systemd` readable, but this was not run under a real systemd. If the Pi shows `unsupported` / `missing_minimum_capabilities`, or capabilities that are unavailable although they exist, the sandbox is the first suspect, and the fix is a narrow, documented relaxation of the unit as described in [ARCHITECTURE.md](ARCHITECTURE.md), not removing hardening.
- **Idle memory.** Not measured (ORB-NFR-001).
- **Ubuntu** on any hardware: only fixtures.
- **USB boot** classification has only been tested with a synthetic sysfs link.

### Checking a device after the release

Run on the device, with the service running (the output has no serial number, address or host name by design; look through it before sharing anyway):

```bash
curl -s http://127.0.0.1:8484/api/v1/device
journalctl -u oarfish -n 30 --no-pager | grep 'platform detected'
tr '\0' '\n' < /proc/device-tree/compatible     # to compare with the fixture
head -n 1 /etc/rpi-issue                         # confirms the Raspberry Pi OS marker
grep VmRSS /proc/$(systemctl show -p MainPID --value oarfish)/status   # memory, a manual step
```

Expected for a Pi Zero 2 W with Raspberry Pi OS: model `Raspberry Pi Zero 2 W Rev 1.0`, SoC `BCM2837`, adapter `raspberry_pi_os`, tier `compatible`, boot medium `mmc`, all capabilities available (journald depends on the image).
