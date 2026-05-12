//go:build linux

package firecracker

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"testing"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// TestApplyEgressRules_Integration is the only test that actually
// pushes nftables state into the kernel. It is gated on
// `os.Geteuid() == 0 && runtime.GOOS == "linux"` so it skips on
// every developer laptop and CI runner that isn't running tests as
// root in a Linux namespace.
func TestApplyEgressRules_Integration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("nftables integration test requires Linux")
	}
	if os.Geteuid() != 0 {
		t.Skip("nftables integration test requires root (euid==0)")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rules := []egress.Rule{
		{ID: "r1", Action: egress.ActionAllow, Proto: egress.ProtoTCP, HostPattern: "127.0.0.1", PortStart: 22, PortEnd: 22, Priority: 100},
		{ID: "r2", Action: egress.ActionDeny, Proto: egress.ProtoAny, HostPattern: "127.0.0.2", Priority: 50},
	}
	rs, err := applyEgressRules(context.Background(), log, "vm-integration-test", "lo", rules)
	if err != nil {
		t.Fatalf("applyEgressRules: %v", err)
	}
	if rs == nil || rs.TableName == "" {
		t.Fatalf("applyEgressRules returned empty Ruleset")
	}
	// Best-effort teardown so we don't leak state on success.
	if err := teardownEgressRules(log, rs.TableName); err != nil {
		t.Fatalf("teardownEgressRules: %v", err)
	}
}
