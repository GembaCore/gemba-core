// Package promptctx defines ContextProvider — the typed primitive that
// gives each Persona (gm-57b) its opt-in view of the workspace: project
// summary, decisions log, issues log, bead DBs, recent commits, CI
// status, open escalations, sprint posture.
//
// Design (gm-eiw / DD-24):
//
//   - Each Persona's config declares which providers it uses, separately
//     from its prompt and output-validation policy.
//   - Providers implement Refresh(ctx, opts) — results are cached by TTL.
//     The Cache wrapper tracks freshness; every invocation records a
//     FetchedAt timestamp so the audit log can flag stale reads.
//   - Providers live in two places. Generic shims (StaticProvider,
//     FuncProvider) are offered under internal/core/promptctx/providers
//     for the trivial cases and for tests. Domain-specific providers
//     (project_summary, decisions_log, bead_database, …) take injectable
//     source interfaces so core never hard-wires paths to git, beads,
//     or CI.
//   - PromptEnvelope (separate bead) consumes the declared providers for
//     a persona and assembles the final prompt.
//
// Package naming: the bead's deliverable nominates
// "internal/core/context/", but the directory is named "promptctx" to
// avoid colliding with the stdlib "context" package at import sites.
// The exported surface matches the bead's intent.
package promptctx
