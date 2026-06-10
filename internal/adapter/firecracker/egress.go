// Package firecracker — runtime egress enforcement entry points
// (gm-o9t8.3.6.2). The platform-specific applier lives in
// egress_linux.go / egress_other.go; this file holds the cross-
// platform translation IR and the helpers tests use to assert the
// shape of the generated nftables ruleset without needing the
// `github.com/google/nftables` library on every host.
//
// The translation IR is the bridge between an []egress.Rule and the
// `nftables` Go-library calls the Linux applier makes. We model each
// statement we know how to emit (verdict, log) and each match clause
// we care about (host IPs, ports, l4-proto). The applier walks the
// IR in priority order and feeds each entry into nftables.AddRule;
// the test path renders the IR to a deterministic JSON form and
// asserts on that — guaranteeing the rule plan stays stable even on
// macOS where nftables itself can't be exercised.
//
// Hostname resolution policy for v1: one-shot at apply time. We
// resolve each rule's HostPattern (or its concrete host literal) via
// the default resolver and stamp the resulting v4 addresses onto the
// IR. Wildcard patterns (`*.example.com`, `**.foo`) are unresolvable
// at apply time without an interception DNS path; for those rules we
// fall back to a host-set entry that records the literal pattern so
// the applier can emit a log-only stub (and the operator gets a
// warning). TTL-aware refresh is the explicit follow-up — tracked in
// the report below.
package firecracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// nftFamily / nftHook / nftChainType mirror the strings the nftables
// library uses on the wire — typing them locally lets the test IR
// stay self-contained.
const (
	nftFamilyInet    = "inet"
	nftHookForward   = "forward"
	nftChainTypeFwd  = "filter"
	nftPriorityFwd   = 0
	nftTableNameFmt  = "gemba_%s"
	defaultResolveTO = 5 * time.Second
)

// Verdict is the action a rule emits when it matches: accept, drop,
// or log+drop. Allow rules become accept; deny rules become drop
// (with an embedded log statement when the operator wants visibility,
// which v1 always does so we get the audit ingest path for free).
type Verdict string

const (
	VerdictAccept Verdict = "accept"
	VerdictDrop   Verdict = "drop"
)

// Statement is one translated rule plan entry — what the Linux
// applier will emit as a single nftables rule. Fields are exported so
// tests can introspect them and so the JSON form is stable across
// builds (no map iteration ordering, no unexported reflection).
type Statement struct {
	// RuleID echoes egress.Rule.ID so operators can map a drop event
	// in the kernel log back to the policy row that produced it.
	RuleID string `json:"rule_id"`

	// Priority is copied from the source rule so the test fixture
	// can assert the IR sort matches Effective() order.
	Priority int `json:"priority"`

	// Proto is "tcp", "udp", or "" for any.
	Proto string `json:"proto,omitempty"`

	// DestIPs is the resolved-at-apply-time IPv4/IPv6 set for the
	// rule's host pattern. Empty when the pattern is a wildcard that
	// can't be eagerly resolved.
	DestIPs []string `json:"dest_ips,omitempty"`

	// HostPattern is preserved verbatim so the Linux applier can
	// stamp it onto the rule comment (and the log prefix when the
	// rule fires a drop event).
	HostPattern string `json:"host_pattern"`

	// PortStart / PortEnd are inclusive; both zero == any port.
	PortStart int `json:"port_start"`
	PortEnd   int `json:"port_end"`

	// Verdict is the terminal verdict the rule applies.
	Verdict Verdict `json:"verdict"`

	// Log toggles a `log prefix "gemba-egress-drop:<rule_id>"`
	// statement before the verdict — set on every deny so kernel
	// ringbuffer / nflog readers can attribute the drop.
	Log bool `json:"log,omitempty"`

	// Note is a free-form annotation used when a rule's pattern
	// can't be expressed as concrete IP literals; the applier emits
	// a degraded log-only rule and warns the operator.
	Note string `json:"note,omitempty"`
}

