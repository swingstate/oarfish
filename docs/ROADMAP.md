# Oarfish delivery roadmap

| Gate | Result |
|---|---|
| G0 | Pflichtenheft and technical baseline |
| G1 | Repository, skeleton, documentation, tests, and CI |
| G2 | Signed GitHub Release pipeline for Linux ARM64 |
| G3 | Pull-based installation script for Raspberry Pi OS |
| G4 | Oarfish Core, hardware, OS, and capability detection |
| G5 | Embedded dashboard with real metrics |
| (task) | Between G5 and G5.1, in this order, each in its own session and pull request and not a gate (D-C42): alert tuning (swap threshold, fewer irrelevant alerts), then a UI foundation task (appearance and structure only, no new functions) |
| G5.1 | Local access control and audit foundation. Scope per [ADR 0009](adr/0009-local-access-control-and-audit.md) (D-C45 to D-C48): Argon2id password hash, in-memory sessions, strict cookie and CSRF protection, setup token for the first account, rate limiting, own audit file, every `/api/v1` route behind a session; loopback only. Exit: state-changing endpoints reject requests without a valid session and without CSRF protection; audit entries contain no secrets; login verified on the Raspberry Pi. Also measured on the Pi: the password hash benchmark and the audit write load. Status: implemented and tested ([`AUTH.md`](AUTH.md)); the Raspberry Pi evidence (reference device, `v0.1.0-alpha.6`) was reviewed and G5.1 passed on 2026-11-02, with one open observation (an unexplained audit entry, see [`AUTH.md`](AUTH.md#open-observations-from-the-device-run)) |
| G6 | GUI self-update, verification, health check, and rollback; Update Helper as a separate privileged process. Design per [ADR 0010](adr/0010-update-helper.md) (accepted 2026-11-02, D-C53 to D-C63), delivered in the sub-steps G6.0 to G6.10 below, each with its own session, pull request and stop point. G6 closes only after G6.10 is accepted |
| G6.0 | Design stop: ADR 0010 accepted with the owner decisions, decisions recorded, specification 0.3.5, ADR 0009 amendment A6. Documentation only. Stop point: the owner says "merge" for the documentation pull request |
| G6.1 | Spike on the device, no product code: verifies the assumptions V1 to V13 of ADR 0010 (systemd behaviour of the hardened helper units, linked units, restart interplay, GitHub limits and redirects, clock). Stop point: the owner confirms or changes the hardening and the unit layout from the findings |
| G6.2 | Release tooling: bundle contract (helper binary and unit files), additive manifest fields, `bundlegen` checks and tests. Stop point: green dry-run release |
| G6.3 | Installer: installs and removes the helper, its units and the link layout, shared lock, tests with the fake root, `systemd-analyze verify` in CI. Stop point: the owner runs it on the device |
| G6.4 | Shared verification package (signature, checksums, manifest, SemVer, tar rules) with tamper, fuzz and replay tests. Stop point: green CI |
| G6.5 | Helper core: protocol, peer check, `status`, `stage`, `discard`, staging, state file, free space, cache file-type tests. Stop point: review of the privileged code |
| G6.6 | Helper activation: `activate`, `ack`, health check, rollback, boot recovery, retention, cooldown, expiry, fault hooks, crash tests. Stop point: review of the privileged code and its tests |
| G6.7 | Service side: GitHub client, check, download, advisory verification, helper client, routes, audit amendment A6 in code, `/healthz` version and `/readyz`. Stop point: review |
| G6.8 | Update page (Svelte). Stop point: the owner reviews the UI |
| G6.9 | Hardware evidence for the base update on the reference device, and documentation. Stop point: the owner reviews the evidence; G6 stays open |
| G6.10 | Replacement of the helper and its units by a GUI update (ADR 0010, U15): unit lint, component transaction, known-good copy, key rule, second confirmation. Until it is accepted, a release that changes the helper or a unit needs an installer run. Stop point: the owner decides whether G6 passes |
| G7 | systemd, journal, processes, reboot, and shutdown; Privilege Broker with a fixed allowlist. Does not start before G6.10 is accepted (D-C63) |
| G8 | Plugin runtime and permission model; broker capability check |
| G9 | Tailscale plugin |
| G10 | Docker and Podman plugin |
| G11 | Oarfish Assist, read-only MVP |
| G12 | Ubuntu Server LTS ARM64 qualification |
| (design stop) | LAN access and HTTPS design stop, before the MVP release, not before G6 (D-D10). Not a gate; until then remote access is an SSH tunnel and the listen guard stays (D-C48) |
| G13 | Hardening and MVP acceptance |
| Post-MVP | Armbian, Rockchip, additional boards, and external plugins |

Each gate receives its own Claude Code prompt. A later gate must not be implemented early unless the specification is formally changed.
