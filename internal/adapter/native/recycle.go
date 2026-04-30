package native

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/adapter/native/preamble"
	"github.com/GembaCore/gemba-core/internal/milestones"
)

// RecycleSession implements the §5.2 recycle protocol from
// docs/design/session-pool.md. Same pool slot, fresh context: keep
// the pane and worktree, mint a new session id, reset profile.
//
// Sequence:
//
//  1. Validate session is Status=Ready. Mid-bead recycle is a contract
//     violation — reject with KindValidation.
//  2. Verify worktree is clean. Dirty → REFUSE recycle, log a warning,
//     convert to EndSession(SessionEndCompleted) on the slot. Spec
//     §5.2 step 2 is explicit: never destructively reset.
//  3. Send the recycle keystroke (`/clear` for claude, etc.).
//  4. Resync clean worktree to base: checkout base + ff-only pull.
//  5. Mint new session id; remap paneSessions; reset metadata.
//     Status returns to SessionInitializing.
//  6. Re-deliver the boot preamble.
//  7. Emit `session.recycled` OrchestrationEvent for the retro
//     pipeline.
//
// On success returns the *new* core.Session (post-recycle id, status
// Initializing). The pane and worktree paths are stable; the logical
// session id changes.
func (o *OrchestrationPlane) RecycleSession(ctx context.Context, sessionID string) (core.Session, error) {
	if o.cfg.Backend == nil {
		return core.Session{}, unsupported("RecycleSession")
	}
	if sessionID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: RecycleSession requires session id")
	}

	o.mu.Lock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		o.mu.Unlock()
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q unknown", sessionID)
	}
	if sess.Status != core.SessionReady {
		curr := sess.Status
		o.mu.Unlock()
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: recycle requires Status=Ready, got %q for %q", curr, sessionID)
	}
	paneID, _ := sess.ProviderMetadata["pane_id"].(string)
	workspace, _ := sess.ProviderMetadata["worktree"].(string)
	agentType, _ := sess.ProviderMetadata["agent_type"].(string)
	beadID, _ := sess.ProviderMetadata["bead_id"].(string)
	personaID, _ := sess.ProviderMetadata["persona_id"].(string)
	priorTitle, _ := sess.ProviderMetadata["pane_title"].(string)
	priorCwd, _ := sess.ProviderMetadata["pane_cwd"].(string)
	priorPanePid := sess.ProviderMetadata["pane_pid"]
	assignmentID := sess.AssignmentID
	o.mu.Unlock()

	// Step 2 — cleanliness gate. The §5.4 contract puts this on the
	// agent's bead-done skill, but defense in depth: if a recycle
	// arrives on a session whose worktree drifted dirty since the
	// bead-done check (e.g. the operator hand-edited a file in the
	// idle window), we still refuse to destroy.
	if workspace != "" && !worktreeIsClean(workspace) {
		// Convert to End. The pool will spawn a fresh slot on next
		// dispatch tick.
		_, endErr := o.EndSession(ctx, sessionID, core.SessionEndCompleted, "")
		if endErr != nil {
			return core.Session{}, core.WrapAdaptorError(core.KindAdaptorDegraded, endErr,
				"native: recycle refused (dirty worktree) and end fallback failed for %s",
				sessionID)
		}
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: recycle refused: %s has uncommitted changes (converted to end; pool will spawn fresh)",
			workspace)
	}

	// Step 3 — recycle keystroke. Best-effort; a stuck pane will be
	// surfaced by the pane-watcher path on the next loop, not here.
	if seq := recycleKeystrokeForAgent(agentType); seq != "" {
		_ = o.cfg.Backend.SendKeys(ctx, paneID, seq)
	}

	// Step 4 — resync to base. Best-effort: we already verified
	// cleanliness, so checkout + pull are safe non-destructive ops.
	// Failures here log + drop through; the new session boots with
	// whatever the worktree currently has.
	if workspace != "" {
		if err := runRecycleGit(ctx, workspace, "checkout", baseBranchForRecycle()); err != nil {
			// Don't fail the recycle — log and continue. The new
			// session will run on whatever branch the worktree is
			// pointing at; the operator will see drift in the SPA.
			_ = err // TODO slog
		}
		if err := runRecycleGit(ctx, workspace, "pull", "--ff-only"); err != nil {
			_ = err // TODO slog
		}
	}

	// Step 5 — mint new session id, remap paneSessions, reset state.
	now := time.Now()
	newSessionID := fmt.Sprintf("%s:recycle:%d", o.cfg.Backend.Name(), now.UnixNano())

	o.mu.Lock()
	prior, ok := o.sessions[sessionID]
	if !ok {
		o.mu.Unlock()
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q vanished mid-recycle", sessionID)
	}
	// Detach the old session id from the pane and attach the new one
	// in place — pool identity stable.
	o.removeSessionFromPaneLocked(paneID, sessionID)
	delete(o.sessions, sessionID)
	newSess := &core.Session{
		ID:           newSessionID,
		AssignmentID: assignmentID,
		AgentID:      prior.AgentID,
		Status:       core.SessionInitializing,
		StartedAt:    now,
		Persona:      personaID,
		ProviderMetadata: map[string]any{
			"pane_id":          paneID,
			"backend":          o.cfg.Backend.Name(),
			"agent_type":       agentType,
			"worktree":         workspace,
			"pane_title":       priorTitle,
			"pane_cwd":         priorCwd,
			"pane_pid":         priorPanePid,
			"started_at":       now.Format(time.RFC3339Nano),
			"reuse_pane":       true,
			"prior_session_id": sessionID,
			"recycled_at":      now.Format(time.RFC3339Nano),
		},
	}
	if personaID != "" {
		newSess.ProviderMetadata["persona_id"] = personaID
	}
	if beadID != "" {
		// Carry the prior bead id forward as metadata for retro
		// grading; the new session has no bead until the next
		// dispatch.
		newSess.ProviderMetadata["prior_bead_id"] = beadID
	}
	o.sessions[newSessionID] = newSess
	o.addSessionToPaneLocked(paneID, newSessionID)
	cp := *newSess
	o.mu.Unlock()

	// Re-register the bridge tailer under the new session id. The
	// pane is the same so frames will land on the new id from now on.
	if o.fanout != nil {
		// Detach old, attach new.
		o.fanout.Unregister(sessionID)
		_ = o.fanout.Register(ctx, newSessionID, agentType)
	}

	// Step 6 — re-deliver the preamble. Best-effort: a missing
	// WorkPlane (tests, degraded mode) just skips this branch and the
	// new session boots without a fresh preamble. The recycle
	// keystroke already cleared the prior in-memory transcript so the
	// model isn't carrying stale context regardless.
	o.redeliverPreamble(ctx, paneID, workspace, agentType, beadID)

	// Step 7 — emit session.recycled for the retro pipeline.
	if o.fanout != nil {
		o.fanout.Publish(core.OrchestrationEvent{
			ID:        "session-recycled:" + newSessionID,
			Kind:      "session.recycled",
			At:        now,
			SessionID: newSessionID,
			Payload: map[string]any{
				"prior_session_id": sessionID,
				"new_session_id":   newSessionID,
				"pane_id":          paneID,
				"persona_id":       personaID,
				"agent_type":       agentType,
				"reason":           "explicit",
				"worktree":         workspace,
			},
		})
	}

	return cp, nil
}

