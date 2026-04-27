// /api/v1/walks/* handlers + walk.* SSE event publishing (gm-wgv).
//
// Surfaces the walk.Store + walk.BuildAgenda machinery from gm-rfy as
// HTTP. Every mutation that lands on the store also Publishes a
// walk-prefixed events.GembaEvent through the Router's eventsHub so
// SSE subscribers (the /walk SPA in gm-i65, audit observers, future
// /metrics gauges) see one canonical signal.
//
// Scope deliberately narrow:
//
//   - POST /api/v1/walks:start runs BuildAgenda against the bound
//     walk.Sources and persists the result. Today walk.Sources is the
//     zero value (all nil), so the agenda is empty until gm-3jv
//     (cross-worker escalation aggregator) wires real listers. Tests
//     inject fakes via Router.AttachWalk.
//   - POST /api/v1/walks/{id}/turn appends a *user* turn only.
//     Persona response generation is gm-4p6's domain — this handler
//     just records the prompt + returns the updated walk. The SPA
//     can still render the user side; the persona reply lands when
//     gm-4p6 ships.
//   - POST /api/v1/walks/{id}/consult is a 501 stub. Multi-persona
//     consult-from-inside-a-walk is gm-4p6's surface.
//   - POST /api/v1/walks/{id}/end records the End transition but
//     does NOT generate the markdown summary artifact yet — that
//     belongs to gm-77u (Documentarian walk-summary skill).
//
// Auth + nonce: walks live behind the same auth middleware as the
// rest of /api. Mutating routes here do NOT carry X-GEMBA-Confirm
// today — the operations are low-blast-radius (no external side
// effects beyond the in-memory store) and the existing /events
// endpoint already deduplicates by event id. Add the gate later if
// the persona applier (gm-4p6) starts mutating WorkPlane state from
// inside /turn or /decide.
//
// Walk-id provenance: handlers that touch a specific walk decorate
// req.Context() with walk.ContextWithID before invoking downstream
// code, so future appliers reading walk.IDFromContext stamp
// ExecutedAction provenance correctly without per-handler plumbing.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/MikeBengtson/gemba/internal/skills/walk_summary"
	"github.com/MikeBengtson/gemba/internal/walk"
)

// walkPlane is the events.Plane stamped on every walk.* event.
// Distinct from PlaneWorkPlane / PlaneOrchestration so SSE
// subscribers can filter walks in or out without parsing kinds.
const walkPlane events.Plane = "walk"

// walkAdaptorID identifies the in-process server as the source of
// walk.* events. Stable across restarts so dedupe by (id) on the
// subscriber side stays consistent.
const walkAdaptorID = "gemba-server"

// Walk SSE event kinds. String values intentionally match the
// design's §API SSE table (walk.started, walk.agenda_added, …).
const (
	kindWalkStarted     events.Kind = "walk.started"
	kindWalkAgendaAdded events.Kind = "walk.agenda_added"
	kindWalkTurn        events.Kind = "walk.turn"
	kindWalkDecided     events.Kind = "walk.decided"
	kindWalkConsulted   events.Kind = "walk.consulted"
	kindWalkPaused      events.Kind = "walk.paused"
	kindWalkResumed     events.Kind = "walk.resumed"
	kindWalkEnded       events.Kind = "walk.ended"
)

// AttachWalk binds the walk.Store + walk.Sources used by the
// /api/v1/walks/* handlers. Calling with nil store removes the
// binding (handlers return 503). Sources is zero-value-safe — a
// nil EscalationLister / HITLLister / GateFailureLister /
// WorkItemLister contributes zero items to BuildAgenda. cmd/gemba
// serve calls this once at boot; tests inject fakes per case.
func (r *Router) AttachWalk(store walk.Store, sources walk.Sources) {
	r.walkStore = store
	r.walkSources = sources
}

// AttachWalkSummary binds the Documentarian's walk_summary skill
// (gm-77u). When attached, the End handler invokes Run after the
// store transition so a markdown artifact lands under
// docs/walks/<date>-<slug>.md. Pass nil to disable — the End
// handler still records the transition and emits the SSE event,
// just without producing the artifact.
func (r *Router) AttachWalkSummary(s *walk_summary.Skill) {
	r.walkSummary = s
}

