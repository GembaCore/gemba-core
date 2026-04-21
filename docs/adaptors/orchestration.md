# OrchestrationPlane adaptor

The OrchestrationPlane is Gemba's adaptor-agnostic contract with an
agent runtime — the thing that *runs* agents (Gas Town, LangGraph,
CrewAI, OpenHands, Devin, Factory, Gas City, …). This document is the
authoring reference for anyone implementing that contract in Go.

It matches `internal/core/orchestration.go` and the design in
`gemba_prime/crew/mike/domain.md` §3.

---

## Scope

The OrchestrationPlane owns everything about a *live session*: who is
running right now, in what workspace, burning how much cost, waiting on
which human. It does **not** own the work item itself — that is the
WorkPlane's jurisdiction (gm-root DD-1). When the two disagree on
anything in the work-item record (status, assignee, labels) the
WorkPlane wins and the OrchestrationPlane reconciles.

Gemba pairs exactly one WorkPlane adaptor with exactly one
OrchestrationPlane adaptor per deployment (README, gm-root DD-1).

---

## Capability manifest

Every adaptor returns an `OrchestrationCapabilityManifest` from
`Describe()`. Six axes are **required** — the UI drops controls that
aren't declared here, so mis-declaring will make features invisible:

| Field | Purpose |
|---|---|
| `transport` | `api`, `jsonl`, or `mcp`. The wire protocol for this adaptor (gm-e3.4 negotiates version on top). MCP is recommended for new adaptors but not required. |
| `workspace_kinds` | Which `WorkspaceKind`s the adaptor can acquire. Every kind MUST guarantee `fs_scoped: true` (gm-root DD-5). |
| `group_modes` | Which of `static`, `pool`, `graph` the adaptor uses to present agent groups (gm-root DD-7). |
| `cost_axes` | Which of `tokens`, `wallclock`, `dollars_native` the adaptor emits samples against (gm-root DD-4). At least one MUST be declared; Gemba will synthesize `dollars_est` by aggregation. |
| `escalation_kinds` | Which `EscalationKind` sources this adaptor raises from (gm-root DD-6). The UI wires inbox categories off this. |
| `peek_modes` | Which `PeekMode`s `PeekSession` supports (`transcript`, `screenshot`, `structured`). |

Plus the standard supporting fields:

- `adaptor_id`, `adaptor_version`, `orchestration_api_version`
- `default_workspace_kind`, `per_kind_isolation`
- `assignment_strategies` — `push`, `pull` (canonical), `hook`
- `native_cost_unit`, `native_cost_to_dollars` — for adaptors that
  meter in proprietary units (e.g. Devin ACUs)
- `event_delivery` — `sse`, `push`, or `poll`
- `extension` — adaptor-private escape hatch

### Declaring the manifest

```go
func (a *MyAdaptor) Describe() core.OrchestrationCapabilityManifest {
    return core.OrchestrationCapabilityManifest{
        AdaptorID:               "myrig",
        AdaptorVersion:          "1.0.0",
        OrchestrationAPIVersion: "1.0",
        Transport:               core.TransportJSONL,
        WorkspaceKinds:          []core.WorkspaceKind{core.WorkspaceWorktree},
        DefaultWorkspaceKind:    core.WorkspaceWorktree,
        PerKindIsolation: map[core.WorkspaceKind]core.IsolationCapabilities{
            core.WorkspaceWorktree: {FSScoped: true},
        },
        GroupModes:           []core.GroupMode{core.GroupStatic, core.GroupPool},
        AssignmentStrategies: []core.AssignmentStrategy{core.StrategyPull, core.StrategyHook},
        CostAxes:             []core.CostAxis{core.CostWallclock, core.CostTokens},
        EscalationKinds:      []core.EscalationKind{core.EscalationPermissionPrompt},
        PeekModes:            []core.PeekMode{core.PeekTranscript},
        EventDelivery:        core.EventDeliverySSE,
    }
}
```

Be honest. A manifest that claims `snapshot_restore: true` but doesn't
actually snapshot will be caught by the conformance suite (§3.8).

---

## Interface

