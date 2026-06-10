// Package purviewgate binds a Persona's [corepersona.Purview] to the
// Manager-mutation path (gm-3on, follow-up to gm-9rv). The gate is a
// pure function: given a persona, the kind of mutation it is about to
// perform, and the project's current Phase, it returns whether the
// mutation may proceed and a human-readable reason.
//
// Invariants enforced (gm-9rv §3 + §28-29):
//
//   - Coach personas BYPASS the gate entirely. Coaches surface
//     SuggestedActions; they never mutate. A Coach reaching this gate
//     is a misuse — the gate still allows it (with reason
//     "coach-bypass") so the call is harmless, but production callers
//     should never invoke the gate for a Coach.
//   - Personas without a declared Purview (zero value) are unrestricted
//     by Purview semantics — `mutation_authority` + `mutation_scope`
//     are the only constraints (those are gates owned elsewhere; this
//     package speaks only Purview).
//   - PurviewAdvisory always allows. A persona whose Purview is
//     advisory has strong opinions but never blocks — invariant #28.
//   - PurviewStrong / PurviewBlocking deny iff:
//     a) the requested MutationKind is NOT in the persona's
//     [corepersona.Persona.MutationAuthority] list, OR
//     b) the persona's Purview declares ActivePhases AND the current
//     phase is NOT in that list — wait, the design inverts this:
//     outside ActivePhases the Purview is DORMANT (allow); the gate
//     only fires when the project's phase is in ActivePhases.
//   - When the persona declares no ActivePhases, the Purview is
//     considered always-active (matches "auto-activate regardless of
//     phase" — Security pattern from invariant #32).
//
// The Phase primitive itself ships with gm-jt9 (sibling agent in
// parallel). Until that lands, callers pass [corepersona.Phase] —
// today defined as a typed string in `internal/core/persona/types.go`
// — and the gate treats the empty string as "unspecified phase". When
// the project phase is unspecified AND the persona's Purview declares
// ActivePhases, we conservatively treat the Purview as DORMANT so the
// fallback path doesn't accidentally over-block before gm-jt9 lands.
package purviewgate

import (
	"fmt"

	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
)

// MutationKind names a category of mutation the dispatcher is about
// to perform on the operator's behalf. The string MUST match an entry
// in the Manager persona's [corepersona.Persona.MutationAuthority]
// list — that list is the persona-side declaration of "actions I am
// allowed to perform"; this gate enforces it.
//
// Examples (free-form so workspaces can declare custom kinds):
//
//   - "documentation_edit" — Documentarian's mutation authority
//   - "release_publish"    — Deployment Engineer
//   - "test_author"        — QA
//
// Free-form by design: gm-9rv declines to enumerate. New personas may
// declare new kinds; the gate is the single chokepoint that checks
// the declared list.
type MutationKind string

// PhaseUnspecified is the zero/sentinel phase value. Used by callers
// before gm-jt9's Phase store is wired so the gate has a defined
// behavior: a persona with empty Purview.ActivePhases is treated as
// "always active"; a persona with non-empty ActivePhases is treated
// as "dormant" when the project phase is unspecified (allow). This
// preserves the Coach/Manager mutation path's existing behavior while
// the Phase primitive lands.
//
// TODO(gm-jt9): once the Phase store lands, callers (dispatcher,
// applier) should resolve the active phase from `phase.WorkspaceState`
// and pass it through. The fallback path here can stay — it documents
// the "no phase configured" case.
const PhaseUnspecified corepersona.Phase = ""

// Decision is the gate's verdict.
type Decision struct {
	// Allowed is true iff the mutation may proceed.
	Allowed bool
	// Reason is the human-readable justification — surfaced through
	// [ErrPurviewBlocked.Error] on deny so the operator knows why.
	// Always populated (even on Allow) so audit logs show the
	// matched rule.
	Reason string
}

// ErrPurviewBlocked is returned by integrators when the gate denies
// a mutation. It is a typed error so callers can `errors.Is` /
// `errors.As` on it without string matching.
type ErrPurviewBlocked struct {
	PersonaID    string
	MutationKind MutationKind
	Phase        corepersona.Phase
	Reason       string
}

// Error implements the error interface.
func (e *ErrPurviewBlocked) Error() string {
	return fmt.Sprintf(
		"persona/purview: mutation %q blocked for persona %q in phase %q: %s",
		string(e.MutationKind), e.PersonaID, string(e.Phase), e.Reason,
	)
}