// publishWalkEvent emits one walk.* GembaEvent through the hub. No-op
// when the hub is nil (degraded mode / tests that don't subscribe).
//
// gm-e3.6.1: ctx carries the request's W3C span context (the Trace
// middleware attaches it on the way in). events.WithTraceID stamps the
// trace_id onto the envelope at emission so a SPA mutation's
// traceparent flows end-to-end into the resulting /events SSE frame.
func (r *Router) publishWalkEvent(ctx context.Context, kind events.Kind, w walk.Walk, payload map[string]any) {
	if r.eventsHub == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["walk_id"] = string(w.ID)
	payload["status"] = string(w.Status)
	ev := events.GembaEvent{
		ID:      newWalkEventID(),
		Kind:    kind,
		At:      time.Now().UTC(),
		Source:  events.Source{Plane: walkPlane, AdaptorID: walkAdaptorID},
		AgentID: string(w.InitiatedBy),
		Payload: payload,
	}
	r.eventsHub.Publish(events.WithTraceID(ctx, ev))
}

// newWalkEventID returns a unique event id. Reuses the same crypto/
// rand-backed generator pattern as Router.newInstanceID via the
// nonceCache's id minting; we keep it inline here so walks.go
// doesn't reach across files. 16 hex bytes is plenty.
func newWalkEventID() string {
	return "walk-evt-" + newInstanceID()
}

// withWalkID returns a derived context decorated with the walk's id
// so downstream code (appliers in future slices) can read
// walk.IDFromContext without per-handler plumbing.
func withWalkID(ctx context.Context, id walk.ID) context.Context {
	return walk.ContextWithID(ctx, id)
}

// walkAdaptorUnavailable writes the 503 envelope handlers reach for
// when AttachWalk hasn't been called. Mirrors the WorkPlane handler
// shape so the SPA's "this surface isn't wired" branch matches.
func walkAdaptorUnavailable(w http.ResponseWriter) {
	httperr.Write(w, http.StatusServiceUnavailable,
		"adaptor_not_configured", "walk store not registered")
}

// startWalkRequest is the wire body of POST /api/v1/walks:start. All
// fields optional; the handler fills sensible defaults so a SPA can
// click "Start walk" with an empty body.
type startWalkRequest struct {
	Workspace         string   `json:"workspace,omitempty"`
	Label             string   `json:"label,omitempty"`
	InitiatedBy       string   `json:"initiated_by,omitempty"`
	PrimaryPersona    string   `json:"primary_persona,omitempty"`
	AssistingPersonas []string `json:"assisting_personas,omitempty"`
}

func (r *Router) startWalk(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}

	var body startWalkRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}

	ws := strings.TrimSpace(body.Workspace)
	if ws == "" {
		ws = r.cfg.Town
	}
	if ws == "" {
		ws = "default"
	}
	initiatedBy := core.AgentID(strings.TrimSpace(body.InitiatedBy))
	if initiatedBy == "" {
		initiatedBy = "operator"
	}
	primary := strings.TrimSpace(body.PrimaryPersona)
	if primary == "" {
		primary = "project-manager"
	}

	agenda, err := walk.BuildAgenda(req.Context(), r.walkSources, walk.AggregateOptions{
		Workspace: ws,
	})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	wk, err := r.walkStore.Start(req.Context(), walk.StartParams{
		Workspace:         ws,
		Label:             body.Label,
		InitiatedBy:       initiatedBy,
		PrimaryPersona:    primary,
		AssistingPersonas: body.AssistingPersonas,
		InitialAgenda:     agenda,
		Now:               time.Now().UTC(),
	})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	r.publishWalkEvent(req.Context(), kindWalkStarted, wk, map[string]any{
		"label":           wk.Label,
		"agenda_size":     len(wk.Agenda),
		"primary_persona": wk.PrimaryPersona,
	})

	writeJSON(w, http.StatusCreated, wk)
}

func (r *Router) getWalk(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id required")
		return
	}
	wk, err := r.walkStore.Get(req.Context(), id)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wk)
}

// addAgendaItemRequest is the wire body for POST /api/v1/walks/{id}/agenda.
// The caller produces topic + source; the handler stamps
// AddedAt + AddedBy + a stable-but-cheap id.
type addAgendaItemRequest struct {
	Topic    string                `json:"topic"`
	Source   walk.AgendaSource     `json:"source,omitempty"`
	Priority int                   `json:"priority,omitempty"`
	AddedBy  core.AgentID          `json:"added_by,omitempty"`
	Status   walk.AgendaItemStatus `json:"status,omitempty"`
	ID       string                `json:"id,omitempty"`
}

