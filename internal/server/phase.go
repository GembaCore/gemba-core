// /api/v1/phase handlers (gm-jt9).
//
// Surfaces the project-level Phase primitive from internal/core/phase
// as HTTP. Two routes:
//
//   - GET  /api/v1/phase           — current WorkspaceState (phase +
//     since + history) plus the canonical phases list and the legal
//     transitions reachable from the current phase. The latter two
//     keep the SPA from hard-coding the enum.
//   - POST /api/v1/phase/advance   — apply one transition. Body:
//     {"to": "<phase>", "reason": "..."}. Nonce-gated via the same
//     X-GEMBA-Confirm middleware every other mutating route uses.
//
// Both routes return 503 when AttachPhase has not been called.
//
// Events: every successful POST emits one phase.transitioned
// GembaEvent through the Router's eventsHub so SSE subscribers see the
// transition. The trace id from the request context propagates onto
// the event via events.WithTraceID (handled inside phase.TransitionEvent).
//
// Auth: phase routes live under /api which is auth-gated by the same
// middleware as the rest of the API surface. The advance route adds
// the nonce gate on top so a SPA double-submit can't double-record a
// transition.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/core/phase"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// AttachPhase binds the phase.Store the /api/v1/phase handlers read
// from. cmd/gemba serve calls this once at boot after seeding the
// initial WorkspaceState; tests inject a phase.NewMemoryStore (or a
// pre-seeded NewSeededMemoryStore) per case. Calling with nil
// detaches the binding — handlers return 503 until the next
// AttachPhase call.
func (r *Router) AttachPhase(store phase.Store) {
	r.phaseStore = store
}

// PhaseStore returns the phase.Store this router was last attached
// with, or nil. cmd/gemba serve uses this to share the store with
// other subsystems (e.g. gm-3on's Purview-as-gate path will read it
// to decide whether a Manager mutation is allowed).
func (r *Router) PhaseStore() phase.Store { return r.phaseStore }

// phaseStateView is the GET /api/v1/phase response. Adds the canonical
// phases list and the legal transitions from the current phase so the
// SPA can render the picker without a second roundtrip.
type phaseStateView struct {
	Phase            phase.Phase        `json:"phase"`
	Since            time.Time          `json:"since"`
	History          []phase.Transition `json:"history"`
	AllPhases        []phase.Phase      `json:"all_phases"`
	LegalTransitions []phase.Phase      `json:"legal_transitions"`
}

// advanceRequest is the POST /api/v1/phase/advance body. Both fields
// required — empty target rejected for shape, empty reason rejected
// because the Transition's audit value depends on it. Operators
// performing a known-illegal transition (graph rejects it) get a 409
// today; a future bead may add an explicit `force` field with a
// nonce-confirmed override path. gm-jt9 ships the strict-rail version.
type advanceRequest struct {
	To     phase.Phase `json:"to"`
	Reason string      `json:"reason"`
	By     string      `json:"by,omitempty"` // optional override; defaults to "operator"
}

func (r *Router) phaseAdaptorUnavailable(w http.ResponseWriter) {
	httperr.Write(w, http.StatusServiceUnavailable,
		"adaptor_not_configured", "phase store not registered")
}

func (r *Router) getPhase(w http.ResponseWriter, req *http.Request) {
	if r.phaseStore == nil {
		r.phaseAdaptorUnavailable(w)
		return
	}
	state, err := r.phaseStore.Get(req.Context())
	if err != nil {
		if errors.Is(err, phase.ErrNotFound) {
			r.phaseAdaptorUnavailable(w)
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	view := phaseStateView{
		Phase:            state.Phase,
		Since:            state.Since,
		History:          state.History,
		AllPhases:        phase.All(),
		LegalTransitions: phase.LegalTransitionsFrom(state.Phase),
	}
	if view.History == nil {
		// JSON `null` is uglier than `[]` for the SPA's history list.
		view.History = []phase.Transition{}
	}
	writeJSON(w, http.StatusOK, view)
}

func (r *Router) advancePhase(w http.ResponseWriter, req *http.Request) {
	if r.phaseStore == nil {
		r.phaseAdaptorUnavailable(w)
		return
	}
	var body advanceRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	if strings.TrimSpace(string(body.To)) == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "to is required")
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "reason is required")
		return
	}
	by := core.AgentID(strings.TrimSpace(body.By))
	if by == "" {
		by = "operator"
	}

	current, err := r.phaseStore.Get(req.Context())
	if err != nil {
		if errors.Is(err, phase.ErrNotFound) {
			r.phaseAdaptorUnavailable(w)
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	next, err := phase.Advance(current, body.To, by, body.Reason, time.Now().UTC())
	if err != nil {
		writePhaseError(w, err)
		return
	}
	if err := r.phaseStore.Set(req.Context(), next); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Emit the transition event. The transition just appended to
	// History is the last entry by construction.
	tr := next.History[len(next.History)-1]
	r.publishPhaseTransition(req.Context(), tr, next)

	view := phaseStateView{
		Phase:            next.Phase,
		Since:            next.Since,
		History:          next.History,
		AllPhases:        phase.All(),
		LegalTransitions: phase.LegalTransitionsFrom(next.Phase),
	}
	writeJSON(w, http.StatusOK, view)
}

// publishPhaseTransition fans the event through the Router's hub.
// No-op when the hub is nil (degraded mode / tests that don't
// subscribe).
func (r *Router) publishPhaseTransition(ctx context.Context, tr phase.Transition, state phase.WorkspaceState) {
	if r.eventsHub == nil {
		return
	}
	phase.PublishTransition(ctx, r.eventsHub, tr, state)
}

// writePhaseError maps phase.* sentinel errors to status codes.
//   - ErrInvalidPhase / ErrInvalidTransition → 400 (operator can fix)
//   - ErrSamePhase                            → 409 (already there)
//   - anything else                           → 500
func writePhaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, phase.ErrInvalidPhase):
		httperr.Write(w, http.StatusBadRequest, "invalid_phase", err.Error())
	case errors.Is(err, phase.ErrInvalidTransition):
		httperr.Write(w, http.StatusBadRequest, "invalid_transition", err.Error())
	case errors.Is(err, phase.ErrSamePhase):
		httperr.Write(w, http.StatusConflict, "same_phase", err.Error())
	default:
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
