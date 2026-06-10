# Gemba Remote — Design

**Date:** 2026-05-11
**Status:** Approved (design phase). Implementation tracked in beads.
**Owner:** mike.bengtson@gmail.com
**Beads root:** `gm-o9t8` (Decision) — milestones `gm-o9t8.1` … `gm-o9t8.4`. Created in `~/gt/gemba/.beads` (durable Dolt plane) following the gastown title/tag schema observed in `ai-intelligence-system`.

## 1. Product framing

Gemba Remote is an **agent ops platform**, not a hosted IDE. Users express intent through the local `gemba` CLI (file a bead, write a plan, dispatch a phase). The remote workspace is a long-lived, isolated environment where agents — Claude Code being the first — execute that intent against a checked-out repo. Interactive shell/IDE access exists but is the escape hatch, not the daily driver.

Primary loop:

```
local CLI  ──intent──▶  gemba server  ──dispatch──▶  workspace VM (agent + repo)
                ▲                                            │
                └──────── streamed events / diffs ◀──────────┘
```

Design choice anchors (confirmed during brainstorming):

- The remote machine is the canonical workspace; the CLI is a thin client. (option C from the interaction-model question)
- The remote workspace holds metadata, source code, running agents, and the user's runtime (option D).
- Dolt federation: gemba hosts per-workspace Dolt; users optionally push to their own DoltHub remote.
- Open-core split: CLI + single-user server are OSS; orchestration plane is proprietary.
- The CLI is the primary user surface; interactive coding is an escape hatch.

## 2. Component boundaries (OSS vs commercial)

| Component | License | Why this side of the line |
|---|---|---|
| `gemba` CLI | Apache-2.0 | Adoption surface; must be trustworthy on user machines. |
| `gemba-server` (single-user) | Apache-2.0 | One VM, one user, one workspace. Self-hostable. Where contributions land. |
| Workspace runtime (agent harness, embedded Dolt, repo sync) | Apache-2.0 | Bundled into the single-user server. |
| Orchestration plane (multi-tenant scheduler, fleet manager, billing, SSO, audit log, quotas) | Proprietary | Operational moat; few would want to run this themselves. |
| Web console at gemba.cloud | Proprietary | Thin UI over the orchestration plane. |

**OSS compact:** features that ship in `gemba-server` stay free forever. Commercialization is additive — new orchestration features are added to the proprietary plane, but existing OSS features are not migrated out. This compact is published explicitly so contributors know the terms.

## 3. Data ownership and federation

Three storage planes, each with a clear escape hatch:

- **Source code.** Canonical home is the user's git remote (GitHub/GitLab/etc). Gemba clones into the workspace volume; commits push back. Gemba is never the source of truth for code.
- **Project state** (beads, plans, design docs). Canonical home is the gemba-hosted Dolt instance for that workspace. Users can configure a DoltHub remote and gemba syncs on every change. The DoltHub repo remains useful if gemba disappears.
- **Workspace ephemera** (build caches, agent scratch, dependencies). Lives on the workspace volume. Treated as cattle.

**Keystone trust promise:** a user can fully reconstruct their gemba project from `git remote` + `dolthub remote` on a fresh gemba install or self-hosted server.

## 4. CLI surface

Thin client. No local source checkout required for read paths. Stores credentials and a small read-only metadata cache (last-seen ready beads, last-seen workspace status) so `gemba` (no args), `gemba bead list`, and `gemba constitution show` work offline. Writes always require connectivity to the gemba server.

The verb space splits into **five groups** plus a small convenience set. The split follows the gemba mental model: declarative intent (what the project *should* be) lives separately from imperative ledger writes (what the project *is doing right now*), and both live separately from execution, observation, and break-glass.

| Group | Owns | Verbs |
|---|---|---|
| **Govern** | Declarative intent; documents that constrain the ledger | `spec new`, `spec lint`, `spec reconcile`, `spec watch`, `spec snapshot`, `spec adopt`, `spec freeze`, `spec unfreeze`, `spec import`, `spec list`, `spec move`, `decision add/lock/unlock/list/show`, `constitution init/show/lint/edit` |
| **Ledger** | Imperative bead CRUD; the operational record | `bead create/update/close/list/show/comment/mail` |
| **Dispatch** | Kick off agent work on the workspace VM | `run <phase>`, `agent dispatch <bead>`, `stop` |
| **Inspect** | Read-only views | `logs`, `diff`, `show`, `status` |
| **Escape** | Break-glass into the workspace | `shell` (TTY), `port-forward <port>`, `code` (launches VS Code Remote SSH) |

