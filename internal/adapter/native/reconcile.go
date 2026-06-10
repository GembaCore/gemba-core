package native

import (
	"context"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// DefaultReconcileInterval is the default tick for the reconcile
// loop (§9 of docs/design/session-pool.md). Spec says 60s.
const DefaultReconcileInterval = 60 * time.Second

// ReconcileConfig tunes the reconcile loop. Zero values get sane
// defaults so an adaptor that doesn't opt into pool semantics still
// behaves identically to today.
type ReconcileConfig struct {
	// Interval is how often the loop scans. Zero defaults to
	// DefaultReconcileInterval.
	Interval time.Duration
	// Now is injected for deterministic tests. Production passes
	// nil and the loop uses time.Now.
	Now func() time.Time
}

// reconcileLoop watches for sessions that violate the §9 invariants:
// most importantly, agents that crashed mid-`bead-done` (their bead
// is closed in beads but ActiveTurnID is still set on the session).
// When detected, the loop transitions the session to SessionStalled
// and emits an escalation so an operator can triage.
type reconcileLoop struct {
	plane  *OrchestrationPlane
	cfg    ReconcileConfig
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// startReconcileLoop launches the goroutine. Returns a stop function;
// the loop exits cleanly when ctx is cancelled.
func (o *OrchestrationPlane) startReconcileLoop(cfg ReconcileConfig) func() {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultReconcileInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &reconcileLoop{
		plane:  o,
		cfg:    cfg,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go r.run(ctx)
	return r.stop
}

func (r *reconcileLoop) run(ctx context.Context) {
	defer close(r.done)
	t := time.NewTicker(r.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tickOnce(ctx)
		}
	}
}

func (r *reconcileLoop) stop() {
	r.once.Do(func() {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
		}
	})
}

// tickOnce performs one reconcile pass. Light-weight: read sessions
// + their bead state and check for the §9 "ActiveTurnID set on a
// closed bead" divergence. WorkPlane is required for the bead
// status check; without one the loop is a no-op.
func (r *reconcileLoop) tickOnce(ctx context.Context) {
	if r.plane.cfg.WorkPlane == nil {
		return
	}
	now := r.cfg.Now()

	// Snapshot the candidate set under the lock; release before
	// touching the WorkPlane (which can block on transport).
	type candidate struct {
		sessionID string
		beadID    string
	}
	r.plane.mu.Lock()
	cands := make([]candidate, 0, len(r.plane.sessions))
	for sid, sess := range r.plane.sessions {
		if sess.ActiveTurnID == "" {
			continue
		}
		if isTerminalStatus(sess.Status) || sess.Status == core.SessionStalled {
			continue
		}
		bead, _ := sess.ProviderMetadata["bead_id"].(string)
		if bead == "" {
			continue
		}
		cands = append(cands, candidate{sessionID: sid, beadID: bead})
	}
	r.plane.mu.Unlock()

	for _, c := range cands {
		item, err := r.plane.cfg.WorkPlane.GetWorkItem(ctx, core.WorkItemID(c.beadID))
		if err != nil {
			continue
		}
		// "Closed in beads" means the bead reached a terminal state.
		// We guard tightly here because false positives — marking a
		// live session Stalled — are operator-visible noise.
		if !beadIsClosed(item) {
			continue
		}
		r.markStalled(c.sessionID, c.beadID, now)
	}
}

// markStalled flips the session to SessionStalled and publishes an
// escalation. Idempotent — a second call against an already-stalled
// session is a no-op.
func (r *reconcileLoop) markStalled(sessionID, beadID string, at time.Time) {
	r.plane.mu.Lock()
	sess, ok := r.plane.sessions[sessionID]
	if !ok {
		r.plane.mu.Unlock()
		return
	}
	if sess.Status == core.SessionStalled || isTerminalStatus(sess.Status) {
		r.plane.mu.Unlock()
		return
	}
	sess.Status = core.SessionStalled
	r.plane.mu.Unlock()

	if r.plane.fanout == nil {
		return
	}
	r.plane.fanout.Publish(core.OrchestrationEvent{
		ID:        "reconcile-stalled:" + sessionID + ":" + at.UTC().Format(time.RFC3339Nano),
		Kind:      "escalation_opened",
		At:        at,
		SessionID: sessionID,
		Payload: map[string]any{
			"escalation_kind": string(core.EscalationSessionStalledTurn),
			"channel":         string(core.ChannelNotification),
			"urgency":         string(core.UrgencyBlocking),
			"title":           "Session stuck mid-turn on closed bead",
			"prompt": "Session " + sessionID + " has ActiveTurnID set but" +
				" bead " + beadID + " is closed. Likely the agent crashed" +
				" mid-`bead-done` emit (§9). Triage manually.",
			"bead_id": beadID,
		},
	})
}

// beadIsClosed reports whether a work-item has reached a terminal
// state. Defined narrowly to keep this lightweight — we only care
// about the spec's failure mode, not a full lifecycle inference.
//
// Reads StateCategory rather than Status because StateCategory is
// the typed enum (Status is the bd-specific free-form string).
// `completed` is the canonical "done" tag; `canceled` also counts
// because a canceled bead is closed from the agent's perspective.
func beadIsClosed(item core.WorkItem) bool {
	switch item.StateCategory {
	case core.StateCompleted, core.StateCanceled:
		return true
	}
	return false
}
