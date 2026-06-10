// Provider interface + supporting value types (gm-1ak,
// design docs/design/code-analysis.md §The primitive).

package codeanalysis

import (
	"context"
	"errors"
	"time"
)

// ErrNotImplemented is returned by registered factory placeholders
// when a backend has been reserved by name but not yet shipped.
// In particular the `"gitnexus"` slot returns this until gm-56z
// installs the real adapter.
//
// Callers MUST treat this as a clean "backend present but not
// usable yet" signal — distinct from "unknown backend" (see
// [ErrUnknownBackend]) which means the registry has no slot for
// the requested name at all.
var ErrNotImplemented = errors.New("codeanalysis: backend not implemented")

// ErrUnknownBackend is returned by [Resolve] when the requested
// name was never registered. Callers usually surface this as a
// configuration error: a `.gemba/code_analysis.toml` referencing
// a backend the binary doesn't know about.
var ErrUnknownBackend = errors.New("codeanalysis: unknown backend")

// ReindexPolicy names how a repo's index is refreshed. The four
// canonical values mirror the design's table; unrecognized values
// are rejected at config-load time.
type ReindexPolicy string

const (
	// PolicyPostMerge re-indexes after every merge to main. Matches
	// the existing GitNexus PostToolUse hook pattern.
	PolicyPostMerge ReindexPolicy = "post_merge"
	// PolicyScheduled re-indexes on a fixed cadence (e.g. nightly).
	PolicyScheduled ReindexPolicy = "scheduled"
	// PolicyManual re-indexes only when an operator runs an
	// explicit `gemba code-analysis reindex` command.
	PolicyManual ReindexPolicy = "manual"
	// PolicyOnDemand re-indexes lazily — the consuming persona
	// triggers a reindex right before relying on a query.
	PolicyOnDemand ReindexPolicy = "on_demand"
)

// IsValid reports whether p is one of the four canonical
// policies. Empty string returns false; callers wanting "use
// default" semantics should resolve the empty value before
// calling IsValid.
func (p ReindexPolicy) IsValid() bool {
	switch p {
	case PolicyPostMerge, PolicyScheduled, PolicyManual, PolicyOnDemand:
		return true
	default:
		return false
	}
}

// ImpactDirection picks which side of the dependency graph an
// [Provider.Impact] call walks. "upstream" returns callers
// (what would BREAK if I change this); "downstream" returns
// callees (what does this depend on); "both" returns the union.
type ImpactDirection string

const (
	ImpactUpstream   ImpactDirection = "upstream"
	ImpactDownstream ImpactDirection = "downstream"
	ImpactBoth       ImpactDirection = "both"
)

// IsValid reports whether d is one of the three canonical
// directions.
func (d ImpactDirection) IsValid() bool {
	switch d {
	case ImpactUpstream, ImpactDownstream, ImpactBoth:
		return true
	default:
		return false
	}
}

// RepoRef is the repository identity passed across the
// [Provider] interface. Name is the operator-chosen handle that
// matches the `[[repos]]` entry in `code_analysis.toml`. Path
// is the local working-tree path (absolute or workspace-relative
// — the loader resolves to absolute). Remote is the canonical
// upstream URL when known; backends that can attach to remote
// indexes use it.
type RepoRef struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote,omitempty"`
}

// Manifest is a provider's self-description. Backends fill it
// in once and return it from [Provider.Manifest]; consumers
// inspect the capability flags before issuing optional queries.
//
// Capability flags are advisory: a backend that returns
// SupportsImpact=false MUST still implement [Provider.Impact],
// but MAY return an empty [ImpactReport] with a descriptive
// note. This keeps the interface flat while letting consumers
// short-circuit when they know a backend can't help.
type Manifest struct {
	// Backend names the registered adapter ("gitnexus",
	// "sourcegraph", "codeql", custom string). MUST match the
	// key the backend was registered under.
	Backend string `json:"backend"`
	// Version is the backend implementation's version string.
	// Free-form — backends choose their own scheme.
	Version string `json:"version,omitempty"`
	// IndexedRepos lists every repo the backend currently has
	// an index for. Subset of the configured repos when an
	// initial index hasn't run yet.
	IndexedRepos []RepoRef `json:"indexed_repos,omitempty"`
	// Capability flags. Default false; backends opt in as they
	// implement each surface. See the per-method docstrings on
	// [Provider] for what each flag gates.
	SupportsSymbolContext bool `json:"supports_symbol_context,omitempty"`
	SupportsImpact        bool `json:"supports_impact,omitempty"`
	SupportsRouteMap      bool `json:"supports_route_map,omitempty"`
	SupportsEmbeddings    bool `json:"supports_embeddings,omitempty"`
	// MaxRepoSize is an advisory cap (bytes) above which the
	// backend declines to index. 0 means "no cap declared".
	MaxRepoSize int64 `json:"max_repo_size,omitempty"`
}

