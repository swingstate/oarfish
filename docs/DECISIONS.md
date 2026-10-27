# Oarfish decisions

## Confirmed

| ID | Decision |
|---|---|
| D-C01 | Product focus is ARM SBCs and embedded Linux. |
| D-C02 | Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit is the initial verified target. |
| D-C03 | Ubuntu Server LTS ARM64 is a mandatory future target. |
| D-C04 | Hardware and OS detection are separate. |
| D-C05 | GitHub Actions performs CI and release builds only. |
| D-C06 | No self-hosted runner and no direct GitHub-to-Pi deployment. |
| D-C07 | Initial installation and updates are pull-based. |
| D-C08 | Self-update belongs to the Oarfish Core. |
| D-C09 | Tailscale is an optional First-Party plugin. |
| D-C10 | Containers and Oarfish Assist are optional First-Party plugins. |
| D-C11 | Oarfish listens by default on 127.0.0.1:8484 (loopback only). |
| D-C12 | The ARM64 smoke test required by ORB-CICD-004 is an explicit acceptance criterion of Gate G2, not of Gate G1. |
| D-C13 | Public release channel: the repository and its GitHub Releases become public (anonymous HTTPS pull). No permanent GitHub token on the target device in the default architecture. |
| D-C14 | License: Apache-2.0. |
| D-C15 | The repository stays private until shortly before the MVP release. Before it becomes public, the full Git history must be reviewed for secrets and machine-specific values and the license must be in place. **Superseded by D-C17.** |
| D-C16 | Product, repository and binary are renamed from Orbit to Oarfish before the first release tag. Requirement IDs ORB-* remain unchanged for traceability. |
| D-C17 | The repository swingstate/oarfish becomes public after Gate G2 and a successful publication review, before Gate G3. The review covers a full history scan for secrets and machine-specific values, license check, fork-workflow approval, branch and tag protection. D-C15 is superseded by this decision. |
| D-C18 | Installer verifies the release signature with system openssl 3.x, see ADR 0006. `install.sh` defaults to the tag of the release it is attached to (stamped at build time), `--version` overrides; OpenSSL older than 3.0 and LibreSSL are refused. |
| D-C19 | Detection reads files only and executes no program in G4 (no `vcgencmd`, `raspi-config`, `lsblk`, `systemctl` or `uname`). All reads go through a read-only, size-limited file source; tests enforce that no detection package imports `os/exec` and that only allow-listed files are opened. See ADR 0007. |
| D-C20 | Profiles are JSON, decoded with `encoding/json` using `DisallowUnknownFields` and checked by an explicit validator. No YAML dependency. |
| D-C21 | Profiles are embedded in the core binary (`go:embed`) and ship inside the signed release. A profile update is a core release; no signed profile packages yet. |
| D-C22 | Tier rules: no shipped profile claims Verified without evidence for all core functions. Raspberry Pi Zero 2 W + Raspberry Pi OS = Compatible; the same board + Ubuntu = Compatible with the limitation "OS adapter not qualified". Board known but OS not covered by the profile, or kernel too old = Generic. No matching board profile = Generic. Not Linux, not ARM64, or `procfs`/`sysfs`/`systemd` missing = Unsupported. `board_actions_allowed` is true only for a matched board profile at Compatible or above. |
| D-C23 | Raspberry Pi OS is told from Debian by `/etc/rpi-issue` (or `ID=raspbian`), because the 64-bit images report `ID=debian` in `/etc/os-release`. This is an OS-side fact; hardware detection does not use it. |
| D-C24 | The minimum capabilities are `procfs`, `sysfs` and `systemd`. Without them the tier is Unsupported. |
| D-C25 | The device API (`GET /api/v1/device`, schema version 1) is served on loopback only (D-C11) and has no authentication until the topic is assigned to a gate (see D-D07). |
| D-C26 | Raspberry Pi models without a shipped board profile are Generic in the MVP, with the detected Pi capabilities, until a board or family profile exists (a data change). Specification 3.2 was aligned in version 0.3.2. |
| D-C27 | A new gate G5.1 "Lokaler Zugriffsschutz und Audit-Grundlage" is inserted after G5 and before G6. No other gate is renumbered. Scope: ORB-ONB-002, ORB-AUTH-001..003, ORB-SEC-004, ORB-SEC-005, ORB-SEC-006, ORB-SEC-007, ORB-SEC-010. Exit: state-changing endpoints reject requests without a valid session and without CSRF protection; audit entries contain no secrets; login verified on the Raspberry Pi. G5.1 has a design stop with an ADR (password hashing, session storage, audit storage with respect to microSD write load, first-start account setup). G6 depends on G5.1 PASS. The first state-changing API endpoint may only be built after G5.1 PASS. Specification 14.2 and 17 (D-07) were aligned in version 0.3.3. |
| D-C28 | The Update Helper (specification 8.6) is delivered in G6 as a separate privileged process, with a design stop and ADR. The Privilege Broker with a fixed allowlist (ORB-SEC-002) is delivered in G7. The broker capability check (ORB-SEC-003) is delivered in G8. |
| D-C29 | Until G5.1 is PASS the Oarfish binary refuses a non-loopback listen address (implemented as a small guard in Gate G5). After G5.1, a non-loopback address needs explicit configuration, a warning and enabled authentication (ORB-SEC-010). D-C11 stays valid. |
| D-C30 | The requirement IDs ORB-UPD-001..003 in specification section 6.4 (OS updates) are renamed ORB-OSU-001..003. Section 6.12 keeps ORB-UPD-001..008. A check found no other duplicate requirement IDs. Specification version 0.3.3. |

## Deferred, not blocking Gate G1

| ID | Decision | Due |
|---|---|---|
| D-D01 | Public release channel and repository visibility | Resolved by D-C13, D-C14 and D-C17 |
| D-D02 | Final open-source license | Resolved by D-C13, D-C14 and D-C17 |
| D-D03 | Exact Ed25519 signing implementation | Resolved by ADR 0005 (minisign 0.12) |
| D-D04 | Local admin authentication details | Gate G5.1 (design stop, ADR) |
| D-D05 | First container engine physically verified on Pi | Gate G10 |
| D-D06 | First Ubuntu hardware combination for Verified status | Gate G12 |
| D-D07 | **Open item, not a decision:** no gate owns local authentication, audit and the privilege broker. D-D04 names G4/G7, but G4 did not deliver it and G7 is not assigned it. | Resolved by D-C27 and D-C28 |
| D-D08 | **Open item, not a decision:** the requirement IDs ORB-UPD-001 to ORB-UPD-003 exist twice in the specification (section 6.4 and section 6.12). | Resolved by D-C30 |
| D-D09 | Password hashing method, session storage, audit storage and first-start account setup are decided in the G5.1 design stop with an ADR. | Gate G5.1 |
