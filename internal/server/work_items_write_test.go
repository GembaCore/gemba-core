package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// Build a router whose work plane records every UpdateWorkItem call
// and returns the patched item. updateRet lets a test inject a
// failure path for one call.
type updateRecorder struct {
	patches   []core.WorkItemPatch
	updateRet func(id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error)
}

func newPatchHost(t *testing.T) (*api.Host, *updateRecorder) {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	rec := &updateRecorder{}
	wp.UpdateFn = func(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		rec.patches = append(rec.patches, patch)
		if rec.updateRet != nil {
			return rec.updateRet(id, patch)
		}
		title := string(id)
		if patch.Title != nil {
			title = *patch.Title
		}
		status := "open"
		if patch.Status != nil {
			status = *patch.Status
		}
		return core.WorkItem{
			ID:            id,
			Kind:          "task",
			Title:         title,
			Status:        status,
			StateCategory: core.StateBacklog,
		}, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	return host, rec
}

func patchReq(t *testing.T, id, nonce string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/work-items/"+id, &buf)
	req.Header.Set("Content-Type", "application/json")
	if nonce != "" {
		req.Header.Set(ConfirmHeader, nonce)
	}
	return req
}

// ---------------------------------------------------------------------
// POST /api/work-items (gm-e12.10)
// ---------------------------------------------------------------------

func newCreateHost(t *testing.T) (*api.Host, *[]core.WorkItem) {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	calls := []core.WorkItem{}
	wp.CreateFn = func(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
		calls = append(calls, wi)
		// Echo back a materialized item with a server-assigned id so the
		// handler's 201 envelope is exercised end-to-end.
		out := wi
		out.ID = "gm-new"
		return out, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	return host, &calls
}

func postCreateReq(t *testing.T, nonce string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/work-items", &buf)
	req.Header.Set("Content-Type", "application/json")
	if nonce != "" {
		req.Header.Set(ConfirmHeader, nonce)
	}
	return req
}

// Happy path: valid body + nonce returns 201 + the materialized item.
func TestCreateWorkItem_HappyPath(t *testing.T) {
	host, calls := newCreateHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title":          "new task",
			"kind":           "task",
			"status":         "open",
			"state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-C", body))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%q", w.Code, w.Body.String())
	}
	var got core.WorkItem
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body=%q", err, w.Body.String())
	}
	if got.ID != "gm-new" || got.Title != "new task" {
		t.Fatalf("returned item mismatch: %+v", got)
	}
	if len(*calls) != 1 {
		t.Fatalf("adaptor saw %d create calls, want 1", len(*calls))
	}
}

// Missing nonce → 400.
func TestCreateWorkItem_MissingNonce_Returns400(t *testing.T) {
	host, calls := newCreateHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title": "x", "kind": "task",
			"status": "open", "state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "", body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(*calls) != 0 {
		t.Fatalf("handler must not run without nonce; saw %d calls", len(*calls))
	}
}

// Empty title → 400 validation envelope from the boundary decoder.
func TestCreateWorkItem_ValidationError(t *testing.T) {
	host, calls := newCreateHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title": "", "kind": "task",
			"status": "open", "state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-V", body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(*calls) != 0 {
		t.Fatalf("adaptor must not see invalid input; saw %d calls", len(*calls))
	}
}

// Parent carried as a parent_child Relationship with To="" round-trips
// to the adaptor so the bd layer can translate it to `--parent`.
func TestCreateWorkItem_ParentRelationshipReachesAdaptor(t *testing.T) {
	host, calls := newCreateHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title":          "child",
			"kind":           "task",
			"status":         "open",
			"state_category": "backlog",
			"relationships": []map[string]any{
				{"kind": "parent_child", "from": "gm-epic-a", "to": ""},
			},
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-P", body))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("want 1 adaptor call, got %d", len(*calls))
	}
	rels := (*calls)[0].Relationships
	if len(rels) != 1 || rels[0].Kind != core.RelParentChild ||
		rels[0].From != "gm-epic-a" || rels[0].To != "" {
		t.Fatalf("parent relationship did not reach adaptor: %+v", rels)
	}
}

// Happy path: PATCH with a valid nonce updates the item and returns
// the materialized WorkItem.
func TestPatchWorkItem_HappyPath(t *testing.T) {
	host, rec := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := patchReq(t, "gm-1", "nonce-A", core.WorkItemPatch{
		Title: ptr("renamed"),
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	var got core.WorkItem
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body=%q", err, w.Body.String())
	}
	if got.ID != "gm-1" || got.Title != "renamed" {
		t.Fatalf("returned item mismatch: %+v", got)
	}
	if len(rec.patches) != 1 || rec.patches[0].Title == nil || *rec.patches[0].Title != "renamed" {
		t.Fatalf("adaptor saw %+v", rec.patches)
	}
}

// Missing nonce → 400 missing_confirm_nonce. The handler MUST NOT have
// run (rec.patches stays empty).
func TestPatchWorkItem_MissingNonce_Returns400(t *testing.T) {
	host, rec := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := patchReq(t, "gm-1", "", core.WorkItemPatch{Title: ptr("x")})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%q", w.Code, w.Body.String())
	}
	var env map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env["error"] != "missing_confirm_nonce" {
		t.Fatalf("want error=missing_confirm_nonce, got %v", env["error"])
	}
	if len(rec.patches) != 0 {
		t.Fatalf("handler ran on a missing-nonce request: %+v", rec.patches)
	}
}

