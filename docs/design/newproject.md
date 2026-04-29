# New project — conversational creation flow

**Status:** accepted (gm-root.17.1, ratified 2026-04-28 by mike)
**Parent epic:** [gm-root.17](../../.beads/) — Conversational New project creation flow
**Author:** mike (captured by polecat)
**Date:** 2026-04-28

## Amendments since ratification

The original design landed as written. The following amendments — all
post-ratification — adjust the surfaces and APIs without changing the
underlying intent. Read this section first; the body below is annotated
where it diverges from current reality.

| Amendment | Bead | Effect |
| --- | --- | --- |
| **Light-form vs conversational split** | `gm-root.17.13` | Project creation does NOT require an LLM. Added `POST /api/v1/newproject/create` (lightweight: `{project_name, description}` → atomic ratify with empty plan tree, no Onboarder spawn). Added `GET /api/v1/onboarder/probe`. The conversational planner moved to `/onboard`; the board's empty-state offers an opt-in **Plan with the Onboarder →** CTA gated behind the probe. |
| **Unified Create-project modal** | `gm-e12.21.3` | The standalone `/new` page has been retired in favor of a single Create-project modal opened from `/board`. `/new` is now a redirect to `/board?createProject=…`. The modal is the canonical entry for every (DB, repo) combination — fresh create, adopt existing beads DB, clone from git URL. The lightweight `/create` endpoint (gm-root.17.13) backs the "fresh create" arm. |
| **Project picker discovery + bind** | `gm-root.17.14` | `GET /api/v1/projects` now returns every reachable beads DB classified by `kind`: `complete` (DB + workspace.toml), `needs_workspace` (DB only, no workspace), `needs_repo` (DB + workspace, but no git repo). Picker entries with `kind !== "complete"` show a *needs setup* badge; clicking opens a **BindDialog** that runs `POST /api/v1/projects/bind` with `mode: "create"` (`git init` here) or `mode: "navigate"` (copy the beads DB into an existing repo). New `[projects].extra_roots` config lets operators register additional discovery roots. |
| **Onboarder credential resolution** | `gm-root.17.10` (env var by `4f734f0`) | Implicit-anthropic via `ANTHROPIC_API_KEY` env var: if `[llm].provider` is unset but `ANTHROPIC_API_KEY` is exported, the Onboarder constructs an anthropic client with built-in defaults. Credentials never touch disk on this path. |
| **Cold-start UI hygiene** | `gm-root.17.12` | On cold-start (no active project), the left sidebar's workspace-scoped panes render as muted spans instead of links. Settings stays interactive. |
| **Sidebar consolidation** | `gm-e12.19.8` | Sidebar collapsed to six first-order panes — Plan / Review / Escalations / Insights / Agent Sessions / Settings. Secondary surfaces survive as deep-link routes and roll up under the panes' tabs as gm-e12.19.4-7 land. |

## Why this doc exists

Project creation in Gemba is the user's first interaction with the
product. Today it is buried under `/setup#bootstrap` (gm-371's four-source
wizard: Jira / Beads workspace / Source-code repo / Fresh) — a surface
optimized for users importing existing work. Most new users are starting
from scratch and want a guided conversation, not a source picker.

This document locks the design for the **New project** surface — an
always-available conversational entry point that replaces the four-source
wizard as the primary path. Import paths demote to "Import from advanced
source…" for the migration case.

## Scope

This document covers:

- The user-visible surfaces (top-bar button, `/new` route,
  `gemba serve` cold-start, terminal interactive mode).
- The `newproject` skill — prompt scaffolding, conversation state
  machine, LLM contract.
- The atomic ratification transaction.
- The Beads-presence install gate at server startup.
- The post-ratification handoff into a Gemba walk.
- Supersedes-relationship to `gm-371`.

Out of scope for this doc:

- Top-bar project switcher chrome (`gm-root.18`).
- `gemba serve` default Beads URL UX (`gm-root.19`).
- Auto-installing `bd` (explicit non-goal — see Beads install gate).

## Supersedes

