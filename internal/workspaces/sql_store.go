package workspaces

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// SQLStore is the production Registry, backed by the same Dolt pool
// the tenant store uses. The first call to Migrate creates the
// `workspaces` table; subsequent calls are no-ops. Schema mirrors the
// gm-o9t8.2.4 design — see createWorkspacesTable for the DDL.
type SQLStore struct {
	db  *sql.DB
	now func() time.Time
}

// NewSQLStore wraps db. The pool's lifecycle is the caller's.
func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock overrides the clock; tests only.
func (s *SQLStore) SetClock(now func() time.Time) { s.now = now }

// createWorkspacesTable is the gm-o9t8.2.4 DDL. Idempotent; safe to run
// on every boot. The unique index on (tenant_id, slug) lets two
// tenants carry the same slug without collision; the secondary index
// on (tenant_id, archived_at) accelerates the common "list active
// workspaces for tenant X" query.
const createWorkspacesTable = `CREATE TABLE IF NOT EXISTS workspaces (
	id              VARCHAR(64) NOT NULL PRIMARY KEY,
	tenant_id       VARCHAR(16) NOT NULL,
	slug            VARCHAR(64) NOT NULL,
	project_path    TEXT        NOT NULL,
	created_at      BIGINT      NOT NULL,
	archived_at     BIGINT      DEFAULT NULL,
	egress_template VARCHAR(32) NOT NULL DEFAULT '',
	layout_version  INT         NOT NULL DEFAULT 1,
	UNIQUE KEY idx_workspaces_tenant_slug (tenant_id, slug),
	KEY idx_workspaces_tenant (tenant_id, archived_at)
)`

// Migrate ensures the workspaces table exists. Safe to call on every boot.
func (s *SQLStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, createWorkspacesTable); err != nil {
		return fmt.Errorf("workspaces: create table: %w", err)
	}
	return nil
}

// Resolve implements Registry. Strategy: parse the wsid, then query by
// (prefix-of-tenant_id, slug) so both the default-tenant fast path and
// the multi-tenant case share one path. Two SQL queries at most: one
// to find a row whose tenant_id starts with the wsid prefix.
func (s *SQLStore) Resolve(ctx context.Context, wsid string) (*Workspace, error) {
	w, fullHint, err := canonicalWSID(wsid)
	if err != nil {
		return nil, err
	}
	prefix := string(w.Tenant)
	// Default tenant: direct lookup by full tenant id.
	if fullHint != "" {
		return s.lookupByTenantSlug(ctx, fullHint, w.Slug)
	}
	// Non-default tenant: match on tenant_id prefix. SQL LIKE 'prefix%'
	// is portable and indexed-prefix-friendly. Returns the first row
	// because (tenant prefix, slug) is unique-by-construction (the
	// 8-char body of a tenant id is rejected at insert time if it
	// collides with another tenant's prefix).
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, slug, project_path, created_at, archived_at, egress_template, layout_version
		 FROM workspaces
		 WHERE slug = ? AND tenant_id LIKE ?
		 LIMIT 1`,
		w.Slug, prefix+"%")
	return scanWorkspace(row)
}

func (s *SQLStore) lookupByTenantSlug(ctx context.Context, tenantID, slug string) (*Workspace, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, slug, project_path, created_at, archived_at, egress_template, layout_version
		 FROM workspaces
		 WHERE tenant_id = ? AND slug = ?`, tenantID, slug)
	return scanWorkspace(row)
}

