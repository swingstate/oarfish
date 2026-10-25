# Architecture (Gate G3 state)

This document describes what exists after Gate G3 and the boundaries later gates build on. The normative target architecture is section 8 of [`PFLICHTENHEFT.md`](PFLICHTENHEFT.md); where this document and the specification differ, the specification wins.

## Repository layout

```text
backend/                     Go module github.com/swingstate/oarfish/backend
  cmd/oarfish/                 process entry point: flags, signal handling, graceful shutdown
  internal/server/           HTTP handler: /healthz, static frontend, security headers
  internal/web/              go:embed of the built frontend (dist/)
  internal/version/          Version and Commit, injected with -ldflags
frontend/                    Svelte 5 + TypeScript + Vite
  src/                       App.svelte, lib/ (health client, i18n dictionary)
docs/                        specification, ADRs, roadmap, prompts, this file
install/                     install.sh (POSIX sh installer) and oarfish.service (unit, shipped in the bundle)
scripts/                     release packaging, signing, verification, installer tests, budget and smoke tests
  bundlegen/                 stdlib-only Go module: tar.gz, manifest.json, checksums.txt (not part of the product)
release/                     public signing key (oarfish-release.pub)
.github/workflows/           ci.yml, release.yml
Makefile
```

The Go module lives in `backend/` rather than the repository root so the Go toolchain never walks into `frontend/node_modules`, and the two toolchains stay clearly separated. All Go code is under `internal/`; nothing is importable from outside the module yet.

## Frontend delivery

```text
frontend/src ──vite build──▶ backend/internal/web/dist ──go:embed──▶ oarfish binary
```

- Vite writes straight into the Go embed directory (`backend/internal/web/dist`).
- Only `dist/.gitkeep` is tracked. It keeps `//go:embed all:dist` valid on a fresh checkout, so `go vet`, `go test` and `go build` work without Node.js. A binary built without a frontend build answers `503` for UI requests instead of a misleading `404`.
- The Vite config restores `.gitkeep` after each build, because `emptyOutDir` would delete it.
- Production assets are served by the Go binary; Node.js is not needed at runtime (ORB-CICD-003, ORB-NFR-008).

## HTTP surface

| Route | Behaviour |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}`, `Cache-Control: no-store` |
| `GET /` and below | embedded frontend; unknown paths `404`; `503` if no frontend is embedded |

All responses carry `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'`, `X-Content-Type-Options`, `X-Frame-Options` and `Referrer-Policy`. The server sets read-header, read, write and idle timeouts and a header size limit.

The process listens on `127.0.0.1:8484` by default (loopback only, ORB-ONB-003). Another address requires an explicit `-listen` flag. The port is a G1 default, not a specification value.

`/readyz`, `/api/v1/*`, Server-Sent Events and authentication are specified (sections 8.7, 13.1) but are not part of G1.

## Frontend

- Svelte 5 (runes), TypeScript strict mode, no router, no UI framework.
- UI strings come from `src/lib/i18n.ts` (German default, ORB-GEN-006); an English dictionary can be added with the same keys.
- The only feature is a health indicator backed by `/healthz`. No other functionality is shown, in line with the "no fake functions" rule (section 7.1).

## Boundaries prepared for later gates

Not implemented, only reserved by the layout and by CLAUDE.md rules:

- Hardware detection and OS detection are separate concerns (ADR 0002, spec 3.4, 8.3). Nothing in G1 reads `/etc/os-release`, procfs or sysfs.
- No privileged code exists. Privilege Broker and Update Helper are separate future components (spec 8.6).
- Plugins and self-update are out of scope until their gates (ADR 0003, ADR 0004). Release signing exists (below); the installer verifies signatures with the system `openssl`, but nothing in the Oarfish binary verifies signatures yet.

## Build and CI

- Builds use `CGO_ENABLED=0` for the Linux ARM64 cross-build, producing a static binary.
- CI runs on GitHub-hosted runners only and never connects to a device (ADR 0003, ORB-CICD-005). Third-party actions are pinned to commit SHAs (ORB-SEC-008) and workflows default to `contents: read`.
- CI jobs: Go checks, frontend checks, release bundle with size budgets and an ARM64 smoke test under qemu-user, reproducibility check, signing and tamper tests, dependency scan and SBOM trial.

