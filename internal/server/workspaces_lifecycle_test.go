package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/egress"
	"github.com/GembaCore/gemba-core/internal/server/audit"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// fakeVault is the test double for VaultDeleter. The lifecycle archive
// path exercises List + Delete on each key; we record both so tests
// can assert the shred happened.
type fakeVault struct {
	mu      sync.Mutex
	keys    map[string][]string
	deletes []string
}

func newFakeVault(seed map[string][]string) *fakeVault {
	return &fakeVault{keys: seed}
}

func (f *fakeVault) List(_ context.Context, wsid string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.keys[wsid]))
	copy(out, f.keys[wsid])
	return out, nil
}

func (f *fakeVault) Delete(_ context.Context, wsid, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, wsid+"/"+key)
	left := f.keys[wsid][:0]
	for _, k := range f.keys[wsid] {
		if k != key {
			left = append(left, k)
		}
	}
	f.keys[wsid] = left
	return nil
}

// newLifecycleRouter builds a router with a chi mux wired to the four
// lifecycle endpoints and the bearer-bound tenant stamped on every
// request via withTenantCtx.
func newLifecycleRouter(t *testing.T, bearer tenant.ID, h *workspaceLifecycleHandler) http.Handler {
	t.Helper()
	r := &Router{}
	r.auditor = audit.NewMemAuditor()
	r.AttachWorkspaceLifecycle(h)

	mux := chi.NewRouter()
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := tenant.WithContext(req.Context(), bearer)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	mux.Route("/api/v1/tenants/{tid}/workspaces", func(api chi.Router) {
		api.Post("/", r.createWorkspace)
		api.Delete("/{wid}", r.softDeleteWorkspace)
		api.Post("/{wid}:restore", r.restoreWorkspace)
		api.Post("/{wid}:archive", r.archiveWorkspace)
	})
	return mux
}

func TestLifecycle_CreateHappyPath(t *testing.T) {
	store := NewMemWorkspaceLifecycleStore()
	egstore := egress.NewMemStore()
	h := NewWorkspaceLifecycle(WorkspaceLifecycleConfig{
		Store:  store,
		Egress: egstore,
		Clock:  func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		IDGen:  func() string { return "ws-test01" },
	})
	mux := newLifecycleRouter(t, tenant.DefaultTenant, h)

	body := strings.NewReader(`{"slug":"my-project","egress_template":"github-only"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t-default/workspaces/", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var rec WorkspaceRecord
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.ID != "ws-test01" || rec.TenantID != "t-default" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.EgressTemplate != "github-only" {
		t.Fatalf("expected egress_template=github-only, got %q", rec.EgressTemplate)
	}
	// Seeded rules should be present in the egress store.
	rules, err := egstore.List(context.Background(), "ws-test01")
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected seeded rules from template")
	}
}

func TestLifecycle_CreateDefaultTemplateAliasesGithubOnly(t *testing.T) {
	store := NewMemWorkspaceLifecycleStore()
	egstore := egress.NewMemStore()
	h := NewWorkspaceLifecycle(WorkspaceLifecycleConfig{
		Store:  store,
		Egress: egstore,
		IDGen:  func() string { return "ws-defalt" },
	})
	mux := newLifecycleRouter(t, tenant.DefaultTenant, h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t-default/workspaces/",
		strings.NewReader(`{"slug":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var rec WorkspaceRecord
	_ = json.Unmarshal(w.Body.Bytes(), &rec)
	if rec.EgressTemplate != "github-only" {
		t.Fatalf("default template should alias github-only, got %q", rec.EgressTemplate)
	}
}

func TestLifecycle_SoftDeleteAndRestore(t *testing.T) {
	store := NewMemWorkspaceLifecycleStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }
	h := NewWorkspaceLifecycle(WorkspaceLifecycleConfig{
		Store: store,
		Clock: clock,
		IDGen: func() string { return "ws-soft01" },
	})
	mux := newLifecycleRouter(t, tenant.DefaultTenant, h)

	// Create.
	cw := httptest.NewRecorder()
	mux.ServeHTTP(cw, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t-default/workspaces/",
		strings.NewReader(`{"slug":"s"}`)))
	if cw.Code != http.StatusCreated {
		t.Fatalf("create: %d", cw.Code)
	}

	// Soft-delete.
	dw := httptest.NewRecorder()
	mux.ServeHTTP(dw, httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t-default/workspaces/ws-soft01", nil))
	if dw.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", dw.Code, dw.Body.String())
	}
	rec, _ := store.Get(context.Background(), "ws-soft01")
	if !rec.IsSoftDeleted() {
		t.Fatalf("expected soft-deleted; got %+v", rec)
	}

	// Restore.
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t-default/workspaces/ws-soft01:restore", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("restore: %d body=%s", rw.Code, rw.Body.String())
	}
	rec, _ = store.Get(context.Background(), "ws-soft01")
	if rec.IsSoftDeleted() {
		t.Fatalf("expected restored; got %+v", rec)
	}
}

