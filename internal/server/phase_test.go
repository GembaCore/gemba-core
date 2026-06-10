// Tests for /api/v1/phase + /api/v1/phase/advance (gm-jt9).
//
// Each test wires a fresh Router with a phase.MemoryStore and
// pre-subscribes to the Router's events hub so SSE publishing is
// asserted against the exact handler under test.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/core/phase"
	"github.com/GembaCore/gemba-core/internal/events"
)

// newPhaseRouter returns a Router whose phase store is seeded at the
// given Phase (or unattached when seed == ""), plus an event channel
// pre-subscribed to the Router's hub.
func newPhaseRouter(t *testing.T, seed phase.Phase) (*Router, phase.Store, <-chan events.GembaEvent) {
	t.Helper()
	r := NewRouter(config.ServeConfig{Town: "test-town"}, fakeSPA(), nil)
	t.Cleanup(r.Close)

	var store phase.Store
	if seed != "" {
		s, err := phase.NewSeededMemoryStore(seed, time.Now().UTC())
		if err != nil {
			t.Fatalf("NewSeededMemoryStore: %v", err)
		}
		store = s
		r.AttachPhase(store)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := r.EventsHub().Subscribe(ctx, events.Filter{})
	return r, store, ch
}

func decodePhaseView(t *testing.T, rec *httptest.ResponseRecorder) phaseStateView {
	t.Helper()
	var v phaseStateView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode phaseStateView: %v; body=%q", err, rec.Body.String())
	}
	return v
}

func phasePostReq(t *testing.T, path, nonce string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if nonce != "" {
		req.Header.Set(ConfirmHeader, nonce)
	}
	return req
}

// ---------------------------------------------------------------------
// 503 paths — handlers refuse to serve until AttachPhase has been called.
// ---------------------------------------------------------------------

