// Package sourceanalysis defines the abstract code-analysis surface
// the planner consumes when computing semantic conflicts (gm-s47n.3,
// docs/design/work-planning.md §4 Layer 2).
//
// The interface is intentionally small: four methods cover everything
// the planner needs to detect that two beads with disjoint file
// targets nonetheless invalidate each other's API contracts.
//
// Implementations
//
//   - GitNexus: the rich one — uses the gitnexus index (gm-s47n.3.3).
//   - Noop:     graceful-degradation default — every query returns
//     an empty slice with [ErrUnavailable] in Describe so
//     callers can decide whether to skip semantic conflict
//     detection or surface a warning (gm-s47n.3.2).
//
// The planner MUST handle either implementation transparently. When
// source analysis is unavailable, target-glob conflict detection
// still works; semantic-conflict detection is silently skipped per
// docs/design/work-planning.md §5.3, with the skip logged so an
// operator can see why two beads got dispatched in parallel that
// later turned out to conflict.
//
// # Wire-shape stability
//
// Target / Symbol / Diff are wire shapes: they cross the planner ↔
// analysis boundary and may eventually cross a process boundary if a
// future implementation runs out-of-band. Keep them JSON-friendly
// (no interface-typed fields, no unexported state). Adding fields is
// fine; renaming or repurposing existing fields is a breaking change.
package sourceanalysis
