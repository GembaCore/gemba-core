---
title: "Two-axis work planning and dispatch"
decision: none
---

# Two-axis work planning and dispatch

> Status: **draft / for review** — 2026-04-26
> Owner: gemba mayor
> Scope: a first-class subsystem inside gemba that analyzes ready work
> along two orthogonal axes (target collision and conceptual affinity),
> produces parallel-safe dispatch plans, matches work to the agent best
> primed to execute it, and recycles agent sessions before context drift
> degrades quality. Applies to both an interactive **coach mode** and an
> autonomous **auto-dispatch mode**.

## 1. Why this exists

Gemba's job is to keep useful work flowing through a fleet of agent
sessions. Today, dispatch is essentially round-robin against `bd ready`
output: pick a bead, find an idle agent, sling. That ignores two things
a good human PM would never ignore:

1. **Two beads that touch the same files cannot be done in parallel.**
   Doing them anyway produces merge conflicts, wasted polecat lifetimes,
   and silent semantic regressions when one bead invalidates the other.
2. **An agent that just spent 90 minutes inside the auth module is the
   cheapest agent in the fleet to give the next auth bead to.** They
   already loaded the context. Handing that bead to a fresh session
   pays the cold-start cost (re-prime, re-read, re-orient) and abandons
   the warm context the prior agent already paid for.

The first concern argues for *spacing work apart*. The second argues for
*clustering work together*. They pull in opposite directions. The
planner must hold both in tension instead of collapsing them.

## 2. The two axes, named

### 2.1 Target axis — *will these beads fight each other?*

A pessimistic, conflict-graph problem. Two beads are **target-adjacent**
when *any* of three relations hold:

1. **File overlap** — declared `targets[]` globs intersect.
2. **Semantic dependency** — one bead modifies a public contract the
   other consumes (requires source analysis; §5.3).
3. **Workspace collision** — both beads would land in the same
   *operational target* (same repo + branch, or two beads requiring
   write access to the same worktree). A worktree is a single
   working copy; two writers on it serialize at the filesystem level
   regardless of whether their file globs overlap.

Target-adjacent beads:

- MUST NOT be dispatched in parallel (merge-conflict or worktree-write
  guarantee).
- SHOULD be ordered: do A, integrate, then do B, so B's author sees
  A's diff.

The output of target-axis analysis is a **conflict graph**: nodes are
ready beads, edges (typed by which relation triggered them) connect
target-adjacent pairs. A maximum independent set on this graph is a
**parallel-safe batch**.

### 2.2 Concept axis — *who is primed to do this cheaply?*

An optimistic, affinity-vector problem. Each bead has a `concepts[]`
tag set drawn from a controlled vocabulary (`auth`, `e2e-fixture`,
`dolt-server`, `spa-routing`, ...). Each live agent session
accumulates a recency-decayed concept profile from the work it has done
so far in the session.

The output of concept-axis analysis is, for each (bead, session) pair,
a scalar **affinity score** with an explanation. High score means
"this session is already warm for this bead."

### 2.3 Why orthogonality matters

In practice, target-adjacent beads are *often* concept-adjacent too
(work in the same file shares concepts). So the planner will routinely
discover: "the highest-affinity bead for session S is also the bead
most likely to conflict with the bead S just shipped." The right answer
is usually to **serialize** within the session (let S finish, integrate,
then take the next concept-adjacent bead) rather than to **parallelize
across sessions** (which would conflict). The planner must distinguish
these cases — collapsing the axes hides the choice.

## 3. Vocabulary

Stable terms used throughout this document and the code.

| Term | Definition |
|------|------------|
| **WorkItem** | A unit of dispatchable work. In the bd adaptor, a bead. |
| **Target** | A path glob the WorkItem is expected to modify. May be multiple. |
| **Concept** | A short tag from a controlled vocabulary describing the conceptual area of the work (`auth`, `e2e-fixture`). |
| **Session** | One live agent worker (crew or polecat) with a continuous context. Identified by `gt` session id. |
| **Session profile** | A recency-decayed vector of concepts and files the session has touched. The session's "warm context" snapshot. |
| **Affinity** | Scalar score in [0, 1] for (WorkItem, Session). Higher = session is more primed. |
| **Conflict** | Boolean (and a reason) for (WorkItem, WorkItem). True = MUST serialize. |
| **Conflict graph** | Graph over a ready set: nodes=WorkItems, edges=conflicts. |
| **Parallel-safe batch** | An independent set in the conflict graph, dispatchable concurrently. |
| **Workspace** | The existing `core.Workspace` struct (`mayor/rig/internal/core/orchestration.go:174`): repo + branch + base SHA + isolation kind. `WorkspaceKind` enumerates `worktree, container, k8s_pod, vm, exec, subprocess`; `worktree` is the preferred dispatch target. |
| **Operational target** | The (repo, branch, worktree-path) tuple a bead would land in. Derived from existing Workspace + the bead's `RepositoryIDs` / branch convention. Distinct from `targets[]` (which is *files*). |
| **Operational context** | The full per-agent picture: identity (`AgentRef`), live workspace (`Workspace`: repo + branch + worktree path + isolation kind), live session (`Session`: status + heartbeat + cost), session profile (concepts + files), and session health (pressure + drift + time). Pulled together by the planner; surfaced as a single card in coach mode. |
| **Source analysis** | An abstract capability that, given a symbol or file, returns its dependency neighborhood. Implementations may use GitNexus, ctags, LSP, or a stub. The planner also schedules *re-indexing* of this capability — see §8. |
| **Source analysis scan** | A re-index run of the configured source analysis tool (e.g. `gitnexus analyze`). Scheduled by the planner as a first-order activity (§8). |
| **Turn retrospective** | A post-merge analysis that compares declared (`targets`, `concepts`) against actual (files touched, symbols changed) and updates priors. |
| **Concept drift** | Cosine distance between a session's *recent-window* concept vector and its *lifetime* concept vector. High drift = the session has shifted topic. |
| **Context pressure** | Fraction of the agent's context window in use. |
| **Recycle** | Cleanly end a session via `gt handoff` so the next bead starts in a fresh context. |
| **Score** | The Layer-3 output: how *cheaply* a bead can be done well by a session. Pure function over targets / concepts / profile / source-analysis (§4 Layer 3). Same inputs → same score. |
| **Selection** | The Layer-5 output: which bead this session should do *next*. Composes Score with operator/session-level signals (intent, soft-blocks, owner-claim, runway). Distinct from scoring (§3.5). |
| **Justification** | The accumulated per-component reasons that produced a Score and a Selection. Every score sub-component contributes one line; every selection-time gate adds one. Surfaced verbatim to the operator (§4 Layer 5). |
| **Blocks-weight** | Score component (§4 Layer 3): how many open beads are blocked by completing this one. Read off the dependency graph; a leaf bead has weight 0, a bead blocking a 5-child epic has weight 5+. Counters the affinity-only bias toward "pick what's cheap regardless of leverage." |
| **Epic-affinity** | Score component (§4 Layer 3): is the candidate bead a sibling of one this session has already completed *this turn*? Captures the operator-observed bias toward closing out an epic before opening another. Decays per-turn, not per-bead. |
| **Agent profile** | A persistent recency-decayed concept + file vector keyed on `AgentRef`, surviving session handoff. Sister to Session profile (§4 Layer 1.2): same shape, longer half-life, different question — "what is this *agent* good at?" vs. "what is this *session* warm on?" |
| **Session intent** | An operator-set focus directive scoped to one session: an epic id, a label, a bead-id regex, or a free-text rationale. The Selection layer respects it as a soft prefilter — beads outside intent are demoted, not excluded (§4 Layer 1.3). |
| **Dispatch status** | A bead-level enum orthogonal to bd `status`: `ready` / `awaiting-design` / `awaiting-vendor` / `awaiting-review` / `not-now`. Selection respects soft-blocks; the conflict graph doesn't see them. Operator-pinned (§4 Layer 0). |
| **Bead size estimate** | A heuristic scalar (small / medium / large; or token-budget bucket) for how much session runway this bead consumes. Bootstrap from description length × DoD line count, calibrate from retrospective time-to-close (§4 Layer 0). |
| **Session runway** | An estimate of how much *productive work* the current session has left before recycle: derived from context-pressure + concept-drift + time-on-task plus a calibration bias from completed-vs-promised cycles in this session (§4 Layer 1.4). |
| **Owner / claim** | The agent currently working a bead (in_progress + assignee from bd). Selection treats a claimed bead as soft-conflicted against any other session, so two agents in a fleet don't race the same bead (§4 Layer 5). |
| **Recommendation calibration** | The retrospective signal that grades planner *recommendations* against operator *picks*: when the planner says "do X" and the operator picks Y instead, the delta is recorded so the score weights can re-tune (§7.5). |

