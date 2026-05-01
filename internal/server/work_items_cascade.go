// Wrapper cascade dispatch (gm-1o9n).
//
// Moving an epic or milestone into progress is an operator command:
// start the runnable leaf work beneath it. The wrapper itself is not
// dispatched as agent work; its visible board state remains derived by
// milestone_autoclose.go from descendant leaf state.

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

const (
	cascadeActiveLabel      = "gemba:cascade-active"
	cascadeAgentLabelPrefix = "gemba:cascade-agent:"
)

type cascadeDispatchRequest struct {
	AgentType string `json:"agent_type"`
	Limit     int    `json:"limit,omitempty"`
}

type cascadeDispatchResponse struct {
	WrapperID  string                 `json:"wrapper_id"`
	Staged     []string               `json:"staged,omitempty"`
	Dispatched []cascadeDispatchedRow `json:"dispatched"`
	Blocked    []string               `json:"blocked,omitempty"`
	Skipped    []string               `json:"skipped,omitempty"`
	Errors     []cascadeErrorRow      `json:"errors,omitempty"`
	Limit      int                    `json:"limit,omitempty"`
}

type cascadeDispatchedRow struct {
	WorkItemID string `json:"work_item_id"`
	SessionID  string `json:"session_id,omitempty"`
}

type cascadeErrorRow struct {
	WorkItemID string `json:"work_item_id"`
	Message    string `json:"message"`
}

