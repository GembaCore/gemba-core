---
title: "Workflow \u2014 Beads molecules as Gemba's first-class motion primitive"
decision: none
---

# Workflow — Beads molecules as Gemba's first-class motion primitive

> **Status:** Design proposal. Locks the vocabulary, the operator-facing UI surfaces, and the integration shape; defers the slice-by-slice bead breakdown until ratification.
>
> **Resolves DD:** TBD — register a new DD on ratification.
>
> **Audience:** SPA + backend implementers, operator early-adopters who want repeatable workflow on top of the work-item plane.

## TL;DR

Beads already ships a chemistry-themed workflow primitive — **molecules** — and Gemba surfaces them only as an issue type today. This proposal promotes molecules to a top-level Gemba concept under the name **Workflow** and wires three integration points:

1. **Manual attach.** From any bead's drawer, the operator can pour a workflow molecule (or wisp it for ephemeral runs).
2. **Session auto-attach.** When the dispatcher starts a session against a bead, the configured workflow for the bead's type / epic / project pours automatically and steers the agent through the lifecycle.
3. **Epic-gate enforcement.** At epic *start* a planning workflow pours; at epic *end* a review/validation workflow pours. Gemba refuses to mark the epic done until every gate-step molecule resolves.

The user-facing label is **Workflow**. The underlying entity is the beads molecule. Operators don't need to learn the chemistry vocabulary unless they author their own formulas; for them, "Workflow" is the noun and "Pour", "Run", "Resume" are the verbs.

## 1. Background — what beads molecules are

Beads's `bd mol` subsystem is a DAG-of-work-items templating engine. Four nouns and four phase-changes carry the model:

| Noun | What it is |
|---|---|
| **Formula** | Authored `.formula.toml` file. Variables, steps with `needs:` deps, and composition rules (`extends`, `compose`). The source layer. |
| **Proto** (a.k.a. **protomolecule**) | A "template epic" produced by `bd cook`-ing a formula. Carries the `template` label; not yet instantiated. |
| **Mol** (poured molecule) | Persistent instantiation of a proto. Lives in `.beads/`, syncs via git. Variables are substituted at pour time. |
| **Wisp** | Ephemeral instantiation. Stored locally, **not** synced via git; auto-cleans. |

The chemistry metaphor extends to the verbs:

| Verb | What happens |
|---|---|
| **Cook** | Formula → proto. Expands macros, applies aspects, materializes the DAG. |
| **Pour** | Proto → mol (solid → liquid). Persistent run. |
| **Wisp** | Proto → wisp (solid → vapor). Ephemeral run. |
| **Bond** | Combine proto+proto, proto+mol, or mol+mol into a compound. |
| **Squash** | Condense a finished molecule's history into a digest entry. |
| **Burn** | Discard a wisp before completion. |
| **Distill** | Extract a formula from an existing ad-hoc epic — reverse direction. |

### The DAG model

A formula is a typed DAG of steps. Each step has `id`, `title`, `description`, and an optional `needs: [other-step-id]` array. Steps with no unresolved `needs` are **ready**; steps with all needs satisfied dispatch in parallel. The `--parallel` flag on `bd mol show` highlights parallelizable groups.

```toml
formula = "shiny"
type = "workflow"
version = 1

[[steps]]
id = "design"
title = "Design {{feature}}"

[[steps]]
id = "implement"
needs = ["design"]
title = "Implement {{feature}}"

[[steps]]
id = "review"
needs = ["implement"]
title = "Review implementation"

[[steps]]
id = "test"
needs = ["review"]
title = "Test {{feature}}"

[[steps]]
id = "submit"
needs = ["test"]
title = "Submit for merge"
```

Variables (`{{feature}}`, `{{assignee}}`, etc.) are required, defaulted, or sourced. Sources today include `hook_bead` (the bead the agent is hooked to), rig config, and operator-supplied `--var` flags.

### Composition rules

Formulas compose via three orthogonal mechanisms:

1. **`extends`** — single-inheritance. `shiny-enterprise` extends `shiny`.
2. **`compose.aspects`** — AOP-style cross-cutting advice. `security-audit` adds `before`/`after` advice around steps matching pointcuts:
   ```toml
   [[advice]]
   target = "implement"
   [advice.around]
   [[advice.around.before]]
   id = "{step.id}-security-prescan"
   title = "Security prescan for {step.id}"
   [[advice.around.after]]
   id = "{step.id}-security-postscan"
   ```
