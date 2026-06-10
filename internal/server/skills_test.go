package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
	"github.com/GembaCore/gemba-core/internal/skills/epic_order"
)

func newSkillsRouter(t *testing.T) (*Router, *corepersona.SkillRegistry) {
	t.Helper()
	sr := corepersona.NewSkillRegistry()
	if err := epic_order.Register(sr); err != nil {
		t.Fatalf("epic_order.Register: %v", err)
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonaDispatcher(nil, sr, nil)
	return r, sr
}

func TestListSkills_BeforeAttachReturnsEmpty(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no-skills empty list)", rec.Code)
	}
	var body struct {
		Skills []skillSummary `json:"skills"`
		Total  int            `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 0 || len(body.Skills) != 0 {
		t.Errorf("expected empty list, got %+v", body)
	}
}

func TestListSkills_RegisteredSurface(t *testing.T) {
	r, _ := newSkillsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Skills []skillSummary `json:"skills"`
		Total  int            `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 {
		t.Fatalf("total = %d, want 1", body.Total)
	}
	row := body.Skills[0]
	if row.ID != epic_order.ID {
		t.Errorf("id = %q, want %q", row.ID, epic_order.ID)
	}
	if row.OutputToolName == "" {
		t.Error("output_tool_name empty; epic_order ships an MCP tool")
	}
	if !row.HasOutputSchema {
		t.Error("has_output_schema = false; epic_order ships a JSON schema")
	}
}

func TestGetSkill_RegisteredID(t *testing.T) {
	r, _ := newSkillsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills/"+epic_order.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var detail skillDetail
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != epic_order.ID {
		t.Errorf("id = %q, want %q", detail.ID, epic_order.ID)
	}
}

func TestGetSkill_UnknownReturns404(t *testing.T) {
	r, _ := newSkillsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills/no-such-skill", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetSkill_BeforeAttachReturns503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills/"+epic_order.ID, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (registry not configured)", rec.Code)
	}
}

func TestGetSkillOutputSchema_ReturnsParseableJSONSchema(t *testing.T) {
	r, _ := newSkillsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills/"+epic_order.ID+"/output_schema.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/schema+json" {
		t.Errorf("Content-Type = %q, want application/schema+json", got)
	}
	var schema map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&schema); err != nil {
		t.Fatalf("schema body did not parse as JSON: %v", err)
	}
	if len(schema) == 0 {
		t.Error("schema body is empty; epic_order ships a populated InputSchema")
	}
}

func TestGetSkillOutputSchema_UnknownReturns404(t *testing.T) {
	r, _ := newSkillsRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills/no-such/output_schema.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
