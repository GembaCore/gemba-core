// Package providers offers concrete ContextProvider implementations
// (gm-eiw). They split into two groups:
//
//  1. Generic shims — StaticProvider, FuncProvider — useful in tests
//     and for the trivial cases where the "provider" is just a blob
//     of text or an inline closure.
//  2. Domain providers — ProjectSummaryProvider, DecisionsLogProvider,
//     BeadDatabaseProvider — take injectable source interfaces so core
//     never hard-wires paths to git, beads, or CI. The interfaces are
//     declared here; concrete source adapters live elsewhere (beads
//     adaptor, git adaptor, …) and get wired in at server boot.
//
// The remaining seed providers (project_state, issues_log,
// recent_commits, ci_status, active_escalations, sprint_context) are
// intentionally left for follow-up beads whose data sources land
// first.
package providers
