# Oarfish delivery roadmap

| Gate | Result |
|---|---|
| G0 | Pflichtenheft and technical baseline |
| G1 | Repository, skeleton, documentation, tests, and CI |
| G2 | Signed GitHub Release pipeline for Linux ARM64 |
| G3 | Pull-based installation script for Raspberry Pi OS |
| G4 | Oarfish Core, hardware, OS, and capability detection |
| G5 | Embedded dashboard with real metrics |
| G6 | GUI self-update, verification, health check, and rollback |
| G7 | systemd, journal, processes, reboot, and shutdown |
| G8 | Plugin runtime and permission model |
| G9 | Tailscale plugin |
| G10 | Docker and Podman plugin |
| G11 | Oarfish Assist, read-only MVP |
| G12 | Ubuntu Server LTS ARM64 qualification |
| G13 | Hardening and MVP acceptance |
| Post-MVP | Armbian, Rockchip, additional boards, and external plugins |

Each gate receives its own Claude Code prompt. A later gate must not be implemented early unless the specification is formally changed.
