# Oarfish

Oarfish is a GUI-first management platform for ARM single-board computers and embedded Linux.

The authoritative specification is [`docs/PFLICHTENHEFT.md`](docs/PFLICHTENHEFT.md). Architecture decisions live in [`docs/adr/`](docs/adr/), the gate sequence in [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Status

**Gates G1 (repository foundation and CI), G2 (signed Linux ARM64 release pipeline), G3 (installer) and G4 (hardware, OS and capability detection, "Gerät" page) are done.** G4 is verified on one Raspberry Pi Zero 2 W; Ubuntu has not been tested on hardware. The repository contains a Go backend skeleton, a Svelte 5 + TypeScript frontend skeleton, an embedded production build, GitHub Actions CI, a release workflow that turns a version tag into a signed GitHub Release ([`docs/RELEASING.md`](docs/RELEASING.md)), and `install/install.sh`, an installer that pulls a release anonymously, verifies it and installs Oarfish as a systemd service ([`docs/INSTALL.md`](docs/INSTALL.md)).

What the binary does today:

- serves `GET /healthz` (`{"status":"ok"}`)
- detects, read-only and without running any program, the board, the operating system, the kernel and what the system can do, and serves the result at `GET /api/v1/device` ([`docs/DETECTION.md`](docs/DETECTION.md))
- serves the embedded frontend: a status view and the "Gerät" page, which shows the detected device and its support status

What it does **not** do yet: metrics, authentication, plugins, self-update. Those belong to later gates. The installer (Gate G3) and the detection (Gate G4, release `v0.1.0-alpha.3`) have run on one Raspberry Pi Zero 2 W, which Oarfish reports as Compatible, not Verified (see [`docs/INSTALL.md`](docs/INSTALL.md)). Nothing has run on Ubuntu Server (fixtures and the architecture test only), on other boards or on other Raspberry Pi models. `docs/INSTALL.md` and `docs/DETECTION.md` list what is and is not verified.

## Install (Linux ARM64 with systemd and OpenSSL 3)

```bash
curl -fsSLO https://github.com/swingstate/oarfish/releases/download/<tag>/install.sh
less install.sh                  # read it first
sudo sh install.sh --dry-run
sudo sh install.sh
```

The GUI listens on `127.0.0.1:8484` only; reach it from another machine with an SSH tunnel. Release signing key id: `11F1C52777090BFC`. Full instructions, uninstall and troubleshooting: [`docs/INSTALL.md`](docs/INSTALL.md).

## Platform baseline

- Initial verified target: Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit
- Mandatory future target: Ubuntu Server LTS ARM64
- Primary runtime architecture: Linux ARM64
- Backend: Go. Frontend: Svelte 5 and TypeScript, embedded into the Go binary
- Delivery: signed GitHub Releases (Gate G2), pull-based installer (Gate G3), GUI self-update (later gate)

The target device needs no Node.js, Python, database, Git, or container runtime.

## Quick start

Requirements on the development machine: Go (version in [`backend/go.mod`](backend/go.mod)), Node.js 22+, npm, make.

```bash
make deps    # npm ci
make build   # frontend build + host binary -> bin/oarfish
bin/oarfish    # listens on 127.0.0.1:8484
```

Open <http://127.0.0.1:8484/> or `curl http://127.0.0.1:8484/healthz`.

Other targets: `make dev`, `make test`, `make lint`, `make build-arm64`, `make release-bundle`. See [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).

## Repository layout

| Path | Content |
|---|---|
| `backend/` | Go module: `cmd/oarfish` entry point, `internal/` packages |
| `frontend/` | Svelte 5 + TypeScript + Vite (build time only) |
| `install/` | `install.sh` (POSIX sh installer) and `oarfish.service` (systemd unit, shipped inside the bundle) |
| `docs/` | Specification, ADRs, roadmap, architecture, install and development guides, gate prompts |
| `scripts/` | Release packaging, signing, verification, installer tests and smoke tests |
| `.github/workflows/` | CI and release (hosted runners only; no deployment to devices) |
| `Makefile` | Developer entry points |

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how the pieces fit together.

## License

Oarfish is licensed under the Apache License 2.0 (SPDX identifier `Apache-2.0`), see [`LICENSE`](LICENSE). The repository becomes public after Gate G2 and a publication review, before Gate G3 (decisions D-C13, D-C14 and D-C17 in [`docs/DECISIONS.md`](docs/DECISIONS.md); checklist in [`docs/PUBLICATION_CHECKLIST.md`](docs/PUBLICATION_CHECKLIST.md)).
