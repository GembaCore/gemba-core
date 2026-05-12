package audit

// Event is a string alias for the audit "event name" — a finer-grained
// label than Kind that handlers stamp onto records so log readers can
// filter by surface (egress, vault, …) without parsing the payload.
//
// Records that carry an Event embed it inside their JSON payload under
// "event". Kind remains the coarse category required by Record.
type Event string

// Egress rule audit events (gm-o9t8.3.6.1). Emitted by the
// /api/v1/workspaces/{wsid}/egress-rules POST/DELETE handlers so
// operators can reconstruct who added or removed a network-policy
// rule. Runtime enforcement traffic events (allow/deny decisions)
// land in gm-o9t8.3.6.2 — those are intentionally NOT modeled here.
const (
	EventEgressRuleCreate Event = "egress.rule.create"
	EventEgressRuleDelete Event = "egress.rule.delete"

	// Auth token management audit events (gm-o9t8.3.5.5). Emitted by
	// the /api/v1/auth/tokens DELETE + .../rotate POST handlers when
	// a user revokes or rotates a bearer they own. Tied to a tenant id
	// + token id; no plaintext or hash material appears in the payload.
	EventAuthTokenRevoke Event = "auth.token.revoke"
	EventAuthTokenRotate Event = "auth.token.rotate"
)
