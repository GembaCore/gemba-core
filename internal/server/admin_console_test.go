package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server/audit"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// newAdminConsoleRig returns a Router with the admin console wired
// against an in-memory tenant store, a static session lister, and a
// real audit Logger (so verify exercises the production code path).
func newAdminConsoleRig(t *testing.T, adminEmails []string) (*Router, *audit.Logger) {
	t.Helper()
	store := tenant.NewMemStore()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	// Seed a couple of tenants beyond the auto-created default.
	_ = store.Create(context.Background(), tenant.Tenant{ID: "t-aaaa1111", Kind: tenant.KindUser})
	_ = store.Create(context.Background(), tenant.Tenant{ID: "t-bbbb2222", Kind: tenant.KindOrg})

	pub, priv, err := audit.GenerateKey()
	if err != nil {
		t.Fatalf("audit.GenerateKey: %v", err)
	}
	logger, err := audit.Open(filepath.Join(t.TempDir(), "audit.log"), priv)
	if err != nil {
		t.Fatalf("audit.Open: %v", err)
	}
	if _, err := logger.Append(context.Background(), "", "", audit.EventTenantCreate, map[string]string{"x": "y"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := logger.Append(context.Background(), "", "", audit.EventTenantCreate, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	cfg := config.ServeConfig{Listen: "127.0.0.1"}
	r := NewRouter(cfg, fakeSPA(), nil)
	r.AttachTenantStore(store)

	lister := AdminSessionLister(func(_ context.Context) ([]AdminSession, error) {
		return []AdminSession{
			{ID: "s1", TenantID: "t-aaaa1111", Status: "working", StartedAt: time.Now()},
			{ID: "s2", TenantID: "t-bbbb2222", Status: "ready", StartedAt: time.Now()},
		}, nil
	})
	r.AttachAdminConsole(adminEmails, lister, pub, func(_ context.Context) ([]audit.Record, error) {
		return logger.Read(0)
	})
	return r, logger
}

func TestAdminConsole_ListTenants(t *testing.T) {
	r, _ := newAdminConsoleRig(t, nil) // empty admin list ⇒ open
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tenants []tenantResponse `json:"tenants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tenants) < 2 {
		t.Fatalf("want >=2 tenants, got %d", len(body.Tenants))
	}
}

func TestAdminConsole_ListSessions(t *testing.T) {
	r, _ := newAdminConsoleRig(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/sessions", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Sessions []AdminSession `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(body.Sessions))
	}
}

func TestAdminConsole_AuditVerifyOK(t *testing.T) {
	r, _ := newAdminConsoleRig(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit/verify", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("want ok=true, got %v", body)
	}
}

func TestAdminConsole_AuditVerifyBrokenSeq(t *testing.T) {
	r, logger := newAdminConsoleRig(t, nil)
	// Swap the records loader with one that mutates a record so Verify
	// fails and broken_seq is reported.
	r.AttachAdminConsole(nil, nil, nil, func(_ context.Context) ([]audit.Record, error) {
		recs, err := logger.Read(0)
		if err != nil || len(recs) == 0 {
			return recs, err
		}
		// Corrupt the signature on the second record so it breaks
		// strictly after the first OK record.
		if len(recs) >= 2 {
			recs[1].Sig = "AAAA"
		}
		return recs, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit/verify", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("want ok=false, got %v", body)
	}
	if v, _ := body["broken_seq"].(float64); v != 2 {
		t.Fatalf("want broken_seq=2, got %v", body["broken_seq"])
	}
}

func TestAdminConsole_AuthRejection(t *testing.T) {
	r, _ := newAdminConsoleRig(t, []string{"ops@example.com"})

	// No header → 401
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Wrong email → 403
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants", nil)
	req.Header.Set(adminEmailHeader, "intruder@example.com")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Allowed email → 200
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants", nil)
	req.Header.Set(adminEmailHeader, "ops@example.com")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminConsole_AuditVerifyUnconfigured(t *testing.T) {
	cfg := config.ServeConfig{Listen: "127.0.0.1"}
	r := NewRouter(cfg, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit/verify", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Compile-time: ensure ed25519.PublicKey reference flows correctly.
var _ ed25519.PublicKey = ed25519.PublicKey(nil)

// silence unused err
var _ = errors.New
