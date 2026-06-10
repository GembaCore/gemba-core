package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/billing"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

func newBillingRig(t *testing.T) (*Router, *billing.Aggregator) {
	t.Helper()
	store := tenant.NewMemStore()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	cfg := config.ServeConfig{Listen: "127.0.0.1"}
	r := NewRouter(cfg, fakeSPA(), nil)
	r.AttachTenantStore(store)
	agg := billing.NewAggregator()
	r.AttachUsageAggregator(agg)
	return r, agg
}

func TestUsageEndpoint_HappyPath(t *testing.T) {
	r, agg := newBillingRig(t)
	agg.Observe(billing.Event{TenantID: string(tenant.DefaultTenant), Kind: "vm.egress.bytes", Bytes: 1024})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t-default/usage", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["tenant_id"] != "t-default" {
		t.Fatalf("want tenant_id=t-default, got %v", body["tenant_id"])
	}
	if v, _ := body["egress_bytes"].(float64); v != 1024 {
		t.Fatalf("want egress_bytes=1024, got %v", body["egress_bytes"])
	}
}

func TestUsageEndpoint_UnauthorizedTenantMismatch(t *testing.T) {
	r, _ := newBillingRig(t)
	// Single-user resolver always answers t-default, so requesting
	// a different tenant id from the same bearer must 403.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t-abcd1234/usage", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUsageEndpoint_NotConfigured(t *testing.T) {
	cfg := config.ServeConfig{Listen: "127.0.0.1"}
	r := NewRouter(cfg, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t-default/usage", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUsageEndpoint_InvalidTenantID(t *testing.T) {
	r, _ := newBillingRig(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/not-a-tenant/usage", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}
