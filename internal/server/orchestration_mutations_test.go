package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// TestCreateScope_NativeKindUnsupported confirms the native branch
// returns 400 KindUnsupported (defense-in-depth — the SPA hides the
// button on native, but we still gate server-side).
func TestCreateScope_NativeKindUnsupported(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/scopes",
		bytes.NewBufferString(`{"name":"foo"}`))
	req.Header.Set(ConfirmHeader, "n1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// adaptorID == "" (no host) → falls through requireGastown's
	// not-gastown branch → 400 kind_unsupported.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "kind_unsupported") {
		t.Errorf("expected kind_unsupported, got %s", rec.Body.String())
	}
}

func TestCreatePolecat_NativeKindUnsupported(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/polecats",
		bytes.NewBufferString(`{"rig":"gemba","persona":"engineer"}`))
	req.Header.Set(ConfirmHeader, "n2")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestPutSchedulerConfig_NativeKindUnsupported(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodPut, "/api/orchestration/scheduler-config",
		bytes.NewBufferString(`{"max_polecats":5}`))
	req.Header.Set(ConfirmHeader, "n3")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestGetSchedulerConfig_NativeReturnsEmpty(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/orchestration/scheduler-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body schedulerConfig
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.MaxPolecats != 0 {
		t.Errorf("max_polecats = %d want 0 (native default)", body.MaxPolecats)
	}
}

// TestCreateScope_RunnerInjection confirms SetGTRunner threads the
// fake through to the handler. We can't easily simulate a "gastown"
// adaptor without a full host; instead we exercise the runner shape
// directly to verify it's plumbed.
func TestCreateScope_RunnerInjection(t *testing.T) {
	called := false
	SetGTRunner(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		called = true
		return []byte("ok\n"), nil, nil
	})
	t.Cleanup(func() { SetGTRunner(nil) })
	got := currentGTRunner()
	if got == nil {
		t.Fatal("runner not installed")
	}
	_, _, _ = got(context.Background(), "", "rig", "create", "test")
	if !called {
		t.Errorf("runner did not invoke")
	}
}
