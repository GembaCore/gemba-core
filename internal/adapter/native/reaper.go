package native

import (
	"context"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// DefaultIdleCeiling is the default idle window (§4.4 of
// docs/design/session-pool.md). A pool member that sits in
// SessionReady longer than this is reaped via EndSession. 30 minutes
// is the spec's recommended default for dev laptops; production
// deployments can raise it via reaper config (the gm-s47n.12 config
// surface).
const DefaultIdleCeiling = 30 * time.Minute

// ReaperConfig tunes the idle reaper. Zero values get defaults so
// callers that don't configure the reaper still get sane behavior.
type ReaperConfig struct {
	// IdleCeiling is the SessionReady residency the reaper drains
	// past. Zero defaults to DefaultIdleCeiling.
	IdleCeiling time.Duration
	// TickInterval is how often the reaper scans. Zero defaults to
	// 1 minute matching the spec.
	TickInterval time.Duration
	// Now is injected for deterministic tests. Production passes
	// nil and the reaper uses time.Now.
	Now func() time.Time
}

// idleSinceMetadataKey is the provider-metadata field the reaper
// reads. Set by handleBeadDone() at the moment the session enters
// SessionReady; falls back to LastHeartbeat when absent.
const idleSinceMetadataKey = "last_bead_done_at"

// idleReaper is the goroutine that drains stale Ready sessions.
// One per OrchestrationPlane (per server), not per pool — pool
// scoping is the daemon's job.
type idleReaper struct {
	plane  *OrchestrationPlane
	cfg    ReaperConfig
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// startIdleReaper launches the reaper goroutine. Returns a stop
// function the adaptor's shutdown path calls; the reaper exits
// cleanly when ctx is cancelled.
//
// The reaper is opt-in via ReaperConfig — zero-config adaptors (no
// Backend) have no panes to reap, so the goroutine is harmless. We
// still launch it so a future test fixture that adds a Backend
// post-construction picks up reaping automatically.
func (o *OrchestrationPlane) startIdleReaper(cfg ReaperConfig) func() {
	if cfg.IdleCeiling <= 0 {
		cfg.IdleCeiling = DefaultIdleCeiling
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &idleReaper{
		plane:  o,
		cfg:    cfg,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go r.run(ctx)
	return r.stop
}

func (r *idleReaper) run(ctx context.Context) {
	defer close(r.done)
	t := time.NewTicker(r.cfg.TickInterval)
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

func (r *idleReaper) stop() {
	r.once.Do(func() {
		r.cancel()
		// Don't block forever on shutdown — the goroutine is
		// guaranteed to return promptly because tickOnce is
		// non-blocking apart from EndSession's grace window.
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
		}
	})
}

// tickOnce scans every session and reaps the stale Ready ones. Pulled
// out so tests can drive single ticks deterministically without
// driving real time.
func (r *idleReaper) tickOnce(ctx context.Context) {
	now := r.cfg.Now()
	candidates := r.staleReadySessions(now)
	for _, sid := range candidates {
		// EndSession owns the destructive cleanup — pane kill,
		// preamble removal, paneSessions teardown. We just call it.
		// SessionEndCompleted is the right mode: reaping is a
		// graceful end of an idle session, not a failure.
		_, _ = r.plane.EndSession(ctx, sid, core.SessionEndCompleted, "")
	}
}

// staleReadySessions returns the session ids whose idle residency
// exceeds the configured ceiling. The lock is held only for the read
// — the actual reap runs unlocked via EndSession.
func (r *idleReaper) staleReadySessions(now time.Time) []string {
	r.plane.mu.Lock()
	defer r.plane.mu.Unlock()
	var out []string
	for sid, sess := range r.plane.sessions {
		if sess.Status != core.SessionReady {
			continue
		}
		// Skip mid-turn sessions even if they got stuck in Ready —
		// the contract is "ActiveTurnID set means do not interrupt"
		// (core/orchestration.go Session doc).
		if sess.ActiveTurnID != "" {
			continue
		}
		idleSince := r.idleSince(sess)
		if idleSince.IsZero() {
			// No timestamp to compare — fall back to StartedAt so a
			// session that's been Ready since boot can still be
			// reaped after the ceiling.
			idleSince = sess.StartedAt
		}
		if now.Sub(idleSince) >= r.cfg.IdleCeiling {
			out = append(out, sid)
		}
	}
	return out
}

// idleSince reads the per-session "when did we go idle?" timestamp.
// Order of preference:
//  1. ProviderMetadata[idleSinceMetadataKey] (set by handleBeadDone)
//  2. Session.LastHeartbeat (the standard health-check timestamp)
//  3. Zero — caller falls back to StartedAt.
func (r *idleReaper) idleSince(sess *core.Session) time.Time {
	if sess == nil {
		return time.Time{}
	}
	if raw, ok := sess.ProviderMetadata[idleSinceMetadataKey].(string); ok && raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return t
		}
	}
	if sess.LastHeartbeat != nil {
		return *sess.LastHeartbeat
	}
	return time.Time{}
}
