# New project — conversational creation flow

**Status:** design · tracked in `gm-root.17.1`
**Parent epic:** [gm-root.17](../../.beads/) — Conversational New project creation flow
**Author:** mike (captured by polecat)
**Date:** 2026-04-28

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

### Top-bar "New project" button

Always-visible affordance in the SPA's global chrome. Visible regardless
of:

- the current route,
- whether a workspace is active,
- how many projects exist on this machine.

Click → navigate to `/new`.

### `/new` SPA route

Full-page conversational layout — not a modal, not a Setup section, not
a wizard with discrete numbered steps. Two panes:

| Pane | Purpose |
| --- | --- |
| Conversation | Message history with the `newproject` skill. User types prompts; the skill replies with proposals and questions. |
| Plan preview | Live-updated tree of emerging Milestones → Epics → Beads + draft project description. Editable in-place; edits feed back to the skill. |

A persistent **Ratify** button at the bottom-right opens the final
nonce-confirmed commit modal showing the full tree + draft
`docs/project.md` for review.

### `gemba serve` cold-start redirect

If no `.gemba/` is detected in the configured projects dir at server
startup, the SPA root redirects to `/new`. This makes `gemba serve`
followed by opening a browser the canonical first-run experience for
operators who installed Gemba and have nothing else.

### Terminal interactive mode

When `gemba serve` is launched headless (no SPA available) and no
`.gemba/` is detected, the binary drops into a terminal interactive
session that runs the same `newproject` skill against stdin/stdout. The
output is the same atomic ratification transaction. This path exists so
ssh-only operators can bootstrap without a browser.

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

## Skill contract — `newproject`

### Special-casing

The `newproject` skill runs **before** any workspace exists. There is no
persona registered, no OrchestrationPlane workspace, no `.gemba/` to
read config from. It is bundled with the binary and invoked directly.

This makes it different from every other Gemba skill. Specifically:

- **No persona host.** It's not "the Onboarder persona running a skill".
  The skill module itself is the runtime.
- **No OrchestrationPlane.** It runs inline in the server process; no
  agent dispatch.
- **No workspace context provider.** Inputs are exactly what the
  conversation produces — there is no codebase, beads database, or
  decisions log to read from.

### Bundled prompt scaffolding

The skill ships with a prompt template covering:

- Role framing (a project planner who turns vague intent into a
  workable Milestone → Epic → Bead tree).
- Output schema (typed `NewProjectState` — see below).
- Few-shot examples spanning different project shapes (web app,
  library, ops tooling, research project).
- Guardrails on output (no premature implementation detail, milestones
  must be testable, every epic must roll up to a milestone, every bead
  must roll up to an epic).

### Output schema

The skill emits structured output validated against:

```go
type DraftMilestone struct {
    Title       string
    Description string
    Epics       []DraftEpic
}

type DraftEpic struct {
    Title       string
    Description string
    Beads       []DraftBead
}

type DraftBead struct {
    Title       string
    Description string
    Type        string  // "task" | "bug" | "feature" | "chore"
    Priority    int     // 0..4
}
```

Validation runs after every turn. Validation failures raise a structured
error to the operator ("the skill returned a milestone with no epics —
asking it to retry") rather than poisoning the plan tree.

### Credential resolution

The skill resolves an LLM client by reading the same configuration the
OrchestrationPlane reads for its agents. Specifically: it asks the
agent-config layer for "the default chat client" and uses that. This
avoids a separate first-run credential prompt — if the operator has
already configured Gemba to run agents, they have a working LLM
endpoint by definition.

If no agent client is configured, the skill surfaces a clear
diagnostic at the start of `/new`: *"No LLM client configured. Set up
agent credentials in `~/.gemba/config.toml` before starting a New
project conversation."*

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
   user-configurable, defaults to `~/projects/`).
2. Create the dir. Failure if it already exists — operator must pick a
   different name (the skill should warn about collisions during the
   conversation).
3. `git init` in the new dir on `main`.
4. Write `.gemba/workspace.toml` with project metadata.
5. Initialize the beads database in the new workspace.
6. For each milestone (in order): create as `bd epic -l type:milestone`;
   capture the new ID for parenting children.
7. For each epic under each milestone: create as `bd epic` with parent
   = the milestone ID.
8. For each bead under each epic: create with parent = the epic ID,
   type/priority from the draft.
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

| Path | Verb | Purpose |
| --- | --- | --- |
| `POST /api/v1/newproject/start` | open a conversation, return a session ID |
| `POST /api/v1/newproject/:id/turn` | submit an operator message, return the updated state + skill reply |
| `POST /api/v1/newproject/:id/ratify` | nonce-confirmed atomic commit |

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
2. `~/projects/` (built-in default; created on first project if
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
