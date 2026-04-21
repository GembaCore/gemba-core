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

## Capability enforcement AT THE PORT (gm-4qf / DD-12)

The `CapabilityManifest` is the **contract** the adaptor publishes.
Core — not each adaptor — is the authoritative gate: the port-level
guard consults the manifest **before** routing a call to the adaptor.
Adaptors MUST ALSO fail-fast on any undeclared op as defense in depth.

The t3code audit found four adapters for the same `sessionModelSwitch`
capability, each with a subtly different enforcement behavior. Central
gating replaces that drift with a single pure function:

```go
d := core.CheckCapability(manifest, core.OpListSprints)
if !d.Allowed {
    return nil, core.EnforceCapability(manifest, core.OpListSprints)
    // → *AdaptorError{Kind: KindCapabilityDenied, Retryable: false,
    //                  Message: d.Reason, Detail: {...}}
}
```

### Gated operations

| Operation           | Manifest field(s) that gate it                            |
| ------------------- | --------------------------------------------------------- |
| `attach_evidence`   | `evidence_synthesis_required` — when false, core does not call AttachEvidence; the adaptor supplies its own Evidence through GetWorkItem. |
| `list_sprints`      | `sprint_native` — when false, no sprints exist to list.    |
| `read_budget_rollup`| `sprint_native` AND `token_budget_enforced` — rollups are per-sprint and require a real budget. |

`describe`, `list_work_items`, `get_work_item`, `create_work_item`, and
`update_work_item` are **always** allowed — they are the unconditional
CRUD surface every WorkPlane must support.

### The guard is pure

`core.CheckCapability(m, op)` returns `{allowed, reason}` with no I/O
and no state. Callers may invoke it on every request; the cost is
bounded by a single `switch` on `Operation`.

### Core-side wrapper

`core.GuardedWorkPlane(inner, manifest)` wraps any `WorkPlane` with the
port-level guard. Gated methods return `capability_denied` without
reaching `inner` when the manifest opts out; unconditional methods
pass through untouched. Production wiring MUST route every adaptor
through this (or an equivalent check) — the bare `inner` should never
be reachable from a mutation handler.

### Adaptor-side fail-fast (defense in depth)

Even with the port-level guard, adaptors MUST raise
`capability_denied` on a call to an op their manifest excludes. The
two checks MUST agree; a disagreement is a conformance failure. Reason:
an adaptor registered out-of-band, a stale cached manifest, or a bug
in the wrapper chain could bypass the port check; the adaptor is the
last line.

### Conformance Group E

`core.AssertCapabilityDenied(err)` is the Group E acceptance helper.
Conformance suites call the adaptor with an undeclared op and run the
observed error through `AssertCapabilityDenied`; anything other than a
tagged `capability_denied` AdaptorError fails the group.

**Legacy `ErrUnsupported` is insufficient.** It maps to
`KindUnsupported`, which is reserved for coarse adaptor-level opt-out;
the guard uses `capability_denied` so the SPA can distinguish "manifest
said no" from "adaptor chose not to".

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

## Error algebra (gm-faz — Conformance Group F)

Every error an adaptor surfaces from a boundary method MUST be an
`*core.AdaptorError` (or wrap one such that `errors.As` can find it).
The t3code spike showed that string-matching `err.Error()` to decide
retry vs fail is the single biggest source of adaptor rot; the tagged
envelope replaces that with structured fields the runtime can branch on.

Wire shape:

```json
{
  "_kind":     "rate_limited",
  "retryable": true,
  "message":   "provider throttled session sess-42",
  "cause":     "http 429",
  "detail":    {"retry_after_seconds": 30}
}
```

Kinds — exactly nine, closed set:

| Kind | Retryable default | Use when |
|---|---|---|
| `validation` | no | Input failed schema / precondition (bad `WorkItemPatch`, illegal transition). |
| `session_not_found` | no | Referenced session / assignment / work item id doesn't exist. |
| `session_closed` | no | Session reached a terminal state. |
| `request_failed` | **yes** | Transport-level failure (connection refused, 5xx, timeout). |
| `process_failed` | no | Structured failure from the backend process (bd exit 2, LG node raise). |
| `rate_limited` | **yes** | Provider throttling (HTTP 429, quota). Populate `detail.retry_after_seconds` when known. |
| `unsupported` | no | Manifest opted out (e.g. `ReadBudgetRollup` on a non-budget adaptor). |
| `capability_denied` | no | Actor lacks authority — permission prompt denied, cross-agent write. |
| `adaptor_degraded` | **yes** | Backend transiently unhealthy (Dolt hung, supervisor restarting). Surfaces to the gm-b1 SPA banner. |

Construction:

```go
return nil, core.NewAdaptorError(core.KindValidation,
    "StateMap missing native status %q", native)

return nil, core.WrapAdaptorError(core.KindRequestFailed, err,
    "bd describe failed")
```

Callers (gm-b1 mutation gate, orchestration retry loops) MUST branch on
`core.AsAdaptorError(err)` and `.Retryable` — never on `err.Error()`.
Legacy `errors.Is(err, core.ErrNotFound)` / `core.ErrUnsupported` keeps
working: `AdaptorError.Is` maps kinds to sentinels transparently.

