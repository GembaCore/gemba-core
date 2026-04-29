// projects_adoptable.go — GET /api/v1/projects/adoptable (gm-gmyl).
//
// Surfaces the beads-shaped DBs visible on the configured Dolt server
// that DON'T already correspond to a filesystem project. The picker
// renders these alongside the configured-projects list, so the
// operator can click one and get the gm-xwa8 ConfigureProjectModal
// pre-filled with the DB's URL instead of typing it by hand.
//
// Filtering rules:
//
//   1. SHOW DATABASES on the configured Dolt host (the host portion
//      of cfg.DoltURL after gm-root.19 resolution; the DB name in
//      that URL is irrelevant — we want the server, not a specific
//      schema).
//   2. Drop system / Dolt-internal databases (information_schema,
//      mysql, performance_schema, sys, dolt, dolt_cluster).
//   3. Filter to "beads-shaped" — at least one of `issues`,
//      `dependencies`, `events` exists in the DB. (Beads' canonical
//      table names; safe enough that a non-beads DB on the same Dolt
//      server doesn't get offered as adoptable.)
//   4. Subtract DBs already attached to a filesystem project. Each
//      .gemba/workspace.toml carries a `beads_db` URL whose tail-of-
//      path names the DB; collect those and drop matching candidates.
//
// Failure modes:
//
//   - Dolt server unreachable / refuses connections → 200 with empty
//     `dbs` list and a non-empty `notice` field. The picker continues
//     working; the operator can still add a remote DB via the manual
//     entry affordance from gm-xwa8.
//   - Per-DB SHOW TABLES failure (e.g. permission denied on one
//     candidate) → skip silently with a debug-level log. Don't drop
//     the entire response.

package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// adoptableDB is one row in the GET /api/v1/projects/adoptable list.
type adoptableDB struct {
	// Name is the schema name on the Dolt server (e.g. "gemba").
	Name string `json:"name"`
	// URL is the full mysql:// URL pointing at this DB on the
	// configured server. Pre-filled into the ConfigureProjectModal
	// when the operator clicks the entry.
	URL string `json:"url"`
}

// adoptableEnvelope is the response body. `notice` is non-empty when
// we couldn't fully enumerate (server unreachable, permission failure
// at the SHOW DATABASES level) so the SPA can render a small inline
// hint.
type adoptableEnvelope struct {
	DBs    []adoptableDB `json:"dbs"`
	Total  int           `json:"total"`
	Notice string        `json:"notice,omitempty"`
}

// AdoptableLister enumerates adoptable beads DBs. Production impl
// connects to the configured Dolt server. Tests substitute a stub
// via AttachProjects(AttachConfig{AdoptableLister: ...}) so they
// don't need a real Dolt server on port 3307.
type AdoptableLister func(ctx context.Context, baseURL string) ([]adoptableDB, error)