// Ruleset is the full translated plan for a single VM. It also
// carries the per-VM nftables table name so the applier and the
// cleanup path agree on what to add and remove.
type Ruleset struct {
	TableName  string      `json:"table_name"`
	Family     string      `json:"family"`
	ChainName  string      `json:"chain_name"`
	Hook       string      `json:"hook"`
	Priority   int         `json:"priority"`
	Statements []Statement `json:"statements"`
}

// JSON renders the ruleset to a stable JSON form. Used by tests as
// the canonical assertion surface — diffing JSON is far cheaper than
// reflecting over the nftables library's nested structs.
func (rs Ruleset) JSON() string {
	b, _ := json.MarshalIndent(rs, "", "  ")
	return string(b)
}

// translate converts a priority-sorted slice of egress.Rule into the
// in-memory Ruleset the applier will materialise. The resolver
// argument is injectable so tests can supply a deterministic map
// without touching the real DNS resolver — production callers pass
// defaultResolver.
//
// Translation is stable: statements come out in the same priority
// order the egress store handed us; ties break on rule id so the
// JSON form is byte-stable across runs.
func translate(ctx context.Context, vmID string, rules []egress.Rule, resolve hostResolver) Ruleset {
	rs := Ruleset{
		TableName: fmt.Sprintf(nftTableNameFmt, sanitizeTableName(vmID)),
		Family:    nftFamilyInet,
		ChainName: "forward",
		Hook:      nftHookForward,
		Priority:  nftPriorityFwd,
	}

	sorted := append([]egress.Rule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})

	for _, r := range sorted {
		st := Statement{
			RuleID:      r.ID,
			Priority:    r.Priority,
			HostPattern: r.HostPattern,
			PortStart:   r.PortStart,
			PortEnd:     r.PortEnd,
		}
		if r.Proto != egress.ProtoAny {
			st.Proto = string(r.Proto)
		}
		switch r.Action {
		case egress.ActionAllow:
			st.Verdict = VerdictAccept
		case egress.ActionDeny:
			st.Verdict = VerdictDrop
			st.Log = true
		default:
			st.Verdict = VerdictDrop
			st.Log = true
		}

		// Resolve concrete literal patterns; leave wildcards as a
		// degraded log-only rule that records the operator intent.
		if isResolvableLiteral(r.HostPattern) {
			ips, err := resolve(ctx, r.HostPattern)
			if err == nil && len(ips) > 0 {
				st.DestIPs = ips
			} else if err != nil {
				st.Note = "dns: " + err.Error()
			}
		} else {
			st.Note = "wildcard host pattern; no eager DNS resolution in v1"
		}

		rs.Statements = append(rs.Statements, st)
	}
	return rs
}

// hostResolver is the seam tests use to inject a deterministic
// hostname → []ip map.
type hostResolver func(ctx context.Context, host string) ([]string, error)

// defaultResolver is the production resolver — uses net.DefaultResolver
// with a bounded context so a stuck DNS server can't wedge the VM
// boot path.
func defaultResolver(ctx context.Context, host string) ([]string, error) {
	rctx, cancel := context.WithTimeout(ctx, defaultResolveTO)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(rctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	sort.Strings(out)
	return out, nil
}

// isResolvableLiteral reports whether host has no glob characters
// and therefore can be handed to a stub resolver as-is.
func isResolvableLiteral(host string) bool {
	if host == "" {
		return false
	}
	return !strings.ContainsAny(host, "*")
}

// sanitizeTableName strips characters nftables doesn't accept in
// table identifiers. Conservative: keep [a-zA-Z0-9_], replace
// everything else with '_'. Length is not bounded here — VM ids are
// already short.
func sanitizeTableName(s string) string {
	if s == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// ErrEgressNotEnforced is returned by the fallback applier so callers
// can distinguish "I asked for enforcement but the host has no
// nftables stack" from "I applied successfully." Production wiring
// treats it as a soft warning, not a spawn-abort.
var ErrEgressNotEnforced = errors.New("firecracker: egress enforcement unavailable on this platform")