`gm-371` (CLOSED, ratified 2026-04-21) defined a four-source Bootstrap
wizard at `/bootstrap`. The conversational scope of `gm-371` (its "Fresh"
path) is **superseded** by this design. The Jira / Beads workspace /
Source-code import paths from `gm-371` remain valid but demote to a
secondary "Import from advanced source…" surface accessible from Setup.

## Surfaces

> **Amended (gm-e12.21.3, gm-root.17.13, gm-root.17.14).** The
> original "/new SPA route" section described a full-page
> conversational layout. The conversational layout still exists but
> moved to `/onboard`; `/new` is now a redirect that opens the
> unified Create-project modal on `/board`.

### Project picker "+" affordance

A "+" button rendered immediately to the **left** of the top-bar
project picker (`gm-root.18`). The picker is the visual anchor; the
"+" sits adjacent to it as a sibling chrome element. Hover text:
*"Create new project"*. Click → navigate to `/new` (which auto-opens
the Create-project modal on `/board`).

The "+" is visible whenever the project picker is — which is always.
The picker chrome renders even when no projects exist, so on a fresh
install the "+" is the first affordance the operator sees in the top
bar.

Visible regardless of:

- the current route,
- whether a workspace is active,
- how many projects exist on this machine.

### Create-project modal (gm-e12.21.3)

The canonical project-creation surface. Opens on top of `/board` via
`/new`, the top-bar `+`, or the project picker's *Adopt existing
beads DB…* link. Three arms:

- **Fresh create** — name + description → `POST /api/v1/newproject/create` (lightweight ratify, empty plan tree, no LLM, gm-root.17.13).
- **Adopt existing beads DB** — pick a discovered beads DB; on
  unmarked DBs the **Configure-project attach** flow runs (gm-xwa8)
  and falls through to the `BindDialog` if the DB needs a workspace
  or repo binding.
- **Clone from git** — clone URL → `POST /api/v1/projects/clone`
  (gm-e12.21.2).

After ratify, the new project becomes the active workspace and the
operator lands on `/board`. The board's empty-state offers an opt-in
**Plan with the Onboarder →** CTA (only when an LLM client is
configured per `GET /api/v1/onboarder/probe`).

### `/onboard` — conversational planner (was: `/new`)

The full-page conversational layout the original design ratified at
`/new` lives here now (`gm-root.17.13`). Two panes:

| Pane | Purpose |
| --- | --- |
| Conversation | Message history with the `newproject` skill. User types prompts; the skill replies with proposals and questions. |
| Plan preview | Live-updated tree of emerging Milestones → Epics → Beads + draft project description. Editable in-place; edits feed back to the skill. |

A persistent **Ratify** button at the bottom-right opens the final
nonce-confirmed commit modal showing the full tree + draft
`docs/project.md` for review. On nonce-confirm the SPA POSTs to
`/api/v1/newproject/:id/ratify` and renders the post-ratify handoff
screen (Start planning → `/walk`; Skip → `/gemba`).

`/onboard` is reachable from the board's empty-state CTA when the
Onboarder probe says the LLM is available, or by direct navigation.
If no LLM is configured, the route surfaces the canonical diagnostic
("Export `ANTHROPIC_API_KEY` …") and the operator can fall back to
the lightweight Create-project modal.

### `/new` route — redirect to the Create-project modal

Today `/new` is a redirect to `/board?createProject=…` that auto-
opens the unified Create-project modal. The route is preserved as a
stable entry-point for cold-start, the top-bar `+`, and external
links; the modal is the actual UI.

### `gemba serve` cold-start redirect

If no `.gemba/` is detected in the configured projects dir at server
startup, the SPA root redirects to `/new` (which then opens the
Create-project modal on `/board`). This makes `gemba serve` followed
by opening a browser the canonical first-run experience for operators
who installed Gemba and have nothing else.

### Terminal interactive mode

When `gemba serve` is launched headless (no SPA available) and no
`.gemba/` is detected, the binary drops into a terminal interactive
session that runs the `newproject` skill against stdin/stdout
(`gemba newproject`). The output is the same atomic ratification
transaction. This path exists so ssh-only operators can bootstrap
without a browser.

## Conversation flow

### State machine

The conversation is a single-turn-at-a-time exchange managed in-memory
on the server. The state carried between turns:

```go
type NewProjectState struct {
    ProjectName    string
    Description    string
    TechStack      []string
    Architecture   string                 // free-form notes from the operator
    Milestones     []DraftMilestone       // each with embedded Epics + Beads
    DraftProjectMD string                 // running synthesis
    Turn           int                    // monotonic counter
    LastChange     ChangeRef              // pointer into the plan tree
}
```

Each turn, the skill reads the prior `NewProjectState`, the operator's
new message, and emits an updated state + a reply. The state is the
authoritative source for what the Ratify button commits.

### Mid-conversation editing

The operator can revise any decision the skill has already proposed —
"change milestone 2 to 'OSS-ready'", "drop epic 1.3", "add a bead under
epic 2.1 about telemetry". The skill MUST:

1. Locate the addressed item in the plan tree.
2. Apply the requested change.
3. Re-derive any downstream items the change invalidates (e.g.,
   renaming a milestone may shift naming on its child epics).
4. Surface the diff in the reply so the operator can confirm the
   regenerated downstream items.

Direct in-place edits in the Plan preview pane bypass the skill (the
operator typed the new text themselves) but feed back into
`NewProjectState` so the next skill turn sees the edit.

### One-shot persistence

Conversation state lives only in the server's process memory. Browser
refresh, server restart, or `Ctrl-C` discards the session. This is
intentional — bootstrap is a short-lived operation (minutes, not days),
and skipping resume infrastructure removes a meaningful surface area.
If the operator loses a session, they start over.

## Onboarder persona + `newproject` skill

### Transient Onboarder persona

The `newproject` skill runs inside a **transient Onboarder persona**.
The persona is the LLM-execution context (prompt scaffolding,
conversation state, model parameters); the skill is the structured
operation that turns conversation turns into a typed plan tree. The
Onboarder is not persisted to `~/.gemba/personas/` or any workspace —
each invocation spins up a fresh instance, runs the conversation,
emits its output, and is discarded. The Onboarder is bundled with the
binary so it is always available.

The Onboarder is invokable any time a New project conversation starts:

- Cold start (`gemba serve` with no `.gemba/`) → spawn Onboarder, run
  conversation.
- Top-bar **New project** click from an existing workspace → spawn
  Onboarder, run conversation. The active workspace is unaffected
  during the conversation; only the post-ratify handoff switches it.
- Terminal interactive mode → spawn Onboarder bound to stdin/stdout.

What's distinct about this persona compared to a workspace persona:

- **Pre-workspace lifetime.** The Onboarder may run before any
  workspace exists, so it cannot rely on workspace-scoped context
  providers.
- **No persistence.** Conversation state lives in the server process
  for the persona's lifetime; nothing survives the persona's exit.
- **No OrchestrationPlane dispatch.** The persona runs inline in the
  server process — no agent runtime is spun up. (Future iterations
  may move it onto the OrchestrationPlane if there's a reason; v1
  does not.)
- **Single skill.** The Onboarder exists to run `newproject` and
  nothing else. It is not a general-purpose persona.

### Bundled prompt scaffolding

The Onboarder ships with a prompt template covering:

- Role framing (a project planner who turns vague intent into a
  workable Milestone → Epic → Bead tree).
- Output schema (typed plan tree — see below).
- Few-shot examples spanning different project shapes (web app,
  library, ops tooling, research project).
- Guardrails on output (no premature implementation detail, milestones
  must be testable, every epic must roll up to a milestone, every bead
  must roll up to an epic).

### Output schema

The skill emits a fully-populated plan tree, validated against the
shape below. Every field maps to a `bd create` flag so ratification can
persist the tree without lossy translation:

```go
type DraftMilestone struct {
    Title              string
    Description        string
    Acceptance         string   // testable acceptance criteria for the milestone
    Labels             []string // free-form labels (type:milestone is added by ratify)
    Priority           int      // 0..4 (0 = highest)
    Estimate           int      // minutes; 0 = unestimated
    Skills             []string // required skills (e.g. "go", "infra", "design")
    DesignNotes        string   // architectural / decision rationale
    Notes              string   // additional context
    Epics              []DraftEpic
}

type DraftEpic struct {
    Title              string
    Description        string
    Acceptance         string
    Labels             []string
    Priority           int
    Estimate           int
    Skills             []string
    DesignNotes        string
    Notes              string
    Beads              []DraftBead
}

type DraftBead struct {
    Title              string
    Description        string
    Type               string   // "task" | "bug" | "feature" | "chore"
    Acceptance         string
    Labels             []string
    Priority           int
    Estimate           int
    Skills             []string
    DesignNotes        string
    Notes              string
    DependsOnRefs      []string // intra-tree references: e.g. "milestone:0/epic:1/bead:2"
    BlocksRefs         []string // intra-tree references
}
```

