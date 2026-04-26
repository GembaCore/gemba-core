package sourceanalysis

import (
	"context"
	"errors"
)

// ErrUnavailable is returned by Describe (or wrapped via errors.Is by
// any other method) when the underlying source analysis backend is
// configured but cannot answer queries right now — index missing,
// daemon down, network partition, etc.
//
// Callers (the planner, work-planning.md §5.3) treat ErrUnavailable
// as "skip semantic conflict detection, log the skip" rather than
// hard failure. It is never appropriate to surface ErrUnavailable as
// a fatal error to the operator; the planner degrades gracefully to
// glob-only conflict detection.
var ErrUnavailable = errors.New("sourceanalysis: backend unavailable")

// SourceAnalysis is the abstract code-analysis surface the planner
// consumes when computing semantic conflicts (work-planning.md §4
// Layer 2 + §5.3).
//
// The four methods are intentionally independent so a binding can
// implement the cheap ones (Dependents / Dependencies) without
// committing to a diff-aware contract analysis. Methods that a
// backend cannot answer return an empty result with ErrUnavailable
// rather than fabricating data.
//
// Implementations MUST be safe for concurrent use — the planner
// fans queries out across the ready set and expects independent
// (target, target) pairs to scale linearly.
type SourceAnalysis interface {
	// Dependents returns files that import, call, or otherwise
	// depend on the given target. The result MAY include the
	// target itself when a self-reference is meaningful; callers
	// filter by identity if they need a strict "other files" set.
	//
	// Returns (nil, ErrUnavailable) when the backend is configured
	// but cannot answer (index missing, daemon down).
	Dependents(ctx context.Context, target Target) ([]Target, error)

	// Dependencies returns files the given target depends on —
	// inverse of Dependents over the same edge set.
	//
	// Same ErrUnavailable contract as Dependents.
	Dependencies(ctx context.Context, target Target) ([]Target, error)

	// PublicContractChanges returns the subset of symbols touched by
	// the diff that are public-contract entities (exported APIs,
	// route signatures, exported types). Best-effort — backends
	// that can't reason about diffs SHOULD return the file-level
	// projection (every exported symbol declared in any file in
	// the diff) rather than nothing, so the planner over-reports
	// rather than misses a real semantic conflict.
	//
	// Same ErrUnavailable contract.
	PublicContractChanges(ctx context.Context, diff Diff) ([]Symbol, error)

	// Describe reports the backend's current health: which
	// implementation is bound, whether it can answer, when its
	// index last refreshed, and a one-line reason if degraded.
	//
	// Describe MUST NOT itself return ErrUnavailable — the whole
	// point is for it to TELL you the backend is unavailable.
	// Implementation-internal errors (process exec failure, etc)
	// are returned as a normal error.
	Describe(ctx context.Context) (Capabilities, error)
}