// List implements Registry.
func (s *SQLStore) List(ctx context.Context, opts ListOpts) ([]*Workspace, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	var (
		q    strings.Builder
		args []any
	)
	q.WriteString(`SELECT id, tenant_id, slug, project_path, created_at, archived_at, egress_template, layout_version FROM workspaces WHERE id > ?`)
	args = append(args, opts.Cursor)
	if opts.TenantID != "" {
		q.WriteString(` AND tenant_id = ?`)
		args = append(args, opts.TenantID)
	}
	if !opts.IncludeArchived {
		q.WriteString(` AND archived_at IS NULL`)
	}
	q.WriteString(` ORDER BY id ASC LIMIT ?`)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("workspaces: list: %w", err)
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Create implements Registry.
func (s *SQLStore) Create(ctx context.Context, in CreateInput) (*Workspace, error) {
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	tid, err := tenant.ParseID(in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	wsid := tid.Prefix() + ":" + in.Slug
	now := s.now().Unix()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workspaces (id, tenant_id, slug, project_path, created_at, archived_at, egress_template, layout_version)
		 VALUES (?, ?, ?, ?, ?, NULL, ?, 1)`,
		wsid, in.TenantID, in.Slug, in.ProjectPath, now, in.EgressTemplate)
	if err != nil {
		// MySQL/Dolt error 1062 is duplicate key. Translate any
		// constraint violation to ErrAlreadyExists rather than
		// surfacing the driver-specific string.
		if isDuplicateKey(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("workspaces: insert: %w", err)
	}
	return &Workspace{
		ID:             wsid,
		TenantID:       in.TenantID,
		Slug:           in.Slug,
		ProjectPath:    in.ProjectPath,
		CreatedAt:      time.Unix(now, 0).UTC(),
		EgressTemplate: in.EgressTemplate,
		LayoutVersion:  1,
	}, nil
}

// Delete implements Registry. Hard delete keyed by full canonical wsid.
func (s *SQLStore) Delete(ctx context.Context, wsid string) error {
	w, fullHint, err := canonicalWSID(wsid)
	if err != nil {
		return err
	}
	prefix := string(w.Tenant)
	var (
		res    sql.Result
		execEr error
	)
	if fullHint != "" {
		res, execEr = s.db.ExecContext(ctx,
			`DELETE FROM workspaces WHERE tenant_id = ? AND slug = ?`,
			fullHint, w.Slug)
	} else {
		res, execEr = s.db.ExecContext(ctx,
			`DELETE FROM workspaces WHERE slug = ? AND tenant_id LIKE ?`,
			w.Slug, prefix+"%")
	}
	if execEr != nil {
		return fmt.Errorf("workspaces: delete: %w", execEr)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("workspaces: delete rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanner abstracts *sql.Row and *sql.Rows for scanWorkspace.
type scanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(r scanner) (*Workspace, error) {
	var (
		w           Workspace
		createdSec  int64
		archivedSec sql.NullInt64
	)
	if err := r.Scan(&w.ID, &w.TenantID, &w.Slug, &w.ProjectPath,
		&createdSec, &archivedSec, &w.EgressTemplate, &w.LayoutVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("workspaces: scan: %w", err)
	}
	w.CreatedAt = time.Unix(createdSec, 0).UTC()
	if archivedSec.Valid {
		t := time.Unix(archivedSec.Int64, 0).UTC()
		w.ArchivedAt = &t
	}
	return &w, nil
}

// isDuplicateKey returns true for MySQL/Dolt error 1062 (duplicate
// entry). We string-match because the driver does not expose a stable
// error code without importing the mysql driver here, and we don't
// want to pull that dependency into this package just for one branch.
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Duplicate entry") ||
		strings.Contains(s, "duplicate key") ||
		strings.Contains(s, "Error 1062")
}

// BootstrapFromDir scans workspacesRoot for existing project directories
// and inserts a registry row for each one, keyed to the default tenant.
// Idempotent — rows that already exist are skipped. Returns the number
// of rows inserted (zero on a clean re-run). Errors reading individual
// directories are logged via the supplied logger and do not abort the
// scan; only a top-level ReadDir failure aborts.
//
// This is the gm-o9t8.2.4 migration path: M1 deployments had no
// registry, so on first boot we materialise one entry per on-disk
// project under the default tenant.
func BootstrapFromDir(ctx context.Context, reg Registry, workspacesRoot string) (int, error) {
	if strings.TrimSpace(workspacesRoot) == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(workspacesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("workspaces: bootstrap readdir %q: %w", workspacesRoot, err)
	}
	inserted := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		projectPath := filepath.Join(workspacesRoot, name)
		_, rerr := reg.Resolve(ctx, name) // bare slug → t-default
		if rerr == nil {
			continue // already registered
		}
		if !errors.Is(rerr, ErrNotFound) && !errors.Is(rerr, tenant.ErrInvalidWSID) {
			return inserted, fmt.Errorf("workspaces: bootstrap resolve %q: %w", name, rerr)
		}
		_, cerr := reg.Create(ctx, CreateInput{
			TenantID:    string(tenant.DefaultTenant),
			Slug:        name,
			ProjectPath: projectPath,
		})
		if cerr != nil {
			if errors.Is(cerr, ErrAlreadyExists) {
				continue
			}
			return inserted, fmt.Errorf("workspaces: bootstrap create %q: %w", name, cerr)
		}
		inserted++
	}
	return inserted, nil
}
