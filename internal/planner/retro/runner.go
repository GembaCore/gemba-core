// Bead-close retrospective runner (gm-s47n.8.3).
//
// Subscribes to the events hub, listens for WorkItemClosed, and for
// each closed bead:
//
//   1. Reads declared (targets + concepts) via DeclaredSource.
//   2. Reads actuals (touched files + inferred concepts) via
//      ActualSource — typically backed by a diff over the bead's
//      merge commits + a SourceAnalysis projection.
//   3. Compares declared vs actual.
//   4. Writes the resulting Grade to scorer_grades (gm-s47n.8.2).
//   5. Updates the assigned session profile via ProfileWriter so
//      the next dispatch sees the actual file/concept weights.
//
// The runner is async + decoupled: it MUST NOT block the dispatch
// hot path. A retro that fails to materialise actuals (DiffSource
// unavailable, SourceAnalysis stale) logs and skips — the bead just
// doesn't get a grade until a follow-up retro pass.

package retro

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/agentprofile"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/planner"
)

// DeclaredSource resolves the declared (planner-time) targets and
// concepts for a bead. Implementations read whatever bd-extras
// schema gm-s47n.1.1 finalises; the runner just needs the bead id.
//
// Returns (Declared{}, nil) for a bead that has no declarations —
// the comparator handles the empty-declared case (divergence 1.0
// when actuals are non-empty, 0.0 when both empty).
type DeclaredSource interface {
	Declared(ctx context.Context, beadID core.WorkItemID) (Declared, error)
}

// ActualSource resolves the actual (post-merge) files and concepts
// for a bead. Implementations typically join a git/dolt diff (for
// the file list) with SourceAnalysis (for the inferred concepts).
//
// Returns (Actual{}, ErrUnavailable) when the diff isn't yet
// reachable — the runner treats that as a transient skip and
// re-tries on the next retro pass.
type ActualSource interface {
	Actual(ctx context.Context, beadID core.WorkItemID) (Actual, error)
}

// ErrUnavailable signals a transient failure that should not be
// fatal to the runner — the retro is skipped and the bead can be
// re-graded later (e.g. after the merge commit shows up in the
// indexed working tree).
var ErrUnavailable = errors.New("retro: actuals unavailable")

// AgentProfileWriter is the narrow interface the runner uses to
// fan a bead-completion contribution into the agent's persistent
// profile. *agentprofile.Store satisfies it; tests substitute a
// fake.
type AgentProfileWriter interface {
	RecordCompletion(ctx context.Context, ev agentprofile.CompletionEvent) (*agentprofile.AgentProfile, error)
}

// Runner orchestrates retrospective execution. Wire one per server
// process; call Run(ctx, hub) to start consuming events.
//
// All fields are optional except Store. A Runner with no DeclaredSource
// reads declared as empty; with no ActualSource it skips every retro
// (useful for tests). With no ProfileWriter it skips the session-profile
// writeback. This intentionally degrades silently — the spec calls
// the retrospective the "single most important feedback loop", which
// means missing pieces should NEVER tank the dispatch path.
type Runner struct {
	Store    *Store
	Declared DeclaredSource
	Actual   ActualSource
	Profiles planner.ProfileWriter
	// AgentProfiles, when set, gets the same actuals written to the
	// per-agent persistent profile. Distinct from Profiles because
	// the per-day decay axis + scaling rule are agent-specific
	// (work-planning.md §4 Layer 1.2). nil = skip agent profile
	// writeback.
	AgentProfiles AgentProfileWriter

	// HalfLife passes through to Profiles.RecordCompletion. Zero
	// means planner.DefaultDecayHalfLife.
	HalfLife int

	// OnError, when set, receives every error produced during a
	// per-bead retro. Non-fatal; the runner keeps consuming events.
	// Tests use this to assert on the error path; production wires
	// it to a structured logger.
	OnError func(err error)

	// Now is injected for deterministic tests; nil → time.Now.
	Now func() time.Time

	pending sync.WaitGroup
}

// Run subscribes to the hub for WorkItemClosed events and processes
// them until ctx is cancelled. Each retro runs in its own goroutine
// so a slow ActualSource can't back up the event stream.
//
// Returns when the subscription channel closes (hub shutdown or
// ctx cancellation). Callers wishing to wait for in-flight retros
// MUST call Wait() after Run returns.
func (r *Runner) Run(ctx context.Context, hub *events.Hub) {
	if r == nil || hub == nil {
		return
	}
	ch := hub.Subscribe(ctx, events.Filter{Kinds: []events.Kind{events.WorkItemClosed}})
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			r.dispatch(ctx, ev)
		}
	}
}