## 3.5 Selection vs. scoring — the load-bearing distinction

The original spec collapsed two questions into "scoring":

1. **"Which bead is cheapest to do well right now?"** — pure function over
   bead enrichment, session profile, and source analysis. Same inputs
   produce the same answer. This is **scoring** (§4 Layer 3).

2. **"Which bead should *this session* do *next*?"** — a higher-level
   decision that uses the score *plus* a bundle of session-level and
   operator-level signals: is the operator focused on a particular
   epic? Is another agent already on this bead? Does the session have
   enough runway? Is the bead soft-blocked on a vendor? Is the score
   even comparable across the candidate set?

   This is **selection** (§4 Layer 5). Same inputs do *not* always
   produce the same answer — selection is parameterized by the
   *moment*, not just the data.

These are not the same question, and they don't fit in the same
layer. A bead can be the highest-scoring work in the workspace and
still be the wrong selection: someone else owns it, the session has
20 minutes of runway and the bead's a 4-hour atomic refactor, the
operator's pinned focus is elsewhere. The score isn't *wrong* — it
honestly answered the question it was asked.

The same bead can also be a low-scoring choice that's the right
selection: the operator-set focus says "finish the gm-s47n epic
this turn", and the cheapest gm-s47n leaf is a P3 with a poor
affinity match. Selection prefers it because the operator's intent
trumps cheapness.

This document treats scoring and selection as orthogonal:

- Scoring is **stateless** with respect to operator preferences.
  The same bead always scores the same way against the same
  session profile.
- Selection is **stateful** — it composes the score with the
  session-purpose, soft-block, owner-claim, and runway gates that
  do change moment-to-moment.
- The retrospective grades both: scoring against outcome (cycle
  time, rework, conflict count); selection against operator
  override (recommendation calibration, §7.5).

The rest of §4 follows this split: Layers 0-3 build the scoring
substrate; Layer 5 is selection on top.

## 4. Primitives, in dependency order

The system is built bottom-up. Each layer is independently useful even
if higher layers are never built — important because the early layers
are cheap and the later layers depend on the data the early layers
collect.

### Layer 0 — WorkItem enrichment (data only)

Add four structured fields to every WorkItem:

- `targets[]` — declared path globs the item is expected to touch.
- `concepts[]` — tags from the controlled vocabulary.
- `dispatch_status` — the soft-block enum (default `ready`):
  `ready` / `awaiting-design` / `awaiting-vendor` / `awaiting-review` /
  `not-now`. Selection (Layer 5) respects this; the conflict graph
  (Layer 3) ignores it. A bead in `ready` status is the only kind
  selection considers a candidate; the others are visible in `bd
  list` but suppressed from the planner's "what's next" surface.
- `estimated_size` — `small` / `medium` / `large` (or a token-budget
  bucket once the calibration loop in §7.6 lands). Bootstrap from
  description-length + DoD-line-count; the retrospective grades it
  against actual time-to-close so the heuristic gets sharper. Used
  by Layer 5 to compare bead size against session runway.

For the bd adaptor, store all four in the bead's structured-extras
map, not in the body, so they're queryable.

**Bootstrap**: at WorkItem creation, an LLM extracts a first guess from
title + body + any linked spec. Human can override at any time. All
four fields are *advisory* until the turn retrospective (§7) starts
grading them.

#### Layer 0 — Extractor (gm-s47n.1.2)

The extraction half of Layer 0 lives behind a small `Extractor`
interface so backends can swap freely:

- `NoopExtractor` — empty enrichment; safe default when no provider
  is wired.
- `HeuristicExtractor` — network-free, ships in the binary. Mines
  path-shaped tokens (backtick-fenced or bareword with a recognized
  prefix from `DefaultHeuristicPathPrefixes`) for targets; matches
  the supplied vocabulary against bead text with word-boundary,
  case-insensitive, dash/underscore/space-flexible comparisons for
  concepts.
- A future LLM-backed extractor (Anthropic / Bedrock / local model)
  will land behind the same interface. It does not need to ship for
  the bead-creation pipeline to start producing useful enrichment;
  the heuristic extractor's output is operator-overridable via
  `gemba bead targets/concepts set`.

`Extractor`s MUST be pure with respect to their inputs (same
`BeadInput` → same `Enrichment`). `BeadInput` carries title + body
+ optional spec + the active vocabulary so the extractor can match
against the closed concept set.

The CLI hook is `gemba bead extract <id>` with `--title`, `--body`,
`--spec`, and `--body-file` / `--spec-file` flags. `--dry-run`
previews; `--merge` unions with any existing enrichment instead of
replacing — operator-pinned targets / concepts survive a re-extract
that way, and the operator's `Source` stamp is preserved.

#### Layer 0 — Backfill (gm-s47n.1.4)

`gemba bead backfill` walks every bead the bd CLI surfaces and
runs the configured extractor against each one. Best-effort:
per-bead errors land in the report but don't abort the loop.

The runner is decoupled from bd via a small `BeadSource` interface
(`Iter(ctx, yield)` per bead). The shipping production source is
`BdJSONSource` — wraps `bd list --json`. `MemoryBeadSource` powers
tests. Adding a new source (e.g. a JSONL file an external tool
produced) is a one-method change.

Defaults reflect the operator-safe posture:

- `--skip-existing=true` — beads with non-empty enrichment stay
  alone; operator pins never get clobbered.
- `--all=false` — closed beads are excluded; the planner reads
  enrichment off active work.
- `--filter` (regex over bead id) and `--limit` compose: filter
  applies first, then limit caps the number of post-filter beads
  the runner attempts. `--filter ^gm-s47n --limit 5` reliably
  attempts the first five `gm-s47n` beads instead of bailing
  after five total of any id.
- `--dry-run` produces the report without persisting.

Source on backfill writes is always `SourceBackfill` so the
operator can grep `bead show` output for backfilled vs operator-
pinned vs interactively-extracted entries.

