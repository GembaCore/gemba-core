// Package retro implements the turn-retrospective comparator
// (gm-s47n.8.1, work-planning.md §7).
//
// After a bead lands, the retrospective compares **declared** values
// (what the bead authoring step recorded as targets/concepts) against
// **actual** values (the file list from the merged diff + concepts
// inferred from the diff via source analysis). The comparator is a
// pure function — no I/O, no SourceAnalysis call. Callers are
// responsible for materialising both sides.
//
// This package is the foundational primitive for the rest of the
// turn-retrospective epic:
//
//   - gm-s47n.8.2 stores comparator output in scorer_grades.
//   - gm-s47n.8.3 wires the close hook that gathers actuals + writes
//     the corrected truth back to the bead and session profile.
//   - gm-s47n.8.4 surfaces queryable views over the recorded grades.
//
// A high target divergence flags one of: a buggy extraction prompt,
// missing concepts in the controlled vocabulary, an under-scoped
// bead, or scope creep during execution. The comparator only
// reports the divergence — diagnosing which case applies is human
// review (§7.4).
package retro
