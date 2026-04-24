// Package core: see doc.go for the overview.
package core

// DerivedSignals are UI-facing booleans computed from a WorkItem plus
// the open escalations attached to it. They answer the three questions
// the Kanban, backlog, and agents dashboards ask most often:
//
//   - "Which cards can I hand to an agent right now?"  → AgentClaimable
//   - "Which cards are stuck waiting on a human?"      → HumanActionRequired
//   - "Which cards are blocked on a review decision?"  → ReviewPending
//
// The foolery spike (docs/prior-art/foolery.md) demonstrated the value
// of these flat booleans — Foolery's Beat carries `isAgentClaimable`,
// `requiresHumanAction`, and `nextActionState` precomputed, and every
// lane / filter site reads them directly instead of rebuilding the
// predicate from native status plus escalation state. Gemba's
// equivalent lives here.
//
// DerivedSignals are emitted on the wire as WorkItem.Derived. They are
// the output of a pure function: callers MUST always recompute from the
// current item + escalation list rather than persist them across
// reconnects.
type DerivedSignals struct {
	// AgentClaimable — an automated agent can pick this card up now.
	AgentClaimable bool `json:"agent_claimable"`

	// HumanActionRequired — at least one blocking escalation is open
	// against this card; forward progress needs a human.
	HumanActionRequired bool `json:"human_action_required"`

	// ReviewPending — an open escalation is waiting on a human review
	// or approval (HITL approval, permission prompt, MCP elicitation,
	// A2A input-required).
	ReviewPending bool `json:"review_pending"`
}

// Derive computes DerivedSignals from a WorkItem, the open escalations
// attached to it, and the WorkPlane's CapabilityManifest. Pure: no I/O,
// no side effects, deterministic in its inputs.
//
// Rules (locked; any change needs a bead and a truth-table update in
// derived_test.go):
//
//  1. AgentClaimable is true iff ALL of:
//     - item.StateCategory == StateUnstarted,
//     - item.Assignee == nil (no one holds the card),
//     - no provided escalation has State=open AND Urgency=blocking,
//     - manifest.StateMap is non-empty (else the adaptor hasn't
//     declared how to normalise its native statuses — gm-cpk's
//     HasDeclaredState=false — and StateCategory can't be trusted
//     enough to invite agents to pick it up).
//
//  2. HumanActionRequired is true iff any provided escalation has
//     State=open AND Urgency=blocking. The manifest is not consulted:
//     an escalation is real regardless of the adaptor's state-map
//     hygiene.
//
//  3. ReviewPending is true iff any provided escalation has State=open
//     AND Source ∈ { hitl_approval, permission_prompt, mcp_elicitation,
//     a2a_input_required } — the sources whose resolution hands control
//     back to a human reviewer. Urgency is ignored here so advisory
//     approvals still surface as "waiting on review" in the UI.
//
// escalations is expected to contain only the escalations that belong
// to item (callers filter by WorkItemID at the adaptor boundary). Derive
// does not re-filter.
func Derive(item WorkItem, escalations []EscalationRequest, manifest CapabilityManifest) DerivedSignals {
	var hasBlocking, hasReviewSrc bool
	for _, e := range escalations {
		if e.State != EscalationOpen {
			continue
		}
		if e.Urgency == UrgencyBlocking {
			hasBlocking = true
		}
		switch e.Source {
		case EscalationHITLApproval,
			EscalationPermissionPrompt,
			EscalationMCPElicitation,
			EscalationA2AInputRequired,
			EscalationBlocker,
			EscalationQuestion:
			// Blocker / Question kinds (gm-97w7.1) count as review
			// sources regardless of Urgency — an open question with
			// Urgency=Advisory still wants the operator's eye; the
			// "is it blocking?" signal flows through hasBlocking.
			hasReviewSrc = true
		}
	}

	claimable := item.StateCategory == StateUnstarted &&
		item.Assignee == nil &&
		!hasBlocking &&
		len(manifest.StateMap) > 0

	return DerivedSignals{
		AgentClaimable:      claimable,
		HumanActionRequired: hasBlocking,
		ReviewPending:       hasReviewSrc,
	}
}