#### Layer 0 — CLI surface (gm-s47n.1.3)

`gemba bead {show, list, targets, concepts, extract}` is the
operator surface for the override / inspect side. The package
`internal/enrichment/` ships the data type + a small `Store`
interface (`Load` / `Save` / `List`) so the storage backend can swap:

- Today: `FileStore` — one JSON file per bead under
  `<workspace>/.gemba/enrichment/<safe-id>.json`. Slashed bd ids
  (`gemba/gemba/gm-1`) escape `/` → `__` so workspace-prefixed ids
  round-trip across filesystems. Atomic write (tmp + rename).
- After **gm-s47n.1.1** lands the WorkItem.targets / .concepts
  schema: a `BdExtrasStore` reads + writes through the bd adaptor.
  CLI surface stays unchanged.

`gemba bead concepts add` cross-checks the new tag against the
loaded vocabulary (`internal/concepts`) and prints a stderr warning
on unknown tags, but the edit still applies — vocabulary is
operator-driven (§6.4) so the CLI must not block on a not-yet-
proposed concept. `--force` suppresses the warning for scripted use.

### Layer 1 — Session profile + operational context (data only)

The session profile is **not a standalone object**. It is a join over
existing core structs plus a small set of new fields. The planner
reads it through one query but the data lives in the right places.

**Existing structures the profile composes with**
(`mayor/rig/internal/core/orchestration.go`):

- `AgentRef` — agent identity (id, name, kind, role, workspace).
- `Session` (line 268) — `id`, `assignment_id`, `agent_id`, `status`,
  `started_at`, `last_heartbeat`, `transcript_ref`, `cost_samples`.
- `Workspace` (line 174) — `id`, `kind` (`WorkspaceKind`: prefer
  `worktree`), `repository`, `branch`, `base_sha`, `status`,
  `isolation`, `provider_metadata`, `created_at`, `released_at`.
- `Assignment` — binds `agent → work_item → workspace → session`.

**New: `session_profiles` table** — keyed by `session_id`, joins to
the above and adds:

- `concepts` — `{tag: weight}` map, sum of recency-decayed
  contributions from completed beads.
- `files` — `{path: weight}` map, same decay.
- `tokens_used`, `context_pct` — last-known agent telemetry.
- `last_beads[]` — ring buffer of last N completed bead ids.
- `last_activity_at` — separate from `Session.last_heartbeat`; updated
  on bead-event boundaries, not on every health ping.

**New: `Workspace.worktree_path`** — currently the worktree path lives
implicitly in `provider_metadata` for `Kind == worktree`. The planner
needs it as a first-class field so it can detect workspace collision
without parsing per-provider metadata. Either promote to a typed
field on `Workspace`, or define a stable `provider_metadata["worktree_path"]`
contract; either is fine, but pick one.

**Operational-context read** — the planner doesn't read these in
isolation. It calls `OperationalContext(session_id)` which returns the
join: AgentRef + Session + Workspace + session_profile +
session_health (§4 Layer 4). This single struct is what the scorers,
coach UI, and auto-dispatch all consume.

Decay function: exponential with a half-life expressed in *bead
events*, not wall time, so an idle session doesn't lose its priming.
Default half-life: 5 beads.

The profile is updated on two triggers:
1. Bead claim — add the bead's declared concepts and targets at full weight.
2. Bead completion — replace declared with *actual* (from turn retrospective)
   and recompute decay.

Lives in dolt because:
- It must survive agent crashes and restarts.
- The planner queries it for every dispatch decision.
- It is itself reviewable history — you can ask "what was session S
  primed on, in which workspace, when it took bead X?"

#### 1.2 Agent profile (persistent across sessions)

Sister to the session profile. Same shape (`concepts {tag:weight}` +
`files {path:weight}`), keyed on `AgentRef.ID`, but with two key
differences:

- **Survives `gt handoff`.** A new session inherits its agent's
  profile as a warm starting point, then accumulates session-specific
  weight on top. The session profile decays per-bead with half-life
  5; the agent profile decays per-day with half-life ~14d.
- **Different question.** Session profile answers "what is this
  session warm on right now?" Agent profile answers "what is this
  agent *good at* over weeks?" Mike4 has been deep in the e2e
  library and the planner family across multiple sessions —
  selection should know that even on Mike4's first bead of a fresh
  session.

The retrospective (§7) writes both profiles on bead completion: the
session row gets the bead's actual concepts/files at full weight; the
agent row gets the same contribution scaled by `1 / (lifetime bead
count)` so a single bead doesn't dominate.

Score-side, the affinity component in §4 Layer 3 reads BOTH
profiles, weighted (default 0.7 session + 0.3 agent — tunable). A
fresh session post-handoff with an empty session profile inherits
its agent's affinity surface; over time the session profile dominates
as it accumulates its own weight.

#### 1.3 Session intent (operator-pinned focus)

A small struct attached to a session by the operator to bias
selection toward a particular slice of work:

- `epic_id` — restrict candidates to descendants of this epic.
- `label` — restrict candidates carrying this bd label.
- `bead_id_regex` — restrict candidates whose id matches.
- `rationale` — free-text "why this focus" for the audit log.

Intent is **soft**: selection demotes candidates outside intent
rather than excluding them. A P0 bead outside intent can still beat
a P3 bead inside intent if the score gap is wide enough. The
demotion factor is operator-tunable per intent (default 0.4 — a
0.8 in-intent score beats a 1.0 out-of-intent score).

Set via `gemba session focus <session-id> --epic <id>` or `--label`
or `--regex`; cleared via `gemba session focus <session-id>
--clear`. Audit row written for every change.

Without explicit intent, selection's `epic-affinity` heuristic
(§4 Layer 3) supplies a softer version of the same signal: if this
session has been consistently working `gm-s47n.*` beads this turn,
the planner biases toward more `gm-s47n.*` even without an explicit
focus directive.

#### 1.4 Session runway (estimate of remaining productive work)

Derived from the existing health telemetry (Layer 4) plus a
calibration bias:

- Start with `1 - context_pressure` as the upper-bound runway in
  "session lifetimes."
- Subtract a `concept_drift` penalty: a session that's drifted hard
  in the last 3 beads has less runway for a new topic.
- Multiply by a calibration scalar from this session's
  promised-vs-actual cycle on the last few beads. A session that
  consistently overruns its declared bead estimate by 2x gets a
  0.5 runway scalar.

The output is a `(small / medium / large)` bucket comparable with
the bead's `estimated_size` (Layer 0). Selection rejects bead
candidates whose size exceeds available runway; `gemba session
status` surfaces it for operator inspection.

This is **read-only / advisory** in the same posture as Layer 4 —
the planner's `auto-dispatch` mode respects it; coach mode shows
the score but lets the operator pick anyway with a one-line
warning.

### Layer 2 — Source analysis (capability, abstract)

Define an internal interface; do not bind to a specific tool.

```go
type SourceAnalysis interface {
    // Files that import, call, or otherwise depend on the given target.
    Dependents(ctx context.Context, target Target) ([]Target, error)

    // Files the given target depends on.
    Dependencies(ctx context.Context, target Target) ([]Target, error)

    // Best-effort: symbols changed in the given diff that have public
    // contracts (exported APIs, route signatures, exported types).
    PublicContractChanges(ctx context.Context, diff Diff) ([]Symbol, error)

    // Health: index freshness, what backend is in use.
    Describe(ctx context.Context) (SourceAnalysisCapabilities, error)
}
```

