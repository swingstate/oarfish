# Oarfish

Oarfish is a GUI-first management platform for ARM single-board computers and embedded Linux.

The authoritative specification is [`docs/PFLICHTENHEFT.md`](docs/PFLICHTENHEFT.md). Architecture decisions live in [`docs/adr/`](docs/adr/), the gate sequence in [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Status

**Gate G1 (repository foundation and CI) is implemented; Gate G2 (signed Linux ARM64 release pipeline) is in progress.** The repository contains a Go backend skeleton, a Svelte 5 + TypeScript frontend skeleton, an embedded production build, a GitHub Actions CI workflow, and a release workflow that turns a version tag into a signed GitHub Release ([`docs/RELEASING.md`](docs/RELEASING.md)).

What the binary does today:

- serves `GET /healthz` (`{"status":"ok"}`)
- serves the embedded frontend, which shows whether `/healthz` answers

What it does **not** do yet: hardware or OS detection, metrics, authentication, plugins, self-update, installation. Those belong to later gates. Nothing in this repository has been run on a Raspberry Pi yet.

## Platform baseline

- Initial verified target: Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit
- Mandatory future target: Ubuntu Server LTS ARM64
- Primary runtime architecture: Linux ARM64
- Backend: Go. Frontend: Svelte 5 and TypeScript, embedded into the Go binary
- Delivery: signed GitHub Releases (pipeline: Gate G2), pull-based installer and GUI self-update (later gates)

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
| `docs/` | Specification, ADRs, roadmap, architecture, development guide, gate prompts |
| `scripts/` | Release packaging, signing, verification and smoke tests |
| `.github/workflows/` | CI and release (hosted runners only; no deployment to devices) |
| `Makefile` | Developer entry points |

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how the pieces fit together.

## License

Oarfish is licensed under the Apache License 2.0 (SPDX identifier `Apache-2.0`), see [`LICENSE`](LICENSE). The repository becomes public after Gate G2 and a publication review, before Gate G3 (decisions D-C13, D-C14 and D-C17 in [`docs/DECISIONS.md`](docs/DECISIONS.md); checklist in [`docs/PUBLICATION_CHECKLIST.md`](docs/PUBLICATION_CHECKLIST.md)).
