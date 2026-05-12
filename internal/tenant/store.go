package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Kind is the tenant kind enum mirrored from the Dolt schema. Three
// values are pinned at this stage; the OAuth integration story
// (gm-o9t8.3.5) is what differentiates user vs org rows on insert.
type Kind string

const (
	KindUser   Kind = "user"
	KindOrg    Kind = "org"
	KindSystem Kind = "system"
)

// Tenant is one row from the `tenants` table.
//
// GitHubLogin / GitHubID are nullable: the DefaultTenant row carries
// nulls, and pre-OAuth seeded rows may also lack a GitHub identity.
type Tenant struct {
	ID          ID
	GitHubLogin *string
	GitHubID    *int64
	Kind        Kind
	CreatedAt   time.Time
}

// ErrTenantNotFound is returned by Get / GetByGitHub when no row
// matches. Callers use errors.Is to distinguish from infra errors.
var ErrTenantNotFound = errors.New("tenant: not found")

// Store is the persistence contract for tenants. The full OAuth
// surface (link/unlink, GitHub-id rotation) lands in gm-o9t8.3.5; the
// gm-o9t8.3.9 foundation only needs read access, an insert for the
// default-tenant seed, and a list for the (admin-only) GET tenant
// metadata route.
type Store interface {
	// Get returns the row for id or ErrTenantNotFound.
	Get(ctx context.Context, id ID) (Tenant, error)
	// Create inserts t. The row's CreatedAt is set by the store on
	// success (callers may leave it zero on input).
	Create(ctx context.Context, t Tenant) error
	// GetByGitHub returns the tenant linked to the given GitHub
	// numeric id, or ErrTenantNotFound. Used during OAuth callback
	// (gm-o9t8.3.5) and by the bearer → tenant resolution stub.
	GetByGitHub(ctx context.Context, githubID int64) (Tenant, error)
	// List returns every tenant ordered by created_at ascending.
	// Admin-only consumers; the full list is expected to be small
	// (operator + a handful of orgs) for the foreseeable future.
	List(ctx context.Context) ([]Tenant, error)
}

// SQLStore is the production Store backed by the same Dolt SQL pool
// the WorkPlane adaptor uses. It owns the `tenants` table; the bd
// adaptor's schema_migrations table is untouched.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore wraps db. The pool's lifecycle is the caller's.
func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

// createTenantsTable is the gemba-owned migration that lands the
// `tenants` table. Idempotent so callers can run it on every boot
// without a separate migration runner. The schema mirrors the
// gm-o9t8.3.9 decision:
//
//   - id           VARCHAR(16) PRIMARY KEY    — "t-<8 alnum>"
//   - github_login VARCHAR(64) UNIQUE NULL    — GitHub username
//   - github_id    BIGINT      UNIQUE NULL    — GitHub numeric id
//   - kind         ENUM(user,org,system)      — operator class
//   - created_at   TIMESTAMP DEFAULT NOW      — insertion time
//
// Dolt accepts MySQL-flavored DDL, so the schema is portable to a
// vanilla MySQL test rig as well.
const createTenantsTable = `CREATE TABLE IF NOT EXISTS tenants (
	id           VARCHAR(16)  NOT NULL PRIMARY KEY,
	github_login VARCHAR(64)  DEFAULT NULL,
	github_id    BIGINT       DEFAULT NULL,
	kind         ENUM('user','org','system') NOT NULL DEFAULT 'user',
	created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE KEY idx_tenants_github_login (github_login),
	UNIQUE KEY idx_tenants_github_id    (github_id)
)`

// createAuthTokensTable is the migration for the OAuth-minted gemba
// bearer store (gm-o9t8.3.5.2). Idempotent. Lives next to the tenants
// table because the lifecycle is identical: both seed on first boot
// and never require manual intervention. The runtime read/write
// implementation lives in internal/server (so the tenant package
// doesn't take a dependency on auth hashing), but the DDL stays here
// so a single tenant.Migrate call provisions everything the
// multi-tenant control plane needs.
const createAuthTokensTable = `CREATE TABLE IF NOT EXISTS auth_tokens (
	tenant_id     VARCHAR(16)  NOT NULL,
	github_id     BIGINT       NOT NULL,
	device_label  VARCHAR(64)  NOT NULL,
	hash_phc      TEXT         NOT NULL,
	created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (tenant_id, device_label),
	KEY idx_auth_tokens_github_id (github_id)
)`

