<a name="top"></a>

[![gemba](branding/banner/banner-05-gradient-dispatch.png)](https://github.com/MikeBengtson/gemba)

# gemba

[![CI](https://github.com/MikeBengtson/gemba/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/MikeBengtson/gemba/actions/workflows/ci.yml)
[![e2e](https://github.com/MikeBengtson/gemba/actions/workflows/e2e.yml/badge.svg?branch=main)](https://github.com/MikeBengtson/gemba/actions/workflows/e2e.yml)
[![docs](https://github.com/MikeBengtson/gemba/actions/workflows/docs.yml/badge.svg?branch=main)](https://github.com/MikeBengtson/gemba/actions/workflows/docs.yml)
[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![TypeScript](https://img.shields.io/badge/TypeScript-React%20%2B%20Vite-3178C6?logo=typescript&logoColor=white)](web/)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-lightgrey)](#)
[![License](https://img.shields.io/github/license/MikeBengtson/gemba)](LICENSE)
[![Last commit](https://img.shields.io/github/last-commit/MikeBengtson/gemba/main)](https://github.com/MikeBengtson/gemba/commits/main)
[![Stars](https://img.shields.io/github/stars/MikeBengtson/gemba?style=social)](https://github.com/MikeBengtson/gemba/stargazers)

⭐ **Star us on GitHub** — it helps people find this project and tells us we're solving a real problem.

[![Share on X](https://img.shields.io/badge/share-000000?logo=x&logoColor=white)](https://x.com/intent/tweet?text=A%20single-binary%20Go%20%2B%20React%20Kanban%20that%20pairs%20any%20work%20tracker%20with%20any%20agent%20orchestrator:%20https%3A//github.com/MikeBengtson/gemba%20%23kanban%20%23agents%20%23golang)
[![Share on LinkedIn](https://img.shields.io/badge/share-0A66C2?logo=linkedin&logoColor=white)](https://www.linkedin.com/sharing/share-offsite/?url=https%3A%2F%2Fgithub.com%2FMikeBengtson%2Fgemba)
[![Share on Reddit](https://img.shields.io/badge/share-FF4500?logo=reddit&logoColor=white)](https://www.reddit.com/submit?url=https%3A%2F%2Fgithub.com%2FMikeBengtson%2Fgemba&title=A%20single-binary%20Kanban%20that%20pairs%20any%20work%20tracker%20with%20any%20agent%20orchestrator)
[![Share on Hacker News](https://img.shields.io/badge/share-FF6600?logo=ycombinator&logoColor=white)](https://news.ycombinator.com/submitlink?u=https%3A%2F%2Fgithub.com%2FMikeBengtson%2Fgemba&t=A%20single-binary%20Kanban%20that%20pairs%20any%20work%20tracker%20with%20any%20agent%20orchestrator)
[![Share on Mastodon](https://img.shields.io/badge/share-6364FF?logo=mastodon&logoColor=white)](https://mastodonshare.com/?text=A%20single-binary%20Kanban%20that%20pairs%20any%20work%20tracker%20with%20any%20agent%20orchestrator&url=https%3A%2F%2Fgithub.com%2FMikeBengtson%2Fgemba)

Kanban like planning, execution, and management of complex vibe coding projects in a single pane of glass. Oganize, review, and dispatch large, parallel batches of work and monitor real progress towards milestones, all with built-in coaching and drift detection. Start a new project with a guided conversation or import your existing work — see [Getting Started](#-getting-started) for a quick start. 

In lean manufacturing, gemba (/ˈɡem.bə/ 現場) is "the actual place" — the factory floor, where real work happens. A gemba walk is when leadership observes the work directly, not through reports, and leaves actionable feedback as they go. Gemba is built around that metaphor. 

## Table of Contents

- [🚀 About](#-about)
- [🧱 Architecture](#-architecture)
- [🚦 Status](#-status)
- [✨ What's New](#-whats-new)
- [📸 Screenshots](#-screenshots)
- [📦 Project Layout](#-project-layout)
- [🏁 Getting Started](#-getting-started)
- [🛠️ Development](#%EF%B8%8F-development)
- [📚 Documentation](#-documentation)
- [🤝 Feedback and Contributions](#-feedback-and-contributions)
- [📜 License](#-license)

## 🚀 About

Gemba was born out of frustration with the current state of vibe coding. 
Massively parallel, "headless" agentic software development was technically
possible but it was cumbersome. Before Gemba, a single app to manage the process using 
concepts and terms familiar to developers did not exist - concepts like 
milestones, epics, and Kanban planning to tie them together.  Systems like Beads made it possible 
for agentic pipelines to discover, claim and report work progress but these systems 
were not functionally integrated with **execution - ordering, dispatching, and monitoring 
the work. 

This left developers to face several obstacles in projects of any appreciable scale:

- Planning is disconnected from execution — issues in Jira, Beads, or Todo.txt, agents in a terminal, output in git.
- Priority is invisible. The dep graph says what can run; nothing says what should run next.
- Expertise is siloed — a side-of-the-desk project needs product judgment, architecture, UX review, QA gating, release mechanics, security, and the operator doesn't have time to personally don each hat.
- State is fragile — git, Beads, LLM sessions, and sidecar artifacts each move on their own timeline; "undo back to yesterday" doesn't exist.
- Orchestration tooling is tied to one runtime — playing nicely with one tracker or one agent framework means a rewrite when either moves.

Gemba addresses all five: a browser-based UI for walking the floor of an agentic project, seeing the work at the right grain, directing it, with a roster of configurable LLM specialists a click away for the expertise the operator doesn't have time to personally deliver for every decision.

In effect, Gemba creates a "side of the desk" experience for running vibe coding projects,
meaning complex multi-milestone projects spanning weeks can be tackled while
minimizing the cognitive load and freeing you up to keep the creative flow
going. It keeps you in the driver's seat for what remains critical - 
high level planning, review, and course correction - while allowing fully 
automated agentic software development to take care of the execution.

Here's a peek:
![Gemba board (default view)](docs/img/board.png)

The only hard requirement is a **data plane**. [![Beads](https://github.com/gastownhall/beads) fullfills that out
of the box, so the minimum working deployment is `gemba serve` and a browser pointed at it — 
no orchestrator, no scheduling infrastructure required. 
**Native terminal orchestration is bundled** so operators who want
agent sessions don't need to install anything extra: `gemba serve
--orchestration=native` drives tmux / iTerm2 / Terminal.app sessions
directly, surfaces permission prompts and HITL requests in the SPA,
and correlates `bd` mutations back to the session that made them. Gemba
also supports existing orchestration solutions like Gas Town, which can
picked at launch using `--orchestration=<name>` when you need its specific
scheduling or isolation semantics.

## 🧱 Architecture

![gemba system architecture — SPA over HTTP/SSE → Go core → required WorkPlaneAdaptor + optional OrchestrationPlaneAdaptor](docs/img/architecture.png)

| Plane | Required | Default | What it gives you |
|---|---|---|---|
| WorkPlane | ✅ | Beads (`bd`) | The data — work items, sprints, evidence, DoD |
| OrchestrationPlane | ❌ | Native (tmux / iTerm2 / Terminal.app) | Agent sessions, HITL, dispatch, escalations |

- **WorkPlane is required.** Gemba won't boot without one — the SPA
  has nothing to render.
- **OrchestrationPlane is optional.** Running without it is a
  supported mode: Gemba becomes a read-only / human-driven Kanban
  over the WorkPlane. Starting sessions, surfacing escalations, and
  agent dispatch light up when an adaptor is wired.
- **Native is the default OrchestrationPlane.** `gemba serve
  --orchestration=native` is the happy path and needs no external
  daemon; it drives terminal sessions directly.

Gemba's ranking layer has two distinct systems for distinct jobs.
**Selection** (`internal/planner/selection/`) is a pure-Go dispatch-time
scorer that decides which ready bead a session should take next; it runs
every dispatch loop and powers the `/coach` affinity grid. **epic_order**
(the PM persona's skill) is an LLM-driven planning consult that ranks
candidate epics for sprint composition; it lives at `/sprints` and
produces narrative recommendations with confidence scores. See
[Dispatch vs Planning](https://mikebengtson.github.io/gemba/concepts/dispatch-vs-planning/).

[⬆ back to top](#top)

## 🚦 Status

**Milestone 3 — Native orchestration shipped (April 2026).** Gemba runs
end-to-end against a Beads rig with native terminal orchestration
out of the box; work items, escalations, and session state all
round-trip through the SPA. The Gas Town adaptor is optional.

Active work lives in the project's Beads rig (`bd list`). Top-level
open epics include token spending management (`gm-root.14`), the
UI/SPA build-out (`gm-e12`), and cross-cutting features (escalation
surfacing, evidence v2, DoD v2 — `gm-e11`). The
[parallelism-boundary design doc](https://mikebengtson.github.io/gemba/design/parallelism-boundary/)
is a good starting point for the Design tree; adaptor authoring
references begin with the
[WorkPlane authoring guide](https://mikebengtson.github.io/gemba/adaptors/workplane/)
and the
[OrchestrationPlane authoring guide](https://mikebengtson.github.io/gemba/adaptors/orchestration/).

## ✨ What's New

### Recent (April 2026)

- **Per-agent parallelism** (`gm-root.16`) — agent types declare
  `intra_parallel` + `max_parallel` in `.gemba/agents.toml`; a
  try-reuse-before-spawn dispatcher policy co-locates beads on capable
  panes; SPA shows per-pane pills + a global in-flight counter. See
  the [Parallelism guide](https://mikebengtson.github.io/gemba/getting-started/parallelism/).
- **TLS support** (`gm-e5.3`) — `--tls-self-signed` for instant HTTPS
  with a fingerprint banner, or `--tls-cert` / `--tls-key` for
  operator-supplied chains.
- **Provider-aware agent detail view** (`gm-e12.15`) — `/agents/:id`
  switches its panel based on `Workspace.kind` (worktree, container,
  k8s_pod, vm, exec, subprocess) so the affordances match the runtime.
- **AgentGroup board** (`gm-e12.8`) — `/agent-groups` renders one card
  per group with mode-dispatched visuals (static / pool / graph).
- **Sprint roster** (`gm-e11.5`) — `/sprints` list + `/sprints/:id`
  detail. Token-budget rollups deferred to `gm-root.14.1`.
- **Noop reference adaptors** (`gm-e3.7`) — `gemba serve --noop` boots
  in ~10ms with in-memory WorkPlane + OrchestrationPlane for offline
  exploration and conformance bring-up.
- **Evidence synthesis library** (`gm-e11.6`) — git log + GitHub PR
  API + CI status collectors with parallel execution and partial-failure
  tolerance.

> See the [GitHub commit log](https://github.com/MikeBengtson/gemba/commits/main)
> for the full history.

## 🏁 Getting Started

### New project (starting from scratch)

The primary path for operators starting a new project. Install Gemba,
run the server, and open a browser — Gemba redirects to `/new`
automatically when no projects exist:

```bash
make build                                               # or: brew install MikeBengtson/tap/gemba (once taps ship)
./bin/gemba serve
# -> http://127.0.0.1:7666  (redirects to /new on first run)
```

The `/new` surface opens a conversational **New project** flow. Gemba
walks you through naming the project, describing its shape, and
building out a Milestone → Epic → Bead plan in real time. When you're
happy with the plan, click **Ratify** — Gemba creates the workspace,
initializes the beads database, and drops you into a Gemba walk to
start executing.

You can start a new project at any time — even from an existing
workspace — using the **+** button next to the project picker in the
top bar.

### Advanced: importing existing work

If you have an existing Jira project, Beads workspace, or source-code
repo you want to bring into Gemba, use the **Import from advanced
source** path accessible from Setup (`/setup#import`).

For the common case of pointing Gemba at an existing Beads rig and
rendering it immediately:

```bash
make build
./bin/gemba serve --beads-dir <path-to-your-beads-rig>
# -> http://127.0.0.1:7666
```

Gemba reads the rig, renders the Kanban, and tails state changes.
No agent sessions start because no OrchestrationPlane is wired —
that's a supported mode, not an error.

End-to-end setup (both `--beads-dir` CLI mode and `--dolt-url`
direct-SQL mode), expected banner output, and troubleshooting:
[Running Gemba against your work items](https://mikebengtson.github.io/gemba/getting-started/running-against-your-work-items/).

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

### Native, with parallelism

Per-agent parallelism lets a single session of a capable agent type
carry multiple concurrent beads (gm-root.16). Declare the cap in
`.gemba/agents.toml`:

```toml
[[agent]]
name           = "claude"
binary         = "claude"
preamble       = "claude_md"
hooks          = "claude_code"
intra_parallel = true
max_parallel   = 3
```

The dispatcher tries to reuse an existing capable session before
spawning a new pane; the SPA renders an `n/max` pill per pane plus a
global in-flight counter. Full detail:
the [Parallelism guide](https://mikebengtson.github.io/gemba/getting-started/parallelism/).

### TLS

```bash
# Self-signed: ephemeral cert + fingerprint banner on boot
./bin/gemba serve --beads-dir <rig> --tls-self-signed

# Operator-supplied chain
./bin/gemba serve --beads-dir <rig> --tls-cert ./cert.pem --tls-key ./key.pem
```

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

`scripts/shader-interop.sh` exercises the full **encode → bd-store → decode**
round-trip against a live `gemba serve` + `bd` backend:

```bash
make build
bash scripts/shader-interop.sh

# Optional — also fire `gt sling` and watch for the polecat to hook.
SHADER_INTEROP_SLING=1 bash scripts/shader-interop.sh
```

[⬆ back to top](#top)


## 📦 Project Layout
```
.
├── cmd/
│   ├── gemba/                    # Cobra root (serve, doctor, version, adaptor test, ...)
│   ├── gemba-bridge/             # hook-shim subprocess Claude Code (+ friends) invoke per lifecycle event
│   ├── gemba-state/              # session-status sentinel CLI (ready / working / prompting / stalled)
│   ├── gemba-ask/                # Coach/Manager question/blocker sentinel CLI
│   └── gemba-mcp/                # MCP-tool server variant of gemba-ask + gemba-state
├── core/                         # adaptor-agnostic types: WorkItem, AgentRef, Relationship, ...
├── internal/
│   ├── server/                   # chi router, handlers, OpenAPI spec
│   ├── events/                   # SSE hub, GembaEvent schema, OTEL propagation
│   ├── auth/                     # bind policy, token, TLS, OIDC interface
│   ├── transport/                # api / jsonl / mcp adaptor hosts
│   ├── evidence/                 # shared evidence-synthesis library (git log, gh PR, CI)
│   └── adapter/
│       ├── noop/                 # in-memory reference (both planes)
│       ├── bd/                   # WorkPlane: Beads (CLI + direct Dolt SQL modes)
│       ├── native/               # OrchestrationPlane: native tmux / iTerm2 / Terminal.app (default)
│       ├── gt/                   # OrchestrationPlane: Gas Town (optional)
│       ├── jira/                 # WorkPlane: Jira (forcing-function)
│       ├── langgraph/            # OrchestrationPlane: LangGraph (forcing-function)
│       └── gascity/              # OrchestrationPlane: Gas City (stub)
├── web/                          # Vite + React + TypeScript + Tailwind + shadcn/ui SPA
│   └── src/
│       ├── api/                  # codegenned client + types
│       ├── capabilities/         # capability-manifest readers + JSX gates
│       ├── pages/                # Board, Sessions, Sprints, AgentGroups, AgentDetail, ...
│       ├── components/           # WorkItemDrawer, Palette, AgentGroupBoard, ...
│       └── extensions/           # adaptor-namespaced widgets (gated by capability manifest)
├── testing/                      # public conformance harness (importable as github.com/MikeBengtson/gemba/testing)
│   ├── fixtures/                 # JSON fixtures used by the harness + scripts/
│   └── e2e/                      # Playwright specs + page objects
├── docs/
│   ├── adaptors/                 # per-adaptor authoring docs + conformance reports
│   ├── design/                   # durable conventions (parallelism-boundary, milestone-convention, ...)
│   ├── getting-started/          # operator-facing guides
│   ├── api/                      # OpenAPI 3.1 spec served at /api/openapi.json
│   └── img/                      # diagrams referenced from docs + README
├── docs-site/                    # Astro Starlight build of docs/, deployed to GitHub Pages
├── branding/                     # banner + social-preview PNGs + Pillow generator
├── scripts/                      # dev/CI shell helpers (run.sh, shader-interop.sh, ...)
├── .github/workflows/            # CI, e2e, docs, release
├── embed.go                      # //go:embed of web/dist (must live above web/, can't be moved deeper)
├── Makefile
└── go.mod
```

[⬆ back to top](#top)

## 🛠️ Development
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

- **Go** (see `go.mod` for the minimum version)
- **[`pnpm`](https://pnpm.io/installation)** — frontend package manager
- **[`air`](https://github.com/air-verse/air)** — Go hot reload for `make dev`
  (`go install github.com/air-verse/air@latest`)
- **[`golangci-lint`](https://golangci-lint.run/usage/install/)** — `make lint`
- **[`goreleaser`](https://goreleaser.com/install/)** — `make release`

> [!TIP]
> `make dev` runs Vite (port 5173) and the Go server (port 7666) with
> hot reload on both sides. The Vite dev server proxies `/api` and
> `/events` to the Go process so you can edit and see changes
> end-to-end without manual rebuilds.

[⬆ back to top](#top)

## 📚 Documentation

The full docsite is published at **<https://mikebengtson.github.io/gemba/>** — use its sidebar to browse each section. The links below jump straight to a representative page in each:

| Section | Start at | What's there |
|---|---|---|
| Getting Started | [Running Gemba against your work items](https://mikebengtson.github.io/gemba/getting-started/running-against-your-work-items/) · [Parallelism](https://mikebengtson.github.io/gemba/getting-started/parallelism/) | Operator-facing guides — initial wiring, parallelism configuration |
| Adaptors | [WorkPlane authoring](https://mikebengtson.github.io/gemba/adaptors/workplane/) · [OrchestrationPlane authoring](https://mikebengtson.github.io/gemba/adaptors/orchestration/) · [Beads adaptor](https://mikebengtson.github.io/gemba/adaptors/beads/) · [Native adaptor](https://mikebengtson.github.io/gemba/adaptors/native/) | Per-adaptor authoring docs + conformance reports |
| Design | [Parallelism boundary](https://mikebengtson.github.io/gemba/design/parallelism-boundary/) · [Gemba walk](https://mikebengtson.github.io/gemba/design/gemba-walk/) · [Milestone convention](https://mikebengtson.github.io/gemba/design/milestone-convention/) · [Persona PPPP](https://mikebengtson.github.io/gemba/design/persona-pppp/) | Durable architectural decisions |
| Agents | [Refinery CI watch](https://mikebengtson.github.io/gemba/agents/refinery-ci-watch/) | Per-role agent operating docs |
| UI spec | [UI spec](https://mikebengtson.github.io/gemba/ui-spec/) | The SPA spec — every surface, every affordance, every test-id |

> [!NOTE]
> Architecture decisions live in beads (`bd list type:decision`) and
> are referenced from design docs. Don't read the design docs as
> stand-alone — read the bead they point to for the decision context.

[⬆ back to top](#top)

## 🤝 Feedback and Contributions

We've tried to make every architectural seam — adaptor boundaries,
capability gating, transport plurality — explicit and inspectable.
Where we got it wrong, your feedback is the fastest path to fixing it.

> [!IMPORTANT]
> Bug reports, feature suggestions, and architecture critiques are all
> welcome. The high-leverage things to share: **what you tried**,
> **what you expected**, and **what actually happened**, in roughly
> that order.

- 🐛 [File an issue](https://github.com/MikeBengtson/gemba/issues/new) — bugs, feature requests, design feedback
- 💬 [Open a discussion](https://github.com/MikeBengtson/gemba/discussions) — questions, design conversations, "how would you …" threads
- 🔧 [Pull requests](https://github.com/MikeBengtson/gemba/pulls) — start with a draft if the change is non-trivial; we'd rather review the design before the code

If you're writing a new adaptor, the conformance harness is the
shortest path to "does it work": see the [WorkPlane authoring guide](https://mikebengtson.github.io/gemba/adaptors/workplane/)
or the [OrchestrationPlane authoring guide](https://mikebengtson.github.io/gemba/adaptors/orchestration/).

[⬆ back to top](#top)

## 📜 License

[MIT](LICENSE) © Gemba contributors.

This means: use it, fork it, ship it, no attribution required beyond
the LICENSE file in your distribution. Pull requests welcome under the
same terms.

[⬆ back to top](#top)