3. **`compose.expand`** — replace one step with a multi-step expansion. `tdd-cycle` replaces `implement` with red-green-refactor. `rule-of-five` replaces it with five-perspective elaboration.

A fourth shape — **convoy** — runs N parallel "legs" with different perspectives and synthesizes one verdict. The bundled `code-review` convoy ships ten review legs (correctness, performance, security, elegance, resilience, style, smells, wiring, commit-discipline, test-quality) and presets that subset them (`gate`, `full`, `custom`).

### Lifecycle: Rig → Cook → Run

```
formulas/*.formula.toml
       │ bd cook
       ▼
   protos (template epics with `template` label)
       │ bd mol pour | bd mol wisp
       ▼
   mol-* / wisp-* (persistent or ephemeral runs)
       │ steps execute as agents work them; bd mol progress shows %
       ▼
   bd mol squash → digest entry
```

Search paths for formulas:
1. `<project-dir>/formulas/` (active project)
2. `~/.beads/formulas/` (operator)
3. `$GT_ROOT/.beads/formulas/` (orchestrator)

Project-local formulas override operator-level which override orchestrator-level. This three-tier composition is exactly what Gemba's "Workflow" UX should expose: project-defined workflows live with the project's source, operator-defined workflows are personal templates, and a shipped library covers conventional patterns.

## 2. How the industry does workflow today

To position Gemba's Workflow surface usefully it helps to map the beads vocabulary against the analogues operators already know:

| Beads | Closest industry analog | What's distinctive about beads |
|---|---|---|
| Formula | GitHub Actions workflow / Tekton Pipeline definition / Argo Workflow template | Beads operates on **work items**, not commands. The "step" is a bead an agent works, not a CI job. |
| Proto | "Template repository" / GitHub reusable workflow / Backstage software template | Protos are first-class issues with the `template` label — visible alongside real work, not tucked in `.github/`. |
| Pour | A workflow run that creates issues (rare in CI) | Most CI runs leave only logs; a poured mol creates real, claimable beads. |
| Wisp | A non-archived job / ephemeral cron | Wisps are inspected through the same surfaces as persistent work, then discarded. |
| Bond | Composite actions / pipeline `include` | Bonding can mix template+instance, not just template+template. |
| Aspect | AspectJ pointcuts / Tekton `Pipeline.spec.finally` | Most CI doesn't have AOP; aspects are unusually expressive. |
| Convoy | Matrix builds, multi-perspective code review tools (Sourcery, CodeScene) | Convoys synthesize specialized verdicts into one prioritized output. |
| Distill | Refactor: extract pattern | Reverse-direction templating is rare; lets teams turn ad-hoc work into a reusable formula. |

Modern engineering platforms (Backstage, Cortex, OpsLevel, Port) increasingly treat **scorecards** and **templates** as first-class. Beads's molecule story reaches further because:

- The DAG operates on work, not commands → it integrates with the issue tracker rather than competing.
- Aspects and convoys capture the cross-cutting and multi-perspective patterns industry tools handle ad-hoc.
- Project / operator / orchestrator layering matches `~/.gitconfig` precedence — operators already know this shape.

The gap industry tools haven't filled: **a single surface where the work plane and the workflow plane share a tracker**. That's the gap Gemba's Workflow surface should claim.

## 3. Proposal — Gemba's Workflow concept

### 3.1 Surface

Workflow lands as an in-pane tab inside **Settings → Workflows** plus contextual entry points across the app. The tabs split:

| Tab | Contents |
|---|---|
| **Library** | All formulas resolved from the three search paths, grouped by type (workflow / aspect / convoy / expansion). Source path + version visible per row. Click → formula detail (DAG visualization + variables). |
| **Active runs** | Live mols + wisps. Status, progress %, step the agent is on, owner. Click → run detail (drawer with the step DAG and per-step status). |
| **Authoring** | Editor for project-local `formulas/*.formula.toml` files with schema-aware completions, dry-run cook + dry-run pour. |

Two additional contextual surfaces piggyback on existing chrome:

- **Bead drawer → "Workflow" section.** Shows the molecule attached to this bead (if any), with progress and step state. "Attach workflow…" button opens a picker over the Library.
- **PM panel quick action: "Run workflow."** Operator says "run TDD on this epic" → PM panel surfaces the matching formula, asks for variable confirmation, pours.

#### 3.1.1 Layout — three-column split with a graph centerpiece

The Library and Active-runs tabs share a three-column layout, with the formula DAG rendered on a React Flow canvas (reusing the engine that already powers `/graph`):

```
┌─────────────────┬───────────────────────────────────────┬──────────────────┐
│ Library         │ shiny-enterprise · workflow · v1      │ Variables        │
│ ─────────────   │ Source: ~/.beads/formulas/            │  feature *       │
│ 🔍 search…      │ Extends: shiny  +  rule-of-five       │  assignee        │
│                 │                                       │                  │
│ 📋 Workflows    │ ┌─────────────────────────────────┐   │ Composition      │
│   ▸ shiny       │ │   ◯ design                      │   │  ↳ extends shiny │
│   ▶ shiny-ent.  │ │       │                         │   │  ↳ ▣ rule-of-5   │
│   ▸ shiny-sec.  │ │       ▼                         │   │     on implement │
│   ▸ polecat-work│ │   ▣ implement (×5 perspectives) │   │                  │
│   …             │ │       │                         │   │ Recent runs (3)  │
│ 🚐 Convoys      │ │       ▼                         │   │  • mol-…  done   │
│   ▸ code-review │ │   ◯ review                      │   │  • mol-…  in-fl. │
│   ▸ design      │ │       │                         │   │                  │
│ 🎯 Aspects      │ │       ▼                         │   │ ─────────────    │
│   ▸ security-…  │ │   ◯ test                        │   │ [ Run            ]│
│ 📐 Expansions   │ │       │                         │   │ [ Run as temp    ]│
│   ▸ rule-of-5   │ │       ▼                         │   │ [ Edit           ]│
│   ▸ tdd-cycle   │ │   ◯ submit                      │   │                  │
│                 │ └─────────────────────────────────┘   │                  │
└─────────────────┴───────────────────────────────────────┴──────────────────┘
```

#### 3.1.2 The graph canvas — load-bearing

- **Nodes** are steps. Title + 1-line description; click expands the right rail to show the full step body.
- **Edges** are `needs:` deps, directional, top-down. No edge labels by default — the topology is the message.
- **Glyphs** declare provenance:
  - `◯` ordinary step
  - `✶` aspect-injected step (e.g. `security-prescan` from the `security-audit` aspect)
  - `▣` expansion-replacement (the original `implement` is gone; the five rule-of-five steps are in its place)
  - `║` convoy leg (parallel; renders as hub-and-spoke around a synthesizer node)
- **Parallel groups** render as faint background bands so operators see "these can run together" without reading edges.
- **State color** (Run mode only): ready=blue, in-progress=animated stroke, done=green, blocked=grey, skipped=hollow.

#### 3.1.3 Three view modes for the same graph

A small toggle at the top of the canvas:

| Mode | What changes |
|---|---|
| **Template** | Pure proto DAG. No state, no agents. The "shape" view. |
| **Composition** | Aspects expand inline as `✶` satellites; expansions show their replacement; convoys render hub-and-spoke. The "what cooks into" view. |
| **Run** | A specific live mol/wisp overlaid: per-step status, assigned agent, time-on-step, click-to-evidence. |

Defaults per surface: Library detail → **Composition**, Active-run drawer → **Run**, Authoring preview → **Composition** (re-rendered on edit).

#### 3.1.4 The right rail — three context states

The rail's content flips based on selection without changing the page:

1. **Formula selected, no step focused** — variables table, composition stack, recent runs, primary actions (Run / Run as temp / Edit).
2. **Step focused** — full step body (markdown), entry/exit criteria, command excerpts. Operators read exactly what `gt prime` shows the agent inline.
3. **Run focused, step focused** — adds: assigned session (chip jumps to Agent Sessions), evidence captured at this step, decisions made, escalations raised.

#### 3.1.5 Authoring split

