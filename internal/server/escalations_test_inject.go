// gm-root.27.22 — POST /api/v1/test/escalations test-mode endpoint.
//
// The headless acceptance harness (gm-root.27) needs to mint a
// blocking escalation deterministically to exercise the triage UI
// path. There is no public escalation-create surface in production
// — escalations are minted internally when sessions emit
// `escalation_opened` events. This endpoint provides a test-only
// shortcut that is registered ONLY when the operator opts in via
// the `GEMBA_ENABLE_TEST_ESCALATIONS=1` environment variable.
//
// Default: not registered. POST returns 404 (the route doesn't
// exist).
//
// When enabled and the bound OrchestrationPlane supports
// InjectSyntheticEscalation, POST writes through to the adapter's
// escalation index (no event-bus round trip). Adapters without a
// synthetic injection hook return 501.

package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// testEscalationsEnabled reports whether the test-mode endpoint
// should be registered. Read once at server startup.
func testEscalationsEnabled() bool {
	return os.Getenv("GEMBA_ENABLE_TEST_ESCALATIONS") == "1"
}

type testEscalationRequest struct {
	Kind    string `json:"kind"`
	Urgency string `json:"urgency"`
	Target  string `json:"target"`
	Summary string `json:"summary"`
	Source  string `json:"source"`
}

type testEscalationResponse struct {
	ID string `json:"id"`
}

type syntheticEscalationInjector interface {
	InjectSyntheticEscalation(core.EscalationRequest) error
}

func (r *Router) postTestEscalation(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no orchestration plane bound")
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no orchestration plane bound")
		return
	}
	injector, ok := op.(syntheticEscalationInjector)
	if !ok {
		httperr.Write(w, http.StatusNotImplemented,
			"unsupported_adaptor",
			"test-escalation injection requires an orchestration plane with synthetic injection support")
		return
	}
	var body testEscalationRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	if body.Target == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"target (workitem id) is required")
		return
	}
	if body.Summary == "" {
		body.Summary = "Synthetic escalation (test-mode)"
	}
	kind := core.EscalationKind(body.Kind)
	if kind == "" {
		kind = core.EscalationHITLApproval
	}
	urgency := core.EscalationUrgency(body.Urgency)
	if urgency == "" {
		urgency = core.UrgencyBlocking
	}
	id := mintEscalationID()
	esc := core.EscalationRequest{
		ID:         id,
		Source:     kind,
		Urgency:    urgency,
		WorkItemID: core.WorkItemID(body.Target),
		Title:      body.Summary,
		Prompt:     body.Summary,
		State:      core.EscalationOpen,
		CreatedAt:  time.Now().UTC(),
	}
	if err := injector.InjectSyntheticEscalation(esc); err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"injection_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, testEscalationResponse{ID: id})
}

// mintEscalationID generates a short random identifier prefixed
// with `synth-` so synthetic escalations are visually distinct in
// logs / SPA panels.
func mintEscalationID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a timestamp-based id; rand.Read failing on a
		// modern OS is essentially impossible, but we never panic.
		return "synth-" + time.Now().UTC().Format("20060102T150405.000")
	}
	return "synth-" + hex.EncodeToString(b)
}
