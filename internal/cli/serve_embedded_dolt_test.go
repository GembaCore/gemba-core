// serve_embedded_dolt_test.go: tests for gm-o9t8.1.14 — auto-bootstrap
// of the bd schema on a fresh embedded-dolt data dir.
//
// Two scopes:
//
//   - TestBootstrapEmbeddedDoltSchema_NoSupNoop verifies the function
//     no-ops when sup is nil (external-dolt mode).
//   - TestBootstrapEmbeddedDoltSchema_Idempotent boots a real dolt
//     supervisor against an empty data dir, runs bootstrap once,
//     stops, restarts, runs bootstrap again, and asserts the second
//     pass is a no-op (idempotent). Skipped unless `dolt` AND `bd`
//     are on PATH.

package cli

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/dolt/supervisor"
	"github.com/GembaCore/gemba-core/internal/config"
)

func TestBootstrapEmbeddedDoltSchema_NoSupNoop(t *testing.T) {
	// nil supervisor → no-op, no error (external dolt mode).
	cfg := &config.ServeConfig{DoltEmbeddedDB: "gemba"}
	if err := bootstrapEmbeddedDoltSchema(context.Background(), nil, cfg); err != nil {
		t.Fatalf("nil sup should be no-op; got %v", err)
	}
	// nil cfg → no-op too.
	if err := bootstrapEmbeddedDoltSchema(context.Background(), nil, nil); err != nil {
		t.Fatalf("nil cfg should be no-op; got %v", err)
	}
}

// TestBootstrapEmbeddedDoltSchema_Idempotent is the gm-o9t8.1.14 DoD:
// an empty data dir + `gemba serve --embedded-dolt` works end-to-end
// without manual `bd init`, AND a restart against the same data dir
// is also a no-op (bootstrap is idempotent).
func TestBootstrapEmbeddedDoltSchema_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not on PATH; skipping embedded-dolt bootstrap integration test")
	}
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH; skipping embedded-dolt bootstrap integration test")
	}

	dataDir := filepath.Join(t.TempDir(), "dolt")
	cfg := &config.ServeConfig{
		EmbeddedDolt:   true,
		DoltDataDir:    dataDir,
		DoltEmbeddedDB: defaultEmbeddedDoltDB,
	}

	// First boot: schema should not exist; bootstrap must run.
	sup1, err := supervisor.New(supervisor.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("supervisor.New: %v", err)
	}
	startCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := sup1.Start(startCtx); err != nil {
		t.Fatalf("first supervisor Start: %v", err)
	}

	// Sanity: pre-bootstrap, schema is absent.
	present, err := embeddedDoltSchemaPresent(context.Background(), sup1.DSN(), cfg.DoltEmbeddedDB)
	if err != nil {
		_ = sup1.Stop(context.Background())
		t.Fatalf("pre-bootstrap probe: %v", err)
	}
	if present {
		_ = sup1.Stop(context.Background())
		t.Fatalf("schema unexpectedly present in fresh data dir %q", dataDir)
	}

	if err := bootstrapEmbeddedDoltSchema(context.Background(), sup1, cfg); err != nil {
		_ = sup1.Stop(context.Background())
		t.Fatalf("first bootstrap: %v", err)
	}

	// Post-bootstrap, schema must be present.
	present, err = embeddedDoltSchemaPresent(context.Background(), sup1.DSN(), cfg.DoltEmbeddedDB)
	if err != nil {
		_ = sup1.Stop(context.Background())
		t.Fatalf("post-bootstrap probe: %v", err)
	}
	if !present {
		_ = sup1.Stop(context.Background())
		t.Fatalf("schema missing after bootstrap")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := sup1.Stop(stopCtx); err != nil {
		t.Fatalf("first supervisor Stop: %v", err)
	}
	stopCancel()

	// Second boot against the same data dir: bootstrap must be a
	// no-op (idempotent) — no error, no bd shell-out.
	sup2, err := supervisor.New(supervisor.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("second supervisor.New: %v", err)
	}
	startCtx2, cancel2 := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel2()
	if err := sup2.Start(startCtx2); err != nil {
		t.Fatalf("second supervisor Start: %v", err)
	}
	defer sup2.Stop(context.Background())

	if err := bootstrapEmbeddedDoltSchema(context.Background(), sup2, cfg); err != nil {
		t.Fatalf("second bootstrap (idempotency check): %v", err)
	}
	present, err = embeddedDoltSchemaPresent(context.Background(), sup2.DSN(), cfg.DoltEmbeddedDB)
	if err != nil {
		t.Fatalf("post-restart probe: %v", err)
	}
	if !present {
		t.Fatalf("schema missing after idempotent re-bootstrap")
	}
}
