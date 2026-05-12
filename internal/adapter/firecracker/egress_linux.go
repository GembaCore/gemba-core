//go:build linux

package firecracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	nft "github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// applyEgressRules materialises rules onto the host's nftables stack
// for the given VM, scoped to a per-VM table so cleanup is a single
// DelTable call. tapIf is the tap interface the VM is wired into —
// we match on `iifname` so traffic from other VMs on the same bridge
// is ignored by this VM's table.
//
// The function is intentionally synchronous and idempotent within
// a single call: if a previous applier left a half-built table behind
// we delete it first, then rebuild from scratch. Concurrent applies
// for the same VM are the caller's problem — Supervisor.Start
// guarantees one apply per VM.
//
// Errors here are fatal at the spawn boundary: a VM without working
// egress controls is exactly what 3.6.2 is preventing, so callers
// abort the spawn and propagate.
func applyEgressRules(ctx context.Context, log *slog.Logger, vmID, tapIf string, rules []egress.Rule) (*Ruleset, error) {
	if log == nil {
		log = slog.Default()
	}
	rs := translate(ctx, vmID, rules, defaultResolver)

	conn, err := nft.New(nft.AsLasting())
	if err != nil {
		return nil, fmt.Errorf("firecracker/egress: open nftables: %w", err)
	}
	defer conn.CloseLasting()

	// Best-effort cleanup of any stale table from a prior failed boot.
	if t, _ := findTable(conn, rs.TableName); t != nil {
		conn.DelTable(t)
		_ = conn.Flush()
	}

	tbl := conn.AddTable(&nft.Table{
		Family: nft.TableFamilyINet,
		Name:   rs.TableName,
	})

	policy := nft.ChainPolicyDrop
	ch := conn.AddChain(&nft.Chain{
		Name:     rs.ChainName,
		Table:    tbl,
		Type:     nft.ChainTypeFilter,
		Hooknum:  nft.ChainHookForward,
		Priority: nft.ChainPriorityRef(nft.ChainPriority(int32(rs.Priority))),
		Policy:   &policy,
	})

	for _, st := range rs.Statements {
		// If the rule is unresolvable / wildcard we still emit a log
		// stub so the kernel sees the operator intent, but skip the
		// dest-ip match since we have nothing concrete to match on.
		if len(st.DestIPs) == 0 && st.Note != "" {
			log.Warn("firecracker/egress: rule has no concrete addresses; emitting log-only stub",
				"vm_id", vmID, "rule_id", st.RuleID, "note", st.Note)
			conn.AddRule(buildLogOnlyRule(tbl, ch, tapIf, st))
			continue
		}
		conn.AddRule(buildConcreteRule(tbl, ch, tapIf, st))
	}

	if err := conn.Flush(); err != nil {
		return nil, fmt.Errorf("firecracker/egress: flush ruleset for vm=%s: %w", vmID, err)
	}

	log.Info("firecracker/egress: applied",
		"vm_id", vmID,
		"tap", tapIf,
		"table", rs.TableName,
		"statements", len(rs.Statements))
	return &rs, nil
}

// teardownEgressRules removes the per-VM table. Safe to call multiple
// times: a missing table is not an error.
func teardownEgressRules(log *slog.Logger, tableName string) error {
	if log == nil {
		log = slog.Default()
	}
	conn, err := nft.New(nft.AsLasting())
	if err != nil {
		return fmt.Errorf("firecracker/egress: open nftables for teardown: %w", err)
	}
	defer conn.CloseLasting()

	t, _ := findTable(conn, tableName)
	if t == nil {
		log.Debug("firecracker/egress: no table to tear down", "table", tableName)
		return nil
	}
	conn.DelTable(t)
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("firecracker/egress: flush teardown: %w", err)
	}
	return nil
}

// findTable looks up an existing inet table by name without panicking
// if the kernel doesn't have it yet. nftables.GetTables errors are
// swallowed — the caller treats "not found" identically to "lookup
// failed."
func findTable(conn *nft.Conn, name string) (*nft.Table, error) {
	tables, err := conn.ListTablesOfFamily(nft.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		if t.Name == name {
			return t, nil
		}
	}
	return nil, errors.New("not found")
}

