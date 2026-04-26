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

## 4. Primitives, in dependency order

The system is built bottom-up. Each layer is independently useful even
if higher layers are never built — important because the early layers
are cheap and the later layers depend on the data the early layers
collect.

### Layer 0 — WorkItem enrichment (data only)

Add two structured fields to every WorkItem:

- `targets[]` — declared path globs the item is expected to touch.
- `concepts[]` — tags from the controlled vocabulary.

For the bd adaptor, store these in the bead's structured-extras map,
not in the body, so they're queryable.

**Bootstrap**: at WorkItem creation, an LLM extracts a first guess from
title + body + any linked spec. Human can override at any time. Both
fields are *advisory* until the turn retrospective (§7) starts grading
them.

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

#### 3.2 `Affinity(bead WorkItem, ctx OperationalContext) (float64, Explanation)`

Takes the joined operational-context struct (§4 Layer 1) so it can see
agent identity, workspace, profile, and health together.

Compute five sub-scores in [0, 1]:

- **Concept overlap**: cosine similarity between `bead.concepts`
  (one-hot) and `ctx.profile.concepts` (decayed weights).
- **File familiarity**: fraction of `bead.targets` that intersect
  `ctx.profile.files` weighted by decay.
- **Workspace match**: 1 if `bead.repository ∈ ctx.workspace.repository`
  AND `bead.branch_convention` matches `ctx.workspace.branch`; 0.5 if
  same repo / different branch (cheap branch switch, expensive only if
  this kind is `worktree`); 0 if different repo (cold-start cost on
  workspace switch is real). For multi-repo beads, take the max over
  declared repos.
- **Recency**: 1 if the session's most recent bead shared a concept
  with this one; decays linearly to 0 over ~10 beads.
- **Headroom**: 1 if `ctx.health.context_pct < 0.5`; decays linearly to
  0 at 0.85; hard 0 above 0.9.

Combined score: weighted sum (default weights 0.30 / 0.20 / 0.20 /
0.15 / 0.15; tunable). Explanation is the per-sub-score breakdown —
never present the scalar without the breakdown.

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

### Layer 5 — The planner (UX)

Two modes share the same scoring engine. The mode flag determines who
makes the final dispatch decision.

#### 5.1 Coach mode (interactive PM)

A SPA view with two halves:

- **Agent context strip** — one card per live session showing the
  full operational context: agent name + role, repo, branch,
  worktree path, isolation kind (with a worktree icon for the
  preferred case), session status, last heartbeat, top concepts in
  the profile, context pressure, concept drift, time-on-task. This
  is the operator's at-a-glance view of *who is loaded with what*
  and *where they're working*.
- **Dispatch grid** — rows are ready beads, columns are agent cards
  from the strip. Each cell shows `(affinity_score, explanation)`.
  Conflict edges between beads are rendered as grouped highlights —
  picking one bead dims the cells of its conflict-adjacent siblings,
  *including* workspace-conflict siblings against currently-active
  sessions in the strip.

The coach (human) picks. The system records the pick along with the
scores at decision time, so the retrospective can grade the model.

This mode is a faithful instrument of what a senior PM does in a live
session today. It does not change the workflow, only surfaces the data
behind it.

#### 5.2 Auto-dispatch mode

A daemon loop. When a session becomes idle, the planner:

1. Reads the ready set, the session's profile, and session health.
2. If the session is over a hard recycle threshold (§4.4), trigger
   `gt handoff`; the next iteration of the loop will see a fresh
   session and re-decide.
3. Compute the conflict graph over the ready set.
4. Find the highest-affinity bead for this session that is **not**
   conflict-adjacent to any bead currently being worked by another
   session.
5. Apply a **fairness boost**: each ready bead gains affinity
   proportional to its age in the ready queue. Stops the planner
   from starving hard work in favor of cheap concept-matched work.
6. Sling the chosen bead.

Auto-dispatch is **opt-in per rig** with a kill-switch in rig settings.
A bad scorer on a fast loop can do real damage; the kill-switch is
non-negotiable.

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
