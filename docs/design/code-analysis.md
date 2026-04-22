# Agentic Code Analysis: `CodeAnalysisProvider` as a first-class capability

**Status:** design · tracked in `gm-l1i`
**Author:** mike (captured by polecat jasper)
**Date:** 2026-04-22 (ratification pending)
**Parents:** `gm-root` (Gemba EPIC), `gm-eiw` (Context providers)
**Related:** `gm-371` (bootstrap), `gm-gq2` (workspace-repo), `gm-57b` (Personas)
**Resolves DD:** DD-33

## Summary

Promote **agentic code analysis** — GitNexus-class knowledge-graph tools that
answer "what calls this?", "what's the impact of changing this?", "what's
untested?" — to a first-class, typed Gemba capability.

Today Gemba reaches for GitNexus ad-hoc when Coaches and Managers need
structural code knowledge the bead-graph and `git log` alone don't provide.
Ad-hoc use means inconsistent queries, no caching, no pluggability, and no
persona-level opt-in.

This design introduces `CodeAnalysisProvider` — a typed, READ-ONLY, pluggable
context provider that exposes a knowledge-graph API over one or more workspace
repos. GitNexus is the reference implementation; Sourcegraph, CodeQL,
tree-sitter / LSP indexers are anticipated adapters.

## Motivation

Every Manager and every Coach benefits from structural code knowledge:

- **PM** — estimating epic decomposition needs module boundaries;
  parallel-safety checks need file-space signatures.
- **Architect** — design review wants the real call graph and module cohesion
  metrics, not a summary someone typed once.
- **Code Reviewer** — PR weight should track how load-bearing the touched code
  is (call-graph fan-in).
- **QA** — regression-surface scans need "what tests cover which code"
  traceability.
- **Documentarian** — documentation-coverage checks need to know which public
  APIs lack docs.
- **Security** — auth-surface audits need reachability analysis from entry
  points to sensitive primitives.
- **Bootstrap** (`gm-371`) — analyzing a source-code repo to derive candidate
  epic decomposition requires more than README + dir-listing; knowledge-graph
  richness produces better first-draft plans.

Without a typed surface, each persona would reach for GitNexus ad-hoc and
produce inconsistent, un-cacheable queries.

## The primitive

`CodeAnalysisProvider` is a typed context provider — it composes with the
`ContextProvider` registry (`gm-eiw`) so personas opt in via their `.toml`
config. It is backend-pluggable: GitNexus is the reference, but any
knowledge-graph backend that can answer the interface is acceptable.

### Interface (Go)

```go
type CodeAnalysisProvider interface {
    Describe() CodeAnalysisManifest

    // Indexing
    IsIndexed(ctx CallCtx, repo RepoRef) (bool, time.Time, error)
    Reindex(ctx CallCtx, repo RepoRef, flags ReindexFlags) error

    // Structure queries (sync or streamed)
    Context(ctx CallCtx, symbol string) (SymbolContext, error)
    Query(ctx CallCtx, q GraphQuery) ([]QueryResult, error)
    Impact(ctx CallCtx, symbol string, direction ImpactDirection) (ImpactReport, error)
    RouteMap(ctx CallCtx, from, to string) ([]Route, error)
    ToolMap(ctx CallCtx, toolName string) (ToolMap, error)

    // Higher-level summaries useful to personas
    ModuleInventory(ctx CallCtx, repo RepoRef) ([]Module, error)
    EntryPoints(ctx CallCtx, repo RepoRef) ([]EntryPoint, error)
    HealthIndicators(ctx CallCtx, repo RepoRef) (HealthReport, error)
}

type CodeAnalysisManifest struct {
    Backend               string   // "gitnexus" | "sourcegraph" | "codeql" | custom
    IndexedRepos          []RepoRef
    SupportsSymbolContext bool
    SupportsImpact        bool
    SupportsRouteMap      bool
    SupportsEmbeddings    bool
    MaxRepoSize           int64
}

type RepoRef struct {
    Name   string
    Path   string // local path
    Remote string
}

type ModuleInventory struct {
    Name         string
    Path         string
    LOC          int
    FileCount    int
    TestCoverage float64 // 0..1 if available
    Dependencies []string
}

type HealthReport struct {
    CycleCount       int
    UntestedModules  []string
    GodClasses       []SymbolRef // high-fan-in types
    DeepFlows        []FlowRef   // long cross-module flows
    UndocumentedAPIs []SymbolRef
    TODOComments     []LocationRef
    StaleCode        []SymbolRef // last-touched > 6mo, 0 test coverage
}
```

### Configuration

Per-workspace in `.gemba/code_analysis.toml`:

```toml
backend = "gitnexus"

[repos.gemba]
path = "./"
reindex_policy = "post_merge"

[repos.gemba_prime]
path = "../gemba_prime"
reindex_policy = "post_merge"

[backend.gitnexus]
embeddings = false
max_repo_size_mb = 500
```

