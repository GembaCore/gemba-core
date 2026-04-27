package persona

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/persona/purviewgate"
)

// purviewGateDispatcher seeds a dispatcher with a Manager-variety
// persona that declares a Purview, plus a single applier and one
// validated line. The MutationKindResolver returns a fixed kind for
// every line so the gate fires deterministically.
//
// authority + activePhases tune the Purview; mutationKind is what the
// resolver returns; managerAuthority is the persona's declared
// MutationAuthority list. phaseFn returns the project's current phase
// (PhaseUnspecified means "no phase wired yet").
func purviewGateDispatcher(
	t *testing.T,
	authority corepersona.PurviewAuthority,
	activePhases []corepersona.Phase,
	managerAuthority []string,
	mutationKind purviewgate.MutationKind,
	phaseFn PhaseSource,
) (*Dispatcher, string) {
	t.Helper()

	resolver := MutationKindResolver(func(skillID string, line any) purviewgate.MutationKind {
		return mutationKind
	})

	d := NewDispatcher(NewAuditLog(t.TempDir()),
		WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
		WithIDFunc(func() string { return "consult-purview-1" }),
		WithWorkspaceDir(t.TempDir()),
		WithMutationKindResolver(resolver),
		WithPhaseSource(phaseFn),
	)

	skill := fakeSkill{id: "manager-skill"}
	persona := &corepersona.Persona{
		ID:                "documentarian",
		Name:              "Docs",
		Role:              "Documentarian",
		Variety:           corepersona.VarietyManager,
		Scope:             corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:            []string{"manager-skill"},
		MutationAuthority: managerAuthority,
		Purview: corepersona.Purview{
			Domain:            "docs",
			ActivePhases:      activePhases,
			BlockingAuthority: authority,
		},
		SystemPrompt: "p",
		Model: corepersona.ModelConfig{
			Vendor: "anthropic", Model: "claude-opus-4-7",
		},
	}

	c, err := d.Begin(BeginRequest{
		Persona:   persona,
		Skill:     skill,
		Workspace: "ws",
		RawInput:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := d.Receive(c.ID, []json.RawMessage{
		json.RawMessage(`{"type":"recommendation","rank":1}`),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	d.SetApplier("manager-skill", &fakeApplier{res: ApplierResult{Detail: "ok"}})
	return d, c.ID
}

// Manager + blocking purview that EXCLUDES the requested mutation
// surfaces ErrPurviewBlocked through the public dispatcher Apply.
func TestDispatcher_Apply_PurviewBlockedSurfaceTypedError(t *testing.T) {
	d, id := purviewGateDispatcher(
		t,
		corepersona.PurviewBlocking,
		[]corepersona.Phase{"building"},
		[]string{"docs_edit"},          // declared authority
		purviewgate.MutationKind("release_publish"), // requested kind — NOT in declared list
		func() corepersona.Phase { return "building" },
	)

	_, err := d.Apply(context.Background(), id, 0)
	if err == nil {
		t.Fatal("Apply did not surface a Purview block")
	}
	var blocked *purviewgate.ErrPurviewBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("err = %v (%T); want *purviewgate.ErrPurviewBlocked", err, err)
	}
	if blocked.PersonaID != "documentarian" {
		t.Errorf("PersonaID = %q, want documentarian", blocked.PersonaID)
	}
	if blocked.MutationKind != "release_publish" {
		t.Errorf("MutationKind = %q, want release_publish", blocked.MutationKind)
	}
	if blocked.Phase != "building" {
		t.Errorf("Phase = %q, want building", blocked.Phase)
	}

	// The mutation must NOT have landed: AppliedIdx stays empty so
	// a corrected retry is allowed (matches applier-failure
	// rollback semantics).
	c, _ := d.Get(id)
	if len(c.AppliedIdx) != 0 {
		t.Errorf("AppliedIdx after block = %v; want empty", c.AppliedIdx)
	}
}

// Manager + blocking purview WITH the requested mutation in scope
// proceeds to the applier and lands.
func TestDispatcher_Apply_PurviewAllowedFiresApplier(t *testing.T) {
	d, id := purviewGateDispatcher(
		t,
		corepersona.PurviewBlocking,
		[]corepersona.Phase{"building"},
		[]string{"docs_edit"},
		purviewgate.MutationKind("docs_edit"),
		func() corepersona.Phase { return "building" },
	)

	res, err := d.Apply(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Executed {
		t.Errorf("Executed = false; want true (gate allowed)")
	}
	c, _ := d.Get(id)
	if len(c.AppliedIdx) != 1 {
		t.Errorf("AppliedIdx = %v; want one entry", c.AppliedIdx)
	}
}

// Out-of-phase: the gate is dormant, so an out-of-scope mutation
// still proceeds (matches invariant #29 — outside ActivePhases the
// Purview has no authority to enforce).
func TestDispatcher_Apply_PurviewDormantOutOfPhaseAllows(t *testing.T) {
	d, id := purviewGateDispatcher(
		t,
		corepersona.PurviewBlocking,
		[]corepersona.Phase{"building"}, // active in building only
		[]string{"docs_edit"},
		purviewgate.MutationKind("release_publish"), // out of scope, but...
		func() corepersona.Phase { return "shipping" }, // dormant phase
	)

	res, err := d.Apply(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("Apply (dormant phase): %v", err)
	}
	if !res.Executed {
		t.Errorf("Executed = false; want true (gate dormant in shipping)")
	}
}

// Without a MutationKindResolver wired the gate is silent (backward
// compat with skills that haven't declared their mutation kind yet).
func TestDispatcher_Apply_NoResolverSkipsGate(t *testing.T) {
	d := NewDispatcher(NewAuditLog(t.TempDir()),
		WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
		WithIDFunc(func() string { return "consult-noresolver-1" }),
		WithWorkspaceDir(t.TempDir()),
	)
	skill := fakeSkill{id: "manager-skill"}
	persona := &corepersona.Persona{
		ID:                "documentarian",
		Name:              "Docs",
		Role:              "Documentarian",
		Variety:           corepersona.VarietyManager,
		Scope:             corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:            []string{"manager-skill"},
		MutationAuthority: []string{"docs_edit"},
		Purview: corepersona.Purview{
			Domain:            "docs",
			ActivePhases:      []corepersona.Phase{"building"},
			BlockingAuthority: corepersona.PurviewBlocking,
		},
		SystemPrompt: "p",
		Model:        corepersona.ModelConfig{Vendor: "anthropic", Model: "claude-opus-4-7"},
	}
	c, err := d.Begin(BeginRequest{
		Persona:   persona,
		Skill:     skill,
		Workspace: "ws",
		RawInput:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Receive(c.ID, []json.RawMessage{json.RawMessage(`{"x":1}`)}); err != nil {
		t.Fatal(err)
	}
	d.SetApplier("manager-skill", &fakeApplier{res: ApplierResult{Detail: "ok"}})

	// No resolver wired → gate silent → applier runs.
	res, err := d.Apply(context.Background(), c.ID, 0)
	if err != nil {
		t.Fatalf("Apply without resolver: %v", err)
	}
	if !res.Executed {
		t.Errorf("Executed = false; want true (gate silent)")
	}
}
