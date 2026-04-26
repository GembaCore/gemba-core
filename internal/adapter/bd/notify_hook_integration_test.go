package bd_test

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
	"strings"
	"testing"
	"time"
)

// TestGembaBDHookEndToEnd exercises the gemba-bd-hook binary
// against an httptest server playing the role of /api/workitems/notify
// (gm-e4.3.3). Skipped by default — runs only when:
//
//  1. GEMBA_BD_HOOK_INTEG=1 is set in the environment, AND
//  2. The gemba-bd-hook binary is reachable at GEMBA_BD_HOOK_BIN, or
//     one is found via `make build` output at ./bin/gemba-bd-hook
//     relative to the repo root.
//
// Lives under internal/adapter/bd_test (external test package) so it
// doesn't pollute the unit-test surface and so the build dependency
// on the cmd/ binary stays one-way.
func TestGembaBDHookEndToEnd(t *testing.T) {
	if os.Getenv("GEMBA_BD_HOOK_INTEG") != "1" {
		t.Skip("set GEMBA_BD_HOOK_INTEG=1 to run gemba-bd-hook integration test")
	}
	bin := os.Getenv("GEMBA_BD_HOOK_BIN")
	if bin == "" {
		// Walk up from the test file's dir to find the repo root, then
		// look for ./bin/gemba-bd-hook there. Same pattern the codegen
		// test uses.
		candidate := repoBinPath(t, "gemba-bd-hook")
		if _, err := os.Stat(candidate); err == nil {
			bin = candidate
		}
	}
	if bin == "" {
		t.Skip("gemba-bd-hook binary not found; run `make build` or set GEMBA_BD_HOOK_BIN")
	}

	// Spin up an httptest server playing /api/workitems/notify.
	type captured struct {
		WorkItemID string `json:"work_item_id"`
		Source     string `json:"source"`
	}
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost ||
			r.URL.Path != "/api/workitems/notify" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body captured
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		got = append(got, body)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"work_item_id": body.WorkItemID,
			"kind":         "workitem_updated",
		})
	}))
	t.Cleanup(srv.Close)

	// Run the hook with explicit ids.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"--id", "gm-integ-1",
		"--id", "gm-integ-2",
		"--source", "integ-test",
		"--strict")
	cmd.Env = append(os.Environ(),
		"GEMBA_NOTIFY_URL="+srv.URL,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("hook run: %v; stderr=%s", err, stderr.String())
	}

	if len(got) != 2 {
		t.Fatalf("got %d posts, want 2: %+v", len(got), got)
	}
	if got[0].WorkItemID != "gm-integ-1" || got[1].WorkItemID != "gm-integ-2" {
		t.Errorf("ids in wrong order: %+v", got)
	}
	for _, c := range got {
		if c.Source != "integ-test" {
			t.Errorf("source = %q, want integ-test", c.Source)
		}
	}
}

// TestGembaBDHookFailsOpenWhenURLUnset confirms the no-op-on-unset
// invariant from the bead's DoD ("hook failure does not break bd").
// Same skip rule as the happy path — this is real-binary territory.
func TestGembaBDHookFailsOpenWhenURLUnset(t *testing.T) {
	if os.Getenv("GEMBA_BD_HOOK_INTEG") != "1" {
		t.Skip("set GEMBA_BD_HOOK_INTEG=1 to run")
	}
	bin := os.Getenv("GEMBA_BD_HOOK_BIN")
	if bin == "" {
		bin = repoBinPath(t, "gemba-bd-hook")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("gemba-bd-hook not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--id", "gm-x")
	// Explicitly clear notify URL so the no-op path triggers.
	env := append([]string(nil), os.Environ()...)
	for i, e := range env {
		if strings.HasPrefix(e, "GEMBA_NOTIFY_URL=") {
			env[i] = "GEMBA_NOTIFY_URL="
		}
	}
	cmd.Env = append(env, "GEMBA_NOTIFY_URL=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("expected exit 0 with URL unset; got %v (stderr=%s)", err, stderr.String())
	}
}

// repoBinPath walks up from the test file's dir to find a sibling
// "bin/" directory and returns the absolute path to bin/<name>.
func repoBinPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "bin", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// (silence unused-import)
var _ = fmt.Sprintf