// Wait blocks until every in-flight retrospective settles. Use
// after Run returns to make sure pending grades land before the
// process exits.
func (r *Runner) Wait() {
	if r == nil {
		return
	}
	r.pending.Wait()
}

// dispatch fans one closed-event out to a goroutine so the runner's
// event loop never stalls on a slow per-bead retrospective.
//
// The spawned goroutine uses a detached context (context.Background)
// rather than the Run-loop's ctx because the retrospective work
// SHOULD outlive a hub shutdown — when ctx cancels (server stop)
// we want pending retros to land their grade rather than abort
// mid-flight. Callers wishing to bound the drain time wrap Wait()
// in their own deadline.
func (r *Runner) dispatch(_ context.Context, ev events.GembaEvent) {
	if ev.WorkItemID == "" {
		return
	}
	r.pending.Add(1)
	go func() {
		defer r.pending.Done()
		if err := r.runOne(context.Background(), ev); err != nil {
			r.report(err)
		}
	}()
}

// RunOne is the synchronous entry point — useful for tests and for
// command-line "retro this bead now" tooling. Production uses Run +
// dispatch which fans out to goroutines.
func (r *Runner) RunOne(ctx context.Context, ev events.GembaEvent) error {
	if r == nil {
		return errors.New("retro.Runner.RunOne: nil runner")
	}
	return r.runOne(ctx, ev)
}

func (r *Runner) runOne(ctx context.Context, ev events.GembaEvent) error {
	beadID := core.WorkItemID(ev.WorkItemID)
	if beadID == "" {
		return errors.New("retro: closed event missing WorkItemID")
	}

	declared, err := r.readDeclared(ctx, beadID)
	if err != nil {
		return fmt.Errorf("retro %s: read declared: %w", beadID, err)
	}
	actual, err := r.readActual(ctx, beadID)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			// Transient — skip this pass; a future retro picks it up.
			return nil
		}
		return fmt.Errorf("retro %s: read actuals: %w", beadID, err)
	}

	diff := Compare(declared, actual)
	now := r.now()
	closedAt := ev.At
	if closedAt.IsZero() {
		closedAt = now
	}

	grade := Grade{
		BeadID:    beadID,
		ClosedAt:  closedAt,
		SessionID: ev.SessionID,
		AgentID:   core.AgentID(ev.AgentID),
		Declared:  declared,
		Actual:    actual,
		Diff:      diff,
		CreatedAt: now,
	}
	if r.Store != nil {
		if err := r.Store.Insert(ctx, grade); err != nil {
			return fmt.Errorf("retro %s: insert: %w", beadID, err)
		}
	}

	// Session profile writeback — actual values flow into the
	// session's warm context so the next dispatch sees ground
	// truth, not the bead's declarations.
	if r.Profiles != nil && ev.SessionID != "" {
		_, err := r.Profiles.RecordCompletion(ctx, planner.CompletionEvent{
			SessionID:      ev.SessionID,
			AssignmentID:   ev.AssignmentID,
			AgentID:        core.AgentID(ev.AgentID),
			BeadID:         beadID,
			ActualConcepts: actual.Concepts,
			ActualFiles:    actual.Files,
			HalfLife:       r.HalfLife,
			Now:            r.Now,
		})
		if err != nil {
			return fmt.Errorf("retro %s: profile writeback: %w", beadID, err)
		}
	}

	// Agent profile writeback — same actuals, scaled by
	// 1/lifetime_bead_count so a single bead doesn't dominate the
	// long-running profile. Spec §4 Layer 1.2.
	if r.AgentProfiles != nil && ev.AgentID != "" {
		_, err := r.AgentProfiles.RecordCompletion(ctx, agentprofile.CompletionEvent{
			AgentID:  core.AgentID(ev.AgentID),
			BeadID:   beadID,
			Concepts: actual.Concepts,
			Files:    actual.Files,
			Now:      r.Now,
		})
		if err != nil {
			return fmt.Errorf("retro %s: agent profile writeback: %w", beadID, err)
		}
	}
	return nil
}

func (r *Runner) readDeclared(ctx context.Context, beadID core.WorkItemID) (Declared, error) {
	if r.Declared == nil {
		return Declared{}, nil
	}
	return r.Declared.Declared(ctx, beadID)
}

func (r *Runner) readActual(ctx context.Context, beadID core.WorkItemID) (Actual, error) {
	if r.Actual == nil {
		// No source bound → ErrUnavailable so the runner skips
		// rather than recording a "everything diverged" grade.
		return Actual{}, ErrUnavailable
	}
	return r.Actual.Actual(ctx, beadID)
}

func (r *Runner) report(err error) {
	if err == nil {
		return
	}
	if r.OnError != nil {
		r.OnError(err)
	}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
