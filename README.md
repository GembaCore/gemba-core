# Gemba

A browser-based Kanban-style UI for multi-agent orchestration.

**v1 runtime: [Gas Town 1.0](https://github.com/gastownhall/gastown)** — the stable, production-ready orchestrator the community is running today. Gemba talks to Gas Town through `gt` and `bd` CLIs, reads `~/gt/` state for low-latency views.

**Architectural compass: [Gas City](https://github.com/gastownhall/gascity)** — the declarative-SDK successor, in alpha, on track for GA. Every design decision here is made with the Gas City transition in mind. The adapter layer is designed so that when Gas City reaches GA, the primary runtime flips from `gt` to `gc` via configuration, not code surgery. The UI is pack-agnostic from day one so it will render `gastown`, `ccat`, `ralph`, `wasteland-feeder`, or user packs without UI changes.

**Status: pre-alpha scaffold.** This is gm-e1 (Phase 1: Foundation) — the skeleton every later phase builds on. Architectural decisions, feature set, and Phase 2+ work are tracked as beads; see the [work package](../issues.jsonl) for details.

## What it looks like in practice

Three concrete workflows Gemba is built to make fast. Everything else in the design (pack-agnostic UI, declarative reconciliation, provider-aware views) exists to serve them.

### 1. Planning & Refinement — work the backlog like a PM tool, not an agent console

Review the backlog — stories, tasks, epics, bugs — across every rig in the town (or city, at Gas City GA) from one screen.

- **Cross-rig work grid** — 10k beads, virtualized, column presets, saved filters. Jira/Linear ergonomics sized for agent-generated bead volume.
- **Edit inline or bulk-import** — update descriptions, labels (`surface:*`, `tier:*`, `risk:*`, `fed:*`, `provider:*`), priorities, and acceptance criteria directly; import whole epics from JSONL for RFC-driven work packages.
- **Dep graph over all seven Beads edge types** — `blocks`, `related`, `parent-child`, `discovered-from`, `waits-for`, `replies-to`, `conditional-blocks`, each visually distinct, with cycle highlighting and critical-path mode.
- **Molecule formula authoring** — build multi-step checkpoint-recoverable workflow DAGs before a single agent is dispatched; per-step prompt rendering so work is legible, not opaque.

Every mutation round-trips through `bd --json`. Dolt is never written directly.

### 2. Scrum / Day-of Ops — drive the standup from one screen instead of tailing tmux panes

Review current state across all rigs and push work forward without context-switching between terminals.

- **Convoy Kanban** — drag Planned → In Progress; status round-trips through `bd update --status`. Multi-select to "create convoy" and dispatch a batch. Slinging goes through `gt sling` today, `gc` at Gas City GA — same UI, adapter flips via config.
- **Desired-vs-actual view** — on Gas City: `city.toml` declared state alongside `.gc/agents/` running sessions, drift highlighted, per-drift or global "reconcile" action. On Gas Town v1: useful-but-partial, derived from the implicit fixed-role structure. The component tree does not change when Gas City arrives.
- **Provider-aware agent detail** — tmux session, k8s pod, subprocess, exec script all render differently. Peek into a running agent, read its session, see its elastic-pool check output. Pluggable from day one.
- **Confirmation-gated mutations** — every state change requires a server-enforced `X-GEMBA-Confirm` nonce; duplicate confirmations are rejected so retries don't double-mutate. `--dangerously-skip-permissions` (name copied verbatim from Claude Code) unlocks a session.

### 3. Retro & Release — close the loop after work lands

Finished convoys, shipped molecules, and completed releases are first-class objects, not an afterthought in a log file.

- **Completed-work filters** — saved queries for "past sprint," "last release," "landed convoys by rig," "molecules that failed a step." Same grid, scoped by time window and status.
- **Molecule replay** — walk a completed formula step-by-step: per-step prompts, outputs, checkpoint state, failure modes. Informs rework without archaeology.
- **Insights panel** — fed from OTEL metrics plus `bd stats`: spawn rate, completion rate, stuck-agent minutes, token cost, merge-queue latency. The signals retros actually need.
- **Truthful audit log** — nonce-idempotent mutations mean history is comparable across runs and safe to replay. Retro conclusions rest on data, not reconstructed narrative.

## Quickstart

```bash
# Prerequisites
brew install go pnpm
go install github.com/air-verse/air@latest

# Install, build, run
git clone https://github.com/YOUR_ORG/gemba.git
cd gemba
make build
cd ~/gt            # Gas Town v1 (stable runtime) — or ~/my-city for Gas City alpha
gemba serve
# -> http://localhost:7666
```

Dev mode with hot reload on both sides:

```bash
make dev
# Vite on :5173 (proxies /api -> :7666)
# Go on :7666 (auto-rebuilds via air)
```

## Commands

```
gemba serve       # run the HTTP server
gemba doctor      # check prerequisites (gt or gc + bd, workspace detection)
gemba version     # print build info
```

### `gemba serve` flags

```
--listen string     interface to bind (default "127.0.0.1"; 0.0.0.0 requires --auth)
--port int          TCP port (default 7666)
--open              open the UI in a browser after starting
--auth string       auth mode: none (default), token, oidc
--tls-cert string   path to TLS certificate
--tls-key string    path to TLS key
--tls-self-signed   generate a self-signed cert on first run
--town string       path to Gas Town HQ (default: auto-detect ~/gt). v1 stable runtime.
--city string       path to Gas City workspace (default: auto-detect city.toml or ~/my-city). Alpha; future-ready.
--dangerously-skip-permissions
                    disable mutation confirmations for the session
                    (flag name copied from Claude Code; intentional)
```

## Remote access

The default bind is `127.0.0.1:7666`. To expose on the network:

```bash
gemba serve --listen 0.0.0.0 --auth token --tls-self-signed
```

Binding a non-loopback interface without `--auth` is a **startup error**, not a warning. This is deliberate — see `internal/config/bind.go` and the gm-e3 epic for the full threat model.

## Architecture

```
+---------------------+        +----------------------+
|  Go binary (bc)     |        |  Gas City stack      |
|                     |        |                      |
|  cmd/gemba             |        |  gt CLI (v1 stable)  |
|  internal/api       |------->|  gc CLI (GA-ready)   |
|  internal/adapter   |------->|                      |
|  internal/events    |        |  bd CLI              |
|  web/dist (embed)   |------->|  ~/gt/ or .gc/       |
+---------------------+        +----------------------+
         ^
         | http / sse
         v
+---------------------+
|  React SPA          |
|  (web/)             |
+---------------------+
```

In v1, the primary runtime is Gas Town 1.0. Mutations shell out to `gt` and `bd`; reads hit `~/gt/` for latency. Gas City's declarative-reconciliation model shapes the design — when Gas City reaches GA, mutations will round-trip through `gc config` edits to `city.toml` and the controller reconciles, exactly as it does for its own tooling. We never write to Dolt, JSONL, `.gt/`, `.gc/`, or any controller socket directly (see gm-root locked decision #9).

## Layout

```
cmd/gemba/               CLI entry, flag parsing
embed.go              go:embed declaration for the built SPA
internal/
  adapter/gt/         wraps gt CLI (Gas Town v1 — primary today)
  adapter/gc/         wraps gc CLI (Gas City — stubbed, primary at GA)
  adapter/bd/         wraps bd CLI (Beads — work tracking, graphs, mail)
  adapter/fs/         reads ~/gt/ today; designed for .gc/ at Gas City GA
  api/                HTTP router, SPA fallback, API surface
  auth/               token, TLS (OIDC post-v1)
  config/             ServeConfig + bind policy
  events/             SSE hub
  model/              shared domain types
web/
  src/                React + TypeScript + Tailwind
  dist/               Vite build output (embedded into binary)
```

## Design philosophy

Gemba is architected around Gas City's principles even though the v1 runtime is Gas Town. This is deliberate — Gas City's declarative-SDK shape tells us what the UI needs to look like so it survives the transition without rewriting:

- **Declarative UX (full on Gas City, stubbed on Gas Town).** On Gas City: UI shows desired state (from `city.toml`) alongside actual state (running sessions); drift is rendered; edits go back through `gc config`. On Gas Town today: desired state is derived from Gas Town's implicit role structure and the view is useful-but-partial. The component tree doesn't change when Gas City arrives.
- **Pack-agnostic.** No hardcoded Mayor, Witness, Polecat columns — even though Gas Town v1 has those as fixed roles. The UI renders whatever agents the active topology declares. Today it renders Gas Town's fixed role set; at Gas City GA the same code renders `ccat`, `ralph`, `wasteland-feeder`, or any user pack.
- **Zero Framework Cognition in the UI.** Gemba shows data and offers actions. It doesn't encode decision logic about what agents should do. "Why is this polecat stuck?" is a question for the human reading the session peek, not for the UI to answer.
- **Provider-aware (pluggable from day one).** Gas Town v1 runs agents in tmux. Gas City generalizes to tmux/k8s/subprocess/exec. The detail view is built pluggable now so Gas City GA requires zero UI rework.

## Contributing

This is a multi-agent orchestrated project managed via [Beads](https://github.com/gastownhall/beads). To see what's ready to work on:

```bash
cd ~/my-city/rigs/gemba
bd ready --json | jq '.[0:5]'
```

Every bead carries a detailed Goal / Inputs / Outputs / Definition of Done and a full label taxonomy (`surface:*`, `tier:*`, `risk:*`, `fed:*`, `provider:*`). Respect `tier:*` when picking work; opus-tier beads should not be taken on with Haiku-class models.

Proposed changes to the locked architectural decisions in `gm-root` require an escalation rather than a local edit.

## License

MIT. See [LICENSE](LICENSE).
