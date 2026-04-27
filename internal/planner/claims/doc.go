// Package claims implements the per-rig owner-claim cross-check
// (gm-v5z2.6, work-planning.md §4 Layer 5.1 inputs + §4 Layer 5.2
// gate 2).
//
// Multi-agent fleet coordination: when session A is already
// working bead X, no other session should be able to pick X.
// The Index is the in-memory lookup Selection consults for this
// gate; Build hydrates it from a snapshot of currently-claimed
// beads (typically a query against session_assignments), and
// Set/Clear keep it warm in response to session-start /
// session-end events.
//
// Stale claims (a session ended without releasing) are handled by
// a Stale() / Clear() pass the orchestration plane runs on a
// timer.
//
// Pure data: no I/O. Concurrency-safe: every method holds the
// embedded RWMutex. Selection reads in the dispatch hot path; the
// orchestration plane writes on relatively rare lifecycle events.
package claims