// recycleKeystrokeForAgent returns the per-agent payload that flushes
// the in-memory transcript so the next prompt boots from a clean
// context window. Empty string means "no recycle keystroke for this
// agent type" — the caller skips the keystroke and relies on the
// preamble re-delivery alone.
func recycleKeystrokeForAgent(agentType string) string {
	switch agentType {
	case "claude":
		// `/clear` resets Claude Code's conversation context. The
		// trailing Enter is the backend's send-keys sentinel; tmux
		// translates it into a real newline.
		return "/clear Enter"
	case "shell-only":
		// A re-exec of the shell would lose env state we'd rather
		// preserve. `clear` resets the visible scrollback without
		// killing the process — the model has no in-memory state on
		// shell-only anyway.
		return "clear Enter"
	default:
		return ""
	}
}

// baseBranchForRecycle is the branch a recycle's worktree is reset
// to. For now this is hard-coded to "main" matching the worktrees
// package's default. Configurability lives in gm-s47n.12 alongside
// the rest of the pool config.
func baseBranchForRecycle() string {
	return "main"
}

// runRecycleGit shells `git -C <wt> <args...>` and returns nil on
// success or an error wrapping git's stderr. Used by the recycle's
// resync step (#4).
func runRecycleGit(ctx context.Context, workspace string, args ...string) error {
	full := append([]string{"-C", workspace}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w (%s)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// redeliverPreamble is the recycle counterpart to start.go's preamble
// delivery (gm-native.10). The pane is alive; we just need to push
// the preamble's first-message bytes back in so the new session
// boots with project + workspace + bead context. Best-effort —
// failures degrade to "no preamble" and the new session boots cold,
// which is recoverable.
func (o *OrchestrationPlane) redeliverPreamble(
	ctx context.Context, paneID, workspace, agentType, beadID string,
) {
	if o.cfg.Backend == nil || workspace == "" {
		return
	}
	agent, ok := o.cfg.Registry.Get(agentType)
	if !ok {
		return
	}
	// Bead-mode preamble requires a WorkPlane. Without one, the
	// recycle still resets the pane (good enough for the cold-start
	// case); the next dispatch will deliver its own preamble.
	if o.cfg.WorkPlane == nil || beadID == "" {
		return
	}
	item, err := o.cfg.WorkPlane.GetWorkItem(ctx, core.WorkItemID(beadID))
	if err != nil {
		return
	}
	milestone, _ := milestones.FindMilestoneAncestor(ctx, o.cfg.WorkPlane, item)
	composed := preamble.Build(preamble.Sources{
		RepoRoot:               o.cfg.RepoRoot,
		WorkspaceDir:           workspace,
		InteractionProfilePath: preamble.ResolveProfilePath(o.cfg.RepoRoot, agent.InteractionProfile),
		InteractionMode:        agent.ResolvedInteractionMode(),
		Repository:             surfaceRepoLabel(item),
		Branch:                 surfaceBranchLabel(item),
		Milestone:              milestone,
	}, item)
	strat, err := preamble.Apply(workspace, agent, composed)
	if err != nil || strat.FirstMessage == "" {
		return
	}
	go o.deliverFirstMessage(ctx, paneID, strat.FirstMessage)
}

// recycleAgentResolved confirms the agent type is in our registry.
// Used as a defensive guard in tests that hand-roll an agent_type
// metadata key without hitting the start path.
func recycleAgentResolved(reg agents.Registry, agentType string) bool {
	_, ok := reg.Get(agentType)
	return ok
}

// _ keeps recycleAgentResolved exported in package internals; the
// reaper test uses it to validate fixture wiring.
var _ = recycleAgentResolved
