// Tests for /api/bootstrap/* (gm-ad1u). Each test wires a fresh
// Router + memory bootstrap store so cases stay independent.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

// newBootstrapRouter builds a Router with a memory bootstrap store
// already attached. Returns the store as well so tests can probe
// state directly.
func newBootstrapRouter(t *testing.T) (*Router, BootstrapStore) {
	t.Helper()
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	store := NewMemoryBootstrapStore()
	r.AttachBootstrap(store)
	return r, store
}

func bootstrapReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ---------------------------------------------------------------------
// 503 paths — handlers refuse until AttachBootstrap has been called.
// ---------------------------------------------------------------------

func TestBootstrap_NotConfigured_503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	// Intentionally NOT calling AttachBootstrap.

	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/bootstrap/start"},
		{http.MethodGet, "/api/bootstrap/progress?analysis_id=x"},
		{http.MethodGet, "/api/bootstrap/plan?analysis_id=x"},
		{http.MethodPost, "/api/bootstrap/commit"},
	}
	for _, c := range cases {
		req := bootstrapReq(t, c.method, c.path, map[string]any{"source": "fresh"})
		// /commit needs the nonce to clear the middleware before the
		// handler runs; without the header the nonce wrapper returns
		// 400 and we never see the 503 branch. Stamp it so this test
		// asserts the per-handler 503, not the middleware short-circuit.
		if c.method == http.MethodPost && strings.Contains(c.path, "commit") {
			req.Header.Set(ConfirmHeader, "test-nonce-503")
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: want 503, got %d body=%q",
				c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// ---------------------------------------------------------------------
// POST /api/bootstrap/start
// ---------------------------------------------------------------------

func TestBootstrapStart_HappyPath(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/start", map[string]any{
		"source": "fresh",
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp bootstrapStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AnalysisID == "" {
		t.Fatalf("expected non-empty analysis_id")
	}
	if !strings.HasPrefix(resp.AnalysisID, "bootstrap-") {
		t.Errorf("analysis_id should be prefixed: %q", resp.AnalysisID)
	}
}

func TestBootstrapStart_AllSourceKinds(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	for _, source := range []string{"jira", "beads", "source-code", "fresh"} {
		req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/start",
			map[string]any{"source": source})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("source=%q: want 201, got %d body=%q",
				source, rec.Code, rec.Body.String())
		}
	}
}