Plus convenience: `init`, `login`, `gemba` (no args, equivalent to `gemba status`).

### Local / Server / Server-streamed

Every verb has exactly one home. Govern is mixed because spec docs live in git; bead/decision writes need the server.

- **Local** — pure ops on git-tracked files (spec.md, lockfile, constitution.md). Works without `gemba login`. No bd mutations. Examples: `spec new`, `spec lint` (default), `constitution init/show/edit`, `spec freeze` (local edit + auto-chained reconcile).
- **Server** — needs the server because it reads or writes bd state. Required: nonce + audit on writes. Examples: `spec reconcile`, `spec snapshot`, `spec adopt`, `decision *`, `bead *`, `gemba init`, dispatch verbs.
- **Server-streamed** — long-lived SSE channel for live views. Examples: `spec watch`, parts of `gemba status` and `logs`.

Lint commands (`spec lint`, `constitution lint`) follow a `--strict` pattern: pure local validation by default; opt-in server-aware checks for schema and cross-reference verification. This matches the offline-CLI promise and gives CI a clean stricter mode.

### Mode awareness

`gemba` (no args) and `gemba status` are **mode-aware**. The workspace is in one of four modes depending on local frontmatter and lockfile state:

- **Ad-hoc** — no specs/*/ dirs. Output: ready beads + in-flight + counts.
- **Scaffolded** — specs exist but all archived. Output: archived rollup + ready beads.
- **Governed** — at least one live spec. Output: spec header (slug, last reconcile, compact T-NN progress bar `✓✓✓ 🟡 ○○○`, drift count) first; ready beads grouped into "under spec" and "off-spec".
- **Reverse-governed** — Governed plus a lockfile flag `adopted_from_bead_state: true`. Same output as Governed plus a one-line banner that clears after first manual edit.

Mode detection is local (no server call); the server only supplies counts via `GET /workspaces/:wsid/specs` and `GET /workspaces/:wsid/status`. Scriptable variants: `gemba status --json` and `gemba status --mode`.

### Slug resolution

When a spec verb runs, the slug is resolved before the HTTP call in this order:

1. Explicit `--spec <slug>` flag wins.
2. CWD inside `specs/<slug>/...` infers `<slug>`.
3. If exactly one live spec exists in the workspace, that slug is inferred.
4. Otherwise the CLI errors with the list of live slugs and asks for `--spec`.

Spec-list verbs (`spec list`, `gemba status`) work without a resolved slug.

### Acceptance test

A user should be able to do 80% of their daily work — author a spec, reconcile it into beads, dispatch agents, observe progress, close beads, snapshot for retro — without invoking the Escape group.

## 5. Workspace lifecycle

Workspaces are Firecracker microVMs (vendor TBD; see §9) with three states:

- **Hot** — running, agent attached. Billed by the minute.
- **Warm** — suspended with memory snapshot. Cold-start <2s. Billed at a low storage rate.
- **Cold** — VM destroyed; volume and Dolt persist. Free or near-free. Cold-start is a full boot (~20–40s).

Default idle policy: agent finishes → 5 min hot → suspend to warm → 1 hr → cold. CLI commands that need the workspace transparently wake it. Free tier coldens aggressively; paid tiers stay warm longer.

## 6. Security and isolation

- One workspace per microVM. No shared filesystem, kernel, or user.
- **Per-workspace secret vault** for `GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, DoltHub creds, etc. Encrypted at rest. Injected into the agent process at dispatch time. Never written to disk inside the VM. Never logged.
- **Egress allowlist** by default: api.anthropic.com, github.com (or configured git host), the user's Dolt/DoltHub remote, package registries. Everything else off; users can extend.
- **Signed agent actions.** Every file write, command run, and bead mutation by an agent is logged with a content hash to an append-only audit log the user can stream. Underpins the "did the agent really do what it said" trust.
- **CLI auth.** GitHub OAuth on first `gemba login`; CLI gets a long-lived API token bound to a device, revocable. Enterprise SSO is a paid-tier feature.
- **Self-hosted single-user server** skips the multi-tenant machinery (no orchestration, no quotas) — one binary, one user, one workspace. Multi-tenancy is the line between OSS and commercial.

## 7. Convenience

- `gemba init` in any existing repo: detects git remote, optionally creates the DoltHub remote, ratifies the workspace via the existing `/api/v1/newproject/:id/ratify` flow.
- `gemba` with no args inside a tracked repo: shows workspace state + ready beads + recent agent activity. Replaces 3–4 lookups.
- **Local-server fallback.** Same CLI talks to a `gemba-server` on localhost when self-hosting or offline. CLI cannot tell the difference. This is what keeps the OSS/commercial UX honest.
- Editor integrations (VS Code Remote auto-config, Cursor, JetBrains Gateway) are Phase 2, not launch-blocking.

## 8. Pricing dimensions (sketch only — not committed)

Three meters:

- **workspace-hours-hot** — fairness mechanic.
- **storage-GB** — Dolt + volume.
- **agent-minutes** — LLM compute, marked up over wholesale. The load-bearing meter.

Tiers (placeholder): Free (1 workspace, ~10 hot-hours/mo, cold otherwise) → Solo paid → Teams (per-seat + pooled) → Enterprise (SSO, audit, dedicated fleet).

## 9. Open items (deferred to implementation planning or later)

1. **Workspace VM vendor.** Fly Machines, e2b, AWS Fargate, or our own Firecracker on bare metal. Knock-on effects on cold-start latency, pricing, regions, and infra surface area.
2. **Agent pluggability.** Claude Code only at launch, or designed as a harness that supports Codex/Aider/Cline too. Hedges against agent-model churn but adds complexity.
3. **LLM key model.** BYO key vs gemba-managed key. Probably both: BYO default for OSS/self-host parity, managed for free-tier credits and the agent-minutes meter.
4. **DoltHub as hard or soft dependency.** Support arbitrary Dolt remotes (e.g. self-hosted dolt-sql-server), not just hosted DoltHub, so the federation story survives DoltHub outages or term changes.
5. **Web console scope at MVP.** Minimal (status + billing) vs full (diff viewer, bead board, agent transcripts). CLI-first vision favors minimal; enterprise sales typically demands full.

## 10. Implementation roadmap (high-level)

Work is tracked in beads as **decision → milestones → epics → stories** in `~/gt/gemba/.beads` (the durable Dolt plane), following the gastown title/tag schema:

- **Title prefixes** are mandatory: `Decision: …` / `Milestone: …` / `Epic: …` / `Story: …`.
- **Labels are cumulative** — every bead carries the initiative tag (`gemba-remote`), all parent labels, its own type label, and a topic refinement (e.g. `cli`, `server`, `dolt`, `agent`, `dispatch`).
- **IDs use dotted hierarchy** rooted at the decision (`gm-o9t8` → `gm-o9t8.1` → `gm-o9t8.1.1` → `gm-o9t8.1.1.1`).

Milestones as filed:

1. **`gm-o9t8.1` — M1 Single-user OSS core.** 6 epics fully decomposed into 24 stories. CLI scaffold, single-user server scaffold, beads/Dolt integration, workspace bootstrap, agent dispatch, inspection.
2. **`gm-o9t8.2` — M2 Federation & data ownership.** 3 epics; stories to be written when M1 is nearing completion.
3. **`gm-o9t8.3` — M3 Hosted control plane (proprietary).** 6 epics, all labeled `proprietary`.
4. **`gm-o9t8.4` — M4 Productization (proprietary).** 5 epics, all labeled `proprietary` (except editor integrations, which stay OSS).

Each milestone owns its open items from §9 and its own threat model. M2-M4 epics are placeholders until M1 lands; their stories will be decomposed in a future planning pass.

### Query recipes

```bash
# Everything for this initiative
bd query 'labels CONTAINS "gemba-remote"'

# OSS surface area only
bd query 'labels CONTAINS "gemba-remote" AND labels CONTAINS "oss"'

# Proprietary plane only
bd query 'labels CONTAINS "gemba-remote" AND labels CONTAINS "proprietary"'

# Ready M1 work
bd ready --parent gm-o9t8.1
```
