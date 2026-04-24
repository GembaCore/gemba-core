.PHONY: help dev build build-go-only test lint clean fmt frontend-install frontend-build dist-sentinel release release-dry gen

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.date=$(DATE)

help: ## show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## --- Dev ---

dev: ## run Vite + Go hot reload together (requires: air, pnpm)
	@command -v air >/dev/null 2>&1 || { echo "install air: go install github.com/air-verse/air@latest"; exit 1; }
	@command -v pnpm >/dev/null 2>&1 || { echo "install pnpm: https://pnpm.io/installation"; exit 1; }
	@echo "starting Vite (proxying /api/* -> :7666) and Go (air)..."
	@( cd web && pnpm dev ) & \
	 GEMBA_DEV=1 air -c .air.toml; \
	 kill %1 2>/dev/null

## --- Build ---

frontend-install: ## install frontend dependencies
	cd web && pnpm install --frozen-lockfile

frontend-build: frontend-install ## build the Vite SPA into web/dist
	cd web && pnpm build
	@touch web/dist/.keep   # vite's emptyOutDir:true sweeps this; restore so go build on a fresh clone still finds the dir

build: frontend-build build-sentinels ## build the single-binary with SPA embedded
	go build -ldflags="$(LDFLAGS)" -o bin/gemba ./cmd/gemba
	@echo "built bin/gemba ($(VERSION))"
	@du -h bin/gemba | awk '{print "  size: " $$1}'

build-go-only: build-sentinels ## build without rebuilding the frontend (fast dev iteration)
	go build -ldflags="$(LDFLAGS)" -o bin/gemba ./cmd/gemba

## Sentinel binaries. gemba-bridge / gemba-state / gemba-ask are the
## shell-callable set; gemba-mcp is the MCP-tool server variant an
## MCP-native agent can speak to over stdio instead (gm-97w7.2). All
## four ship alongside the main binary so install-bridge can place
## them on PATH in the session's worktree env.
build-sentinels: ## build the sentinel CLIs + MCP server
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-bridge ./cmd/gemba-bridge
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-state  ./cmd/gemba-state
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-ask    ./cmd/gemba-ask
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-mcp    ./cmd/gemba-mcp

## --- Test / Lint ---

dist-sentinel: ## ensure web/dist exists with sentinel so //go:embed all:web/dist matches on fresh clones
	@mkdir -p web/dist && touch web/dist/.keep

test: dist-sentinel frontend-install ## run Go (race) + frontend tests
	go test -race -count=1 ./...
	cd web && pnpm test

lint: dist-sentinel frontend-install ## run golangci-lint and frontend lint
	@command -v golangci-lint >/dev/null 2>&1 || { echo "install: https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run ./...
	cd web && pnpm lint

fmt: ## format Go + frontend
	gofmt -s -w .
	cd web && pnpm format

## --- Codegen ---

gen: ## regenerate code (TS types from Go core)
	go run ./cmd/gen-core-types

## --- Release ---

release: ## build a local snapshot release via goreleaser
	@command -v goreleaser >/dev/null 2>&1 || { echo "install goreleaser: https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

release-dry: release ## alias for `make release` (snapshot, no publish)

## --- Housekeeping ---

clean: ## remove build artifacts
	rm -rf bin/ dist/ web/dist/* web/node_modules
