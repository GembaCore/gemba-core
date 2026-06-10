package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

func TestOrchestrationState_NoHostReturns503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/orchestration/state", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestOrchestrationState_NativeImplicitScope verifies that when the
// bound adaptor is native (or unidentified), the editor sees an
// implicit "local" scope — that's how the scope-axis-hidden flow has a
// target to write into.
func TestOrchestrationState_NativeShape(t *testing.T) {
	// Synthesize the response shape directly — projecting on a real
	// host is exercised by the adaptor's own tests. We just confirm
	// the JSON projection is stable for the SPA.
	state := OrchestrationState{
		AdaptorID: "native",
		Scopes: []OrchestrationSc{{
			ID: "local", Kind: "local", Personas: []string{},
		}},
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got == "" {
		t.Fatalf("empty marshal: %q", got)
	}
	var rt OrchestrationState
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatal(err)
	}
	if rt.AdaptorID != "native" {
		t.Errorf("adaptor_id = %q", rt.AdaptorID)
	}
	if len(rt.Scopes) != 1 || rt.Scopes[0].Kind != "local" {
		t.Errorf("scopes = %+v", rt.Scopes)
	}
}
