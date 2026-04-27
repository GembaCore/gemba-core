// Tests for /api/v1/personas (gm-8qr). Covers the empty / unbound
// 503, the round-trip of all three gm-9rv axes onto the wire, and
// the absence-as-omitted contract for personas that did not declare
// the new blocks.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// personasResponse mirrors the wire shape; tests decode into it
// rather than reaching into private types.
type personasResponse struct {
	Personas []struct {
		ID          string                 `json:"id"`
		Name        string                 `json:"name"`
		Role        string                 `json:"role"`
		Variety     string                 `json:"variety"`
		Scope       map[string]any         `json:"scope"`
		Personality map[string]any         `json:"personality,omitempty"`
		Perspective map[string]any         `json:"perspective,omitempty"`
		Purview     map[string]any         `json:"purview,omitempty"`
	} `json:"personas"`
	Total int `json:"total"`
}

func TestListPersonasV1_503BeforeAttach(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/personas", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestListPersonasV1_EmptyRegistry(t *testing.T) {
	reg := corepersona.NewRegistry()
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonaDispatcher(nil, nil, reg)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/personas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got personasResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || len(got.Personas) != 0 {
		t.Errorf("want empty roster; got %+v", got)
	}
}

// gm-8qr: a persona declaring all three PPPP blocks surfaces them on
// the wire under their canonical JSON names.
func TestListPersonasV1_SurfacesPPPPFields(t *testing.T) {
	reg := corepersona.NewRegistry()
	p := &corepersona.Persona{
		ID:      "architect",
		Name:    "Arch",
		Role:    "Architect",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Personality: corepersona.Personality{
			ID:          "laconic",
			Description: "Precise and economical.",
		},
		Perspective: corepersona.Perspective{
			Statement:     "design integrity",
			Triggers:      []string{"area:core"},
			VolunteerMode: corepersona.VolunteerOnTrigger,
			CostTier:      "haiku",
		},
		Purview: corepersona.Purview{
			Domain:            "design",
			ActivePhases:      []corepersona.Phase{"design", "building"},
			BlockingAuthority: corepersona.PurviewStrong,
		},
	}
	if err := reg.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonaDispatcher(nil, nil, reg)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/personas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got personasResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Personas) != 1 {
		t.Fatalf("want exactly one persona; got %+v", got)
	}
	row := got.Personas[0]
	if row.ID != "architect" {
		t.Errorf("id = %q, want architect", row.ID)
	}

	// Personality
	if row.Personality["id"] != "laconic" {
		t.Errorf("personality.id = %v, want laconic", row.Personality["id"])
	}
	if row.Personality["description"] != "Precise and economical." {
		t.Errorf("personality.description = %v", row.Personality["description"])
	}

	// Perspective
	if row.Perspective["statement"] != "design integrity" {
		t.Errorf("perspective.statement = %v", row.Perspective["statement"])
	}
	if row.Perspective["volunteer_mode"] != "on_trigger" {
		t.Errorf("perspective.volunteer_mode = %v, want on_trigger", row.Perspective["volunteer_mode"])
	}
	if row.Perspective["cost_tier"] != "haiku" {
		t.Errorf("perspective.cost_tier = %v", row.Perspective["cost_tier"])
	}
	triggers, _ := row.Perspective["triggers"].([]any)
	if len(triggers) != 1 || triggers[0] != "area:core" {
		t.Errorf("perspective.triggers = %v", triggers)
	}

	// Purview
	if row.Purview["domain"] != "design" {
		t.Errorf("purview.domain = %v", row.Purview["domain"])
	}
	if row.Purview["blocking_authority"] != "strong" {
		t.Errorf("purview.blocking_authority = %v", row.Purview["blocking_authority"])
	}
	phases, _ := row.Purview["active_phases"].([]any)
	if len(phases) != 2 || phases[0] != "design" || phases[1] != "building" {
		t.Errorf("purview.active_phases = %v", phases)
	}
}

// gm-8qr: a persona that does NOT declare PPPP blocks must emit
// no personality/perspective/purview keys (omitempty). Sibling beads
// gm-1w7 add concrete values to the seeds; until then the wire
// behaviour for the existing personas must be "absence".
func TestListPersonasV1_OmitsZeroValuePPPP(t *testing.T) {
	reg := corepersona.NewRegistry()
	p := &corepersona.Persona{
		ID:      "tester",
		Name:    "Test",
		Role:    "Tester",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
	}
	if err := reg.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonaDispatcher(nil, nil, reg)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/personas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Decode into a generic map so we can assert key absence rather
	// than parsing into a struct that defaults the fields to nil.
	var raw struct {
		Personas []map[string]any `json:"personas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Personas) != 1 {
		t.Fatalf("want 1 persona; got %d", len(raw.Personas))
	}
	row := raw.Personas[0]
	for _, key := range []string{"personality", "perspective", "purview"} {
		if _, present := row[key]; present {
			t.Errorf("zero-value persona surfaced key %q on wire (want omitted): %v", key, row[key])
		}
	}
}
