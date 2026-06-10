---
title: "Agentic Spec-Driven Development (ASDD)"
decision: gm-v0sp
---

# Agentic Spec-Driven Development (ASDD)

The gemba approach to building software with autonomous and semi-autonomous
agents *without* surrendering authorship of intent.

---

## 1. The problem with how the industry is doing this

Two reference points frame the current state of spec-driven agent workflows.

**Augment Intent** treats a markdown spec as a *living* document that agents
rewrite as they complete work. The spec and the codebase converge toward each
other; the spec is the source of truth, but the source of truth is mutated by
the same actors it governs. That is elegant in a demo and structurally unsafe
in production. When the entity executing the work is also the entity rewriting
the description of the work, `git blame` no longer answers "whose intent was
this?" — a property humans rely on for retros, audits, and incident review.

**GitHub Spec Kit** treats specs as phased, human-authored artifacts
(`/specify` → `/plan` → `/tasks` → `/implement`) gated by a constitution.
This preserves authorship and intent but leaves the *work* stranded in
markdown. Tasks declared in `tasks.md` have no operational identity: they
cannot be claimed, blocked, slung to a convoy, or appear in a standup view.
The spec is a planning artifact that dies on the way to the runtime.

Both approaches make the same category error in opposite directions. Intent
collapses *intent* and *work* into one mutable surface. Spec Kit separates
them so cleanly that the bridge has to be rebuilt by hand every cycle.

## 2. The gemba constraints that force a different shape

Gemba's locked architectural decisions rule out both endpoints:

1. **Beads is the work system of record.** Tasks live in `bd`, not markdown.
   Anything that doesn't round-trip through `bd --json` is not work, it is
   notes.
2. **No direct writes** to Dolt, JSONL, `.gt/`, `.gc/`, or any controller
   socket. State changes go through the published CLIs.
3. **Zero Framework Cognition in the UI.** The system shows data and offers
   actions; it does not decide *what* should happen on the user's behalf.
4. **Declarative desired-vs-actual.** The gap between intent and reality is a
   first-class concept that is *rendered*, not silently reconciled.
5. **Confirmation-gated mutations** with idempotent nonces. Every state
   change is auditable and replay-safe.

The Intent model violates 1, 3, and 5. The Spec Kit model leaves 1 unmet.
The shape that satisfies all five is the one gemba already uses for every
other declarative input: **author intent in a file, reconcile it into beads,
render the gap.**

## 3. ASDD: the model

Three primitives, one loop.

### 3.1 The spec is declared intent

A spec lives at `specs/<slug>/spec.md` as a human-authored markdown document
with structured frontmatter and four canonical sections: **Why**,
**Decisions**, **Plan**, **Tasks**. The spec is the single place a human
explains what should be true about the system and why. The spec is *never*
written to by an agent without a human-mediated promotion step.

```yaml
---
spec: auth-refactor
epic: gm-e7
status: declared       # declared | reconciling | live | archived
constitution: .gemba/constitution.md
labels: [surface:auth, tier:opus, risk:high]
---
```

Task headings inside the spec carry stable, spec-local handles (`T-01`,
`T-02`, …) so the document can be edited, reordered, and refactored without
losing the mapping to operational beads.

### 3.2 Beads are filed tasks — the operational ledger

The spec does not execute. Beads execute. Each `T-NN` in the spec corresponds
to one bead, mapped in `specs/<slug>/.lockfile.json` (human-readable,
checked in alongside the spec). Beads carry the things a markdown bullet
cannot: status, assignee, edges (`blocks`, `waits-for`, `parent-child`,
`discovered-from`, `related`, `replies-to`, `conditional-blocks`), labels
(`surface:*`, `tier:*`, `risk:*`, `fed:*`, `provider:*`), audit history.

Beads are the only thing convoys, molecules, and the Kanban view consume.
The spec does not appear in the runtime. The spec *produces* the runtime.

### 3.3 Reconciliation is the bridge — and it is visible

`gemba spec reconcile <slug>` is a pure planner. It:

1. Parses `spec.md` → desired bead set.
2. Reads existing beads via `bd list --json --label spec:<slug>`.
3. Computes three lists — **create**, **update**, **orphan** — and renders
   them in the same desired-vs-actual diff component the Convoy Kanban uses
   for agent state.
4. Applies on confirmation, each mutation gated by an `X-GEMBA-Confirm`
   nonce so the operation is auditable and idempotent.

This is the same reconciliation loop gemba commits to for Gas City's
`city.toml`. The spec is just another declarative input feeding the same
machine. No new paradigm; one more declarative source.

