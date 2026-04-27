// Package sources contains the live wiring between Gemba's data
// planes (OrchestrationPlane today; Witness, Refinery, Beads-degraded,
// Budget, Adaptor-degraded later) and the gm-rfy walk agenda
// aggregator.
//
// The sibling package internal/walk defines the consumer-side
// interfaces (EscalationLister, HITLLister, GateFailureLister,
// WorkItemLister); this package implements them against concrete
// adaptors. Keeping the live wiring here — rather than inside
// internal/walk proper — preserves the gm-rfy package's "no
// dependencies on transport / server / event hub" property: the
// aggregator stays unit-testable with fakes alone, and the live
// edge can grow without churning the core.
//
// # Sources covered today (gm-3jv)
//
//   - OrchestrationPlane.ListOpenEscalations — covers every
//     escalation flavour that already converges on the canonical
//     core.EscalationRequest shape: polecat / crew / mayor escalations
//     (raised via the OrchestrationEvent escalation.* taxonomy),
//     gt escalate output, MCP elicitations, A2A input-required,
//     permission prompts, HITL approvals, orchestrator pauses, and
//     the Manager-skill blocker / question kinds (gm-97w7.1). All
//     reach the walk via OrchestrationPlaneEscalationLister.
//
//   - OrchestrationPlane.Subscribe → events hub bridge — turns
//     escalation.* OrchestrationEvents into walk.escalation_changed
//     events on the gemba events.Hub so an active walk's UI can
//     refresh its agenda live. See StartOrchestrationBridge.
//
// # Sources not yet wired (TODO follow-ups)
//
// The design (docs/design/gemba-walk.md §Escalation-source
// aggregation) names nine flavours; the four below do not yet emit
// canonical OrchestrationEvent escalations and need upstream work
// before this package can normalize them. Each has a stub or a
// comment pointing at the missing wiring.
//
//   - Witness findings — gm-witness pipeline does not currently
//     publish escalation.raised events; findings land via mail. A
//     bridge from witness mail → core.EscalationRequest is the
//     follow-up. Tracked via a follow-up bead filed alongside
//     gm-3jv (see commit notes).
//
//   - Refinery merge rejections — the refinery rig's reject path
//     emits a beads comment but no orchestration event. Wiring is
//     a follow-up; the canonical shape would be an EscalationKind
//     "refinery_rejection" raised through the OrchestrationPlane.
//
//   - Beads daemon degraded states — gm-b1 surfaces degraded
//     adaptor health via /api/adaptors today; the walk agenda
//     wants the same signal as a synthetic escalation. Wire when
//     the adaptor health bus gains an event delivery mode.
//
//   - Sprint TokenBudget thresholds (budget.warn / budget.stop) —
//     these already emit GembaEvents on the events.Hub. The bridge
//     here could be extended to translate a budget.warn frame into
//     a synthetic walk EscalationRequest. Deferred until the
//     budget engine ships threshold-stable IDs (so we don't surface
//     duplicates on every recalc).
//
//   - Adaptor-degraded events — gm-root.7 health-bus already feeds
//     /api/adaptors/stream; same translation question as budgets.
//
// # Package layout rationale
//
// Putting the live wiring under internal/walk/sources/ keeps the
// import graph one-way: server → walk/sources → walk → core. The
// walk package never imports events / transport / server, so
// running BuildAgenda in a CLI tool, a test, or a future RPC
// surface stays cheap.
package sources
