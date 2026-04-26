// Package concepts owns the controlled-vocabulary half of the
// gm-s47n two-axis planner (see docs/design/work-planning.md §6).
//
// The package is self-contained: it does NOT depend on
// internal/core's WorkItem schema. Beads are surfaced through a small
// [BeadConceptStore] interface so the package compiles and tests run
// before gm-s47n.1.1 lands the WorkItem.concepts field.
//
// Three interlocking surfaces:
//
//   - [Bootstrap] runs N pluggable [BootstrapSource]s in parallel,
//     unions the candidates, normalizes, dedupes, and caps. Ships
//     three sources: Go packages, route prefixes, fixture taxonomy.
//   - [DetectDrift] reads bead concept usage and emits suggestions
//     for near-duplicate merges, drifter follow-ups, and singleton
//     deletes. Idempotent, pure, never mutates state.
//   - [ReviewQueue] persists suggestions and operator decisions; an
//     approval drives historical rewrites via [ApplyMerge] /
//     [ApplyRename] / [ApplyDelete] over the [BeadConceptStore].
//
// Storage lives in <workspace>/.gemba/concepts/.
package concepts
