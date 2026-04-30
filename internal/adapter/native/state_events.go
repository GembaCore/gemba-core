package native

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// handleStateEvent reacts to the bridge's session_state_reported
// events (written by the gemba-state CLI, see cmd/gemba-state/main.go
// and internal/adapter/native/bridge/translate.go) and mutates the
// session's Status field. gm-cdph.
//
// The agent calls `gemba-state ready|working|prompting|...` at every
// state boundary; this handler is what makes those signals observable
// on Session.Status.
//
// "bead-done" (gm-s47n.11, session-pool.md §4.2) is a special wire
// token: it means "agent finished this bead and is going idle as a
// pool member." Before transitioning to SessionReady the bridge runs
// a worktree-cleanliness check (§5.4.2 — defense in depth). If the
// worktree is dirty, the transition is REFUSED and an
// `escalation.bead_done_with_dirty_worktree` is opened; the session
// stays in SessionWorking for operator triage.
//
// Invariants:
//   - Terminal statuses absorb: a gemba-state call against an already-
//     completed/failed session is a silent no-op (the agent may still
//     be flushing frames when the operator ended the pane).
//   - Unknown state tokens are dropped silently (already validated
//     server-side by gemba-state; this is a defence-in-depth gate).
func (o *OrchestrationPlane) handleStateEvent(ev core.OrchestrationEvent) {
	if ev.Kind != "session_state_reported" {
		return
	}
	raw, _ := ev.Payload["state"].(string)

	// bead-done is the only token that takes a separate code path: it
	// must run a worktree cleanliness check BEFORE writing the new
	// status (§5.4.2). Every other token is a straight assignment.
	if raw == "bead-done" {
		o.handleBeadDone(ev)
		return
	}

	status, ok := stateToken(raw)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, known := o.sessions[ev.SessionID]
	if !known {
		return
	}
	if isTerminalStatus(sess.Status) {
		return
	}
	sess.Status = status
}

// handleBeadDone implements §5.4.2 of session-pool.md. The agent has
// signaled "this bead is complete." Before we transition the session
// from SessionWorking to SessionReady (the pool's idle state), we
// verify the worktree is clean. A dirty worktree means the agent
// violated §5.4.1 (the bead-done skill must commit + push before
// emitting the token); we refuse the transition and surface the
// divergence as an escalation rather than silently mask it.
//
// Worktree retention (§4.3): on a clean bead-done, the worktree is
// retained — that's the central pool-lifecycle change. The session
// keeps its pane and worktree across the idle window so a future
// dispatch can reuse the warm context. End.go branches on the
// `bead_done_clean` provider-metadata flag we set here to skip
// worktree release.
func (o *OrchestrationPlane) handleBeadDone(ev core.OrchestrationEvent) {
	o.mu.Lock()
	sess, known := o.sessions[ev.SessionID]
	if !known {
		o.mu.Unlock()
		return
	}
	if isTerminalStatus(sess.Status) {
		o.mu.Unlock()
		return
	}
	worktree, _ := sess.ProviderMetadata["worktree"].(string)
	o.mu.Unlock()

	// Empty worktree path is the manual-mode case (gm-hmqj) — sessions
	// running against the canonical RepoRoot don't have a per-bead
	// worktree. Treat them as "clean" by construction; the manual flow
	// pre-dates the pool model and isn't a pool member.
	if worktree != "" && !worktreeIsClean(worktree) {
		o.emitDirtyWorktreeEscalation(ev.SessionID, worktree, ev.At)
		return
	}

	// Clean — proceed with the Working → Ready transition. Mark the
	// session as having reached a clean bead boundary so EndSession
	// (when it eventually fires) skips worktree release on the
	// graceful path.
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, known = o.sessions[ev.SessionID]
	if !known {
		return
	}
	if isTerminalStatus(sess.Status) {
		return
	}
	sess.Status = core.SessionReady
	sess.ActiveTurnID = ""
	if sess.ProviderMetadata == nil {
		sess.ProviderMetadata = map[string]any{}
	}
	sess.ProviderMetadata["bead_done_clean"] = true
	sess.ProviderMetadata["last_bead_done_at"] = time.Now().UTC().Format(time.RFC3339Nano)
}

// worktreeIsClean runs `git -C <worktree> status --porcelain`. Empty
// output → clean. Non-empty or error → dirty (we err on the side of
// surfacing the contract violation rather than silently treating an
// unreadable worktree as clean).
//
// Exposed via a package-level var so tests can inject a fake without
// requiring a real git checkout — recycle.go and reaper.go also use
// it.
var worktreeIsClean = func(path string) bool {
	if path == "" {
		return true
	}
	cmd := exec.CommandContext(context.Background(), "git", "-C", path, "status", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false
	}
	return out.Len() == 0
}

// emitDirtyWorktreeEscalation publishes an escalation_opened event for
// a §5.4.2 contract violation. We piggy-back the existing escalation
// infrastructure (see escalations.go) — the event flows through the
// fanout's observer chain into the in-memory escalationIndex and is
// then visible via ListPendingRequests / ListOpenEscalations.
func (o *OrchestrationPlane) emitDirtyWorktreeEscalation(sessionID, worktree string, at time.Time) {
	if o.fanout == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	o.fanout.Publish(core.OrchestrationEvent{
		ID:        "bead-done-dirty:" + sessionID + ":" + at.UTC().Format(time.RFC3339Nano),
		Kind:      "escalation_opened",
		At:        at,
		SessionID: sessionID,
		Payload: map[string]any{
			"escalation_kind": string(core.EscalationBeadDoneWithDirtyWorktree),
			"channel":         string(core.ChannelNotification),
			"urgency":         string(core.UrgencyBlocking),
			"title":           "bead-done with dirty worktree",
			"prompt": "Agent emitted bead-done while " + worktree +
				" had uncommitted changes. The §5.4.1 contract requires" +
				" commit + push before bead-done. Session held in Working" +
				" until operator triage.",
			"worktree": worktree,
		},
	})
}

// stateToken maps the wire token ("ready", "working", …) to a
// core.SessionStatus. Returns ("", false) for unknowns so callers can
// drop the event. "bead-done" is intentionally NOT in this table —
// it's handled separately via handleBeadDone (§5.4.2).
func stateToken(token string) (core.SessionStatus, bool) {
	switch token {
	case "initializing":
		return core.SessionInitializing, true
	case "ready":
		return core.SessionReady, true
	case "working":
		return core.SessionWorking, true
	case "prompting":
		return core.SessionPrompting, true
	case "stalled":
		return core.SessionStalled, true
	}
	return "", false
}
