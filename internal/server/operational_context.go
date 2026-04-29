// /api/operational-context handler (gm-s47n.5.4).
//
// One read returns the join the planner / coach UI / operators all
// consume: AgentRef + Session + Workspace + Assignment + Profile +
// Health. Wraps planner.ReadOperationalContext with adapters that
// satisfy its narrow lookup interfaces from the bound
// OrchestrationPlane.
//
// Graceful degradation per spec §4 Layer 1: missing readers (no
// Assignment lookup today, no Profile until gm-s47n.2.4 lands)
// leave the corresponding pointer nil rather than erroring. The
// SPA reads what's there and renders a degraded card when fields
// are absent.

package server

import (
	"context"
	"net/http"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// orchSessionLookup adapts core.OrchestrationPlaneAdaptor to the
// planner.SessionLookup interface. There is no FindSession on the
// orchestration plane today; ListSessions filtered by id is the
// nearest equivalent and the planner only needs the filtered
// session anyway.
type orchSessionLookup struct {
	op core.OrchestrationPlaneAdaptor
}

func (s orchSessionLookup) FindSession(ctx context.Context, sessionID string) (*core.Session, error) {
	if s.op == nil {
		return nil, nil
	}
	all, err := s.op.ListSessions(ctx, core.SessionFilter{IncludeTerminal: true})
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == sessionID {
			return &all[i], nil
		}
	}
	return nil, nil
}

// orchAgentLookup adapts ReadAgent.
type orchAgentLookup struct {
	op core.OrchestrationPlaneAdaptor
}

func (a orchAgentLookup) ReadAgent(ctx context.Context, id core.AgentID) (*core.AgentRef, error) {
	if a.op == nil {
		return nil, nil
	}
	return a.op.ReadAgent(ctx, id)
}

// orchWorkspaceLookup adapts InspectWorkspace.
type orchWorkspaceLookup struct {
	op core.OrchestrationPlaneAdaptor
}

func (w orchWorkspaceLookup) InspectWorkspace(ctx context.Context, workspaceID string) (core.Workspace, error) {
	if w.op == nil {
		return core.Workspace{}, nil
	}
	return w.op.InspectWorkspace(ctx, workspaceID)
}

// operationalContext handles GET /api/operational-context?session_id=ID.
//
// Returns:
//   - 200 with OperationalContext JSON when the session exists
//     (Assignment / Profile may be nil per the graceful-degradation
//     contract).
//   - 400 when session_id is missing.
//   - 404 when no session with that id exists.
//   - 503 (adaptor_not_configured) when no OrchestrationPlane is bound.
//
// AssignmentLookup + ProfileLookup are intentionally not wired in
// this v1 — there is no FindAssignment on core.OrchestrationPlane
// today (gm-s47n.5.4 follow-up will land it) and the SessionProfile
// dolt store needs gm-s47n.2.4. Callers see Assignment / Profile /
// non-pressure Health fields populate as those beads land; the
// endpoint contract stays stable.
func (r *Router) operationalContext(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("session_id")
	if sessionID == "" {
		httperr.Write(w, http.StatusBadRequest, "validation",
			"session_id query parameter is required")
		return
	}
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "orchestration plane not configured")
		return
	}
	op := r.host.OrchestrationPlane()

	readers := planner.OperationalContextReaders{
		Sessions:   orchSessionLookup{op: op},
		Agents:     orchAgentLookup{op: op},
		Workspaces: orchWorkspaceLookup{op: op},
		// Assignments / Profiles / BeadConcepts intentionally nil —
		// adaptor surfaces don't expose them yet. See file header.
	}
	ctx, err := planner.ReadOperationalContext(req.Context(), sessionID, readers)
	if err != nil {
		// ErrSessionNotFound is the only "expected" error from the
		// read API; anything else is a transport / adaptor fault
		// the operator needs to see.
		if err == planner.ErrSessionNotFound {
			httperr.Write(w, http.StatusNotFound, "session_not_found",
				"session "+sessionID+" not found")
			return
		}
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}
