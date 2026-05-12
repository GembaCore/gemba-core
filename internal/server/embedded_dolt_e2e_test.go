// Integration test: embedded dolt supervisor → dolt WorkPlane →
// HTTP server round-trip (gm-o9t8.1.7).
//
// This test is the "real dolt" counterpart to TestSchemaRoundTrip in
// bead_crud_e2e_test.go. It exercises the actual wiring that
// `gemba serve --embedded-dolt` performs:
//
//   1. Boot a supervisor.Supervisor on a tempdir
//   2. Bootstrap the bd schema in the supervised server via `bd init
//      --server --external --database gemba` (the dolt adaptor's
//      verifySchema requires a populated `schema_migrations` table)
//   3. Construct dolt.NewWorkPlane against mysql://...:<port>/gemba
//   4. Register on an api.Host, build a Router, expose via httptest
//   5. POST /api/work-items, GET /api/work-items, assert round-trip
//
// Skip rules (in spec spirit):
//   - dolt binary missing from PATH → skip
//   - bd binary missing from PATH   → skip (we use bd to bootstrap
//     the schema; no other migration runner is callable from test
//     code at this commit)
//   - bd init returns nonzero       → skip (the local bd may not
//     support the --external/--database flag set this test depends on)
//
// The supervisor is torn down via t.Cleanup so even on test failure
// no orphan dolt subprocesses are left behind.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/dolt"
	"github.com/GembaCore/gemba-core/internal/adapter/dolt/supervisor"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/transport/api"
)

func TestEmbeddedDoltDSNRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not on PATH; skipping embedded-dolt e2e")
	}
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd binary not on PATH; cannot bootstrap bd schema for embedded dolt")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dataDir := filepath.Join(t.TempDir(), "dolt-data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir dataDir: %v", err)
	}

	sup, err := supervisor.New(supervisor.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("supervisor.New: %v", err)
	}
	startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startCancel()
	if err := sup.Start(startCtx); err != nil {
		_ = sup.Stop(context.Background())
		t.Skipf("supervisor.Start failed (dolt may be unavailable in this env): %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = sup.Stop(stopCtx)
	})

	port := sup.Port()
	mysqlURL := fmt.Sprintf("mysql://root@127.0.0.1:%d/gemba", port)

	// Bootstrap the bd schema in the embedded server. `bd init
	// --server --external --database gemba` creates the database +
	// runs migrations against the externally-managed server. We run
	// it from a tempdir cwd so it doesn't touch the developer's
	// home or repo state.
	bdCwd := t.TempDir()
	initCmd := exec.CommandContext(ctx, "bd", "init",
		"--server",
		"--external",
		"--server-host", "127.0.0.1",
		"--server-port", fmt.Sprintf("%d", port),
		"--server-user", "root",
		"--database", "gemba",
		"--prefix", "gemba",
		"--non-interactive",
		"--role", "maintainer",
		"--skip-agents",
		"--skip-hooks",
		"--quiet",
	)
	initCmd.Dir = bdCwd
	initCmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1", "CI=true")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("bd init against embedded supervisor failed (the local bd may not "+
			"support this flag set): %v\noutput:\n%s", err, string(out))
	}

	// Stand up the dolt WorkPlane against the embedded supervisor's
	// DSN. This is the exact constructor `gemba serve` runs after
	// gm-o9t8.1.7's wiring: serve.go feeds the supervisor's URL
	// into cfg.DoltURL, which registerDoltWorkPlane passes here.
	wp, err := dolt.NewWorkPlane(dolt.Config{URL: mysqlURL})
	if err != nil {
		t.Fatalf("dolt.NewWorkPlane(%s): %v", mysqlURL, err)
	}
	t.Cleanup(func() { _ = wp.Close() })

	host := api.New()
	if _, err := host.RegisterWorkPlane(ctx, wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}

	router := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	// POST /api/work-items — create a row through the bound WorkPlane
	createBody := map[string]any{
		"item": map[string]any{
			"title":          "embedded dolt round trip",
			"kind":           "task",
			"status":         "open",
			"state_category": "backlog",
			"labels":         []string{"gm-o9t8.1.7", "embedded-dolt"},
		},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(createBody); err != nil {
		t.Fatalf("encode create: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/work-items", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-"+t.Name())

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/work-items: %v", err)
	}
	body, _ := readAndClose(resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create response: %v\nbody=%s", err, body)
	}
	createdID, _ := created["id"].(string)
	if createdID == "" {
		t.Fatalf("created row missing id: %v", created)
	}

	// GET /api/work-items — list must include the created row
	listResp, err := srv.Client().Get(srv.URL + "/api/work-items")
	if err != nil {
		t.Fatalf("GET /api/work-items: %v", err)
	}
	listBody, _ := readAndClose(listResp)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", listResp.StatusCode, listBody)
	}
	var list struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("decode list: %v\nbody=%s", err, listBody)
	}
	found := false
	for _, it := range list.Items {
		if it["id"] == createdID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("list did not contain created id %q; got %d items", createdID, len(list.Items))
	}
}

// readAndClose drains the body and closes it, returning the bytes.
// Used so the test can decode + reference the body in error messages
// without leaking the underlying connection.
func readAndClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}
