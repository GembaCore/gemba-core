package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/MikeBengtson/gemba/internal/skills/epic_order"
)

// newConsultsPostRouter builds a Router with both the dispatcher
// and the persona registry attached. Returns the Router and the
// persona it pre-registered so tests can reference its ID.
func newConsultsPostRouter(t *testing.T) (*Router, *corepersona.Persona) {
	t.Helper()
	r, _ := newConsultsRouter(t)
	pr := corepersona.NewRegistry()
	p := pmPersona()
	if err := pr.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Re-attach with the persona registry slot populated.
	r.AttachPersonaDispatcher(r.personaDispatcher, r.skillRegistry, pr)
	return r, p
}

func postConsult(t *testing.T, r *Router, body any, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/consults", buf)
	req.Header.Set("Content-Type", "application/json")
	if nonce != "" {
		req.Header.Set(ConfirmHeader, nonce)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCreateConsult_BeforeAttachReturns503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := postConsult(t, r, map[string]any{
		"persona_id": "x", "skill_id": "y", "workspace": "ws",
		"raw_input": map[string]any{},
	}, "nonce-pre-attach")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestCreateConsult_RegistryNotConfiguredReturns503(t *testing.T) {
	// Dispatcher + skills attached, but no persona registry.
	r, _ := newConsultsRouter(t)
	rec := postConsult(t, r, map[string]any{
		"persona_id": "project-manager",
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  map[string]any{},
	}, "nonce-no-registry")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateConsult_HappyPathReturns201Summary(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	rec := postConsult(t, r, map[string]any{
		"persona_id":     p.ID,
		"skill_id":       epic_order.ID,
		"workspace":      "gemba",
		"raw_input":      validEpicOrderInput(t),
		"guidance":       "favor unblocking UI work",
		"template": map[string]any{
			"workspace_name": "Gemba",
			"project_prefix": "gm",
		},
	}, "nonce-create-1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var summary consultSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.PersonaID != p.ID {
		t.Errorf("persona_id = %q, want %q", summary.PersonaID, p.ID)
	}
	if summary.SkillID != epic_order.ID {
		t.Errorf("skill_id = %q, want %s", summary.SkillID, epic_order.ID)
	}
	if summary.Status != "running" {
		t.Errorf("status = %q, want running", summary.Status)
	}
	// Consult is registered with the dispatcher.
	if _, ok := r.personaDispatcher.Get(summary.ID); !ok {
		t.Error("dispatcher did not register the new consult")
	}
}

func TestCreateConsult_NonceReplayReturnsCachedResponse(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	body := map[string]any{
		"persona_id": p.ID,
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
	}
	first := postConsult(t, r, body, "nonce-replay")
	if first.Code != http.StatusCreated {
		t.Fatalf("first: status = %d, body=%s", first.Code, first.Body.String())
	}
	second := postConsult(t, r, body, "nonce-replay")
	if second.Code != http.StatusCreated {
		t.Errorf("replay: status = %d, want 201 from cache", second.Code)
	}
	// The Consult dispatcher should still hold exactly one consult
	// for the (persona, skill, workspace) tuple — the second POST
	// must not have forked a new one.
	if got := len(r.personaDispatcher.List()); got != 1 {
		t.Errorf("dispatcher consult count = %d, want 1 (replay forked)", got)
	}
}

func TestCreateConsult_RejectsMissingFields(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	cases := map[string]map[string]any{
		"missing-persona": {"skill_id": epic_order.ID, "workspace": "gemba", "raw_input": map[string]any{}},
		"missing-skill":   {"persona_id": p.ID, "workspace": "gemba", "raw_input": map[string]any{}},
		"missing-workspace": {"persona_id": p.ID, "skill_id": epic_order.ID, "raw_input": map[string]any{}},
		"missing-raw-input": {"persona_id": p.ID, "skill_id": epic_order.ID, "workspace": "gemba"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postConsult(t, r, body, "nonce-"+name)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateConsult_UnknownPersonaReturns404(t *testing.T) {
	r, _ := newConsultsPostRouter(t)
	rec := postConsult(t, r, map[string]any{
		"persona_id": "no-such-persona",
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
	}, "nonce-unknown-p")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateConsult_UnknownSkillReturns404(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	rec := postConsult(t, r, map[string]any{
		"persona_id": p.ID,
		"skill_id":   "no-such-skill",
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
	}, "nonce-unknown-s")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateConsult_RejectsUnauthorizedPersonaSkillPair(t *testing.T) {
	// Build a persona that lists no skills — the dispatcher's
	// PersonaCanInvoke gate rejects.
	r, _ := newConsultsRouter(t)
	pr := corepersona.NewRegistry()
	p := &corepersona.Persona{
		ID:           "lonely",
		Name:         "Lonely",
		Role:         "Lonely",
		Variety:      corepersona.VarietyCoach,
		Scope:        corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:       []string{}, // no skills
		SystemPrompt: "you are {{role}}",
	}
	if err := pr.Register(p); err != nil {
		t.Fatal(err)
	}
	r.AttachPersonaDispatcher(r.personaDispatcher, r.skillRegistry, pr)
	rec := postConsult(t, r, map[string]any{
		"persona_id": p.ID,
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
	}, "nonce-unauthorized")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (persona not authorized for skill)", rec.Code)
	}
}

func TestCreateConsult_RejectsInvalidJSON(t *testing.T) {
	r, _ := newConsultsPostRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/consults", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-bad-json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCreateConsult_RejectsUnknownFields(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	rec := postConsult(t, r, map[string]any{
		"persona_id":     p.ID,
		"skill_id":       epic_order.ID,
		"workspace":      "gemba",
		"raw_input":      validEpicOrderInput(t),
		"surprise_field": "should reject",
	}, "nonce-unknown-field")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (DisallowUnknownFields)", rec.Code)
	}
}

// validEpicOrderInput returns the same fixture-shaped body
// consults_test.go uses, lifted into a typed RawMessage for the
// post body.
func validEpicOrderInput(t *testing.T) json.RawMessage {
	t.Helper()
	return epicOrderRawInput(t)
}

func TestCreateConsult_DefaultSpawnsThroughDispatcher(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	called := 0
	r.personaDispatcher.SetSpawnFunc(func(context.Context, *persona.Consult) error {
		called++
		return nil
	})
	rec := postConsult(t, r, map[string]any{
		"persona_id": p.ID,
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
	}, "nonce-spawn-default")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if called != 1 {
		t.Errorf("spawn func called %d times, want 1 (default-true)", called)
	}
}

func TestCreateConsult_SpawnFalseSkipsDispatcherSpawn(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	called := 0
	r.personaDispatcher.SetSpawnFunc(func(context.Context, *persona.Consult) error {
		called++
		return nil
	})
	spawnFalse := false
	rec := postConsult(t, r, map[string]any{
		"persona_id": p.ID,
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
		"spawn":      spawnFalse,
	}, "nonce-spawn-false")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if called != 0 {
		t.Errorf("spawn func called %d times, want 0 (spawn=false)", called)
	}
}

func TestCreateConsult_SpawnFailureReturns502(t *testing.T) {
	r, p := newConsultsPostRouter(t)
	r.personaDispatcher.SetSpawnFunc(func(context.Context, *persona.Consult) error {
		return errSpawn
	})
	rec := postConsult(t, r, map[string]any{
		"persona_id": p.ID,
		"skill_id":   epic_order.ID,
		"workspace":  "gemba",
		"raw_input":  validEpicOrderInput(t),
	}, "nonce-spawn-fail")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

var errSpawn = errors.New("backend unreachable")
