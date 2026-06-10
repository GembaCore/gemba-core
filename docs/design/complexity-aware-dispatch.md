---
title: "Complexity-aware dispatch"
decision: gm-8c26
---

# Complexity-aware dispatch

> Status: **draft / for review** — 2026-05-03
> Owner: gemba mayor
> Scope: extend two-axis work planning so Gemba can estimate work
> complexity and route each bead to an agent/model profile with enough
> capability, without over-spending premium models on routine work.

## 1. Decision

Introduce **Work Complexity** as a first-class dispatch signal. Gemba
will estimate every dispatchable work item along two primary dimensions:

- **Depth**: how hard the core reasoning is inside the bead's most
  difficult technical path.
- **Span**: how much context must be held together across files,
  modules, surfaces, repositories, tests, docs, and product decisions.

Those estimates produce a **complexity band**. Agent profiles declare
their **capability envelope**: supported bands, model/provider, context
window, tool access, cost tier, autonomy level, and known strengths.

The dispatcher then adds a **complexity-fit gate** between the existing
conflict/parallel-safety analysis and the existing affinity/selection
ranking:

1. Build the ready set.
2. Compute target-axis conflicts and parallel-safe batches.
3. Estimate each bead's depth/span/risk.
4. Filter or demote candidate agent profiles that are below the bead's
   required capability.
5. Run the existing selection score inside the remaining feasible
   agent/session pairs.

This preserves the current two-axis design:

- The **target axis** still answers "what can safely run together?"
- The **concept axis** still answers "who is warm on this work?"
- The new **complexity-fit step** answers "who is capable enough to
  attempt it, and what capability tier is cost-rational?"

## 2. Why this exists

Gemba is moving from "one strong agent type handles everything" toward
fleets with mixed capability and cost:

- premium frontier models for hard refactors, architecture-sensitive
  features, cross-cutting changes, and ambiguous debugging;
- solid mid-tier models for contained UI work, routine backend changes,
  test authoring, documentation, and local product polish;
- local/open-source or low-cost models for mechanical edits, simple
  merge conflicts, formatting, fixture updates, and low-risk follow-ups.

Without explicit capability matching, a cheap agent may take work it
cannot finish, or a premium agent may burn expensive tokens on chores.
Both are dispatch failures. The first creates rework and escalations;
the second destroys the economics of autonomous dispatch.

Human teams already do this informally. Sprint planning often estimates
story size, then pairs the work with developers who have the right
skill, context, and seniority. Gemba needs the same PM instinct in a
machine-readable form.

## 3. External analogies

This design borrows ideas, not implementation, from several adjacent
models:

| Model | What Gemba borrows | Limit |
|---|---|---|
| Agile story points | Estimate relative effort/uncertainty before assignment. | Story points collapse many causes into one number; Gemba keeps depth and span separate. |
| Seniority-based assignment | Match work risk and ambiguity to developer capability. | Agent/model capability is more explicit and more volatile than human seniority. |
| Routing workflows in agent systems | Classify an input and route it to the specialized handler/model. | Most router examples choose one branch by input type; Gemba routes inside a live, constrained dispatch graph. |
| Dynamic model selection | Use stronger models only when task complexity justifies it. | Provider-level routing does not know source conflicts, session warmth, or bead dependencies. |
| Cynefin-style work classification | Separate obvious/routine, complicated/expert, and complex/discovery work. | Gemba still needs deterministic dispatch; complex work may require escalation or decomposition. |
| SWE-bench-style software tasks | Treat issue resolution as variable-difficulty engineering work with tests and codebase context. | Benchmarks grade final patches; Gemba must decide before the patch exists. |

Useful references:

- Anthropic's agent design guidance describes routing as a workflow
  that classifies an input and directs it to the appropriate follow-on
  process or model: <https://www.anthropic.com/engineering/building-effective-agents>
- LangChain agents document dynamic model selection as middleware that
  swaps models based on message/task characteristics:
  <https://docs.langchain.com/oss/python/langchain/agents>
- GitHub's SWE-bench paper frames issue resolution as real repository
  maintenance, with tasks drawn from GitHub issues and pull requests:
  <https://arxiv.org/abs/2310.06770>
- The Cynefin framework is a useful language for separating obvious,
  complicated, complex, and chaotic work:
  <https://en.wikipedia.org/wiki/Cynefin_framework>

## 4. Vocabulary

