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
| G5.1 | Local access control and audit foundation. Scope per [ADR 0009](adr/0009-local-access-control-and-audit.md) (D-C45 to D-C48): Argon2id password hash, in-memory sessions, strict cookie and CSRF protection, setup token for the first account, rate limiting, own audit file, every `/api/v1` route behind a session; loopback only. Exit: state-changing endpoints reject requests without a valid session and without CSRF protection; audit entries contain no secrets; login verified on the Raspberry Pi. Also measured on the Pi: the password hash benchmark and the audit write load. Status: implemented and tested ([`AUTH.md`](AUTH.md)); hardware verification pending, so not PASS yet |
| G6 | GUI self-update, verification, health check, and rollback; Update Helper as a separate privileged process |
| G7 | systemd, journal, processes, reboot, and shutdown; Privilege Broker with a fixed allowlist |
| G8 | Plugin runtime and permission model; broker capability check |
| G9 | Tailscale plugin |
| G10 | Docker and Podman plugin |
| G11 | Oarfish Assist, read-only MVP |
| G12 | Ubuntu Server LTS ARM64 qualification |
| (design stop) | LAN access and HTTPS design stop, before the MVP release, not before G6 (D-D10). Not a gate; until then remote access is an SSH tunnel and the listen guard stays (D-C48) |
| G13 | Hardening and MVP acceptance |
| Post-MVP | Armbian, Rockchip, additional boards, and external plugins |

Each gate receives its own Claude Code prompt. A later gate must not be implemented early unless the specification is formally changed.
