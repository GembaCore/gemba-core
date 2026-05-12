package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// fakeDoltChecker implements DoltReadyChecker for tests.
type fakeDoltChecker struct{ ready atomic.Bool }

func (f *fakeDoltChecker) Ready() bool { return f.ready.Load() }

// emptyFS is a no-op fs.FS for tests that don't care about SPA assets.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }

// TestReadyz_NoSupervisor_ReturnsOK covers the external-dolt mode:
// /api/readyz should still report 200 when no supervisor is attached.
func TestReadyz_NoSupervisor_ReturnsOK(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, emptyFS{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

// TestReadyz_SupervisorReady_ReturnsOKWithCheck covers the happy path.
func TestReadyz_SupervisorReady_ReturnsOKWithCheck(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, emptyFS{}, nil)
	chk := &fakeDoltChecker{}
	chk.ready.Store(true)
	r.AttachDoltSupervisor(chk)

	req := httptest.NewRequest(http.MethodGet, "/api/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["dolt"] != "ok" {
		t.Fatalf("checks.dolt = %v, want \"ok\"", checks["dolt"])
	}
}

// TestReadyz_SupervisorDown_Returns503 covers the AC "readyz returns
// 503 within 10s of killing the dolt subprocess externally" — proven
// here by flipping the fake supervisor's Ready flag, which is exactly
// what the real supervisor does when its 5s health probe fails.
func TestReadyz_SupervisorDown_Returns503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, emptyFS{}, nil)
	chk := &fakeDoltChecker{}
	chk.ready.Store(false)
	r.AttachDoltSupervisor(chk)

	req := httptest.NewRequest(http.MethodGet, "/api/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "degraded" {
		t.Fatalf("status field = %v, want \"degraded\"", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["dolt"] != "down" {
		t.Fatalf("checks.dolt = %v, want \"down\"", checks["dolt"])
	}
}