| Term | Definition |
|---|---|
| **Depth** | Single-path technical difficulty. Examples: algorithmic subtlety, concurrency, data correctness, API design, performance, type-system complexity, migration risk. |
| **Span** | Context breadth. Examples: number of files, modules, domains, routes, packages, repositories, tests, docs, and product decisions that must stay coherent. |
| **Risk** | Blast radius if wrong. Security, auth, persistence, migrations, public APIs, billing, data loss, and release-critical code raise risk. |
| **Ambiguity** | How much interpretation remains in the bead. Clear DoD lowers ambiguity; vague product language, unknown repro steps, or missing acceptance criteria raise it. |
| **Verification burden** | How much proof is required. A simple unit test is low; cross-browser E2E, generated artifacts, screenshots, migrations, or production-like demos are high. |
| **Complexity estimate** | The structured estimate `{depth, span, risk, ambiguity, verification}` plus derived band. |
| **Capability envelope** | What an agent/model profile can responsibly attempt: max band, max span, depth strengths, context budget, tool permissions, cost tier, and escalation policy. |
| **Complexity fit** | A per-(bead, agent profile) result: pass, demote, escalate, or require decomposition. |
| **Under-qualified dispatch** | Dispatching a bead to a profile below required capability. This should be blocked in auto-dispatch and warned in coach mode. |
| **Over-qualified dispatch** | Dispatching routine work to a premium profile when a cheaper profile fits. This should be allowed but cost-demoted. |

## 5. Complexity model

Depth and span are scored independently on a 0-4 scale. Keeping them
separate is important: a one-file concurrency bug can be deep but
narrow; a broad copy update can be wide but shallow.

### 5.1 Depth

| Score | Label | Examples |
|---|---|---|
| 0 | Mechanical | Rename, formatting, comment sync, fixture literal change. |
| 1 | Routine | Add a small field, update a simple UI control, write a straightforward unit test. |
| 2 | Standard engineering | Moderate feature in known area, contained backend endpoint, predictable refactor. |
| 3 | Expert | Cross-cutting refactor, performance/concurrency issue, tricky E2E failure, data model change. |
| 4 | Frontier / architectural | Multi-system architecture, ambiguous deep bug, migration with rollback concerns, security-critical design. |

### 5.2 Span

| Score | Label | Examples |
|---|---|---|
| 0 | Tiny | One file or one config entry. |
| 1 | Local | A small component or package plus tests. |
| 2 | Feature slice | UI + API + tests, or several files in one subsystem. |
| 3 | Cross-subsystem | Multiple packages/surfaces, docs, tests, and dispatch/runtime wiring. |
| 4 | Whole product / multi-repo | Multiple repos, orchestration/runtime changes, broad documentation and demo impact. |

### 5.3 Modifiers

Risk, ambiguity, and verification burden do not replace depth/span.
They modify routing:

- **Risk** raises the minimum capability band even when depth/span are
  modest. A one-line auth change may require a strong model.
- **Ambiguity** favors stronger models or a coaching/decomposition
  step. High ambiguity with high span should not auto-dispatch.
- **Verification burden** raises required tool capability and runway.
  Work that must run Playwright, inspect screenshots, or generate
  artifacts needs a profile with those tools and enough context/time.

### 5.4 Derived bands

The first implementation should use deterministic rules:

| Band | Rule of thumb | Default route |
|---|---|---|
| `trivial` | depth <= 1 and span <= 1 and low risk | low-cost/local profile allowed |
| `routine` | max(depth, span) <= 2 and low/medium risk | mid-tier profile preferred |
| `skilled` | depth == 3 or span == 3, or high verification | strong profile preferred |
| `expert` | depth == 4 or span == 4, or high risk + ambiguity | premium profile required |
| `decompose` | expert band plus unclear DoD, huge span, or missing acceptance criteria | do not dispatch directly; create refinement/decomposition work |

Bands are advisory in coach mode and enforceable in auto-dispatch.

## 6. Estimation signals

The estimator should start cheap and improve through retrospectives.

### 6.1 Static bead signals

- Title/body length and density of technical nouns.
- Definition of Done count and specificity.
- Work item kind: bug, feature, task, decision, epic, chore.
- Labels: `risk:*`, `surface:*`, `layer:*`, `needs:e2e`,
  `needs:migration`, `needs:design`, `security`, `performance`.
- Targets and concepts already extracted by Layer 0 enrichment.
- Dependency count: blocks many beads, is blocked by many beads, or
  sits under a milestone/epic.
- Whether the item modifies generated artifacts, public API, schema,
  auth, persistence, orchestration, bridge hooks, or runtime drivers.

### 6.2 Source-analysis signals

When GitNexus or another source analysis tool is fresh:

