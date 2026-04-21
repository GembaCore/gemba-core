# WorkPlane adaptor contract

> Source: `internal/core/workplane.go`. Resolves DDs 9, 12, 14, 15.

Every Gemba deployment binds exactly one `WorkPlane` adaptor to one
`OrchestrationPlane` adaptor (gm-root DD-1). This document is the
author-facing description of the WorkPlane half of that contract.

## What a WorkPlane is

A WorkPlane is the adaptor-agnostic face of a **work tracker**: Beads,
Jira, GitHub Issues, Linear, whatever the operator runs. The interface
exposes a read / mutate surface over work items, plus two optional
feature groups (sprints and token budgets). The core UI and the
transport layer only ever call these methods — they never touch the
backend's private storage (gm-root DD-9).

## The interface

```go
type WorkPlane interface {
    Describe(ctx context.Context) (CapabilityManifest, error)

    ListWorkItems(ctx context.Context, filter WorkItemFilter) ([]WorkItem, error)
    GetWorkItem(ctx context.Context, id WorkItemID) (WorkItem, error)
    CreateWorkItem(ctx context.Context, wi WorkItem) (WorkItem, error)
    UpdateWorkItem(ctx context.Context, id WorkItemID, patch WorkItemPatch) (WorkItem, error)
    AttachEvidence(ctx context.Context, id WorkItemID, ev Evidence) error

    ListSprints(ctx context.Context) ([]Sprint, error)
    ReadBudgetRollup(ctx context.Context, sprintID string) (BudgetRollup, error)
}
```

### Method groups

1. **Describe** — declarative capability advertisement. Idempotent,
   side-effect-free, called on startup and on every reconnect.
2. **Work item CRUD** — the main query / mutation surface. All
   mutations MUST go through the backend's **public** CLI or API
   (gm-root DD-9). Writing the backend's private store directly is a
   conformance failure.
3. **Sprint + budget** — optional. Adaptors that set
   `SprintNative = false` or `TokenBudgetEnforced = false` MAY return
   `ErrUnsupported` from the corresponding method and the UI will hide
   the relevant chrome.

### Sentinel errors

| Sentinel         | When to return it                                                              |
| ---------------- | ------------------------------------------------------------------------------ |
| `ErrNotFound`    | Lookup id does not exist in the backend. Wrap with `%w` for detail.            |
| `ErrUnsupported` | Caller asked for a feature group the manifest opts out of. UI hides the widget. |

Adaptors MAY wrap the sentinels with `fmt.Errorf("...: %w", ErrNotFound)`;
`errors.Is` must continue to match.

## The CapabilityManifest

```go
type CapabilityManifest struct {
    AdaptorName     string
    AdaptorVersion  string
    ProtocolVersion string // core contract version (gm-e3.4 negotiation)

    Transport       Transport // api | jsonl | mcp (gm-root DD-12)
    StateMap        StateMap  // native status -> five core buckets

    EdgeExtensions         []EdgeExtension         // non-core relationship kinds
    FieldExtensions        []FieldExtension        // non-core WorkItem.Custom fields
    RelationshipExtensions []RelationshipExtension // per-edge metadata fields

    SprintNative              bool
    TokenBudgetEnforced       bool
    EvidenceSynthesisRequired bool
}
```

The manifest is the **single source of truth** the capability-
negotiation UI consults before rendering adaptor-specific controls
(gm-e11.4, gm-root DD-15). Controls for unsupported capabilities are
**hidden**, not merely disabled, so the operator never sees a button
they cannot use.

### Transport (DD-12)

Exactly one of `api | jsonl | mcp`. Multi-transport adaptors are out of
scope for v1. An adaptor MUST ship with the same `ProtocolVersion` the
core advertises at boot; mismatches fail fast with an actionable error.

### StateMap

A required declarative map from the adaptor's native status tokens
(`"open"`, `"in_progress"`, `"To Do"`, `"hooked"`, …) to the five core
`StateCategory` buckets (`backlog | unstarted | started | completed | canceled`).
Every native status the adaptor can emit MUST appear as a key; gaps are
a conformance failure. Core never guesses: this keeps Kanban lane
placement deterministic and keeps backend-specific vocabulary out of
the SPA (gm-root DD-4).

### Extension slots

