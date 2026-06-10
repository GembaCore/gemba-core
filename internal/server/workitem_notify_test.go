package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/bd"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/events"
	"github.com/GembaCore/gemba-core/internal/transport/api"
)

// stubBdRunner is a tiny stand-in for the real bd CLI just for the
// notify handler tests. It serves one bead by id; create / update
// fail because notify never invokes them. Mirrors the
// internal/adapter/bd test fakes in spirit but stays in this
// package to avoid a test-cycle import.
type stubBdRunner struct {
	id        string
	title     string
	status    string
	updatedAt string // RFC3339Nano
}

func (s *stubBdRunner) run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	switch args[0] {
	case "show":
		// args: ["show", "<id>", "--json"]; bd expects an ARRAY back
		// (matches `bd list --json` shape — show returns a single-row
		// array, not a bare object).
		if len(args) < 2 || args[1] != s.id {
			return nil, &exitError{stderr: "issue not found: " + args[1]}
		}
		body := []map[string]any{{
			"id":         s.id,
			"title":      s.title,
			"status":     s.status,
			"priority":   2,
			"issue_type": "task",
			"created_at": s.updatedAt,
			"updated_at": s.updatedAt,
		}}
		b, _ := json.Marshal(body)
		return b, nil
	case "list":
		return []byte("[]"), nil
	}
	return nil, nil
}

// exitError mimics the shape isNotFoundError keys off of in
// internal/adapter/bd/workplane.go (it pattern-matches "not found"
// in the message; isNotFoundError type-asserts to *exec.ExitError
// for stderr but falls back to err.Error() so this string is enough).
type exitError struct{ stderr string }

func (e *exitError) Error() string { return "bd: " + e.stderr }

// notifyTestRouter constructs a Router with a bd WorkPlane backed by
// a stub runner. Returns the router and the wired hub so tests can
// subscribe to events.
func notifyTestRouter(t *testing.T, runner *stubBdRunner) (*Router, *events.Hub) {
	t.Helper()
	wp := bd.NewWorkPlaneWithRunner(runner.run, "")
	host := api.New()
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	t.Cleanup(r.Close)

	// Wire the WorkPlane's emitter into the hub the same way
	// cmd/gemba serve does at boot.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch, err := wp.Subscribe(ctx, core.WorkPlaneSubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	r.EventsHub().AttachWorkPlaneStream(ctx, "bd", ch)
	return r, r.EventsHub()
}

// post sends the body to the notify endpoint and returns the
// response. Caller closes the body.
func post(t *testing.T, r *Router, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/workitems/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Result()
}