// Allow runs the Purview gate. It is pure: no clocks, no I/O. The
// dispatcher / applier wrap the result into [ErrPurviewBlocked] when
// the decision is a deny. See package doc for the full ruleset.
//
// Inputs:
//   - persona — the persona about to mutate. Nil is rejected.
//   - action — the mutation kind being attempted.
//   - currentPhase — the project's active phase, or PhaseUnspecified
//     until gm-jt9's phase store is wired.
//
// Returns the Decision; never returns an error itself (an unknown
// authority value silently degrades to "deny" with reason "invalid
// blocking_authority" — Validate on the persona load should have
// caught that earlier).
func Allow(persona *corepersona.Persona, action MutationKind, currentPhase corepersona.Phase) Decision {
	if persona == nil {
		return Decision{Allowed: false, Reason: "persona is nil"}
	}

	// Coaches never mutate. Reaching the gate at all is a
	// misuse, but we allow with a clear reason so the call is
	// inert in production. Production wiring uses [DenyForCoach]
	// to harden this further if needed.
	if persona.Variety == corepersona.VarietyCoach {
		return Decision{Allowed: true, Reason: "coach-bypass"}
	}

	// Manager with no Purview declared: this gate is silent.
	// `mutation_authority` + `mutation_scope` are the only
	// restrictions, and they are enforced elsewhere.
	if persona.Purview.IsZero() {
		return Decision{Allowed: true, Reason: "no-purview-declared"}
	}

	authority := persona.Purview.BlockingAuthority

	switch authority {
	case corepersona.PurviewAdvisory:
		// Invariant #28: advisory never blocks.
		return Decision{Allowed: true, Reason: "advisory-only"}

	case corepersona.PurviewStrong, corepersona.PurviewBlocking:
		// Phase check first. A Purview is DORMANT outside its
		// ActivePhases (invariant #29). Dormant = allow.
		if !phaseActive(persona.Purview.ActivePhases, currentPhase) {
			return Decision{
				Allowed: true,
				Reason:  fmt.Sprintf("purview dormant in phase %q", string(currentPhase)),
			}
		}

		// In-phase: the action MUST be in the persona's declared
		// MutationAuthority. Outside that list, even a Manager
		// behaves as a Coach (gm-uf7 + invariant #29) — the gate
		// blocks with a reason naming both the kind and the
		// declared authority list.
		if !mutationAuthorized(persona.MutationAuthority, action) {
			return Decision{
				Allowed: false,
				Reason: fmt.Sprintf(
					"mutation %q outside declared mutation_authority %v (purview=%s, authority=%s)",
					string(action), persona.MutationAuthority,
					persona.Purview.Domain, string(authority),
				),
			}
		}

		return Decision{
			Allowed: true,
			Reason: fmt.Sprintf(
				"in-scope (purview=%s, authority=%s)",
				persona.Purview.Domain, string(authority),
			),
		}

	default:
		// Unknown authority — defensive deny. Persona.Validate
		// rejects this earlier; reaching here means a programmatic
		// construction skipped Validate.
		return Decision{
			Allowed: false,
			Reason:  fmt.Sprintf("invalid blocking_authority %q (programmer error — Validate should reject)", string(authority)),
		}
	}
}

// phaseActive reports whether currentPhase is one in which the
// Purview is binding. Empty activePhases means "always active"
// (Security-style — invariant #32). When currentPhase is
// PhaseUnspecified AND activePhases is non-empty, return false
// (DORMANT) so the gate doesn't over-block before gm-jt9 lands.
func phaseActive(activePhases []corepersona.Phase, currentPhase corepersona.Phase) bool {
	if len(activePhases) == 0 {
		return true
	}
	if currentPhase == PhaseUnspecified {
		// Phase primitive not yet wired (gm-jt9). Treat as
		// dormant so we don't accidentally hard-block work that
		// declared phase-conditional authority.
		return false
	}
	for _, p := range activePhases {
		if p == currentPhase {
			return true
		}
	}
	return false
}

// mutationAuthorized reports whether action is in the persona's
// declared MutationAuthority list. Empty list means "no mutations
// authorized" — Persona.Validate enforces that a Manager declares at
// least one entry, so the empty case here should only occur in tests
// that bypass Validate.
func mutationAuthorized(authority []string, action MutationKind) bool {
	for _, a := range authority {
		if a == string(action) {
			return true
		}
	}
	return false
}