| Slot                     | Purpose                                                                              |
| ------------------------ | ------------------------------------------------------------------------------------ |
| `EdgeExtensions`         | Non-core relationship kinds (beyond `blocks | parent_child | relates_to`, DD-9).     |
| `FieldExtensions`        | Non-core fields the adaptor emits on `WorkItem.Custom`.                              |
| `RelationshipExtensions` | Per-edge metadata (Jira link categories, beads edge confidence, LangGraph contracts). |

The SPA only renders extensions from
`web/src/extensions/<adaptor-id>/` (gm-root DD-4). Anything an adaptor
declares here that lacks a registered renderer falls back to
`relates_to` semantics.

### Capability booleans

| Flag                        | Meaning                                                                                                      |
| --------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `SprintNative`              | Adaptor emits first-class `Sprint` records. When false, `ListSprints` MAY return empty and UI hides sprint chrome. |
| `TokenBudgetEnforced`       | Adaptor carries real three-tier `inform/warn/stop` enforcement (gm-root DD-14). When false, the stop tier is cosmetic. |
| `EvidenceSynthesisRequired` | Core must synthesize `Evidence` from transport artifacts (gm-root DD-13). When false, the adaptor supplies its own. |

## Mutation model (DD-9)

Mutations are carried by `WorkItemPatch`. Zero values mean "do not
touch"; adaptors translate the patch to their backend's public API.
Status and StateCategory travel together: if both are set they must be
consistent with the adaptor's `StateMap`.

Mutation requests reach the WorkPlane only after the transport layer
verifies the `X-GEMBA-Confirm` nonce (gm-root DD-7); adaptors can assume
that check has already fired.

### Event emission is mandatory (DD-12 / Foolery-spike lesson)

Every state-changing WorkPlane call — `CreateWorkItem`, `UpdateWorkItem`,
`AttachEvidence`, and all the future mutation methods (`transition`,
`claim`, `unclaim`, `close`, `link`, `unlink`, sprint mutations) — **MUST**
produce a matching `WorkPlaneEvent` visible on `Subscribe` within the
adaptor's declared latency budget (default 250ms for SSE/push, 5s for
poll). A mutation that returns success but emits no event is a **hard
conformance failure** (§2.6 Group D `mutation_without_event_is_failure`).

This is a **MUST**, not a SHOULD. Declaring `event_delivery: "poll"`
controls the adaptor's *internal* fetch strategy; it does not permit the
adaptor to push snapshot-diffing onto the UI. Poll-mode adaptors MUST
queue and emit events on `Subscribe` after each poll tick. The UI's 500ms
state-freshness bar (gm-e12.2 DoD) is unmeetable if state updates require
client-side polling, which is precisely the failure mode the Foolery
spike uncovered (docs/prior-art/foolery.md).

## Sprint + TokenBudget (DD-14)

- `ListSprints` returns whatever sprints the backend declares today.
  Noop for non-sprint backends.
- `ReadBudgetRollup` returns a `BudgetRollup` scoped to one sprint,
  including `by_work_item` breakdown and the derived
  `TokenBudget.Tier()` at read time.
- Three-tier enforcement (`inform | warn | stop`) is a **core**
  construct: the adaptor just reports consumption; the UI and the
  orchestration side decide what "stop" means.

## Version negotiation (DD-12 / gm-e3.4)

`ProtocolVersion` is compared against the core's advertised
`core_version` at startup. The transport layer (gm-e3.4) surfaces
mismatches as an actionable error before the adaptor is ever asked for
a manifest. Adaptor authors should bump `AdaptorVersion` on their own
cadence and `ProtocolVersion` only when the core contract changes.

## Authoring checklist

- [ ] `Describe` returns a manifest that passes `CapabilityManifest.Validate`.
- [ ] `StateMap` covers every native status the backend can emit.
- [ ] Every mutation path calls the backend's **public** CLI or API.
- [ ] Every mutation path emits a matching `WorkPlaneEvent` on `Subscribe`
      within the declared latency budget (**MUST** — conformance Group D
      `mutation_without_event_is_failure`).
- [ ] `ErrNotFound` and `ErrUnsupported` are returned in the right places.
- [ ] Extension renderers live under `web/src/extensions/<adaptor-id>/`.
- [ ] Manifest round-trips through JSON unchanged (covered by the
      conformance harness, gm-e3.5).

## Reference implementations

- `internal/adapter/bd/` — Beads WorkPlane (gm-e6).
- Forthcoming: Jira WorkPlane (gm-e8) as the non-Beads forcing function.
- `internal/adapter/noop/` — in-memory adaptor used by the conformance
  harness (gm-e3.7).
