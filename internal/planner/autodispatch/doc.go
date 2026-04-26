// Package autodispatch implements the auto-dispatch daemon
// (gm-s47n.6.3, work-planning.md §4 Layer 5.2).
//
// The daemon polls for idle sessions, picks the highest-affinity
// non-conflicting bead from the ready set, and slings the pick
// through the orchestration plane. Every gate the spec requires —
// per-rig kill switch, per-session rate limit, fleet-wide concurrency
// cap, recycle-before-take rules — is honoured before the dispatch
// is recorded.
//
// Design:
//   - Tick is the unit. One pass over (idle sessions × ready set)
//     producing zero or more dispatch actions. Pure-ish: I/O is
//     pushed to injected interfaces so the loop is exhaustively
//     testable with fakes.
//   - Run is the production wrapper around Tick — a context-aware
//     loop with a configurable period.
//   - Fairness boost (gm-s47n.6.4) lands as a separate pre-scoring
//     hook on the same Daemon struct. This bead ships the loop
//     skeleton without it.
package autodispatch
