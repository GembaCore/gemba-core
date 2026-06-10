// Package phase implements the project-level Phase primitive (gm-jt9,
// design docs/design/persona-pppp.md §3 + invariants #29–30).
//
// A Phase is a workspace-level mode that determines which Purviews are
// active. The project is in exactly one Phase at a time. Phases form
// a transitionable graph; each transition emits a `phase.transitioned`
// GembaEvent on the events hub.
//
// This package owns:
//
//   - The typed Phase enum (Discovery/Design/Build/Validate/Ship/
//     Commercialize) — concretely: ideation, design, building,
//     validating, shipping, commercialization.
//   - The transition graph + Advance handler that enforces it.
//   - WorkspaceState (current Phase + transition history) + a Store
//     interface for persistence (in-memory implementation now,
//     SQL-backed implementation later).
//   - Events emission helpers that fan a transition out through the
//     shared events.Hub so SSE clients and the /metrics aggregator see
//     one canonical signal.
//
// Out of scope for this bead:
//
//   - Binding Phase to the Manager mutation path (Purview-as-gate) —
//     gm-3on consumes WorkspaceState.Phase to gate mutations.
//   - Seeding persona TOMLs with concrete Phase values — gm-1w7.
//   - HTTP API surface — wired in internal/server/phase.go which lives
//     in the server package per Gemba's transport-free core convention.
package phase
