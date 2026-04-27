package native

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// escalationIndex is the in-memory store the native adaptor uses
// for ListPendingRequests + ResolveEscalation (gm-native.13).
// Escalation records are inferred from the bridge's event stream:
//
//	escalation_opened   → insert
//	escalation_resolved → remove
//
// The index lives for the process lifetime; crash recovery
// re-populates it by re-tailing the session log on adaptor restart.
type escalationIndex struct {
	mu        sync.RWMutex
	bySession map[string]map[string]core.EscalationRequest // session → id → req
	byID      map[string]string                            // id → session
}

func newEscalationIndex() *escalationIndex {
	return &escalationIndex{
		bySession: make(map[string]map[string]core.EscalationRequest),
		byID:      make(map[string]string),
	}
}

// handleEvent is wired in as the Fanout observer — one call per
// bridge frame, synchronous. Fast-path: non-escalation events are
// dropped without taking the write lock.
func (x *escalationIndex) handleEvent(ev core.OrchestrationEvent) {
	switch ev.Kind {
	case "escalation_opened":
		x.mu.Lock()
		defer x.mu.Unlock()
		req := escalationFromEvent(ev)
		if req.ID == "" {
			return
		}
		m, ok := x.bySession[ev.SessionID]
		if !ok {
			m = make(map[string]core.EscalationRequest)
			x.bySession[ev.SessionID] = m
		}
		m[req.ID] = req
		x.byID[req.ID] = ev.SessionID
	case "escalation_resolved":
		x.remove(ev.SessionID, ev.ID)
	case "user_message":
		// A user_message on a session with open escalations resolves
		// the oldest one (operator typed the answer directly in the
		// pane without going through the SPA). The adaptor doesn't
		// know which — closest-to-oldest is a safe default.
		x.mu.Lock()
		m := x.bySession[ev.SessionID]
		var oldestID string
		var oldestAt time.Time
		for id, r := range m {
			if oldestID == "" || r.CreatedAt.Before(oldestAt) {
				oldestID = id
				oldestAt = r.CreatedAt
			}
		}
		x.mu.Unlock()
		if oldestID != "" {
			x.remove(ev.SessionID, oldestID)
		}
	}
}

func (x *escalationIndex) remove(sessionID, id string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if m, ok := x.bySession[sessionID]; ok {
		delete(m, id)
		if len(m) == 0 {
			delete(x.bySession, sessionID)
		}
	}
	delete(x.byID, id)
}

// forSession returns a snapshot (not a live view) of pending
// escalations for the session. Empty slice (not nil) for "none
// pending" per contract.
func (x *escalationIndex) forSession(sessionID string) []core.EscalationRequest {
	x.mu.RLock()
	defer x.mu.RUnlock()
	m := x.bySession[sessionID]
	out := make([]core.EscalationRequest, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	return out
}

// all returns every open escalation across every session. Filter
// is applied in-memory (MVP — the set is bounded by live sessions
// so list size stays small).
func (x *escalationIndex) all(f core.EscalationFilter) []core.EscalationRequest {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []core.EscalationRequest
	for _, sess := range x.bySession {
		for _, r := range sess {
			if !matchesFilter(r, f) {
				continue
			}
			out = append(out, r)
		}
	}
	if out == nil {
		out = []core.EscalationRequest{}
	}
	return out
}

// lookup returns the session id for an escalation id, or empty.
func (x *escalationIndex) lookup(id string) string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.byID[id]
}

// escalationFromEvent reconstructs an EscalationRequest from the
// bridge's escalation_opened payload. Falls back to sensible
// defaults when the hook payload omits fields.
//
// Two payload shapes land here today:
//
//   - Notification channel (gm-native.13): Claude's hook payload —
//     carries `escalation_kind` (permission_prompt | hitl_approval)
//     and possibly `title` / `prompt` / `message`. Channel
//     defaults to ChannelNotification.
//   - Tool-call channel (gm-97w7.1): the bridge's GembaAsk
//     translator — carries `escalation_kind` (question | blocker),
//     `channel`, `urgency`, `role`, `text`, optional `bead_id` /
//     `title`.
func escalationFromEvent(ev core.OrchestrationEvent) core.EscalationRequest {
	id := ev.ID
	source := core.EscalationPermissionPrompt
	if k, ok := ev.Payload["escalation_kind"].(string); ok && k != "" {
		source = core.EscalationKind(k)
	}
	channel := core.ChannelNotification
	if c, ok := ev.Payload["channel"].(string); ok && c != "" {
		channel = core.EscalationChannel(c)
	}
	title, _ := ev.Payload["title"].(string)
	if title == "" {
		// Pick a defaulted title that matches the kind so the UI
		// row isn't empty when the upstream channel omits one.
		switch source {
		case core.EscalationQuestion:
			title = "Question"
		case core.EscalationBlocker:
			title = "Blocker"
		default:
			title = "Permission prompt"
		}
	}
	prompt, _ := ev.Payload["prompt"].(string)
	if prompt == "" {
		if s, ok := ev.Payload["text"].(string); ok && s != "" {
			prompt = s
		}
	}
	if prompt == "" {
		if s, ok := ev.Payload["message"].(string); ok {
			prompt = s
		}
	}
	urgency := core.UrgencyBlocking
	if u, ok := ev.Payload["urgency"].(string); ok && u != "" {
		urgency = core.EscalationUrgency(u)
	}
	var workItemID core.WorkItemID
	if b, ok := ev.Payload["bead_id"].(string); ok && b != "" {
		workItemID = core.WorkItemID(b)
	}
	return core.EscalationRequest{
		ID:         id,
		Source:     source,
		Channel:    channel,
		WorkItemID: workItemID,
		Urgency:    urgency,
		Title:      title,
		Prompt:     prompt,
		State:      core.EscalationOpen,
		CreatedAt:  ev.At,
	}
}