### 3.4 Writeback is a view-time overlay, not a file mutation

The Intent insight worth preserving is *seeing live status against intent
text*. ASDD achieves this without violating authorship:

- **Status badges are rendered, not written.** When the UI displays
  `spec.md`, each `T-NN` heading gets a live badge pulled from `bd show
  <id> --json` — ✅ closed, 🟡 in-progress, 🔵 ready, ⛔ blocked. The badge
  exists in the view; the file is unchanged. `git diff spec.md` continues
  to mean "intent changed," never "the work happened."
- **Explicit snapshots.** `gemba spec snapshot` produces a
  `spec.snapshot.md` with badges baked in for releases and retros. Always
  explicit, always audit-logged.
- **Decision promotion via `bd mail`.** When an agent or operator wants to
  record a decision that surfaced during implementation, it lands as a
  mail message on the spec's epic bead. A UI affordance ("promote to spec")
  opens a diff that *proposes* appending to the spec's Decisions section.
  A human merges it. Git is the journal. No autonomous spec edits.
- **Constitutional closure.** Closing the epic requires every `T-NN` bead
  closed or explicitly `wontdo` in the lockfile. A `bd` hook enforces it.

## 4. The ASDD loop, end to end

```
            ┌─────────────────────────────────────┐
            │           specs/<slug>/spec.md      │
            │  (declared intent, human-authored)  │
            └────────────┬────────────────────────┘
                         │  reconcile (diff → nonce → apply)
                         ▼
            ┌─────────────────────────────────────┐
            │              bd ledger              │
            │ (filed tasks, edges, status, audit) │
            └────────────┬────────────────────────┘
                         │  status, mail, completions
                         ▼
            ┌─────────────────────────────────────┐
            │       view-time overlay & gap       │
            │   (badges on spec, drift indicator) │
            └────────────┬────────────────────────┘
                         │  decision promotion (PR-style)
                         ▼
                    (back to spec.md)
```

Authorship flows in one direction. Status flows in the other. The two never
collide on the same bytes.

## 5. Why the boundary matters

- **Declared intent stays declared.** Specs describe the standard; the
  reconciler is responsible for "how to get there." A spec author never
  writes `bd create` calls.
- **The gap is rendered, not hidden.** The difference between spec and
  ledger is computed, surfaced, and confirmed — never silently closed. The
  gap is observable before it is resolved. This is the gemba walk made
  literal.
- **Intent is durable.** The spec captures *why* and *what*, not *who did
  it when*. Authorship is preserved because the file is never rewritten by
  the workers it commands.
- **Filed tasks have operational identity.** Work in `bd` can be slung,
  blocked, claimed, replayed. Markdown bullets cannot.

## 6. What ASDD is not

- **Not a coordinator agent.** Coordination is `bd`'s job: edges, molecules,
  convoys. The spec does not reinvent the DAG.
- **Not an IDE.** Gemba is a viewer over `gt/gc/bd`. ASDD adds a spec
  surface and a reconciler; it does not absorb the editor, the browser, or
  the terminal.
- **Not a writeback path for agents.** Agents never edit `spec.md`. They
  emit decisions via `bd mail`; humans promote.
- **Not a replacement for the constitution.** `.gemba/constitution.md`
  remains the immovable layer. The spec is governed by it; the reconciler
  enforces it on lint.

## 7. Comparison

|                              | Augment Intent | GitHub Spec Kit | **ASDD** |
|------------------------------|----------------|-----------------|----------|
| Spec format                  | Markdown       | Markdown        | Markdown |
| Spec is human-authored       | Initially      | Always          | Always   |
| Spec is agent-rewritten      | **Yes**        | No              | **No**   |
| Tasks have operational ID    | Implicit       | No              | **Yes (beads)** |
| Status visible on spec text  | **Yes**        | No              | **Yes (overlay)** |
| Reconciliation is rendered   | Hidden         | N/A             | **Yes**  |
| Mutations are nonce-gated    | No             | N/A             | **Yes**  |
| Decisions preserved in git   | Mixed          | Yes             | **Yes**  |
| Single source for *work*     | Spec           | None            | **Beads** |
| Single source for *intent*   | Spec (mutable) | Spec            | **Spec (immutable to agents)** |

## 8. The gemba walk, made literal

A gemba walk in lean practice is a manager standing on the floor, observing
the gap between the standard and the work, and resolving it in dialogue
with the people doing the work. The standard is not rewritten by the line
worker. The status of production is not hidden from the manager. The gap is
not eliminated by hiding either side.