```go
type OrchestrationPlaneAdaptor interface {
    Describe() OrchestrationCapabilityManifest

    // Desired-vs-actual (gm-root §Novel §8)
    DeclaredState(ctx context.Context) (WorkspaceTopology, error)
    ObservedState(ctx context.Context) (WorkspaceTopology, error)

    // Agents
    ListAgents(ctx, AgentFilter) ([]AgentRef, error)
    ReadAgent(ctx, AgentID) (*AgentRef, error)

    // Groups
    ListGroups(ctx) ([]AgentGroup, error)
    ResolveGroupMembers(ctx, groupID string) ([]AgentRef, error)

    // Assignment lifecycle
    ClaimNextReady(ctx, ReadyFilter, claimant AgentRef) (*Reservation, error)
    ReleaseReservation(ctx, reservationID string) error
    StartSession(ctx, assignmentID string, prompt SessionPrompt) (Session, error)
    PauseSession(ctx, sessionID string, nonce ConfirmNonce) (Session, error)
    ResumeSession(ctx, sessionID string, nonce ConfirmNonce) (Session, error)
    EndSession(ctx, sessionID string, mode SessionEndMode, nonce ConfirmNonce) (Session, error)
    PeekSession(ctx, sessionID string) (SessionPeek, error)

    // Workspaces
    AcquireWorkspace(ctx, WorkspaceRequest) (Workspace, error)
    ReleaseWorkspace(ctx, workspaceID string) error
    InspectWorkspace(ctx, workspaceID string) (Workspace, error)

    // Escalations
    ListOpenEscalations(ctx, EscalationFilter) ([]EscalationRequest, error)
    ResolveEscalation(ctx, escalationID string, r EscalationResolution, nonce ConfirmNonce) (EscalationRequest, error)

    // Events
    Subscribe(ctx, SubscribeFilter) (<-chan OrchestrationEvent, error)
}
```

Implementations MUST be safe for concurrent use — Gemba calls them
from the HTTP handler pool.

### Desired-vs-actual

`DeclaredState` returns the topology the adaptor's configuration *asks
for* (Gas City `city.toml`, Gas Town `gastown.toml`, LangGraph's
static graph). `ObservedState` returns what the adaptor actually sees
running. Gemba diffs these to surface drift in the Agents dashboard
and capability UI.

For pure-runtime adaptors without any declared form, return an empty
topology with only `CapturedAt` set — don't fabricate a declaration.

### Assignment protocol (canonical: pull)

1. `ClaimNextReady(filter, claimant)` atomically reserves the next
   ready work item and returns a `Reservation` with a TTL.
2. Gemba calls `WorkPlane.Claim(workItemID, agentID, nonce)` to flip
   the work item to "started" on the tracker.
3. On success: Gemba creates an `Assignment`, calls
   `AcquireWorkspace`, then `StartSession`.
4. On failure at step 2: Gemba calls `ReleaseReservation` and retries
   with the next candidate.

`push` and `hook` are supported alternatives; adaptors declare which.

### Session lifecycle + nonces

Pause, resume, and end are idempotent under a `ConfirmNonce`. The same
nonce passed twice MUST be a no-op (conformance B.4). Different nonces
may return different results.

`EndSession` takes a `SessionEndMode` (`completed`, `failed`,
`canceled`) — the adaptor records this in the session's final
`Status`.

### Workspaces

`AcquireWorkspace` picks the weakest supported kind that satisfies the
caller's `required_isolation`. **Never silently downgrade.** If no
kind satisfies the ask, return an error; the caller decides whether to
refuse the assignment or relax the requirements.

`fs_scoped: true` is non-negotiable for every workspace kind (gm-root
DD-5). The conformance suite (§3.8 Group C) actively probes this.

### Escalations

Any adaptor source that needs a human answer maps onto a single
`EscalationRequest` shape (gm-root DD-6): MCP elicitation, A2A
input-required, permission prompts, HITL approvals, orchestrator
pauses. The source sets `Source`; the urgency determines whether the
session is suspended (`blocking`) or merely badged (`advisory`).

`ResolveEscalation` MUST unblock the associated session for blocking
escalations that resolve to `approve` or `modify`.

### Events

`Subscribe` returns a channel that closes when `ctx` is cancelled or
the transport disconnects. Event kinds include:

