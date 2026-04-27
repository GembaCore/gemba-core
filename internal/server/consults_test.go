package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/MikeBengtson/gemba/internal/skills/epic_order"
)

// newConsultsRouter builds a Router with a populated dispatcher: a PM
// persona, the epic_order skill, a workspace dir, and a deterministic
// clock. Returns the Router plus the constructed Dispatcher so tests
// can drive it through Begin to seed consult state.
func newConsultsRouter(t *testing.T) (*Router, *persona.Dispatcher) {
	t.Helper()
	sr := corepersona.NewSkillRegistry()
	if err := epic_order.Register(sr); err != nil {
		t.Fatalf("epic_order.Register: %v", err)
	}
	auditDir := t.TempDir()
	wsDir := t.TempDir()
	d := persona.NewDispatcher(
		persona.NewAuditLog(auditDir),
		persona.WithWorkspaceDir(wsDir),
		persona.WithClock(func() time.Time {
			return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
		}),
	)
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonaDispatcher(d, sr, nil)
	return r, d
}

func pmPersona() *corepersona.Persona {
	return &corepersona.Persona{
		ID:      "project-manager",
		Name:    "PM",
		Role:    "Project Manager",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:  []string{epic_order.ID},
		// Tiny system prompt so the dispatcher's template helper has
		// something to render. The compose call substitutes {{role}}.
		SystemPrompt: "You are {{role}}.",
	}
}

// epicOrderRawInput returns a tiny but valid epic_order input. The
// shape mirrors EpicOrderInput in internal/skills/epic_order/types.go
// closely enough to slip past Skill.ValidateInput's
// disallow-unknown-fields decoder; we only populate the fields the
// schema marks as required, omitting the optional ones.
func epicOrderRawInput(t *testing.T) json.RawMessage {
	t.Helper()
	body := map[string]any{
		"workspace":      "gemba",
		"workspace_name": "Gemba",
		"as_of":          "2026-04-26T00:00:00Z",
		"candidate_epics": []map[string]any{
			{
				"epic_id":  "gm-test-1",
				"title":    "first epic",
				"ui_state": "on_deck",
			},
		},
		"constraints": map[string]any{},
	}
	out, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func TestListConsults_BeforeAttachReturnsEmpty(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty list when no dispatcher)", rec.Code)
	}
	var body struct {
		Consults []consultSummary `json:"consults"`
		Total    int              `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 0 || len(body.Consults) != 0 {
		t.Errorf("expected empty list, got %+v", body)
	}
}

func TestListConsults_RendersInFlightConsult(t *testing.T) {
	r, d := newConsultsRouter(t)
	skill, _ := r.skillRegistry.Get(epic_order.ID)
	c, err := d.Begin(persona.BeginRequest{
		Persona:   pmPersona(),
		Skill:     skill,
		Workspace: "test-workspace",
		RawInput:  epicOrderRawInput(t),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Consults []consultSummary `json:"consults"`
		Total    int              `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 {
		t.Fatalf("total = %d, want 1", body.Total)
	}
	row := body.Consults[0]
	if row.ID != c.ID {
		t.Errorf("id = %q, want %q", row.ID, c.ID)
	}
	if row.PersonaID != "project-manager" {
		t.Errorf("persona_id = %q, want project-manager", row.PersonaID)
	}
	if row.SkillID != epic_order.ID {
		t.Errorf("skill_id = %q, want %s", row.SkillID, epic_order.ID)
	}
	if row.Status != "running" {
		t.Errorf("status = %q, want running", row.Status)
	}
}

