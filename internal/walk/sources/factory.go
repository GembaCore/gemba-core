// gm-vch2: LiveSources factory composes the four live escalation
// sources (OrchestrationPlane, Witness, Refinery, Beads-degraded)
// into a single walk.Sources value the server attaches via
// Router.AttachWalk.
//
// The composition is order-stable: agenda items from the
// OrchestrationPlane appear before witness, refinery, and
// beads-degraded rows. The walk's downstream dedupe path
// (walk.dedupeStable) keeps higher-priority rows when two sources
// converge on the same (Kind, Ref) pair.

package sources

import (
	"context"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/walk"
)

// CombinedEscalationLister fans out across multiple
// walk.EscalationLister implementations and concatenates their
// results in declaration order. Used to expose Witness, Refinery,
// Beads-degraded, and the OrchestrationPlane through a single
// walk.Sources.Escalations binding.
//
// Errors from any sub-lister abort the call — partial results are
// not surfaced because the agenda would silently lose escalations
// without any signal to the operator. Callers that prefer
// best-effort semantics can wrap each sub-lister in a never-error
// shim before composing.
type CombinedEscalationLister struct {
	listers []walk.EscalationLister
}

// NewCombinedEscalationLister wires the composition. nil entries
// are skipped so callers can pass NewWitnessLister(nil) etc.
// without first checking each return value. Returns nil when
// every entry is nil — composing nil through walk.Sources is a
// no-op contributor.
func NewCombinedEscalationLister(listers ...walk.EscalationLister) *CombinedEscalationLister {
	out := make([]walk.EscalationLister, 0, len(listers))
	for _, l := range listers {
		if l == nil || isTypedNil(l) {
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return &CombinedEscalationLister{listers: out}
}

// ListOpenEscalations runs each sub-lister in order and concatenates
// the results.
func (c *CombinedEscalationLister) ListOpenEscalations(ctx context.Context, workspace string) ([]core.EscalationRequest, error) {
	if c == nil {
		return nil, nil
	}
	var out []core.EscalationRequest
	for _, l := range c.listers {
		got, err := l.ListOpenEscalations(ctx, workspace)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// LiveSourcesConfig bundles the upstream signal sources every live
// lister depends on. Each field is optional — a nil field skips
// the corresponding lister, and a Sources with all four nil reduces
// to walk.Sources{} (the same shape the agenda aggregator handles
// today).
type LiveSourcesConfig struct {
	// Plane is the bound OrchestrationPlaneAdaptor. Provides the
	// canonical OrchestrationPlaneEscalationLister coverage —
	// every escalation that already converges on EscalationRequest
	// (MCP, A2A, permission, HITL, blocker, question, refinery
	// once the upstream wires through, …).
	Plane core.OrchestrationPlaneAdaptor
	// HealthBus drives BeadsDegradedLister. Nil → no synthetic
	// degraded-adaptor escalations.
	HealthBus HealthSnapshotter
	// Witness is the source the WitnessLister reads. Nil → no
	// witness rows.
	Witness WitnessFindingSource
	// Refinery is the source the RefineryRejectionLister reads.
	// Nil → no refinery rejection rows. (Once the refinery rig
	// publishes through the OrchestrationPlane this can stay nil
	// in production — Plane's lister picks them up natively.)
	Refinery RefineryRejectionSource
	// HITL, GateFailures, WorkItems flow through unchanged from
	// the caller; LiveSources doesn't synthesize these — they
	// already have live wirings (gm-uf7 / gm-rfy / WorkPlane).
	HITL         walk.HITLLister
	GateFailures walk.GateFailureLister
	WorkItems    walk.WorkItemLister
}

// LiveSources composes a walk.Sources from a LiveSourcesConfig.
// The Escalations field is the CombinedEscalationLister that
// fans out across every configured live source.
func LiveSources(cfg LiveSourcesConfig) walk.Sources {
	combined := NewCombinedEscalationLister(
		NewOrchestrationPlaneEscalationLister(cfg.Plane),
		NewWitnessLister(cfg.Witness),
		NewRefineryRejectionLister(cfg.Refinery),
		NewBeadsDegradedLister(cfg.HealthBus),
	)
	// CombinedEscalationLister is the right type for
	// walk.Sources.Escalations. A nil combined value satisfies the
	// nil-safety contract on the agenda aggregator (zero items).
	var esc walk.EscalationLister
	if combined != nil {
		esc = combined
	}
	return walk.Sources{
		Escalations:  esc,
		HITL:         cfg.HITL,
		GateFailures: cfg.GateFailures,
		WorkItems:    cfg.WorkItems,
	}
}

// isTypedNil reports whether i is a typed-nil interface value (e.g.
// a (*WitnessLister)(nil) returned by NewWitnessLister(nil) when
// the caller's source was nil). Avoids the common Go footgun where
// `if i != nil` is true for a typed nil and the next method call
// crashes on a nil receiver.
func isTypedNil(i walk.EscalationLister) bool {
	switch v := i.(type) {
	case *OrchestrationPlaneEscalationLister:
		return v == nil
	case *WitnessLister:
		return v == nil
	case *RefineryRejectionLister:
		return v == nil
	case *BeadsDegradedLister:
		return v == nil
	case *CombinedEscalationLister:
		return v == nil
	}
	return false
}
