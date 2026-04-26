package persona

import (
	"context"
	"errors"
	"fmt"

	"github.com/MikeBengtson/gemba/internal/core"
)

// SpawnFunc is the post-Begin hook the dispatcher fires after a
// successful Begin. The default in-process callers (POST
// /api/consults) install [NativeSpawn] so a successful Begin
// immediately launches a Claude Code session via the bound
// OrchestrationPlaneAdaptor.
//
// Implementations MUST be safe for concurrent use — multiple Begin
// calls can fire in parallel from different operators.
type SpawnFunc func(ctx context.Context, c *Consult) error

// SetSpawnFunc installs the post-Begin spawn callback. nil clears
// the callback (Begin returns the consult without spawning, the
// "dry run" mode the operator picks via spawn=false on POST
// /api/consults).
//
// Concurrency: tests may install / clear during a run; the field
// is read inside Begin's tail under no extra lock since the read
// is a single-pointer load.
func (d *Dispatcher) SetSpawnFunc(f SpawnFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.spawn = f
}

// MaybeSpawn invokes the configured spawn func for c. Returns nil
// when no spawn func is set (dry-run mode) or when the spawn
// succeeded; surfaces the spawn error otherwise. The dispatcher's
// HTTP entrypoint calls this after Begin so a failed spawn flips
// the consult status to Failed via Finish before the response
// returns.
func (d *Dispatcher) MaybeSpawn(ctx context.Context, c *Consult) error {
	d.mu.RLock()
	f := d.spawn
	d.mu.RUnlock()
	if f == nil {
		return nil
	}
	return f(ctx, c)
}

// NativeSpawn returns a SpawnFunc that wraps a bound
// OrchestrationPlaneAdaptor (gm-twp2). It composes a SessionPrompt
// pointing at the consult's working directory with the consult ID
// pinned as the spawned session's id, then calls op.StartSession.
//
// agentType is the agents.toml entry the persona consult should
// dispatch as — typically "claude" for the PM persona since the
// emit_skill_output MCP tool ships with the Claude Code bridge.
// Operators can specialise per-persona by passing different agent
// types; the spawn helper itself is persona-agnostic.
//
// What this slice intentionally DOES NOT do:
//
//   - Inject the composed prompt into the spawned session's
//     CLAUDE.md. The native StartSession path tries to fetch a
//     bead by the same id; for a consult ID it fails silently and
//     the preamble step skips. A follow-up slice (filed) wires
//     the consult's composed prompt through preamble.ApplyToClaudeMD
//     so the spawned agent sees its instructions on first read.
//
//   - Reconcile spawn failures with consult status. A spawn
//     failure today leaves the consult registered with status
//     "running"; the operator sees the registered consult but no
//     session lands in /api/sessions. The follow-up slice that
//     wires the preamble also flips the consult to Failed via
//     Finish on spawn error so the SPA's drawer surfaces the
//     reason.
func NativeSpawn(op core.OrchestrationPlaneAdaptor, agentType string) SpawnFunc {
	return func(ctx context.Context, c *Consult) error {
		if op == nil {
			return errors.New("persona: NativeSpawn called with nil OrchestrationPlaneAdaptor")
		}
		if agentType == "" {
			return errors.New("persona: NativeSpawn called with empty agent type")
		}
		if c == nil {
			return errors.New("persona: NativeSpawn called with nil consult")
		}
		spec := core.SessionPrompt{
			Extension: map[string]any{
				// The consult ID stands in for the bead ID at the
				// adaptor boundary — both are workspace-unique
				// session-correlation handles. The native adaptor
				// uses it for worktree provisioning + pane title;
				// the override below pins the session ID itself
				// so bridge frames carry the right consult key.
				"gemba:bead_id":              c.ID,
				"gemba:agent_type":           agentType,
				"gemba:workspace":            c.WorkingDir,
				"gemba:session_id_override":  c.ID,
				"gemba:title":                fmt.Sprintf("consult: %s · %s", c.PersonaID, c.SkillID),
			},
		}
		_, err := op.StartSession(ctx, c.ID, spec)
		if err != nil {
			return fmt.Errorf("persona: NativeSpawn(%s): %w", c.ID, err)
		}
		return nil
	}
}
