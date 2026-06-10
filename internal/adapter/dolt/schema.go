package dolt

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// knownSchemaVersions pins the bd `schema_migrations` tip versions
// this adaptor has been verified against. bd owns its schema; when
// bd ships a new migration the gemba binary either needs an update
// (new columns to read, renamed tables) or a new entry here (benign
// additive migrations). Either way, operators should get a clear
// "I don't know this schema" error instead of a mid-query column-
// missing failure.
//
// Source of truth is bd's migrations directory. If you're adding a
// version here without any code change, you're asserting that every
// table/column this package reads (issues, labels, dependencies —
// see listColumns in workplane.go) survives the new migration
// untouched.
var knownSchemaVersions = map[int]struct{}{
	32: {},
}

// schemaCheckTimeout bounds the startup schema probe. Shares the
// same budget as a connect ping — if the server is healthy enough
// to answer a ping, a single SELECT MAX() against a tiny table
// must return inside this window.
var schemaCheckTimeout = 2 * time.Second

// verifySchema reads the bd schema tip version from the
// `schema_migrations` table and fails with KindAdaptorDegraded when
// the version is outside the pinned known-good set. Called from
// NewWorkPlane after the ping succeeds so version drift surfaces
// once at startup rather than as scattered "unknown column" errors
// during normal queries.
//
// A missing `schema_migrations` table (or any other scan error)
// also maps to KindAdaptorDegraded — the caller either pointed us
// at a non-beads database (URL error) or at a beads DB on a version
// old enough to predate schema_migrations; both need operator
// attention before queries can be trusted.
func verifySchema(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, schemaCheckTimeout)
	defer cancel()
	var version int
	row := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&version); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"dolt: schema_migrations probe timed out")
		}
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"dolt: schema_migrations probe failed (is --dolt-url pointed at a beads database?)")
	}
	if _, ok := knownSchemaVersions[version]; ok {
		return nil
	}
	known := knownSchemaVersionsSorted()
	ae := core.NewAdaptorError(core.KindAdaptorDegraded,
		"dolt: schema version %d unknown — upgrade gemba or file an issue (known: %v)",
		version, known)
	ae.Detail = map[string]any{
		"schema_version": version,
		"known_versions": known,
	}
	return ae
}

// knownSchemaVersionsSorted returns the pinned versions in ascending
// order — handy for deterministic error messages and Detail payloads.
func knownSchemaVersionsSorted() []int {
	out := make([]int, 0, len(knownSchemaVersions))
	for v := range knownSchemaVersions {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}
