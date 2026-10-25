SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/swingstate/oarfish/backend/internal/version.Version=$(VERSION) \
	-X github.com/swingstate/oarfish/backend/internal/version.Commit=$(COMMIT)

LISTEN ?= 127.0.0.1:8484

.DEFAULT_GOAL := help
.PHONY: help dev dev-backend dev-frontend deps test test-backend test-frontend test-release-tools \
	lint lint-backend lint-frontend frontend-build build build-arm64 \
	release-bundle check-reproducible test-signing test-installer lint-shell clean

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

deps: ## Install frontend dependencies from the lockfile
	cd frontend && npm ci

dev: ## Run the Go backend and the Vite dev server (UI on http://localhost:5173)
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd backend && go run ./cmd/oarfish -listen $(LISTEN)

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

frontend-build:
	cd frontend && npm run build

build: frontend-build ## Build the host binary with embedded frontend into bin/oarfish
	cd backend && go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/oarfish ./cmd/oarfish

build-arm64: frontend-build ## Cross-build bin/oarfish-linux-arm64 (CGO_ENABLED=0)
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/oarfish-linux-arm64 ./cmd/oarfish

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

clean: ## Remove build outputs (keeps the tracked embed placeholder)
	rm -rf bin dist
	find backend/internal/web/dist -mindepth 1 ! -name .gitkeep -delete