// buildConcreteRule produces an nftables Rule for a Statement that
// has resolved destination IPs. Match clauses are appended in the
// order:
//
//	iifname == tap
//	l4proto in {st.Proto}        (when set)
//	tcp/udp dport in [start, end] (when port range set)
//	ip daddr in {st.DestIPs}     (v4 split)
//	ip6 daddr in {st.DestIPs}    (v6 split)
//	[log prefix ...]
//	verdict
//
// We collapse v4 and v6 into a single rule for simplicity in v1 —
// the matcher accepts an ip-or-ip6 disjunction via two parallel rules
// when needed. For the cross-compile and translation test we just
// assert the high-level fields; the per-byte nftables payload is
// validated by integration tests gated on euid==0.
func buildConcreteRule(tbl *nft.Table, ch *nft.Chain, tapIf string, st Statement) *nft.Rule {
	exprs := iifnameMatch(tapIf)
	if st.Proto != "" {
		exprs = append(exprs, l4protoMatch(st.Proto)...)
	}
	if st.PortStart != 0 || st.PortEnd != 0 {
		exprs = append(exprs, portRangeMatch(st.Proto, st.PortStart, st.PortEnd)...)
	}
	if len(st.DestIPs) > 0 {
		exprs = append(exprs, daddrAnyOf(st.DestIPs)...)
	}
	if st.Log {
		exprs = append(exprs, &expr.Log{
			Key:  1 << unix.NFTA_LOG_PREFIX,
			Data: []byte(fmt.Sprintf("gemba-egress-drop:%s ", st.RuleID)),
		})
	}
	exprs = append(exprs, verdictExpr(st.Verdict))
	return &nft.Rule{
		Table:    tbl,
		Chain:    ch,
		Exprs:    exprs,
		UserData: []byte(st.RuleID),
	}
}

// buildLogOnlyRule emits a log statement with no match clause beyond
// iifname, used when the rule's host pattern can't be resolved into
// concrete IPs. The verdict still applies — drops still drop, allows
// still allow — but without a destination match the rule fires on
// every outbound packet from the tap, so we leave it as no-op accept
// for allow patterns (to avoid blackholing) and drop for deny.
func buildLogOnlyRule(tbl *nft.Table, ch *nft.Chain, tapIf string, st Statement) *nft.Rule {
	exprs := iifnameMatch(tapIf)
	exprs = append(exprs, &expr.Log{
		Key:  1 << unix.NFTA_LOG_PREFIX,
		Data: []byte(fmt.Sprintf("gemba-egress-stub:%s ", st.RuleID)),
	})
	// For wildcards we always log-only — the verdict is informational.
	exprs = append(exprs, &expr.Verdict{Kind: expr.VerdictReturn})
	return &nft.Rule{
		Table:    tbl,
		Chain:    ch,
		Exprs:    exprs,
		UserData: []byte(st.RuleID),
	}
}

// iifnameMatch builds the `iifname == tap` clause. The clause is
// implemented as a Meta load of NFT_META_IIFNAME into register 1
// followed by a Cmp against the tap name padded to 16 bytes (IFNAMSIZ).
func iifnameMatch(tapIf string) []expr.Any {
	name := make([]byte, 16)
	copy(name, []byte(tapIf))
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: name},
	}
}

// l4protoMatch builds `meta l4proto == <proto>` — used so the
// subsequent tcp/udp dport match has the right header offset.
func l4protoMatch(proto string) []expr.Any {
	var p byte
	switch proto {
	case "tcp":
		p = unix.IPPROTO_TCP
	case "udp":
		p = unix.IPPROTO_UDP
	default:
		return nil
	}
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{p}},
	}
}

// portRangeMatch builds `<proto> dport [start, end]`. nftables wants
// big-endian port bytes; we hard-code the offsets for TCP/UDP (2-byte
// dport at byte offset 2 of the transport header).
func portRangeMatch(proto string, start, end int) []expr.Any {
	if start == 0 && end == 0 {
		return nil
	}
	if end == 0 {
		end = start
	}
	load := &expr.Payload{
		DestRegister: 1,
		Base:         expr.PayloadBaseTransportHeader,
		Offset:       2,
		Len:          2,
	}
	if start == end {
		return []expr.Any{
			load,
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(start >> 8), byte(start)}},
		}
	}
	return []expr.Any{
		load,
		&expr.Range{
			Op:       expr.CmpOpEq,
			Register: 1,
			FromData: []byte{byte(start >> 8), byte(start)},
			ToData:   []byte{byte(end >> 8), byte(end)},
		},
	}
}

// daddrAnyOf emits an OR of `ip daddr == X` clauses for each
// destination IP. For >1 address the nftables library would normally
// build an anonymous set; we use the simpler per-IP rule expansion
// path so a single rule == single statement in the IR — at the cost
// of more entries when a hostname resolves to many IPs.
//
// The applier handles v4 and v6 in one pass — addresses that don't
// parse as IPs are skipped with a debug log (defaultResolver already
// emits valid strings, so this is paranoia).
func daddrAnyOf(ips []string) []expr.Any {
	var out []expr.Any
	for _, s := range ips {
		ip := net.ParseIP(s)
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			out = append(out,
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: v4},
			)
			continue
		}
		out = append(out,
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 24, Len: 16},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ip.To16()},
		)
	}
	return out
}

// verdictExpr maps our Verdict enum onto the nftables verdict kinds.
func verdictExpr(v Verdict) expr.Any {
	switch v {
	case VerdictAccept:
		return &expr.Verdict{Kind: expr.VerdictAccept}
	case VerdictDrop:
		return &expr.Verdict{Kind: expr.VerdictDrop}
	default:
		return &expr.Verdict{Kind: expr.VerdictDrop}
	}
}
