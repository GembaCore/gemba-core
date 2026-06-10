// Package codeanalysis is the core typed surface for Gemba's
// agentic code-analysis capability (gm-l1i, design
// docs/design/code-analysis.md).
//
// A [Provider] is a READ-ONLY, pluggable knowledge-graph adapter
// that answers structural questions about one or more workspace
// repos: "what calls this symbol?", "what's the blast radius of
// changing this file?", "which modules are untested?". GitNexus
// is the reference backend; Sourcegraph, CodeQL, and tree-sitter
// / LSP indexers are anticipated adapters.
//
// This package owns the boundary between configuration on disk
// and runtime backends:
//
//   - [Provider] is the interface every backend implements.
//   - [Manifest], [RepoRef], [Module], [HealthReport],
//     [ImpactReport], and [ReindexFlags] are the shared value
//     types passed across the interface.
//   - [LoadConfig] / [Config] parse `.gemba/code_analysis.toml`
//     into a typed bundle of repos and backend settings.
//   - [Register] / [Resolve] are the backend-name-keyed registry
//     callers reach for at startup. The `"gitnexus"` slot is
//     wired here with a placeholder factory; the real adapter
//     ships in gm-56z.
//
// What this package does NOT own:
//
//   - Any concrete backend implementation. The `gitnexus` factory
//     installed by this package returns [ErrNotImplemented] until
//     gm-56z replaces it.
//   - The four context providers
//     (`code_analysis_summary` / `symbol_context` /
//     `impact_analysis` / `health_report`) — those land in
//     gm-bro and live under `internal/core/promptctx`.
//   - Reindex policy execution. [Config] accepts the four
//     policy strings (`post_merge`, `scheduled`, `manual`,
//     `on_demand`) but does not run them; that's gm-x7n.
//   - The HTTP / MCP API surface. Wiring lives elsewhere.
//
// Stability: the [Provider] interface, [Config] schema, and
// registered backend name keys are part of Gemba's public typed
// surface. Adding methods to [Provider] is a breaking change for
// every backend; add capability flags to [Manifest] instead and
// gate optional methods behind those.
package codeanalysis
