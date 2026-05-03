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
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/events"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
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

func deleteReq(id, nonce string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/work-items/"+id, nil)
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

func TestCreateWorkItem_BeadsReadOnlyBlocksBeforeAdaptor(t *testing.T) {
	host, calls := newCreateHost(t)
	h := NewRouter(config.ServeConfig{BeadsOnly: true, BeadsReadOnly: true}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title": "x", "kind": "task",
			"status": "open", "state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-RO", body))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(*calls) != 0 {
		t.Fatalf("read-only mode must not call adaptor; saw %d calls", len(*calls))
	}
}

// Milestone create auto-prefixes the title with `M<n>` (gm-lw6h).
// Numbering is monotonic against the existing milestone set.
func TestCreateWorkItem_MilestoneAutoPrefixes(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.ListFn = func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
		return []core.WorkItem{
			{ID: "gm-a", Kind: core.KindMilestone, Title: "M1 Beta"},
			{ID: "gm-b", Kind: core.KindMilestone, Title: "M3 Q3"},
		}, nil
	}
	var seen core.WorkItem
	wp.CreateFn = func(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
		seen = wi
		out := wi
		out.ID = "gm-new"
		return out, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title":          "Production launch",
			"kind":           core.KindMilestone,
			"status":         "open",
			"state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-M", body))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%q", w.Code, w.Body.String())
	}
	if seen.Title != "M4 Production launch" {
		t.Errorf("title sent to adaptor = %q, want %q", seen.Title, "M4 Production launch")
	}
}

// Operator-supplied M<n> prefix is preserved (gm-lw6h).
func TestCreateWorkItem_MilestoneRespectsExplicitPrefix(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.ListFn = func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
		return []core.WorkItem{
			{ID: "gm-a", Kind: core.KindMilestone, Title: "M5 thing"},
		}, nil
	}
	var seen core.WorkItem
	wp.CreateFn = func(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
		seen = wi
		out := wi
		out.ID = "gm-new"
		return out, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	body := map[string]any{
		"item": map[string]any{
			"title":          "M99 Special",
			"kind":           core.KindMilestone,
			"status":         "open",
			"state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-MX", body))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%q", w.Code, w.Body.String())
	}
	if seen.Title != "M99 Special" {
		t.Errorf("operator prefix lost: title = %q, want %q", seen.Title, "M99 Special")
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

// Adaptor returns KindReadOnly (explicit read-only path) → 405. The shared
// httperr mapper handles the conversion; this test pins the gate.
func TestPatchWorkItem_ReadOnlyAdaptor_Returns405(t *testing.T) {
	host, rec := newPatchHost(t)
	rec.updateRet = func(_ core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
		return core.WorkItem{}, core.NewAdaptorError(core.KindReadOnly,
			"adaptor is explicitly read-only")
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

// gm-gsbj — `parent_id` on the PATCH body must round-trip through the
// boundary decoder and reach the adaptor as patch.Parent. Both the
// "set" path (string id) and the "clear" path (null) are pinned —
// dropping either is what blocks the milestone child-epic panel.
func TestPatchWorkItem_ParentIDReachesAdaptor(t *testing.T) {
	host, rec := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"parent_id":"gm-foo"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/work-items/gm-1", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-parent-set")
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(rec.patches) != 1 {
		t.Fatalf("want 1 adaptor call, got %d", len(rec.patches))
	}
	if rec.patches[0].Parent == nil || *rec.patches[0].Parent != "gm-foo" {
		t.Fatalf("patch.Parent did not reach adaptor: %+v", rec.patches[0].Parent)
	}
}

func TestPatchWorkItem_ParentIDClearReachesAdaptor(t *testing.T) {
	host, rec := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	// JSON null on parent_id is the clear sentinel: pointer to "" on
	// the Go side, which the bd adaptor translates to `--parent ""`.
	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"parent_id":""}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/work-items/gm-1", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-parent-clear")
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(rec.patches) != 1 {
		t.Fatalf("want 1 adaptor call, got %d", len(rec.patches))
	}
	if rec.patches[0].Parent == nil {
		t.Fatalf("clear-sentinel lost: patch.Parent==nil")
	}
	if *rec.patches[0].Parent != "" {
		t.Fatalf("clear-sentinel mangled: patch.Parent=%q", *rec.patches[0].Parent)
	}
}

