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

## Cross-process mutations — the post-write hook (gm-e4.3.3)

`bd` mutations made *outside* the running `gemba serve` process — a
terminal `bd close gm-foo`, an ops runbook, a second editor pane —
don't flow through the in-process `core.WorkPlaneEmitter`, so /events
SSE subscribers wouldn't see them without help. The fix is a small
post-write hook installed alongside `bd` that POSTs the changed bead's
id at `/api/workitems/notify` (gm-e4.3.2). Gemba re-reads the bead
through its bound WorkPlane and publishes a normal `workitem.*`
GembaEvent — same kind, same envelope, same SSE path as in-process
mutations.

### Wire shape

The hook is a dumb trigger; the body is intentionally minimal.

```http
POST /api/workitems/notify
Content-Type: application/json
Authorization: Bearer <token>     # only if --auth=token is on
{
  "work_item_id": "gemba/gemba/gm-foo",
  "source":       "bd-git-hook"     # optional — echoed onto the event payload
}
```

Gemba ignores any other state in the body — the canonical bead is
re-read from the WorkPlane on every notify, so a stale or maliciously
crafted body cannot poison the cache. The handler returns `200 OK`
with `{work_item_id, kind, skipped?}` so the hook can log what landed.

### Idempotency

The handler dedupes on `(work_item_id, UpdatedAt)`. A duplicate POST
for the same bead at the same `UpdatedAt` returns `200 OK` with
`skipped: true` and does NOT republish. This means a hook-side retry
loop is safe; a re-run after a real mutation still publishes (the
`UpdatedAt` advances).

The dedup window is bounded (FIFO, 1024 entries by default). On
overflow the oldest id evicts and the next notify for that id will
re-emit — false-negative on the dedup, never a false-positive on the
event. Operators don't need to tune this.

### Installing the hook

> **Status (2026-04-25)**: the hook itself ships in the upstream `beads`
> repo (gm-e4.3.3 upstream PR — the gemba-side bead is for docs and a
> skipped integration test). When that PR lands, the install path is
> `bd hooks install --gemba-url ... --gemba-auth ...`; the section
> below describes the contract the hook is required to honour.

The hook is invoked by `bd` after every successful write that touches
a bead's `UpdatedAt`. It reads two values from its environment:

| Variable             | Required | Meaning                                              |
| -------------------- | -------- | ---------------------------------------------------- |
| `GEMBA_NOTIFY_URL`   | yes      | Base URL of `gemba serve`, e.g. `http://localhost:7666`. |
| `GEMBA_NOTIFY_AUTH`  | only when `--auth=token` | Bearer token mounted on every request. |

Without `GEMBA_NOTIFY_URL` set, the hook is a no-op — the same `bd`
binary works on a machine that doesn't run gemba locally.

### Fail-open contract

A hook-side error (gemba is down, network drops, auth misconfigured)
**MUST NOT** fail the underlying `bd` mutation. The hook logs to
stderr and exits 0. This is non-negotiable: the operator's terminal
write is authoritative, and the SSE pump is a convenience. A loud
warning on stderr is the upper bound on user-visible breakage.

### Verifying the round trip

In one terminal, watch the SSE stream:

```bash
curl -N http://localhost:7666/events?topics=workitem.*
```

In a second terminal, mutate a bead:

```bash
bd close gm-foo
```

Within ~250ms the first terminal should print a `workitem.closed` (or
`workitem.updated`) event with the bead's canonical id. If nothing
arrives:

* Check `GEMBA_NOTIFY_URL` is set in the second terminal's env.
* Check `gemba serve` is running on that URL.
* Run `bd hooks list` to confirm the gemba hook is installed.
* Tail `gemba serve`'s log — auth failures and adaptor-degraded errors
  surface there.

The post-write hook is the only piece of cross-process plumbing
between `bd` and `gemba`. Anything more sophisticated (Dolt binlog
tail, polling shim) would replace this hook, not stack on top of it.
