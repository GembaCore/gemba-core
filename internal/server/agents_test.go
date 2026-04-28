package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// gm-root.10: /api/agents wraps OrchestrationPlaneAdaptor.ListAgents.
// Returns the envelope shape the AssigneePicker / OwnerPicker hooks
// off of.
func TestListAgents_HappyPath(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	dialect := "claude"
	op.AgentsFn = func(_ context.Context, _ core.AgentFilter) ([]core.AgentRef, error) {
		return []core.AgentRef{
			{ID: "gemba/crew/mike", Name: "mike", Kind: core.AgentKindHuman},
			{ID: "gemba/polecats/obsidian", Name: "obsidian", Kind: core.AgentKindAgent, Dialect: &dialect},
		}, nil
	}
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Agents []core.AgentRef `json:"agents"`
		Total  int             `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 2 || len(env.Agents) != 2 {
		t.Fatalf("envelope shape mismatch: %+v", env)
	}
	if env.Agents[0].Name != "mike" {
		t.Fatalf("first agent: %+v", env.Agents[0])
	}
}

// No orchestration plane bound → 200 with empty list (the SPA's
// freeform AgentEditor remains the fallback). 503 here would block the
// drawer entirely in the common single-plane M2 deployment.
func TestListAgents_NoOrchestration_Returns200Empty(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), `"agents":[]`) {
		t.Fatalf("want empty agents array; body=%q", rec.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