Provide at minimum:
- A **GitNexus** implementation (the rich one).
- A **noop** implementation (for environments where source analysis
  isn't installed — degrades gracefully: target conflict still works
  on glob overlap, semantic conflict detection is silently skipped).

This abstraction is **a hard dependency** for semantic-conflict
detection (§5.3). Without it, the conflict detector sees only literal
target overlap and misses two beads that touch disjoint files but
invalidate each other's API assumptions. The interface keeps gemba
from being chained to a single tool.

### Layer 3 — Scorers (compute)

Two pure functions over the data in Layers 0–1, with optional Layer 2
input.

#### 3.1 `Conflicts(beads []WorkItem, live []OperationalContext) ConflictGraph`

For each unordered pair (a, b) in the input set, classify:

- **Target conflict** if the glob set of `a.targets` and `b.targets`
  intersect non-trivially (overlap algorithm in §5.2).
- **Semantic conflict** if Layer 2 reports that `a` modifies a public
  contract that `b` consumes (or vice versa). Requires source analysis;
  skipped silently if unavailable.
- **Workspace conflict** if both beads route to the same operational
  target — same `(repo, branch)` pair, or both require write access
  to the same `worktree_path`. The planner cross-references against
  `live` (currently active operational contexts) so a bead routed to
  a worktree another session is already writing in is flagged even if
  no other ready bead in the set conflicts on files.
- **Otherwise**: no edge.

Edge metadata records which kind of conflict and a one-line reason
(for the explanation surface).

#### 3.2 `Affinity(bead WorkItem, ctx OperationalContext) (float64, Justification)`

Takes the joined operational-context struct (§4 Layer 1) so it can see
agent identity, workspace, profile, and health together.

Compute seven sub-scores in [0, 1]:

- **Concept overlap (session)**: cosine similarity between
  `bead.concepts` (one-hot) and `ctx.session_profile.concepts`
  (decayed weights).
- **Concept overlap (agent)**: same, against `ctx.agent_profile.concepts`.
  The composite "concept" sub-score is `0.7 * session + 0.3 * agent`
  (tunable). On a fresh session post-handoff the agent half carries
  the weight; over time the session half dominates (§4 Layer 1.2).
- **File familiarity**: fraction of `bead.targets` that intersect
  `ctx.session_profile.files` weighted by decay. Uses session-only
  here — file familiarity decays fast and the agent-level signal is
  already captured by the source analysis layer.
- **Workspace match**: 1 if `bead.repository ∈ ctx.workspace.repository`
  AND `bead.branch_convention` matches `ctx.workspace.branch`; 0.5 if
  same repo / different branch; 0 if different repo. Multi-repo
  beads take the max over declared repos.
- **Epic-affinity**: 1 if the candidate bead is a sibling (same
  parent epic id) of a bead this session has *closed* this turn;
  decays per-turn, hard 0 once a different epic has been
  contiguously worked. The "in-progress epic gravity" from §3.5 —
  expresses that finishing 75%-done epics beats starting new ones.
- **Recency**: 1 if the session's most recent bead shared a concept
  with this one; decays linearly to 0 over ~10 beads.
- **Headroom**: 1 if `ctx.health.context_pct < 0.5`; decays linearly
  to 0 at 0.85; hard 0 above 0.9.

Combined score: weighted sum (default weights 0.25 concept / 0.15
file / 0.15 workspace / 0.15 epic-affinity / 0.15 recency / 0.15
headroom; tunable). Returns a `Justification` slice — every sub-score
contributes one line — so the coach surface and the audit log can
render *why* without re-running the math.

#### 3.3 `Leverage(bead WorkItem, deps DependencyGraph) (float64, Justification)`

Pure score over the bead's downstream impact in the dependency graph.
Counters the affinity-only bias toward "pick what's cheapest"
regardless of how many open beads it would unblock. A leaf bead with
no downstream dependents has leverage 0; a bead blocking a 5-child
epic has leverage proportional to the open count in its transitive-
dependents subgraph.

The score is `1 - exp(-k * blocks_weight)` so an isolated leaf maps
to 0, single-blocker beads to ~0.4, and 5+-blocker beads asymptote
toward 1. Selection (Layer 5) combines Leverage with Affinity via
a tunable mix (default `0.7 * affinity + 0.3 * leverage`). Operators
preferring "knock out small wins to clear the queue" can lower the
leverage weight; operators on a deadline boost it.

Justification names the specific blocked beads (by id), so the
operator sees not just "leverage 0.6" but "blocks gm-X, gm-Y, gm-Z."

`Leverage` is part of the score because it's a *property of the
bead*, not of the moment — same dependency graph means same
leverage. (Selection-time signals like owner-claim and runway live
in Layer 5.)

### Layer 4 — Session-health telemetry (read-only first)

Per active session, expose three numbers:

- **Context pressure** = `tokens_used / context_window_max`.
- **Concept drift** = cosine distance between the session profile over
  its last 3 beads and the session profile over its lifetime.
- **Time-on-task** = wall clock since `started_at`.

Surface as `gemba session-health` (CLI) and as a SPA panel. Define
**advisory thresholds**:

- `context_pressure > 0.6` → warn.
- `context_pressure > 0.8` → strongly suggest recycle before taking new work.
- `concept_drift > 0.5` → warn.
- `concept_drift > 0.7` → suggest recycle when next bead's concepts
  differ from session lifetime average.

Phase 4 is **read-only**. The planner can read these and *suggest*; it
must not auto-kill sessions. Auto-recycle (§4.5) is opt-in and gated
behind explicit configuration.

### Layer 5 — Selection (compose Score with session-level signals)

Selection is the §3.5 "which bead should *this session* do *next*?"
question. It takes the per-(bead, session) `Score` from Layer 3 and
composes it with the moment-dependent signals that Layer 3 deliberately
leaves out:

#### 5.1 Inputs

- `Score` and `Justification` from `Affinity(bead, ctx)` and
  `Leverage(bead, deps)` for every (ready bead, this session) pair.
- `ctx.intent` — the operator's session-pinned focus (§4 Layer 1.3).
  May be empty.
- `ctx.runway` — the small/medium/large estimate (§4 Layer 1.4).
- `bead.dispatch_status` — the soft-block enum (§4 Layer 0). Beads
  not in `ready` are dropped before scoring even runs; they never
  reach selection. The reason is recorded in the report so the
  operator sees "5 candidates suppressed: 3 awaiting-design, 2
  not-now."
- `bead.estimated_size` (§4 Layer 0).
- `claim_index[bead] -> session_id` from the OperationalContext
  registry (§4 Layer 1) — a bead claimed by another live session
  is soft-conflicted against this session's selection.

#### 5.2 Selection gates (in order)

The gates run in sequence; the first that fires demotes or excludes
the candidate, with a one-line reason added to its `Justification`.

1. **Dispatch-status filter** (hard): `bead.dispatch_status != ready`
   → exclude.
2. **Owner-claim filter** (hard): `claim_index[bead] != nil &&
   != ctx.session_id` → exclude. Two agents in a fleet can't
   double-claim a bead just because both score it well.
