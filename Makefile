.PHONY: help dev build build-go-only test lint clean fmt frontend-install frontend-build dist-sentinel release release-dry gen codegen lint-openapi image image-push image-load image-build-only

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
## MCP-native agent can speak to over stdio instead (gm-97w7.2);
## gemba-bd-hook is the out-of-process notify trigger (gm-e4.3.3).
## All ship alongside the main binary so install-bridge can place
## them on PATH in the session's worktree env.
build-sentinels: ## build the sentinel CLIs + MCP server + bd hook
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-bridge  ./cmd/gemba-bridge
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-state   ./cmd/gemba-state
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-ask     ./cmd/gemba-ask
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-mcp     ./cmd/gemba-mcp
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-bd-hook ./cmd/gemba-bd-hook

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

# gm-e4.2: copy the canonical OpenAPI spec from the embed location
# (internal/server/openapi/openapi.json) to the published copy
# (docs/api/openapi.json), then regenerate the typed SPA client at
# web/src/api/gen/types.ts. The Go test in internal/server pins these
# two copies in lockstep — running this target after editing the spec
# is the only step required to keep the contract aligned.
codegen: ## regenerate OpenAPI client (web/src/api/gen) + Go core TS types
	cp internal/server/openapi/openapi.json docs/api/openapi.json
	cd web && pnpm gen:openapi
	go run ./cmd/gen-core-types

# gm-e4.2: lint the OpenAPI spec with Spectral. Requires `pnpm exec
# spectral` to be installable (Stoplight's @stoplight/spectral-cli).
# Not part of the default codegen flow because it pulls a heavy
# devDependency only useful at PR time; run it manually before merging
# spec changes. The .spectral.yaml ruleset at repo root pins the rules
# the spec is expected to pass.
lint-openapi: ## run Spectral against docs/api/openapi.json
	@command -v pnpx >/dev/null 2>&1 || { echo "install pnpm: https://pnpm.io"; exit 1; }
	pnpx --package=@stoplight/spectral-cli spectral lint docs/api/openapi.json --ruleset .spectral.yaml

## --- Release ---

release: ## build a local snapshot release via goreleaser
	@command -v goreleaser >/dev/null 2>&1 || { echo "install goreleaser: https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

release-dry: release ## alias for `make release` (snapshot, no publish)

## --- Container image (gm-e14.8) ---
##
## ko (https://ko.build) builds OCI images straight from Go source. The
## SPA is embedded in the binary, so ko packages exactly what `make build`
## produces — no Dockerfile needed. See .ko.yaml for the base image,
## platforms, and ldflags injection. The frontend must be pre-built so
## the //go:embed all:web/dist directive picks up real assets.
##
## Set KO_DOCKER_REPO to choose the registry, e.g.:
##   KO_DOCKER_REPO=ghcr.io/mikebengtson/gemba-server make image-push
##
## Image tags default to the git rev; the workflow (.github/workflows/
## release-image.yml) overrides this with the release tag on tag push.

KO_DOCKER_REPO ?= ghcr.io/mikebengtson/gemba-server
KO_TAGS        ?= $(COMMIT),latest

# Inject the same ldflags ko consumes via templating in .ko.yaml.
KO_ENV := VERSION=$(VERSION) COMMIT=$(COMMIT) DATE=$(DATE) KO_DOCKER_REPO=$(KO_DOCKER_REPO)

image: frontend-build ## build container image (local OCI tarball, no push)
	@command -v ko >/dev/null 2>&1 || { echo "install ko: go install github.com/google/ko@latest"; exit 1; }
	$(KO_ENV) ko build --bare --tags=$(KO_TAGS) --platform=linux/amd64,linux/arm64 --push=false ./cmd/gemba

image-load: frontend-build ## build + load image into the active Docker daemon (single-arch, current host)
	@command -v ko >/dev/null 2>&1 || { echo "install ko: go install github.com/google/ko@latest"; exit 1; }
	$(KO_ENV) ko build --bare --tags=$(KO_TAGS) --local ./cmd/gemba

image-push: frontend-build ## build + push multi-arch image to KO_DOCKER_REPO
	@command -v ko >/dev/null 2>&1 || { echo "install ko: go install github.com/google/ko@latest"; exit 1; }
	$(KO_ENV) ko build --bare --tags=$(KO_TAGS) --platform=linux/amd64,linux/arm64 ./cmd/gemba

# Smoke-test target: builds the image config without pushing or requiring
# a registry login. Used in CI to verify .ko.yaml stays consumable on
# every PR, without burning the bandwidth of a full multi-arch publish.
image-build-only: dist-sentinel ## smoke-test ko config (single-arch, local, no push)
	@command -v ko >/dev/null 2>&1 || { echo "install ko: go install github.com/google/ko@latest"; exit 1; }
	$(KO_ENV) ko build --bare --tags=$(COMMIT) --push=false --platform=linux/amd64 ./cmd/gemba

## --- Housekeeping ---

clean: ## remove build artifacts
	rm -rf bin/ dist/ web/dist/* web/node_modules