// Module is a code module's typed projection — the unit
// [Provider.Modules] returns. Name and Path are required;
// everything else is best-effort. LOC and FileCount are 0 when
// the backend hasn't computed them. TestCoverage is in [0, 1]
// when known and -1 when not measured (callers MUST treat
// negative values as "unknown", not "0% covered").
type Module struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Summary      string   `json:"summary,omitempty"`
	LOC          int      `json:"loc,omitempty"`
	FileCount    int      `json:"file_count,omitempty"`
	TestCoverage float64  `json:"test_coverage"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// SymbolRef is a lightweight pointer to a symbol the backend
// has surfaced (e.g. a god-class, an undocumented API). The
// fields are deliberately narrow: Name is the qualified symbol
// identifier; File / Line locate it in the working tree when
// known.
type SymbolRef struct {
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// HealthReport is the per-repo health surface [Provider.Health]
// returns. Fields are sliced by concern: cycles vs untested
// modules vs god-classes vs documentation gaps. Empty slices
// mean "the backend looked and found nothing"; nil slices mean
// "the backend doesn't surface this dimension". Callers that
// need to distinguish should check Manifest capability flags.
//
// FetchedAt + StaleAfter together drive the "analysis from 12h
// ago; reindex recommended" warning the design calls for.
type HealthReport struct {
	Repo             RepoRef       `json:"repo"`
	FetchedAt        time.Time     `json:"fetched_at"`
	StaleAfter       time.Duration `json:"stale_after,omitempty"`
	CycleCount       int           `json:"cycle_count,omitempty"`
	UntestedModules  []string      `json:"untested_modules,omitempty"`
	GodClasses       []SymbolRef   `json:"god_classes,omitempty"`
	UndocumentedAPIs []SymbolRef   `json:"undocumented_apis,omitempty"`
	StaleCode        []SymbolRef   `json:"stale_code,omitempty"`
	// Notes carries human-readable backend warnings ("index
	// older than expected", "embeddings disabled — health gaps
	// estimated"). UI surfaces these alongside the table.
	Notes []string `json:"notes,omitempty"`
}

// IsStale reports whether the report's age has exceeded its
// declared StaleAfter. Returns false when StaleAfter is zero
// (the backend declined to express a freshness window).
func (h HealthReport) IsStale(now time.Time) bool {
	if h.StaleAfter == 0 {
		return false
	}
	return now.Sub(h.FetchedAt) > h.StaleAfter
}

// ImpactReport is the blast-radius output of [Provider.Impact].
// Target is echoed back so a caller batching multiple queries
// can correlate. Direction records which side of the graph was
// walked. Affected lists the symbols within the requested
// depth; Risk is a coarse "low/medium/high/critical" classifier
// the backend computes from fan-in / fan-out / cluster
// crossings.
type ImpactReport struct {
	Target    string          `json:"target"`
	Direction ImpactDirection `json:"direction"`
	// Depth is the maximum hop count the report covers. Mirrors
	// the gitnexus depth model (1 = direct, 2 = transitive,
	// 3+ = far transitive).
	Depth    int         `json:"depth,omitempty"`
	Affected []SymbolRef `json:"affected,omitempty"`
	Risk     RiskLevel   `json:"risk,omitempty"`
	// Notes carries human-readable provenance ("4 callers in
	// `internal/walk`, 1 in `web/src/api`"). UI joins them with
	// newlines.
	Notes []string `json:"notes,omitempty"`
}

// RiskLevel classifies an [ImpactReport]'s severity. Empty
// string is "unclassified" — callers MAY treat that as low.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// ReindexFlags controls a [Provider.Reindex] call. Full
// requests a from-scratch rebuild; otherwise the backend MAY
// run an incremental update against its existing index. DryRun
// asks the backend to report what it WOULD do without writing
// anything to its index store. WithEmbeddings opts the rebuild
// in to embedding generation when the backend supports it.
type ReindexFlags struct {
	Full           bool `json:"full,omitempty"`
	DryRun         bool `json:"dry_run,omitempty"`
	WithEmbeddings bool `json:"with_embeddings,omitempty"`
}

// Provider is the typed surface every code-analysis backend
// implements. Methods are READ-MOSTLY; only [Provider.Reindex]
// mutates the backend's state, and even that only updates the
// backend's own index store — never the working tree.
//
// All methods are context-aware so callers can honour deadlines
// + cancellation when a backend's underlying RPC is slow or
// hung. Backends MUST respect ctx.Done().
//
// Implementations SHOULD be safe for concurrent reads; they are
// not required to be safe for concurrent writes — callers MUST
// serialise [Provider.Reindex] against itself per RepoRef.
type Provider interface {
	// Manifest returns the backend's self-description. SHOULD
	// be cheap (cached) — callers may invoke it on every
	// request to gate optional queries.
	Manifest(ctx context.Context) (Manifest, error)

	// ListRepos returns the configured repos this provider
	// will answer queries for. Order is implementation-defined
	// but stable across calls within a session.
	ListRepos(ctx context.Context) ([]RepoRef, error)

	// Reindex (re)builds the backend's index for repo. See
	// [ReindexFlags] for the knobs. Returns nil on success;
	// returns a descriptive error otherwise. Long-running —
	// callers SHOULD pass a context with a generous deadline.
	Reindex(ctx context.Context, repo RepoRef, flags ReindexFlags) error

	// Modules returns the module inventory for repo. Empty
	// slice when the index is empty or the backend does not
	// classify code into modules.
	Modules(ctx context.Context, repo RepoRef) ([]Module, error)

	// Health returns a fresh health report for repo. Backends
	// MAY cache internally; callers SHOULD treat the response
	// as a snapshot keyed off the backend's last index commit.
	Health(ctx context.Context, repo RepoRef) (HealthReport, error)

	// Impact returns the blast-radius for target inside repo.
	// target is the qualified symbol or file path the backend
	// understands; direction picks the side of the graph to
	// walk. Backends gated on Manifest.SupportsImpact=false
	// MUST still implement this — they MAY return an empty
	// report with a Notes entry instead of an error.
	Impact(ctx context.Context, repo RepoRef, target string, direction ImpactDirection) (ImpactReport, error)
}