3. **Conflict filter** (hard): bead conflict-adjacent to a bead
   currently being worked by another session → exclude.
4. **Runway gate** (soft): `bead.estimated_size > ctx.runway` →
   demote by 0.5. Coach mode shows the warning and lets the
   operator override; auto-dispatch respects the demotion.
5. **Intent gate** (soft): `ctx.intent != nil && bead ∉ intent` →
   demote by `ctx.intent.demotion_factor` (default 0.4). Out-of-
   intent P0 beads can still beat in-intent P3 beads when the score
   gap is wide enough.
6. **Fairness boost** (soft): each candidate gains affinity
   proportional to its age in the ready queue. Stops the planner
   from starving hard work in favor of cheap concept-matched work.

The output is a sorted list of `(bead, score, justification)`
tuples. Coach mode renders the top-N; auto-dispatch picks the top-1
(or skips if the top score falls below a configurable floor).

#### 5.3 Selection is stateless — but its INPUTS are not

Selection itself is a pure function over its inputs. The non-pure
behavior over time comes from inputs changing: intent gets pinned,
the claim_index updates as sessions take and finish work, runway
estimates shift as the session's context-pressure climbs.

This matters for testing — selection can be exercised with a frozen
input bundle and produce reproducible outputs. The planner's
correctness can be debugged without time-travel.

#### 5.4 Claim model — adaptor-declared atomicity boundary (gm-e3.8)

Selection produces a sorted list of candidates; **claiming** a bead
(committing the dispatch so no other session takes the same work) is
an adaptor concern. Different orchestration adaptors solve the
cross-session race differently, and the planner declines to layer a
TTL'd reservation contract on top of an adaptor that already has its
own atomic claim primitive. Each `OrchestrationCapabilityManifest`
declares a `claim_model`:

- **`inline`** (default for every adaptor in tree today). The claim
  happens *inside* `StartSession`. The adaptor's spawn primitive is
  atomic with the hook: `gt sling` rejects on the
  bead-already-hooked branch; the native adaptor refuses a second
  StartSession for a bead already in flight on another session. The
  planner does NOT call `ClaimNextReady`; `ClaimNextReady` /
  `ReleaseReservation` may legitimately return `KindUnsupported` for
  an inline-claim adaptor — that's the deliberate adaptor shape, not
  a gap to fix. On the inline path, the planner picks a candidate,
  calls `StartSession`, and on a tagged `core.ErrBeadAlreadyClaimed`
  error treats the loss as a **soft skip**: pick the next candidate
  from the ranked list. The retry budget is bounded
  (`MaxSoftSkipRetriesPerTick`, default 3) so a misbehaving cluster
  of beads can't blow up a single tick.

- **`two_phase`** (reserved for adaptors with explicit
  hold-without-spawn semantics; none in tree today). The planner
  calls `ClaimNextReady` to obtain a TTL'd `Reservation`, then
  `StartSession` to convert. Reservations auto-release if the
  session never spawns. This is the historical Gemba contract; it
  remains reachable via the manifest gate so a future adaptor can
  opt in without rewiring the daemon.

The framing matters: `gt sling` IS the atomic claim. Filing
`KindUnsupported` on Gas Town's `ClaimNextReady` is the *correct*
adaptor shape, not a follow-up. Adaptors declaring the wrong claim
model — e.g. an inline adaptor stamping `claim_model: two_phase` —
fail conformance Group F at registration; the planner cannot rescue
a manifest that lies about its claim semantics.

### Layer 6 — Surface (coach + auto-dispatch UX)

Two modes share the same selection engine. The mode flag determines
who makes the final dispatch decision.

#### 6.1 Coach mode (interactive PM)

A SPA view with two halves:

- **Agent context strip** — one card per live session showing the
  full operational context: agent name + role, repo, branch,
  worktree path, isolation kind (with a worktree icon for the
  preferred case), session status, last heartbeat, top concepts in
  the profile, context pressure, concept drift, time-on-task,
  pinned intent (§4 Layer 1.3), runway estimate (§4 Layer 1.4).
  This is the operator's at-a-glance view of *who is loaded with
  what* and *where they're working*.
- **Dispatch grid** — rows are ready beads, columns are agent cards
  from the strip. Each cell shows the selection output:
  `(score, justification)`. The justification IS the explanation —
  every selection-time gate (intent demote, runway warn, claim
  exclude) and every score component (concept, leverage, epic-
  affinity) contributes one line. Conflict edges between beads are
  rendered as grouped highlights — picking one bead dims the cells
  of its conflict-adjacent siblings.

The coach (human) picks. The system records the pick along with
the full score + justification at decision time so the retrospective
can grade BOTH the score (against outcome) AND the recommendation
(against operator override) — see §7.5.

This mode is a faithful instrument of what a senior PM does in a
live session today. It does not change the workflow, only surfaces
the data behind it.

#### 6.2 Auto-dispatch mode

A daemon loop. When a session becomes idle, the planner:

1. Reads the ready set, the session's operational context, and the
   live claim_index across the rig.
2. If the session is over a hard recycle threshold, trigger
   `gt handoff`; the next iteration of the loop will see a fresh
   session and re-decide.
3. Run Layer 5 selection over the ready set for this session.
4. If the top selection's score falls below `auto_dispatch_floor`
   (default 0.5), do nothing — wait for either new ready beads or
   for operator-set intent to bias the selection. Don't sling
   low-confidence picks.
5. Otherwise dispatch the top bead via the path declared by the
   adaptor's `claim_model` (§5.4). The selection's `Justification` is
   stamped on the dispatch event so the auto-dispatch decision is
   auditable post-hoc.

Step 5 in pseudo-code:

```
candidates ← rank ready set
top ← top above floor
if manifest.claim_model == inline:
  for cand in candidates (bounded by MaxSoftSkipRetriesPerTick):
    err ← StartSession(cand.bead)
    if IsAlreadyClaimed(err):
      # another session won the inline race; record OutcomeAlreadyClaimed
      # and walk to the next candidate
      continue
    return DispatchResult{cand, err}
else:  # two_phase
  reservation ← ClaimNextReady(top.bead)
  StartSession(reservation)
```

Auto-dispatch is **opt-in per rig** with a kill-switch in rig
settings. A bad scorer on a fast loop can do real damage; the
kill-switch is non-negotiable.

Coach mode and auto-dispatch share the same `Layer 5 Selection`
output; the difference is who reads the sorted list. This is the
load-bearing reason for the §3.5 selection-vs-scoring split — auto-
dispatch's correctness is *exactly* "the operator would have picked
the same top-1 in coach mode," which the recommendation calibration
loop (§7.5) measures.

## 5. Algorithms

### 5.1 Concept profile decay

Let `e_1, ..., e_n` be bead-completion events for a session, oldest to
newest, each with concept set `C_i`. With half-life `h` (in events),
weight of event `e_i` at time of event `e_n` is:

```
w_i = 0.5 ^ ((n - i) / h)
```

Session concept weight for tag `t`:

```
S(t) = Σ_{i : t ∈ C_i} w_i
```

This favors recent work without erasing older priming. Half-life in
events (not wall time) so a session that was idle overnight still
"remembers" what it did yesterday.

### 5.2 Target glob overlap

Two glob sets `A` and `B` overlap when there exists at least one path
matched by some glob in `A` and some glob in `B`. Implementation:

