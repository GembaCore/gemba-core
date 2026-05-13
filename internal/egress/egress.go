// Package egress is the per-workspace egress-policy storage layer
// (gm-o9t8.3.6.1). Runtime enforcement (nftables / Firecracker rate
// limiter) lives in gm-o9t8.3.6.2 and is intentionally out of scope
// here — this story owns the data model, the in-memory + Dolt-backed
// stores, the host-pattern matcher, and the HTTP surface that lets
// operators read/write rules.
//
// Conceptual model:
//
//   - Each workspace has zero or more Rules.
//   - The package ships a hard-coded baseline (Defaults) — an allow-list
//     of well-known egress endpoints (Anthropic, OpenAI, GitHub, the
//     common package mirrors, Go module proxy) plus a default-deny
//     terminal rule. Defaults are NEVER stored in the table and NEVER
//     deletable; they participate in Effective() exclusively via merge.
//   - Operators add per-workspace Rules to allow additional egress or to
//     pre-empt a default with a higher-priority allow/deny.
//   - Effective(wsid) returns the sorted join (workspace rules ∪
//     defaults) ordered by priority descending; the runtime enforcer
//     (3.6.2) walks the list and returns the first match.
package egress

import "time"

// Action is the verb of a rule: allow or deny.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

// Proto is the layer-4 protocol scope of a rule. "any" matches both
// tcp and udp; this is the value the default-deny terminal rule
// carries.
type Proto string

const (
	ProtoTCP Proto = "tcp"
	ProtoUDP Proto = "udp"
	ProtoAny Proto = "any"
)

// Rule is one egress decision record. The combination
// (Action, Proto, HostPattern, PortStart, PortEnd, Priority) defines
// the match semantics; ID + WorkspaceID + CreatedAt + CreatedBy are
// the bookkeeping fields the storage layer owns.
//
// PortStart..PortEnd is inclusive on both ends. A single-port rule
// uses PortStart==PortEnd. Port 0 on both ends means "any port".
type Rule struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Action      Action    `json:"action"`
	Proto       Proto     `json:"proto"`
	HostPattern string    `json:"host_pattern"`
	PortStart   int       `json:"port_start"`
	PortEnd     int       `json:"port_end"`
	Priority    int       `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by,omitempty"`
}

// IsDefault returns true when r is one of the hard-coded baseline
// rules. Default rules have an empty WorkspaceID — they apply to
// every workspace and are never stored in the table.
func (r Rule) IsDefault() bool { return r.WorkspaceID == "" }
