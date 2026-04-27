// Tests for /api/v1/walks/* (gm-wgv).
//
// Each test wires a fresh Router + walk.MemoryStore so cases are
// independent and the SSE hub stays under the test's control. SSE
// publishing is asserted by subscribing to the hub directly rather
// than scraping the /events stream — keeps the assertion narrow to
// the walk handler's contract.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/skills/walk_summary"
	"github.com/MikeBengtson/gemba/internal/walk"
)

// newWalkRouter returns a Router with a fresh in-memory walk store
// bound + an event channel pre-subscribed to the Router's hub. The
// returned hub channel observes every event the handlers emit.
func newWalkRouter(t *testing.T) (*Router, *walk.MemoryStore, <-chan events.GembaEvent) {
	t.Helper()
	r := NewRouter(config.ServeConfig{Town: "test-town"}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	store := walk.NewMemoryStore()
	r.AttachWalk(store, walk.Sources{})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := r.EventsHub().Subscribe(ctx, events.Filter{})
	return r, store, ch
}

// drainWalkEvent waits up to 250ms for one event matching kind on
// ch. Fails the test on timeout. Drops events of other kinds (e.g.
// when a single test triggers two transitions and only cares about
// the second one).
func drainWalkEvent(t *testing.T, ch <-chan events.GembaEvent, kind events.Kind) events.GembaEvent {
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

func walkReq(t *testing.T, method, path string, body any) *http.Request {
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

func decodeWalk(t *testing.T, rec *httptest.ResponseRecorder) walk.Walk {
	t.Helper()
	var w walk.Walk
	if err := json.Unmarshal(rec.Body.Bytes(), &w); err != nil {
		t.Fatalf("decode walk: %v; body=%q", err, rec.Body.String())
	}
	return w
}

// ---------------------------------------------------------------------
// 503 paths — handlers refuse to serve until AttachWalk has been called.
// ---------------------------------------------------------------------

func TestWalks_NotConfigured_503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	// Intentionally NOT calling AttachWalk.

	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/walks:start"},
		{http.MethodGet, "/api/v1/walks:recent"},
		{http.MethodGet, "/api/v1/walks/walk-x"},
		{http.MethodPost, "/api/v1/walks/walk-x/agenda"},
		{http.MethodPost, "/api/v1/walks/walk-x/turn"},
		{http.MethodPost, "/api/v1/walks/walk-x/decide/i"},
		{http.MethodPost, "/api/v1/walks/walk-x/pause"},
		{http.MethodPost, "/api/v1/walks/walk-x/resume"},
		{http.MethodPost, "/api/v1/walks/walk-x/end"},
	}
	for _, c := range cases {
		req := walkReq(t, c.method, c.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: want 503, got %d body=%q",
				c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/walks:start
// ---------------------------------------------------------------------

func TestStartWalk_DefaultsAndEvent(t *testing.T) {
	r, store, ch := newWalkRouter(t)

	req := walkReq(t, http.MethodPost, "/api/v1/walks:start", map[string]any{
		"label": "pre-release review",
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	wk := decodeWalk(t, rec)
	if wk.Workspace != "test-town" {
		t.Errorf("workspace default not honoured: %q", wk.Workspace)
	}
	if wk.PrimaryPersona != "project-manager" {
		t.Errorf("primary persona default not honoured: %q", wk.PrimaryPersona)
	}
	if wk.InitiatedBy != "operator" {
		t.Errorf("initiated_by default not honoured: %q", wk.InitiatedBy)
	}
	if wk.Status != walk.StatusActive {
		t.Errorf("want StatusActive, got %q", wk.Status)
	}

	// Store ↔ response agree.
	stored, err := store.Get(context.Background(), wk.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.ID != wk.ID {
		t.Fatalf("store walk id mismatch: %s vs %s", stored.ID, wk.ID)
	}

	ev := drainWalkEvent(t, ch, kindWalkStarted)
	if got := ev.Payload["walk_id"]; got != string(wk.ID) {
		t.Errorf("event walk_id mismatch: %v", got)
	}
}

func TestStartWalk_BadJSON(t *testing.T) {
	r, _, _ := newWalkRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/walks:start",
		strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// GET /api/v1/walks/{id}
// ---------------------------------------------------------------------

func TestGetWalk_NotFound(t *testing.T) {
	r, _, _ := newWalkRouter(t)
	req := walkReq(t, http.MethodGet, "/api/v1/walks/missing", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGetWalk_HappyPath(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk, err := store.Start(context.Background(), walk.StartParams{
		Workspace:   "ws",
		InitiatedBy: "op",
	})
	if err != nil {
		t.Fatalf("store.Start: %v", err)
	}
	req := walkReq(t, http.MethodGet, "/api/v1/walks/"+string(wk.ID), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if got.ID != wk.ID {
		t.Errorf("id mismatch: %s vs %s", got.ID, wk.ID)
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/walks/{id}/agenda
// ---------------------------------------------------------------------

func TestAddWalkAgenda_HappyPath(t *testing.T) {
	r, store, ch := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace:   "ws",
		InitiatedBy: "op",
	})
	body := map[string]any{
		"topic": "Discuss budget overrun",
	}
	req := walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/agenda", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if len(got.Agenda) != 1 {
		t.Fatalf("want 1 agenda item, got %d", len(got.Agenda))
	}
	if got.Agenda[0].Source.Kind != walk.SourceUser {
		t.Errorf("default source kind: %q", got.Agenda[0].Source.Kind)
	}
	if got.Agenda[0].Status != walk.AgendaQueued {
		t.Errorf("default status: %q", got.Agenda[0].Status)
	}
	ev := drainWalkEvent(t, ch, kindWalkAgendaAdded)
	if ev.Payload["walk_id"] != string(wk.ID) {
		t.Errorf("agenda_added event walk_id mismatch")
	}
}

func TestAddWalkAgenda_MissingTopic(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})
	req := walkReq(t, http.MethodPost,
		"/api/v1/walks/"+string(wk.ID)+"/agenda", map[string]any{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// PATCH /api/v1/walks/{id}/agenda/{item}
// ---------------------------------------------------------------------

func TestPatchWalkAgenda_StatusTransition(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
		InitialAgenda: []walk.AgendaItem{{
			ID: "item-1", Topic: "t",
			Source: walk.AgendaSource{Kind: walk.SourceUser},
			Status: walk.AgendaQueued,
		}},
	})

	body := map[string]any{"status": string(walk.AgendaActive)}
	req := walkReq(t, http.MethodPatch,
		"/api/v1/walks/"+string(wk.ID)+"/agenda/item-1", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if got.Agenda[0].Status != walk.AgendaActive {
		t.Errorf("status not updated: %q", got.Agenda[0].Status)
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/walks/{id}/turn
// ---------------------------------------------------------------------

func TestAddWalkTurn_AppendsUserTurn(t *testing.T) {
	r, store, ch := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})
	body := map[string]any{"content": "let's start"}
	req := walkReq(t, http.MethodPost,
		"/api/v1/walks/"+string(wk.ID)+"/turn", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if len(got.Transcript) != 1 {
		t.Fatalf("want 1 turn, got %d", len(got.Transcript))
	}
	if got.Transcript[0].Speaker != walk.SpeakerUser {
		t.Errorf("speaker: %q", got.Transcript[0].Speaker)
	}
	if got.Transcript[0].Content != "let's start" {
		t.Errorf("content: %q", got.Transcript[0].Content)
	}
	drainWalkEvent(t, ch, kindWalkTurn)
}

func TestAddWalkTurn_RequiresContent(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})
	req := walkReq(t, http.MethodPost,
		"/api/v1/walks/"+string(wk.ID)+"/turn", map[string]any{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/walks/{id}/decide/{item}
// ---------------------------------------------------------------------

func TestDecideWalkItem_HappyPath(t *testing.T) {
	r, store, ch := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
		InitialAgenda: []walk.AgendaItem{{
			ID: "item-1", Topic: "t",
			Source: walk.AgendaSource{Kind: walk.SourceUser},
			Status: walk.AgendaQueued,
		}},
	})
	body := map[string]any{
		"kind":      string(walk.DecisionRatify),
		"rationale": "looks good",
	}
	req := walkReq(t, http.MethodPost,
		"/api/v1/walks/"+string(wk.ID)+"/decide/item-1", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if len(got.Decisions) != 1 {
		t.Fatalf("want 1 decision, got %d", len(got.Decisions))
	}
	if got.Decisions[0].Kind != walk.DecisionRatify {
		t.Errorf("decision kind: %q", got.Decisions[0].Kind)
	}
	if got.Agenda[0].Status != walk.AgendaDecided {
		t.Errorf("agenda status not advanced: %q", got.Agenda[0].Status)
	}
	drainWalkEvent(t, ch, kindWalkDecided)
}

func TestDecideWalkItem_RejectsUnknownKind(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})
	body := map[string]any{"kind": "bogus"}
	req := walkReq(t, http.MethodPost,
		"/api/v1/walks/"+string(wk.ID)+"/decide/item-1", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// POST /api/v1/walks/{id}/consult
// ---------------------------------------------------------------------

func TestConsultWalk_NotImplemented(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})
	req := walkReq(t, http.MethodPost,
		"/api/v1/walks/"+string(wk.ID)+"/consult", map[string]any{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("want 501, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------
// pause / resume / end lifecycle
// ---------------------------------------------------------------------

func TestPauseResumeEnd_Lifecycle(t *testing.T) {
	r, store, ch := newWalkRouter(t)
	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})

	// pause
	req := walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/pause", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if got.Status != walk.StatusPaused {
		t.Errorf("paused status: %q", got.Status)
	}
	if got.ResumeContext == "" {
		t.Errorf("expected resume context to be populated")
	}
	drainWalkEvent(t, ch, kindWalkPaused)

	// resume
	req = walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/resume", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got = decodeWalk(t, rec)
	if got.Status != walk.StatusActive {
		t.Errorf("resumed status: %q", got.Status)
	}
	drainWalkEvent(t, ch, kindWalkResumed)

	// end
	req = walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/end", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("end: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	got = decodeWalk(t, rec)
	if got.Status != walk.StatusCompleted {
		t.Errorf("ended status: %q", got.Status)
	}
	if got.EndedAt == nil {
		t.Errorf("ended_at should be set")
	}
	drainWalkEvent(t, ch, kindWalkEnded)

	// post-end mutation is rejected with the walk_terminal envelope.
	req = walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/turn",
		map[string]any{"content": "too late"})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("post-end turn: want 409, got %d body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env["error"] != "walk_terminal" {
		t.Errorf("want walk_terminal envelope, got %q", env["error"])
	}
}

// ---------------------------------------------------------------------
// GET /api/v1/walks:recent
// ---------------------------------------------------------------------

func TestListRecentWalks_FilterByWorkspaceAndStatus(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	w1, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "alpha", InitiatedBy: "op",
	})
	_, _ = store.Start(context.Background(), walk.StartParams{
		Workspace: "beta", InitiatedBy: "op",
	})
	// Pause one of them so the status filter can discriminate.
	_, _ = store.Pause(context.Background(), w1.ID)

	req := walkReq(t, http.MethodGet,
		"/api/v1/walks:recent?workspace=alpha&status=paused", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Walks []walk.Walk `json:"walks"`
		Total int         `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Total != 1 || len(env.Walks) != 1 {
		t.Fatalf("want 1 walk, got %d (%+v)", env.Total, env.Walks)
	}
	if env.Walks[0].Workspace != "alpha" {
		t.Errorf("workspace filter leak: %q", env.Walks[0].Workspace)
	}
	if env.Walks[0].Status != walk.StatusPaused {
		t.Errorf("status filter leak: %q", env.Walks[0].Status)
	}
}

// ---------------------------------------------------------------------
// BuildAgenda integration — fakes flow through into the persisted agenda.
// ---------------------------------------------------------------------

type fakeWorkItemLister struct {
	out []core.WorkItem
}

func (f fakeWorkItemLister) ListWorkItems(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
	// Only return for the "recent unstarted" filter so we land on
	// SourceBeadFiled exactly once.
	for _, sc := range filter.StateCategory {
		if sc == core.StateUnstarted {
			return f.out, nil
		}
	}
	return nil, nil
}

// gm-77u: when a walk_summary skill is attached, ending a walk
// produces a markdown artifact under docs/walks/ and the End SSE
// event payload carries the absolute path. Validates both the
// integration (handler → skill) and the SSE payload contract the
// SPA's preview pane will read.
func TestEndWalk_InvokesWalkSummarySkill(t *testing.T) {
	r, store, ch := newWalkRouter(t)
	tmpRoot := t.TempDir()
	r.AttachWalkSummary(walk_summary.New(walk_summary.NewApplier(tmpRoot)))

	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace:   "ws",
		Label:       "Sprint retro",
		InitiatedBy: "op",
	})

	req := walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/end", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("end: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	ev := drainWalkEvent(t, ch, kindWalkEnded)
	got, _ := ev.Payload["summary_path"].(string)
	if got == "" {
		t.Fatalf("expected summary_path in payload, got %v", ev.Payload)
	}
	if !strings.HasPrefix(got, tmpRoot) {
		t.Errorf("summary_path %q not under tmp root %q", got, tmpRoot)
	}
	if !strings.HasSuffix(got, "-sprint-retro.md") {
		t.Errorf("summary_path suffix unexpected: %q", got)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(body), "Sprint retro") {
		t.Errorf("summary missing label: %q", body)
	}
}

// A render failure (or any other walk_summary error) MUST NOT fail
// the End response — the store transition has already committed.
// The SSE event still fires; the summary_path is simply omitted.
func TestEndWalk_SkillFailureDoesNotBreakEnd(t *testing.T) {
	r, store, ch := newWalkRouter(t)
	// Applier configured with a path that points at a *file*
	// (existing temp file), so MkdirAll will fail when the skill
	// tries to create docs/ underneath it.
	tmpRoot := t.TempDir()
	bogusRoot := tmpRoot + "/file-not-dir"
	if err := os.WriteFile(bogusRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.AttachWalkSummary(walk_summary.New(walk_summary.NewApplier(bogusRoot)))

	wk, _ := store.Start(context.Background(), walk.StartParams{
		Workspace: "ws", InitiatedBy: "op",
	})

	req := walkReq(t, http.MethodPost, "/api/v1/walks/"+string(wk.ID)+"/end", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("end should still 200 on skill failure: got %d body=%q", rec.Code, rec.Body.String())
	}
	ev := drainWalkEvent(t, ch, kindWalkEnded)
	if _, ok := ev.Payload["summary_path"]; ok {
		t.Errorf("summary_path should be omitted on failure: %v", ev.Payload)
	}
}

func TestStartWalk_AgendaFromSources(t *testing.T) {
	r := NewRouter(config.ServeConfig{Town: "ws"}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	store := walk.NewMemoryStore()
	r.AttachWalk(store, walk.Sources{
		WorkItems: fakeWorkItemLister{out: []core.WorkItem{
			{ID: "gm-1", Title: "Stale bead"},
		}},
	})

	req := walkReq(t, http.MethodPost, "/api/v1/walks:start", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	got := decodeWalk(t, rec)
	if len(got.Agenda) != 1 {
		t.Fatalf("expected 1 agenda item from fake WorkItemLister, got %d", len(got.Agenda))
	}
	if got.Agenda[0].Source.Kind != walk.SourceBeadFiled {
		t.Errorf("source kind: %q", got.Agenda[0].Source.Kind)
	}
	if got.Agenda[0].Source.Ref != "gm-1" {
		t.Errorf("source ref: %q", got.Agenda[0].Source.Ref)
	}
}