- dependency neighborhood size around target files/symbols;
- fan-in/fan-out of changed modules;
- public API/exported symbol involvement;
- test coverage and nearest owning test suites;
- prior churn and bug density for target files;
- semantic conflict history from retrospectives.

These are span and risk signals more than depth signals, but they
provide the grounding that text-only estimates lack.

### 6.3 Session outcome signals

Retrospectives should calibrate the estimator with:

- actual time to close;
- number of files touched;
- number of test/build failures before success;
- escalation count;
- operator intervention count;
- whether a stronger agent had to rescue or redo the work;
- whether the original complexity band was over/under.

## 7. Agent profile capability envelope

Agent profiles should grow beyond "persona + model" into a routing
contract. The shape can live beside the existing native agent registry
and pool config.

Example:

```toml
[agent_profile.engineer-premium]
provider = "codex"
model = "gpt-5.4"
cost_tier = "premium"
max_complexity_band = "expert"
max_depth = 4
max_span = 4
context_window_class = "large"
autonomy = "high"
tools = ["shell", "git", "playwright", "source-analysis", "beads"]
strengths = ["architecture", "refactor", "debugging", "e2e"]
auto_dispatch = true

[agent_profile.engineer-standard]
provider = "claude"
model = "sonnet"
cost_tier = "standard"
max_complexity_band = "skilled"
max_depth = 3
max_span = 3
context_window_class = "medium"
autonomy = "medium"
tools = ["shell", "git", "beads"]
strengths = ["ui", "tests", "docs", "contained-backend"]
auto_dispatch = true

[agent_profile.engineer-local]
provider = "local"
model = "qwen-coder"
cost_tier = "low"
max_complexity_band = "routine"
max_depth = 2
max_span = 1
context_window_class = "small"
autonomy = "bounded"
tools = ["shell", "git"]
strengths = ["mechanical-edits", "merge-conflicts", "fixtures"]
auto_dispatch = true
requires_human_review = true
```

Capability is not only model quality. It is also context size,
available tools, configured MCP endpoints, write permissions, network
access, verification ability, and the reliability history of that
profile in this repository.

## 8. Pipeline fit

The existing dispatch design already separates deterministic selection
from LLM planning. Complexity-aware dispatch belongs in the deterministic
selection path.

### 8.1 Placement

```
ready set
  -> enrichment: targets, concepts, dispatch_status, size
  -> conflict graph: target/workspace/semantic conflicts
  -> complexity estimate: depth/span/risk/ambiguity/verification
  -> capability fit: bead x agent profile
  -> selection: affinity + leverage + runway + intent + fairness
  -> claim/dispatch through adaptor claim model
```

Complexity estimation can be cached per bead and recomputed when the
bead body, labels, targets, concepts, dependencies, or linked design doc
change. Capability fit is recomputed each dispatch tick because live
agent availability and profile health change.

### 8.2 Fit results

For each `(bead, profile)`:

| Result | Meaning | Auto-dispatch behavior | Coach behavior |
|---|---|---|---|
| `fit` | Profile meets capability and cost is reasonable. | Eligible. | Normal score. |
| `overqualified` | Profile exceeds need and cheaper profiles exist. | Cost-demote unless no cheaper fit has capacity. | Show cost warning. |
| `underqualified` | Profile below required band. | Exclude. | Allow manual override with warning. |
| `missing_tools` | Model may be capable but lacks required tool/MCP/runtime. | Exclude. | Show missing tool list. |
| `needs_decomposition` | Work is too broad/ambiguous for direct dispatch. | Exclude and file/offer refinement bead. | Open refine/coaching flow. |

### 8.3 Scoring interaction

Complexity fit should not be mixed directly into affinity. Affinity
answers "who is warm?" Capability fit answers "who can responsibly do
it?" Keep them separate so the explanation stays legible.

Recommended composition:

1. Hard filters: dispatch_status, owner claim, target conflict, missing
   tools, severe underqualification.
2. Complexity demotions: mild underqualification in coach mode,
   overqualification cost penalty, uncertainty penalty.
3. Existing score: affinity, leverage, epic-affinity, recency,
   headroom, runway, fairness.

This prevents the worst failure: a cheap local model with high concept
affinity taking an expert-band refactor because it happened to touch the
same files last turn.

## 9. UI and operator surfaces

### 9.1 Board and RHP detail

Cards should show a compact complexity pill:

- `Routine · D1/S2`
- `Skilled · D3/S2`
- `Expert · D4/S3`
- `Decompose`

The RHP work item detail should show:

