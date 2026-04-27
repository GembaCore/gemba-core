// walk_id provenance via context (gm-rfy, design §Resumability).
//
// Every ExecutedAction emitted while a walk is active should
// stamp walk_id in its provenance metadata so the audit log can
// answer "which decisions came out of walk XYZ?". Plumbing the
// id through every applier signature would be invasive; using a
// context-based pattern keeps the change surface narrow.
//
// The persona dispatcher / HTTP handler decorates the request
// context with ContextWithID before invoking applier paths;
// appliers that care read it via IDFromContext and stamp
// provenance. Unrelated paths see a zero ID and behave
// unchanged.

package walk

import "context"

// idCtxKey is unexported so external packages can't collide on
// it. Stable for the lifetime of the binary.
type idCtxKey struct{}

// ContextWithID returns a derived context carrying the walk id.
// Empty id removes any prior value so a nested non-walk action
// doesn't accidentally inherit a parent walk's provenance.
func ContextWithID(ctx context.Context, id ID) context.Context {
	if id == "" {
		// Re-set with empty so a previously-bound id is
		// shadowed by zero rather than persisting.
		return context.WithValue(ctx, idCtxKey{}, ID(""))
	}
	return context.WithValue(ctx, idCtxKey{}, id)
}

// IDFromContext returns the bound walk id, or "" when no walk
// is active in this branch of the call graph.
func IDFromContext(ctx context.Context) ID {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(idCtxKey{}).(ID)
	return v
}

// IsActive reports whether the context carries a non-empty walk
// id. Convenience for "if walk.IsActive(ctx) { stamp provenance }"
// in applier code.
func IsActive(ctx context.Context) bool {
	return IDFromContext(ctx) != ""
}
