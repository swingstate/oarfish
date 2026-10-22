SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/swingstate/orbit-linux/backend/internal/version.Version=$(VERSION) \
	-X github.com/swingstate/orbit-linux/backend/internal/version.Commit=$(COMMIT)

LISTEN ?= 127.0.0.1:8484

.DEFAULT_GOAL := help
.PHONY: help dev dev-backend dev-frontend deps test test-backend test-frontend \
	lint lint-backend lint-frontend frontend-build build build-arm64 clean

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

deps: ## Install frontend dependencies from the lockfile
	cd frontend && npm ci

dev: ## Run the Go backend and the Vite dev server (UI on http://localhost:5173)
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd backend && go run ./cmd/orbit -listen $(LISTEN)

dev-frontend:
	cd frontend && npm run dev

test: test-backend test-frontend ## Run backend and frontend tests

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test

lint: lint-backend lint-frontend ## gofmt, go vet and frontend type checks

lint-backend:
	@unformatted="$$(cd backend && gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	cd backend && go vet ./...

lint-frontend:
	cd frontend && npm run check

frontend-build:
	cd frontend && npm run build

build: frontend-build ## Build the host binary with embedded frontend into bin/orbit
	cd backend && go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/orbit ./cmd/orbit

build-arm64: frontend-build ## Cross-build bin/orbit-linux-arm64 (CGO_ENABLED=0)
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/orbit-linux-arm64 ./cmd/orbit

clean: ## Remove build outputs (keeps the tracked embed placeholder)
	rm -rf bin
	find backend/internal/web/dist -mindepth 1 ! -name .gitkeep -delete