## Release pipeline

```text
tag vX.Y.Z ──▶ ci (reused) ──▶ build ──▶ sign ──▶ publish ──▶ GitHub Release
                               no secret  secret   contents: write
                                          only here only here
```

- `scripts/release-bundle.sh` builds the bundle; `scripts/bundlegen` writes the tarball (sorted, commit-time mtimes, numeric owner 0, no gzip name or time), `manifest.json` and `checksums.txt`. The same script runs locally (`make release-bundle`) and in both workflows.
- `checksums.txt` is signed with minisign (Ed25519), `checksums.txt.sig` is published next to it (ADR 0005). The private key exists only as a GitHub Actions secret; the public key is committed as `release/oarfish-release.pub`.
- A tag push publishes; a manual run is a dry run that signs with a throwaway key and publishes nothing.
- A release holds the bundle, `manifest.json`, `install.sh` (the repository's installer with the release version written into one fixed line), `checksums.txt` (covers all of them), `checksums.txt.sig` and the SBOMs. The updater (Gate G6) does not exist yet.

Details: [`RELEASING.md`](RELEASING.md) and [`DEVELOPMENT.md`](DEVELOPMENT.md).

## Installer and the device layout (Gate G3)

```text
download (HTTPS, anonymous)         verify (before any change)               install
checksums.txt + .sig  ──▶  Ed25519 with system openssl 3.x and the  ──▶  user, dirs, release dir,
manifest.json, bundle      embedded key; then SHA-256 against the        current symlink, unit,
                           signed checksums.txt; bundle entry checks     start, /healthz, rollback
```

- **Trust model (ADR 0006).** The root of trust on the device is `install.sh`, which the user downloads over HTTPS from GitHub and can read before running it. The release public key is a constant in it (CI checks that it equals `release/oarfish-release.pub`). Everything else is authenticated against that key: `checksums.txt` by its Ed25519 signature, the manifest and the bundle by the SHA-256 values in it. The installer proves first that its `openssl` rejects a modified file (negative control), refuses OpenSSL older than 3.0 and LibreSSL, and accepts only legacy (`Ed`) minisign signatures with the expected key id. It does not protect against someone who controls the repository and replaces `install.sh` together with the key; the documentation says so and prints the key id for an independent comparison.
- **No execution before verification.** Nothing downloaded is run before the signature and hashes are verified. The first execution of the new binary is the health check after installation. The tools the installer runs are those of the distribution.
- **Layout (spec 8.2).** `/opt/oarfish/releases/<version>/` (immutable, root-owned), `/opt/oarfish/current` (relative symlink, switched with `mv -T`, an atomic rename), `/etc/oarfish` (`root:oarfish` 0750, `secrets/` 0700), `/var/lib/oarfish`, `/var/cache/oarfish`, `/run/oarfish` (created by systemd), unit in `/etc/systemd/system/oarfish.service`, user and group `oarfish`. `/usr/libexec/oarfish-update-helper` is not created; the update helper belongs to Gate G6.
- **Unit.** `install/oarfish.service` travels in the bundle, so it always matches its binary. It runs as `oarfish`, binds `127.0.0.1:8484` (D-C11), uses `StateDirectory`, `CacheDirectory` and `RuntimeDirectory`, and sandboxes the process (`NoNewPrivileges`, empty capability set, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, kernel and cgroup protections, namespace and realtime restrictions, `@system-service` system call filter). It has never been started by a real systemd yet. Hardening that later gates need relaxed (journal access, network management, a privilege broker) is added by those gates.
- **Idempotence and failure.** A re-run with the same version compares files byte for byte and changes nothing (no restart). A release directory that exists but differs is an error. All downloads and checks happen in a temporary directory; the first change to the system happens after everything verified. If a newly activated version fails its health check, the previous version and its unit are restored (a minimal safety net for the installer, not the update logic of Gate G6).
- **Privilege.** The installer runs once as root. It is not a general executor: its commands are fixed. The service is unprivileged. No privileged component is created.
- **No network exposure.** The installer never opens a port or changes a firewall.
- **Out of scope here:** authentication, onboarding, platform and hardware detection, metrics, self-update, plugins, reverse proxy and TLS.

User instructions: [`INSTALL.md`](INSTALL.md).