Notes on the schema:

- **All fields are populated.** The skill MUST emit values for every
  field; empty strings, empty slices, and `Estimate=0` are valid empty
  states. The contract is "no missing keys" so ratification is total.
- **Dependencies use intra-tree refs.** During the conversation the
  beads have no IDs yet; the skill references them by tree position.
  Ratification translates these to real `bd-…` IDs in step 6–8 below.
- **Labels are inheritable.** `bd` inherits labels from parent unless
  `--no-inherit-labels`; the skill should not duplicate inherited
  labels on children.
- **`type:milestone` is added by ratification, not by the skill.**
  Milestones go through the canonical `bd epic -l type:milestone`
  convention (see `docs/design/milestone-convention.md`); the skill
  emits `DraftMilestone`s and lets ratify do the encoding.

Validation runs after every turn. Validation failures raise a
structured "the skill returned an invalid plan — asking it to retry"
to the operator rather than poisoning the plan tree.

### Credential resolution

The Onboarder resolves an LLM client in this order:

1. **`~/.gemba/config.toml` `[llm]` table** — explicit `provider` +
   `api_key` (and optional `model`, `endpoint`). This is the same
   config the OrchestrationPlane reads for its agents.
2. **`ANTHROPIC_API_KEY` environment variable** — implicit anthropic
   provider. If `[llm].provider` is unset (or absent entirely) and
   `ANTHROPIC_API_KEY` is exported in the process environment, the
   Onboarder constructs an anthropic client with that key and
   built-in defaults for model/endpoint. This lets an operator who
   already has `ANTHROPIC_API_KEY` exported run `gemba serve` with
   zero config.toml and still hit the happy path. Credentials never
   touch disk.
3. **`[llm].provider = "anthropic"` with empty `api_key`** also
   falls back to `ANTHROPIC_API_KEY`, so the same env var covers
   both the explicit-anthropic-without-key and no-config-at-all
   paths.

If neither path resolves, the Onboarder fails to spawn and the SPA
surfaces a clear diagnostic at the top of `/new`: *"No LLM client
configured. Export `ANTHROPIC_API_KEY` in your environment, or set
`[llm]` in `~/.gemba/config.toml`, before starting a New project
conversation."*

Credentials are NEVER written to disk by Gemba. Operators export the
env var in their shell profile or pass it inline; the env-var path
explicitly avoids materializing the key in any file Gemba could
later commit to git.

### Persona-skill binding contract

The persona-skill binding is fixed at compile time — the Onboarder
runs the `newproject` skill and nothing else. There is no general
persona that selectively dispatches `newproject` among other skills;
trying to bind `newproject` to another persona is a programmer
error.

The contract lives in `internal/personas/onboarder/`:

| Surface | Type / Function | Purpose |
| --- | --- | --- |
| `onboarder.Persona` | type | One transient instance, one conversation |
| `onboarder.Spawn(ctx, Resolver) (*Persona, error)` | func | Resolve a chat client and return a fresh Persona |
| `onboarder.SpawnWithClient(LLMClient) (*Persona, error)` | func | Bypass resolver — used by tests + terminal mode |
| `Persona.Greeting() string` | method | Opening line on /start |
| `Persona.Turn(ctx, prior, message) (TurnResult, error)` | method | One conversational turn; wraps newproject.Run with validation-retry-once |
| `Persona.Discard()` | method | Idempotent release; today a no-op |
| `onboarder.SkillTurner` | type | Adapter implementing `internal/server.SkillTurner` so `AttachNewProject` can plug it in |
| `onboarder.NewSkillTurner(Resolver) *SkillTurner` | func | Lazy-spawning, single-Persona, server-bound adapter |
| `onboarder.DefaultResolver(configPath string) Resolver` | func | Reads `~/.gemba/config.toml` `[llm]` table; ErrNoLLMClient when unset |
| `onboarder.ErrNoLLMClient` | sentinel | The diagnostic surfaced as the SPA's no-client message |
| `onboarder.IsNoClient(err) bool` | func | Classify spawn-failure vs. infra failure |