func TestNotify_Happy(t *testing.T) {
	stub := &stubBdRunner{
		id:        "gm-1",
		title:     "x",
		status:    "open",
		updatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	r, hub := notifyTestRouter(t, stub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := hub.Subscribe(ctx, events.Filter{})

	resp := post(t, r, `{"work_item_id":"gm-1","source":"test"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body notifyResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.WorkItemID != "gm-1" {
		t.Errorf("body.WorkItemID = %q", body.WorkItemID)
	}
	if body.Skipped {
		t.Error("first post should not be skipped")
	}
	if body.Kind != "workitem_updated" {
		t.Errorf("body.Kind = %q, want workitem_updated", body.Kind)
	}

	select {
	case ev := <-subCh:
		// The bd adaptor prefixes ids with the workspace/repo segments
		// from defaultPrefix; assert the suffix to stay format-agnostic.
		if !strings.HasSuffix(string(ev.WorkItemID), "/gm-1") {
			t.Errorf("hub event WorkItemID = %q, want suffix /gm-1", ev.WorkItemID)
		}
		if ev.Kind != events.WorkItemUpdated {
			t.Errorf("hub event Kind = %q, want %q", ev.Kind, events.WorkItemUpdated)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for SSE event")
	}
}

// gm-e4.3.2 DoD: closed beads emit workitem.closed.
func TestNotify_ClosedBeadEmitsClosedKind(t *testing.T) {
	stub := &stubBdRunner{
		id:        "gm-2",
		title:     "y",
		status:    "closed",
		updatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	r, hub := notifyTestRouter(t, stub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := hub.Subscribe(ctx, events.Filter{})

	resp := post(t, r, `{"work_item_id":"gm-2"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	select {
	case ev := <-subCh:
		if ev.Kind != events.WorkItemClosed {
			t.Errorf("Kind = %q, want %q", ev.Kind, events.WorkItemClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for SSE event")
	}
}

// gm-e4.3.2 DoD: idempotent — same notify twice does not double-publish.
func TestNotify_Idempotent(t *testing.T) {
	stub := &stubBdRunner{
		id:        "gm-3",
		title:     "z",
		status:    "open",
		updatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	r, hub := notifyTestRouter(t, stub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := hub.Subscribe(ctx, events.Filter{})

	// First post: publishes.
	resp1 := post(t, r, `{"work_item_id":"gm-3"}`)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", resp1.StatusCode)
	}
	select {
	case <-subCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first event")
	}

	// Second post with the same UpdatedAt: skipped.
	resp2 := post(t, r, `{"work_item_id":"gm-3"}`)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d", resp2.StatusCode)
	}
	var body notifyResponse
	_ = json.NewDecoder(resp2.Body).Decode(&body)
	if !body.Skipped {
		t.Errorf("second post should be skipped: %+v", body)
	}
	if body.Kind != "" {
		t.Errorf("skipped response kind = %q, want empty", body.Kind)
	}
	// No event should have arrived on the hub.
	select {
	case ev := <-subCh:
		t.Errorf("idempotent dup published: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

// A change in UpdatedAt re-emits even for the same id (the bead was
// touched again between notifies).
func TestNotify_NewUpdatedAtReEmits(t *testing.T) {
	stub := &stubBdRunner{
		id:        "gm-4",
		title:     "w",
		status:    "open",
		updatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	r, hub := notifyTestRouter(t, stub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := hub.Subscribe(ctx, events.Filter{})

	post(t, r, `{"work_item_id":"gm-4"}`)
	select {
	case <-subCh:
	case <-time.After(time.Second):
		t.Fatal("first event timeout")
	}

	// Simulate another mutation between notifies — the runner's
	// next `bd show` returns a fresher timestamp.
	stub.updatedAt = time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
	resp := post(t, r, `{"work_item_id":"gm-4"}`)
	var body notifyResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Skipped {
		t.Error("changed UpdatedAt should re-emit, not skip")
	}
	select {
	case <-subCh:
	case <-time.After(time.Second):
		t.Fatal("second event timeout")
	}
}

func TestNotify_MissingWorkItemID(t *testing.T) {
	stub := &stubBdRunner{id: "gm-1"}
	r, _ := notifyTestRouter(t, stub)

	resp := post(t, r, `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestNotify_MalformedJSON(t *testing.T) {
	stub := &stubBdRunner{id: "gm-1"}
	r, _ := notifyTestRouter(t, stub)

	req := httptest.NewRequest(http.MethodPost,
		"/api/workitems/notify", bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

func TestNotify_UnknownWorkItem(t *testing.T) {
	stub := &stubBdRunner{id: "gm-known"}
	r, _ := notifyTestRouter(t, stub)

	resp := post(t, r, `{"work_item_id":"gm-ghost"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestNotify_NoWorkPlaneRegistered(t *testing.T) {
	host := api.New() // no RegisterWorkPlane call
	r := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	t.Cleanup(r.Close)

	resp := post(t, r, `{"work_item_id":"gm-1"}`)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// gm-e4.3.2 DoD: bound WorkPlane that doesn't implement
// WorkItemNotifier (read-only adaptor) returns 409.
func TestNotify_NonNotifyingAdaptorReturnsConflict(t *testing.T) {
	host := api.New()
	if _, err := host.RegisterWorkPlane(context.Background(), nonNotifyingWorkPlane{}); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	t.Cleanup(r.Close)

	resp := post(t, r, `{"work_item_id":"gm-1"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
	body, _ := readBody(resp)
	if !strings.Contains(body, "capability_denied") {
		t.Errorf("body missing capability_denied: %s", body)
	}
}

// nonNotifyingWorkPlane satisfies core.WorkPlane minimally without
// implementing WorkItemNotifier — same shape as the dolt read-only
// adaptor for the purposes of this test.
type nonNotifyingWorkPlane struct{}

func (nonNotifyingWorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	return core.CapabilityManifest{
		AdaptorName:     "non-notifying",
		AdaptorVersion:  "0.0.0",
		ProtocolVersion: core.ProtocolVersion,
		Transport:       core.TransportAPI,
		StateMap: core.StateMap{
			"open": core.StateUnstarted,
		},
	}, nil
}
func (nonNotifyingWorkPlane) ListWorkItems(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
	return nil, nil
}
func (nonNotifyingWorkPlane) GetWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error) {
	return core.WorkItem{}, core.NewAdaptorError(core.KindSessionNotFound, "not found")
}
func (nonNotifyingWorkPlane) CreateWorkItem(context.Context, core.WorkItem) (core.WorkItem, error) {
	return core.WorkItem{}, core.NewAdaptorError(core.KindReadOnly, "ro")
}
func (nonNotifyingWorkPlane) UpdateWorkItem(context.Context, core.WorkItemID, core.WorkItemPatch) (core.WorkItem, error) {
	return core.WorkItem{}, core.NewAdaptorError(core.KindReadOnly, "ro")
}
func (nonNotifyingWorkPlane) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error {
	return core.NewAdaptorError(core.KindUnsupported, "unsupported")
}
func (nonNotifyingWorkPlane) ListSprints(context.Context) ([]core.Sprint, error) {
	return nil, nil
}
func (nonNotifyingWorkPlane) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, core.NewAdaptorError(core.KindUnsupported, "unsupported")
}
func (nonNotifyingWorkPlane) Subscribe(context.Context, core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	return nil, core.NewAdaptorError(core.KindUnsupported,
		"non-notifying: Subscribe is not supported")
}

func readBody(r *http.Response) (string, error) {
	defer r.Body.Close()
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r.Body)
	return buf.String(), err
}

func TestNotifyDeduper_FIFOEviction(t *testing.T) {
	d := newNotifyDeduper(2)
	if !d.shouldEmit("a", "t1") {
		t.Error("first 'a' should emit")
	}
	if !d.shouldEmit("b", "t1") {
		t.Error("first 'b' should emit")
	}
	if d.shouldEmit("a", "t1") {
		t.Error("repeat 'a' at t1 should be skipped")
	}
	// Insert third → 'a' (oldest insertion) is evicted.
	if !d.shouldEmit("c", "t1") {
		t.Error("first 'c' should emit")
	}
	// 'a' was evicted, so a re-post emits again (false negative — fine).
	if !d.shouldEmit("a", "t1") {
		t.Error("evicted 'a' re-emits")
	}
}

func TestNotifyDeduper_DefaultsToBoundedCap(t *testing.T) {
	d := newNotifyDeduper(0)
	if d.cap != defaultNotifyDedupCap {
		t.Errorf("cap = %d, want %d", d.cap, defaultNotifyDedupCap)
	}
}
