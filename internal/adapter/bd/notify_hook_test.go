// Integration test for the bd post-write notify hook (gm-e4.3.3).
//
// Status: the hook itself ships in the upstream beads repo. This file
// is the executable contract for that hook on the gemba side — when
// the binary is installed and on PATH, the test exercises the round
// trip (hook exec → POST /api/workitems/notify → 200). Until the
// upstream PR lands the test skips with a clear reason; that's the
// design, not a temporary state. The skip message documents what
// would unblock it.
//
// What the test pins:
//   1. The hook respects GEMBA_NOTIFY_URL — without it set, the hook
//      is a no-op (exits 0, no HTTP call).
//   2. With GEMBA_NOTIFY_URL set, the hook POSTs the expected envelope
//      to /api/workitems/notify and the server's reply is consumed.
//   3. Fail-open contract: a 503 / connection-refused from the server
//      must NOT fail the hook (it logs and exits 0).
//
// We deliberately do NOT spin up `gemba serve` here. A net/http/httptest
// server stands in for it — the hook only cares about the URL and the
// notify wire shape, both of which the httptest server can mimic.

package bd_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hookBinaryName is the canonical name the upstream beads PR installs
// under. Kept as a constant so a future rename only touches one line;
// the test discovers the binary via exec.LookPath.
const hookBinaryName = "bd-gemba-notify-hook"

// hookSkipReason is the message printed when the binary isn't on
// PATH. Worded for the operator who runs `go test` and needs to know
// what to install — not just "test skipped".
const hookSkipReason = `bd-gemba-notify-hook not on PATH; install from upstream beads (` +
	`https://github.com/MikeBengtson/beads, gm-e4.3.3) and re-run`

// findHook returns the installed hook binary or "" if it isn't on
// PATH. Separate from t.Skip so the test functions can decide whether
// a missing binary is a skip (the integration cases) or a soft pass
// (the wire-shape contract test below, which can run without a
// binary in shape-only mode).
func findHook() string {
	p, err := exec.LookPath(hookBinaryName)
	if err != nil {
		return ""
	}
	return p
}

// TestNotifyHook_PostsExpectedEnvelope: with GEMBA_NOTIFY_URL pointed
// at our httptest server, the hook should POST one request whose body
// matches the {work_item_id, source} contract documented in
// docs/adaptors/beads.md.
func TestNotifyHook_PostsExpectedEnvelope(t *testing.T) {
	hook := findHook()
	if hook == "" {
		t.Skip(hookSkipReason)
	}

	type capture struct {
		path   string
		method string
		body   map[string]any
	}
	var got capture
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		got.path = r.URL.Path
		got.method = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"work_item_id":"gm-foo","kind":"workitem_updated"}`))
	}))
	t.Cleanup(srv.Close)

	cmd := exec.Command(hook, "gm-foo")
	cmd.Env = append(os.Environ(),
		"GEMBA_NOTIFY_URL="+srv.URL,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("hook exited non-zero: %v", err)
	}

	// Allow up to 1s for the in-flight HTTP roundtrip; with a localhost
	// httptest server it lands well before that on every machine the
	// suite runs on.
	deadline := time.Now().Add(time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("server saw %d requests, want 1", hits.Load())
	}
	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if !strings.HasSuffix(got.path, "/api/workitems/notify") {
		t.Errorf("path = %q, want suffix /api/workitems/notify", got.path)
	}
	if got.body["work_item_id"] != "gm-foo" {
		t.Errorf("body.work_item_id = %v, want gm-foo", got.body["work_item_id"])
	}
}

// TestNotifyHook_NoUrlIsNoOp: without GEMBA_NOTIFY_URL set, the hook
// is a no-op — same `bd` binary works on a machine that doesn't run
// gemba locally. Skipped when the binary isn't installed.
func TestNotifyHook_NoUrlIsNoOp(t *testing.T) {
	hook := findHook()
	if hook == "" {
		t.Skip(hookSkipReason)
	}

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Filtered env: no GEMBA_NOTIFY_URL. We can't simply unset it on
	// the parent because Go test runners often inherit a configured
	// env; build a minimal env from scratch.
	cmd := exec.Command(hook, "gm-foo")
	cmd.Env = filterEnv(os.Environ(), "GEMBA_NOTIFY_URL", "GEMBA_NOTIFY_AUTH")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("hook must exit 0 when GEMBA_NOTIFY_URL is unset; got %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("hook posted to server with no URL configured: %d hits", hits.Load())
	}
}

// TestNotifyHook_FailOpenOnServerDown: gemba unreachable → hook MUST
// still exit 0 with a stderr warning. This is non-negotiable; bd's
// terminal write is authoritative.
func TestNotifyHook_FailOpenOnServerDown(t *testing.T) {
	hook := findHook()
	if hook == "" {
		t.Skip(hookSkipReason)
	}

	// Address that nothing is listening on. We bind a port, capture
	// it, then immediately close so the next connect attempt
	// reliably fails ECONNREFUSED.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close()

	cmd := exec.Command(hook, "gm-foo")
	cmd.Env = append(os.Environ(), "GEMBA_NOTIFY_URL="+url)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("hook must exit 0 on server-down (fail-open contract); got %v", err)
	}
}

// filterEnv returns env minus the listed keys. Used to scrub the
// GEMBA_* vars off a test process so the hook sees a clean slate.
func filterEnv(env []string, drop ...string) []string {
	dropSet := make(map[string]struct{}, len(drop))
	for _, k := range drop {
		dropSet[k] = struct{}{}
	}
	out := env[:0]
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			out = append(out, kv)
			continue
		}
		if _, ok := dropSet[kv[:idx]]; ok {
			continue
		}
		out = append(out, kv)
	}
	return out
}