func TestGetConsult_ReturnsFullDetailIncludingComposedAndRawRequest(t *testing.T) {
	r, d := newConsultsRouter(t)
	skill, _ := r.skillRegistry.Get(epic_order.ID)
	raw := epicOrderRawInput(t)
	c, err := d.Begin(persona.BeginRequest{
		Persona:   pmPersona(),
		Skill:     skill,
		Workspace: "test-workspace",
		RawInput:  raw,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/"+c.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body consultDetail
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != c.ID {
		t.Errorf("id = %q, want %q", body.ID, c.ID)
	}
	if body.Composed.System == "" && body.Composed.User == "" {
		// At least one Composed slot must be populated; the dispatcher
		// runs Compose during Begin so a fresh consult always carries
		// rendered prompt text.
		t.Errorf("composed prompt is entirely empty: %+v", body.Composed)
	}
	if len(body.RawRequest) == 0 {
		t.Error("raw_request is empty; the SPA needs the original input to re-display it")
	}
	if body.ValidatedLines == nil {
		t.Error("validated_lines is nil; should be an empty array (json/[] is the SPA's expected shape)")
	}
}

func TestGetConsult_UnknownReturns404(t *testing.T) {
	r, _ := newConsultsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/no-such-consult", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetConsult_LiveResponseTagsSourceLive(t *testing.T) {
	r, d := newConsultsRouter(t)
	skill, _ := r.skillRegistry.Get(epic_order.ID)
	c, err := d.Begin(persona.BeginRequest{
		Persona:   pmPersona(),
		Skill:     skill,
		Workspace: "test-workspace",
		RawInput:  epicOrderRawInput(t),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/"+c.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body consultDetail
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Source != sourceLive {
		t.Errorf("source = %q, want %q", body.Source, sourceLive)
	}
	if !body.ComposedPersisted {
		t.Errorf("composed_persisted = false on live consult; want true")
	}
}

func TestGetConsult_FallsThroughToAuditLogAfterFinish(t *testing.T) {
	r, d := newConsultsRouter(t)
	skill, _ := r.skillRegistry.Get(epic_order.ID)
	c, err := d.Begin(persona.BeginRequest{
		Persona:   pmPersona(),
		Skill:     skill,
		Workspace: "test-workspace",
		RawInput:  epicOrderRawInput(t),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// Finish the consult so it's removed from the live registry
	// and lands in the audit log.
	if _, err := d.Finish(c.ID, persona.FinishInfo{
		Tokens: corepersona.TokenUsage{In: 100, Out: 250},
		Model:  "claude-test",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, ok := d.Get(c.ID); ok {
		t.Fatal("Finish did not remove consult from live registry")
	}

	// GET should now serve the audit-log shape.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/"+c.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (audit fallthrough); body=%s", rec.Code, rec.Body.String())
	}
	var body consultDetail
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Source != sourceAudit {
		t.Errorf("source = %q, want %q", body.Source, sourceAudit)
	}
	if body.ComposedPersisted {
		t.Errorf("composed_persisted = true on audit-log record; want false")
	}
	if body.Status != "completed" {
		t.Errorf("status = %q, want completed", body.Status)
	}
	if body.Tokens.Total != 350 {
		t.Errorf("tokens total = %d, want 350 (in+out)", body.Tokens.Total)
	}
	if body.Model != "claude-test" {
		t.Errorf("model = %q, want claude-test", body.Model)
	}
}

func TestGetConsult_AuditFallthroughMarksFailedWhenErrorPresent(t *testing.T) {
	r, d := newConsultsRouter(t)
	skill, _ := r.skillRegistry.Get(epic_order.ID)
	c, err := d.Begin(persona.BeginRequest{
		Persona:   pmPersona(),
		Skill:     skill,
		Workspace: "test-workspace",
		RawInput:  epicOrderRawInput(t),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := d.Finish(c.ID, persona.FinishInfo{
		Error: "spawn failed: backend unreachable",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/"+c.ID, nil))
	var body consultDetail
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "failed" {
		t.Errorf("status = %q, want failed (Error non-empty)", body.Status)
	}
	if body.Error == "" {
		t.Error("error field empty in archived response")
	}
}

func TestGetConsult_StillReturns404WhenAuditMissesToo(t *testing.T) {
	r, _ := newConsultsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/never-existed", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetConsult_BeforeAttachReturns503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/consults/anything", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// Sanity: consultSummary must marshal core.RepositoryID through as a
// plain JSON string (it's a string-typed alias). A future change that
// renames the json tag or wraps the type would break SPA consumers.
func TestConsultSummary_RepositoryIDMarshalsAsString(t *testing.T) {
	s := consultSummary{
		ID:           "consult-1",
		PersonaID:    "pm",
		SkillID:      "x",
		RepositoryID: core.RepositoryID("gemba"),
		Status:       "running",
		StartedAt:    time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatal(err)
	}
	if probe["repository_id"] != "gemba" {
		t.Errorf("repository_id marshaled as %v (%T), want \"gemba\"", probe["repository_id"], probe["repository_id"])
	}
}
