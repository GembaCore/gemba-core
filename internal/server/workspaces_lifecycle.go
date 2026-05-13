// gm-o9t8.4.2 — workspace lifecycle API.
//
// Surface (mounted on the shared /api/* block, behind apiAuth):
//
//	POST   /api/v1/tenants/{tid}/workspaces                — create
//	DELETE /api/v1/tenants/{tid}/workspaces/{wid}          — soft-delete
//	POST   /api/v1/tenants/{tid}/workspaces/{wid}:restore  — restore
//	POST   /api/v1/tenants/{tid}/workspaces/{wid}:archive  — archive
//
// Soft-delete sets DeletedAt; data is retained for the configured
// soft-delete window (default 30 days). Restore clears DeletedAt.
// Archive is destructive: vault keys are shredded via the configured
// secrets.Vault.Delete, the workspace directory is removed, an audit
// EventWorkspaceArchived record is emitted, and an S3 archive is
// uploaded via the injected S3Uploader (production wiring lands in
// slice (b); the fake implementation here keeps the surface testable).
//
// Tenant resolution mirrors tenants_admin.go: the bearer-bound tenant
// is read from the request context (set by tracemw.WithTenant). When
// the path-supplied {tid} does not match the bearer's tenant the
// handler returns 403 — operators cannot reach other tenants'
// workspaces through this surface.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/egress"
	"github.com/GembaCore/gemba-core/internal/server/audit"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// SoftDeleteRetention is the default retention window for soft-deleted
// workspaces. After this window an external sweeper (out of scope for
// slice a) is expected to archive or hard-delete the row.
const SoftDeleteRetention = 30 * 24 * time.Hour

