// Package planner is the home of the two-axis work planning and
// dispatch subsystem (gm-s47n, docs/design/work-planning.md).
//
// This package will eventually own:
//
//   - Layer 1 read API (OperationalContext) — gm-s47n.2.5
//   - Layer 3 scorers (Conflicts, Affinity)  — gm-s47n.4
//   - Layer 4 session-health telemetry        — gm-s47n.5
//   - Layer 5 planner UX entry points         — gm-s47n.6
//   - Source analysis scheduling              — gm-s47n.9
//
// gm-s47n.2.1 ships the data shapes only — no behavior. Concrete
// triggers and decay are split across gm-s47n.2.{2,3,4,5}.
//
// # Why a single package
//
// The planner reads from existing core structs (AgentRef, Session,
// Workspace, Assignment in internal/core/orchestration.go), the new
// session_profiles table defined here, and the SourceAnalysis
// abstraction in internal/sourceanalysis. None of those packages
// should depend on planner — the dependency arrow points one way:
// planner → core / sourceanalysis. Keeps the planner replaceable
// without rippling through the data layer.
package planner
