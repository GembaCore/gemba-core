// Package scanscheduler owns scheduling re-indexing of the source
// analysis tool (gm-s47n.9, work-planning.md §8).
//
// Re-indexing is a first-order planner concern: a stale index produces
// silently wrong dependent sets, which produces silently missed
// semantic conflicts, which produces parallel-dispatched beads that
// turn out to collide. The planner is the only component in the
// system that sees merge waves, parallel completions, wall-clock
// drift, and pre-dispatch demand — only it can schedule scans well.
// This package wraps that scheduling.
//
// The package is split along the five gm-s47n.9.* sub-beads:
//
//   - triggers.go  (.9.1) Pure trigger evaluation: post-merge wave,
//     parallel-completion barrier, wall-clock floor,
//     drift signal from the source analysis backend
//     itself.
//   - demand.go    (.9.2) Synchronous pre-dispatch demand check —
//     does the planner have to block on a scan before
//     computing the next conflict graph?
//   - scheduler.go (.9.3) Cooldown, coalescing, in-flight tracking,
//     manual override (RunNow / PauseAutoTriggers).
//   - activity.go  (.9.4) ScanActivity record so a scan participates
//     in the workspace-conflict graph and lands in the
//     same activity log as dispatch + retrospective.
//   - grading.go   (.9.5) Trigger-grading from retrospective signals:
//     when a semantic conflict was missed, which trigger
//     should have fired and was it suppressed?
//
// Decoupling: the scheduler talks to source analysis through the
// narrow [Rescanner] interface in this package, not through the
// full sourceanalysis.SourceAnalysis surface. That keeps the
// scheduler unit-testable with a fake and lets a non-gitnexus
// backend plug in without dragging the full analysis API along.
package scanscheduler