- `session_transition` — payload: `{before, after}`
- `cost_sample` — payload: the `CostSample`
- `escalation_opened` / `escalation_resolved` / `escalation_expired`
- `potential_conflict` — two assignments touching overlapping files
- `workspace_acquired` / `workspace_released`
- `reservation_claimed` / `reservation_released`

Event ordering MUST be causal within a single assignment (conformance
E.3).

#### Event emission is mandatory (DD-12 / Foolery-spike lesson)

Every state-changing OrchestrationPlane call — `ClaimNextReady`,
`ReleaseReservation`, `StartSession`, `PauseSession`, `ResumeSession`,
`EndSession`, `AcquireWorkspace`, `ReleaseWorkspace`, and
`ResolveEscalation` — **MUST** emit a matching `OrchestrationEvent`
visible on `Subscribe` within the adaptor's declared latency budget
(default 250ms for SSE/push, 5s for poll). Successful mutation with no
event is a **hard conformance failure** (§3.8 Group E
`mutation_without_event_is_failure`).

This is a **MUST**, not a SHOULD. `event_delivery: "poll"` controls the
adaptor's internal fetch strategy only; poll-mode adaptors MUST still
queue and emit events on `Subscribe`. The UI's 500ms freshness bar
(gm-e12.2 DoD) cannot be met when state updates require client-side
polling — the exact failure mode surfaced by the Foolery spike
(docs/prior-art/foolery.md: SSE for terminal streams, polled beats).

---

## Error algebra (gm-faz — Conformance Group F)

Every non-nil error returned from an OrchestrationPlaneAdaptor boundary
method MUST be an `*core.AdaptorError` (or wrap one). See
[workplane.md §Error algebra](./workplane.md#error-algebra-gm-faz--conformance-group-f)
for the full kind table, wire shape, and constructors — the contract is
identical across both planes.

Orchestration-specific guidance:

- `StartSession` / `PauseSession` / `ResumeSession` / `EndSession` on an
  unknown session id → `KindSessionNotFound`.
- `PauseSession` / `ResumeSession` on a terminal session → `KindSessionClosed`.
- `AcquireWorkspace` that cannot satisfy `required_isolation` →
  `KindUnsupported` (this is a manifest-declared limit, not a transient
  failure) or `KindCapabilityDenied` when isolation was withheld by
  policy.
- Any call during an `adaptor_degraded` window (agent-runtime supervisor
  restarting, provider reachability lost) → `KindAdaptorDegraded` so the
  gm-b1 banner surfaces verbatim.

Retry loops in the orchestrator MUST consult `core.IsRetryable(err)` —
never string-match on the message.

---

## Minimum conformance (domain.md §3.8)

The full conformance suite lands with `gm-e3.8`, but adaptors are
expected to pass at minimum:

| Group | Probe | What it proves |
|---|---|---|
| A | `list_agents_returns_declared_capabilities` | Manifest + runtime agree. |
| B | `claim_next_ready_reserves` | Two concurrent claims don't double-book. |
| B | `end_session_idempotent` | Same nonce twice = no-op. |
| C | `acquire_fs_scoped_honored` | Writes don't leak across workspaces. |
| C | `required_isolation_honored` | Manifest is not a lie. |
| D | `resolve_escalation_unblocks_session` | Blocking escalation → running. |
| E | `event_ordering_across_assignment` | Causal order preserved. |
| F | `every_boundary_error_is_tagged` | `core.AssertAdaptorError` passes on every non-nil error from a boundary call. |

---

## Design decisions resolved by this contract

- **DD-5** — Workspace/isolation: one required invariant (`fs_scoped`),
  the rest declared per-kind.
- **DD-7** — Grouping: three modes (`static`, `pool`, `graph`) with a
  single `AgentGroup` shape.
- **DD-10** — Adaptor-agnostic shapes for cross-plane records
  (`Assignment`, `Session`, `Workspace`).
- **DD-12** — Capability manifest drives UI affordances, not hardcoded
  role vocabulary.

---

## Related

- `internal/core/orchestration.go` — the Go source of truth.
- `internal/core/types.go` — `AgentRef`, `WorkItemID`, `AgentID`.
- `docs/adaptors/workplane.md` — the paired WorkPlane contract
  (gm-e3.2).
- `gemba_prime/crew/mike/domain.md` §3 — full design.
