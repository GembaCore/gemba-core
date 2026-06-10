package sourceanalysis

import "context"

// Noop is the graceful-degradation SourceAnalysis (gm-s47n.3.2).
// Use it as the default binding when the operator hasn't installed a
// real backend (gitnexus, ctags, lsp). Every query returns
// (nil, ErrUnavailable) so the planner sees a uniform "skip semantic
// conflict detection, log the skip" signal — degraded but functional,
// per docs/design/work-planning.md §5.3.
//
// Reason is the operator-visible explanation surfaced via Describe;
// the planner copies it into its own dispatch log so an operator
// browsing "why didn't gemba catch this conflict?" sees a clear
// pointer to the missing backend.
//
// Concurrency: trivially safe — the struct holds no mutable state.
type Noop struct {
	Reason string
}

// NewNoop builds a Noop with a default reason. Operators who care
// about the wording can construct &Noop{Reason: "..."} directly.
func NewNoop() *Noop {
	return &Noop{Reason: "no source analysis backend configured"}
}

// Compile-time interface check — keeps the noop honest if the
// SourceAnalysis surface evolves.
var _ SourceAnalysis = (*Noop)(nil)

func (n *Noop) Dependents(_ context.Context, _ Target) ([]Target, error) {
	return nil, ErrUnavailable
}

func (n *Noop) Dependencies(_ context.Context, _ Target) ([]Target, error) {
	return nil, ErrUnavailable
}

func (n *Noop) PublicContractChanges(_ context.Context, _ Diff) ([]Symbol, error) {
	return nil, ErrUnavailable
}

func (n *Noop) Describe(_ context.Context) (Capabilities, error) {
	reason := n.Reason
	if reason == "" {
		reason = "no source analysis backend configured"
	}
	return Capabilities{
		Backend:   "noop",
		Available: false,
		Reason:    reason,
	}, nil
}
