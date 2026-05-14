package workspaces

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// MemStore is the in-memory Registry implementation. Used by tests and
// by the single-user fallback when no Dolt pool is wired. Concurrent
// safe via a single RW mutex; the workspace count is small enough that
// finer-grained locking isn't worth the complexity.
type MemStore struct {
	mu   sync.RWMutex
	rows map[string]*Workspace // key: full canonical wsid
	now  func() time.Time
}

// NewMemStore returns a fresh in-memory registry. The clock is
// time.Now.UTC; tests override via SetClock.
func NewMemStore() *MemStore {
	return &MemStore{
		rows: map[string]*Workspace{},
		now:  func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the clock used for CreatedAt. Tests only.
func (m *MemStore) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

// Resolve implements Registry.
func (m *MemStore) Resolve(_ context.Context, wsid string) (*Workspace, error) {
	w, _, err := canonicalWSID(wsid)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Walk the table: match by (prefix-of-tenant_id == w.Tenant) and slug.
	// O(N) is fine — the workspace count is tiny in practice and tests
	// run with single-digit rows.
	prefix := string(w.Tenant)
	for _, row := range m.rows {
		if row.Slug != w.Slug {
			continue
		}
		if tenant.ID(row.TenantID).Prefix() != prefix {
			continue
		}
		out := *row
		return &out, nil
	}
	return nil, ErrNotFound
}

// List implements Registry.
func (m *MemStore) List(_ context.Context, opts ListOpts) ([]*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	ids := make([]string, 0, len(m.rows))
	for id := range m.rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Workspace, 0, limit)
	for _, id := range ids {
		if opts.Cursor != "" && id <= opts.Cursor {
			continue
		}
		row := m.rows[id]
		if opts.TenantID != "" && row.TenantID != opts.TenantID {
			continue
		}
		if !opts.IncludeArchived && row.ArchivedAt != nil {
			continue
		}
		copy := *row
		out = append(out, &copy)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Create implements Registry.
func (m *MemStore) Create(_ context.Context, in CreateInput) (*Workspace, error) {
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	tid, err := tenant.ParseID(in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	wsid := tid.Prefix() + ":" + in.Slug

	m.mu.Lock()
	defer m.mu.Unlock()
	// Check existing row by (tenant, slug). The wsid alone isn't
	// enough — two tenants whose prefix collides would clash, though
	// today the 8-char body avoids that in practice.
	for _, row := range m.rows {
		if row.TenantID == in.TenantID && row.Slug == in.Slug {
			return nil, ErrAlreadyExists
		}
	}
	w := &Workspace{
		ID:             wsid,
		TenantID:       in.TenantID,
		Slug:           in.Slug,
		ProjectPath:    in.ProjectPath,
		CreatedAt:      m.now(),
		EgressTemplate: in.EgressTemplate,
		LayoutVersion:  1,
	}
	m.rows[wsid] = w
	out := *w
	return &out, nil
}

// Delete implements Registry.
func (m *MemStore) Delete(_ context.Context, wsid string) error {
	w, _, err := canonicalWSID(wsid)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := string(w.Tenant)
	for id, row := range m.rows {
		if row.Slug != w.Slug {
			continue
		}
		if tenant.ID(row.TenantID).Prefix() != prefix {
			continue
		}
		delete(m.rows, id)
		return nil
	}
	return ErrNotFound
}

// validateCreate enforces the minimal shape rules every Create implementation
// shares. Pulled out so SQLStore can reuse it before issuing INSERT.
func validateCreate(in CreateInput) error {
	if strings.TrimSpace(in.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.Slug) == "" {
		return fmt.Errorf("%w: slug required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.ProjectPath) == "" {
		return fmt.Errorf("%w: project_path required", ErrInvalidInput)
	}
	return nil
}