```
┌────────────────────────────────────┬─────────────────────────────┐
│ formulas/my-feature.formula.toml   │ Live preview                │
│ ─────────────────────────────────  │ ┌─────────────────────────┐ │
│ formula = "my-feature"             │ │   ◯ design              │ │
│ type = "workflow"                  │ │       │                 │ │
│                                    │ │       ▼                 │ │
│ [[steps]]                          │ │   ◯ implement           │ │
│ id = "design"                      │ └─────────────────────────┘ │
│ title = "Design {{feature}}"       │                             │
│                                    │ Validation: ✓ cook clean    │
│ [[steps]]                          │ Variables: feature *        │
│ id = "implement"                   │                             │
│ needs = ["design"]                 │ [Validate]  [Save]          │
│ …                                  │                             │
└────────────────────────────────────┴─────────────────────────────┘
```

Monaco / CodeMirror editor with schema-aware completions; right pane re-cooks on debounce; **Validate** runs `bd cook --dry-run`; **Save** writes back to the project's `formulas/` directory and bumps `version`. Behind a feature flag in v1 — the validation story (§5.7) deserves to land before Authoring goes wide.

#### 3.1.6 Active runs — same graph, Run mode

```
mol-shiny-feature/auth · 3 of 5 · 12m elapsed · ETA ~8m
[Pause] [Burn] [Resume from step]
```

Steps that need an agent show the assigned session as a small chip on the node; clicking the chip jumps to that session in the Agent Sessions pane. Convoy runs render the legs around a center synthesizer node and animate the synthesis arrow when the verdict lands.

#### 3.1.7 Promotion to a first-order rail item — three criteria

