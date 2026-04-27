// Gas Town escalation mapping (gm-e7.5).
//
// `gt escalate` writes per-escalation beads tagged with
// labels[]="gt:escalation". `gt escalate list --json` returns the
// open set in a stable shape; this file maps each entry onto a
// canonical core.EscalationRequest (Source: orchestrator_pause).
//
// v1 the implementation polls (manifest declares EventDeliveryPoll
// already). A future stream surface (gt escalate watch --json) can
// drop in here without changing ListOpenEscalations.

package gt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// gtEscalation is the row shape `gt escalate list --json` emits.
// Mapped onto core.EscalationRequest by toEscalationRequest below.
type gtEscalation struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    int       `json:"priority"`
	IssueType   string    `json:"issue_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
	Assignee    string    `json:"assignee"`
	Labels      []string  `json:"labels"`
}

// listEscalationsRaw shells out to `gt escalate list --json` and
// parses the response. Test seam — the OrchestrationPlane.run
// hook can be stubbed without spawning a process.
func (o *OrchestrationPlane) listEscalationsRaw(ctx context.Context) ([]gtEscalation, error) {
	out, err := o.run(ctx, "escalate", "list", "--json")
	if err != nil {
		return nil, wrapGtError(err, "gt escalate list --json")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var rows []gtEscalation
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("gastown: decode escalate list: %w", err)
	}
	return rows, nil
}

// ListOpenEscalations returns the open escalation set as
// canonical core.EscalationRequest values. The filter narrows
// the output:
//
//   - filter.Source non-empty → keep rows whose mapped Source
//     matches.
//   - filter.Urgency non-empty → keep rows whose mapped Urgency
//     matches.
//   - filter.AgentID non-empty → keep rows whose CreatedBy or
//     Assignee equals the agent id.
//   - filter.Workspace, filter.WorkItemID, filter.AssignmentID:
//     gt's escalation surface is town-scoped and doesn't yet
//     carry workspace / work-item / assignment provenance, so
//     these filters are a no-op today (documented; gm-e7.7
//     conformance will not assert on them for this adaptor).
func (o *OrchestrationPlane) ListOpenEscalations(ctx context.Context, filter core.EscalationFilter) ([]core.EscalationRequest, error) {
	rows, err := o.listEscalationsRaw(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.EscalationRequest, 0, len(rows))
	for _, r := range rows {
		if r.Status != "" && r.Status != "open" {
			continue
		}
		req := toEscalationRequest(r)
		if !escalationMatchesFilter(req, r, filter) {
			continue
		}
		out = append(out, req)
	}
	return out, nil
}

// toEscalationRequest projects one gt-side escalation row onto
// the canonical core shape. Source is fixed at
// EscalationOrchestratorPause — every gt-side escalation is, by
// design, the orchestrator (a polecat / crew session / mayor)
// pausing autonomously and asking the operator. The single source
// keeps the SPA's escalation badge taxonomy consistent.
func toEscalationRequest(r gtEscalation) core.EscalationRequest {
	severity := severityFromLabels(r.Labels)
	urgency := core.UrgencyAdvisory
	if severity == "critical" || severity == "high" {
		urgency = core.UrgencyBlocking
	}
	return core.EscalationRequest{
		ID:        r.ID,
		Source:    core.EscalationOrchestratorPause,
		AgentID:   core.AgentID(strings.TrimSpace(r.CreatedBy)),
		Urgency:   urgency,
		Title:     r.Title,
		Prompt:    r.Description,
		State:     core.EscalationOpen,
		CreatedAt: r.CreatedAt,
	}
}

// severityFromLabels reads severity:<level> off the labels[]
// slice. Returns "" when no severity label is present (the
// caller treats unknown as advisory).
func severityFromLabels(labels []string) string {
	const prefix = "severity:"
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}

// escalationMatchesFilter applies the EscalationFilter fields
// the gt adaptor can honour. See ListOpenEscalations doc for
// the no-op fields (Workspace, WorkItemID, AssignmentID).
func escalationMatchesFilter(req core.EscalationRequest, raw gtEscalation, f core.EscalationFilter) bool {
	if f.Source != "" && f.Source != req.Source {
		return false
	}
	if f.Urgency != "" && f.Urgency != req.Urgency {
		return false
	}
	if f.AgentID != "" {
		if req.AgentID != f.AgentID && core.AgentID(strings.TrimSpace(raw.Assignee)) != f.AgentID {
			return false
		}
	}
	return true
}