func (r *Router) addWalkAgenda(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id required")
		return
	}
	var body addAgendaItemRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Topic) == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "topic is required")
		return
	}
	now := time.Now().UTC()
	itemID := body.ID
	if itemID == "" {
		// Server-side generation — mirrors how the aggregator builds
		// stable ids when no ref is available.
		itemID = "user-" + newInstanceID()
	}
	source := body.Source
	if source.Kind == "" {
		source.Kind = walk.SourceUser
	}
	status := body.Status
	if status == "" {
		status = walk.AgendaQueued
	}
	addedBy := body.AddedBy
	if addedBy == "" {
		addedBy = "operator"
	}
	item := walk.AgendaItem{
		ID:       itemID,
		Topic:    body.Topic,
		Source:   source,
		Priority: body.Priority,
		Status:   status,
		AddedAt:  now,
		AddedBy:  addedBy,
	}
	ctx := withWalkID(req.Context(), id)
	wk, err := r.walkStore.AddAgendaItem(ctx, id, item)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	r.publishWalkEvent(ctx, kindWalkAgendaAdded, wk, map[string]any{
		"item_id":     itemID,
		"topic":       item.Topic,
		"source_kind": string(source.Kind),
	})
	writeJSON(w, http.StatusCreated, wk)
}

// patchAgendaRequest is the body of PATCH /api/v1/walks/{id}/agenda/{item}.
// Today only Status mutates; reorder + topic edits land later.
type patchAgendaRequest struct {
	Status walk.AgendaItemStatus `json:"status,omitempty"`
}

func (r *Router) patchWalkAgenda(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	itemID := chi.URLParam(req, "item")
	if id == "" || itemID == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id and item id required")
		return
	}
	var body patchAgendaRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.Status == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "status is required")
		return
	}
	ctx := withWalkID(req.Context(), id)
	wk, err := r.walkStore.UpdateAgendaItemStatus(ctx, id, itemID, body.Status)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wk)
}

// addTurnRequest is the body of POST /api/v1/walks/{id}/turn. We
// only accept user turns at this layer — persona response is gm-4p6.
type addTurnRequest struct {
	Content      string          `json:"content"`
	UserID       core.AgentID    `json:"user_id,omitempty"`
	AgendaItemID string          `json:"agenda_item_id,omitempty"`
	Citations    []walk.Citation `json:"citations,omitempty"`
}

func (r *Router) addWalkTurn(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id required")
		return
	}
	var body addTurnRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "content is required")
		return
	}
	userID := body.UserID
	if userID == "" {
		userID = "operator"
	}
	turn := walk.WalkTurn{
		At:           time.Now().UTC(),
		Speaker:      walk.SpeakerUser,
		UserID:       userID,
		Content:      body.Content,
		Citations:    body.Citations,
		AgendaItemID: body.AgendaItemID,
	}
	ctx := withWalkID(req.Context(), id)
	wk, err := r.walkStore.AppendTurn(ctx, id, turn)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	r.publishWalkEvent(ctx, kindWalkTurn, wk, map[string]any{
		"speaker":        string(walk.SpeakerUser),
		"agenda_item_id": body.AgendaItemID,
	})
	writeJSON(w, http.StatusOK, wk)
}

// decideRequest is the body of POST /api/v1/walks/{id}/decide/{item}.
type decideRequest struct {
	Kind             walk.DecisionKind `json:"kind"`
	Rationale        string            `json:"rationale,omitempty"`
	AppliedActionIDs []string          `json:"applied_action_ids,omitempty"`
	Citations        []walk.Citation   `json:"citations,omitempty"`
	DecidedBy        core.AgentID      `json:"decided_by,omitempty"`
}

func (r *Router) decideWalkItem(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	itemID := chi.URLParam(req, "item")
	if id == "" || itemID == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id and item id required")
		return
	}
	var body decideRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	switch body.Kind {
	case walk.DecisionRatify, walk.DecisionModify, walk.DecisionReject,
		walk.DecisionDefer, walk.DecisionHandoff:
	default:
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"kind must be one of ratify | modify | reject | defer | handoff")
		return
	}
	decidedBy := body.DecidedBy
	if decidedBy == "" {
		decidedBy = "operator"
	}
	decision := walk.WalkDecision{
		AgendaItemID:     itemID,
		Kind:             body.Kind,
		Rationale:        body.Rationale,
		AppliedActionIDs: body.AppliedActionIDs,
		Citations:        body.Citations,
		DecidedAt:        time.Now().UTC(),
		DecidedBy:        decidedBy,
	}
	ctx := withWalkID(req.Context(), id)
	wk, err := r.walkStore.AppendDecision(ctx, id, decision)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	r.publishWalkEvent(ctx, kindWalkDecided, wk, map[string]any{
		"item_id": itemID,
		"kind":    string(body.Kind),
	})
	writeJSON(w, http.StatusOK, wk)
}

