package firecracker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// fakeResolver returns a deterministic map of host → IPs so the
// translation matrix is reproducible across CI runs. Unknown hosts
// surface ErrNotFound which the translator records on the Statement.
type fakeResolver map[string][]string

var errResolveMiss = errors.New("fake: host not in fixture")

func (f fakeResolver) resolve(_ context.Context, host string) ([]string, error) {
	if ips, ok := f[host]; ok {
		return ips, nil
	}
	return nil, errResolveMiss
}

// TestTranslate_Matrix exercises >=4 rules including overlapping
// priorities and a wildcard host. The asserted shape is the JSON form
// of the resulting Ruleset; we don't pin the literal JSON because the
// nftables low-level expressions aren't part of the IR — only the
// high-level Statement fields.
func TestTranslate_Matrix(t *testing.T) {
	resolver := fakeResolver{
		"api.anthropic.com": {"1.2.3.4", "1.2.3.5"},
		"github.com":        {"140.82.112.3"},
		"proxy.golang.org":  {"34.117.59.81"},
	}
	rules := []egress.Rule{
		{ID: "r1", Action: egress.ActionAllow, Proto: egress.ProtoTCP, HostPattern: "api.anthropic.com", PortStart: 443, PortEnd: 443, Priority: 100},
		{ID: "r2", Action: egress.ActionDeny, Proto: egress.ProtoAny, HostPattern: "evil.example.com", PortStart: 0, PortEnd: 0, Priority: 50},
		{ID: "r3", Action: egress.ActionAllow, Proto: egress.ProtoTCP, HostPattern: "github.com", PortStart: 22, PortEnd: 22, Priority: 50},
		{ID: "r4", Action: egress.ActionAllow, Proto: egress.ProtoTCP, HostPattern: "*.anthropic.com", PortStart: 443, PortEnd: 443, Priority: 75},
		{ID: "r5", Action: egress.ActionAllow, Proto: egress.ProtoTCP, HostPattern: "proxy.golang.org", PortStart: 443, PortEnd: 443, Priority: 50},
	}

	rs := translate(context.Background(), "ws-test", rules, resolver.resolve)

	if rs.TableName != "gemba_ws_test" {
		t.Fatalf("table name: got %q want gemba_ws_test", rs.TableName)
	}
	if rs.Family != "inet" {
		t.Fatalf("family: %q", rs.Family)
	}
	if len(rs.Statements) != 5 {
		t.Fatalf("statement count: got %d want 5", len(rs.Statements))
	}

	// Priority-desc order, tie-broken by id ascending.
	wantOrder := []string{"r1", "r4", "r2", "r3", "r5"}
	for i, want := range wantOrder {
		if rs.Statements[i].RuleID != want {
			t.Fatalf("statement %d: got %q want %q (full plan:\n%s)",
				i, rs.Statements[i].RuleID, want, rs.JSON())
		}
	}

	// r1 must carry the resolved fixture IPs.
	if !equalStrSlice(rs.Statements[0].DestIPs, []string{"1.2.3.4", "1.2.3.5"}) {
		t.Fatalf("r1 dest_ips: got %v", rs.Statements[0].DestIPs)
	}

	// r4 is a wildcard; we expect no resolved IPs and a note.
	r4 := findStmt(rs, "r4")
	if r4 == nil || len(r4.DestIPs) != 0 || r4.Note == "" {
		t.Fatalf("r4 should be unresolved wildcard with a note; got %+v", r4)
	}

	// r2 is a deny → must be VerdictDrop + Log=true.
	r2 := findStmt(rs, "r2")
	if r2.Verdict != VerdictDrop || !r2.Log {
		t.Fatalf("r2: expected drop+log; got %+v", r2)
	}

	// r1 is an allow → must be VerdictAccept + Log=false.
	if rs.Statements[0].Verdict != VerdictAccept || rs.Statements[0].Log {
		t.Fatalf("r1: expected accept+nolog; got %+v", rs.Statements[0])
	}

	// JSON form must be non-empty and stable across renders.
	got1 := rs.JSON()
	got2 := rs.JSON()
	if got1 == "" || got1 != got2 {
		t.Fatalf("JSON not stable")
	}
}

// TestTranslate_SanitizesTableName ensures VM ids containing dashes
// (which is exactly what newVMID produces) are sanitised before
// reaching nftables — the kernel rejects '-' in identifier names.
func TestTranslate_SanitizesTableName(t *testing.T) {
	rs := translate(context.Background(), "ws-foo-abcd1234", nil, fakeResolver{}.resolve)
	if got := rs.TableName; got != "gemba_ws_foo_abcd1234" {
		t.Fatalf("table name not sanitised: %q", got)
	}
}

// TestApplyEgressRules_FallbackNoop verifies the cross-platform
// applier returns the translated Ruleset and emits a WARN log without
// touching the kernel. Linux integration tests live in
// egress_linux_integration_test.go and are gated on root + GOOS=linux.
func TestApplyEgressRules_FallbackBehavior(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	rules := []egress.Rule{
		{ID: "r1", Action: egress.ActionAllow, Proto: egress.ProtoTCP, HostPattern: "api.anthropic.com", PortStart: 443, PortEnd: 443, Priority: 100},
	}
	rs, err := applyEgressRules(context.Background(), log, "ws-test-vm", "tap-gm-1", rules)
	if err != nil {
		t.Fatalf("applyEgressRules: %v", err)
	}
	if rs == nil {
		t.Fatalf("applyEgressRules returned nil Ruleset")
	}

	// The fallback (non-Linux) path emits WARN; the Linux path emits
	// INFO. We accept either to keep this test build-agnostic, but
	// the message text differs so we look for the common stem.
	if !strings.Contains(buf.String(), "firecracker/egress") {
		t.Fatalf("expected firecracker/egress log entry, got: %s", buf.String())
	}
}

// TestTeardownEgressRules_FallbackNoop exercises the symmetric
// teardown path. On non-Linux it must return nil unconditionally.
func TestTeardownEgressRules_NoErrorOnMissingTable(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	if err := teardownEgressRules(log, "gemba_does_not_exist"); err != nil {
		// On Linux this can fail if nftables isn't available — accept
		// "open nftables" failures as the path we don't care about
		// for the unit test; the integration test covers the happy
		// path.
		if !strings.Contains(err.Error(), "open nftables") {
			t.Fatalf("teardownEgressRules: unexpected error: %v", err)
		}
	}
}

// --- helpers --------------------------------------------------------

func findStmt(rs Ruleset, id string) *Statement {
	for i := range rs.Statements {
		if rs.Statements[i].RuleID == id {
			return &rs.Statements[i]
		}
	}
	return nil
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