1. If any glob in `A` exactly equals any glob in `B`, overlap.
2. Expand globs to a normalized prefix tree; if any prefix in `A` is a
   prefix of any in `B` (or vice versa), overlap.
3. As a safety net, if both sets are small (<20 globs), enumerate
   matched files against the working tree and intersect — catches
   awkward `**` patterns the prefix check misses.

False positives here are fine (they cause unnecessary serialization);
false negatives are not (they cause merge conflicts).

### 5.3 Semantic conflict via source analysis

Given two beads `a` and `b`, both with target sets that don't overlap:

1. Ask source analysis for the **public symbols** likely to change in
   each bead — a heuristic, since we don't have the diff yet. Approximate
   from `targets` by taking exported symbols defined in those files.
2. For each public symbol `s` in `a`'s likely changes, ask source
   analysis for `Dependents(s)`. If any dependent file is in `b.targets`,
   mark a semantic conflict.
3. Symmetrically for `b`'s symbols against `a.targets`.

When source analysis is unavailable, this entire step is skipped. The
planner logs that semantic conflict detection was skipped so an
operator can see why two beads got dispatched in parallel that later
turned out to conflict.

### 5.4 Affinity composition

```
affinity = 0.30 · concept_overlap
         + 0.20 · file_familiarity
         + 0.20 · workspace_match
         + 0.15 · recency
         + 0.15 · headroom
```

Weights are configurable per rig. The retrospective (§7) grades these
weights against outcomes (cycle time, rework, merge conflicts) and can
recommend adjustments — but never auto-tunes without operator approval.
A self-tuning weight loop sounds smart and is a foot-cannon: it tunes
toward whatever metric you wrote down, not whatever you actually wanted.

### 5.5 Auto-recycle decision

Recycle the session **before** taking a new bead when **any** of:

- `context_pressure > 0.85` AND incoming bead's affinity is below the
  median for ready beads (i.e. the session isn't perfectly primed for
  this one anyway, so cold-starting costs little).
- `concept_drift > 0.7` AND incoming bead shares < 0.3 concept
  overlap with session lifetime.
- `time_on_task > 4h` AND incoming bead is the start of a new
  concept area.

Never recycle a session **mid-bead**. The handoff happens at the
boundary between completing one bead and accepting the next.

## 6. Concept vocabulary governance

Ungoverned tags become noise within weeks. The vocabulary needs care.

### 6.1 Initial vocabulary

Bootstrap from the rig's existing structure: top-level package names,
the SPA's route prefixes, the e2e fixture taxonomy. Aim for 30–60
concepts at the start. Resist the urge to be exhaustive.

### 6.2 Drift detection (continuous, lightweight)

As beads accumulate concepts over time, the system watches for:

- **Near-duplicates**: tags with cosine similarity > 0.85 in their
  co-occurrence vectors with other tags (`auth-token` and `auth-tokens`
  almost certainly mean the same thing).
- **Drifters**: tags whose co-occurrence pattern has changed
  significantly compared to their first 20 uses (the meaning shifted).
- **Singletons**: tags used on fewer than 3 beads after 90 days
  (probably a typo or a one-off).

These are surfaced as **suggestions**, not auto-applied. The
operator (or the coach in coach-mode) approves a merge / rename /
delete. Operator input is the only source of vocabulary changes.

### 6.3 Pruning

Periodic (e.g. monthly) review queue surfaces the suggestions in
priority order. Approving a merge rewrites historical bead concept
sets so the profile decay math stays consistent. The dolt commit
makes this auditable.

### 6.4 Why this is operator-driven, not LLM-driven

Vocabulary is a domain ontology. An LLM is great at proposing
candidates from co-occurrence patterns; it is bad at deciding whether
`auth` and `auth-token` are synonyms in this codebase or meaningful
distinctions (they might be — auth could mean *authorization* and
auth-token specifically *bearer tokens*). The human knows; the system
proposes.

### 6.5 Implementation notes (gm-s47n.7.1-.4)

The package `internal/concepts/` ships the four .7 children as one
cohesive subsystem. Highlights:

- **Storage**: `<workspace>/.gemba/concepts/{vocabulary,suggestions}.json`
  + `decisions.log` (JSONL append-only audit trail). Atomic writes
  via tmp + rename so a crashed run never leaves half-written state.
- **Bootstrap sources** (.7.1): `go-packages` (walks `internal/` +
  `cmd/`), `route-prefixes` (regex over `web/src/App.tsx`), and
  `fixture-taxonomy` (`testing/e2e/specs/*` directory names). Sources
  run in parallel; first-source-wins on duplicate names; cap at
  `--max` (default 60).
- **Drift thresholds** (.7.2): Jaccard 0.7 + use-ratio guard 0.5 for
  near-duplicates; `< 3 beads` + `dormant > 90d` for singletons. The
  Jaccard / cosine choice differs from §6.2's literal language because
  Jaccard is the right shape for sparse bead-id sets; the threshold
  is calibrated for similar precision. Drifters (semantic neighbor
  walks) defer to **gm-s47n.3** because they need the source-analysis
  abstraction.
- **Integration boundary** (.7.4): a small `BeadConceptStore`
  interface (`List` / `Set`) keeps the package independent of the
  WorkItem.concepts schema landing in **gm-s47n.1.1**. The in-memory
  implementation powers tests + CLI dry-runs; production wiring lands
  alongside the schema.
- **CLI**: `gemba concepts {bootstrap, list, drift, review, approve,
  reject, log}`. `drift` and `approve` no-op cleanly when no
  production store is wired so the commands are usable today.

## 7. Turn retrospective

After a bead lands (merged, closed), the retrospective compares
**declared** to **actual** and updates priors. It is the single most
important feedback loop in the system — without it, every other layer
operates on guesses that never get graded.

### 7.1 What it grades

For the bead just merged:

| Declared | Actual | Action on mismatch |
|----------|--------|--------------------|
| `targets[]` | files touched in the merge commit | Update bead's `targets` to actual; flag bead's *creator's* extraction prompt for review if drift is large |
| `concepts[]` | concepts inferred from the diff and the symbols changed (via source analysis) | Same |
| Estimated affinity score for the assigned session | Cycle time, rework events, merge conflicts during integration | Append to scorer-grading dataset |

### 7.2 What it produces

- Updated bead row with corrected `targets` / `concepts` (the *truth*
  for future analysis).
- Incremented contribution to the assigned session's profile, using
  *actual* values not declared ones.
- An entry in a `scorer_grades` table joining (predicted affinity,
  conflict graph at dispatch time, observed outcome).

### 7.3 Frequency / latency

Retrospectives run on bead close, asynchronously, off the dispatch
hot path. They should complete within minutes; a backlog is fine but
must not block dispatch.

### 7.4 Human review