The conformance harness's `core.AssertAdaptorError` (Group F) fails any
adaptor that returns a bare `errors.New()` or `fmt.Errorf` without a
tagged envelope. Run it against every non-nil error observed from a
boundary call before accepting a new adaptor.

## Boundary obligations — decode lives at the transport (gm-io4)

> Resolves DD-12 + DD-15 per the t3code audit.

**Adaptors MUST trust their inputs.** Every WorkPlane method receives
typed, validated values because the transport layer has already decoded
the wire payload and rejected every structurally-invalid request with
`KindValidation`. Re-validating the same invariants inside an adaptor is
the drift the t3code spike flagged: per-adaptor validation diverges over
time, and defense-in-depth becomes offense-through-depth the moment one
of the layers skips a check.

Boundary decoders live in `internal/transport/schemas.go` with thin
per-transport wrappers in:

- `internal/transport/api/schemas.go`  (HTTP + JSON, chi handlers)
- `internal/transport/jsonl/schemas.go` (newline-delimited JSON frames)
- `internal/transport/mcp/schemas.go`  (MCP tool-call arguments)

The decoder contract:

1. Unknown JSON fields are rejected — silent drop of an unrecognised key
   is a bug in the caller, not a feature.
2. Required fields enforce presence at decode time
   (`create_work_item.item.title`, `end_session.nonce`, …).
3. Enum-valued fields (`state_category`, `session_end_mode`,
   `evidence.kind`, `preferred_kind`, `resolution.kind`) are checked
   against the canonical set and surface a `code: "enum"` issue when
   invalid.
4. Server-assigned fields (e.g. `WorkItem.ID`, `WorkItem.CreatedAt`)
   cannot be supplied on create; the decoder rejects them with
   `code: "server_assigned"`.

Validation failures surface as the tagged error below — transport hosts
map this straight onto the wire response (HTTP 400, MCP `isError=true`,
jsonl error frame) without involving the adaptor:

```json
{
  "_kind":     "validation",
  "retryable": false,
  "message":   "create_work_item.item.kind: missing required field",
  "detail":    {
    "issue": {
      "path":   "create_work_item.item.kind",
      "reason": "missing required field",
      "code":   "required"
    }
  }
}
```

`core.NewValidationError(core.ValidationIssue{…})` is the one canonical
constructor; `core.ValidationIssueOf(err)` extracts the typed issue for
UI consumption. Conformance Group F's boundary check,
`core.AssertBoundaryValidation`, fails any decoder that accepts
malformed input, emits an untagged error, uses the wrong kind, or drops
the structured issue.

**Adaptor authors do not re-decode.** `WorkPlane.CreateWorkItem` receives
a `core.WorkItem` whose required fields have been validated. If your
backend has *additional* invariants beyond the core contract (e.g. Jira
requires `project_key` in `Custom`), enforce those in the adaptor with
`NewAdaptorError(KindValidation, …)` — but never re-run the shared
checks the boundary already performed.

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
- [ ] Every gated op (attach_evidence, list_sprints, read_budget_rollup)
      fails fast with `capability_denied` when the manifest opts out
      (**MUST** — conformance Group E via
      `core.AssertCapabilityDenied`). The port-level
      `core.GuardedWorkPlane` is the primary gate; the adaptor-side
      check is defense in depth.
- [ ] Every boundary error is an `*core.AdaptorError` with a valid
      `_kind` + explicit `retryable` (conformance Group F via
      `core.AssertAdaptorError`). Legacy `ErrNotFound`/`ErrUnsupported`
      still match via `errors.Is` once the kind is set correctly.
- [ ] The adaptor does NOT re-validate input already checked at the
      boundary — `CreateWorkItem`, `UpdateWorkItem`, `AttachEvidence`
      trust their typed arguments (**MUST** — gm-io4). Adaptor-specific
      invariants surface as `KindValidation` with a per-adaptor path;
      cross-cutting shape checks belong in `internal/transport/schemas.go`.
- [ ] Extension renderers live under `web/src/extensions/<adaptor-id>/`.
- [ ] Manifest round-trips through JSON unchanged (covered by the
      conformance harness, gm-e3.5).

## Conformance harness (gm-2am)

The contract tests ship as an importable Go package so third-party
adaptor authors can run them in their own CI:

```go
import gembatesting "github.com/MikeBengtson/gemba/testing"

func TestYourAdaptorConformance(t *testing.T) {
    impl := youradaptor.New(...)
    gembatesting.RunWorkPlaneConformance(t, impl, &gembatesting.WorkPlaneFixture{
        KnownMissingID: core.WorkItemID("your-workspace/your-repo/does-not-exist"),
    })
}
```

See `testing/README.md` for the full probe catalogue and fixture
contract. The import path is the canonical entry point referenced in
the "Writing a Gemba adaptor" guide (gm-e14.5).

## Reference implementations

- `internal/adapter/bd/` — Beads WorkPlane (gm-e6).
- Forthcoming: Jira WorkPlane (gm-e8) as the non-Beads forcing function.
- `internal/adapter/noop/` — in-memory adaptor that exercises the
  exported harness (see `internal/adapter/noop/conformance_test.go`).
