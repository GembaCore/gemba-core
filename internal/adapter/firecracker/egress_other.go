//go:build !linux

package firecracker

import (
	"context"
	"log/slog"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// applyEgressRules is the no-op fallback. The non-Linux supervisor is
// a bash subprocess shim that doesn't allocate tap interfaces, so
// there is nothing for nftables to attach to. We still build the
// Ruleset (so dispatch and CLI tooling can inspect the plan) and log
// a WARN so operators don't mistake the silent path for "enforced."
func applyEgressRules(ctx context.Context, log *slog.Logger, vmID, tapIf string, rules []egress.Rule) (*Ruleset, error) {
	if log == nil {
		log = slog.Default()
	}
	rs := translate(ctx, vmID, rules, defaultResolver)
	log.Warn("firecracker/egress: enforcement skipped on non-Linux host",
		"vm_id", vmID,
		"tap", tapIf,
		"rule_count", len(rs.Statements))
	return &rs, nil
}

// teardownEgressRules is the no-op symmetric pair. Returns nil so the
// supervisor's Stop path doesn't surface a spurious error on dev hosts.
func teardownEgressRules(log *slog.Logger, tableName string) error {
	if log == nil {
		log = slog.Default()
	}
	log.Debug("firecracker/egress: nothing to tear down on non-Linux host",
		"table", tableName)
	return nil
}
