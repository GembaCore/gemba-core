// POST /api/consults/{id}/apply/{idx} — record an applied line
// (gm-twp2).
//
// The persona dispatcher tracks the operator's confirmed applies on
// the live Consult.AppliedIdx slice and persists the slice to the
// audit log on Finish. This slice records intent only — the
// dispatcher does NOT execute the SuggestedAction inside the
// returned line. A follow-up bead (filed) wires per-skill appliers
// that translate verb+path+body into actual WorkPlane mutations.
// Until then the operator (or their tooling) reads the response's
// `line` field and dispatches the action manually; the audit trail
// proves intent regardless.
//
// Idempotency is doubled up: the X-GEMBA-Confirm middleware drops
// nonce replays at the HTTP boundary; the dispatcher's
// duplicate-idx check inside Apply rejects a same-id second-call
// even when the nonce is fresh (e.g. operator clicks twice in
// different SPA tabs without a nonce-share).

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/go-chi/chi/v5"
)

// applyResponse is the wire shape POST /api/consults/{id}/apply/{idx}
// returns. The line is rendered as the skill's typed shape so the
// SPA's drawer can extract suggested_action without re-fetching the
// full consult detail.
type applyResponse struct {
	ConsultID  string `json:"consult_id"`
	Idx        int    `json:"idx"`
	Line       any    `json:"line"`
	AppliedIdx []int  `json:"applied_idx"`
	// Executed reports whether a registered Applier ran the
	// SuggestedAction (gm-twp2.1). False = record-only mode (no
	// applier registered for the consult's skill); the operator
	// dispatches manually.
	Executed bool `json:"executed"`
	// Executor is the applier's response when Executed=true. Zero
	// value (Detail empty, Body nil) when Executed=false.
	Executor persona.ApplierResult `json:"executor"`
}

func (r *Router) applyConsult(w http.ResponseWriter, req *http.Request) {
	if r.personaDispatcher == nil {
		http.Error(w, "persona dispatcher not configured", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(req, "id")
	if id == "" {
		http.Error(w, "consult id required", http.StatusBadRequest)
		return
	}
	idxRaw := chi.URLParam(req, "idx")
	idx, err := strconv.Atoi(idxRaw)
	if err != nil {
		http.Error(w, "idx must be an integer: "+err.Error(), http.StatusBadRequest)
		return
	}

	res, err := r.personaDispatcher.Apply(req.Context(), id, idx)
	if err != nil {
		http.Error(w, applyErrorMessage(err), applyErrorStatus(err))
		return
	}

	writeJSON(w, http.StatusOK, applyResponse{
		ConsultID:  id,
		Idx:        idx,
		Line:       res.Line,
		AppliedIdx: res.AppliedIdx,
		Executed:   res.Executed,
		Executor:   res.Executor,
	})
}

// applyErrorMessage strips the "persona/dispatcher: " prefix from
// dispatcher errors so the HTTP response surface stays operator-
// facing instead of leaking package names.
func applyErrorMessage(err error) string {
	msg := err.Error()
	const prefix = "persona/dispatcher: "
	if len(msg) >= len(prefix) && msg[:len(prefix)] == prefix {
		return msg[len(prefix):]
	}
	return msg
}

// applyErrorStatus picks the right HTTP status for an Apply error.
// Apply has three known failure modes; everything else falls
// through to 400.
func applyErrorStatus(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unknown consult_id"):
		// The id may have been Finished and aged out of the live
		// registry. Operators can re-fetch /api/consults/{id} (which
		// falls through to the audit log) to confirm — Apply itself
		// only operates on live consults, so 404 is the right shape
		// for the SPA's drawer to recognise "this consult is no
		// longer applyable".
		return http.StatusNotFound
	case strings.Contains(msg, "out of range"):
		return http.StatusBadRequest
	case strings.Contains(msg, "already recorded"):
		// Idempotency: same idx applied twice. The SPA treats this
		// as "already done" — the apply state is already recorded.
		return http.StatusConflict
	case strings.Contains(msg, "applier for skill"),
		strings.Contains(msg, "finished during applier"):
		// gm-twp2.1: an applier was registered and the executor
		// failed (e.g. WorkPlane mutation rejected, network error).
		// 502 surfaces "the upstream we delegated to refused" and
		// signals the SPA to enable a Retry button — AppliedIdx
		// stays unrolled so the retry path doesn't 409.
		return http.StatusBadGateway
	default:
		return http.StatusBadRequest
	}
}

// _ keeps the persona import alive for builds that rebuild this
// file without others touching persona — the type alias below
// documents the dispatcher dependency at file scope.
var _ = (*persona.Dispatcher)(nil)
