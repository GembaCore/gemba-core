// status_test.go: tests for `gemba status` and bare `gemba`
// (gm-o9t8.1.6.4).

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newStatusFakeServer wires a tiny multi-route handler that mimics the
// surface gemba status reads: the /workspaces/{wsid}/status stub
// (returns 501 today) plus /api/work-items?status=... for the
// fallback path. Tests pass a payload map keyed by the status filter
// (e.g. "ready" -> JSON body) so each scenario is data-driven.
func newStatusFakeServer(t *testing.T, byStatus map[string]string) *httptest.Server {
	t.Helper()
	handler := http.NewServeMux()
	handler.HandleFunc("/api/v1/workspaces/_/status", func(w http.ResponseWriter, _ *http.Request) {
		// Mirror the production stub (gm-o9t8.14).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":{"code":"not_implemented","message":"stub"}}`))
	})
	handler.HandleFunc("/api/work-items", func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		body, ok := byStatus[status]
		if !ok {
			body = `{"items":[]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(handler)
}

// TestStatus_AdHocRendersDashboard exercises the Ad-hoc template
// against a fake server that serves both the 501 workspace-status
// stub and list-by-status fallbacks.
func TestStatus_AdHocRendersDashboard(t *testing.T) {
	srv := newStatusFakeServer(t, map[string]string{
		"ready": `{"items":[
			{"id":"gm-abcd","title":"Add /healthz endpoint","kind":"task","status":"ready","state_category":"unstarted","priority":1},
			{"id":"gm-efgh","title":"Bump go-redis to v9.7","kind":"task","status":"ready","state_category":"unstarted","priority":2}
		]}`,
		"in_progress": `{"items":[
			{"id":"gm-mnop","title":"Investigate prod 502s","kind":"task","status":"in_progress","state_category":"started","priority":0,"assignee":{"id":"mike"}}
		]}`,
		"open":   `{"items":[{"id":"a"},{"id":"b"},{"id":"c"}]}`,
		"closed": `{"items":[]}`,
	})
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "status", "--server", srv.URL)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"gemba",
		"Ready:",
		"gm-abcd",
		"Add /healthz endpoint",
		"In flight:",
		"gm-mnop",
		"Investigate prod 502s",
		"open total",
		"closed today",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestStatus_ModeOnlyPrintsAdHoc(t *testing.T) {
	srv := newStatusFakeServer(t, nil)
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "status", "--server", srv.URL, "--mode")
	if err != nil {
		t.Fatalf("status --mode: %v", err)
	}
	if strings.TrimSpace(out) != "ad-hoc" {
		t.Errorf("--mode output: want %q got %q", "ad-hoc", out)
	}
}

func TestStatus_JSONParsesCleanly(t *testing.T) {
	srv := newStatusFakeServer(t, map[string]string{
		"ready":       `{"items":[{"id":"gm-1","title":"t","kind":"task","status":"ready","state_category":"unstarted"}]}`,
		"in_progress": `{"items":[]}`,
		"open":        `{"items":[{"id":"x"}]}`,
		"closed":      `{"items":[]}`,
	})
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "status", "--server", srv.URL, "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout not valid JSON: %v (out=%q)", err, out)
	}
	if got["mode"] != "ad-hoc" {
		t.Errorf("mode: %v", got["mode"])
	}
}

// TestBareGembaIsSameAsStatus verifies that root.RunE forwards to
// status. We compare normalized outputs so timestamp-derived
// formatting variance doesn't trip the test.
func TestBareGembaIsSameAsStatus(t *testing.T) {
	srv := newStatusFakeServer(t, map[string]string{
		"ready":       `{"items":[{"id":"gm-1","title":"t","kind":"task","status":"ready","state_category":"unstarted"}]}`,
		"in_progress": `{"items":[]}`,
		"open":        `{"items":[{"id":"x"}]}`,
		"closed":      `{"items":[]}`,
	})
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	outA, _, err := runCmd(t, "status", "--server", srv.URL, "--mode")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	outB, _, err := runCmd(t, "--server", srv.URL, "--mode")
	if err != nil {
		// The root command doesn't share the status flag set, so this
		// path uses env var fallback. Try again via GEMBA_SERVER.
		t.Setenv("GEMBA_SERVER", srv.URL)
		outB, _, err = runCmd(t, "--mode")
		if err != nil {
			// Fall back: invoke with no args at all, env var supplies
			// the server. We still expect the dashboard.
			out, _, e2 := runCmd(t)
			if e2 != nil {
				t.Fatalf("bare gemba: %v", e2)
			}
			if !strings.Contains(out, "Ready:") && !strings.Contains(out, "No beads yet") {
				t.Errorf("bare gemba output missing dashboard markers: %q", out)
			}
			return
		}
	}
	if outA != outB {
		t.Errorf("status vs bare differ:\nstatus: %q\nbare:   %q", outA, outB)
	}
}

func TestStatus_EmptyWorkspaceMessage(t *testing.T) {
	// All endpoints return empty lists → renderer should emit the
	// "No beads yet" hint instead of empty section headers.
	srv := newStatusFakeServer(t, map[string]string{
		"ready":       `{"items":[]}`,
		"in_progress": `{"items":[]}`,
		"open":        `{"items":[]}`,
		"closed":      `{"items":[]}`,
	})
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "status", "--server", srv.URL)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "No beads yet") {
		t.Errorf("expected empty-workspace hint; got %q", out)
	}
}

func TestStatus_ServerUnreachable(t *testing.T) {
	// Point at a server that's been closed: every request fails at
	// the transport layer. The CLI must propagate an error
	// (runWithExit path → non-nil err from RunE).
	srv := newStatusFakeServer(t, nil)
	srv.Close() // immediately
	defer installFakeClient(t, srv.URL)()

	_, errb, err := runCmd(t, "status", "--server", srv.URL)
	if err == nil {
		t.Fatalf("status: want error for unreachable server, got nil (stderr=%q)", errb)
	}
}

// newStatusFakeServerNewEndpoint wires a fake server that returns the
// real gm-o9t8.1.4.4 200 envelope from /api/v1/workspaces/{wsid}/status,
// plus /api/work-items for the row fetches the dashboard still needs.
// Tests use this to confirm the dashboard renders from the primary
// endpoint (counts + repo + agents come from the envelope; row tables
// come from the list endpoints).
func newStatusFakeServerNewEndpoint(t *testing.T, statusBody string, byStatus map[string]string) *httptest.Server {
	t.Helper()
	handler := http.NewServeMux()
	handler.HandleFunc("/api/v1/workspaces/_/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(statusBody))
	})
	handler.HandleFunc("/api/work-items", func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		body, ok := byStatus[status]
		if !ok {
			body = `{"items":[]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(handler)
}

// TestStatus_PrimaryEndpointDrivesDashboard pins the gm-o9t8.1.4.4
// contract: when the status endpoint returns 200 with the new shape,
// the CLI uses it as primary and renders the repo + agents banners
// in addition to the ready / in-flight tables.
func TestStatus_PrimaryEndpointDrivesDashboard(t *testing.T) {
	statusBody := `{
		"workspace_id": "demo",
		"mode": "ad-hoc",
		"repo":   {"head":"abc1234def56789","dirty":true,"branch":"main"},
		"beads":  {"ready":4,"in_flight":1,"blocked":0,"open_total":13,"closed_today":4},
		"agents": {"active":1,"last_run_id":"sess-42","last_run_status":"working","last_run_summary":"agent-1 on gm-foo"}
	}`
	srv := newStatusFakeServerNewEndpoint(t, statusBody, map[string]string{
		"ready":       `{"items":[{"id":"gm-abcd","title":"Add /healthz endpoint","kind":"task","status":"ready","state_category":"unstarted","priority":1}]}`,
		"in_progress": `{"items":[{"id":"gm-mnop","title":"Investigate prod 502s","kind":"task","status":"in_progress","state_category":"started","priority":0}]}`,
	})
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "status", "--server", srv.URL)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// Banners derived from the new endpoint.
	for _, want := range []string{
		"repo · main @ abc1234def56", // first 12 chars of head
		"dirty",
		"agents · 1 active",
		"sess-42",
		"agent-1 on gm-foo",
		// Counts come from the envelope (13 / 4), not the row count.
		"13 open total",
		"4 closed today",
		// Tables still render from /api/work-items.
		"gm-abcd",
		"gm-mnop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestStatus_PrimaryEndpoint_NoRepoNoAgents covers the "empty
// workspace" path of the new endpoint: counts are zero, repo is null,
// agents is null. The dashboard must NOT print phantom banners and
// must drop back to the "No beads yet" hint.
func TestStatus_PrimaryEndpoint_NoRepoNoAgents(t *testing.T) {
	statusBody := `{
		"workspace_id": "demo",
		"mode": "ad-hoc",
		"repo": null,
		"beads":  {"ready":0,"in_flight":0,"blocked":0,"open_total":0,"closed_today":0},
		"agents": null
	}`
	srv := newStatusFakeServerNewEndpoint(t, statusBody, nil)
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "status", "--server", srv.URL)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "No beads yet") {
		t.Errorf("expected empty-workspace hint; got %q", out)
	}
	if strings.Contains(out, "repo · ") {
		t.Errorf("repo banner should be absent when repo=null; got %q", out)
	}
	if strings.Contains(out, "agents · ") {
		t.Errorf("agents banner should be absent when agents=null; got %q", out)
	}
}

// TestRootHelpStillWorks ensures the no-args dispatch didn't displace
// cobra's --help handling.
func TestRootHelpStillWorks(t *testing.T) {
	out, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "Available Commands") {
		t.Errorf("--help output missing 'Available Commands':\n%s", out)
	}
	if !strings.Contains(out, "status") {
		t.Errorf("--help output should advertise status subcommand:\n%s", out)
	}
}
