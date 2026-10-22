# Orbit Starter Pack v0.3

**Baseline date:** 22 October 2026  
**Purpose:** Complete input package for starting Gate G1 with Claude Code on macOS.

## Authoritative files

- `docs/PFLICHTENHEFT.md`: normative product and system specification
- `CLAUDE.md`: persistent project instructions for Claude Code
- `docs/prompts/G1_REPOSITORY_AND_CI.md`: executable prompt for Gate G1
- `docs/adr/`: accepted architecture decisions
- `docs/DECISIONS.md`: confirmed and deferred decisions
- `docs/ROADMAP.md`: gate sequence
- `START_HERE.md`: operator instructions

## Review copy

- `docs/export/Orbit_Pflichtenheft_v0.3.docx`: formatted Word version of the specification

## Baseline

- Initial verified platform: Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit
- Mandatory future platform: Ubuntu Server LTS ARM64
- Development: Claude Code on MacBook
- CI and release builds: GitHub-hosted Actions runners
- Distribution: signed GitHub Releases
- Initial installation: pull-based install script
- Updates: pull-based Orbit GUI updater with rollback
- Tailscale: optional First-Party plugin
- No self-hosted runner and no direct GitHub-to-device deployment

## Integrity

`SHA256SUMS.txt` contains SHA-256 digests for all package files except itself.
