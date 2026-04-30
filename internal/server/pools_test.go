package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

func TestListPools_NoConfigReturnsEmpty(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body PoolState
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Pools) != 0 {
		t.Errorf("expected no pools (zero-delta); got %d", len(body.Pools))
	}
	if body.CapturedAt.IsZero() {
		t.Errorf("captured_at should be populated")
	}
}

func TestListPools_DeclaredAndEffectiveSurfaced(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPools([]config.ResolvedPool{
		{
			Rig:            "gemba",
			Persona:        "engineer-claude",
			SizeDeclared:   5,
			SizeEffective:  3,
			ClampActivated: true,
			Floor:          0.5,
		},
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body PoolState
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(body.Pools))
	}
	p := body.Pools[0]
	if p.SizeTargetDeclared != 5 || p.SizeTargetEffective != 3 {
		t.Errorf("declared=%d effective=%d, want 5/3", p.SizeTargetDeclared, p.SizeTargetEffective)
	}
	if p.Persona != "engineer-claude" {
		t.Errorf("persona = %q", p.Persona)
	}
	if p.SizeActual != 0 {
		t.Errorf("size_actual = %d, want 0 (no host)", p.SizeActual)
	}
	if p.Members == nil {
		t.Errorf("members should be non-nil array (got null in JSON)")
	}
}

func TestListPools_V1Alias(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPools([]config.ResolvedPool{
		{Rig: "gemba", Persona: "engineer-claude", SizeDeclared: 1, SizeEffective: 1},
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("v1 alias status = %d", rec.Code)
	}
	var body PoolState
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Pools) != 1 {
		t.Errorf("v1 alias should return same shape; got %d pools", len(body.Pools))
	}
}
