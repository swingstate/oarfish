# Orbit decisions

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
| D-C08 | Self-update belongs to the Orbit Core. |
| D-C09 | Tailscale is an optional First-Party plugin. |
| D-C10 | Containers and Orbit Assist are optional First-Party plugins. |
| D-C11 | Orbit listens by default on 127.0.0.1:8484 (loopback only). |
| D-C12 | The ARM64 smoke test required by ORB-CICD-004 is an explicit acceptance criterion of Gate G2, not of Gate G1. |

## Deferred, not blocking Gate G1

| ID | Decision | Due |
|---|---|---|
| D-D01 | Public release channel and repository visibility | Before Gate G2 completion |
| D-D02 | Final open-source license | Before repository becomes public |
| D-D03 | Exact Ed25519 signing implementation | Gate G2 |
| D-D04 | Local admin authentication details | Gate G4/G7 |
| D-D05 | First container engine physically verified on Pi | Gate G10 |
| D-D06 | First Ubuntu hardware combination for Verified status | Gate G12 |
