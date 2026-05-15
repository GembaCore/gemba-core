// Package workspaces is the multi-tenant workspace registry
// (gm-o9t8.2.4). It replaces the M1 wsid-resolution path
// (config.ListAllProjects basename match + active-project fallback)
// with a real registry so wsid can name workspaces across tenants.
//
// Identity model:
//
//   - A workspace is uniquely keyed by its full wsid:
//     "<tenant-prefix>:<slug>" (canonical) or bare "<slug>" (legacy M1,
//     mapped to tenant "t-default").
//   - (tenant_id, slug) is unique inside one tenant. Two different
//     tenants may carry the same slug — that is exactly the federation
//     requirement gm-o9t8.2.4 calls out.
//
// Surface:
//
//   - Registry is the persistence contract every caller depends on.
//   - SQLStore (sql_store.go) is the production impl backed by Dolt.
//   - MemStore (mem_store.go) is the in-memory impl used for tests and
//     the no-dolt fallback.
//
// Migration: SQLStore is self-migrating — the first call creates the
// workspaces table. Optional bootstrap-from-disk lives in BootstrapFromDir
// and is invoked once at boot from the CLI when --workspaces-bootstrap
// is set (default on).
package workspaces

import (
	"context"
	"errors"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// ErrNotFound is returned by Resolve / Delete when no workspace matches.
var ErrNotFound = errors.New("workspaces: not found")

// ErrAlreadyExists is returned by Create when (tenant_id, slug) already
// owns a row.
var ErrAlreadyExists = errors.New("workspaces: already exists")

// ErrInvalidInput is returned by Create when required fields are
// missing or malformed.
var ErrInvalidInput = errors.New("workspaces: invalid input")

// Workspace is one row in the registry. ID is the full canonical wsid
// ("<tenant-prefix>:<slug>"). TenantID is the full tenant id
// ("t-<8 chars>" or "t-default") — NOT the 6-char prefix that lives
// inside the wsid wire format. ArchivedAt is non-nil for soft-deleted
// rows; Delete is a hard delete (used by the CLI), while archival is a
// status field producers can flip without removing the row.
type Workspace struct {
	ID             string
	TenantID       string
	Slug           string
	ProjectPath    string
	CreatedAt      time.Time
	ArchivedAt     *time.Time
	EgressTemplate string
	LayoutVersion  int
}

// ListOpts controls Registry.List pagination / filtering. Limit clamps
// the page size (zero falls back to a store-side default, currently
// 100). Cursor is an opaque continuation token — today it is the last
// returned ID, but callers should treat it as opaque so impls can
// migrate the encoding later.
type ListOpts struct {
	TenantID        string
	IncludeArchived bool
	Limit           int
	Cursor          string
}

// CreateInput carries the fields a new workspace row needs. Slug and
// TenantID are required; ProjectPath is required (we never insert a
// floating registry row without a backing on-disk path); EgressTemplate
// is optional (empty means "use defaults").
type CreateInput struct {
	TenantID       string
	Slug           string
	ProjectPath    string
	EgressTemplate string
}

// Registry is the persistence contract. Implementations MUST be safe
// for concurrent use. Resolve accepts a wsid in either canonical
// "<tenant-prefix>:<slug>" form or the legacy bare "<slug>" form (which
// maps to tenant "t-default"); both forms hit the same (tenant_id,
// slug) unique index.
type Registry interface {
	// Resolve looks up a workspace by wsid. Returns ErrNotFound when
	// no row matches. Bare-slug wsids resolve against tenant "t-default".
	Resolve(ctx context.Context, wsid string) (*Workspace, error)

	// List returns workspaces matching opts. The slice is ordered by ID
	// ascending so the cursor is monotonic.
	List(ctx context.Context, opts ListOpts) ([]*Workspace, error)

	// Create inserts a new workspace row. Returns ErrAlreadyExists when
	// (TenantID, Slug) already owns a row, ErrInvalidInput when the
	// required fields are missing.
	Create(ctx context.Context, in CreateInput) (*Workspace, error)

	// Delete removes the workspace row keyed by wsid (full canonical
	// form). Returns ErrNotFound when no row matches. Hard delete —
	// callers that need a tombstone should set ArchivedAt instead via
	// a future Update path.
	Delete(ctx context.Context, wsid string) error
}

// defaultListLimit caps page size when ListOpts.Limit is unset.
const defaultListLimit = 100

// canonicalWSID normalises a wsid string into (tenantID full, slug, wsid full).
// Bare-slug input maps to tenant "t-default". The returned wsid is in
// canonical "<6-char prefix>:<slug>" form so it can be used as a
// primary key.
//
// Returns ErrInvalidInput when the input is empty or malformed.
//
// Resolution rule: the wsid wire format carries only the 6-char tenant
// prefix; the registry stores the full tenant id ("t-<8 chars>"). When
// a caller passes a canonical wsid (e.g. "t-abcd:foo") we keep the
// prefix as-is in the wsid key, but we must reconstruct the full
// tenant id from the registered tenants. For the default tenant the
// mapping is unambiguous (prefix "t-defa" → "t-default"). For other
// tenants the resolver delegates to the store, which queries by
// (slug, prefix-of-tenant_id) instead.
func canonicalWSID(wsid string) (parsed tenant.WSID, fullTenantHint string, err error) {
	w, perr := tenant.ParseWSID(wsid)
	if perr != nil {
		return tenant.WSID{}, "", perr
	}
	// Map the default-tenant prefix back to the full id. Other
	// tenants are resolved at the store layer (slug + prefix lookup).
	if string(w.Tenant) == tenant.DefaultTenant.Prefix() {
		return w, string(tenant.DefaultTenant), nil
	}
	return w, "", nil
}