func TestPhase_NotConfigured_503(t *testing.T) {
	r, _, _ := newPhaseRouter(t, "") // intentionally not attached

	cases := []struct {
		method, path string
		body         any
		nonce        string
	}{
		{http.MethodGet, "/api/v1/phase", nil, ""},
		{http.MethodPost, "/api/v1/phase/advance", advanceRequest{To: phase.PhaseDesign, Reason: "x"}, "n1"},
	}
	for _, tc := range cases {
		var req *http.Request
		if tc.method == http.MethodGet {
			req = httptest.NewRequest(tc.method, tc.path, nil)
		} else {
			req = phasePostReq(t, tc.path, tc.nonce, tc.body)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503; body=%q",
				tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

// ---------------------------------------------------------------------
// GET /api/v1/phase — current state.
// ---------------------------------------------------------------------

func TestGetPhase_ReturnsCurrentState(t *testing.T) {
	r, _, _ := newPhaseRouter(t, phase.PhaseBuilding)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/phase", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}
	view := decodePhaseView(t, w)
	if view.Phase != phase.PhaseBuilding {
		t.Errorf("Phase = %s, want building", view.Phase)
	}
	if view.History == nil {
		t.Error("History is nil; expected []")
	}
	if len(view.AllPhases) != len(phase.All()) {
		t.Errorf("AllPhases len = %d, want %d", len(view.AllPhases), len(phase.All()))
	}
	wantLegal := phase.LegalTransitionsFrom(phase.PhaseBuilding)
	if len(view.LegalTransitions) != len(wantLegal) {
		t.Errorf("LegalTransitions = %v, want %v", view.LegalTransitions, wantLegal)
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/phase/advance — happy path.
// ---------------------------------------------------------------------

func TestAdvancePhase_HappyPath(t *testing.T) {
	r, store, ch := newPhaseRouter(t, phase.PhaseDesign)

	body := advanceRequest{To: phase.PhaseBuilding, Reason: "ADR-12 ratified", By: "alice"}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, phasePostReq(t, "/api/v1/phase/advance", "nonce-A1", body))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}
	view := decodePhaseView(t, w)
	if view.Phase != phase.PhaseBuilding {
		t.Errorf("Phase = %s, want building", view.Phase)
	}
	if len(view.History) != 1 {
		t.Fatalf("History len = %d, want 1", len(view.History))
	}
	tr := view.History[0]
	if tr.From != phase.PhaseDesign || tr.To != phase.PhaseBuilding {
		t.Errorf("Transition = %s→%s, want design→building", tr.From, tr.To)
	}
	if tr.Reason != "ADR-12 ratified" {
		t.Errorf("Reason = %q", tr.Reason)
	}
	if string(tr.By) != "alice" {
		t.Errorf("By = %q, want alice", tr.By)
	}

	// Persisted in the store.
	persisted, _ := store.Get(context.Background())
	if persisted.Phase != phase.PhaseBuilding {
		t.Errorf("store.Get.Phase = %s, want building", persisted.Phase)
	}

	// Event was emitted.
	ev := drainEvent(t, ch, phase.KindPhaseTransitioned)
	if got, _ := ev.Payload["from"].(string); got != "design" {
		t.Errorf("event payload.from = %q", got)
	}
	if got, _ := ev.Payload["to"].(string); got != "building" {
		t.Errorf("event payload.to = %q", got)
	}
	if ev.AgentID != "alice" {
		t.Errorf("event AgentID = %q, want alice", ev.AgentID)
	}
}

// drainEvent waits up to 250ms for the next event matching kind.
func drainEvent(t *testing.T, ch <-chan events.GembaEvent, kind events.Kind) events.GembaEvent {
	t.Helper()
	deadline := time.After(250 * time.Millisecond)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("event hub closed before %s arrived", kind)
			}
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", kind)
		}
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/phase/advance — illegal transition rejected.
// ---------------------------------------------------------------------

func TestAdvancePhase_IllegalTransitionRejected(t *testing.T) {
	r, store, _ := newPhaseRouter(t, phase.PhaseIdeation)

	// ideation → shipping is not in the graph.
	body := advanceRequest{To: phase.PhaseShipping, Reason: "skip ahead"}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, phasePostReq(t, "/api/v1/phase/advance", "nonce-illegal", body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", w.Code, w.Body.String())
	}
	var env map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env["error"] != "invalid_transition" {
		t.Errorf("error code = %q, want invalid_transition", env["error"])
	}

	// Store unchanged.
	persisted, _ := store.Get(context.Background())
	if persisted.Phase != phase.PhaseIdeation {
		t.Errorf("store mutated despite illegal transition: %s", persisted.Phase)
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/phase/advance — invalid phase token.
// ---------------------------------------------------------------------

func TestAdvancePhase_InvalidPhaseTokenRejected(t *testing.T) {
	r, _, _ := newPhaseRouter(t, phase.PhaseDesign)
	body := advanceRequest{To: phase.Phase("bogus"), Reason: "x"}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, phasePostReq(t, "/api/v1/phase/advance", "nonce-bogus", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", w.Code, w.Body.String())
	}
	var env map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env["error"] != "invalid_phase" {
		t.Errorf("error code = %q, want invalid_phase", env["error"])
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/phase/advance — same-phase no-op returns 409.
// ---------------------------------------------------------------------

func TestAdvancePhase_SamePhaseConflict(t *testing.T) {
	r, _, _ := newPhaseRouter(t, phase.PhaseDesign)
	body := advanceRequest{To: phase.PhaseDesign, Reason: "stay"}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, phasePostReq(t, "/api/v1/phase/advance", "nonce-same", body))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%q", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/phase/advance — nonce required.
// ---------------------------------------------------------------------

func TestAdvancePhase_NonceRequired(t *testing.T) {
	r, _, _ := newPhaseRouter(t, phase.PhaseDesign)
	body := advanceRequest{To: phase.PhaseBuilding, Reason: "x"}
	w := httptest.NewRecorder()
	// No X-GEMBA-Confirm header.
	r.ServeHTTP(w, phasePostReq(t, "/api/v1/phase/advance", "", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing nonce); body=%q", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/phase/advance — empty reason rejected.
// ---------------------------------------------------------------------

func TestAdvancePhase_ReasonRequired(t *testing.T) {
	r, _, _ := newPhaseRouter(t, phase.PhaseDesign)
	body := advanceRequest{To: phase.PhaseBuilding, Reason: "   "}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, phasePostReq(t, "/api/v1/phase/advance", "nonce-noreason", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", w.Code, w.Body.String())
	}
	var env map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env["error"] != "bad_request" {
		t.Errorf("error code = %q, want bad_request", env["error"])
	}
}
