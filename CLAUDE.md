# Oarfish project instructions

## Authoritative specification

The authoritative product and system specification is:

- `docs/PFLICHTENHEFT.md`

Read the relevant sections before implementing any gate. If code, a prompt, an ADR, or another document conflicts with the specification, stop and report the conflict before changing files.

## Product boundaries

- Oarfish is a GUI-first management platform for ARM SBCs and embedded Linux.
- Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit is the initial verified target.
- Ubuntu Server LTS ARM64 is a mandatory future target.
- Hardware detection and operating-system detection are independent concerns.
- Normal supported workflows must be fully GUI-driven.
- Oarfish must run without Docker, Podman, Tailscale, or an AI provider.
- Tailscale, Containers, and Oarfish Assist are optional plugins.
- Self-update is a core capability, not a plugin.
- No generic privileged shell executor is permitted.
- No Node.js, Python, database, Git, or build-tool runtime is permitted on the target.
- Builds happen on the MacBook or GitHub-hosted runners, never on the Pi.
- Distribution is pull-based through signed GitHub Releases.
- GitHub Actions must never connect directly to a development device.
- Do not introduce a self-hosted GitHub runner for this project.

## Architecture rules

- Backend: Go, standard library first, small reviewed dependencies.
- Frontend: Svelte 5, TypeScript, and Vite for build time only.
- Production frontend assets are embedded into the Go binary.
- Prefer procfs, sysfs, hwmon, systemd, journald, and other standard Linux interfaces.
- Do not hardcode the `pi` username, `/home/pi`, `/boot/config.txt`, `raspi-config`, `vcgencmd`, or a single network backend.
- Detect the OS through `/etc/os-release` and detect actual capabilities separately.
- Keep the core functional in Generic ARM64 mode.

## Delivery process

- Implement only the gate explicitly requested by the current prompt.
- Do not implement later gates in advance.
- Keep documentation and ADRs synchronized with the code.
- Run all relevant tests, checks, and builds before finishing.
- Never commit secrets, machine-specific credentials, API keys, or SSH private keys.
- Document unverified behavior honestly.

## Local-only information

Machine-specific paths, Pi hostnames, usernames, and local endpoints belong in `CLAUDE.local.md`, which must not be committed.
