package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

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
