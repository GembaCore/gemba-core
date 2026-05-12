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

// applyEmbeddedDoltDefault resolves the runtime value of --embedded-dolt
// when the operator didn't pass the flag explicitly (gm-o9t8.1.2.3).
//
// Rule:
//   - --embedded-dolt explicitly set → honor that value verbatim
//   - --dolt-url set                  → embedded=false (external dolt wins)
//   - otherwise                       → embedded=true (single-user OSS core)
//
// Also resolves the data dir default ("<cwd>/data/dolt") when unset.
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
	return nil
}

// startEmbeddedDolt constructs a supervisor.Supervisor, starts it,
// waits up to startupReadyTimeout for it to become ready, and wires
// its readiness signal into the router's /api/readyz endpoint
// (gm-o9t8.1.2.3).
//
// On success the returned *supervisor.Supervisor is non-nil and the
// caller is responsible for calling Stop() during shutdown. On failure
// the supervisor is torn down before returning so no orphan dolt is
// left behind.
//
// If cfg.EmbeddedDolt is false, this is a no-op that returns (nil, nil)
// — the operator opted into an external dolt server.
func startEmbeddedDolt(ctx context.Context, cfg *config.ServeConfig, handler *server.Router) (*supervisor.Supervisor, error) {
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

	// Inject the supervisor's Ready() into the router so /api/readyz
	// reflects dolt state immediately.
	handler.AttachDoltSupervisor(sup)

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
		"data_dir", sup.DataDir(),
		"dsn_for_clients", sup.DSN())
	return sup, nil
}
