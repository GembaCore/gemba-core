.PHONY: help dev build build-go-only test lint clean fmt frontend-install frontend-build dist-sentinel release release-dry gen codegen lint-openapi deps install uninstall smoke

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# `make install` installs the binary under $(PREFIX)/bin. Defaults to
# /usr/local — override for a userland install:
#   make install PREFIX=$$HOME/.local
# Toolchain prerequisites (matches what `make deps` bootstraps):
#   - Go     >= 1.25  (see go.mod)
#   - Node   >= 20
#   - pnpm   >= 9     (corepack enable; pnpm@9)
#   - git, make, a POSIX shell
PREFIX     ?= /usr/local
BINDIR     ?= $(PREFIX)/bin
SYSTEMDDIR ?= /etc/systemd/system

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

## --- Install (build-from-source path; gm-e14.7) ---

deps: ## bootstrap toolchain (Go modules + frontend pnpm install)
	@command -v go   >/dev/null 2>&1 || { echo "install Go >= 1.25: https://go.dev/dl/"; exit 1; }
	@command -v pnpm >/dev/null 2>&1 || { echo "install pnpm: corepack enable && corepack prepare pnpm@latest --activate"; exit 1; }
	go mod download
	cd web && pnpm install --frozen-lockfile

install: build ## install the gemba binary to $(BINDIR) (default /usr/local/bin); override with PREFIX=...
	@install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 bin/gemba "$(DESTDIR)$(BINDIR)/gemba"
	@echo "installed $(DESTDIR)$(BINDIR)/gemba"
	@echo
	@echo "next steps:"
	@echo "  - copy systemd unit:    sudo cp packaging/systemd/gemba.service $(SYSTEMDDIR)/"
	@echo "  - copy env example:     sudo install -d /etc/gemba && sudo cp packaging/systemd/gemba.env.example /etc/gemba/gemba.env"
	@echo "  - enable + start:       sudo systemctl daemon-reload && sudo systemctl enable --now gemba"
	@echo "  - rotate auth token:    sudo -u gemba $(BINDIR)/gemba auth token rotate"
	@echo "  - verify:               curl -fsS http://127.0.0.1:7666/api/v1/capabilities"

uninstall: ## remove the installed binary and (if present) the systemd unit
	rm -f "$(DESTDIR)$(BINDIR)/gemba"
	rm -f "$(DESTDIR)$(SYSTEMDDIR)/gemba.service"
	@echo "removed $(DESTDIR)$(BINDIR)/gemba and $(DESTDIR)$(SYSTEMDDIR)/gemba.service (if they existed)"
	@echo "note: /etc/gemba and /var/lib/gemba (if any) are left in place — remove manually if desired"

smoke: build-go-only ## build-only smoke: assert the binary exists and `gemba --help` exits 0
	@test -x bin/gemba || { echo "smoke: bin/gemba is missing or not executable"; exit 1; }
	@./bin/gemba --help >/dev/null 2>&1 || { echo "smoke: bin/gemba --help did not exit 0"; exit 1; }
	@./bin/gemba version >/dev/null 2>&1 || { echo "smoke: bin/gemba version did not exit 0"; exit 1; }
	@test -f packaging/systemd/gemba.service || { echo "smoke: packaging/systemd/gemba.service missing"; exit 1; }
	@grep -q '^ExecStart=' packaging/systemd/gemba.service || { echo "smoke: systemd unit missing ExecStart"; exit 1; }
	@echo "smoke: ok ($$( ./bin/gemba version 2>/dev/null | head -1 ))"

## --- Release ---

release: ## build a local snapshot release via goreleaser
	@command -v goreleaser >/dev/null 2>&1 || { echo "install goreleaser: https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

release-dry: release ## alias for `make release` (snapshot, no publish)

## --- Housekeeping ---

clean: ## remove build artifacts
	rm -rf bin/ dist/ web/dist/* web/node_modules
