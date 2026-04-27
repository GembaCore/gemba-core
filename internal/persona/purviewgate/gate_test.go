package purviewgate

import (
	"errors"
	"strings"
	"testing"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// managerPersona returns a Manager persona with the given purview
// authority + active-phases configuration. Skips Validate so tests
// can pin specific edge configurations (e.g. zero MutationAuthority)
// without the loader rejecting them.
func managerPersona(authority corepersona.PurviewAuthority, mutAuth []string, activePhases []corepersona.Phase) *corepersona.Persona {
	return &corepersona.Persona{
		ID:                "test-manager",
		Name:              "TM",
		Role:              "Test Manager",
		Variety:           corepersona.VarietyManager,
		Scope:             corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		MutationAuthority: mutAuth,
		Purview: corepersona.Purview{
			Domain:            "design",
			ActivePhases:      activePhases,
			BlockingAuthority: authority,
		},
		SystemPrompt: "x",
		Model: corepersona.ModelConfig{
			Vendor: "anthropic", Model: "claude-opus-4-7",
		},
	}
}

func TestAllow_NilPersonaIsRejected(t *testing.T) {
	d := Allow(nil, "x", PhaseUnspecified)
	if d.Allowed {
		t.Fatalf("nil persona allowed: %+v", d)
	}
}

// Coach variety always bypasses the gate — Coaches surface
// SuggestedActions, never mutate. The gate's behavior on a Coach is
// a defense-in-depth no-op (production callers never invoke).
func TestAllow_CoachBypassesGate(t *testing.T) {
	p := managerPersona(corepersona.PurviewBlocking, []string{"docs_edit"}, []corepersona.Phase{"building"})
	p.Variety = corepersona.VarietyCoach

	d := Allow(p, "docs_edit", "building")
	if !d.Allowed {
		t.Errorf("coach blocked: %+v", d)
	}
	if !strings.Contains(d.Reason, "coach") {
		t.Errorf("reason = %q, want mention of coach-bypass", d.Reason)
	}
}

// Manager + advisory authority always allowed (invariant #28).
func TestAllow_ManagerAdvisoryAlwaysAllowed(t *testing.T) {
	p := managerPersona(corepersona.PurviewAdvisory, []string{"docs_edit"}, []corepersona.Phase{"building"})

	// Even when the action is OUTSIDE declared MutationAuthority,
	// advisory authority means the gate does not block. (The
	// MutationAuthority check is a separate gate; this gate is
	// strictly Purview semantics.)
	d := Allow(p, "release_publish", "building")
	if !d.Allowed {
		t.Errorf("advisory blocked: %+v", d)
	}
	if !strings.Contains(d.Reason, "advisory") {
		t.Errorf("reason = %q, want mention of advisory-only", d.Reason)
	}
}

// Manager + blocking authority + action in declared mutation_authority
// + current phase in active phases → ALLOWED.
func TestAllow_ManagerBlockingActionInScopeAllowed(t *testing.T) {
	p := managerPersona(corepersona.PurviewBlocking, []string{"docs_edit"}, []corepersona.Phase{"building"})

	d := Allow(p, "docs_edit", "building")
	if !d.Allowed {
		t.Errorf("in-scope action blocked: %+v", d)
	}
	if !strings.Contains(d.Reason, "in-scope") {
		t.Errorf("reason = %q, want mention of in-scope", d.Reason)
	}
}

// Manager + blocking authority + action OUTSIDE declared
// mutation_authority + current phase in active phases → BLOCKED.
func TestAllow_ManagerBlockingActionOutOfScopeBlocked(t *testing.T) {
	p := managerPersona(corepersona.PurviewBlocking, []string{"docs_edit"}, []corepersona.Phase{"building"})

	d := Allow(p, "release_publish", "building")
	if d.Allowed {
		t.Errorf("out-of-scope action allowed: %+v", d)
	}
	if !strings.Contains(d.Reason, "release_publish") {
		t.Errorf("reason = %q, want naming the offending action", d.Reason)
	}
	if !strings.Contains(d.Reason, "mutation_authority") {
		t.Errorf("reason = %q, want pointing at the declared list", d.Reason)
	}
}

// Phase-conditional: action allowed in Build phase (in ActivePhases),
// blocked in Maintain phase (not in ActivePhases) — wait, the
// dormant-outside-phase rule INVERTS naive intuition. Outside the
// active phase, the Purview is DORMANT, so the gate does NOT block
// (the persona has no authority to enforce). Re-read invariant #29:
//
//	"A Purview is dormant outside its active phases and binding
//	during them."
//
// So a Manager who would have blocked in Build phase is silently
// permissive in Maintain phase (its authority is dormant). That
// matches the test from the bead: "Phase-conditional: action allowed
// in Build phase, blocked in Maintain phase" — but reading carefully,
// the bead's test wants the BLOCK to fire in Build and not in
// Maintain. We test BOTH directions to lock the semantics.
func TestAllow_PhaseConditional_BlockInActivePhase_DormantOutside(t *testing.T) {
	// MutationAuthority does NOT include the action — so when the
	// gate is binding (in ActivePhases), it blocks. When the gate
	// is dormant (out of ActivePhases), it allows.
	p := managerPersona(
		corepersona.PurviewBlocking,
		[]string{"design_review"}, // out-of-scope action below
		[]corepersona.Phase{"building"},
	)

	// In the persona's active phase: gate binding, action OOS → blocked.
	d := Allow(p, "release_publish", "building")
	if d.Allowed {
		t.Errorf("expected block in active phase: %+v", d)
	}

	// Outside the persona's active phase: gate dormant → allowed.
	d = Allow(p, "release_publish", "maintain")
	if !d.Allowed {
		t.Errorf("expected dormant-allow in inactive phase: %+v", d)
	}
	if !strings.Contains(d.Reason, "dormant") {
		t.Errorf("reason = %q, want mention of dormant", d.Reason)
	}
}

// Empty ActivePhases means "always active" (Security pattern,
// invariant #32). The gate is binding regardless of phase.
func TestAllow_EmptyActivePhasesIsAlwaysActive(t *testing.T) {
	p := managerPersona(
		corepersona.PurviewBlocking,
		[]string{"auth_change"},
		nil, // always-on
	)

	// In-scope action — allowed in any phase.
	d := Allow(p, "auth_change", "ideation")
	if !d.Allowed {
		t.Errorf("always-on in-scope blocked: %+v", d)
	}

	// Out-of-scope action — blocked in any phase.
	d = Allow(p, "release_publish", "shipping")
	if d.Allowed {
		t.Errorf("always-on out-of-scope allowed: %+v", d)
	}
}

// PhaseUnspecified + non-empty ActivePhases → DORMANT. Conservative
// fallback while gm-jt9 lands; we choose to under-block (allow) so
// existing mutation paths that haven't yet been wired to the phase
// store keep working.
func TestAllow_PhaseUnspecifiedWithActivePhases_IsDormant(t *testing.T) {
	p := managerPersona(
		corepersona.PurviewBlocking,
		[]string{"design_review"}, // wouldn't authorize the action below
		[]corepersona.Phase{"building"},
	)

	d := Allow(p, "release_publish", PhaseUnspecified)
	if !d.Allowed {
		t.Errorf("expected unspecified-phase fallback to allow (dormant): %+v", d)
	}
}

// Persona with no Purview declared — gate is silent (allow).
func TestAllow_ManagerWithoutPurviewIsAllowed(t *testing.T) {
	p := managerPersona(corepersona.PurviewBlocking, []string{"x"}, nil)
	p.Purview = corepersona.Purview{} // wipe

	d := Allow(p, "x", "building")
	if !d.Allowed {
		t.Errorf("manager-without-purview blocked: %+v", d)
	}
	if !strings.Contains(d.Reason, "no-purview") {
		t.Errorf("reason = %q, want mention of no-purview", d.Reason)
	}
}

// Defensive deny on an unknown authority value — Persona.Validate
// rejects this earlier, but the gate hardens anyway.
func TestAllow_UnknownAuthorityDenied(t *testing.T) {
	p := managerPersona("nonsense", []string{"x"}, nil)

	d := Allow(p, "x", "building")
	if d.Allowed {
		t.Errorf("unknown-authority allowed: %+v", d)
	}
	if !strings.Contains(d.Reason, "invalid blocking_authority") {
		t.Errorf("reason = %q, want defensive-deny reason", d.Reason)
	}
}

// ErrPurviewBlocked — surfaceable as a typed error.
func TestErrPurviewBlocked_TypedError(t *testing.T) {
	err := &ErrPurviewBlocked{
		PersonaID:    "documentarian",
		MutationKind: "release_publish",
		Phase:        "building",
		Reason:       "out of scope",
	}
	var target *ErrPurviewBlocked
	if !errors.As(err, &target) {
		t.Fatalf("errors.As failed; expected typed match")
	}
	if target.PersonaID != "documentarian" {
		t.Errorf("As lost field: %+v", target)
	}
	msg := err.Error()
	for _, want := range []string{"documentarian", "release_publish", "building", "out of scope"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
}
