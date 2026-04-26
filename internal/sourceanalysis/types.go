package sourceanalysis

import "time"

// Target identifies a file (or path glob) the planner cares about.
// In practice this is the same shape as a WorkItem.targets[] entry
// (gm-s47n.1) — keeping the type local rather than reaching into core
// avoids a cyclic import and lets sourceanalysis evolve its
// representation independently if globbing later needs metadata
// (e.g. role hints, exclusion flags) the data layer doesn't carry.
//
// Path is repo-relative, forward-slashed, glob-expanded by the
// caller before reaching SourceAnalysis methods that take a
// concrete file (e.g. Dependents). Implementations MAY reject
// values containing `..` or absolute paths.
type Target struct {
	// Repository is the bead's primary repository id (matches
	// core.Repository.id). Required when the planner's working set
	// spans multiple repos so an "auth.go" in repo A doesn't get
	// linked to "auth.go" in repo B.
	Repository string `json:"repository"`
	// Path is the repo-relative file path or glob.
	Path string `json:"path"`
}

// Symbol names a single exported entity (function, type, method,
// route handler, …) in a Target file. The planner uses these to
// reason about public-contract changes — see
// docs/design/work-planning.md §5.3.
//
// Kind is a free string so the interface doesn't have to enumerate
// every language's symbol vocabulary. Concrete implementations
// document the kinds they emit; the planner treats them opaquely.
type Symbol struct {
	Target Target `json:"target"`
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
}

// Diff is the input to PublicContractChanges. The planner builds it
// from a bead's actual or projected file changes; SourceAnalysis
// returns the subset that touch a public contract.
//
// Files is repo-relative. Hunks is optional — implementations that
// can do better than file-level analysis read it; ones that can't
// fall back to "every public symbol declared in this file" and
// over-report rather than under-report.
type Diff struct {
	Repository string     `json:"repository"`
	Files      []string   `json:"files"`
	Hunks      []DiffHunk `json:"hunks,omitempty"`
}

// DiffHunk is the optional, line-granular view of a single file
// edit. Implementations that don't consume it can ignore the field.
type DiffHunk struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// Capabilities describes a SourceAnalysis implementation's current
// state. The planner reads this once per dispatch cycle to decide
// whether semantic-conflict detection is reliable enough to act on.
//
// IndexUpdatedAt is the wall-clock when the backing index last
// caught up with the working tree. The planner compares it against
// recent merge waves (work-planning.md §8) to decide whether a
// scheduled re-index is overdue.
type Capabilities struct {
	// Backend names the implementation in human-readable form
	// ("gitnexus", "noop", "ctags", "lsp", ...). Free string so a
	// future binding doesn't need a SourceAnalysisKind enum churn.
	Backend string `json:"backend"`
	// Available is false when the backend is configured but cannot
	// answer queries right now (index missing, daemon down, etc).
	// The planner treats !Available as "skip semantic conflict
	// detection, log the skip" — a degraded-but-functional fall
	// through, not an error.
	Available bool `json:"available"`
	// IndexUpdatedAt is the most recent index refresh. Zero value
	// means "no index, or freshness unknown".
	IndexUpdatedAt time.Time `json:"index_updated_at,omitempty"`
	// Reason carries a short human string when Available is false
	// or the index is stale. Surface text only — never parsed.
	Reason string `json:"reason,omitempty"`
}
