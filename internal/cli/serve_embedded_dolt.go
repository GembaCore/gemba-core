package cli

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/GembaCore/gemba-core/internal/adapter/dolt/supervisor"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server"
)

// defaultEmbeddedDoltDB is the database name the dolt adaptor opens
// against the embedded supervisor when the operator hasn't picked one
// via config. Matches the "gemba" convention used throughout the
// codebase (see internal/adapter/dolt/workplane.go's defaultPrefix).
const defaultEmbeddedDoltDB = "gemba"

// applyEmbeddedDoltDefault resolves the runtime value of --embedded-dolt
// when the operator didn't pass the flag explicitly (gm-o9t8.1.2.3).
//
// Rule:
//   - --embedded-dolt explicitly set → honor that value verbatim
//   - --dolt-url set                  → embedded=false (external dolt wins)
//   - otherwise                       → embedded=true (single-user OSS core)
//
// Also resolves the data dir default ("<cwd>/data/dolt") when unset
// and the embedded db name default ("gemba").
func applyEmbeddedDoltDefault(cfg *config.ServeConfig) error {
	if !cfg.EmbeddedDoltSet {
		if cfg.DoltURL != "" {
			cfg.EmbeddedDolt = false
		} else {
			cfg.EmbeddedDolt = true
		}
	}
	if cfg.EmbeddedDolt && cfg.DoltDataDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd for --dolt-data-dir default: %w", err)
		}
		cfg.DoltDataDir = filepath.Join(cwd, "data", "dolt")
	}
	if cfg.EmbeddedDolt && cfg.DoltEmbeddedDB == "" {
		cfg.DoltEmbeddedDB = defaultEmbeddedDoltDB
	}
	return nil
}

// bootEmbeddedDoltSupervisor constructs a supervisor.Supervisor, starts
// it, and waits up to 30s for it to become ready (gm-o9t8.1.2.3 /
// gm-o9t8.1.7). On success the returned *supervisor.Supervisor is
// non-nil and the caller is responsible for calling Stop() during
// shutdown. On failure the supervisor is torn down before returning so
// no orphan dolt is left behind.
//
// If cfg.EmbeddedDolt is false, this is a no-op that returns (nil, nil)
// — the operator opted into an external dolt server.
//
// This is the "phase 1" startup hook: it boots the SQL server, but
// does NOT touch a Router (the router does not exist yet at this
// point in serve.go's startup). Wire the supervisor's readiness into
// the router via attachEmbeddedDoltToRouter once the router is built.
func bootEmbeddedDoltSupervisor(ctx context.Context, cfg *config.ServeConfig) (*supervisor.Supervisor, error) {
	if !cfg.EmbeddedDolt {
		return nil, nil
	}
	sup, err := supervisor.New(supervisor.Config{
		DataDir: cfg.DoltDataDir,
		Logger:  slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("supervisor.New: %w", err)
	}

	// Bound start so we fail fast if dolt never opens its listener
	// (matches the bead's "If Supervisor never becomes ready in 30s,
	// fail to start with a clear error" criterion).
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := sup.Start(startCtx); err != nil {
		_ = sup.Stop(context.Background())
		return nil, fmt.Errorf("embedded dolt: %w", err)
	}

	// If the supervisor permanently fails later, log loudly so the
	// operator sees it. /api/readyz will already be reporting 503
	// because Ready() is locked at false at that point.
	go func() {
		<-sup.Wait()
		if err := sup.Err(); err != nil {
			slog.Error("embedded dolt: supervisor gave up; /api/readyz will return 503 until gemba is restarted",
				"err", err)
		}
	}()

	slog.Info("embedded dolt sql-server supervised",
		"host", "127.0.0.1",
		"port", sup.Port(),
		"data_dir", cfg.DoltDataDir,
		"dsn_for_clients", sup.DSN())
	return sup, nil
}

// bootstrapEmbeddedDoltSchema ensures the bd schema is present on the
// supervised embedded dolt server (gm-o9t8.1.14). On a fresh data dir
// the dolt server comes up empty — no `gemba` database, no
// `schema_migrations` table — and the dolt WorkPlane adaptor then
// trips its startup probe with "schema_migrations probe failed". The
// step that normally installs the schema is `bd init --server
// --external …`, which a hand-driven operator runs once before booting
// gemba. Doing it automatically on first start removes that friction.
//
// Strategy:
//
//  1. Connect to the supervisor via DSN(). If the configured database
//     (cfg.DoltEmbeddedDB) exists AND contains a `schema_migrations`
//     table, return nil — the bootstrap has already run.
//  2. Otherwise, shell out to `bd init --server --external
//     --server-host 127.0.0.1 --server-port <port> --server-user root
//     --database <db> --non-interactive` in a temp working directory.
//     The bd CLI owns the schema; we just trigger it.
//  3. Re-verify; surface the original probe failure to the caller if
//     bootstrap silently didn't materialize the table.
//
// Idempotent: when the schema is already there, the function returns
// nil without invoking bd. No-op when sup is nil (external dolt mode
// — the operator is responsible for their own schema).
func bootstrapEmbeddedDoltSchema(ctx context.Context, sup *supervisor.Supervisor, cfg *config.ServeConfig) error {
	if sup == nil || cfg == nil {
		return nil
	}
	db := cfg.DoltEmbeddedDB
	if db == "" {
		db = defaultEmbeddedDoltDB
	}

	if ok, err := embeddedDoltSchemaPresent(ctx, sup.DSN(), db); err != nil {
		return fmt.Errorf("embedded dolt: schema probe: %w", err)
	} else if ok {
		slog.Debug("embedded dolt: schema already bootstrapped",
			"database", db)
		return nil
	}

	slog.Info("embedded dolt: empty data dir detected; bootstrapping bd schema",
		"database", db, "port", sup.Port())

	// bd init creates .beads/ in cwd. Use a throwaway temp dir so we
	// don't leave a stray .beads/ next to the gemba binary; the
	// schema lands on the dolt server (the durable artifact), and
	// the local .beads/ is incidental.
	tmpDir, err := os.MkdirTemp("", "gemba-embedded-dolt-bootstrap-")
	if err != nil {
		return fmt.Errorf("embedded dolt: mkdtemp for bd init: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	bdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bdCtx, "bd", "init",
		"--server", "--external",
		"--server-host", "127.0.0.1",
		"--server-port", strconv.Itoa(sup.Port()),
		"--server-user", "root",
		"--database", db,
		"--non-interactive",
	)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("embedded dolt: bd init failed: %w\n%s", err, out)
	}

	// Re-probe so we surface a clear error if bd reported success but
	// the schema didn't actually land (shouldn't happen, but the
	// downstream adaptor will fail with a worse message if we don't
	// catch it here).
	if ok, err := embeddedDoltSchemaPresent(ctx, sup.DSN(), db); err != nil {
		return fmt.Errorf("embedded dolt: post-bootstrap schema probe: %w", err)
	} else if !ok {
		return fmt.Errorf("embedded dolt: bd init reported success but schema_migrations is still missing in %q", db)
	}
	slog.Info("embedded dolt: bd schema bootstrap complete", "database", db)
	return nil
}

// embeddedDoltSchemaPresent reports whether the named database exists
// on the supervisor AND has a `schema_migrations` table. Both are
// required: bd init creates the database (so a missing one means a
// fresh data dir) AND populates schema_migrations (so a present db
// without it is a half-init that bd would refuse to use anyway).
func embeddedDoltSchemaPresent(ctx context.Context, dsn, database string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return false, fmt.Errorf("sql.Open: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(probeCtx); err != nil {
		return false, fmt.Errorf("ping: %w", err)
	}
	var name string
	row := db.QueryRowContext(probeCtx,
		"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?",
		database)
	if err := row.Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("probe database: %w", err)
	}
	var tbl string
	row = db.QueryRowContext(probeCtx,
		"SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'schema_migrations'",
		database)
	if err := row.Scan(&tbl); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("probe schema_migrations: %w", err)
	}
	return true, nil
}

