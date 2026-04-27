package native

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// Correlator consumes OrchestrationEvents (gm-native.14) and attaches
// Evidence to the bead identified by any `bd update|close|create|show`
// Bash invocation the bridge captured. The correlation lets the bead
// drawer show "worked by session <id> at <time>" without the session
// writing evidence explicitly — the action of running `bd` is the
// implicit evidence.
//
// Deduping: one session+bead pair produces at most one evidence
// record per dedupeWindow (10s). Prevents a single rapid sequence
// of `bd show gm-foo; bd update gm-foo --status in_progress; bd
// close gm-foo` from stamping three records on the same bead.
//
// Failure policy: AttachEvidence errors are logged, never fatal —
// correlation is best-effort observability, not a mutation path the
// operator waits on.
type Correlator struct {
	wp           core.WorkPlane
	dedupeWindow time.Duration

	mu   sync.Mutex
	seen map[string]time.Time // key = "<session_id>|<bead_id>"
}

// NewCorrelator wraps the WorkPlaneAdaptor the adaptor uses for
// attach-evidence calls. WorkPlane may be nil; in that mode the
// correlator is a no-op (useful for the zero-value native adaptor
// tests).
func NewCorrelator(wp core.WorkPlane) *Correlator {
	return &Correlator{
		wp:           wp,
		dedupeWindow: 10 * time.Second,
		seen:         make(map[string]time.Time),
	}
}

// Consume processes the event stream until ctx is canceled. Events
// that don't match the bd-Bash shape are ignored silently.
func (c *Correlator) Consume(ctx context.Context, events <-chan core.OrchestrationEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			c.handle(ctx, ev)
		}
	}
}

// Handle processes a single event. Exported so tests can drive it
// without owning a channel.
func (c *Correlator) Handle(ctx context.Context, ev core.OrchestrationEvent) {
	c.handle(ctx, ev)
}

func (c *Correlator) handle(ctx context.Context, ev core.OrchestrationEvent) {
	if c.wp == nil {
		return
	}
	if ev.Kind != "tool_use" {
		return
	}
	cmd := extractBashCommand(ev.Payload)
	if cmd == "" {
		return
	}
	sub, target, ok := parseBdInvocation(cmd)
	if !ok {
		return
	}
	key := ev.SessionID + "|" + string(target)
	c.mu.Lock()
	last, seen := c.seen[key]
	if seen && time.Since(last) < c.dedupeWindow {
		c.mu.Unlock()
		return
	}
	c.seen[key] = time.Now()
	c.mu.Unlock()

	summary := fmt.Sprintf("session %s ran `bd %s`", ev.SessionID, sub)
	evidence := core.Evidence{
		Kind:       core.EvidenceLog,
		Source:     "native:session:" + ev.SessionID,
		Ref:        ev.ID,
		Summary:    summary,
		CapturedAt: ev.At,
		Payload: map[string]any{
			"session_id": ev.SessionID,
			"bd_command": cmd,
			"subcommand": sub,
		},
	}
	if err := c.wp.AttachEvidence(ctx, target, evidence); err != nil {
		slog.Warn("native/correlate: AttachEvidence failed",
			"bead", target, "session", ev.SessionID, "err", err)
	}
}

// extractBashCommand digs the Bash command out of a Claude Code
// PreToolUse payload. Tolerant of shape variation — payloads can
// carry the tool under tool_name, tool, or name; args under args,
// arguments, or input.
func extractBashCommand(payload map[string]any) string {
	toolName := firstString(payload, "tool_name", "tool", "name")
	if !strings.EqualFold(toolName, "Bash") {
		return ""
	}
	// args may be either a map or a JSON-encoded string (different
	// Claude versions vary). Handle both.
	for _, k := range []string{"args", "arguments", "input"} {
		if v, ok := payload[k]; ok {
			switch a := v.(type) {
			case map[string]any:
				if cmd, ok := a["command"].(string); ok {
					return cmd
				}
			case string:
				// Try to re-parse as JSON.
				var nested map[string]any
				if err := json.Unmarshal([]byte(a), &nested); err == nil {
					if cmd, ok := nested["command"].(string); ok {
						return cmd
					}
				}
			}
		}
	}
	return ""
}

// parseBdInvocation recognizes a shell command that invokes the bd
// CLI and extracts (subcommand, target bead id). Tolerates leading
// env vars (FOO=bar bd close gm-x) and argument re-ordering (bd
// --json close gm-x).
func parseBdInvocation(cmd string) (sub string, target core.WorkItemID, ok bool) {
	fields := strings.Fields(cmd)
	// Skip leading "K=V" env var assignments.
	i := 0
	for i < len(fields) && strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "--") {
		i++
	}
	if i >= len(fields) || fields[i] != "bd" {
		return "", "", false
	}
	i++
	// Next non-flag word is the subcommand.
	for i < len(fields) && strings.HasPrefix(fields[i], "-") {
		i++
	}
	if i >= len(fields) {
		return "", "", false
	}
	sub = fields[i]
	if !isMutatingBdSubcommand(sub) && !isObservingBdSubcommand(sub) {
		return "", "", false
	}
	i++
	// First non-flag positional after the subcommand is the target.
	for i < len(fields) {
		if !strings.HasPrefix(fields[i], "-") {
			return sub, core.WorkItemID(fields[i]), true
		}
		i++
	}
	return "", "", false
}

// isMutatingBdSubcommand identifies bd verbs that change bead state.
// Correlation for these is the motivating use case.
func isMutatingBdSubcommand(s string) bool {
	switch s {
	case "update", "close", "create", "claim", "comment", "add-label", "remove-label":
		return true
	}
	return false
}

// isObservingBdSubcommand covers `bd show` / `bd ready` which is
// useful signal even if non-mutating — the session read this bead
// at this time, which the drawer's activity view will care about.
// `bd list` is excluded because it matches multiple beads.
func isObservingBdSubcommand(s string) bool {
	switch s {
	case "show", "ready":
		return true
	}
	return false
}

// firstString returns the first map key whose value is a non-empty
// string. Used when Claude's hook payload uses variant key names.
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