ASDD applies the same loop to software. The spec is the standard. The
beads are the floor. The reconciler is the walk. The badges are what the
manager sees when they look up from the spec. Nothing on the floor edits
the standard; nothing on the standard pretends to know the floor.

That symmetry is the entire point.

## 9. Status

Filed as gemba epic `gm-v0sp` with child beads covering: spec parser,
lockfile format, reconciler diff engine, nonce-gated apply, watch panel
with live badges, decision-promotion flow, constitution linter, snapshot
command, spec-aware bd hook for closure gating, and template scaffold
(`gemba spec new`).

CLI design decisions are filed under `gm-o9t8` and resolved:

- `gm-o9t8.7` — **Conflict policy.** Field-level ownership: spec owns
  title, description, type, parent, priority, edges within the spec, and
  labels in declared namespaces (`surface:*`, `tier:*`, `risk:*`, `fed:*`,
  `provider:*`); bead owns status, close reason, assignee, external ref,
  estimates, comments, and edges outside the spec. `wontdo` closures get
  dual writeback (lockfile authoritative; spec absorbs via a proposed
  Decisions append so spec ↔ implementation stays diffable).
- `gm-o9t8.5` — **Verb taxonomy.** Five groups: **Govern** (spec /
  decision / constitution), **Ledger** (bead), **Dispatch** (run / agent /
  stop), **Inspect** (logs / diff / show / status), **Escape** (shell /
  port-forward / code). `plan` and `design` from the original draft are
  dropped; spec/decision/constitution land under Govern.
- `gm-o9t8.6` — **Local/server split.** `spec new/lint`, `constitution
  init/show/lint/edit` run local. `spec reconcile/snapshot/adopt`, `decision *`,
  and writes run server-side with nonce. `spec watch` is server-streamed.
- `gm-o9t8.11` — **Bypass policy.** `spec_strict` key in
  `.gemba/constitution.md`; permissive default. `--force-no-spec --reason`
  escape hatch records an audit event when strict.
- `gm-o9t8.8` — **Mode signaling.** Local mode detection (Ad-hoc,
  Scaffolded, Governed, Reverse-governed) drives the `gemba status` output.
- `gm-o9t8.9` — **Multi-spec.** At-most-one `spec:<slug>` label per bead,
  server-enforced. Cross-cutting work uses a parent spec.
- `gm-o9t8.10` — **Transitions.** `spec adopt` (Ad-hoc → Reverse-governed),
  `spec freeze`/`unfreeze` (Governed ↔ Scaffolded), `spec import` (RFC-PR
  pattern with `--replace`).
- `gm-o9t8.12` — **Workspace ↔ spec coupling.** One workspace, many
  specs; slug inferred from cwd or single-live spec or explicit `--spec`.

The remote design doc (`docs/superpowers/specs/2026-05-11-gemba-remote-design.md` §4)
is the canonical CLI surface reference; this whitepaper is the canonical
reference for the *approach*; the constitution and per-spec documents are
downstream of both.

## 10 Constitution Schema (Reference)

The constitution lives at `.gemba/constitution.md`. The canonical JSON
Schema is `internal/spec/schema/constitution.schema.json` (id
`https://gemba.dev/schema/constitution-1.0.0.json`). Keys are supplied via a
top-level YAML frontmatter block and/or a `## Config` yaml fence; both are
merged. Unknown top-level keys are rejected (`additionalProperties: false`).

Current catalog (schema `1.0.0`):

- `schema_version` (string, semver, required) — constitution schema version.
- `asdd_mode` (bool, default false) — tasks live in bd, not tasks.md.
- `spec_strict` (bool, default false) — master strict-mode switch; child knobs inherit when unset.
- `spec_strict_no_tasks_md` (bool, inherits `spec_strict`) — reject writes to tasks.md / todo.md.
- `require_decision_parent` (bool, inherits `spec_strict`) — spec frontmatter must declare `decision_parent`.
- `forbid_orphan_beads` (bool, inherits `spec_strict`) — every story bead must reference its spec.
- `require_priority` (bool, inherits `spec_strict`) — every Story must declare `Priority:`.
- `min_ac_count` (integer, default 0) — minimum AC list items per Story; 0 disables.

Versioning. The active version constant lives in
`internal/spec/constitution/version.go`; future schema changes bump
`CurrentVersion` and add a branch in `MigrateConstitution`. Existing files
without `schema_version` are migrated transparently and surface a
`constitution-version-mismatch` (warn) finding from `gemba constitution lint`.

## Decisions

- ADR 0001 — Spec Kit integration: hook-layer only (2026-05-13). See [docs/design/adr/0001-spec-kit-integration-strategy.md](adr/0001-spec-kit-integration-strategy.md).