// Absence of parent_id keeps patch.Parent nil so an unrelated PATCH
// can't accidentally orphan the bead.
func TestPatchWorkItem_ParentIDAbsentLeavesNil(t *testing.T) {
	host, rec := newPatchHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"title":"renamed"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/work-items/gm-1", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "nonce-parent-absent")
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	if len(rec.patches) != 1 {
		t.Fatalf("want 1 adaptor call, got %d", len(rec.patches))
	}
	if rec.patches[0].Parent != nil {
		t.Fatalf("patch.Parent must stay nil when parent_id absent; got %+v", rec.patches[0].Parent)
	}
}

func TestDeleteWorkItem_HappyPath(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	var got core.WorkItemID
	wp.DeleteFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		got = id
		return core.WorkItem{
			ID:            id,
			Kind:          "task",
			Title:         "old bead",
			Status:        "open",
			StateCategory: core.StateBacklog,
		}, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, deleteReq("gemba%2Fgemba%2Fgm-delete", "nonce-delete"))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	if got != "gemba/gemba/gm-delete" {
		t.Fatalf("adaptor saw %q", got)
	}
	var out core.WorkItem
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ID != got {
		t.Fatalf("returned item id=%q want %q", out.ID, got)
	}
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------
// Wrapper state reconciliation (gm-mqiz, gm-1o9n)
// ---------------------------------------------------------------------

// milestoneAutocloseFixture wires a FakeWorkPlane backed by an
// in-memory map of WorkItems so a PATCH to a leaf can observe whether
// epic/milestone wrappers are reconciled from descendant leaves. The
// map is keyed by id and mutated by both UpdateFn and the reconcile
// path.
type milestoneAutocloseFixture struct {
	host    *api.Host
	router  *Router
	wp      *testadaptors.FakeWorkPlane
	items   map[core.WorkItemID]core.WorkItem
	updates []core.WorkItemID // every UpdateWorkItem id, in call order
}

func newMilestoneAutocloseFixture(t *testing.T, items []core.WorkItem) *milestoneAutocloseFixture {
	t.Helper()
	fx := &milestoneAutocloseFixture{
		items: make(map[core.WorkItemID]core.WorkItem, len(items)),
	}
	for _, it := range items {
		fx.items[it.ID] = it
	}
	fx.host = api.New()
	fx.wp = testadaptors.NewFakeWorkPlane(core.TransportAPI)
	fx.wp.GetFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		if it, ok := fx.items[id]; ok {
			return it, nil
		}
		return core.WorkItem{}, core.ErrNotFound
	}
	fx.wp.UpdateFn = func(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		fx.updates = append(fx.updates, id)
		it, ok := fx.items[id]
		if !ok {
			return core.WorkItem{}, core.ErrNotFound
		}
		if patch.StateCategory != nil {
			it.StateCategory = *patch.StateCategory
		}
		if patch.Status != nil {
			it.Status = *patch.Status
		}
		if patch.Title != nil {
			it.Title = *patch.Title
		}
		fx.items[id] = it
		return it, nil
	}
	if _, err := fx.host.RegisterWorkPlane(context.Background(), fx.wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	fx.router = NewRouter(config.ServeConfig{}, fakeSPA(), fx.host)
	return fx
}

func parentChildRel(parent, child core.WorkItemID) core.Relationship {
	return core.Relationship{Kind: core.RelParentChild, From: parent, To: child}
}

