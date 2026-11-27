# Oarfish

Oarfish is a GUI-first management platform for ARM single-board computers and embedded Linux.

The authoritative specification is [`docs/PFLICHTENHEFT.md`](docs/PFLICHTENHEFT.md). Architecture decisions live in [`docs/adr/`](docs/adr/), the gate sequence in [`docs/ROADMAP.md`](docs/ROADMAP.md). The standing rules for the implementation agent are in [`docs/AGENT_RULES.md`](docs/AGENT_RULES.md); only the files that configure the development tool and the files with machine-specific values (`*.local.md`) stay local and untracked.

## Status

Gates G1 to G7 have passed: CI, signed releases, the installer, detection, the dashboard, local access control and audit, the GUI self-update with rollback (G6) and the privilege broker for systemd services, reboot and shutdown (G7). The latest release is on the [release page](https://github.com/swingstate/oarfish/releases). Each gate was observed on one Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit, which Oarfish reports as **Compatible, not Verified**; nothing has run on Ubuntu Server (fixtures and adapter tests only), on other boards or on other Raspberry Pi models. What comes next, and in which order, is in [`docs/ROADMAP.md`](docs/ROADMAP.md) (decision D-C101).

What the binary does today:

- serves `GET /healthz` (`{"status":"ok","version":"...","pid":n}`, public, no device data) and `GET /readyz`
- protects everything else under `/api/v1` with one local administrator account: setup with a one-time token, login, logout, password change, in-memory sessions, CSRF, origin and Host checks, a rate-limited login and its own audit file ([`docs/AUTH.md`](docs/AUTH.md))
- detects the board, the operating system, the kernel and the capabilities from files, without running a program ([`docs/DETECTION.md`](docs/DETECTION.md))
- streams live metrics (CPU, memory, temperatures, storage, network with default route and DNS) and alerts while a browser watches ([`docs/DASHBOARD.md`](docs/DASHBOARD.md))
- updates itself from the GUI: it checks the release page, verifies the signature in a separate root helper, switches atomically, checks the health and rolls back on failure; a release that changes the privileged components needs a second confirmation ([`docs/UPDATE.md`](docs/UPDATE.md))
- lists systemd services and starts, stops or restarts them, and reboots or shuts down the device with the password, through a privilege broker with a closed list of typed operations ([ADR 0011](docs/adr/0011-privilege-broker.md))
- serves the pages Dashboard, Gerät, Aktivität, Update and Konto in German and English
- refuses to listen on anything but a literal loopback address (D-C29, D-C48)

Not yet: LAN access and HTTPS (D-D10), key rotation (D-D12), files, settings, network and storage pages, plugins (Tailscale, containers, Oarfish Assist), several users.

## Install (Linux ARM64 with systemd and OpenSSL 3)

```bash
curl -fsSLO https://github.com/swingstate/oarfish/releases/download/<tag>/install.sh
less install.sh                  # read it first
sudo sh install.sh --dry-run
sudo sh install.sh
```

The GUI listens on `127.0.0.1:8484` only; reach it from another machine with an SSH tunnel. The binary refuses any other listen address until LAN access and HTTPS are designed (D-C48, D-D10). On first start the GUI asks for the setup token: `sudo cat /var/lib/oarfish/setup-token` on the device. Release signing key id: `11F1C52777090BFC`. Full instructions, uninstall and troubleshooting: [`docs/INSTALL.md`](docs/INSTALL.md).

## Platform baseline

- Initial verified target: Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit
- Mandatory future target: Ubuntu Server LTS ARM64
- Primary runtime architecture: Linux ARM64
- Backend: Go. Frontend: Svelte 5 and TypeScript, embedded into the Go binary
- Delivery: signed GitHub Releases, a pull-based installer for the first installation, GUI self-update after it

The target device needs no Node.js, Python, database, Git, or container runtime.

## Quick start

Requirements on the development machine: Go (version in [`backend/go.mod`](backend/go.mod)), Node.js 22+, npm, make.

```bash
make deps    # npm ci
make build   # frontend build + host binary -> bin/oarfish
mkdir -m 0700 -p .dev-state
bin/oarfish -state-dir .dev-state   # listens on 127.0.0.1:8484 (only a loopback address is accepted, D-C48)
```

Open <http://127.0.0.1:8484/> (the setup page asks for the token in `.dev-state/setup-token`) or `curl http://127.0.0.1:8484/healthz`.

Other targets: `make dev`, `make test`, `make lint`, `make build-arm64`, `make release-bundle`. See [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).

## Repository layout

| Path | Content |
|---|---|
| `backend/` | Go module: `cmd/oarfish` entry point, `internal/` packages |
| `frontend/` | Svelte 5 + TypeScript + Vite (build time only) |
| `install/` | `install.sh` (POSIX sh installer) and the systemd units of the service, the update helper and the broker (shipped inside the bundle) |
| `docs/` | Specification, ADRs, rules for the implementation agent, roadmap, architecture, install and development guides |
| `scripts/` | Release packaging, signing, verification, installer tests and smoke tests |
| `.github/workflows/` | CI and release (hosted runners only; no deployment to devices) |
| `Makefile` | Developer entry points |

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how the pieces fit together.

## License

Oarfish is licensed under the Apache License 2.0 (SPDX identifier `Apache-2.0`), see [`LICENSE`](LICENSE). The repository is public (decisions D-C13, D-C14 and D-C17 in [`docs/DECISIONS.md`](docs/DECISIONS.md); checklist in [`docs/PUBLICATION_CHECKLIST.md`](docs/PUBLICATION_CHECKLIST.md)).
