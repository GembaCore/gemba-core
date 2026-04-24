# gemba

[![CI](https://github.com/MikeBengtson/gemba/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/MikeBengtson/gemba/actions/workflows/ci.yml)

A single-binary Go service with an embedded React SPA that reads a
**WorkPlane adaptor** (work tracker — the data plane) and, optionally,
an **OrchestrationPlane adaptor** (agent runtime) and renders whatever
the two declare.

The only hard requirement is a data plane. **Beads fulfills that out
of the box**, so the minimum working deployment is `gemba serve
--beads-dir <rig>` and a browser pointed at it — no orchestrator, no
second daemon, no scheduling infrastructure. Other work trackers
(Jira, Linear, GitHub Projects, Azure DevOps, Shortcut, Plane, …) will
arrive via the conformance harness (`docs/adaptors/workplane.md`) as
their adaptors ship.

**Native terminal orchestration is bundled** so operators who want
agent sessions don't need to install anything extra: `gemba serve
--orchestration=native` drives tmux / iTerm2 / Terminal.app sessions
directly, surfaces permission prompts and HITL requests in the SPA,
and correlates `bd` mutations back to the session that made them. The
existing Gas Town / Gas City / LangGraph / CrewAI / OpenHands / Devin
/ Factory adaptor slots stay available as optional alternatives; pick
one with `--orchestration=<name>` when you need their specific
scheduling or isolation semantics.

## Status

**M3 — Native orchestration shipped (April 2026).** Gemba runs
end-to-end against a Beads rig with native terminal orchestration
out of the box; work items, escalations, and session state all
round-trip through the SPA. The Gas Town adaptor is optional.

Active work lives in the project's Beads rig (`bd list`). Top-level
open epics include token spending management (`gm-root.14`), the
UI/SPA build-out (`gm-e12`), and cross-cutting features (escalation
surfacing, evidence v2, DoD v2 — `gm-e11`). Design docs live in
`docs/design/`; adaptor authoring references in `docs/adaptors/`.

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
        │   REQUIRED                                   │   OPTIONAL
        │   transport: api | jsonl | mcp               │   transport: api | jsonl | mcp
        ▼                                              ▼
  ┌───────────────────────┐                   ┌───────────────────────────┐
  │  out-of-the-box: Beads│                   │  out-of-the-box: Native   │
  │  forcing fn:   Jira   │                   │  optional: Gas Town,      │
  │  (Linear, GH, …)      │                   │  LangGraph, Gas City,     │
  │                       │                   │  OpenHands, CrewAI, …     │
  └───────────────────────┘                   └───────────────────────────┘
```

- **WorkPlane is required.** Gemba won't boot without one — the SPA
  has nothing to render.
- **OrchestrationPlane is optional.** Running without it is a
  supported mode: Gemba becomes a read-only / human-driven Kanban
  over the WorkPlane. Starting sessions, surfacing escalations, and
  agent dispatch light up when an adaptor is wired.
- **Native is the default OrchestrationPlane.** `gemba serve
  --orchestration=native` is the happy path and needs no external
  daemon; it drives tmux / iTerm2 / Terminal.app sessions directly.

## Project layout

```
.
├── cmd/
│   ├── gemba/                    # Cobra root (serve, doctor, version, adaptor test, adaptor register, install-bridge)
│   ├── gemba-bridge/             # hook-shim subprocess Claude Code (+ friends) invoke per lifecycle event
│   ├── gemba-state/              # session-status sentinel CLI (ready / working / prompting / stalled)
│   ├── gemba-ask/                # Coach/Manager question/blocker sentinel CLI
│   └── gemba-mcp/                # MCP-tool server variant of gemba-ask + gemba-state
├── internal/
│   ├── core/                     # adaptor-agnostic types: WorkItem, AgentRef, Relationship, ...
│   ├── server/                   # chi router, handlers, OpenAPI spec
│   ├── events/                   # SSE hub, GembaEvent schema, OTEL propagation
│   ├── auth/                     # bind policy, token, TLS, OIDC interface
│   ├── transport/                # api / jsonl / mcp adaptor hosts
│   └── adapter/
│       ├── noop/                 # in-memory reference (both planes)
│       ├── bd/                   # WorkPlane: Beads (CLI + direct Dolt SQL modes)
│       ├── native/               # OrchestrationPlane: native tmux / iTerm2 / Terminal.app (default)
│       ├── gastown/               # OrchestrationPlane: Gas Town (optional)
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
│   ├── design/                   # durable conventions (e.g. milestone-convention.md)
│   └── guides/                   # writing-an-adaptor, migration guide, ...
├── .github/workflows/ci.yml
├── Makefile
└── go.mod
```

## Getting started

### Read-only mode — WorkPlane only

The fastest path to a running UI. One binary, one flag, one browser
tab:

```bash
make build                                               # or: brew install MikeBengtson/tap/gemba (once taps ship)
./bin/gemba serve --beads-dir <path-to-your-beads-rig>
# -> http://127.0.0.1:7666
```

Gemba reads the rig, renders the Kanban, and tails state changes.
No agent sessions start because no OrchestrationPlane is wired —
that's a supported mode, not an error.

End-to-end setup (both `--beads-dir` CLI mode and `--dolt-url`
direct-SQL mode), expected banner output, and troubleshooting:
[`docs/getting-started/running-against-your-work-items.md`](docs/getting-started/running-against-your-work-items.md).

### Native terminal orchestration

Add `--orchestration=native` to light up agent sessions without any
external daemon:

```bash
./bin/gemba serve --beads-dir <rig> --orchestration=native
```

On boot, the native adaptor auto-detects an available terminal backend
(tmux in priority order, then iTerm2, then Terminal.app) and exposes
`/sessions` in the SPA. Clicking "New session" for a bead:

1. Provisions a git worktree (`git worktree add -b bead/<id>`).
2. Runs `gemba install-bridge` in the worktree (idempotent merge
   into `.claude/settings.local.json`; installs the hook stanza + the
   `gemba-mcp` MCP server).
3. Spawns a terminal pane with the chosen agent (Claude Code by
   default; shell-only for operators driving the bead by hand).
4. Injects the project + epic + bead preamble through
   `core/prompt.Envelope`, including the interaction-profile section
   that governs question / blocker behaviour.

Permission prompts and HITL approvals that Claude Code raises surface
live in the SPA; operator answers route back to the terminal as
input. Coach and Manager skill output (`## Questions` / `## Blockers`
via `gemba-ask`) surfaces on the same escalations surface with
kind/channel distinguished.

### Optional: Gas Town, LangGraph, other orchestrators

When you want the scheduling or isolation semantics those stacks
provide, swap the orchestration flag:

```bash
./bin/gemba serve --beads-dir <rig> --orchestration=gastown
```

Native, Gas Town, Gas City, LangGraph, CrewAI, OpenHands, Devin, and
Factory adaptors are mutually exclusive — exactly one or zero
OrchestrationPlane adaptors per `gemba serve` process.

### Shader interop smoke (gm-root.5)

`scripts/shader-interop.sh` exercises the full **encode → bd-store → decode** round-trip against a live `gemba serve` + `bd` backend. It boots gemba on a sandbox port (`:17666` by default) with the gastown shader from `fixtures/gastown.json`, creates a probe bead via `bd create`, PATCHes the title through the running gemba (firing the encoder), asserts `bd show` carries the encoded prefix, then asserts gemba's GET strips it back off. Probe bead is closed in the EXIT trap.

```bash
make build
bash scripts/shader-interop.sh

# Optional — also fire `gt sling` and watch for the polecat to hook.
# Skipped by default because the sling pipeline is flaky on this rig.
SHADER_INTEROP_SLING=1 bash scripts/shader-interop.sh
```

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