// Patching the LAST open descendant leaf of a milestone closes the
// containing epic and milestone, then emits an escalation.opened event.
func TestPatchWorkItem_DerivesWrappersCompletedFromLeafClosure(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicA := core.WorkItemID("gm-epic-a")
	epicB := core.WorkItemID("gm-epic-b")
	leafA := core.WorkItemID("gm-leaf-a")
	leafB := core.WorkItemID("gm-leaf-b")

	fx := newMilestoneAutocloseFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
				parentChildRel(milestoneID, epicB),
			},
		},
		{
			ID: epicA, Kind: "epic", Title: "epic-a",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
				parentChildRel(epicA, leafA),
			},
		},
		{
			ID: epicB, Kind: "epic", Title: "epic-b",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicB),
				parentChildRel(epicB, leafB),
			},
		},
		{
			ID: leafA, Kind: "task", Title: "leaf-a",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{parentChildRel(epicA, leafA)},
		},
		{
			ID: leafB, Kind: "task", Title: "leaf-b",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{parentChildRel(epicB, leafB)},
		},
	})

	// Subscribe to the hub BEFORE we issue the PATCH so we don't race
	// with the auto-close emit.
	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	evCh := fx.router.eventsHub.Subscribe(subCtx, events.Filter{})

	// Close leaf-b — the only open runnable item remaining.
	completed := core.StateCompleted
	body := core.WorkItemPatch{StateCategory: &completed}
	w := httptest.NewRecorder()
	fx.router.ServeHTTP(w, patchReq(t, string(leafB), "nonce-mq-A", body))
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH leaf-b: want 200, got %d; body=%q", w.Code, w.Body.String())
	}

	if got := fx.items[epicB].StateCategory; got != core.StateCompleted {
		t.Fatalf("epic-b state_category = %q, want %q", got, core.StateCompleted)
	}
	if got := fx.items[milestoneID].StateCategory; got != core.StateCompleted {
		t.Fatalf("milestone state_category = %q, want %q", got, core.StateCompleted)
	}

	// The reconcile path updates the leaf, then its epic wrapper, then
	// the milestone wrapper.
	wantUpdates := []core.WorkItemID{leafB, epicB, milestoneID}
	if len(fx.updates) != len(wantUpdates) {
		t.Fatalf("UpdateWorkItem call sequence = %v, want %v", fx.updates, wantUpdates)
	}
	for i, id := range wantUpdates {
		if fx.updates[i] != id {
			t.Fatalf("update[%d] = %q, want %q (full=%v)", i, fx.updates[i], id, fx.updates)
		}
	}

	// And an escalation.opened event must have fired with the
	// milestone in the work_item_id slot. Drain with a tight timeout
	// so a regression to "no emit" fails fast instead of hanging.
	select {
	case ev := <-evCh:
		if ev.Kind != events.EscalationOpened {
			t.Fatalf("event kind = %q, want %q", ev.Kind, events.EscalationOpened)
		}
		if ev.WorkItemID != string(milestoneID) {
			t.Fatalf("event work_item_id = %q, want %q", ev.WorkItemID, milestoneID)
		}
		if reason, _ := ev.Payload["reason"].(string); reason != "milestone_autoclosed" {
			t.Errorf("payload.reason = %q, want milestone_autoclosed", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected escalation.opened on auto-close; got nothing in 2s")
	}
}

func TestPatchWorkItem_DerivesWrappersStartedWhenLeafStarts(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicA := core.WorkItemID("gm-epic-a")
	leafA := core.WorkItemID("gm-leaf-a")

	fx := newMilestoneAutocloseFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
			},
		},
		{
			ID: epicA, Kind: "epic", Title: "epic-a",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
				parentChildRel(epicA, leafA),
			},
		},
		{
			ID: leafA, Kind: "task", Title: "leaf-a",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(epicA, leafA)},
		},
	})

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	evCh := fx.router.eventsHub.Subscribe(subCtx, events.Filter{})

	started := core.StateStarted
	body := core.WorkItemPatch{StateCategory: &started}
	w := httptest.NewRecorder()
	fx.router.ServeHTTP(w, patchReq(t, string(leafA), "nonce-mq-B", body))
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH leaf-a: want 200, got %d", w.Code)
	}

	if got := fx.items[epicA].StateCategory; got != core.StateStarted {
		t.Fatalf("epic state_category = %q, want %q", got, core.StateStarted)
	}
	if got := fx.items[milestoneID].StateCategory; got != core.StateStarted {
		t.Fatalf("milestone state_category = %q, want %q", got, core.StateStarted)
	}
	wantUpdates := []core.WorkItemID{leafA, epicA, milestoneID}
	if len(fx.updates) != len(wantUpdates) {
		t.Fatalf("UpdateWorkItem call sequence = %v, want %v", fx.updates, wantUpdates)
	}
	for i, id := range wantUpdates {
		if fx.updates[i] != id {
			t.Fatalf("update[%d] = %q, want %q (full=%v)", i, fx.updates[i], id, fx.updates)
		}
	}
	select {
	case ev := <-evCh:
		t.Fatalf("unexpected event published: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

// When some leaf work is complete and some remains open, wrappers
// remain in progress instead of prematurely completing.
func TestPatchWorkItem_DerivesWrappersStartedWhenSiblingLeavesOpen(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicA := core.WorkItemID("gm-epic-a")
	leafA := core.WorkItemID("gm-leaf-a")
	leafB := core.WorkItemID("gm-leaf-b")

	fx := newMilestoneAutocloseFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(milestoneID, epicA)},
		},
		{
			ID: epicA, Kind: "epic", Title: "epic-a",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
				parentChildRel(epicA, leafA),
				parentChildRel(epicA, leafB),
			},
		},
		{
			ID: leafA, Kind: "task", Title: "leaf-a",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{parentChildRel(epicA, leafA)},
		},
		{
			ID: leafB, Kind: "task", Title: "leaf-b",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(epicA, leafB)},
		},
	})

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	evCh := fx.router.eventsHub.Subscribe(subCtx, events.Filter{})

	completed := core.StateCompleted
	body := core.WorkItemPatch{StateCategory: &completed}
	w := httptest.NewRecorder()
	fx.router.ServeHTTP(w, patchReq(t, string(leafA), "nonce-mq-C", body))
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH leaf-a: want 200, got %d", w.Code)
	}

	if got := fx.items[epicA].StateCategory; got != core.StateStarted {
		t.Fatalf("epic state_category = %q, want %q", got, core.StateStarted)
	}
	if got := fx.items[milestoneID].StateCategory; got != core.StateStarted {
		t.Fatalf("milestone state_category = %q, want %q", got, core.StateStarted)
	}
	wantUpdates := []core.WorkItemID{leafA, epicA, milestoneID}
	if len(fx.updates) != len(wantUpdates) {
		t.Fatalf("UpdateWorkItem call sequence = %v, want %v", fx.updates, wantUpdates)
	}
	for i, id := range wantUpdates {
		if fx.updates[i] != id {
			t.Fatalf("update[%d] = %q, want %q (full=%v)", i, fx.updates[i], id, fx.updates)
		}
	}
	select {
	case ev := <-evCh:
		t.Fatalf("unexpected event published: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

// If the milestone is already closed, a subsequent child-close PATCH
// must NOT re-fire the notification or re-patch the milestone.
func TestPatchWorkItem_AlreadyClosedMilestoneDoesNotRefire(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicA := core.WorkItemID("gm-epic-a")
	epicB := core.WorkItemID("gm-epic-b")
	leafA := core.WorkItemID("gm-leaf-a")
	leafB := core.WorkItemID("gm-leaf-b")

	fx := newMilestoneAutocloseFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateCompleted, // already closed
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
				parentChildRel(milestoneID, epicB),
			},
		},
		{
			ID: epicA, Kind: "epic", Title: "epic-a",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicA),
				parentChildRel(epicA, leafA),
			},
		},
		{
			ID: epicB, Kind: "epic", Title: "epic-b",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicB),
				parentChildRel(epicB, leafB),
			},
		},
		{
			ID: leafA, Kind: "task", Title: "leaf-a",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{parentChildRel(epicA, leafA)},
		},
		{
			ID: leafB, Kind: "task", Title: "leaf-b",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{parentChildRel(epicB, leafB)},
		},
	})

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	evCh := fx.router.eventsHub.Subscribe(subCtx, events.Filter{})

	// Close leaf-b — milestone is already closed; no rollup should
	// trigger.
	completed := core.StateCompleted
	body := core.WorkItemPatch{StateCategory: &completed}
	w := httptest.NewRecorder()
	fx.router.ServeHTTP(w, patchReq(t, string(leafB), "nonce-mq-D", body))
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH leaf-b: want 200, got %d", w.Code)
	}

	// Exactly one update — the caller's PATCH. Wrappers must not be
	// re-patched when their derived state is unchanged.
	if len(fx.updates) != 1 || fx.updates[0] != leafB {
		t.Fatalf("UpdateWorkItem calls = %v, want [%q] (milestone re-patched?)",
			fx.updates, leafB)
	}
	select {
	case ev := <-evCh:
		t.Fatalf("unexpected duplicate notification: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}