func TestBootstrapStart_InvalidSource(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/start", map[string]any{
		"source": "github",
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestBootstrapStart_BadJSON(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/bootstrap/start",
		strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// GET /api/bootstrap/progress
// ---------------------------------------------------------------------

func TestBootstrapProgress_ReturnsFrames(t *testing.T) {
	r, _ := newBootstrapRouter(t)

	id := startAnalysis(t, r, "fresh")

	req := bootstrapReq(t, http.MethodGet,
		"/api/bootstrap/progress?analysis_id="+id, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp bootstrapProgressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Frames) != 3 {
		t.Fatalf("want 3 frames, got %d", len(resp.Frames))
	}
	if !resp.Frames[2].Done {
		t.Errorf("final frame should mark done=true")
	}
	if !strings.Contains(resp.Frames[0].Line, "Scanning") {
		t.Errorf("first frame should mention Scanning, got %q", resp.Frames[0].Line)
	}
}

func TestBootstrapProgress_SinceFilter(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	id := startAnalysis(t, r, "fresh")

	// Only ask for frames after seq 1; expect 2 + 3.
	req := bootstrapReq(t, http.MethodGet,
		"/api/bootstrap/progress?analysis_id="+id+"&since=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp bootstrapProgressResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Frames) != 2 {
		t.Fatalf("want 2 frames, got %d", len(resp.Frames))
	}
	if resp.Frames[0].Seq != 2 || resp.Frames[1].Seq != 3 {
		t.Errorf("seq mismatch: %d, %d", resp.Frames[0].Seq, resp.Frames[1].Seq)
	}
}

func TestBootstrapProgress_MissingID(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := bootstrapReq(t, http.MethodGet, "/api/bootstrap/progress", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestBootstrapProgress_UnknownID(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := bootstrapReq(t, http.MethodGet,
		"/api/bootstrap/progress?analysis_id=unknown", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// GET /api/bootstrap/plan
// ---------------------------------------------------------------------

func TestBootstrapPlan_HappyPath(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	id := startAnalysis(t, r, "fresh")

	req := bootstrapReq(t, http.MethodGet,
		"/api/bootstrap/plan?analysis_id="+id, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp bootstrapPlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AnalysisID != id {
		t.Errorf("analysis_id mismatch: %s vs %s", resp.AnalysisID, id)
	}
	if resp.Source != BootstrapSourceFresh {
		t.Errorf("source mismatch: %q", resp.Source)
	}
	if len(resp.Plan) == 0 {
		t.Fatalf("plan must not be empty")
	}
	if len(resp.Findings) == 0 {
		t.Fatalf("findings must not be empty")
	}
	// Plan structure matches what PlanReviewStep reads.
	if resp.Plan[0].Kind != "epic" {
		t.Errorf("first plan item should be an epic, got %q", resp.Plan[0].Kind)
	}
	if len(resp.Plan[0].Children) == 0 {
		t.Errorf("epic should have child milestones")
	}
	// Every finding has level + title.
	for _, f := range resp.Findings {
		if f.Level == "" || f.Title == "" {
			t.Errorf("finding missing required field: %+v", f)
		}
	}
}

func TestBootstrapPlan_UnknownID(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := bootstrapReq(t, http.MethodGet,
		"/api/bootstrap/plan?analysis_id=unknown", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// POST /api/bootstrap/commit
// ---------------------------------------------------------------------

func TestBootstrapCommit_NoNonce_400(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	id := startAnalysis(t, r, "fresh")
	req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/commit",
		map[string]any{"analysis_id": id})
	// Deliberately NO X-GEMBA-Confirm header.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestBootstrapCommit_HappyPath(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	id := startAnalysis(t, r, "fresh")
	req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/commit",
		map[string]any{"analysis_id": id})
	req.Header.Set(ConfirmHeader, "test-nonce-commit")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp BootstrapCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ProjectPath == "" {
		t.Errorf("project_path empty")
	}
	if resp.BoardURL != "/board" {
		t.Errorf("board_url mismatch: %q", resp.BoardURL)
	}
}

func TestBootstrapCommit_UnknownID(t *testing.T) {
	r, _ := newBootstrapRouter(t)
	req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/commit",
		map[string]any{"analysis_id": "bootstrap-does-not-exist"})
	req.Header.Set(ConfirmHeader, "test-nonce-unknown")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestBootstrapCommit_NonceReplay(t *testing.T) {
	// Verify the nonce middleware caches the response so a SPA
	// double-click returns the same envelope without a second store
	// write.
	r, _ := newBootstrapRouter(t)
	id := startAnalysis(t, r, "fresh")

	body := map[string]any{"analysis_id": id}
	first := bootstrapReq(t, http.MethodPost, "/api/bootstrap/commit", body)
	first.Header.Set(ConfirmHeader, "test-nonce-replay")
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, first)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first: want 200, got %d body=%q", rec1.Code, rec1.Body.String())
	}

	second := bootstrapReq(t, http.MethodPost, "/api/bootstrap/commit", body)
	second.Header.Set(ConfirmHeader, "test-nonce-replay")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, second)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay: want 200, got %d body=%q", rec2.Code, rec2.Body.String())
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Errorf("replay body mismatch:\n first=%q\n second=%q",
			rec1.Body.String(), rec2.Body.String())
	}
	if rec2.Header().Get(ConfirmHeader+"-Replay") != "true" {
		t.Errorf("replay should set %s-Replay header", ConfirmHeader)
	}
}

// startAnalysis is a small helper that calls /start and returns the
// minted analysis_id. Centralised so the per-test setup stays a single
// line.
func startAnalysis(t *testing.T, r *Router, source string) string {
	t.Helper()
	req := bootstrapReq(t, http.MethodPost, "/api/bootstrap/start",
		map[string]any{"source": source})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("startAnalysis: want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp bootstrapStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("startAnalysis decode: %v", err)
	}
	return resp.AnalysisID
}
