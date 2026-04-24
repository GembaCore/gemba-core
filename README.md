# gemba

[![CI](https://github.com/MikeBengtson/gemba/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/MikeBengtson/gemba/actions/workflows/ci.yml)

A single-binary Go service with an embedded React SPA that pairs *exactly one* **WorkPlane adaptor** (work tracker — Beads, Jira, Linear, GitHub Projects, Azure DevOps, Shortcut, Plane, …) with *exactly one* **OrchestrationPlane adaptor** (agent runtime — Gas Town, Gas City, LangGraph, CrewAI, OpenHands, Devin, Factory, …) and renders whatever the two declare.

## Status

Pre-implementation. The full design + work breakdown lives in the `gemba_prime` workspace at `~/gt/gemba_prime/crew/mike/`:

- `README.md` — project overview, twelve locked decisions, work-graph shape
- `RFC.md` — community-facing design proposal
- `domain.md` — full type system, adaptor interfaces, design decisions
- `landscape.md` — evidence-grounded survey of the agent + tracker landscape
- `issues.jsonl` — 104-bead work package across 14 phase epics

The build executes here once the work package imports into the local Beads rig and the orchestrator decomposes Phase 1 (Foundation).

## Architecture summary

```
┌─────────────────────────────────────────────────────────────────┐
│                      Gemba SPA (React/TS)                        │
│        no role names · no pack vocabulary · capability-driven    │
└─────────────────────────────────────────────────────────────────┘
                                ▲
                  HTTP / SSE — capability-negotiated
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Gemba core (Go binary)                       │
│   types: WorkItem · AgentRef · Relationship · Evidence · DoD     │
│          Sprint · TokenBudget · CostMeter · EscalationRequest    │
└─────────────────────────────────────────────────────────────────┘
        ▲                                              ▲
        │   WorkPlaneAdaptor                           │   OrchestrationPlaneAdaptor
        │   transport: api | jsonl | mcp               │   transport: api | jsonl | mcp
        ▼                                              ▼
  ┌───────────────────────┐                   ┌───────────────────────────┐
  │  v1 reference: Beads  │                   │  v1 reference: Gas Town   │
  │  forcing fn:   Jira   │                   │  forcing fn:  LangGraph   │
  │  (Linear, GH, …)      │                   │  (Gas City, OpenHands, …) │
  └───────────────────────┘                   └───────────────────────────┘
```

## Project layout (target — to be scaffolded by Phase 1)

```
.
├── cmd/gemba/                    # Cobra root (serve, doctor, version, adaptor test, adaptor register)
├── internal/
│   ├── core/                     # adaptor-agnostic types: WorkItem, AgentRef, Relationship, ...
│   ├── server/                   # chi router, handlers, OpenAPI spec
│   ├── events/                   # SSE hub, GembaEvent schema, OTEL propagation
│   ├── auth/                     # bind policy, token, TLS, OIDC interface
│   ├── transport/                # api / jsonl / mcp adaptor hosts
│   └── adapter/
│       ├── noop/                 # in-memory reference (both planes)
│       ├── beads/                # WorkPlane: Beads
│       ├── gastown/              # OrchestrationPlane: Gas Town
│       ├── jira/                 # WorkPlane: Jira (forcing-function)
│       ├── langgraph/            # OrchestrationPlane: LangGraph (forcing-function)
│       └── gascity/              # OrchestrationPlane: Gas City (stub)
├── web/                          # Vite + React + TypeScript + Tailwind + shadcn/ui SPA
│   └── src/
│       ├── api/                  # codegenned client + types
│       ├── capabilities/         # capability-manifest readers + JSX gates
│       ├── views/                # Board, Backlog, Grid, Graph, Insights, Capabilities, ...
│       ├── components/           # WorkItemDrawer, Palette, AgentGroupBoard, ...
│       └── extensions/           # adaptor-namespaced widgets (gated by capability manifest)
│           ├── beads/
│           ├── gastown/
│           ├── jira/
│           └── langgraph/
├── docs/
│   ├── adaptors/                 # per-adaptor authoring docs + conformance reports
│   ├── dd/                       # design-decision outcomes from validation phase
│   └── guides/                   # writing-an-adaptor, migration guide, ...
├── .github/workflows/ci.yml
├── Makefile
└── go.mod
```

## Getting started (post-Phase-1)

```bash
brew install MikeBengtson/tap/gemba
cd <a workspace whose adaptors Gemba can detect>
gemba serve
# -> http://localhost:7666
```

Until then, the orchestrator (Mayor in Gas Town terms) decomposes work from `bd ready` against the imported `issues.jsonl`.

### M1 quickstart — run it against your work items

Gemba at M1 ships the Beads WorkPlane adaptor in two modes (`--beads-dir` via the `bd` CLI, or `--dolt-url` for a direct Dolt SQL connection). End-to-end instructions, expected banner output, and troubleshooting live in [`docs/getting-started/running-against-your-work-items.md`](docs/getting-started/running-against-your-work-items.md).

## Development

The repo ships a single `Makefile` covering the full dev/build/release loop. From a fresh clone:

```bash
make help         # list every target with one-line docs
make dev          # Vite + Go server, both with hot reload (needs: air, pnpm)
make build        # build the SPA, embed it, produce ./bin/gemba
make test         # go test -race ./... + pnpm test --run
make lint         # golangci-lint + pnpm lint
make release      # local goreleaser snapshot (multi-OS/arch tarballs in ./dist)
make clean        # remove bin/, dist/, web/dist/*, web/node_modules
```

Tooling expected on `PATH`:

- Go (see `go.mod` for the minimum version)
- [`pnpm`](https://pnpm.io/installation) — frontend package manager
- [`air`](https://github.com/air-verse/air) — Go hot reload for `make dev` (`go install github.com/air-verse/air@latest`)
- [`golangci-lint`](https://golangci-lint.run/usage/install/) — `make lint`
- [`goreleaser`](https://goreleaser.com/install/) — `make release`
