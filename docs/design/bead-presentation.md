---
title: "Bead entity presentation \u2014 card / tooltip / drawer"
decision: none
backfill: pending
---

# Bead entity presentation — card / tooltip / drawer

**Bead:** gm-c7q (M1.7d) · **Resolves DD:** bead-entity-visual-vocabulary
**Status:** decided · 2026-04-23
**Consumers:** M1.7a (`BeadCard`), M1.7b (`BeadTooltip`), M1.7c (`BeadDrawer`, already shipped)

## 1. Context

A `WorkItem` surfaces on three presentation planes in Gemba's board pane:

| Surface | Density | Trigger | Goal |
|---|---|---|---|
| **BoardCard** | compact | always visible in the column | scannable in peripheral vision; answer *"what / who / how urgent / how fresh"* in <200ms |
| **BeadTooltip** | transient | ~500ms hover or keyboard focus | one more step of context than the card without committing to a drawer open |
| **BeadDrawer** | complete | click card, or navigate from relationship | every field on the wire; navigable relationship graph |

This doc locks the visual vocabulary so a priority P0 bead looks the same on all three, and so card → tooltip → drawer feels like a progressive zoom, not three unrelated components.

Source of truth for the entity shape: `internal/core/types.go` (`WorkItem`) and its generated mirror `web/src/types/core.gen.ts`. Adaptor-specific fields arrive under `WorkItem.custom` under `"<adaptor>:<key>"` namespaces (see `BeadDrawer.groupCustom`).

## 2. Shared visual vocabulary

These tokens are shared across all three surfaces. Any surface that departs from them is a bug.

### 2.1 Priority chip (`priority: number | null`)

Priority comes in as an integer; `0` is the most urgent. We render four buckets plus "unset".

| `priority` | Label | Light | Dark | Notes |
|---|---|---|---|---|
| `0` | `P0` | `bg-red-100 text-red-800` | `bg-red-900/40 text-red-200` | drop-everything |
| `1` | `P1` | `bg-orange-100 text-orange-800` | `bg-orange-900/40 text-orange-200` | current sprint |
| `2` | `P2` | `bg-amber-100 text-amber-800` | `bg-amber-900/40 text-amber-200` | default |
| `3` | `P3` | `bg-neutral-100 text-neutral-700` | `bg-neutral-800 text-neutral-300` | backlog |
| `≥4` | `P<n>` | `bg-neutral-100 text-neutral-500` | `bg-neutral-800 text-neutral-500` | rarely used |
| `null` | — | hidden | hidden | never render an empty chip; absence is meaningful |

Chip shape: `inline-flex items-center rounded px-1.5 py-0.5 text-xs font-mono`. Always exactly the letter `P` concatenated with the integer — no icon, no colored dot. The chip carries its own color signal.

### 2.2 State dot (`state_category: StateCategory`)

A single 8px dot encodes the normalized lane:

| `state_category` | Glyph | Light | Dark |
|---|---|---|---|
| `backlog` | `○` outline | `text-neutral-400` | `text-neutral-600` |
| `unstarted` | `◐` half | `text-blue-500` | `text-blue-400` |
| `started` | `●` filled | `text-amber-500` | `text-amber-400` |
| `completed` | `●` filled | `text-emerald-500` | `text-emerald-400` |
| `canceled` | `✕` cross | `text-neutral-400 line-through` | `text-neutral-600 line-through` |