// defaultAdoptableLister is the production implementation. baseURL is
// the host-only mysql:// URL (no schema) — we'll add `/<schema>` per
// candidate as we probe each one.
func defaultAdoptableLister(ctx context.Context, baseURL string) ([]adoptableDB, error) {
	host, err := beadsHostFromURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}

	// Connect against the server's `mysql` system DB to run
	// SHOW DATABASES. dbname doesn't matter for that query but the
	// driver requires *some* schema in the DSN.
	dsn, err := beadsDSN(host + "mysql")
	if err != nil {
		return nil, fmt.Errorf("dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(cctx, "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("show databases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if isSystemDatabase(name) {
			continue
		}
		candidates = append(candidates, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	out := make([]adoptableDB, 0, len(candidates))
	for _, name := range candidates {
		shaped, err := isBeadsShaped(cctx, db, name)
		if err != nil {
			// Per-DB failure (permissions, transient) — skip,
			// don't tank the whole response.
			slog.Debug("adoptable: skipping candidate",
				"db", name, "err", err)
			continue
		}
		if !shaped {
			continue
		}
		out = append(out, adoptableDB{
			Name: name,
			URL:  host + name,
		})
	}
	return out, nil
}

// systemDBs are the schemas a Dolt server hosts that aren't user
// data. Filter them out before any beads-shape probe.
var systemDBs = map[string]struct{}{
	"information_schema": {},
	"mysql":              {},
	"performance_schema": {},
	"sys":                {},
	"dolt":               {},
	"dolt_cluster":       {},
}

func isSystemDatabase(name string) bool {
	_, ok := systemDBs[strings.ToLower(name)]
	return ok
}

// isBeadsShaped reports whether the named DB has at least one of the
// beads canonical tables. Cheap probe — INFORMATION_SCHEMA query
// scoped to the candidate schema. Returns (false, nil) for non-beads
// DBs so the caller drops them silently.
func isBeadsShaped(ctx context.Context, db *sql.DB, schema string) (bool, error) {
	const q = `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_name IN ('issues', 'dependencies', 'events')
	`
	var n int
	if err := db.QueryRowContext(ctx, q, schema).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// beadsHostFromURL strips the schema suffix off a mysql:// URL,
// returning the host portion with a trailing slash so callers can
// concatenate a candidate DB name. Mirrors beadsDSN's parsing.
func beadsHostFromURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("beads url is empty")
	}
	if !strings.HasPrefix(trimmed, "mysql://") {
		return "", fmt.Errorf("beads url must start with mysql://")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("beads url missing host:port")
	}
	out := "mysql://"
	if u.User != nil {
		out += u.User.String() + "@"
	}
	out += u.Host + "/"
	return out, nil
}

// beadsDBLine matches `beads_db = "mysql://..."` in a workspace.toml.
// We don't pull the full TOML parser in here just to read one field;
// the lines we care about are written by gm-root.17.6 / gm-xwa8 in
// a stable shape we control.
var beadsDBLine = regexp.MustCompile(`(?m)^\s*beads_db\s*=\s*"([^"]+)"\s*$`)

// attachedDBNamesFromProjects enumerates the DB names referenced by
// every filesystem project's workspace.toml — these are the schemas
// already attached to a project, so they should NOT show up as
// adoptable. Best-effort: a project whose workspace.toml fails to
// open or doesn't carry a beads_db line is logged + skipped; we'd
// rather offer too many adoptable candidates than too few.
func attachedDBNamesFromProjects(projects []config.ProjectEntry) map[string]struct{} {
	seen := make(map[string]struct{}, len(projects))
	for _, p := range projects {
		path := filepath.Join(p.Path, ".gemba", "workspace.toml")
		raw, err := os.ReadFile(path)
		if err != nil {
			slog.Debug("adoptable: workspace.toml unreadable",
				"path", path, "err", err)
			continue
		}
		m := beadsDBLine.FindSubmatch(raw)
		if len(m) < 2 {
			continue
		}
		u, err := url.Parse(string(m[1]))
		if err != nil {
			continue
		}
		dbName := strings.TrimPrefix(u.Path, "/")
		if dbName == "" {
			continue
		}
		seen[dbName] = struct{}{}
	}
	return seen
}

// listAdoptableProjects handles GET /api/v1/projects/adoptable.
func (r *Router) listAdoptableProjects(w http.ResponseWriter, req *http.Request) {
	ucfg, err := config.LoadUserConfig(r.cfg.ConfigPath)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"config_load_failed", "failed to load user config: "+err.Error())
		return
	}
	defaultDir, err := config.ResolveDefaultDir(ucfg)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"config_resolve_failed", "failed to resolve default_dir: "+err.Error())
		return
	}
	attached, err := config.ListProjectsUnder(defaultDir)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"projects_scan_failed", "failed to enumerate projects: "+err.Error())
		return
	}
	attachedSet := attachedDBNamesFromProjects(attached)

	baseURL := r.cfg.DoltURL
	if baseURL == "" {
		baseURL = config.ResolveBeadsURL("", ucfg)
	}

	lister := r.adoptableLister
	if lister == nil {
		lister = defaultAdoptableLister
	}

	out := adoptableEnvelope{DBs: []adoptableDB{}}
	candidates, lerr := lister(req.Context(), baseURL)
	if lerr != nil {
		// Reachable-failure path: empty list + notice. Picker still
		// works against the filesystem-only enumeration.
		out.Notice = fmt.Sprintf("dolt server at %s unreachable; only filesystem projects shown",
			redactedHost(baseURL))
		writeJSON(w, http.StatusOK, out)
		return
	}

	for _, c := range candidates {
		if _, already := attachedSet[c.Name]; already {
			continue
		}
		out.DBs = append(out.DBs, c)
	}
	out.Total = len(out.DBs)
	writeJSON(w, http.StatusOK, out)
}

// redactedHost returns just the host:port portion of a beads URL for
// inclusion in a user-facing notice. Drops credentials.
func redactedHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}
