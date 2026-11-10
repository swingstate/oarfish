SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/swingstate/oarfish/backend/internal/version.Version=$(VERSION) \
	-X github.com/swingstate/oarfish/backend/internal/version.Commit=$(COMMIT)

LISTEN ?= 127.0.0.1:8484
# The development backend keeps its account, setup token and audit here
# (git-ignored). The service on a device uses $STATE_DIRECTORY from systemd.
DEV_STATE ?= $(CURDIR)/.dev-state
FUZZTIME ?= 10s

.DEFAULT_GOAL := help
.PHONY: help dev dev-backend dev-frontend deps test test-backend test-frontend test-release-tools \
	lint lint-backend lint-frontend fuzz frontend-build build build-arm64 bench-arm64 \
	release-bundle check-reproducible test-signing test-installer lint-shell clean \
	build-helper test-helper check-helper test-relverify-producer

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

deps: ## Install frontend dependencies from the lockfile
	cd frontend && npm ci

dev: ## Run the Go backend and the Vite dev server (UI on http://localhost:5173)
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	mkdir -p -m 0700 $(DEV_STATE)
	cd backend && go run ./cmd/oarfish -listen $(LISTEN) -state-dir $(DEV_STATE)

dev-frontend:
	cd frontend && npm run dev

test: test-backend test-frontend test-release-tools ## Run backend, frontend and release-tool tests

test-backend:
	cd backend && go test ./...

test-release-tools:
	cd scripts/bundlegen && go test ./...

test-frontend:
	cd frontend && npm test

lint: lint-backend lint-frontend ## gofmt, go vet and frontend type checks

lint-backend:
	@unformatted="$$(cd backend && gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	cd backend && go vet ./...
	@unformatted="$$(cd scripts/bundlegen && gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	cd scripts/bundlegen && go vet ./...

lint-frontend:
	cd frontend && npm run check

fuzz: ## Fuzz the os-release, device-tree, metrics, password-hash, update-helper request, update service and release-verification parsers (FUZZTIME=10s each)
	cd backend && go test -run '^$$' -fuzz '^FuzzParseOSRelease$$' -fuzztime $(FUZZTIME) ./internal/platform/operating_system
	cd backend && go test -run '^$$' -fuzz '^FuzzParseCompatible$$' -fuzztime $(FUZZTIME) ./internal/platform/hardware
	@for f in FuzzParseStat FuzzParseMeminfo FuzzParseFibTrie FuzzParseInet6 FuzzParseMountinfo FuzzParseRoutes; do \
		(cd backend && go test -run '^$$' -fuzz "^$$f\$$" -fuzztime $(FUZZTIME) ./internal/metrics/parse) || exit 1; \
	done
	cd backend && go test -run '^$$' -fuzz '^FuzzParsePHC$$' -fuzztime $(FUZZTIME) ./internal/auth
	@for f in FuzzDecodeRequest FuzzDecodeActivate FuzzDecodeAck FuzzDecodeState FuzzHealthzBody FuzzUnitLint; do \
		(cd backend && go test -run '^$$' -fuzz "^$$f\$$" -fuzztime $(FUZZTIME) ./cmd/oarfish-update-helper) || exit 1; \
	done
	@for f in FuzzHelperResponse FuzzReleaseList FuzzPlainNotes; do \
		(cd backend && go test -run '^$$' -fuzz "^$$f\$$" -fuzztime $(FUZZTIME) ./internal/updater) || exit 1; \
	done
	@for f in FuzzParseSignature FuzzParseChecksums FuzzParseManifest FuzzVersion FuzzInspectTar; do \
		(cd backend && go test -run '^$$' -fuzz "^$$f\$$" -fuzztime $(FUZZTIME) ./internal/relverify) || exit 1; \
	done

frontend-build:
	cd frontend && npm run build

build: frontend-build ## Build the host binary with embedded frontend into bin/oarfish
	cd backend && go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/oarfish ./cmd/oarfish

build-arm64: frontend-build ## Cross-build bin/oarfish-linux-arm64 (CGO_ENABLED=0)
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/oarfish-linux-arm64 ./cmd/oarfish

bench-arm64: ## Cross-build the Argon2id benchmark test binary to build/bench/ (not part of any release)
	mkdir -p build/bench
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -trimpath -o ../build/bench/oarfish-auth-bench.test ./internal/auth
	@echo "Copy build/bench/oarfish-auth-bench.test to the device; docs/AUTH.md, \"Measurements on the device\", has the commands."

release-bundle: frontend-build ## Build the unsigned release bundle + manifest into dist/release (same script as CI)
	scripts/release-bundle.sh

check-reproducible: ## Build the bundle twice from scratch and compare SHA-256 (rebuilds the frontend)
	scripts/check-reproducible.sh --rebuild-frontend

test-signing: ## Sign/verify tests with throwaway keys, incl. tamper rejection (needs minisign)
	scripts/test-signing.sh

test-installer: ## Installer tests against a fake root (needs minisign and OpenSSL 3; OARFISH_TEST_OPENSSL=... on macOS)
	scripts/test-installer.sh

lint-shell: ## shellcheck the installer (POSIX sh, all severities) and the repository scripts (warnings)
	shellcheck -s sh install/install.sh scripts/installer-container/run.sh scripts/installer-container/systemctl-stub.sh
	shellcheck -x -S warning scripts/*.sh

build-helper: ## Cross-build the update helper stub, reproducibly, to bin/oarfish-update-helper-linux-arm64 (part of the default release bundle since G6.3b)
	scripts/build-helper.sh --twice

test-helper: ## Update helper and its key package: vet (also linux/arm64, linux/amd64 and the faulthooks test build) and tests with -race
	cd backend && go vet ./cmd/oarfish-update-helper ./internal/trustkey
	cd backend && GOOS=linux GOARCH=arm64 go vet ./cmd/oarfish-update-helper ./internal/trustkey
	cd backend && GOOS=linux GOARCH=amd64 go vet ./cmd/oarfish-update-helper ./internal/trustkey
	cd backend && go vet -tags faulthooks ./cmd/oarfish-update-helper
	cd backend && go test -race -count=1 ./cmd/oarfish-update-helper ./internal/trustkey
	cd backend && go test -count=1 -tags faulthooks -run '^TestFaultHooks' ./cmd/oarfish-update-helper

check-helper: test-helper build-helper ## Everything CI checks for the update helper (tests, key copy, no fault hooks in the release binary, unit texts and their mutation test, reproducible build, bytes independent of the release version)
	scripts/check-helper-independent.sh
	scripts/check-helper-key.sh
	scripts/check-helper-hooks.sh bin/oarfish-update-helper-linux-arm64
	scripts/check-units.sh
	scripts/check-units.sh --self-test

test-relverify-producer: ## relverify accepts what scripts/bundlegen produces, both bundle shapes, signed with a throwaway minisign key (needs minisign and bash; the signing job of CI runs it)
	cd backend && OARFISH_RELVERIFY_PRODUCER=1 go test -race -count=1 -run '^TestProducerCompatibility$$' -v ./internal/relverify

clean: ## Remove build outputs (keeps the tracked embed placeholder)
	rm -rf bin dist build
	find backend/internal/web/dist -mindepth 1 ! -name .gitkeep -delete