// cascadeDispatchWorkItem handles POST /api/work-items/{id}/cascade-dispatch.
// It marks the wrapper as cascade-active, stages backlog descendants into
// the execution scope, then starts currently unblocked runnable leaves below
// it up to the configured limit.
func (r *Router) cascadeDispatchWorkItem(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.WorkPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	if r.host.OrchestrationPlane() == nil {
		httperr.WriteError(w, core.NewAdaptorError(core.KindUnsupported,
			"orchestration plane not configured"))
		return
	}

	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing work item id")
		return
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"malformed work item id: "+err.Error())
		return
	}

	var body cascadeDispatchRequest
	if req.Body != nil && req.ContentLength != 0 {
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	if body.AgentType == "" {
		body.AgentType = "claude"
	}

	wp := r.host.WorkPlane()
	wrapper, err := wp.GetWorkItem(req.Context(), core.WorkItemID(id))
	if err != nil {
		if err == core.ErrNotFound && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "work item %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}
	if !isWrapperKind(wrapper.Kind) {
		httperr.Write(w, http.StatusBadRequest, "not_wrapper",
			"cascade dispatch is only supported for epic and milestone work items")
		return
	}

	wrapper, err = r.markCascadeActive(req.Context(), wp, wrapper, body.AgentType)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	resp := r.dispatchReadyDescendants(req.Context(), wp, r.host.OrchestrationPlane(),
		wrapper, body.AgentType, body.Limit)
	writeJSON(w, http.StatusAccepted, resp)
}

func (r *Router) dispatchReadyDescendants(
	ctx context.Context,
	wp core.WorkPlane,
	op core.OrchestrationPlaneAdaptor,
	wrapper core.WorkItem,
	agentType string,
	limit int,
) cascadeDispatchResponse {
	resp := cascadeDispatchResponse{WrapperID: string(wrapper.ID), Dispatched: []cascadeDispatchedRow{}}
	if wp == nil || op == nil {
		return resp
	}
	if agentType == "" {
		agentType = "claude"
	}
	if limit <= 0 {
		limit = r.cascadeDispatchLimit(agentType)
	}
	resp.Limit = limit

	leaves, err := collectRunnableLeaves(ctx, wp, wrapper, map[core.WorkItemID]bool{})
	if err != nil {
		resp.Errors = append(resp.Errors, cascadeErrorRow{WorkItemID: string(wrapper.ID), Message: err.Error()})
		return resp
	}
	sort.SliceStable(leaves, func(i, j int) bool { return leaves[i].ID < leaves[j].ID })

	for i, leaf := range leaves {
		if leaf.StateCategory != core.StateBacklog {
			continue
		}
		staged := core.StateStaged
		updated, err := wp.UpdateWorkItem(ctx, leaf.ID, core.WorkItemPatch{StateCategory: &staged})
		if err != nil {
			resp.Errors = append(resp.Errors, cascadeErrorRow{WorkItemID: string(leaf.ID), Message: err.Error()})
			continue
		}
		leaves[i] = updated
		resp.Staged = append(resp.Staged, string(leaf.ID))
		r.reconcileWrapperAncestors(ctx, wp, updated)
	}

	for _, leaf := range leaves {
		if !isCascadeDispatchableLeafState(leaf.StateCategory) {
			resp.Skipped = append(resp.Skipped, string(leaf.ID))
			continue
		}
		if leaf.DispatchStatus.Effective() != core.DispatchReady {
			resp.Blocked = append(resp.Blocked, string(leaf.ID))
			continue
		}
		blocked, err := hasOpenBlocker(ctx, wp, leaf)
		if err != nil {
			resp.Errors = append(resp.Errors, cascadeErrorRow{WorkItemID: string(leaf.ID), Message: err.Error()})
			continue
		}
		if blocked {
			resp.Blocked = append(resp.Blocked, string(leaf.ID))
			continue
		}
		if limit > 0 && len(resp.Dispatched) >= limit {
			resp.Skipped = append(resp.Skipped, string(leaf.ID))
			continue
		}

		started := core.StateStarted
		updated, err := wp.UpdateWorkItem(ctx, leaf.ID, core.WorkItemPatch{StateCategory: &started})
		if err != nil {
			resp.Errors = append(resp.Errors, cascadeErrorRow{WorkItemID: string(leaf.ID), Message: err.Error()})
			continue
		}
		r.reconcileWrapperAncestors(ctx, wp, updated)

		sessionID, err := startCascadeLeafSession(ctx, op, string(leaf.ID), agentType)
		if err != nil {
			resp.Errors = append(resp.Errors, cascadeErrorRow{WorkItemID: string(leaf.ID), Message: err.Error()})
			continue
		}
		resp.Dispatched = append(resp.Dispatched, cascadeDispatchedRow{
			WorkItemID: string(leaf.ID),
			SessionID:  sessionID,
		})
	}
	return resp
}

func isCascadeDispatchableLeafState(state core.StateCategory) bool {
	return state == core.StateUnstarted || state == core.StateStaged
}

func collectRunnableLeaves(
	ctx context.Context,
	wp core.WorkPlane,
	wrapper core.WorkItem,
	seen map[core.WorkItemID]bool,
) ([]core.WorkItem, error) {
	if seen[wrapper.ID] {
		return nil, nil
	}
	seen[wrapper.ID] = true

	out := []core.WorkItem{}
	for _, id := range childIDs(wrapper) {
		if id == "" || seen[id] {
			continue
		}
		ch, err := wp.GetWorkItem(ctx, id)
		if err != nil {
			return nil, err
		}
		if isWrapperKind(ch.Kind) {
			nested, err := collectRunnableLeaves(ctx, wp, ch, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		if isAutodispatchRunnableKind(ch.Kind) {
			out = append(out, ch)
		}
	}
	return out, nil
}

func startCascadeLeafSession(
	ctx context.Context,
	op core.OrchestrationPlaneAdaptor,
	beadID string,
	agentType string,
) (string, error) {
	body := startSessionRequest{BeadID: beadID, AgentType: agentType}
	reusePaneID := pickReusePane(op, agentType)
	prompt := buildBeadPrompt(body, newAutodispatchNonce(), reusePaneID)
	sess, err := op.StartSession(ctx, beadID, prompt)
	if err != nil && reusePaneID != "" && isCapRace(err) {
		sess, err = op.StartSession(ctx, beadID, buildBeadPrompt(body, newAutodispatchNonce(), ""))
	}
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (r *Router) cascadeDispatchLimit(agentType string) int {
	if r == nil || r.pools == nil {
		return 1
	}
	r.pools.mu.RLock()
	defer r.pools.mu.RUnlock()
	total := 0
	for _, p := range r.pools.resolved {
		if p.AgentType == agentType {
			total += p.SizeEffective
		}
	}
	if total <= 0 {
		return 1
	}
	return total
}

func (r *Router) markCascadeActive(
	ctx context.Context,
	wp core.WorkPlane,
	wrapper core.WorkItem,
	agentType string,
) (core.WorkItem, error) {
	labels := cascadeLabels(wrapper.Labels, agentType)
	if sameStringSet(labels, wrapper.Labels) {
		return wrapper, nil
	}
	updated, err := wp.UpdateWorkItem(ctx, wrapper.ID, core.WorkItemPatch{Labels: labels})
	if err != nil {
		return core.WorkItem{}, err
	}
	return updated, nil
}

func cascadeLabels(labels []string, agentType string) []string {
	out := make([]string, 0, len(labels)+2)
	seen := map[string]bool{}
	for _, label := range labels {
		if label == "" || strings.HasPrefix(label, cascadeAgentLabelPrefix) {
			continue
		}
		if !seen[label] {
			out = append(out, label)
			seen[label] = true
		}
	}
	if !seen[cascadeActiveLabel] {
		out = append(out, cascadeActiveLabel)
	}
	if agentType == "" {
		agentType = "claude"
	}
	out = append(out, cascadeAgentLabelPrefix+agentType)
	sort.Strings(out)
	return out
}

func clearCascadeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == cascadeActiveLabel || strings.HasPrefix(label, cascadeAgentLabelPrefix) {
			continue
		}
		out = append(out, label)
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func cascadeAgentType(labels []string) string {
	for _, label := range labels {
		if strings.HasPrefix(label, cascadeAgentLabelPrefix) {
			agentType := strings.TrimPrefix(label, cascadeAgentLabelPrefix)
			if agentType != "" {
				return agentType
			}
		}
	}
	return "claude"
}

func isCascadeActive(labels []string) bool {
	for _, label := range labels {
		if label == cascadeActiveLabel {
			return true
		}
	}
	return false
}

func (r *Router) continueActiveCascades(ctx context.Context, wp core.WorkPlane, changed core.WorkItem) {
	if wp == nil || r == nil || r.host == nil || r.host.OrchestrationPlane() == nil {
		return
	}
	parents := collectWrapperAncestors(ctx, wp, changed, map[core.WorkItemID]bool{})
	for _, parent := range parents {
		if !isCascadeActive(parent.Labels) {
			continue
		}
		if parent.StateCategory == core.StateCompleted || parent.StateCategory == core.StateCanceled {
			labels := clearCascadeLabels(parent.Labels)
			if !sameStringSet(labels, parent.Labels) {
				if _, err := wp.UpdateWorkItem(ctx, parent.ID, core.WorkItemPatch{Labels: labels}); err != nil {
					continue
				}
			}
			continue
		}
		if parent.StateCategory != core.StateStarted {
			continue
		}
		agentType := cascadeAgentType(parent.Labels)
		resp := r.dispatchReadyDescendants(ctx, wp, r.host.OrchestrationPlane(), parent, agentType, 0)
		if len(resp.Errors) > 0 {
			// Keep this best-effort path non-fatal: the user's state
			// patch already succeeded and future mutations can retry.
			for _, e := range resp.Errors {
				slog.Warn("cascade dispatch retry failed",
					"wrapper_id", parent.ID, "work_item_id", e.WorkItemID, "err", e.Message)
			}
		}
	}
}

func collectWrapperAncestors(
	ctx context.Context,
	wp core.WorkPlane,
	child core.WorkItem,
	seen map[core.WorkItemID]bool,
) []core.WorkItem {
	out := []core.WorkItem{}
	for _, id := range parentIDs(child) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		parent, err := wp.GetWorkItem(ctx, id)
		if err != nil {
			continue
		}
		if isWrapperKind(parent.Kind) {
			out = append(out, parent)
		}
		out = append(out, collectWrapperAncestors(ctx, wp, parent, seen)...)
	}
	return out
}