The full retrospective stream is a queryable view ("show me beads
where actual targets diverged > 50% from declared"). Used to spot
extraction prompt bugs, missing concepts in the vocabulary, or beads
that were under-scoped to begin with.

### 7.5 Recommendation calibration

The retrospective grades the *score* against outcomes (§7.1). Layer
5 selection adds a second feedback loop: grade the *recommendation*
against operator overrides.

Every coach-mode pick records `(recommended_top_bead, picked_bead,
score_delta, justification, operator_reason)`. When the operator
takes the top recommendation, the calibration row is degenerate —
the planner agreed with itself. When the operator picks something
else, the row carries real signal: the score thought X was best,
the operator picked Y, and the operator may have entered a one-
line `--reason` explaining why.

Aggregate signals the calibration loop watches for:

- **Systematic intent miss.** Operator consistently picks beads
  outside the planner's top-3 when intent is set. Suggests the
  intent demotion factor is too lenient (the planner is letting
  out-of-intent beads slip through).
- **Systematic leverage miss.** Operator picks high-blocks-weight
  beads when the planner ranked them lower. Suggests bumping the
  leverage weight in §4 Layer 5.2's score mix.
- **Systematic runway over-trust.** Operator overrides the runway
  warning frequently and the bead lands fine. Suggests the runway
  estimator is too pessimistic.
- **Systematic affinity drift.** Operator picks beads with low
  affinity but a particular concept-tag pattern that the score
  doesn't capture. Suggests adding a vocabulary entry or
  rebalancing the session-vs-agent profile mix.

These signals are surfaced as suggestions in the same operator-
review queue the vocabulary governance uses (§6 / gm-s47n.7.3) —
nothing auto-applies. The operator approves a re-tune
("`--leverage-weight 0.4`") and the planner reads it from rig
settings on the next loop.

### 7.6 Bead-size calibration

The `estimated_size` heuristic in §4 Layer 0 starts as
description-length × DoD-line-count. The retrospective grades it
by comparing predicted size against actual time-to-close on the
session that took the bead. Drift > 2x in either direction
contributes a delta to the estimator's bucket boundaries.

Calibration is per-rig — different codebases have different
description norms — and per-author when enough signal exists. A
rig that systematically under-describes leaves the size heuristic
shifted; the calibration loop captures that without per-rig
config edits.

## 8. Source analysis scheduling — a first-order planner concern

The Layer 2 source analysis interface (§4 Layer 2) is only useful if its
index is fresh. A stale index produces silently wrong dependent sets,
which produces silently missed semantic conflicts (§5.3), which
produces parallel-dispatched beads that turn out to collide. The
planner is the **only component in the system that knows when a scan
is worth running** — it sees merge waves, parallel completions, and
overall fleet state. So scan scheduling is owned by the planner, not
left to the source analysis tool's own watcher.

### 8.1 Scan triggers

The planner considers a scan when **any** of the following fire,
debounced against §8.3:

- **Post-merge wave**: ≥ N beads merged within a sliding window
  (default N=5, window=15 min). After a wave, the cumulative diff is
  large and the index is now systematically stale across many areas
  semantic-conflict checks may need to look at next.
- **Parallel-completion barrier**: the last bead in a parallel-safe
  batch (§5.1) just finished. The whole batch's diffs are now
  integrated; the index reflects none of them. Re-scan before
  computing the *next* batch's conflict graph.
- **Wall-clock floor**: ≥ T hours since the last successful scan and
  any beads have merged in that time (default T=4h). Stops the index
  from drifting indefinitely on a slow day.
- **Drift signal from source analysis itself**: the Layer 2 capability
  reports its own staleness (last-indexed commit far behind HEAD,
  symbol counts looking off). Treat as a high-priority trigger.
- **Pre-dispatch demand**: the planner is about to compute a conflict
  graph and the index is stale **and** any candidate bead has
  semantic conflicts in its concept area in past retrospectives.
  Synchronous: block dispatch on the scan in this case.

### 8.2 Scan as a planner-managed activity

A scan is a job the planner schedules just like a dispatch decision:

- It has an **operational target** (the repo or repos being indexed)
  and so participates in the workspace-conflict graph (§5.1) — a scan
  on `repo X` should not run while a session is mid-bead in `repo X`'s
  worktree where uncommitted state would skew the index.
- It has a **declared duration estimate** (from the last N runs of
  the same tool on this repo) so the planner can decide whether to
  block dispatch (synchronous) or background (async).
- It is **logged** in the same activity stream as bead dispatch and
  retrospectives, so the operator can see "the planner ran a
  gitnexus rescan at 14:02 because 7 beads merged in the last
  10 minutes."

### 8.3 Debouncing and rate limits

Scans are not free; left unchecked, the triggers above can stampede.

- **Cooldown**: no more than one scan per repo per `min_scan_interval`
  (default 10 min), regardless of triggers.
- **Coalescing**: triggers that fire during an in-progress scan are
  noted and treated as "scan immediately after this one finishes" if
  the firing reason is *new* (different from what the running scan
  was kicked off by). Identical triggers are dropped.
- **Async by default**: most scans run in the background; only
  pre-dispatch demand (§8.1 last bullet) blocks.
- **Operator override**: `gemba scan --now` for forced manual scans;
  `gemba scan --pause <duration>` to suppress all auto-triggers
  during e.g. a known-noisy refactor.

### 8.4 Tool abstraction

Scan scheduling lives **above** the source analysis interface and
issues `Rescan(repo)` against it. Implementations:

- **GitNexus**: shells out to `gitnexus analyze` (with `--embeddings`
  if the prior index had them — see CLAUDE.md).
- **Noop**: succeeds silently. Allows the planner loop to run
  uniformly even when no real source analysis is configured.

### 8.5 Closing the loop

Each scan run records: trigger reason, target repo, start/end times,
result (success / failure / skipped-because-cooldown), and
post-scan freshness telemetry. The retrospective (§7) joins this
stream against subsequently-discovered semantic conflicts: when a
conflict turns out to have been missed at dispatch time, was the
index stale? If yes, was the trigger that *should* have fired
suppressed by debouncing or a missing rule? This is how the
scheduling rules themselves get tuned.

## 9. Caveats and known fragilities

Worth saying out loud — anyone working on this should know the
failure modes before they build them in.

- **Scoring is fundamentally fuzzy.** Numeric output makes it look
  precise. Always pair scores with explanations; never let a UI show
  the score without the breakdown. Operators stop trusting the system
  the first time it confidently dispatches wrong.
- **Auto-dispatch is high blast radius.** A bad weight tune can push
  a fleet of agents into a corner of the codebase for hours. Hard
  rate-limit auto-dispatch (e.g. ≤1 bead / session / 5 min) and keep
  the kill-switch one command away.
- **Cold start is a real cost the model can't see.** A session with
  `context_pct = 0.05` looks "fresh" and "ready for anything," but
  giving it a concept-mismatched bead is exactly the cold-start cost
  we're trying to avoid in primed sessions. Affinity must score new
  sessions neither high nor low — *neutral*. The planner should
  prefer warm matches and only spin up new sessions when nothing in
  the fleet is primed for the work.
- **Retrospective lag means the model is always slightly stale.**
  The session profile reflects yesterday's truth, not today's. Fine —
  the alternative (waiting for retro before updating) blocks dispatch
  on integration. Document the staleness; don't try to engineer
  around it.
- **Source analysis indexes go stale.** Detect this on every call to
  the source analysis interface; degrade to "skipped semantic check"
  with a warning rather than silently returning stale dependents.
- **Beads aren't the only unit of work.** Long-running design and
  exploration sessions don't fit neatly into the bead model and won't
  appear in the dispatch queue. The planner correctly ignores them
  for auto-dispatch but the session-profile capture should still
  happen (a coach session that spent 3h on auth design should make
  that session a strong candidate for auth implementation work
  afterward).
- **Fairness boost is a band-aid for a deeper problem.** If hard
  work consistently scores low on affinity, the work is mis-tagged or
  mis-scoped. Treat sustained fairness-boost reliance as a signal to
  fix the upstream beads, not as a permanent feature.

## 10. Sequencing

Build bottom-up. Each step is shippable on its own and useful even if
the next step never lands.

| Step | Builds | Value at this stop |
|------|--------|--------------------|
| 1 | Layer 0: `targets[]` and `concepts[]` on beads, with LLM bootstrap | Better search, filter, and reporting on existing beads. Zero behavior change. |
| 2 | Layer 3.1: `gemba conflicts` (target overlap only, no semantic check yet) + a SPA panel | Operators can see and avoid conflicts manually. Highest immediate ROI. |
| 3 | Layer 1: session profile capture (passive write to dolt; no reader yet) | Data accumulates so later steps work on real history rather than synthetic data. |
| 4 | Layer 2: source analysis interface + GitNexus binding + noop | Unlocks semantic conflict detection in step 6 without coupling gemba to a tool. |
| 5 | §8 source analysis scheduling (manual-trigger + wall-clock + post-merge wave); cooldown + activity-stream logging | Index freshness becomes a planner concern instead of a side effect. Required for step 6 to be trustworthy. |
| 6 | Layer 3.1 upgrade: semantic conflict via source analysis; workspace-conflict edge against live operational contexts | Catches non-overlapping but semantically-conflicting beads, and beads routing to an in-use worktree. |
| 7 | Layer 3.2: `gemba affinity` (with workspace_match sub-score) + coach-mode SPA view (agent context strip + dispatch grid) | Human PM gets scores and sees full operational context per session. Still in the loop. |
| 8 | Layer 4: session-health surface (read-only) | Operators see drift and pressure. Manual recycle decisions. |
| 9 | §7 turn retrospective (target/concept actual-vs-declared only) + §8.5 scan-trigger grading | Bead data starts self-correcting. Session profile uses ground truth. Scan rules tune from missed-conflict outcomes. |
| 10 | Layer 5.2: auto-dispatch mode, opt-in, with kill switch; auto-recycle + auto-scan-trigger integration | Hands-off dispatch where the operator wants it. |
| 11 | Retrospective expansion: scorer grading, weight-tuning *recommendations* (not auto-apply) | The system learns from outcomes and surfaces suggestions. Operator approves changes. |

The first three steps deliver ~70% of the operator value at ~30% of
the complexity. If the project stops at step 3, gemba is still
meaningfully better at coordinating work than it is today. Steps 4–10
are how it becomes the "central feature" — but each only earns its
keep on top of the data the earlier steps collect.

### Work Planning 2.0 — selection layer + signals (gm-wp2 epic)

Steps 1-11 above ship the *scoring substrate*: a planner that
correctly answers "which bead is cheapest to do well?" The §3.5
analysis identified that the operator-observed bias when
recommending work draws on a second class of signals — moment-
dependent, session-specific, operator-pinned — that the scoring
layer deliberately doesn't model.

WP2 ships those signals + the Layer 5 selection that composes them.
Each step is independently usable, layered on the same bottom-up
order:

| Step | Builds | Value at this stop |
|------|--------|--------------------|
| 12 | Layer 0 grow: `dispatch_status` enum + `estimated_size` heuristic + bd extras schema | Soft-blocks finally have a vocabulary; the scorer no longer recommends "awaiting-design" beads. Bead size becomes queryable. |
| 13 | Layer 1.2: persistent agent profile (write-through alongside session profile) | A fresh session inherits its agent's warm context. Mike4's first bead post-handoff isn't cold-start. |
| 14 | Layer 1.3: session intent + `gemba session focus` CLI | Operator can pin "finish gm-s47n this turn" and the planner respects it. Selection's first new gate. |
| 15 | Layer 1.4: runway estimator (read-only, advisory) + `gemba session status` | Operators see "this session has small/medium/large runway left." Used by step 18's selection gate. |
| 16 | Layer 3.3: `Leverage(bead, deps)` scorer (blocks-weight) + `epic-affinity` sub-score in Affinity | "Pick what unblocks more" + "finish what you started" land as scoreable signals. |
| 17 | OperationalContext claim_index + owner-claim cross-check | Multi-agent fleets stop racing the same bead. Required for step 18's hard gate. |
| 18 | Layer 5: Selection layer composing Score + Justification + the new gates (dispatch_status, owner-claim, runway, intent, fairness) | The §3.5 split materializes. Coach mode and auto-dispatch share one selection engine; both render `Justification` verbatim. |
| 19 | §7.5 recommendation calibration loop; §7.6 bead-size calibration | Operator overrides become tuning signal. The planner learns *what to recommend*, not just *how to score*. |
| 20 | `gemba session focus` SPA surface + Justification rendering in dispatch grid | Coach-mode UX catches up to the new selection signals. |

Steps 12-15 are *data*: they pay for themselves once the operator
or extractor populates the new fields, even before the selection
layer reads them. Steps 16-18 are *compute*: they need the data
from 12-15 plus the existing scoring substrate. Step 19 is the
*feedback loop*: it grades the system from steps 18 down. Step 20
is the *surface*.

## 11. Open questions

Things deliberately left undecided in this document. These should be
resolved as the corresponding step approaches, with input from
operators using the system, not in advance from a designer's chair.

- **Concept vocabulary scope**: per-rig, per-town, or both? Per-rig
  is more flexible; cross-rig comparisons (e.g. "which agent in the
  whole town is most primed for auth?") need a shared vocab.
- **Multi-bead dispatch**: should the planner ever sling a small
  cluster of mutually-conflict-free beads to one session as a batch,
  to amortize cold-start? Tempting, but multiplies blast radius.
- **Session profile decay across handoffs**: when a session recycles
  via `gt handoff`, does the new session inherit a *fraction* of the
  old profile (cheaper restart for related work) or start fresh
  (cleaner accounting)? Probably inherit, with a handoff-decay
  multiplier ~0.5.
- **What constitutes a "completed bead event"** for profile-update
  purposes — claim, PR open, PR merge, or close? Each has different
  latency-vs-accuracy tradeoffs.
- **Granularity of `targets`**: file-level, directory-level, or
  symbol-level? File-level is the obvious starting point; symbol-level
  needs source analysis for every bead and may be premature.
- **Worktree path as first-class field vs `provider_metadata`
  contract**: §4 Layer 1 calls this out — promote `Workspace.worktree_path`
  to a typed field, or define a stable `provider_metadata["worktree_path"]`
  key. Pick one before the conflict graph starts depending on it.
- **Scan trigger thresholds**: defaults in §8.1 (N=5, window=15min,
  T=4h, cooldown=10min) are guesses. Tune from §8.5 retrospective
  data once the scheduler has been running for a sprint.
- **Scan during in-progress writes**: §8.2 says "don't scan a repo
  while a session is mid-bead in its worktree." But what about
  during a long-running bead — wait indefinitely, or scan against
  HEAD knowing the index will miss the in-flight diff? Probably the
  latter, with a flag noting "scanned with N in-flight beads."
- **Concept-axis cross-rig portability**: a session in rig A spawned
  via `gt worktree` into rig B has agent identity from A but
  operational context in B. Whose session profile owns it, and does
  the operator see one card or two?