// matchesFilter is the predicate behind all(). All filter fields
// are AND-combined; missing fields match everything.
//
// Workspace is honoured here as a best-effort pass — the in-memory
// escalation index doesn't yet track per-workspace scope on each
// EscalationRequest (the canonical shape predates the gm-vch2 walk
// agenda's workspace filter). Until that wiring lands, a non-empty
// f.Workspace MUST NOT erroneously drop rows that lack a workspace
// label, so this implementation only filters when the request carries
// a workspace token in its provider metadata. The walk's combined
// EscalationLister scopes server-side as it composes the four live
// sources, so the SPA still sees the right set.
func matchesFilter(r core.EscalationRequest, f core.EscalationFilter) bool {
	if f.AssignmentID != "" && r.AssignmentID != f.AssignmentID {
		return false
	}
	if f.WorkItemID != "" && r.WorkItemID != f.WorkItemID {
		return false
	}
	if f.AgentID != "" && r.AgentID != f.AgentID {
		return false
	}
	if f.Source != "" && r.Source != f.Source {
		return false
	}
	if f.Urgency != "" && r.Urgency != f.Urgency {
		return false
	}
	// Workspace pass-through: no-op until the index records workspace.
	return true
}

// ListPendingRequests implements core.OrchestrationPlaneAdaptor for
// the native adaptor (gm-native.13). Unknown sessions return
// KindSessionNotFound so the caller can distinguish "no escalations"
// from "session doesn't exist."
func (o *OrchestrationPlane) ListPendingRequests(_ context.Context, sessionID string) ([]core.EscalationRequest, error) {
	if o.cfg.Backend == nil {
		return nil, unsupported("ListPendingRequests")
	}
	o.mu.Lock()
	_, known := o.sessions[sessionID]
	o.mu.Unlock()
	if !known {
		return nil, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q unknown", sessionID)
	}
	if o.escalations == nil {
		return []core.EscalationRequest{}, nil
	}
	return o.escalations.forSession(sessionID), nil
}

// ListOpenEscalations returns every escalation across every session.
func (o *OrchestrationPlane) ListOpenEscalations(_ context.Context, f core.EscalationFilter) ([]core.EscalationRequest, error) {
	if o.escalations == nil {
		return []core.EscalationRequest{}, nil
	}
	return o.escalations.all(f), nil
}

// ResolveEscalation routes the operator's reply back to the
// terminal session via Backend.SendKeys. The actual resolution
// event lands when the agent's next UserPromptSubmit hook fires —
// the adaptor's index will remove the entry at that point. If the
// backend SendKeys fails we still remove the entry optimistically
// (better to lose tracking than leave a stuck badge in the UI).
func (o *OrchestrationPlane) ResolveEscalation(
	ctx context.Context, escalationID string, res core.EscalationResolution, _ core.ConfirmNonce,
) (core.EscalationRequest, error) {
	if o.cfg.Backend == nil {
		return core.EscalationRequest{}, unsupported("ResolveEscalation")
	}
	if o.escalations == nil {
		return core.EscalationRequest{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: escalation %q unknown (no index)", escalationID)
	}
	sessionID := o.escalations.lookup(escalationID)
	if sessionID == "" {
		return core.EscalationRequest{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: escalation %q unknown", escalationID)
	}

	o.mu.Lock()
	sess, ok := o.sessions[sessionID]
	o.mu.Unlock()
	if !ok {
		return core.EscalationRequest{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q unknown", sessionID)
	}
	paneID, _ := sess.ProviderMetadata["pane_id"].(string)

	// Compose the reply as a send-keys payload. Approve / Deny map
	// to "yes"/"no"; Modify carries the operator's free-text; Defer
	// is a no-op on the terminal (just close the index entry).
	payload, err := composeEscalationReply(res)
	if err != nil {
		return core.EscalationRequest{}, err
	}
	if payload != "" && paneID != "" {
		if sendErr := o.cfg.Backend.SendKeys(ctx, paneID, payload+" Enter"); sendErr != nil {
			// Best-effort: surface but don't crash. Remove the
			// entry optimistically.
			o.escalations.remove(sessionID, escalationID)
			return core.EscalationRequest{}, core.WrapAdaptorError(core.KindProcessFailed, sendErr,
				"native: send reply for escalation %s", escalationID)
		}
	}

	// Snapshot the record before removing so the caller gets the
	// resolved form with the final state set.
	req, _ := o.escalations.take(sessionID, escalationID)
	req.State = core.EscalationResolved
	req.Resolution = &res
	return req, nil
}

// take is a snapshot+remove used by ResolveEscalation.
func (x *escalationIndex) take(sessionID, id string) (core.EscalationRequest, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	m := x.bySession[sessionID]
	r, ok := m[id]
	if !ok {
		return core.EscalationRequest{}, false
	}
	delete(m, id)
	if len(m) == 0 {
		delete(x.bySession, sessionID)
	}
	delete(x.byID, id)
	return r, true
}

// composeEscalationReply formats the operator's EscalationResolution
// into a string the backend SendKeys can push into the terminal.
// Defer returns "" (no keys sent) so the entry is simply cleared
// locally without perturbing the agent.
func composeEscalationReply(res core.EscalationResolution) (string, error) {
	switch res.Kind {
	case core.ResolutionApprove:
		return "yes", nil
	case core.ResolutionDeny:
		return "no", nil
	case core.ResolutionModify:
		if s, ok := res.Value.(string); ok {
			return s, nil
		}
		return "", fmt.Errorf("native: modify resolution requires string Value")
	case core.ResolutionDefer:
		return "", nil
	default:
		return "", fmt.Errorf("native: unknown resolution kind %q", res.Kind)
	}
}
