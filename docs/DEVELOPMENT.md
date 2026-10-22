# Development

Development happens on macOS (or any machine with the toolchain). Nothing is built on the target device.

## Prerequisites

| Tool | Version |
|---|---|
| Go | the version in `backend/go.mod` (CI reads it from there) |
| Node.js | 22 LTS or newer (`frontend/.node-version`, `engines` in `package.json`) |
| npm | bundled with Node.js |
| make | any recent version |

Node.js is a build-time tool only. The Orbit binary and the target device do not need it.

## Make targets

| Target | What it does |
|---|---|
| `make deps` | `npm ci` in `frontend/` (installs exactly the lockfile) |
| `make dev` | Go backend on `127.0.0.1:8484` plus the Vite dev server; open <http://localhost:5173>. Vite proxies `/healthz` to the backend. `LISTEN=<addr>` changes the backend address (also update the proxy target in `frontend/vite.config.ts`). |
| `make test` | `go test ./...` and `vitest run` |
| `make lint` | gofmt check, `go vet`, `svelte-check` (TypeScript and Svelte type check) |
| `make build` | frontend production build, then host binary `bin/orbit` with the frontend embedded |
| `make build-arm64` | same, cross-compiled with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` to `bin/orbit-linux-arm64` |
| `make clean` | removes `bin/` and the generated frontend build (keeps `dist/.gitkeep`) |

`make build` injects `Version` (from `git describe`) and `Commit` via `-ldflags`; `bin/orbit -version` prints them.

## Running without make

```bash
cd frontend && npm ci && npm run build   # writes backend/internal/web/dist
cd ../backend && go run ./cmd/orbit      # http://127.0.0.1:8484
```

## Working on the backend only

`go vet`, `go test` and `go build` work without a frontend build, because `backend/internal/web/dist/.gitkeep` is tracked. UI requests then return `503`.

## Dependencies

- Backend: standard library only. There are no third-party Go modules.
- Frontend (all dev dependencies, pinned exactly in `package.json`, locked in `package-lock.json`): `svelte`, `vite`, `@sveltejs/vite-plugin-svelte`, `typescript`, `svelte-check`, `@tsconfig/svelte`, `vitest`, `jsdom`, `@testing-library/svelte`, `@types/node`. They are build and test tooling and do not ship to the device.

Adding a dependency should be justified in the change that adds it.

## CI

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`:

1. **backend**: gofmt, `go vet`, `go test -race`
2. **frontend**: `npm ci`, `svelte-check`, `vitest`, `vite build`, upload of the built assets
3. **build** (needs both): downloads the frontend assets, builds the host binary and the `linux/arm64` binary, checks that the ARM64 output is an aarch64 ELF, and records its size in the job summary

Rules for editing workflows:

- Pin every third-party action to a full commit SHA with the version as a comment (ORB-SEC-008).
- Keep `permissions` minimal.
- Never add SSH keys, device hostnames, Tailscale access, or a self-hosted runner (ADR 0003).

## Local-only information

Device hostnames, usernames, endpoints and credentials belong in `CLAUDE.local.md` (git-ignored) or an untracked `.env`. `.env.example` documents variables the code reads; Gate G1 reads none.

## Known limitations at Gate G1

- The CI workflow has been written but not yet executed on GitHub; the remote repository does not exist yet.
- The ARM64 binary has been cross-built and inspected, not executed on ARM64 hardware or under emulation (ORB-CICD-004 asks for a smoke test; that is not part of G1).
- The embedded UI was verified by fetching the served HTML and assets, and the frontend by unit tests in jsdom. It has not been opened in Chromium, Firefox or Safari.
- No dependency vulnerability scan or SBOM yet (ORB-SEC-009, SOLL); planned with the release pipeline.
- No frontend bundle-size budget check yet (ORB-NFR-006, SOLL). Current gzip size is about 12 KB for JS plus 0.5 KB for CSS.