Rendered as a single Unicode glyph, not an SVG, so it inlines with the ID in monospace without breaking baseline. The `status` string (the adaptor's own word — "In Review", "ready", etc.) is rendered as plain text next to the dot in the drawer only; the card and tooltip compress to the glyph.

### 2.3 Label chip (`labels: string[]`)

Labels are adaptor-native classifier strings (`fed:safe`, `layer:ui`, `milestone:m1`, `surface:frontend`, `risk:medium`). We render them as mono chips:

```
bg-neutral-100 text-neutral-700 rounded px-1.5 py-0.5 font-mono text-xs
dark: bg-neutral-800 text-neutral-300
```

**Colon-prefixed labels** (`fed:safe`, `layer:ui`) carry implicit grouping. We do NOT color-code per namespace — the variety of label prefixes across adaptors is unbounded and any fixed palette becomes wrong. A future milestone may add a small registry of well-known-namespace colors (`risk:high` in red, etc.); until then, all labels render identically.

**Truncation per surface:** see §3, §4, §5.

### 2.4 Agent pill (`assignee | owner: AgentRef | null`)

Two visual tokens encoding `agent_kind`:

| kind | Light | Dark |
|---|---|---|
| `agent` | `bg-blue-100 text-blue-800` | `bg-blue-900/40 text-blue-200` |
| `human` | `bg-emerald-100 text-emerald-800` | `bg-emerald-900/40 text-emerald-200` |

This is the only place human/agent distinction is rendered as color rather than text — it answers *"is a human on the hook?"* at a glance, which is the single most common question on the board.

Display format:
- Compact (card / tooltip): just the agent-kind pill + last segment of `name` or `id` in mono (`quartz`, not `gemba/polecats/quartz`).
- Full (drawer): pill + full name + `· role` + `· dialect` (see `BeadDrawer.AgentPill`).

Missing assignee renders as muted `unassigned`; missing owner renders the same. We never drop the row silently — the slot is reserved so misalignment is visible.

### 2.5 Glyph row (card + tooltip only)

A single-row affordance at the bottom of the card shows presence of high-value side-data without rendering it. Each glyph is a `lucide-react` icon at 12px; hidden when the underlying collection is empty.

| Glyph | Meaning | Visible iff |
|---|---|---|
| `MessageSquare` | has description | `description` length > 0 |
| `Link` | has evidence | `evidence.length > 0` |
| `GitBranch` | has relationships | `relationships.length > 0` |
| `CornerUpLeft` | has parent | any `parent_child` edge where `to === id` |
| `CheckSquare` | has DoD | `dod != null` |
| `AlertCircle` | `human_action_required` is true | `derived?.human_action_required` |
| `Hand` | `agent_claimable` is true | `derived?.agent_claimable` |

All glyphs use `text-neutral-400 dark:text-neutral-500` at rest. `AlertCircle` escalates to `text-amber-500` because human-action-required is the one derived signal worth reading across the column.

Counts are NOT rendered on the card (too noisy at board density). The tooltip surfaces them; the drawer lists the actual items.

## 3. Surface: `BoardCard` (M1.7a — `web/src/components/board/BeadCard.tsx`)

**Goal.** Scannable in peripheral vision. Answer: who owns it, how urgent, is it fresh, is anything unusual about it. ~80px tall max, fits ~10–15 per column on a 1440px screen.

### 3.1 Layout

```
┌────────────────────────────────────────────┐
│ ● gm-c7q       P0 · 2h                     │  ← row 1: state dot + id (mono) … priority chip · relative updated-at
│ DESIGN: Bead entity presentation — card…   │  ← row 2: title, clamp-2
│ [agent] quartz   fed:safe layer:ui +2      │  ← row 3: assignee pill + first 2 labels + overflow chip
│ 💬 🔗 🌿                                   │  ← row 4: glyph row (only shown glyphs)
└────────────────────────────────────────────┘
```

### 3.2 Hierarchy of prominence

1. **State dot + ID.** The state dot is the first pixel that tells you the lane; the ID in mono is what an operator copy-pastes into chat.
2. **Priority chip + relative updated-at.** Right-justified on row 1. Urgency and freshness are the two axes a human scans first.
3. **Title.** Two-line clamp (`line-clamp-2`), `text-sm font-medium`. Goes to `text-base` at `≥ lg` breakpoint.
4. **Assignee + labels.** Assignee pill first (who), then labels (what-kind). First 2 labels inline; overflow as `+N` chip with tooltip-on-hover listing the rest.
5. **Glyph row.** Only shown glyphs render; no reserved space for missing ones. If the whole row would be empty, the row collapses (card shrinks).

### 3.3 Truncation rules

| Field | Rule | Overflow affordance |
|---|---|---|
| `title` | `line-clamp-2` | ellipsis; full title available via tooltip + drawer |
| `labels` | first 2 visible | `+N` chip; tooltip lists all |
| `id` | never truncate | — |
| `status` | not shown (compressed to state dot) | — (available in tooltip + drawer) |
| `description` | not shown | glyph only |

### 3.4 Empty / degraded states

| Missing field | Rendering |
|---|---|
| `assignee == null` | render empty pill slot with muted `unassigned` label — reserve the space so cards align vertically |
| `labels` empty | omit row-3 labels entirely; assignee fills the row |
| `priority == null` | omit chip; relative updated-at shifts right |
| everything on glyph row empty | collapse row 4 |
| `title` empty | fall back to `id` in title slot (mono) |

**Explicit rule:** the card NEVER renders "—" or "n/a" text. It either shows a real value or omits the element. This keeps the card visually sparse; missing data is communicated by the presence/absence of chips, not by placeholder text.

### 3.5 Interaction

- Click anywhere on the card → open `BeadDrawer`.
- Hover (500ms) or keyboard focus → `BeadTooltip`.
- Labels and assignee pills are NOT individually clickable at M1.7 — filter UI is out of scope.

## 4. Surface: `BeadTooltip` (M1.7b — `web/src/components/board/BeadCardTooltip.tsx`)

**Goal.** The "one more step" view. Answer the questions the card couldn't fit without committing to a drawer open. ~320px wide, 5–10 rows tall.

### 4.1 Layout

```
┌─────────────────────────────────────────────┐
│ ● gm-c7q · started                          │  ← state dot + id + adaptor status word
│ DESIGN: Bead entity presentation —          │  ← full title, no clamp
│ card / tooltip / drawer (M1.7d)             │
│                                             │
│ P0 · updated 2h ago · type: decision        │  ← priority, updated-at, kind (all in one line)
│                                             │
│ assignee [agent] quartz · gemba/polecats    │  ← full assignee, full path
│ owner    [human] mike · gemba/crew          │
│                                             │
│ labels  fed:safe layer:ui milestone:m1      │  ← ALL labels; wraps
│         surface:frontend                    │
│                                             │
│ 💬 description · 🔗 3 evidence · 🌿 2 rel   │  ← glyph + count summary of what's in the drawer
└─────────────────────────────────────────────┘
```

### 4.2 What changes vs. the card

- **Title** uncramped (no line clamp).
- **Status** string rendered next to the state dot (so "In Review" surfaces even though lane is `started`).
- **Owner row** appears (hidden on the card for density).
- **All labels** — no overflow chip. The tooltip is transient and vertically flexible, so cost is low.
- **Glyph row gains counts.** `🔗 3 evidence` rather than bare `🔗`. This is the one place counts are useful — they tell the user whether the drawer is worth opening.
- **No description preview.** Descriptions are long, often multi-paragraph, and render poorly in a 320px tooltip. The `💬` glyph is enough of a hint.

### 4.3 Transience rules

- Appears on hover after 500ms (matches Radix default).
- Also appears on keyboard focus — accessibility hard requirement (see M1.7b DoD).
- Disappears on `mouseleave` with no delay.
- Escape key closes.
- Never shows `loading…` state — the tooltip reads from the same `WorkItem` the card already has. If the card exists, the data is local.

### 4.4 Degraded states

Same omission rule as the card: missing fields are omitted, not placeheld. Exceptions:
- `assignee` / `owner` rows always render; if null, show `unassigned` in muted tone. These two rows are the spine of the tooltip; their absence would feel like the tooltip rendered broken.
- `close_reason` — if the bead is `canceled` or `completed` and has `custom["beads:close_reason"]`, render it in a muted row at the bottom. This is the single most-asked question about a closed bead.

## 5. Surface: `BeadDrawer` (M1.7c — `web/src/components/board/BeadDrawer.tsx`, shipped gm-qai)

**Goal.** Show *every* field on the wire. Collapse empty sections rather than drop them, so missing data is still visible as a labeled "no evidence attached" row. Navigable via relationship clicks.

This surface is already built; the design lock here is: **do not regress its exhaustiveness**. Current section ordering is canonical:

1. Overview (status / state / type / priority chips; assignee; owner; labels)
2. Description (whitespace-preserved prose)
3. Close reason (if present — from `custom["beads:close_reason"]`)
4. Relationships (blocks / blocked by / parent / children / relates to / extension edges with `ext` badge)
5. Evidence (per-item rows with kind chip, source, ref, captured-at, summary)
6. Definition of Done (acceptance criteria bullets + notes + version)
7. Sprint & budget
8. Derived signals (`agent_claimable`, `human_action_required`, `review_pending`)
9. Timestamps (created / started / updated / closed)
10. Extension fields (grouped by adaptor namespace, e.g. `beads:*`; edges and reason are suppressed here to avoid duplication)

### 5.1 Truncation

- Relationships: no truncation. Render every edge as a clickable chip that pushes onto the drawer's nav stack.
- Evidence: no truncation. Scroll the drawer.
- Custom fields: values rendered as JSON in a monospace block; no truncation, scroll on overflow.
- Description: no truncation; `whitespace-pre-wrap` preserves prose formatting.

The drawer owns a nav stack; clicking a relationship pushes a new bead, `ArrowLeft` pops. Parent-initiated open (clicking a different card while drawer is open) resets the stack. This is already implemented.

### 5.2 Degraded states

Drawer sections render an empty-state line rather than being removed:
- No relationships: `No relationships.`
- No evidence: `No evidence attached.`
- No DoD: `No DoD declared.`
- No derived: `Not populated by the adaptor.`
- No sprint/budget: `No sprint or budget set.`

This is explicitly different from the card rule — on the drawer, presence of the labeled empty row is a feature, because the drawer's job is to be exhaustive.

## 6. Dark / light mode token table

All three surfaces share this table. Any deviation is a bug.

| Role | Light | Dark |
|---|---|---|
| surface (card bg) | `bg-white` | `bg-neutral-950` |
| surface border | `border-neutral-200` | `border-neutral-800` |
| body text | `text-neutral-900` | `text-neutral-100` |
| muted text | `text-neutral-500` | `text-neutral-400` |
| mono (IDs, labels) | `text-neutral-700` | `text-neutral-300` |
| hover bg (interactive) | `hover:bg-neutral-100` | `hover:bg-neutral-800` |
| focus ring | `focus-visible:ring-2 ring-blue-500` | `focus-visible:ring-blue-400` |

Priority / state / agent-kind color pairs are listed in their respective sections above and MUST be used verbatim — they are the points where color is semantic, not decorative.

## 7. Component contract appendix

### 7.1 `BeadCard`

```tsx
// web/src/components/board/BeadCard.tsx
export interface BeadCardProps {
  item: WorkItem;
  onOpen: (id: WorkItemID) => void;
  // density is reserved for a future "compact column" mode; default
  // "normal" matches the layout specified in §3.
  density?: 'normal' | 'compact';
}
```

Data shape consumed: full `WorkItem`. The card reads only these fields:

```
id, title, status (unused visually — reserved), state_category,
priority, assignee, labels, updated_at, derived?,
description?  (presence only, not content)
evidence?     (presence only, not content)
relationships? (presence only, parent detection)
dod?          (presence only)
```

### 7.2 `BeadTooltip`

```tsx
// web/src/components/board/BeadCardTooltip.tsx
export interface BeadTooltipProps {
  item: WorkItem;   // same item the card already has — no re-fetch
  children: React.ReactNode;   // the card is the trigger
  openDelayMs?: number;        // default 500
}
```

Built on `@radix-ui/react-tooltip`. No data-fetching. Reads:

```
id, title, status, state_category, priority, kind,
assignee, owner, labels (all), updated_at,
description? (presence), evidence? (count),
relationships? (count), dod? (presence),
custom["beads:close_reason"]? (if canceled/completed)
```

### 7.3 `BeadDrawer` (already shipped; documenting for completeness)

```tsx
// web/src/components/board/BeadDrawer.tsx
export interface BeadDrawerProps {
  openId: string | null;   // null keeps drawer closed
  onClose: () => void;
}
```

Data flow: `useBead(id)` → full `WorkItem` → every section above. Nav stack is internal state; relationship clicks push, `ArrowLeft` pops.

### 7.4 Shared primitives (extract into `components/board/primitives/` during M1.7a)

These tokens recur across all three surfaces. Extract before writing `BeadCard` so `BeadTooltip` (M1.7b) can reuse them:

- `PriorityChip({ priority: number | null })`
- `StateDot({ state: StateCategory })`
- `LabelChip({ label: string })` + `LabelList({ labels, max, overflow })`
- `AgentPill({ agent: AgentRef | null, compact?: boolean })`
- `GlyphRow({ item: WorkItem })`
- `RelativeTime({ iso: string })` — "2h ago" formatting

The drawer (`BeadDrawer.AgentPill`, `Chip`, `DerivedPill`) currently inlines these; a targeted refactor is allowed but NOT required for M1.7a — the drawer is shipped code, and we should only refactor it if the extracted primitives land first and the diff is mechanical.

## 8. Worked example — one WorkItem on all three surfaces

Representative `WorkItem` (fields chosen to exercise the vocabulary):

```json
{
  "id": "gm-c7q",
  "kind": "decision",
  "title": "DESIGN: Bead entity presentation — card / tooltip / drawer (M1.7d)",
  "description": "Decide how Gemba represents a single WorkItem…",
  "status": "in_progress",
  "state_category": "started",
  "priority": 0,
  "owner":    { "id": "gemba/crew/mike",   "name": "mike",   "agent_kind": "human" },
  "assignee": { "id": "gemba/polecats/quartz", "name": "quartz", "agent_kind": "agent",
                "role": "polecat", "dialect": "claude" },
  "labels": ["fed:safe", "layer:ui", "milestone:m1", "surface:frontend"],
  "relationships": [
    { "kind": "parent_child", "from": "gm-root.1", "to": "gm-c7q" }
  ],
  "evidence": [
    { "id": "e1", "kind": "url",    "source": "github", "ref": "…", "captured_at": "…" },
    { "id": "e2", "kind": "commit", "source": "git",    "ref": "…", "captured_at": "…" }
  ],
  "dod": { "acceptance_criteria": ["Doc committed", "Referenced by M1.7a/b/c"] },
  "derived": { "agent_claimable": false, "human_action_required": false, "review_pending": true },
  "created_at": "2026-04-22T…",
  "updated_at": "2026-04-23T11:30:00Z",
  "custom": { "beads:dependencies": [{ "issue_id": "gm-wisp-vkh7", "kind": "mol" }] }
}
```

### 8.1 BoardCard (compact)

```
┌───────────────────────────────────────────────┐
│ ● gm-c7q                           [P0] · 2h  │
│ DESIGN: Bead entity presentation — card /     │
│ tooltip / drawer (M1.7d)                      │
│ [agent] quartz   fed:safe layer:ui  +2        │
│ 💬 🔗 🌿 ↰ ☑                                 │
└───────────────────────────────────────────────┘
```

State dot is amber-filled (`started`). Priority chip is red (`P0`). Assignee pill is blue (`agent`). Glyphs: description present, 2 evidence, 1 relationship, has-parent, has DoD.

### 8.2 BeadTooltip (transient)

```
┌─────────────────────────────────────────────────┐
│ ● gm-c7q · in_progress                          │
│ DESIGN: Bead entity presentation —              │
│ card / tooltip / drawer (M1.7d)                 │
│                                                 │
│ [P0] · updated 2h ago · type: decision          │
│                                                 │
│ assignee  [agent] quartz · polecat · claude     │
│ owner     [human] mike · gemba/crew             │
│                                                 │
│ labels    fed:safe  layer:ui  milestone:m1      │
│           surface:frontend                      │
│                                                 │
│ 💬 description · 🔗 2 evidence · 🌿 1 rel · ☑  │
└─────────────────────────────────────────────────┘
```

All labels shown. Full assignee (role, dialect). Status word rendered. Glyph row now has counts.

### 8.3 BeadDrawer (complete)

```
┌───────────────────────────────────────────────────────────┐
│ ← DESIGN: Bead entity presentation — card / tooltip …   × │
│   gm-c7q  ⧉                                               │
├───────────────────────────────────────────────────────────┤
│ OVERVIEW                                                  │
│   [status=in_progress] [state=started] [type=decision] [P=0]│
│   ASSIGNEE  [agent] quartz · polecat · claude             │
│   OWNER     [human] mike                                  │
│   LABELS    fed:safe  layer:ui  milestone:m1  surface:…   │
│                                                           │
│ DESCRIPTION                                               │
│   Decide how Gemba represents a single WorkItem…          │
│                                                           │
│ RELATIONSHIPS                                             │
│   parent      [gm-root.1]                                 │
│   extension   [gm-wisp-vkh7  mol  ext]                    │
│                                                           │
│ EVIDENCE                                                  │
│   [url]    github    …   2h ago                           │
│   [commit] git       …   3h ago                           │
│                                                           │
│ DEFINITION OF DONE                                        │
│   • Doc committed                                         │
│   • Referenced by M1.7a/b/c                               │
│                                                           │
│ SPRINT & BUDGET     No sprint or budget set.              │
│                                                           │
│ DERIVED SIGNALS                                           │
│   ○ agent-claimable  ○ human-action-required  ● review-…  │
│                                                           │
│ TIMESTAMPS                                                │
│   Created  2026-04-22T…    Started  —                     │
│   Updated  2026-04-23T…    Closed   —                     │
│                                                           │
│ EXTENSION FIELDS                                          │
│   BEADS                                                   │
│     (any custom beads:* fields not already surfaced)      │
└───────────────────────────────────────────────────────────┘
```

Sections with no data still render their header with an empty-state line (Sprint & budget above). Relationships include an `ext` badge on edges that came from `custom["beads:*"]` rather than core `Relationship[]`.

## 9. Ratification

This design doc is self-ratified per gm-c7q DoD ("reviewed by PM persona — or self-ratified with an explicit DD"). The DD resolved here is **bead-entity-visual-vocabulary**: the board card, hover tooltip, and drill-in drawer share a single visual token set (priority color, state glyph, agent-kind color, label chip style, glyph row), and the three surfaces form a progressive-zoom (card → tooltip → drawer) rather than three independent views.

**Out of scope** (future design beads):
- Filter / search UI atop the board
- Non-board surfaces (backlog list, graph, sprint panel)
- Per-label color registry (`risk:high` in red, etc.)
- Markdown rendering for description (currently whitespace-preserved prose)
- Drag-to-move between columns