Lifecycle invariants:

- Spawn does not perform a network call — only credential
  resolution. The first model round-trip happens in `Turn`.
- Discard is idempotent. Calling it twice is safe.
- One `SkillTurner` serves the whole serve process — the Persona
  is stateless modulo its `LLMClient`, so HTTP sessions share it.
  The "spawn-on-demand → run conversation → discard" language in
  the design above describes the conversation, not the underlying
  shared chat client.
- Validation-retry-once: a single `*newproject.ValidationError`
  triggers one retry with the validator's complaint embedded as a
  `[onboarder retry]` banner in the user message. A second
  validation failure surfaces the wrapped error to the caller. The
  system prompt is NOT mutated by the retry — the prompt-validator
  contract is locked at the skill package boundary.

Server wiring:

```go
handler.AttachNewProject(
    server.NewMemoryNewProjectStore(),
    onboarder.NewSkillTurner(onboarder.DefaultResolver(cfg.ConfigPath)),
    server.NewRatifier(server.RatifierConfig{}),
)
```

The `internal/server.SkillTurner` interface gained a `Probe(ctx)
error` method when the Onboarder landed (gm-root.17.10): `/start`
calls `Probe` before allocating a session and returns
`503 no_llm_client` carrying the diagnostic when probe fails.

`config.toml` `[llm]` table:

```toml
[llm]
provider = "anthropic"               # only "anthropic" today
api_key  = "sk-..."                  # or leave blank to fall back to ANTHROPIC_API_KEY
model    = "claude-3-5-sonnet-latest" # optional
endpoint = ""                         # optional override
```

## Atomic ratification

When the operator clicks **Ratify**:

1. The SPA opens a nonce-confirmed modal showing the full
   `NewProjectState` tree + draft `docs/project.md`.
2. On confirmation, the SPA POSTs to `/api/v1/newproject/ratify` with
   the nonce + serialized state.
3. The server runs the transaction below.

### Transaction steps

In strict order:

1. Resolve target dir: `<default_dir>/<project-name>/` where
   `default_dir` comes from `~/.gemba/config.toml` (`projects.default_dir`,
   user-configurable, defaults to `~/gemba/projects/`).
2. Create the dir. Failure if it already exists — operator must pick a
   different name (the skill should warn about collisions during the
   conversation).
3. `git init` in the new dir on `main`.
4. Write `.gemba/workspace.toml` with project metadata.
5. Initialize the beads database in the new workspace.
6. For each milestone (in order): create as `bd epic -l type:milestone`
   plus the milestone's labels, priority, estimate, skills, acceptance,
   design, and notes; capture the new ID for parenting children.
7. For each epic under each milestone: create as `bd epic` with
   parent = the milestone ID; carry through labels, priority,
   estimate, skills, acceptance, design, and notes.
8. For each bead under each epic: create with parent = the epic ID,
   type/priority/labels/estimate/skills/acceptance/design/notes from
   the draft.
8a. Resolve intra-tree dependency refs (`DependsOnRefs`,
    `BlocksRefs`) to real `bd-…` IDs and apply with `bd dep`. Cycles
    detected at this step abort the transaction (rollback).
9. Write `docs/project.md` (the synthesized narrative from the skill).
10. Stage all files; create initial commit on `main`
    (`feat: initial project bootstrap`).

### Failure rollback

Any step failure rolls back the entire tree:

- Delete the new project dir (only if step 2 succeeded — never touch a
  pre-existing dir).
- Surface the error to the SPA with the failing step + diagnostic so
  the operator can retry or escalate.

The transaction is **not** restartable mid-flight. A failed ratify
discards everything; the operator restarts the conversation.

### API surface

Conversational planner (Onboarder, `/onboard`):

