package phase

import (
	"errors"
	"fmt"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// Phase names a project-level mode (gm-9rv §3). The string values are
// the canonical wire form — they appear in TOML configs (Purview
// active_phases), in API responses, and in event payloads. Treat the
// set as additive: introducing a new Phase requires updating
// allPhases + transitionGraph; existing values are stable.
type Phase string

const (
	// PhaseIdeation — research, prior-art analysis, concept shaping.
	// Default initial Phase for a fresh workspace.
	PhaseIdeation Phase = "ideation"
	// PhaseDesign — architectural decisions, type-system design, UI
	// spec, API contract locked.
	PhaseDesign Phase = "design"
	// PhaseBuilding — implementation, test authoring, iterative work.
	PhaseBuilding Phase = "building"
	// PhaseValidating — QA pass, security review, release-readiness
	// checks.
	PhaseValidating Phase = "validating"
	// PhaseShipping — final release mechanics, deploy, post-release
	// monitoring.
	PhaseShipping Phase = "shipping"
	// PhaseCommercialization — product-strategy work, go-to-market,
	// pricing. Future CEO pack.
	PhaseCommercialization Phase = "commercialization"
)

// allPhases is the canonical enumeration in design-document order. The
// HTTP /api/v1/phase handler returns it so clients can render the full
// set without hard-coding the list.
var allPhases = []Phase{
	PhaseIdeation,
	PhaseDesign,
	PhaseBuilding,
	PhaseValidating,
	PhaseShipping,
	PhaseCommercialization,
}

// All returns every known Phase in canonical order. The returned slice
// is a copy — callers may mutate it without affecting the package
// state.
func All() []Phase {
	out := make([]Phase, len(allPhases))
	copy(out, allPhases)
	return out
}

// IsValid reports whether p is one of the enumerated Phases. The empty
// string is invalid — callers normalising a TOML default should pick
// PhaseIdeation explicitly rather than relying on the zero value.
func (p Phase) IsValid() bool {
	for _, q := range allPhases {
		if p == q {
			return true
		}
	}
	return false
}

// Validate returns a typed error when p is not a known Phase. The
// error wraps ErrInvalidPhase so callers can errors.Is against it
// without string matching.
func (p Phase) Validate() error {
	if p.IsValid() {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidPhase, string(p))
}

// String returns the canonical wire form. Implements fmt.Stringer so
// log lines stamp the Phase as plain text instead of a Go-syntax
// quoted form.
func (p Phase) String() string { return string(p) }

// Errors -----------------------------------------------------------------

// ErrInvalidPhase signals that a Phase value is not one of the
// enumerated tokens. Wrapped by Phase.Validate.
var ErrInvalidPhase = errors.New("phase: invalid phase")

// ErrInvalidTransition signals that a (from, to) Phase pair is not
// permitted by the transition graph. Wrapped by Advance.
var ErrInvalidTransition = errors.New("phase: invalid transition")

// ErrSamePhase signals that Advance was called with from == to. Kept
// distinct from ErrInvalidTransition so the API surface can return a
// 409 Conflict rather than a 400 Bad Request — staying in the current
// Phase is a no-op, not a malformed call.
var ErrSamePhase = errors.New("phase: already in target phase")

// transitionGraph defines the legal forward-and-backward edges between
// Phases. Each value lists the Phases reachable in a single Advance
// call from the key. Rules (gm-9rv design + simplest-defensible
// extension):
//
//   - Forward progression: every Phase may advance to its immediate
//     successor in canonical order (ideation→design→building→
//     validating→shipping→commercialization).
//   - Forward jumps: validating → building (rework triggered by QA),
//     shipping → validating (release-readiness regression),
//     commercialization → shipping (post-launch fix cycle).
//   - Lateral (re-open design): design → ideation, building → design,
//     validating → design (architectural rethink mid-stream).
//   - Backwards from terminal phases is allowed only one step
//     (commercialization → shipping). Maintain → Discovery is not in
//     this enum (those names came from the bead description, the
//     design doc names commercialization which is the spec we follow).
//
// Operators always have a nonce-confirmed override path at the API
// layer; this graph is the safe-by-default rail.
var transitionGraph = map[Phase][]Phase{
	PhaseIdeation: {
		PhaseDesign,
	},
	PhaseDesign: {
		PhaseIdeation,
		PhaseBuilding,
	},
	PhaseBuilding: {
		PhaseDesign,
		PhaseValidating,
	},
	PhaseValidating: {
		PhaseDesign,
		PhaseBuilding,
		PhaseShipping,
	},
	PhaseShipping: {
		PhaseValidating,
		PhaseCommercialization,
	},
	PhaseCommercialization: {
		PhaseShipping,
	},
}

// CanTransition reports whether from → to is a legal single-step
// transition under the current graph. Reflexive transitions (from ==
// to) return false — callers wanting "stay in place" should not call
// Advance at all (or expect ErrSamePhase).
func CanTransition(from, to Phase) bool {
	if from == to {
		return false
	}
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	for _, q := range transitionGraph[from] {
		if q == to {
			return true
		}
	}
	return false
}

// LegalTransitionsFrom returns the set of Phases reachable from p in
// one Advance call. Empty when p is invalid. The returned slice is a
// copy.
func LegalTransitionsFrom(p Phase) []Phase {
	src := transitionGraph[p]
	out := make([]Phase, len(src))
	copy(out, src)
	return out
}

// Transition is one entry in WorkspaceState.History — the audit trail
// the HTTP API surfaces and the SSE event payload mirrors. All fields
// are populated by Advance; callers SHOULD NOT construct Transitions
// directly.
type Transition struct {
	// From is the Phase the workspace was in immediately before the
	// transition.
	From Phase `json:"from"`
	// To is the Phase the workspace is in after the transition. By
	// construction (Advance), this differs from From and is reachable
	// from From in the transition graph.
	To Phase `json:"to"`
	// At is the wall-clock time the transition was recorded. UTC.
	At time.Time `json:"at"`
	// By is the AgentID that requested the transition. Empty when the
	// transition is server-internal (e.g. boot-time default), but the
	// HTTP layer always supplies a non-empty value.
	By core.AgentID `json:"by,omitempty"`
	// Reason is a free-form operator-supplied justification. Required
	// at the API layer (the handler rejects empty strings); optional
	// here so tests + boot-time seeding can omit it.
	Reason string `json:"reason,omitempty"`
}
