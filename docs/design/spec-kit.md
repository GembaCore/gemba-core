---
decision: none
---

# Spec Kit Integration

Spec Kit fits Gemba as the first **bootstrap pack** provider: a planning
substrate that can generate a draft Beads decomposition without replacing
Beads as the work tracker.

## Semantic Mapping

Spec Kit artifacts use this hierarchy:

- Feature directory: a coherent feature specification and plan.
- `spec.md` user stories: independently testable increments, prioritized as P1, P2, P3.
- `tasks.md` tasks: implementation checklist rows (`T###`) mapped back to user stories with `[US#]`.

Gemba projects that into a staged Beads draft as:

- Feature import milestone: groups the generated Beads-only tree and preserves the source artifacts.
- Feature epic: captures the feature-level requirements and acceptance criteria.
- User-story stories: one Beads story per Spec Kit `User Story #`.
- Implementation tasks: one Beads task per Spec Kit `T###`, parented beneath the matching story when `[US#]` is present.

Labels provide stable traceability:

- `source:spec-kit`
- `speckit:<feature-id>`
- `speckit:milestone`, `speckit:epic`, `speckit:story`, `speckit:task`
- `speckit:US#`, `speckit:T###`
- `speckit:parallel` when the task has `[P]`

Typed custom fields mirror the labels for adaptors that can persist them:

- `speckit.feature_id`
- `speckit.kind`
- `speckit.user_story_id`
- `speckit.task_id`

## Bootstrap Pack Fit

As a bootstrap pack, Spec Kit helps create the shape of work before Gemba
has Beads, or before an existing Beads database has this feature:

1. Draft or refine the constitution.
2. Run specify/clarify/plan/tasks for a feature.
3. Open Refine -> Bootstrap and load the Spec Kit provider.
4. Review the generated draft set in the cascade view.
5. Use the RHP coaching interaction to reshape the draft as a batch.
6. Manually tweak any remaining item details.
7. Ratify by committing the staged set into a selected Beads database, or export it as Beads-compatible JSONL.

Draft beads are not stored in a Beads database. They are process-local
review objects until the operator ratifies them or exports JSONL. This
keeps Gemba's bootstrap flow grounded in user outcomes and acceptance
scenarios before implementation sessions start.

The same flow works against an existing Beads database. In that mode, Spec Kit is not "import once"; every run compares the current artifacts against prior `source:spec-kit` beads and produces a staged plan.

## Iterative Sync

Successive Spec Kit runs are reconciled by traceability metadata:

- Create: desired milestone/epic/story/task does not exist yet.
- Update: desired item exists by trace label and should be refreshed from the source artifact.
- Delete: existing `source:spec-kit` item for the feature no longer appears in the desired artifact graph.

Gemba must show this plan before applying it. The preview and draft
routes are read-only; the apply route uses the same plan and is
nonce-gated. Deletes require a WorkPlane that supports deletion.

The apply route also requires the preview's `hash` value. The hash is
computed from the staged change list, counts, JSONL manifest, and
warnings. If Spec Kit files or matching Beads change between preview and
apply, the hash changes and the request is rejected as stale. Plans with
deletes require explicit `allow_deletes=true`; the UI exposes that only
after listing the stale items.

## Coach Fit

As a coach input, Spec Kit gives Gemba durable drift checks and a rich
batch-editing prompt. The bootstrap interaction is seeded with:

- The goal: translate the input into milestones, epics, stories, and beads that represent the operator's view of the project.
- The Spec Kit feature title, source paths, user stories, acceptance scenarios, functional requirements, tasks, and `[P]` markers.
- The staged plan hash, create/update/delete counts, and draft item tree.
- The constraint that draft beads remain uncommitted until ratification or JSONL export.

The RHP surface offers prepared reply bubbles for common review states:
"Looks good", "I want changes", "I'll edit on board", "Export JSONL",
and "Ask questions". Those are prompts into the same interaction, not
database mutations.

Spec Kit also gives Gemba durable checks:

- Readiness: every story has an independent test criterion and enough tasks to execute.
- Traceability: every synced Bead links back to a feature, user story, or `T###` task.
- Drift: Beads can be compared against changed `spec.md`, `plan.md`, and `tasks.md`.
- Parallelism: `[P]` markers become planner hints, not hard scheduling rules.

## Backlog

An upstream Spec Kit extension or preset should eventually call Gemba directly after `tasks.md` generation. That belongs upstream because Spec Kit already exposes extension hooks around specify/plan/tasks.

`bd import` can ingest newline-delimited JSON with upsert semantics and
supports `--dry-run`, so a Spec Kit generated Beads JSONL manifest is
viable for create/update workflows. The Gemba manifest uses stable
synthetic IDs (`speckit/<feature>/<part>`) and validates required JSONL
fields before exposing the preview. It is not a complete substitute for
the Gemba-native staged sync because direct JSONL import does not model
deletes as a first-class operation today.

Future bootstrap packs should implement the same generic contract:
provider context -> draft Beads set -> guided batch review -> manual
edits -> ratify to database or export JSONL.

Operator-facing usage lives in [Working with Spec Kit](../getting-started/working-with-spec-kit).