| Path | Verb | Purpose |
| --- | --- | --- |
| `POST /api/v1/newproject/start` | open a conversation, return a session ID |
| `POST /api/v1/newproject/:id/turn` | submit an operator message, return the updated state + skill reply |
| `POST /api/v1/newproject/:id/ratify` | nonce-confirmed atomic commit |
| `GET /api/v1/onboarder/probe` | reports `{available, reason?}` so the SPA can gate the optional planning CTA |

Lightweight create + project discovery (no LLM required):

| Path | Verb | Purpose |
| --- | --- | --- |
| `POST /api/v1/newproject/create` | nonce-gated `{project_name, description}` → atomic ratify with empty plan tree (`gm-root.17.13`) |
| `GET /api/v1/projects` | enumerate every reachable beads DB; each entry carries `kind: "complete" \| "needs_workspace" \| "needs_repo"` (`gm-root.17.14`) |
| `POST /api/v1/projects/bind` | nonce-gated `{beads_db_path, target_repo_path, mode: "create" \| "navigate"}` → bind a partially-set-up beads DB to a git repo (`gm-root.17.14`; `mode: "navigate"` is copy-only today, `gm-kw6a` tracks move) |
| `POST /api/v1/projects/clone` | clone a git URL into the projects dir (`gm-e12.21.2`) |
| `POST /api/v1/projects/switch` | switch the active workspace (`gm-root.18`) |

## Beads install gate

At `gemba serve` startup, before any other initialization:

1. Probe `bd --version`.
2. If absent, print install instructions to stderr (link to the bd
   install docs + the canonical brew/pipx command) and exit non-zero.
3. If present, continue.

This is a hard gate — Gemba does not run without `bd`. The flow does
not auto-install — that decision belongs to the user, and a CLI tool
silently mutating `$PATH` is an anti-pattern. The install instructions
include a one-liner the operator can copy.

## "Start planning" handoff

After successful ratification, the SPA shows a one-screen handoff:

- **Start planning** (primary CTA) — switches the active workspace to
  the new project + opens the **Gemba walk** surface (`gm-3nk`) with
  the freshly-created milestones and epics seeded as agenda items. The
  operator and PM persona can immediately walk the milestones and
  begin ratifying agenda items.
- **Skip** (secondary) — switches the active workspace to the new
  project + lands on `/gemba` (the dashboard). The operator can start
  a Gemba walk later.

There is no third option. The newly-created project is always the
active workspace after ratification.

## Project root resolution

`<default_dir>/<project-name>/` becomes:

- the git root (`.git/` lives here),
- the workspace root (`.gemba/` lives here),
- the beads database root,
- the project's identifier in the project switcher (`gm-root.18`).

`default_dir` resolution order:

1. `~/.gemba/config.toml` → `[projects].default_dir` if set.
2. `~/gemba/projects/` (built-in default; created on first project if
   missing).

A project name conflicts the moment the dir already exists. Conflict
detection is the skill's job (warn during the conversation) and the
transaction's job (fail-closed at step 2 if the skill missed it).

## Non-goals

- **No auto-install of `bd`.** The install gate prompts; it does not
  install.
- **No conversation resume.** One-shot, in-memory only.
- **No raw transcript persistence.** Only the synthesized
  `docs/project.md` survives.
- **No multi-project federation.** Each project is an independent
  workspace. Cross-project queries are a separate concern (not in
  scope).
- **No retries inside the ratify transaction.** Atomic or nothing.
- **No template gallery.** The skill drives the conversation freshly
  each time; pre-built templates ("Rails app", "data pipeline", …) are
  follow-on work, not v1.

## References

- Parent epic: `gm-root.17` — Conversational New project creation flow.
- Superseded scope: `gm-371` (CLOSED) — the conversational / Fresh path.
- Related: `gm-3nk` (Gemba walk — the "Start planning" target),
  `gm-root.3` (Milestone convention — what milestones are stored as),
  `gm-root.18` (project switcher), `gm-root.19` (default Beads URL).
- Surface impact: `docs/ui-spec.md` §5.15 (demoted), §5.20 Setup
  table (`#bootstrap` row replaced by `#import`).
- UI consolidation amendments: `gm-e12.19`, `gm-e12.19.2`.