Multiple repos per workspace is first-class — it composes with the
workspace-repo model (`gm-gq2`): a workspace typically has both the workspace
repo and the output repo indexed.

## Integration with other primitives

### Context providers (`gm-eiw`)

`CodeAnalysisProvider` exposes four new context provider IDs, separately
toggleable per persona:

| ID | Purpose | Default consumers |
|---|---|---|
| `code_analysis_summary` | Module inventory + health top-line | PM |
| `symbol_context` | Callers / callees / enclosing module / related beads | Architect, Code Reviewer (on-demand) |
| `impact_analysis` | Cascade of changing a symbol | Architect, Code Reviewer |
| `health_report` | Cycles, untested modules, god-classes, undocumented APIs | QA, Documentarian |

`gm-eiw` is amended to list these four in its seed catalog (see
`internal/core/promptctx/provider.go`).

### PM Skills

- `epic_decompose` (agentic) — uses `module_inventory` to propose epic splits
  aligned with module boundaries.
- `parallel_safety` — uses `impact_analysis` over member-WorkItems' anticipated
  file touches to detect overlaps.
- `change_this` — uses `impact_analysis` to estimate cascade breadth.

### Architect Skills

- `design_review` — uses the full graph for coupling / cohesion / cycle checks.
- `api_shape_review` — uses `tool_map` / `impact_analysis` to check API-surface
  stability.

### QA Skills

- `health_scan` — reads `health_report` directly.
- `regression_impact_analysis` — uses `impact_analysis` to identify affected
  tests.

### Documentarian

- `document_scan` consumes `UndocumentedAPIs` from `health_report`.
- `update_summary` uses `module_inventory` to keep the project summary honest.

### Bootstrap (`gm-371`)

- Source-code-import calls `ModuleInventory` + `EntryPoints` +
  `HealthIndicators` to produce a rich first-draft epic decomposition.
- Without a `CodeAnalysisProvider`, source-code-import falls back to README +
  `git log` heuristics.

## Re-indexing policy

| Policy | Trigger |
|---|---|
| `post_merge` (default) | Re-index after every merge to main (matches existing GitNexus PostToolUse hook pattern) |
| `scheduled` | Nightly re-index |
| `manual` | Explicit `gemba code-analysis reindex` command |
| `on_demand` | Persona-triggered (a PM skill that wants fresh analysis fires a reindex first) |

Stale indexes surface as warnings in persona responses:
*"analysis from 12h ago; reindex recommended before relying on
`health_report`."*

## API surface

| Path | Verb | Purpose |
|---|---|---|
| `/api/v1/code-analysis/manifest` | GET | Backend + configured repos + capabilities |
| `/api/v1/code-analysis/index` | POST | Trigger reindex |
| `/api/v1/code-analysis/context` | POST | Symbol context query |
| `/api/v1/code-analysis/query` | POST | Graph query (Cypher-like for GitNexus; backend-specific) |
| `/api/v1/code-analysis/impact` | POST | Impact analysis |
| `/api/v1/code-analysis/health` | GET | Health report for a repo |
| `/api/v1/code-analysis/modules` | GET | Module inventory |

Exposed via an MCP-style server so agent polecats (not just personas) can query
the graph — polecat work benefits from graph access too.

## Architectural invariants

- **Pluggable, not forced.** A workspace without GitNexus (or equivalent) still
  works; personas that opt in to code-analysis context get empty results with
  `warning: no code-analysis backend configured`.
- **Deterministic queries cache.** Graph queries are content-addressed against
  the index commit; repeat queries return cached results until a reindex fires.
- **Privacy-aware.** The provider runs LOCAL by default. Optional remote
  backends require explicit opt-in + per-repo allow-list.
- **No write authority.** `CodeAnalysisProvider` is READ-ONLY. Personas consume
  it; they don't mutate it.

## Follow-up beads

| Bead | Scope |
|---|---|
| `gm-code-analysis-core` | Interface + registry + configuration |
| `gm-gitnexus-adapter` | GitNexus reference backend (wraps `mcp__gitnexus__*`) |
| `gm-code-analysis-context-providers` | Four context providers integrated with `gm-eiw` |
| `gm-code-analysis-reindex-policies` | The four policies + hook integration |
| `gm-code-analysis-bootstrap-integration` | Source-code-import path (ties `gm-371`) |
| Optional | `gm-sourcegraph-adapter`, `gm-codeql-adapter` |

## Definition of Done

- Design ratified by mike.
- This document committed.
- Follow-up beads filed (see above).
- `gm-eiw` amended to include the four new context provider IDs.
- `gm-371` (bootstrap) pointer to this design exists.

## Not in scope

- Implementation.
- Running the knowledge graph in-process (always an out-of-process backend
  call).
- Authoring a custom indexer (only the pluggable adapter shape is defined
  here).