func (r *Router) consultWalk(w http.ResponseWriter, _ *http.Request) {
	// Multi-persona consult-from-inside-a-walk lands with gm-4p6.
	// Returning 501 keeps the route discoverable in the OpenAPI
	// surface without lying about behaviour.
	httperr.Write(w, http.StatusNotImplemented, "not_implemented",
		"persona consult inside a walk is not yet wired; see gm-4p6")
}

func (r *Router) pauseWalk(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id required")
		return
	}
	ctx := withWalkID(req.Context(), id)
	current, err := r.walkStore.Get(ctx, id)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	summary := walk.BuildResumeContext(current, walk.ResumeOptions{})
	if _, err := r.walkStore.SetResumeContext(ctx, id, summary); err != nil {
		writeWalkError(w, err)
		return
	}
	wk, err := r.walkStore.Pause(ctx, id)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	r.publishWalkEvent(ctx, kindWalkPaused, wk, map[string]any{
		"resume_context_size": len(summary),
	})
	writeJSON(w, http.StatusOK, wk)
}

func (r *Router) resumeWalk(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id required")
		return
	}
	ctx := withWalkID(req.Context(), id)
	wk, err := r.walkStore.Resume(ctx, id)
	if err != nil {
		writeWalkError(w, err)
		return
	}
	r.publishWalkEvent(ctx, kindWalkResumed, wk, nil)
	writeJSON(w, http.StatusOK, wk)
}

func (r *Router) endWalk(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	id := walk.ID(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "walk id required")
		return
	}
	ctx := withWalkID(req.Context(), id)
	wk, err := r.walkStore.End(ctx, id, walk.StatusCompleted, time.Now().UTC())
	if err != nil {
		writeWalkError(w, err)
		return
	}
	// gm-77u: invoke the Documentarian walk-summary skill. Runs
	// unconditionally when attached (rather than gating on persona
	// membership) — every completed walk produces the same summary
	// artifact whether or not Documentarian assisted, which keeps
	// the operator-facing contract simple. Best-effort: a render or
	// write failure is logged but does not fail the End response —
	// the store transition has already committed and the SSE event
	// is what subscribers depend on.
	summaryPath := ""
	if r.walkSummary != nil {
		_, abs, sumErr := r.walkSummary.Run(ctx, wk, time.Now().UTC())
		if sumErr != nil {
			slog.Warn("walk_summary: render/write failed",
				"walk_id", wk.ID, "err", sumErr)
		} else {
			summaryPath = abs
		}
	}
	payload := map[string]any{
		"decisions_total": len(wk.Decisions),
		"agenda_total":    len(wk.Agenda),
	}
	if summaryPath != "" {
		payload["summary_path"] = summaryPath
	}
	r.publishWalkEvent(ctx, kindWalkEnded, wk, payload)
	writeJSON(w, http.StatusOK, wk)
}

func (r *Router) listRecentWalks(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	q := req.URL.Query()
	filter := walk.ListFilter{
		Workspace: q.Get("workspace"),
	}
	if v := q.Get("status"); v != "" {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			filter.Statuses = append(filter.Statuses, walk.Status(s))
		}
	}
	out, err := r.walkStore.List(req.Context(), filter)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if out == nil {
		out = []walk.Walk{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"walks": out,
		"total": len(out),
	})
}

// writeWalkError maps walk-package errors onto the shared error
// envelope. ErrNotFound → 404; ErrTerminal → 409 (the walk has
// reached a terminal state and the operation is no longer valid).
// Everything else flows through the generic mapper.
func writeWalkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, walk.ErrNotFound):
		httperr.Write(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, walk.ErrTerminal):
		httperr.Write(w, http.StatusConflict, "walk_terminal", err.Error())
	default:
		httperr.WriteError(w, err)
	}
}

// decodeJSONBody is the same shape the patch handler uses but
// permissive about empty bodies (the route is a noun + verb, not a
// PATCH probe). DisallowUnknownFields so a typo in the SPA surfaces
// as a 400 instead of silently dropping data.
func decodeJSONBody(req *http.Request, dst any) error {
	if req.Body == nil || req.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
