.PHONY: help dev build build-go-only test lint clean fmt frontend-install frontend-build dist-sentinel release release-dry gen codegen lint-openapi deps install uninstall smoke image image-push image-load image-build-only quickstart-image quickstart-run docs docs-dev docs-install install-hooks uninstall-hooks acceptance-purge

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
## gemba-bd-hook is the out-of-process notify trigger (gm-e4.3.3);
## gemba-codex-driver is the non-interactive Codex exec lifecycle
## driver for native auto-dispatch.
## All ship alongside the main binary so install-bridge can place
## them on PATH in the session's worktree env.
build-sentinels: ## build the sentinel CLIs + MCP server + bd hook
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-bridge  ./cmd/gemba-bridge
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-state   ./cmd/gemba-state
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-ask     ./cmd/gemba-ask
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-mcp     ./cmd/gemba-mcp
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-bd-hook ./cmd/gemba-bd-hook
	go build -ldflags="$(LDFLAGS)" -o bin/gemba-codex-driver ./cmd/gemba-codex-driver

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

lint-decisions: ## verify docs/design ↔ decision-bead linkage (D6 / gm-d1m1)
	@command -v bd >/dev/null 2>&1 || { echo "install bd: https://github.com/steveyegge/beads"; exit 1; }
	go run ./cmd/check-decisions

fmt: ## format Go + frontend
	gofmt -s -w .
	cd web && pnpm format

install-hooks: ## point git at scripts/git-hooks/ (versioned pre-commit + friends)
	@git config core.hooksPath scripts/git-hooks
	@echo "git hooks installed → scripts/git-hooks/"
	@echo "  pre-commit: prettier --prose-wrap always --print-width 72 on staged *.md"
	@echo "uninstall: make uninstall-hooks"

uninstall-hooks: ## restore git's default hooks path
	@git config --unset core.hooksPath || true
	@echo "git hooks restored to default (.git/hooks/)"

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
GEMBA_QUICKSTART_IMAGE ?= gemba-core-quickstart:local

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

quickstart-image: ## build the self-contained Docker quickstart image (Gemba + bd + sample Beads)
	@command -v docker >/dev/null 2>&1 || { echo "install Docker: https://docs.docker.com/get-docker/"; exit 1; }
	docker build \
	  -f Dockerfile.quickstart \
	  -t $(GEMBA_QUICKSTART_IMAGE) \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg COMMIT=$(COMMIT) \
	  --build-arg DATE=$(DATE) \
	  .

quickstart-run: quickstart-image ## run the quickstart image on http://localhost:7666
	docker run --rm -it \
	  -p 7666:7666 \
	  -v gemba-quickstart-data:/data \
	  $(GEMBA_QUICKSTART_IMAGE)

## --- Docs site (gm-e14.4) ---
##
## The operator-facing docs site (Astro Starlight) lives under
## `docs-site/`. Source markdown stays in `docs/` — the docs-site
## sync step mirrors it into the build at `pnpm build` time.

docs-install: ## install docs-site dependencies
	cd docs-site && pnpm install

docs-dev: docs-install ## run the docs site locally (http://localhost:4321/gemba/)
	cd docs-site && pnpm dev

docs: docs-install ## build the docs site to docs-site/dist
	cd docs-site && pnpm build

## --- Housekeeping ---

clean: ## remove build artifacts
	rm -rf bin/ dist/ web/dist/* web/node_modules docs-site/dist docs-site/node_modules docs-site/.astro docs-site/src/content/docs

# Emergency cleanup for acceptance-test resource leaks (gm-root.27.20).
# Hunts /tmp/gemba-acceptance-* dirs and any processes still bound to
# them. Operators run this when a CI lane crashes mid-test and leaves
# orphan Dolt servers / gemba serve processes / temp dirs behind.
acceptance-purge: ## kill orphan acceptance-test resources (/tmp/gemba-acceptance-*)
	@echo ">> hunting acceptance-test processes"
	-@ps -axo pid,command | awk '/gemba-acceptance/ && !/awk/ { print $$1 }' | xargs -I{} -- sh -c 'echo "  killing pid {}"; kill {} 2>/dev/null || true'
	@sleep 1
	-@ps -axo pid,command | awk '/gemba-acceptance/ && !/awk/ { print $$1 }' | xargs -I{} -- sh -c 'echo "  SIGKILL pid {}"; kill -9 {} 2>/dev/null || true'
	@echo ">> removing /tmp/gemba-acceptance-* dirs"
	-@find /tmp -maxdepth 1 -name 'gemba-acceptance-*' -print -exec rm -rf {} + 2>/dev/null
	@echo ">> done"
