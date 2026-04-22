package bd

import (
	"strings"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Beads has a single scalar `assignee` field and a flat label bag. The
// AgentRef federation (gm-e6.3, DD-1) projects both onto the richer
// core.AgentRef shape by reading role + parent_id out of convention-
// named labels. Writing back works the same way in reverse: labels are
// authoritative for the federated fields, `--assignee` is authoritative
// for the id.
const (
	// labelAgentRolePrefix tags the current assignee's role — "polecat",
	// "witness", "crew", etc. A bead may carry at most one agent:role:*
	// label; callers that need history must read work history, not the
	// current label set.
	labelAgentRolePrefix = "agent:role:"

	// labelAgentParentPrefix tags the current assignee's parent agent
	// id. Used by hierarchical orchestrators (Gas Town Mayor → Polecat)
	// so the UI can render the parent→child link without an adaptor-
	// specific probe.
	labelAgentParentPrefix = "agent:parent:"
)

// agentRefFromBead synthesizes an AgentRef from a Beads assignee string
// plus the bead's label bag. Returns nil when the bead has no assignee
// — a no-assignee bead has no agent to describe.
//
// Role and ParentID come from labels (`agent:role:*`, `agent:parent:*`);
// both are optional and absent labels leave the fields zero. Kind is
// always AgentKindAgent here: Beads's assignee slot is used exclusively
// by automated actors in the gemba ecosystem (humans live in the owner
// field, which is projected separately in types.go). If a future Beads
// convention lets humans claim beads, toggling Kind on an
// `agent:kind:human` label is the extension point.
func agentRefFromBead(assignee string, labels []string) *core.AgentRef {
	if assignee == "" {
		return nil
	}
	ref := &core.AgentRef{
		ID:   core.AgentID(assignee),
		Name: assignee,
		Kind: core.AgentKindAgent,
	}
	for _, l := range labels {
		switch {
		case strings.HasPrefix(l, labelAgentRolePrefix):
			ref.Role = strings.TrimPrefix(l, labelAgentRolePrefix)
		case strings.HasPrefix(l, labelAgentParentPrefix):
			pid := core.AgentID(strings.TrimPrefix(l, labelAgentParentPrefix))
			if pid != "" {
				ref.ParentID = &pid
			}
		}
	}
	return ref
}

// agentLabels returns the labels that encode ref's federated fields
// (role, parent_id). Empty slice when ref is nil or carries no federated
// metadata — the caller should not emit --add-label in that case.
func agentLabels(ref *core.AgentRef) []string {
	if ref == nil {
		return nil
	}
	var out []string
	if ref.Role != "" {
		out = append(out, labelAgentRolePrefix+ref.Role)
	}
	if ref.ParentID != nil && *ref.ParentID != "" {
		out = append(out, labelAgentParentPrefix+string(*ref.ParentID))
	}
	return out
}

// stripAgentLabels returns in with every agent:role:* / agent:parent:*
// entry removed. Used on the write path so a patch carrying a new
// AgentRef authoritatively replaces stale federated labels rather than
// stacking on top of them. Caller-supplied non-agent labels pass through
// unchanged.
func stripAgentLabels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, l := range in {
		if strings.HasPrefix(l, labelAgentRolePrefix) ||
			strings.HasPrefix(l, labelAgentParentPrefix) {
			continue
		}
		out = append(out, l)
	}
	return out
}
