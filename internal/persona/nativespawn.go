package persona

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/claudemd"
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
// Preamble injection: NativeSpawn writes the consult's composed
// prompt to <WorkingDir>/CLAUDE.md via preamble.ApplyToClaudeMD
// BEFORE calling StartSession. The native StartSession's own
// preamble step does a bead lookup that returns no-such-bead for a
// consult ID and skips silently; our pre-write is what the spawned
// Claude Code session reads on boot. EndSession's preamble cleanup
// strips the same sentinel block so the operator's hand-authored
// CLAUDE.md returns to its pre-spawn state.
//
// Spawn failure is the caller's problem: createConsult inspects
// the returned error and calls Dispatcher.Finish with a Failed
// status so the consult's audit-log row carries the spawn error.
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

		// Best-effort preamble: write the composed prompt to
		// CLAUDE.md so the spawned session reads its instructions
		// on first turn. A write failure is logged via the error
		// chain but does NOT block the spawn — the operator can
		// always paste from /api/consults/:id manually.
		if c.WorkingDir != "" {
			text := composedToText(c.Composed)
			if err := claudemd.Apply(c.WorkingDir, text); err != nil {
				return fmt.Errorf("persona: NativeSpawn preamble write to %s: %w", c.WorkingDir, err)
			}
		}

		spec := core.SessionPrompt{
			Extension: map[string]any{
				// The consult ID stands in for the bead ID at the
				// adaptor boundary — both are workspace-unique
				// session-correlation handles. The native adaptor
				// uses it for worktree provisioning + pane title;
				// the override below pins the session ID itself
				// so bridge frames carry the right consult key.
				"gemba:bead_id":             c.ID,
				"gemba:agent_type":          agentType,
				"gemba:workspace":           c.WorkingDir,
				"gemba:session_id_override": c.ID,
				"gemba:title":               fmt.Sprintf("consult: %s · %s", c.PersonaID, c.SkillID),
			},
		}
		_, err := op.StartSession(ctx, c.ID, spec)
		if err != nil {
			// Best-effort cleanup: strip the CLAUDE.md sentinel
			// block we wrote above so the operator's hand-authored
			// content isn't left with a half-attached preamble.
			// Failure here is logged via the original spawn error;
			// remove failures don't override the load-bearing
			// StartSession error.
			if c.WorkingDir != "" {
				_ = claudemd.Remove(c.WorkingDir)
			}
			return fmt.Errorf("persona: NativeSpawn(%s): %w", c.ID, err)
		}
		return nil
	}
}

// composedToText renders a persona.Composed (System + User) into
// the single Text slot prompt.Composed expects. The two slots are
// joined with a blank line so the model reads them as system-then-
// user without the terminal block markers — preamble.ApplyToClaudeMD
// brackets the whole thing in its own sentinel pair.
func composedToText(c Composed) string {
	switch {
	case c.System == "" && c.User == "":
		return ""
	case c.System == "":
		return c.User
	case c.User == "":
		return c.System
	}
	return strings.Join([]string{c.System, c.User}, "\n\n")
}
