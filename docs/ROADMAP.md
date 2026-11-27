# Oarfish delivery roadmap

The status of every gate and task, and the order of the work from now (D-C101 item 5). A row says what holds now in at most three sentences (D-C102); the history is in the ADRs, [`UPDATE.md`](UPDATE.md) and [`DECISIONS.md`](DECISIONS.md). One task at a time, nothing in parallel. Updating the status line of a gate or task is part of its definition of done; the rules for the implementation agent are in [`AGENT_RULES.md`](AGENT_RULES.md) (D-C79), and each gate gets its own prompt. A later gate is not implemented early unless the specification is changed first.

## Done

| Gate or task | Result |
|---|---|
| G0 | Specification and technical baseline. |
| G1 | Repository, skeleton, documentation, tests and CI. |
| G2 | Signed GitHub Release pipeline for Linux ARM64 ([ADR 0005](adr/0005-release-signing.md)). |
| G3 | Pull-based installer for Raspberry Pi OS ([ADR 0006](adr/0006-installer-verification.md), [`INSTALL.md`](INSTALL.md)). |
| G4 | Hardware, OS and capability detection and the Gerät page ([ADR 0007](adr/0007-detection-and-profiles.md), [`DETECTION.md`](DETECTION.md)). |
| G5 | Dashboard with real metrics ([ADR 0008](adr/0008-dashboard-metrics.md), [`DASHBOARD.md`](DASHBOARD.md)); the alert tuning and the UI foundation tasks followed (D-C42). |
| G5.1 | Local access control and audit ([ADR 0009](adr/0009-local-access-control-and-audit.md), [`AUTH.md`](AUTH.md)). Passed on 2026-11-02 on the reference device, with one open observation in `AUTH.md`. |
| G6 | GUI self-update with verification, health check and rollback through the update helper, steps G6.0 to G6.10 ([ADR 0010](adr/0010-update-helper.md#current-state), [`UPDATE.md`](UPDATE.md)). Closed by D-C86 (2026-11-13), which lists the items accepted as open. |
| G7 | systemd services, reboot and shutdown through the privilege broker, steps G7.0 to G7.6 and G7-FIX1 ([ADR 0011](adr/0011-privilege-broker.md#current-state), amendments A1 to A8). Passed by D-C100 (2026-11-26); the journal and process views are reserve behind the MVP (D-C69). |
| (spike) S1 | Device spike: wireless data, storage, write baseline, throttling, DNS source, OS update display ([ADR 0008](adr/0008-dashboard-metrics.md), "Amendment after S1"; D-C78). |
| (task) D-C79 | Agent rules in the tree, `docs/AGENT_RULES.md` (PR #37). |
| (task) HB | Helper bytes independent of the release version (D-C80, ADR 0010 A13; PR #38). |
| (task) metrics test race | A racy test of the metrics sampler fixed, test file only (PR #40). |
| (task) U1 | UI shell: navigation, language choice, tokens, colour baseline, logo mark (PR #34; [`UI.md`](UI.md)). |
| (task) N0 | Default route and DNS on the network card, the installed memory on the Gerät page (ADR 0008 "N0 as implemented", PR #68). Released in `v0.1.0-alpha.15` and observed on the reference device (ADR 0008 N0.9). Open: the resolver-stub fallback, the SSID (with D-D10) and the reload after the next update. |
| (task) C1 | Documentation cleanup, D-C101 and D-C102, `debian:13` and the N0 fuzz targets in CI, the signing approval in `release.yml`, board profile 1.2.0 (limitation `evidence_one_device`). Merged (PR #70). |
| (task) C2 | Specification 0.4.6 after D-C101, and the owner's choices as D-C103: OS and kernel updates through the GUI in the MVP, history and G12 behind it, the rule for Verified ([`VERIFIED.md`](VERIFIED.md)). Status: pull request open. |
| (probe) UBU0 | Ubuntu Server 24.04 LTS on the reference board with `v0.1.0-alpha.14` and `v0.1.0-alpha.15`, class D, one device and one image ([`UBUNTU_PROBE.md`](UBUNTU_PROBE.md)). Install, units, detection, dashboard, GUI update and reboot work; the services list fails because `list-unit-files` takes 17 s there and blocks PID 1 (U1). Fix proposals U1 and U4 and inputs for DB1 and N1 are in the document; status: pull request open. |

Nothing is Verified; the reference device (one Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit) is Compatible.

## Next, in this order (D-C101 item 5)

| Step | Scope and status |
|---|---|
| (design block) DB1 | On paper, ADRs only: **D-D10** LAN access and HTTPS without an SSH tunnel, with the update health over the person's path and commit-confirmed network and certificate changes (D-C101 item 4), the twelve loopback assumptions of the interim assessment, the login limiter lockout (finding F-5 item 1) and the inputs of D-D10; **D-D12** an offline root key per maintainer and a threshold (for example 2 of 3) that signs the list of valid release keys; **the privileged system service** (D-C101 items 2 and 3, and the operations and display of OS and kernel updates (OSU1): one service or two with a common pattern, a catalogue that grows without an installer run and covers G9, G10 and the OS and kernel updates of OSU1 (operations and display), the authentication of root actions, the reboot loop of ADR 0011 "Risks"). Not started. |
| (transition) TR1 | One transition that implements D-D10, D-D12 and the layout of the privileged service, so that no device needs the installer again after it (D-C101 items 1 and 3). Not started. |
| (design stop) F1, (task) F2 | Files, read-only: the first fixed roots, block list, size limit and acceptance criteria with an ADR, then the implementation; root reading through typed operations after authentication (D-C101 item 5; D-C75). Not started. |
| (design stop) H1 | The settings store only, which SET1 needs (D-C101 item 6; D-C73). Input from S1: the medium is an SD card, the idle write baseline is about 392 KB per hour. Not started. |
| (task) SET1 | Settings page in the light procedure (one PR, short row, no ADR amendment unless root code changes): language (D-C77, ORB-GEN-006), the off switch of the update notice (D-C68), later the certificate fingerprint (ORB-SEC-011). Depends on H1. Not started. |
| (task) N1 | Network page in the light procedure: signal strength from `/proc/net/wireless` and IP details (prefix, gateway, IPv6); the SSID needs netlink and comes with D-D10 (D-C72). Not started. |
| (task) ST1 | Storage page in the light procedure, read-only: file systems, mounts, capacity, the detected boot medium (ORB-STO-001 to ORB-STO-004, D-C69 item 4). The backend already parses `mountinfo` and detects the boot medium. Not started. |
| (task) ONB1 | First-start assistant in the light procedure (ORB-ONB-001: account, binding, device name, detected platform, optional plugins); the binding part follows D-D10, the plugin part follows G8. Not started. |
| (probe) ENG0, (design) G8.0 | Container engine probe on the device, then the G8 design (plugin process model). Not started. |
| G8 | Plugin runtime and permission model; the capability check at the privileged side (ORB-SEC-003). |
| G10 | Docker and Podman plugin; the service's access to the engine is decided in a design stop before G10 (D-C75). |
| G9 | Tailscale plugin, installed through the GUI by a typed privileged operation, with its own design stop (D-C75). |
| G11 | Oarfish Assist, read-only MVP; it never gets root (D-C101 item 2). |
| (task) VR1 | Visual refresh of the UI, including U2 (theme switch and header); U2 has no row of its own (D-C103). Not started. |
| (task) OSU1 | OS and kernel updates through the GUI (ORB-OSU-001 to ORB-OSU-003, D-C101, D-C103): design in DB1, build after TR1. Not started. |
| (task) VER1 | Evidence for the status Verified (specification 3.6, 16 item 8), under the rules of [`VERIFIED.md`](VERIFIED.md) (D-C103); part of the G13 acceptance. Not started. |
| G13 | Hardening and MVP acceptance. |

## Specification items without a step

Items the specification asks for that no step covers yet. Each keeps the proposal of the C1 report (D-C103 item 6).

| Item | Specification | Proposal |
|---|---|---|
| OpenAPI description of `/api/v1` | 8.7 | Plan in G13, with a test that every route is in the file. |
| `GET /api/v1/about` | 13.1 | Plan with SET1 (an "About" part of the settings page). |
| Diagnostics bundle with preview and redaction | 13.1 | Plan with G11, which needs the same redaction; otherwise behind the MVP. |
| Recovery flag to start without plugins | 13.3 | Plan with G8, together with the emergency path to disable a plugin. |
| `/etc/oarfish/config.json` | 8.2 | Decide in DB1: the service cannot write `/etc` (`ProtectSystem=strict`); either the privileged service writes it, or 8.2 names the settings store of H1. |
| Documented backup path | 13.2 | Plan in G13, after TR1 has fixed the file layout. |

## Behind the MVP

| Item | Note |
|---|---|
| (task) H2 | 24-hour history of CPU, memory and temperature, behind the MVP (D-C101 item 6); the specification change is made (4.1, 12.3; D-C103). |
| G12 | Full Ubuntu Server LTS ARM64 qualification, behind the MVP (D-C101 item 6, specification 14.2); the MVP keeps the preparation (specification 16 item 10). |
| Reserve | Journal and process views (ORB-LOG-001, ORB-PRC-001; D-C69). |
| (task) T1 | Throttling and undervoltage warnings (ORB-DASH-004). S1: undervoltage is readable from the `rpi_volt` hwmon alarm file; a frequency cap is not available as a flag. |
| Web terminal | An optional expert plugin with its own design and decision; no administration task needs it (D-C101 item 1). |
| Post-MVP | Armbian, Rockchip, additional boards and external plugins. |
