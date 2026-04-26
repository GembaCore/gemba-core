package native

import (
	"context"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
	"github.com/MikeBengtson/gemba/internal/adapter/native/preamble"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/persona"
)

// graceBeforeKill is how long EndSession waits after sending the
// agent's quit sequence before force-killing the pane. 3s is long
// enough for Claude Code to flush pending tool output and exit
// cleanly, short enough that a stuck session doesn't block the
// operator.
const graceBeforeKill = 3 * time.Second

// EndSession implements the graceful teardown path (gm-native.12).
// Flow:
//  1. Look up the session + its pane; unknown id → KindSessionNotFound.
//  2. Same-nonce replay returns the cached terminal session verbatim.
//  3. Terminal absorption: re-ending a completed/failed session is a
//     no-op that returns the prior terminal session.
//  4. Send the agent's quit sequence (/exit for Claude, exit for
//     shell-only) with a grace window.
//  5. If the pane is still alive, Backend.Kill.
//  6. Remove the preamble sentinel block from CLAUDE.md.
//  7. Unregister the bridge tailer.
//  8. Remove state (session record, paneActive entry).
//  9. Populate SessionCloseReason based on EndMode.
func (o *OrchestrationPlane) EndSession(
	ctx context.Context, sessionID string, mode core.SessionEndMode, nonce core.ConfirmNonce,
) (core.Session, error) {
	if o.cfg.Backend == nil {
		return core.Session{}, unsupported("EndSession")
	}
	if sessionID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: EndSession requires session id")
	}

	// Nonce replay: return cached session (which may already be
	// terminal from the first call).
	if nonce != "" {
		if cachedID, seen := o.lookupNonce(string(nonce)); seen && cachedID == sessionID {
			if s := o.readSession(sessionID); s != nil {
				return *s, nil
			}
		}
	}

	o.mu.Lock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		o.mu.Unlock()
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q unknown", sessionID)
	}
	if isTerminalStatus(sess.Status) {
		cp := *sess
		o.mu.Unlock()
		return cp, nil
	}
	paneID, _ := sess.ProviderMetadata["pane_id"].(string)
	workspace, _ := sess.ProviderMetadata["worktree"].(string)
	agentType, _ := sess.ProviderMetadata["agent_type"].(string)
	o.mu.Unlock()

	// Send quit sequence for the agent type. Best-effort — SendKeys
	// may fail if the pane is already dead, which is exactly the
	// case where we still want to proceed with kill + cleanup.
	if paneID != "" {
		quit := quitSequenceForAgent(agentType)
		if quit != "" {
			_ = o.cfg.Backend.SendKeys(ctx, paneID, quit)
		}
		// Grace window — poll ListPanes to see if the pane exits
		// cleanly before we force-kill.
		if !o.waitPaneGone(ctx, paneID) {
			_ = o.cfg.Backend.Kill(ctx, paneID)
		}
	}

	// Preamble cleanup.
	if workspace != "" {
		_ = preamble.RemoveFromClaudeMD(workspace)
	}

	// Surface-file cleanup (gm-v8vr.1): unlink the on-disk allow-list
	// the bridge was reading so a future session id collision can't
	// inherit a stale file. RemoveSurfaceFile is a no-op when the
	// file doesn't exist (best-effort, same as the preamble cleanup).
	_ = persona.RemoveSurfaceFile(sessionID)

	// Unregister bridge tailer.
	o.fanout.Unregister(sessionID)

	// Finalize state.
	now := time.Now()
	reason := closeReasonFromEndMode(mode)
	o.mu.Lock()
	sess.EndedAt = &now
	sess.CloseReason = &reason
	switch mode {
	case core.SessionEndFailed:
		sess.Status = core.SessionFailed
	default:
		sess.Status = core.SessionCompleted
	}
	delete(o.paneActive, paneID)
	if nonce != "" {
		o.nonces[string(nonce)] = sessionID
	}
	cp := *sess
	o.mu.Unlock()
	return cp, nil
}

// quitSequenceForAgent returns the keystroke payload that asks the
// agent to exit cleanly. Convention: send-keys body + trailing
// "Enter" so the backend translates the sentinel into a real
// newline (tmux) or appends one automatically (AppleScript).
func quitSequenceForAgent(agentType string) string {
	switch agentType {
	case "claude":
		return "/exit Enter"
	case "shell-only":
		return "exit Enter"
	default:
		return ""
	}
}

// closeReasonFromEndMode maps the caller's intent into the Session's
// typed close reason. A caller-initiated EndSession is user_stop;
// an end-as-failed is also user_stop (the adaptor has no other
// signal from the mode alone). Pane-death detection populates a
// different reason via the observer goroutine.
func closeReasonFromEndMode(mode core.SessionEndMode) core.SessionCloseReason {
	switch mode {
	case core.SessionEndFailed:
		return core.CloseUserStop
	case core.SessionEndCanceled:
		return core.CloseUserStop
	default:
		return core.CloseUserStop
	}
}

// waitPaneGone polls ListPanes in short increments to see if the
// pane exits within graceBeforeKill. Returns true when the pane is
// gone, false when the grace window expires.
func (o *OrchestrationPlane) waitPaneGone(ctx context.Context, paneID string) bool {
	deadline := time.Now().Add(graceBeforeKill)
	for time.Now().Before(deadline) {
		panes, err := o.cfg.Backend.ListPanes(ctx)
		if err != nil {
			return false
		}
		if !containsPaneID(panes, paneID) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

func isTerminalStatus(s core.SessionStatus) bool {
	return s == core.SessionCompleted || s == core.SessionFailed
}

func containsPaneID(panes []backend.Pane, id string) bool {
	for _, p := range panes {
		if p.ID == id {
			return true
		}
	}
	return false
}