// Replay: same nonce, second PATCH returns the cached response and the
// adaptor sees only one update. Verbatim status and body so a client
// retry is genuinely idempotent.
func TestPatchWorkItem_ReplayReturnsCached(t *testing.T) {
	host, rec := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, patchReq(t, "gm-1", "nonce-B", core.WorkItemPatch{Title: ptr("a")}))
	if first.Code != http.StatusOK {
		t.Fatalf("first: want 200, got %d", first.Code)
	}

	// Second call uses the SAME nonce but a DIFFERENT body; the cache
	// should win and the new body should NOT reach the adaptor.
	second := httptest.NewRecorder()
	h.ServeHTTP(second, patchReq(t, "gm-1", "nonce-B", core.WorkItemPatch{Title: ptr("b")}))
	if second.Code != first.Code {
		t.Fatalf("replay status drift: %d vs %d", second.Code, first.Code)
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("replay body drift:\n  first=%q\n  second=%q",
			first.Body.String(), second.Body.String())
	}
	if got := second.Header().Get(ConfirmHeader + "-Replay"); got != "true" {
		t.Errorf("replay must surface X-GEMBA-Confirm-Replay=true; got %q", got)
	}
	if len(rec.patches) != 1 {
		t.Fatalf("adaptor must see exactly 1 update on replay; got %d", len(rec.patches))
	}
}

// Adaptor returns KindReadOnly (the dolt-url path) → 405. The shared
// httperr mapper handles the conversion; this test pins the gate.
func TestPatchWorkItem_ReadOnlyAdaptor_Returns405(t *testing.T) {
	host, rec := newPatchHost(t)
	rec.updateRet = func(_ core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
		return core.WorkItem{}, core.NewAdaptorError(core.KindReadOnly,
			"adaptor is --dolt-url read-only")
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, patchReq(t, "gm-1", "nonce-RO", core.WorkItemPatch{}))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d; body=%q", w.Code, w.Body.String())
	}
}

// Adaptor returns KindAdaptorDegraded → 503.
func TestPatchWorkItem_AdaptorDegraded_Returns503(t *testing.T) {
	host, rec := newPatchHost(t)
	rec.updateRet = func(_ core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
		return core.WorkItem{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"bd probe timed out; Dolt may be hung")
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, patchReq(t, "gm-1", "nonce-deg", core.WorkItemPatch{}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

// Wrapped ErrNotFound is tagged in-handler so the envelope is
// self-describing — mirrors the GET handler's behaviour.
func TestPatchWorkItem_NotFoundIsTagged(t *testing.T) {
	host, rec := newPatchHost(t)
	rec.updateRet = func(id core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
		return core.WorkItem{}, core.ErrNotFound
	}
	_ = errors.Is // keep the import live; not needed but harmless if the line is dropped
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, patchReq(t, "gm-x", "nonce-nf", core.WorkItemPatch{}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env["error"] != "session_not_found" {
		t.Fatalf("want session_not_found, got %v", env["error"])
	}
	if !strings.Contains(env["message"].(string), "gm-x") {
		t.Fatalf("envelope message should include id; got %q", env["message"])
	}
}

// Body with unknown fields → 400. Keeps the wire schema honest so a
// typo in the patch payload can't be silently dropped.
func TestPatchWorkItem_UnknownField_Returns400(t *testing.T) {
	host, _ := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := bytes.NewBufferString(`{"not_a_real_field":"x"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/work-items/gm-1", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-bad")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%q", w.Code, w.Body.String())
	}
}

// Workspace-prefixed ids ("gemba/gemba/gm-foo") MUST be path-decoded
// before reaching the adaptor — same contract as the GET handler.
func TestPatchWorkItem_URLEncodedSlashes_DecodedBeforeUpdate(t *testing.T) {
	host, rec := newPatchHost(t)
	var got core.WorkItemID
	rec.updateRet = func(id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		got = id
		return core.WorkItem{ID: id, Kind: "task", Title: "x", Status: "open", StateCategory: core.StateBacklog}, nil
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body, _ := json.Marshal(core.WorkItemPatch{Title: ptr("ok")})
	req := httptest.NewRequest(http.MethodPatch,
		"/api/work-items/gemba%2Fgemba%2Fgm-foo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-prefix")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	if string(got) != "gemba/gemba/gm-foo" {
		t.Fatalf("adaptor saw encoded id: %q", got)
	}
}

// No host wired → 503 adaptor_not_configured. Mirrors the GET handler.
func TestPatchWorkItem_NoHost_Returns503(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, patchReq(t, "gm-1", "nonce-noh", core.WorkItemPatch{}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

func ptr[T any](v T) *T { return &v }