- depth/span/risk/ambiguity/verification values;
- estimator explanation and confidence;
- required capability band;
- profiles that fit, profiles excluded, and why;
- calibration history if the bead or similar concepts have prior data.

### 9.2 Coach and auto-dispatch status

The dispatch grid should add capability-fit badges per cell:

- green: fit;
- amber: overqualified/cost-demoted;
- red: underqualified or missing tools;
- gray: needs decomposition.

The Status RHP should include rollups:

- ready work by complexity band;
- currently dispatched work by model/cost tier;
- premium model usage against expert/skilled work;
- underqualified manual overrides this run;
- decomposition recommendations waiting for the operator.

### 9.3 Configuration

Settings should expose:

- profiles and model/provider bindings;
- per-profile maximum band/depth/span;
- cost tier and auto-dispatch eligibility;
- allowed tools/MCP endpoints;
- per-band default routing policy;
- operator override policy.

## 10. Data model additions

### 10.1 Work item extras

Add structured extras:

```json
{
  "complexity": {
    "depth": 3,
    "span": 2,
    "risk": "medium",
    "ambiguity": "low",
    "verification": "high",
    "band": "skilled",
    "confidence": 0.72,
    "source": "heuristic+source-analysis",
    "explanation": [
      "Touches web/src and internal/server",
      "Requires Playwright verification",
      "No schema/auth/persistence risk detected"
    ],
    "updated_at": "2026-05-02T00:00:00Z"
  }
}
```

### 10.2 Agent profile

Add profile config and runtime health:

```json
{
  "id": "engineer-standard",
  "provider": "claude",
  "model": "sonnet",
  "cost_tier": "standard",
  "max_depth": 3,
  "max_span": 3,
  "max_band": "skilled",
  "tools": ["shell", "git", "beads"],
  "strengths": ["ui", "tests", "docs"],
  "observed_success": {
    "routine": 0.93,
    "skilled": 0.78,
    "expert": 0.31
  }
}
```

### 10.3 Dispatch decision audit

Dispatch decision rows should include:

- complexity estimate snapshot;
- chosen profile and model;
- profiles excluded and why;
- cost demotion applied, if any;
- operator override reason, if coach mode overrode the recommendation;
- outcome calibration after completion.

## 11. Implementation sequence

| Step | Builds | Value |
|---|---|---|
| 1 | Add complexity estimate schema in bead extras and Go types. | Data can be inspected and edited without behavior change. |
| 2 | Add heuristic estimator CLI/API: `gemba complexity estimate <id>` and backfill. | Immediate visibility for current beads. |
| 3 | Add agent profile capability envelope to config/registry. | Profiles become routable by more than persona/model name. |
| 4 | Add capability-fit pure function and tests. | Deterministic routing primitive. |
| 5 | Thread fit into coach selection explanations. | Human can validate before auto-dispatch depends on it. |
| 6 | Thread fit into auto-dispatch as hard filters/demotions. | Mixed-cost fleets route safely. |
| 7 | Add RHP/board/status UI pills and settings surface. | Operators can understand and tune the system. |
| 8 | Add retrospective calibration for complexity estimates and profile success by band. | The system learns from observed outcomes. |
| 9 | Add decomposition recommendation flow for `decompose` band. | Huge/ambiguous work gets split before dispatch. |

## 12. Open questions

- Should premium profiles ever auto-dispatch `expert` work without a
  human confirmation, or should expert-band auto-dispatch require a
  per-project policy toggle?
- Should risk labels be operator-authored only, or can source analysis
  raise risk automatically for auth/persistence/schema paths?
- How much should observed success by band affect routing before it
  unfairly starves a new profile of learning opportunities?
- Should "local/open-source" profiles be allowed to write directly, or
  should they default to suggestion-only patches until calibrated?
- Do depth/span estimates belong in bead extras long-term, or should
  they become first-class WorkItem fields once the pattern stabilizes?

## 13. Non-goals

- This does not replace concept affinity or target conflict analysis.
- This does not let an LLM make the hot-path dispatch decision.
- This does not auto-tune profile capability without operator review.
- This does not assume provider/model rankings are stable; profiles are
  local configuration plus observed performance, not global truth.

## 14. Acceptance criteria

- Operators can inspect a bead's complexity estimate and why it was
  assigned that band.
- Agent profiles can declare maximum depth/span/band and required tools.
- Coach mode explains why a profile is fit, overqualified,
  underqualified, missing tools, or blocked by decomposition.
- Auto-dispatch refuses underqualified and missing-tool assignments.
- Dispatch audit records include complexity and capability-fit snapshots.
- Retrospectives can compare estimated complexity to observed outcome.