The Workflow surface earns a sidebar slot (between **Insights** and **Agent Sessions** — it's reflective + reference, not live runtime) when these three things hit:

1. **Daily click rate.** Today's flow is "set up workflows once, auto-attach handles the rest." That's not a daily destination. Once the **distill** path gets traction (operators extracting formulas from finished epics on a regular cadence), the surface earns daily traffic.
2. **Authoring adoption.** When operators are writing formulas, not just running them, the editor needs more breathing room than a Settings sub-tab.
3. **Convoy verdict surface.** The synthesized output of convoys (10-leg `code-review` with prioritized findings) is gnarly enough to want its own page. When that lands, Workflow has the gravitas for a rail slot.

The cost of promotion later is one entry in `Sidebar.tsx`'s items array; nothing else moves.

### 3.2 Current state — what Gemba touches today

The Workflow proposal is largely additive. An audit of the existing tree turns up only **four real touchpoints** + **two passive interactions** + **three small leaks** that should be cleaned up before the Workflow surface lands.

**Real touchpoints (active code):**

| File / Line | What it does |
|---|---|
| `internal/adapter/bd/workplane.go:162` | Manifest declares `beads:issue_type` field extension; the value list includes `molecule`. Display-only — the SPA renders it as a chip. |
| `internal/adapter/dolt/workplane.go:185` | Same declaration on the dolt-direct adaptor. |
| `internal/shader/gastown/shader.go:109,147,161` | `isWisp(id)` short-circuit (4 lines): the title-prefix shader passes wisps through unencoded so prefix-stacking doesn't corrupt their titles. |
| `internal/adapter/bd/doc.go:6` | Comment-only mention of `molecule` as one of the bd subcommands the adaptor wraps. The adaptor does NOT actually shell `bd mol *`. |

**Passive interactions (the boundary leaks):**

1. **Templates render as ordinary beads.** A formula's cooked proto carries the `template` label and shows up in `bd list` output. The board, the backlog, the agent cards, the planner — every consumer of the WorkPlane sees them. No filter today.
2. **Wisps appear in lists.** `bd list` returns wisps when they exist. The bd adaptor passes them through unchanged. They show up in Plan / Sessions / Insights as ordinary beads.

**No code in Gemba calls `bd mol`, `bd formula`, or `bd cook`.** The Workflow proposal will be the first surface that does.

**Three small leaks to clean up before Workflow ships (file as gm-flow-design.0):**

- **`L1`. Filter templates out of the board / planner.** Beads with the `template` label belong on the Workflow surface, not on Plan. The bd adaptor's `ListWorkItems` should accept a "hide templates" filter that the SPA's board query sets by default; only the Workflow Library should opt in.
- **`L2`. Filter wisps out of the board / planner.** Same logic — wisps are operational scaffolding, not work the operator is tracking. They belong only in Active runs.
- **`L3`. Step beads from poured molecules need a "this is a workflow step" badge.** When a molecule like `mol-polecat-work` pours, its 8 steps become real beads. The dispatcher might assign one to a session that doesn't realize it's part of a workflow run. The board should render workflow-step beads with a small `▢ workflow` chip linking back to the parent run, so the operator can navigate to the run from the bead and back.

These leaks aren't blocking the design — they're the v1 prerequisites for the Library and Active runs tabs to feel clean. Each is a small change in the `bd` adaptor + a filter at the board query layer.

### 3.2 Vocabulary translation

The chemistry metaphor is delightful and load-bearing for the underlying engine, but it's optional cognitive overhead for the operator. Gemba's UI uses these translations:

| Beads token | Gemba UI label |
|---|---|
| Formula | Workflow definition |
| Proto / protomolecule | Workflow template |
| Mol | Workflow run |
| Wisp | Workflow run *(ephemeral)* — small badge `temporary` |
| Pour | Run |
| Bond | Combine |
| Aspect | Cross-cutting rule |
| Convoy | Multi-perspective workflow |
| Distill | Extract from work |

Hover tooltips reveal the underlying vocabulary so power users connect the two views; CLI-savvy operators recognize the same nouns they already know from `bd mol`.

### 3.3 Three integration points

#### (a) Manual attach — bead drawer + PM panel

From any bead drawer, the operator clicks **Attach workflow** to open the picker. The picker filters by:

- Formula `type` (workflow only — aspects and expansions don't pour standalone)
- Compatibility hints from the formula (e.g., `applicable_to: [epic, story, bug]` — new field, see §5 risks)
- Variables that can be auto-resolved from the bead context (`hook_bead`, project, repo)

After picking, the operator confirms variable values (most pre-filled), then chooses **Run** or **Run as temporary**. The chosen molecule is poured/wisped via `bd mol pour|wisp`, attached to the bead via `bd mol attach <bead> <mol>`, and the drawer's Workflow section refreshes.

The PM panel's quick-action shortcut — "Recommend workflow" or "Run workflow on this" — surfaces the formulas most relevant to the open bead's labels and history. The PM persona's `epic_order` skill grows a sibling **`recommend_workflow`** skill (filed under `gm-858` PM use-case catalog) that ranks candidates by past success.

#### (b) Session auto-attach — dispatcher → workflow

When the dispatcher starts a session against a bead, the orchestrator consults a project-level **workflow policy** (a small TOML file in `.gemba/workflows.toml`):

```toml
# .gemba/workflows.toml — project workflow policy

[default]
formula = "mol-polecat-work"      # Every session gets this unless overridden

[[overrides]]
match.kind = "epic"
formula = "shiny-enterprise"

[[overrides]]
match.label = "type:bug"
formula = "mol-polecat-work-monorepo-tdd"

[[overrides]]
match.label = "tier:opus"
formula = "shiny-secure"          # High-tier work runs the security aspect
```

On session start, Gemba pours the resolved formula, attaches it to the bead, and primes the agent's `gt prime` output with the molecule's first ready step inline. The agent works the step, marks it done (or the bridge auto-detects completion via existing `bd update --status` plumbing), and the next ready step lights up.

The reuse here is non-trivial: every conversation about agent contracts, retry on rejection, and "what does done mean" already lives in formulas like `mol-polecat-work` (8 steps including `pre-verify` and `submit-and-exit`). Surfacing that in Gemba turns the existing operational lore into an operator-visible contract.

#### (c) Epic gates — start + end enforcement

Epic UIState transitions become the trigger points for two kinds of gating workflow:

**Epic start (`backlog` → `in_scope`):** pour a planning workflow (`mol-idea-to-plan`, `mol-plan-review`, or operator-configured equivalent). Until the gate molecule reports `complete`, the epic stays `in_scope`-pending. Operator sees a yellow chip on the epic card: "Gate: planning in progress · 3/5".

**Epic end (`in_progress` → `done`):** pour a review/validation workflow (`code-review` convoy + `security-audit` aspect for high-tier work, or a project-specific gate). Marking the epic done is **blocked** by Gemba's UI until every gate-step resolves. Bypass requires explicit operator override (nonce-gated, audit-logged).

Gate workflows are configured in the same `.gemba/workflows.toml`:

```toml
[[gate]]
when.transition = "ui_state:on_deck->in_scope"
formula = "mol-idea-to-plan"
required = true                   # Refuse the transition until complete

[[gate]]
when.transition = "status:in_progress->closed"
when.match.kind = "epic"
formula = "code-review"
preset = "gate"                   # Use the convoy's "gate" preset (4 lightweight legs)
required = true
```

This is the load-bearing claim for the proposal: **a Gemba workspace gets a project-local declarative workflow policy** that ties motion to template, and the SPA enforces it. That's the "Workflow" pane's reason to exist.

## 4. Use-case catalog

Beyond the three integration points, several patterns become natural once Workflow is a first-class concept:

### 4.1 Sprint open / close

- Sprint open → wisp `mol-sync-workspace` (workspace prep across all participants).
- Sprint close → wisp `mol-digest-generate` for the operator + the PM persona's retro skill.

### 4.2 Onboarding

A new project's `gemba serve` lands `/new` open. After ratification, pour `mol-idea-to-plan` so the new project ships with an end-to-end thinking template, not just an empty board.

### 4.3 Bug intake

When a bug is filed, auto-attach `shiny` (design-before-code keeps the fix from regressing) and gate the close transition on `code-review` preset=gate.

### 4.4 PR review

`bd events: workitem_updated` events that flip a bead from `in_progress` → `in_review` pour `mol-polecat-review-pr` for the assigned reviewer. The convoy's parallel legs return a synthesized verdict the SPA renders inline on the bead's card.

### 4.5 Standing patrols

Operational hygiene wisps run on a cadence — `mol-deacon-patrol`, `mol-pr-feedback-patrol`, `mol-dog-doctor`. Today they're agent-managed; surfacing them in Gemba's Active runs tab makes the maintenance loop visible to humans.

### 4.6 Cross-cutting policy via aspects

Compliance / regulated environments declare aspects in `.gemba/workflows.toml`:

```toml
[[aspect]]
formula = "security-audit"
applies_to.label = "fed:safe"     # Every fed:safe bead gets the aspect

[[aspect]]
formula = "audit-trail"           # Hypothetical "log every step to compliance store"
applies_to.epic_under = "gm-compliance-root"
```

The cook step at pour time injects the aspect's advice into the resolved DAG. Operators see one workflow run; the SPA highlights aspect-injected steps with a `cross-cutting` glyph.

### 4.7 Distill — capture team patterns

After running an ad-hoc planning epic, the operator clicks **Extract workflow…** on the epic drawer. Gemba shells `bd mol distill <epic-id>`, opens the result in the Authoring tab, and the team gets a reusable formula that captures what they actually did. This closes the loop industry tools usually miss: most workflow systems treat the template as authored-once-up-front; beads + Gemba treat it as **discoverable from real motion**.

## 5. Risks & open questions

1. **Vocabulary tension.** Hiding "molecule" entirely loses the chemistry mnemonic that makes the beads model coherent. Hiding it entirely also makes CLI ⇄ SPA cross-references confusing (the operator's terminal still says "mol"). The proposal threads this with hover tooltips and a small "what is this?" link in the Workflow pane.

2. **Compatibility hints don't exist yet.** The picker filters on `applicable_to`, but no formula declares that today. We'll either (a) add the field to the formula schema, opt-in, or (b) infer compatibility from the variables a formula needs (a formula requiring `{{epic_id}}` is epic-applicable). I'd prototype (b) first because it touches no existing files.

3. **Gate enforcement vs. bypass.** Operator override is necessary — gates that can't be bypassed deadlock teams. Nonce + audit-log makes this safe but explicit. Open question: who can override? Project-level config (`bypass: operator-only`) or per-gate config (`bypass: any`).

4. **Workflow policy file location.** `.gemba/workflows.toml` lives alongside `.gemba/agents.toml` and `.gemba/personas/`. Three small configs are easier to reason about than one big config.

5. **SPA surface load-bearing.** A "Workflows" rail item conflicts with the six-pane sidebar we just ratified (Plan / Review / Escalations / Insights / Agent Sessions / Settings). Workflow doesn't deserve its own rail slot — it lives as a Settings tab + bead-drawer section + PM-panel quick action. This is consistent with the activity-organization principle from the sidebar reorg.

6. **Wisp visibility.** Today wisps don't sync via git, so two operators on the same project see different wisp runs. Gemba surfacing wisps in Active runs needs to make this clear (a `local-only` badge); otherwise a remote operator will be confused why they can't see what their teammate is running.

7. **Edit-author safety.** The Authoring tab edits TOML files in the project source tree. Validation needs to reject malformed YAML/TOML before write — `bd cook --dry-run` should land as a build-prereq. The SPA's editor must not silently ship a broken formula.

## 6. Implementation slices

If this proposal is ratified, the work breaks into roughly five slices, each ~one bead:

1. **Workflow API surface** — `GET /v1/workflows/formulas`, `GET /v1/workflows/formulas/:id`, `GET /v1/workflows/runs`, `POST /v1/workflows/runs` (pour), `POST /v1/workflows/runs/:id/burn` (wisp discard). All proxied to `bd mol` / `bd formula` underneath.

2. **Settings → Workflows pane** — Library + Active runs + Authoring tabs. Reuses the `<TabBar>` from gm-e12.19.7. Authoring's editor lands behind a feature flag while we work the validation story (§5.7).

3. **Bead drawer Workflow section** — picker, attach, progress display. Reuses the Active runs row component for compact display.

4. **Workflow policy + session auto-attach** — `.gemba/workflows.toml` schema, the `WorkflowPolicy` resolver, dispatcher hook that pours the resolved formula on `StartSession`. Backwards-compatible: missing config → existing dispatcher behavior, unchanged.

5. **Epic gates** — UI-state transition guard reading the policy's `[[gate]]` table. Override surface (nonce-gated, audit-logged). Surface the gate state on the epic card and the drawer.

A sixth slice — the PM persona's `recommend_workflow` skill — files under `gm-858` (PM skill catalog) and lands when the PM persona is ready to grow a second skill alongside `epic_order`.

## 7. Resolution plan

1. Open this design as `gm-flow-design` for ratification (parent for the slice beads above).
2. Pre-ratification reviewers: PM persona owner (gm-518), conformance-suite owner (gm-e3.6.3), ratifier of `.gemba/agents.toml` shape.
3. Ratification gate is the same as gm-3nk and gm-57b — operator review of the surface naming, then file the slices.

## Appendix — formula examples already in the tree

| Formula | Type | What it does | Use in Gemba |
|---|---|---|---|
| `shiny` | workflow | 5-step canonical: design → implement → review → test → submit | Default per-bead workflow on session start |
| `shiny-enterprise` | workflow | shiny + Rule of Five expansion on `implement` | Tier:opus auto-attach |
| `shiny-secure` | workflow | shiny + security-audit aspect | High-blast-radius work auto-attach |
| `mol-polecat-work` | workflow | 8-step polecat lifecycle through MQ submission | Already the implicit session contract; surfacing makes it explicit |
| `mol-polecat-work-monorepo-tdd` | workflow | TDD variant for the monorepo rig | Bug-fix label auto-attach |
| `mol-idea-to-plan` | workflow | Vague idea → ratified, beads-ready impl plan | Epic-start gate |
| `mol-plan-review` | convoy | Parallel implementation-plan review | Plan ratification step |
| `mol-prd-review` | convoy | Parallel PRD review | Pre-implementation review |
| `code-review` | convoy | 10 review legs (correctness, security, smells, etc.) with `gate`/`full` presets | Epic-end gate (`gate` preset for routine, `full` for high-tier) |
| `security-audit` | aspect | Pre/post security scan around `implement` and `submit` | Auto-applied to `fed:safe` work via `.gemba/workflows.toml` |
| `tdd-cycle` | expansion | Replace `implement` with red-green-refactor | Auto-applied to bug fixes |
| `rule-of-five` | expansion | Replace one step with five-perspective elaboration | Auto-applied to high-stakes implement steps |

Operators get a useful library on day one — every formula above ships in `~/.beads/formulas/` already. The Workflow pane just makes them visible.