// WorkspaceRecord is the lifecycle-state row managed by this handler.
// Egress and secrets state live in their own stores; the record here
// carries the lifecycle bookkeeping (creation, soft-delete, archive)
// plus the egress-template name we seeded the workspace with so
// audit consumers can reconstruct the network policy at creation
// time.
type WorkspaceRecord struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	Slug            string     `json:"slug"`
	CreatedAt       time.Time  `json:"created_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	EgressTemplate  string     `json:"egress_template,omitempty"`
	ArchiveLocation string     `json:"archive_location,omitempty"`
}

// IsSoftDeleted reports whether the record's lifecycle is currently
// soft-deleted (DeletedAt set, ArchivedAt unset).
func (w WorkspaceRecord) IsSoftDeleted() bool {
	return w.DeletedAt != nil && w.ArchivedAt == nil
}

// IsArchived reports whether the record has been archived.
func (w WorkspaceRecord) IsArchived() bool { return w.ArchivedAt != nil }

// WorkspaceLifecycleStore persists WorkspaceRecord rows. Implementations
// must be safe for concurrent calls. The slice-a default is the
// in-memory MemWorkspaceLifecycleStore; cmd/gemba serve will swap in a
// Dolt-backed implementation in a follow-up.
type WorkspaceLifecycleStore interface {
	Create(ctx context.Context, w WorkspaceRecord) error
	Get(ctx context.Context, id string) (WorkspaceRecord, error)
	Update(ctx context.Context, w WorkspaceRecord) error
	List(ctx context.Context, tenantID string) ([]WorkspaceRecord, error)
}

// ErrWorkspaceNotFound is returned by stores when the requested id
// is absent.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// MemWorkspaceLifecycleStore is the in-memory default store. Safe for
// concurrent use.
type MemWorkspaceLifecycleStore struct {
	mu      sync.RWMutex
	records map[string]WorkspaceRecord
}

// NewMemWorkspaceLifecycleStore returns an empty in-memory store.
func NewMemWorkspaceLifecycleStore() *MemWorkspaceLifecycleStore {
	return &MemWorkspaceLifecycleStore{records: map[string]WorkspaceRecord{}}
}

// Create implements WorkspaceLifecycleStore.
func (m *MemWorkspaceLifecycleStore) Create(_ context.Context, w WorkspaceRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[w.ID]; ok {
		return errors.New("workspace already exists")
	}
	m.records[w.ID] = w
	return nil
}

// Get implements WorkspaceLifecycleStore.
func (m *MemWorkspaceLifecycleStore) Get(_ context.Context, id string) (WorkspaceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.records[id]
	if !ok {
		return WorkspaceRecord{}, ErrWorkspaceNotFound
	}
	return r, nil
}

// Update implements WorkspaceLifecycleStore.
func (m *MemWorkspaceLifecycleStore) Update(_ context.Context, w WorkspaceRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[w.ID]; !ok {
		return ErrWorkspaceNotFound
	}
	m.records[w.ID] = w
	return nil
}

// List implements WorkspaceLifecycleStore.
func (m *MemWorkspaceLifecycleStore) List(_ context.Context, tenantID string) ([]WorkspaceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]WorkspaceRecord, 0, len(m.records))
	for _, r := range m.records {
		if r.TenantID == tenantID {
			out = append(out, r)
		}
	}
	return out, nil
}

// VaultDeleter narrows secrets.Vault.Delete so the archive path can
// shred per-workspace keys without depending on the full vault surface.
type VaultDeleter interface {
	List(ctx context.Context, wsid string) ([]string, error)
	Delete(ctx context.Context, wsid, key string) error
}

// S3Uploader is the contract the archive handler calls into to push
// the layered workspace bundle to long-term object storage. The
// production implementation wraps an AWS SDK client; tests substitute
// FakeS3Uploader. Out of scope for slice a: the real wiring lives in
// slice (c).
type S3Uploader interface {
	// Upload writes payload at the given key. Returns the canonical
	// remote URL (e.g. s3://bucket/key) the audit record captures.
	Upload(ctx context.Context, key string, payload []byte) (string, error)
}

// FakeS3Uploader is an in-memory test double. Safe for concurrent use.
type FakeS3Uploader struct {
	mu      sync.Mutex
	Bucket  string
	Objects map[string][]byte
}

// NewFakeS3Uploader returns an in-memory S3Uploader stub.
func NewFakeS3Uploader(bucket string) *FakeS3Uploader {
	return &FakeS3Uploader{Bucket: bucket, Objects: map[string][]byte{}}
}

// Upload satisfies S3Uploader.
func (f *FakeS3Uploader) Upload(_ context.Context, key string, payload []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Objects == nil {
		f.Objects = map[string][]byte{}
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.Objects[key] = cp
	bucket := f.Bucket
	if bucket == "" {
		bucket = "fake"
	}
	return "s3://" + bucket + "/" + key, nil
}

// workspaceLifecycleHandler glues the store + vault + s3 + audit +
// egress-template wiring together. Built via NewWorkspaceLifecycle and
// attached to the Router via AttachWorkspaceLifecycle.
type workspaceLifecycleHandler struct {
	store      WorkspaceLifecycleStore
	vault      VaultDeleter
	s3         S3Uploader
	egress     egress.Store
	dataDir    string
	clock      func() time.Time
	idGen      func() string
	retention  time.Duration
}

// WorkspaceLifecycleConfig bundles the optional collaborators for the
// lifecycle handler. Store is required; the rest fall back to nil
// (skipping the corresponding phase) so tests can exercise individual
// transitions without standing up every collaborator.
type WorkspaceLifecycleConfig struct {
	Store     WorkspaceLifecycleStore
	Vault     VaultDeleter
	S3        S3Uploader
	Egress    egress.Store
	DataDir   string
	Retention time.Duration
	Clock     func() time.Time
	IDGen     func() string
}

// NewWorkspaceLifecycle constructs the handler. Returns nil when
// cfg.Store is nil — the Router treats a nil handler as "endpoints
// return 503 adaptor_not_configured".
func NewWorkspaceLifecycle(cfg WorkspaceLifecycleConfig) *workspaceLifecycleHandler {
	if cfg.Store == nil {
		return nil
	}
	h := &workspaceLifecycleHandler{
		store:     cfg.Store,
		vault:     cfg.Vault,
		s3:        cfg.S3,
		egress:    cfg.Egress,
		dataDir:   cfg.DataDir,
		clock:     cfg.Clock,
		idGen:     cfg.IDGen,
		retention: cfg.Retention,
	}
	if h.clock == nil {
		h.clock = func() time.Time { return time.Now().UTC() }
	}
	if h.idGen == nil {
		h.idGen = defaultWorkspaceIDGen
	}
	if h.retention == 0 {
		h.retention = SoftDeleteRetention
	}
	return h
}

// AttachWorkspaceLifecycle wires h as the active lifecycle handler.
// Pass nil to disable the surface (endpoints will return 503).
func (r *Router) AttachWorkspaceLifecycle(h *workspaceLifecycleHandler) {
	r.workspaceLifecycle = h
}

// defaultWorkspaceIDGen produces "ws-<random tenant-style suffix>".
// We reuse tenant.NewID's randomness to avoid pulling in a second
// random source — the prefix swap keeps ws ids visually distinct.
func defaultWorkspaceIDGen() string {
	return "ws-" + string(tenant.NewID())[2:]
}

// createWorkspaceRequest is the POST body shape.
type createWorkspaceRequest struct {
	Slug           string `json:"slug"`
	EgressTemplate string `json:"egress_template,omitempty"`
}

// resolveLifecycleTenant pulls {tid} off the URL, validates it, and
// confirms the bearer-bound tenant matches. Returns the tenant id on
// success; on failure, writes the appropriate error envelope and
// returns the empty string so callers can early-return.
func (r *Router) resolveLifecycleTenant(w http.ResponseWriter, req *http.Request) tenant.ID {
	pathTid, err := tenant.ParseID(chi.URLParam(req, "tid"))
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "invalid_tenant_id", err.Error())
		return ""
	}
	bearer, ok := tenant.FromContext(req.Context())
	if !ok {
		// No bearer-bound tenant — fall through (admin-style surface).
		// We still require the path tid to be well-formed.
		return pathTid
	}
	// Mirror tenants_admin.go: any valid bearer is admin in v1. We
	// still require the path tenant to either match the bearer or
	// be the DefaultTenant (single-user / M1 backwards compat).
	if pathTid != bearer && pathTid != tenant.DefaultTenant && bearer != tenant.DefaultTenant {
		httperr.Write(w, http.StatusForbidden, "tenant_mismatch",
			"bearer-bound tenant does not match the path tenant")
		return ""
	}
	return pathTid
}

// createWorkspace handles POST /api/v1/tenants/{tid}/workspaces.
func (r *Router) createWorkspace(w http.ResponseWriter, req *http.Request) {
	h := r.workspaceLifecycle
	if h == nil {
		httperr.Write(w, http.StatusServiceUnavailable, "adaptor_not_configured",
			"workspace lifecycle store not attached")
		return
	}
	tid := r.resolveLifecycleTenant(w, req)
	if tid == "" {
		return
	}
	var body createWorkspaceRequest
	if err := decodeTenantJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.Slug == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "slug required")
		return
	}
	rec := WorkspaceRecord{
		ID:        h.idGen(),
		TenantID:  string(tid),
		Slug:      body.Slug,
		CreatedAt: h.clock(),
	}
	tpl, err := r.SeedWorkspaceEgress(req.Context(), h.egress, rec.ID, body.EgressTemplate)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError, "egress_seed_failed", err.Error())
		return
	}
	rec.EgressTemplate = tpl.Name
	if err := h.store.Create(req.Context(), rec); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventTenantUpdate, map[string]any{
		"tenant_id":       string(tid),
		"workspace_id":    rec.ID,
		"event":           "workspace.create",
		"egress_template": rec.EgressTemplate,
	})
	writeJSON(w, http.StatusCreated, rec)
}

// softDeleteWorkspace handles DELETE /api/v1/tenants/{tid}/workspaces/{wid}.
func (r *Router) softDeleteWorkspace(w http.ResponseWriter, req *http.Request) {
	h := r.workspaceLifecycle
	if h == nil {
		httperr.Write(w, http.StatusServiceUnavailable, "adaptor_not_configured",
			"workspace lifecycle store not attached")
		return
	}
	tid := r.resolveLifecycleTenant(w, req)
	if tid == "" {
		return
	}
	wid := chi.URLParam(req, "wid")
	rec, err := h.store.Get(req.Context(), wid)
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	if rec.TenantID != string(tid) {
		httperr.Write(w, http.StatusForbidden, "tenant_mismatch",
			"workspace belongs to a different tenant")
		return
	}
	if rec.IsArchived() {
		httperr.Write(w, http.StatusConflict, "workspace_archived",
			"workspace is already archived")
		return
	}
	now := h.clock()
	rec.DeletedAt = &now
	if err := h.store.Update(req.Context(), rec); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventTenantUpdate, map[string]any{
		"tenant_id":         string(tid),
		"workspace_id":      rec.ID,
		"event":             "workspace.soft_delete",
		"retention_seconds": int(h.retention.Seconds()),
	})
	writeJSON(w, http.StatusOK, rec)
}

// restoreWorkspace handles POST /api/v1/tenants/{tid}/workspaces/{wid}:restore.
func (r *Router) restoreWorkspace(w http.ResponseWriter, req *http.Request) {
	h := r.workspaceLifecycle
	if h == nil {
		httperr.Write(w, http.StatusServiceUnavailable, "adaptor_not_configured",
			"workspace lifecycle store not attached")
		return
	}
	tid := r.resolveLifecycleTenant(w, req)
	if tid == "" {
		return
	}
	wid := chi.URLParam(req, "wid")
	rec, err := h.store.Get(req.Context(), wid)
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	if rec.TenantID != string(tid) {
		httperr.Write(w, http.StatusForbidden, "tenant_mismatch",
			"workspace belongs to a different tenant")
		return
	}
	if rec.IsArchived() {
		httperr.Write(w, http.StatusConflict, "workspace_archived",
			"archived workspaces cannot be restored")
		return
	}
	if !rec.IsSoftDeleted() {
		httperr.Write(w, http.StatusConflict, "workspace_active",
			"workspace is not soft-deleted")
		return
	}
	// Enforce the retention window — once the workspace has been
	// soft-deleted longer than retention, restore is no longer
	// permitted (the row is awaiting archive).
	if h.clock().Sub(*rec.DeletedAt) > h.retention {
		httperr.Write(w, http.StatusConflict, "retention_expired",
			"workspace soft-delete retention window has expired")
		return
	}
	rec.DeletedAt = nil
	if err := h.store.Update(req.Context(), rec); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventTenantUpdate, map[string]any{
		"tenant_id":    string(tid),
		"workspace_id": rec.ID,
		"event":        "workspace.restore",
	})
	writeJSON(w, http.StatusOK, rec)
}

// archiveWorkspace handles POST /api/v1/tenants/{tid}/workspaces/{wid}:archive.
//
// Phases (each one audited via EventWorkspaceArchived):
//
//  1. Shred per-workspace vault keys (List + Delete each key).
//  2. Upload a manifest payload to S3 via the injected uploader.
//  3. Remove the on-disk workspace directory under DataDir.
//  4. Mark the record archived (ArchivedAt + ArchiveLocation).
func (r *Router) archiveWorkspace(w http.ResponseWriter, req *http.Request) {
	h := r.workspaceLifecycle
	if h == nil {
		httperr.Write(w, http.StatusServiceUnavailable, "adaptor_not_configured",
			"workspace lifecycle store not attached")
		return
	}
	tid := r.resolveLifecycleTenant(w, req)
	if tid == "" {
		return
	}
	wid := chi.URLParam(req, "wid")
	rec, err := h.store.Get(req.Context(), wid)
	if err != nil {
		writeLifecycleError(w, err)
		return
	}
	if rec.TenantID != string(tid) {
		httperr.Write(w, http.StatusForbidden, "tenant_mismatch",
			"workspace belongs to a different tenant")
		return
	}
	if rec.IsArchived() {
		httperr.Write(w, http.StatusConflict, "workspace_archived",
			"workspace is already archived")
		return
	}

	// Phase 1 — shred vault keys.
	shreddedKeys := 0
	if h.vault != nil {
		keys, err := h.vault.List(req.Context(), rec.ID)
		if err != nil {
			httperr.Write(w, http.StatusInternalServerError, "vault_list_failed", err.Error())
			return
		}
		for _, k := range keys {
			if err := h.vault.Delete(req.Context(), rec.ID, k); err != nil {
				httperr.Write(w, http.StatusInternalServerError, "vault_delete_failed", err.Error())
				return
			}
			shreddedKeys++
		}
	}
	r.appendTenantAudit(req.Context(), audit.EventWorkspaceArchived, map[string]any{
		"tenant_id":     string(tid),
		"workspace_id":  rec.ID,
		"phase":         "vault_shred",
		"shredded_keys": shreddedKeys,
	})

	// Phase 2 — upload manifest to S3.
	now := h.clock()
	location := ""
	if h.s3 != nil {
		manifest, _ := json.Marshal(map[string]any{
			"workspace_id": rec.ID,
			"tenant_id":    rec.TenantID,
			"slug":         rec.Slug,
			"archived_at":  now.Format(time.RFC3339Nano),
		})
		key := "workspaces/" + rec.TenantID + "/" + rec.ID + "/manifest.json"
		loc, err := h.s3.Upload(req.Context(), key, manifest)
		if err != nil {
			httperr.Write(w, http.StatusInternalServerError, "s3_upload_failed", err.Error())
			return
		}
		location = loc
	}
	r.appendTenantAudit(req.Context(), audit.EventWorkspaceArchived, map[string]any{
		"tenant_id":    string(tid),
		"workspace_id": rec.ID,
		"phase":        "s3_upload",
		"location":     location,
	})

	// Phase 3 — remove on-disk repo. Best-effort: a missing dir is
	// not a fatal error (test environments may never have created one).
	if h.dataDir != "" {
		dir := filepath.Join(h.dataDir, rec.ID)
		if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusInternalServerError, "fs_remove_failed", err.Error())
			return
		}
	}
	r.appendTenantAudit(req.Context(), audit.EventWorkspaceArchived, map[string]any{
		"tenant_id":    string(tid),
		"workspace_id": rec.ID,
		"phase":        "fs_remove",
	})

	// Phase 4 — finalize.
	rec.ArchivedAt = &now
	rec.ArchiveLocation = location
	// Soft-delete is implied once archived; carry it forward if not
	// already set so audit consumers see a complete timeline.
	if rec.DeletedAt == nil {
		rec.DeletedAt = &now
	}
	if err := h.store.Update(req.Context(), rec); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventWorkspaceArchived, map[string]any{
		"tenant_id":    string(tid),
		"workspace_id": rec.ID,
		"phase":        "finalize",
		"location":     location,
	})
	writeJSON(w, http.StatusOK, rec)
}

// writeLifecycleError maps store errors to the standard envelope. Pulled
// out so every handler reuses the same mapping (NotFound → 404,
// everything else → 500).
func writeLifecycleError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrWorkspaceNotFound) {
		httperr.Write(w, http.StatusNotFound, "workspace_not_found", err.Error())
		return
	}
	httperr.Write(w, http.StatusInternalServerError, "store_error", err.Error())
}
