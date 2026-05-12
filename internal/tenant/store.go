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

// ErrTenantExists is returned by Create when a row with the same id
// (or unique key) already exists.
var ErrTenantExists = errors.New("tenant: already exists")

// Update carries the mutable fields a PATCH /tenants/{tid} call may
// touch. Nil pointers mean "leave unchanged"; non-nil pointers replace
// the stored value. Only github_login is mutable today; kind /
// github_id rotation lands with the OAuth story (gm-o9t8.3.5).
type Update struct {
	GitHubLogin *string
}

// ListOptions controls Store.List pagination. Limit clamps page size
// (zero / negative → store default, currently 50). After is an
// exclusive lower bound on tenant id; the empty string returns the
// first page.
type ListOptions struct {
	Limit int
	After ID
}

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
	// List returns tenants ordered by id ascending, honouring the
	// pagination knobs in opts. Admin-only consumers; the full list
	// is expected to be small (operator + a handful of orgs) for the
	// foreseeable future, but the page primitive future-proofs the
	// shape against larger SaaS deployments.
	List(ctx context.Context, opts ListOptions) ([]Tenant, error)
	// Update mutates the row for id with the non-nil fields in u.
	// Returns ErrTenantNotFound when the row is absent. The
	// DefaultTenant row is mutable through this surface (operators
	// may need to link a GitHub identity to it post-OAuth).
	Update(ctx context.Context, id ID, u Update) (Tenant, error)
	// Delete removes the row for id. Returns ErrTenantNotFound when
	// the row is absent. Callers are responsible for any cross-row
	// referential checks (workspaces-still-exist, etc.).
	Delete(ctx context.Context, id ID) error
}

// defaultListLimit caps page size when ListOptions.Limit is unset.
const defaultListLimit = 50

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

// Migrate ensures the tenants table exists and seeds the DefaultTenant
// row if the table is empty. Safe to call on every server boot.
//
// Seeding policy: we only insert DefaultTenant when the table has no
// rows. Operators upgrading from M1 get the seed automatically; fresh
// installs land with a single 'system'-kind row; multi-tenant rigs
// add real tenants via the OAuth flow (gm-o9t8.3.5).
func (s *SQLStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, createTenantsTable); err != nil {
		return fmt.Errorf("tenant: create tenants table: %w", err)
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

// List implements Store with id-ordered pagination.
func (s *SQLStore) List(ctx context.Context, opts ListOptions) ([]Tenant, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, github_login, github_id, kind, created_at
		 FROM tenants
		 WHERE id > ?
		 ORDER BY id ASC
		 LIMIT ?`, string(opts.After), limit)
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

// Update implements Store. The implementation builds a sparse UPDATE
// so unchanged columns keep their stored value (including NULL).
func (s *SQLStore) Update(ctx context.Context, id ID, u Update) (Tenant, error) {
	if u.GitHubLogin != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE tenants SET github_login = ? WHERE id = ?`,
			nullableString(u.GitHubLogin), string(id)); err != nil {
			return Tenant{}, fmt.Errorf("tenant: update %s: %w", id, err)
		}
	}
	return s.Get(ctx, id)
}

// Delete implements Store.
func (s *SQLStore) Delete(ctx context.Context, id ID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, string(id))
	if err != nil {
		return fmt.Errorf("tenant: delete %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tenant: delete %s rows: %w", id, err)
	}
	if n == 0 {
		return ErrTenantNotFound
	}
	return nil
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
		return fmt.Errorf("%w: %s", ErrTenantExists, t.ID)
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

// List returns tenants ordered by id ascending. We sort a snapshot of
// the keys rather than relying on insertion order so the pagination
// "after" cursor is meaningful for callers that interleave Create with
// List.
func (m *MemStore) List(_ context.Context, opts ListOptions) ([]Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	ids := make([]ID, 0, len(m.byID))
	for id := range m.byID {
		ids = append(ids, id)
	}
	sortIDs(ids)
	out := make([]Tenant, 0, limit)
	for _, id := range ids {
		if id <= opts.After {
			continue
		}
		out = append(out, m.byID[id])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Update implements Store.
func (m *MemStore) Update(_ context.Context, id ID, u Update) (Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return Tenant{}, ErrTenantNotFound
	}
	if u.GitHubLogin != nil {
		v := *u.GitHubLogin
		t.GitHubLogin = &v
	}
	m.byID[id] = t
	return t, nil
}

// Delete implements Store.
func (m *MemStore) Delete(_ context.Context, id ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return ErrTenantNotFound
	}
	delete(m.byID, id)
	if t.GitHubID != nil {
		delete(m.byGHID, *t.GitHubID)
	}
	for i, x := range m.created {
		if x == id {
			m.created = append(m.created[:i], m.created[i+1:]...)
			break
		}
	}
	return nil
}

// sortIDs sorts an []ID in ascending order. Pulled out of List so the
// sort import sits next to the call site that needs it.
func sortIDs(ids []ID) {
	// hand-rolled insertion sort — N is tiny (operator + a handful
	// of orgs) and avoids pulling sort just for this surface.
	for i := 1; i < len(ids); i++ {
		j := i
		for j > 0 && ids[j-1] > ids[j] {
			ids[j-1], ids[j] = ids[j], ids[j-1]
			j--
		}
	}
}