// Migrate ensures the tenants + auth_tokens tables exist and seeds
// the DefaultTenant row if the tenants table is empty. Safe to call
// on every server boot.
//
// Seeding policy: we only insert DefaultTenant when the table has no
// rows. Operators upgrading from M1 get the seed automatically; fresh
// installs land with a single 'system'-kind row; multi-tenant rigs
// add real tenants via the OAuth flow (gm-o9t8.3.5).
func (s *SQLStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, createTenantsTable); err != nil {
		return fmt.Errorf("tenant: create tenants table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, createAuthTokensTable); err != nil {
		return fmt.Errorf("tenant: create auth_tokens table: %w", err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tenants").Scan(&n); err != nil {
		return fmt.Errorf("tenant: count tenants: %w", err)
	}
	if n > 0 {
		return nil
	}
	if err := s.Create(ctx, Tenant{ID: DefaultTenant, Kind: KindSystem}); err != nil {
		return fmt.Errorf("tenant: seed default tenant: %w", err)
	}
	return nil
}

// Get implements Store.
func (s *SQLStore) Get(ctx context.Context, id ID) (Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, github_login, github_id, kind, created_at FROM tenants WHERE id = ?`, string(id))
	return scanTenant(row)
}

// GetByGitHub implements Store.
func (s *SQLStore) GetByGitHub(ctx context.Context, githubID int64) (Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, github_login, github_id, kind, created_at FROM tenants WHERE github_id = ?`, githubID)
	return scanTenant(row)
}

// Create implements Store. github_login / github_id are inserted as
// NULL when the pointers are nil; the unique constraints then permit
// multiple null rows (MySQL semantics).
func (s *SQLStore) Create(ctx context.Context, t Tenant) error {
	if t.ID == "" {
		return errors.New("tenant: Create requires non-empty ID")
	}
	if t.Kind == "" {
		t.Kind = KindUser
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, github_login, github_id, kind) VALUES (?, ?, ?, ?)`,
		string(t.ID), nullableString(t.GitHubLogin), nullableInt64(t.GitHubID), string(t.Kind))
	if err != nil {
		return fmt.Errorf("tenant: insert %s: %w", t.ID, err)
	}
	return nil
}

// List implements Store.
func (s *SQLStore) List(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, github_login, github_id, kind, created_at FROM tenants ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("tenant: list: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scanner abstracts *sql.Row and *sql.Rows so scanTenant works for
// both single-row Get and List iteration.
type scanner interface {
	Scan(dest ...any) error
}

func scanTenant(r scanner) (Tenant, error) {
	var (
		t           Tenant
		idStr, kind string
		login       sql.NullString
		gid         sql.NullInt64
	)
	if err := r.Scan(&idStr, &login, &gid, &kind, &t.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		return Tenant{}, fmt.Errorf("tenant: scan: %w", err)
	}
	t.ID = ID(idStr)
	t.Kind = Kind(kind)
	if login.Valid {
		v := login.String
		t.GitHubLogin = &v
	}
	if gid.Valid {
		v := gid.Int64
		t.GitHubID = &v
	}
	return t, nil
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// --- in-memory store ------------------------------------------------------

// MemStore is a process-local Store implementation. Used by tests and
// by the single-user fallback when --dolt-url is not configured —
// every request resolves to DefaultTenant, but handlers can still
// dereference a Store for metadata.
type MemStore struct {
	mu      sync.RWMutex
	byID    map[ID]Tenant
	byGHID  map[int64]Tenant
	created []ID // preserves insertion order for List
}

// NewMemStore returns an empty in-memory store. Callers typically
// call Migrate to seed DefaultTenant before serving.
func NewMemStore() *MemStore {
	return &MemStore{byID: map[ID]Tenant{}, byGHID: map[int64]Tenant{}}
}

// Migrate seeds DefaultTenant if the store is empty (mirrors
// SQLStore.Migrate semantics).
func (m *MemStore) Migrate(ctx context.Context) error {
	m.mu.RLock()
	empty := len(m.byID) == 0
	m.mu.RUnlock()
	if !empty {
		return nil
	}
	return m.Create(ctx, Tenant{ID: DefaultTenant, Kind: KindSystem})
}

func (m *MemStore) Get(_ context.Context, id ID) (Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.byID[id]
	if !ok {
		return Tenant{}, ErrTenantNotFound
	}
	return t, nil
}

func (m *MemStore) Create(_ context.Context, t Tenant) error {
	if t.ID == "" {
		return errors.New("tenant: Create requires non-empty ID")
	}
	if t.Kind == "" {
		t.Kind = KindUser
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byID[t.ID]; exists {
		return fmt.Errorf("tenant: duplicate id %s", t.ID)
	}
	m.byID[t.ID] = t
	if t.GitHubID != nil {
		m.byGHID[*t.GitHubID] = t
	}
	m.created = append(m.created, t.ID)
	return nil
}

func (m *MemStore) GetByGitHub(_ context.Context, githubID int64) (Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.byGHID[githubID]
	if !ok {
		return Tenant{}, ErrTenantNotFound
	}
	return t, nil
}

func (m *MemStore) List(_ context.Context) ([]Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Tenant, 0, len(m.created))
	for _, id := range m.created {
		out = append(out, m.byID[id])
	}
	return out, nil
}
