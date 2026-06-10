// Package codeanalysis is the GitNexus reference backend for the
// codeanalysis.Provider interface defined in
// internal/core/codeanalysis (gm-1ak). It implements the live
// adapter that gm-56z slots into the registry's reserved
// "gitnexus" placeholder.
//
// The adapter shells out to the local `gitnexus` CLI — the same
// channel internal/sourceanalysis/gitnexus.go uses for the
// planner's semantic-conflict scans. Choosing the CLI channel
// (over the MCP RPC or a future HTTP API) keeps the adapter
// dependency-free, stateless across calls, and testable via a
// simple exec hook.
//
// Surface mapping:
//
//   - Manifest    → `gitnexus list` (which repos are indexed)
//   - ListRepos   → `gitnexus list`
//   - Reindex     → `gitnexus analyze [--embeddings] [--full]`
//   - Modules     → `gitnexus cypher` over Module / Cluster nodes
//   - Health      → `gitnexus cypher` over the graph + .gitnexus
//     meta.json freshness
//   - Impact      → `gitnexus cypher` walking incoming/outgoing
//     edges from the named symbol
//
// The package's init() calls codeanalysis.Replace("gitnexus", ...)
// so a blank import (`_ "…/internal/adapter/gitnexus/codeanalysis"`)
// from `cmd/gemba` or `internal/cli/serve.go` is enough to swap
// the placeholder factory for the live adapter at binary boot.
//
// Capability flags reflect what the CLI actually supports today:
// SupportsImpact and SupportsSymbolContext are advertised true;
// SupportsRouteMap and SupportsEmbeddings stay conservative
// (false) until the planner has a use for them — the adapter
// would happily fulfill them but the wider system gates on the
// flag.
package codeanalysis
