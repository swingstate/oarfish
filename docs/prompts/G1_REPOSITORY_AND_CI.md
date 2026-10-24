# Claude Code Prompt: Gate G1, Repository foundation and CI

You are working in the local Oarfish repository.

Your task is to implement **Gate G1 only: repository foundation and continuous integration**.

## Mandatory reading before changes

1. Read `docs/PFLICHTENHEFT.md` completely.
2. Read `CLAUDE.md`.
3. Read all accepted ADRs in `docs/adr/`.
4. Identify the requirements relevant to repository structure, target hardware, OS architecture, build process, security, and Gate G1.
5. Summarize the applicable constraints before editing files.
6. Report any material contradiction between this prompt and the specification. Stop if such a contradiction exists.

## Product baseline

- Oarfish is a GUI-first management platform for ARM SBCs and embedded Linux.
- Raspberry Pi Zero 2 W running Raspberry Pi OS Lite 64-bit is the initial verified target.
- Ubuntu Server LTS ARM64 is a mandatory future target.
- Hardware detection and OS detection must remain independent.
- The backend is written in Go.
- The frontend uses Svelte 5 and TypeScript.
- Production frontend assets are embedded into the Go binary.
- The target runtime must not require Node.js, Python, a database, Git, or a container runtime.
- GitHub-hosted Actions runners perform tests and builds.
- GitHub Actions must not connect to or deploy directly to the Raspberry Pi.
- Do not configure a self-hosted GitHub runner.
- Initial installation will later pull a signed GitHub Release through an install script.
- Subsequent updates will later be initiated through the Oarfish GUI and pulled from GitHub Releases.
- Tailscale is an optional plugin and must not be required for build, installation, updates, or recovery.

## Operating-system architecture constraints

- Do not hardcode the `pi` username, `/home/pi`, `/boot/config.txt`, `raspi-config`, `vcgencmd`, dhcpcd, NetworkManager, Netplan, or any single network backend.
- Future OS detection will use `/etc/os-release`.
- Future capability detection will inspect actual services, binaries, sockets, procfs, sysfs, hwmon, systemd, and journald.
- Prefer standard Linux interfaces.
- Raspberry Pi-specific utilities may only be optional enhancement providers.
- Ubuntu Core is explicitly outside the MVP.

## Scope of Gate G1

1. Validate or initialize the local Git repository.
2. Preserve the supplied specification, ADRs, roadmap, decisions, and prompt files.
3. Create a clean project directory structure.
4. Refine the root `CLAUDE.md` only when necessary and keep it concise.
5. Create a useful `README.md` for the repository foundation.
6. Create initial architecture and development documentation.
7. Create a minimal Go backend skeleton.
8. Create a minimal Svelte 5 and TypeScript frontend skeleton.
9. Add a minimal `/healthz` endpoint.
10. Build the frontend and embed its production assets into the Go binary.
11. Serve the embedded frontend from the Go application.
12. Add backend and frontend tests sufficient to validate the skeleton.
13. Add a GitHub Actions CI workflow for:
    - Go formatting checks
    - Go vet
    - Go unit tests
    - frontend dependency installation with a lockfile
    - frontend type checks
    - frontend tests
    - frontend production build
    - Go host build
    - Linux ARM64 cross-build with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`
14. Add a Makefile with at least:
    - `make dev`
    - `make test`
    - `make lint`
    - `make build`
    - `make build-arm64`
15. Add or refine `.gitignore` and `.env.example`.
16. Add `docs/ARCHITECTURE.md` and `docs/DEVELOPMENT.md`.
17. Add an ADR only if a new architectural decision is unavoidable. Do not duplicate existing ADRs.
18. Run all available tests and builds locally.
19. Document the result and any unverified assumptions.

## Required repository structure

At minimum:

```text
backend/
frontend/
docs/
docs/adr/
docs/prompts/
.github/workflows/
CLAUDE.md
README.md
Makefile
```

You may choose a more idiomatic Go module layout, but explain it and keep the frontend clearly separated.

## Explicitly out of scope for Gate G1

- GitHub Release publishing
- release signing
- install scripts
- Raspberry Pi installation or deployment
- SSH deployment
- Tailscale transport
- self-hosted GitHub runners
- self-update implementation
- authentication
- real system metrics
- hardware detection
- OS detection implementation
- systemd administration
- plugin runtime
- Tailscale integration
- Docker or Podman integration
- Oarfish Assist
- privileged helpers
- package installation on the Pi

Do not add placeholders that falsely imply these features work. Do not implement future gates in advance.

## GitHub repository handling

1. Run `gh auth status`.
2. Do not overwrite or reuse an existing remote repository without explicit confirmation.
3. If no remote exists, prepare the local repository first.
4. Before creating a remote, show the intended GitHub owner, repository name, visibility, and exact command.
5. Intended initial repository name: `oarfish`.
6. Intended initial visibility: private.
7. After user confirmation, create the private repository, set `origin`, and push the initial `main` branch.
8. Do not change repository visibility in Gate G1.

## Implementation quality

- Prefer the Go standard library.
- Keep dependencies minimal and justified.
- Commit a deterministic frontend lockfile.
- Keep generated artifacts out of Git.
- Use clear error handling and timeouts where applicable.
- Do not add telemetry.
- Do not add secrets or machine-specific values.
- Keep the initial UI visually clean, but do not spend Gate G1 on product feature design.

## Before finishing

1. Run the full test suite.
2. Run lint and type checks.
3. Run the production frontend build.
4. Run the host Go build.
5. Run the Linux ARM64 cross-build.
6. Start the host binary locally and verify `/healthz` plus the embedded frontend.
7. Review the Git diff.
8. Confirm that no secrets or machine-specific values are committed.
9. Update documentation to match the actual result.

## Final response format

Provide:

- concise summary of work completed
- files created or materially changed
- commands run
- test, lint, frontend build, host build, and ARM64 build results
- local run command
- remaining Gate G1 limitations
- any user confirmation still required for GitHub remote creation
- proposed commit message

Do not begin Gate G2.