// attachEmbeddedDoltToRouter wires the supervisor's Ready() into the
// router so /api/readyz reflects dolt state immediately. Called after
// the router has been constructed. No-op when sup is nil (external
// dolt mode).
func attachEmbeddedDoltToRouter(sup *supervisor.Supervisor, handler *server.Router) {
	if sup == nil || handler == nil {
		return
	}
	handler.AttachDoltSupervisor(sup)
}

// resolveWorkspacesRoot returns the on-disk root the diff handler
// uses to locate <wsid>/repo/ trees (gm-o9t8.1.16). Resolution order:
//
//  1. --workspaces-root explicitly set → honor verbatim
//  2. --dolt-data-dir set (or defaulted) → "<dirname(DoltDataDir)>/workspaces"
//  3. otherwise → "" (router leaves /diff returning 503)
//
// The convention follows internal/server/workspacelayout: workspace
// trees live at "<data-dir>/workspaces/<id>" where <data-dir> is the
// gemba-server's data dir (the directory that also holds the shared
// dolt/ subtree). The default folds DoltDataDir back to its parent to
// match.
func resolveWorkspacesRoot(cfg *config.ServeConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.WorkspacesRoot != "" {
		return cfg.WorkspacesRoot
	}
	if cfg.DoltDataDir != "" {
		return filepath.Join(filepath.Dir(cfg.DoltDataDir), "workspaces")
	}
	return ""
}

// embeddedDoltMySQLURL builds the mysql:// URL the dolt WorkPlane
// adaptor consumes (parseDoltURL in internal/adapter/dolt/workplane.go).
// The supervisor's DSN is go-sql-driver-native — "user@tcp(host:port)/"
// — so we re-frame it as a URL with the configured database name
// appended.
func embeddedDoltMySQLURL(sup *supervisor.Supervisor, dbName string) string {
	if sup == nil || dbName == "" {
		return ""
	}
	return fmt.Sprintf("mysql://root@127.0.0.1:%d/%s", sup.Port(), dbName)
}