func TestLifecycle_ArchiveHappyPath(t *testing.T) {
	store := NewMemWorkspaceLifecycleStore()
	vault := newFakeVault(map[string][]string{"ws-arc001": {"key1", "key2"}})
	s3 := NewFakeS3Uploader("test-bucket")
	h := NewWorkspaceLifecycle(WorkspaceLifecycleConfig{
		Store: store,
		Vault: vault,
		S3:    s3,
		IDGen: func() string { return "ws-arc001" },
	})
	mux := newLifecycleRouter(t, tenant.DefaultTenant, h)

	// Create + archive.
	cw := httptest.NewRecorder()
	mux.ServeHTTP(cw, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t-default/workspaces/",
		strings.NewReader(`{"slug":"arc"}`)))
	if cw.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", cw.Code, cw.Body.String())
	}

	aw := httptest.NewRecorder()
	mux.ServeHTTP(aw, httptest.NewRequest(http.MethodPost,
		"/api/v1/tenants/t-default/workspaces/ws-arc001:archive", nil))
	if aw.Code != http.StatusOK {
		t.Fatalf("archive: %d body=%s", aw.Code, aw.Body.String())
	}
	if len(vault.deletes) != 2 {
		t.Fatalf("expected 2 vault shreds, got %v", vault.deletes)
	}
	if len(s3.Objects) != 1 {
		t.Fatalf("expected 1 s3 object, got %d", len(s3.Objects))
	}
	rec, _ := store.Get(context.Background(), "ws-arc001")
	if !rec.IsArchived() {
		t.Fatalf("expected archived; got %+v", rec)
	}
	if !strings.HasPrefix(rec.ArchiveLocation, "s3://test-bucket/") {
		t.Fatalf("unexpected archive location %q", rec.ArchiveLocation)
	}
}

func TestLifecycle_UnauthorizedTenantRejection(t *testing.T) {
	store := NewMemWorkspaceLifecycleStore()
	h := NewWorkspaceLifecycle(WorkspaceLifecycleConfig{
		Store: store,
		IDGen: func() string { return "ws-other1" },
	})
	// Pre-seed a workspace belonging to t-other001.
	_ = store.Create(context.Background(), WorkspaceRecord{
		ID:        "ws-other1",
		TenantID:  "t-aaaaaaaa",
		Slug:      "x",
		CreatedAt: time.Now(),
	})
	// Bearer is bound to a different tenant.
	mux := newLifecycleRouter(t, tenant.ID("t-bbbbbbbb"), h)

	// Soft-delete attempt against the path tenant "t-aaaaaaaa" with a
	// bearer for "t-bbbbbbbb" must be rejected with 403.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete,
		"/api/v1/tenants/t-aaaaaaaa/workspaces/ws-other1", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLifecycle_NotConfigured503(t *testing.T) {
	mux := newLifecycleRouter(t, tenant.DefaultTenant, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t-default/workspaces/",
		strings.NewReader(`{"slug":"x"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
