# Beads WorkPlane adaptor — mapping notes

> Source: `internal/adapter/bd/`. Adaptor id: `beads`.

This document describes how the Beads (`bd`) CLI is projected onto the
`core.WorkPlane` interface. For the contract itself, see
[`workplane.md`](workplane.md). This note focuses on the mapping rules
that matter when reading or debugging the adaptor.

## Status → StateCategory

Declared in `types.go` (`beadsStateMap`). Covers every status `bd` can
emit today. A bead whose status is outside the map is placed in
`backlog` and the port logs a validation warning; the adaptor-startup
`CapabilityManifest.Validate()` call fails fast if a known bd token is
missing.

| `bd` status   | `core.StateCategory` |
| ------------- | -------------------- |
| `open`        | `unstarted`          |
| `in_progress` | `started`            |
| `hooked`      | `started`            |
| `pinned`      | `started`            |
| `blocked`     | `started`            |
| `deferred`    | `backlog`            |
| `closed`      | `completed`          |

## WorkItemID encoding (DD-6)

`bd` ids are bare (e.g. `gm-abc`). The adaptor prefixes them with the
configured workspace chunk (default `gemba/gemba`) when projecting onto
`core.WorkItemID`, and strips the prefix before handing a native id back
to `bd`. `nativeID` accepts any `/`-separated path whose last segment is
the `bd` id so conformance runs that mint ids as
`gemba/gemba/gm-...` under every adaptor keep working.

## Agent federation (gm-e6.3 / DD-1)

`bd` has a single scalar `assignee` field and a flat label bag. The
adaptor synthesizes a richer `core.AgentRef` by combining `assignee`
(the id) with convention-named labels (the federated fields).

### Read path — label → AgentRef

| Label                 | `AgentRef` field | Notes                                                |
| --------------------- | ---------------- | ---------------------------------------------------- |
| `agent:role:<role>`   | `Role`           | Free-form string. `polecat`, `witness`, `crew`, etc. |
| `agent:parent:<id>`   | `ParentID`       | Expected to be a workspace-qualified `AgentID`.      |

A bead with an `assignee` and no federated labels still yields a valid
`AgentRef` — `Role` stays blank and `ParentID` stays nil. A bead with no
`assignee` yields a nil `AgentRef`, regardless of what labels are
present. Synthesis is permissive; missing metadata is never fabricated.

`AgentKind` is always `agent`. Beads's `assignee` slot is used
exclusively by automated actors in the gemba ecosystem; humans live in
the `owner` field (projected separately as `AgentKindHuman`). If a
future Beads convention lets a human claim a bead, the extension point
is an `agent:kind:*` label — not in scope for gm-e6.3.

### Write path — AgentRef → label

`CreateWorkItem` and `UpdateWorkItem` rewrite the `agent:role:*` /
`agent:parent:*` portion of the label set from `wi.Assignee`. The
`AgentRef` is **authoritative** for those two keys:

* any `agent:role:*` or `agent:parent:*` value in the caller-supplied
  `Labels` slice is stripped first, then the `AgentRef`'s values are
  added. Callers cannot accidentally ship an `AgentRef` that contradicts
  its own label encoding.
* non-agent labels pass through unchanged.

Update has two shapes for label writes:

1. `patch.Labels` populated → `--set-labels` is used, meaning the
   caller's slice (with agent labels merged in) fully replaces the
   bead's label set. Use this when you want a clean state.
2. `patch.Labels` empty, `patch.Assignee` carries federated metadata →
   `--add-label` is used (additive). An assignee re-claim must not
   silently erase every other label on the bead, so the adaptor never
   converts an "assignee-only" patch into a `--set-labels` write.

Consequence: mode (2) can leave stale `agent:role:*` / `agent:parent:*`
labels behind when the previous assignee's values differ. Callers that
care about stale-label hygiene should pass an explicit `Labels` slice
(mode 1). The adaptor documents this rather than paying a bd round-trip
to read existing labels on every assignee-only patch.

### Round-trip guarantee

Setting `Assignee = {ID, Role, ParentID}` on a WorkItem, pushing it
through `UpdateWorkItem`, and reading it back via `GetWorkItem` produces
an identical `AgentRef`. Tests live in
`internal/adapter/bd/agents_test.go` — that file is the executable
contract for this section.

## Extension channel (gm-e6.1)

`core.WorkItem.Custom` carries Beads-specific fields that have no core
counterpart under the `beads:` namespace:

| Key                    | Source          | Purpose                                                      |
| ---------------------- | --------------- | ------------------------------------------------------------ |
| `beads:issue_type`     | `issue_type`    | `task | feature | bug | decision | epic | chore | molecule | event`. |
| `beads:notes`          | `notes`         | Free-text field `bd` writes independently of `description`.  |
| `beads:parent`         | `parent`        | Parent bead id for hierarchical children.                    |
| `beads:created_by`     | `created_by`    | Actor string for the initial create.                         |
| `beads:started_at`     | `started_at`    | Lifecycle timestamp.                                         |
| `beads:dependencies`   | `dependencies`  | Raw native-edge rows (`gm-e6.2` decodes the mapped subset).  |
| `beads:dependents`     | `dependents`    | Raw native-edge rows in the inverse direction.               |

The SPA renders these under `web/src/extensions/beads/`. Adding a new
extension field is a two-step change: declare it in
`beadsManifest.FieldExtensions` and surface it in the renderer.

## Mutation boundary (DD-9)

All writes shell out to the public `bd` CLI. The adaptor never touches
`.beads/*.db` directly — that store's shape changes across `bd`
versions, and the CLI is the stable contract. This rule is asserted by
conformance group F.
