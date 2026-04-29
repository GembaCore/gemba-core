package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
)

// gm-e12.8: /api/agent-groups wraps OrchestrationPlaneAdaptor.ListGroups
// and returns the envelope shape {groups, total}. Same convention as
// /api/agents.
//
// FakeOrchestrationPlane.ListGroups returns nil today; we use a
// shimming wrapper to inject groups for the happy-path test.
func TestListAgentGroups_HappyPath(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	op := &fakeOrchWithGroups{
		FakeOrchestrationPlane: testadaptors.NewFakeOrchestrationPlane(core.TransportAPI),
		groups: []core.AgentGroup{
			{
				ID:          "role:mayor",
				DisplayName: "Mayor",
				Mode:        core.GroupStatic,
				Members:     core.GroupMembers{Static: []core.AgentID{"gemba/hq/mayor"}},
				Extension:   map[string]any{"role": "mayor"},
			},
			{
				ID:          "rig:hq/polecats",
				DisplayName: "hq polecats",
				Mode:        core.GroupPool,
				Members:     core.GroupMembers{PoolCheckURL: "gt://polecat/list?rig=hq"},
				Repository:  []string{"hq"},
				Extension:   map[string]any{"role": "polecats", "rig": "hq"},
			},
		},
	}
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/agent-groups", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Groups []core.AgentGroup `json:"groups"`
		Total  int               `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 2 || len(env.Groups) != 2 {
		t.Fatalf("envelope shape mismatch: %+v", env)
	}
	if env.Groups[0].Mode != core.GroupStatic {
		t.Fatalf("first group: %+v", env.Groups[0])
	}
	if env.Groups[1].Mode != core.GroupPool {
		t.Fatalf("second group: %+v", env.Groups[1])
	}
}

// No orchestration plane bound → 200 with empty list. The SPA's
// AgentGroupsPage shows the empty state rather than a degraded banner.
func TestListAgentGroups_NoOrchestration_Returns200Empty(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/agent-groups", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), `"groups":[]`) {
		t.Fatalf("want empty groups array; body=%q", rec.Body.String())
	}
}

// fakeOrchWithGroups is a thin wrapper that lets the test override
// ListGroups on top of the shared FakeOrchestrationPlane. The fake's
// ListGroups isn't programmable today (only AgentsFn / SessionsFn are),
// so this shim adds the hook locally rather than mutating the shared
// fixture.
type fakeOrchWithGroups struct {
	*testadaptors.FakeOrchestrationPlane
	groups []core.AgentGroup
}

func (f *fakeOrchWithGroups) ListGroups(context.Context) ([]core.AgentGroup, error) {
	return f.groups, nil
}
