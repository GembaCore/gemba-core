// Package walk implements the Gemba walk core (gm-rfy, design
// docs/design/gemba-walk.md).
//
// A "Gemba walk" is an interactive multi-topic decisioning
// conversation between the operator and the Project Manager
// persona. The walk gathers an agenda from five workspace
// sources (open escalations, suspended HITL sessions, open gate
// failures, recently-filed beads, recently-closed beads) and
// proceeds one item at a time. Each item produces a
// WalkDecision the operator ratifies / modifies / rejects /
// defers / hands off.
//
// This package owns:
//
//   - Core domain types: Walk, AgendaItem, WalkTurn,
//     WalkDecision and their enums.
//   - The agenda aggregator that pulls from the five sources,
//     deduplicates, ranks, and caps the result.
//   - A Store interface for walk persistence + an in-memory
//     implementation. SQL persistence lands when the Manager
//     HITL session store does (gm-uf7) — this package's Store
//     interface is the integration point.
//   - A ResumeContext builder so a paused walk's resume turn
//     can rehydrate the persona with a deterministic synopsis.
//   - Context-based walk_id propagation so any ExecutedAction
//     emitted under an active walk can stamp walk_id provenance
//     without threading it through every signature.
//
// Wiring (HTTP API + SSE, persona prompt extension, UI) lives
// in gm-wgv / gm-4p6 / gm-i65 / gm-77u — this package stays
// transport-free.
package walk
