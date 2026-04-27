// Auto-dispatch daemon (gm-s47n.6.3). Spec §4 Layer 5.2.
//
// Pulls idle sessions, picks the highest-affinity non-conflicting
// bead, slings it. Every gate the spec requires (kill switch, rate
// limit, recycle threshold) is honoured before the dispatch is
// recorded.

package autodispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/dispatch"
)

// IdleSessionLister returns sessions that just finished a bead and
// are waiting for the next assignment. The daemon dispatches against
// these only — never against actively-working sessions.
type IdleSessionLister interface {
	ListIdle(ctx context.Context) ([]planner.OperationalContext, error)
}

// LiveSessionLister returns sessions currently working a bead. Used
// for the conflict graph: the daemon won't pick a bead whose target
// repo+branch overlaps with a bead a live session is already in.
type LiveSessionLister interface {
	ListLive(ctx context.Context) ([]planner.OperationalContext, error)
}

// ReadySetReader returns the unblocked ready set. Each entry must
// carry the bead's id, target files, repo + branch, and concept tags
// — everything Affinity / WorkspaceCollisions need.
//
// Age is the bead's wall-clock age in the ready queue; the fairness
// boost (gm-s47n.6.4) reads it. For this skeleton, Age is plumbed
// through but not yet consulted.
type ReadySetReader interface {
	ReadySet(ctx context.Context) ([]ReadyBead, error)
}

// ReadyBead is one row in the planner's ready set. The fields here
// duplicate planner.AffinityBeadInputs + planner.BeadTarget rather
// than referencing them directly so a future shape change in either
// package doesn't ripple through the daemon's wire contract.
type ReadyBead struct {
	BeadID       core.WorkItemID
	Concepts     []planner.ConceptTag
	Targets      []string
	Repositories []string
	Branch       string
	WorktreePath string
	// Age is how long this bead has been ready. Carried through
	// for the fairness boost (gm-s47n.6.4).
	Age time.Duration
}

// SessionDispatcher slings a bead onto a session. The native adaptor
// implementation calls OrchestrationPlane.StartSession (or its
// follow-up assignment hook); tests inject a fake that just records
// the call.
type SessionDispatcher interface {
	Dispatch(ctx context.Context, sessionID string, beadID core.WorkItemID) error
}

// SessionRecycler triggers a handoff when ShouldRecycle votes
// recycle. The orchestration plane has the hooks to do this; the
// daemon just asks. Optional — leave nil to skip the recycle gate.
type SessionRecycler interface {
	Recycle(ctx context.Context, sessionID string) error
}

// Action describes a single decision the Tick made. Useful for
// observability + tests; the production loop logs Actions as
// structured events.
type Action struct {
	SessionID string          `json:"session_id"`
	BeadID    core.WorkItemID `json:"bead_id,omitempty"`
	Outcome   Outcome         `json:"outcome"`
	// Reason names which gate fired (if any). Empty when Outcome
	// is OutcomeDispatched.
	Reason string `json:"reason,omitempty"`
	// Affinity is the chosen bead's score; zero-value when no
	// bead was chosen.
	Affinity planner.AffinityScores `json:"affinity,omitempty"`
	// DecisionID is the dispatch.Store row id when the dispatch
	// landed and a store was bound; empty otherwise.
	DecisionID string `json:"decision_id,omitempty"`
}

// Outcome enumerates the per-(session, tick) result. Stable wire
// shape: log consumers + tests pattern-match on the string.
type Outcome string

const (
	OutcomeDispatched      Outcome = "dispatched"
	OutcomeRecycled        Outcome = "recycled"
	OutcomeBlockedByGate   Outcome = "blocked_by_gate"
	OutcomeNoEligibleBead  Outcome = "no_eligible_bead"
	OutcomeBlockedConflict Outcome = "blocked_by_conflict"
	OutcomeError           Outcome = "error"
)

// FairnessConfig controls the age-in-ready-queue boost applied
// when ranking beads for dispatch (gm-s47n.6.4, spec §4 Layer 5.2).
//
// Boost is added to a bead's combined affinity for ranking purposes
// only — the persisted Decision still records the raw planner
// score so the retrospective grades the affinity model, not the
// dispatch tie-breaker. Recycle decisions also use raw scores.
//
// Default per-hour: 0.05; cap: 0.30. A bead that has waited 6+
// hours therefore picks up a +0.30 boost — enough to overtake a
// concept-matched competitor (typical AffinityScores.Combined sit
// in the 0.2-0.6 band) without dwarfing it.
type FairnessConfig struct {
	// PerHour is the linear boost added per hour of age in the
	// ready queue. Zero disables the boost entirely (caller
	// passes &FairnessConfig{} to opt out without a nil check
	// branch).
	PerHour float64
	// Max caps the cumulative boost so a forever-blocked bead
	// can't drown out the affinity signal entirely. Zero means
	// "no cap" (the linear ramp keeps going); operators should
	// set this in production.
	Max float64
}

// DefaultFairness matches the spec note's guidance: gentle boost,
// hard cap. Tunable via Daemon.Fairness.
var DefaultFairness = FairnessConfig{PerHour: 0.05, Max: 0.30}

// Boost returns the additive boost for a bead with the given age.
// Linear ramp clamped at Max. Negative ages and zero PerHour both
// return 0.
func (f FairnessConfig) Boost(age time.Duration) float64 {
	if f.PerHour <= 0 || age <= 0 {
		return 0
	}
	v := f.PerHour * age.Hours()
	if f.Max > 0 && v > f.Max {
		return f.Max
	}
	return v
}

// Daemon holds the dependencies the loop pulls each tick. All
// interface fields are required EXCEPT Recycler (optional — when
// nil the recycle gate becomes a pure no-op) and Decisions (optional
// — when nil dispatch decisions are not persisted).
type Daemon struct {
	Idle       IdleSessionLister
	Live       LiveSessionLister
	Ready      ReadySetReader
	Dispatcher SessionDispatcher
	Recycler   SessionRecycler
	Gate       *planner.DispatchGate
	Decisions  *dispatch.Store

	// Now defaults to time.Now in production; tests inject a
	// fixed clock so per-session rate limits are deterministic.
	Now func() time.Time

	// Logger is consulted on every Action and on transport
	// errors. nil → discard. Production wires slog.Default().
	Logger *slog.Logger

	// Weights overrides the default affinity weighting. nil →
	// planner.DefaultAffinityWeights.
	Weights *planner.AffinityWeights

	// Fairness tunes the age-in-ready-queue boost. nil →
	// DefaultFairness. Pass &FairnessConfig{} to opt out.
	Fairness *FairnessConfig
}

// TickResult bundles every Action produced this tick plus any
// transport-level error that aborted the loop. The error here is
// the loop-level error (e.g. ListIdle returned a database
// disconnect); per-session errors land in Actions with
// Outcome=OutcomeError.
type TickResult struct {
	Actions []Action
	Err     error
}

// Tick runs one pass: pull idle sessions, ready set, live sessions;
// for each idle session apply (recycle-gate, rate-gate, conflict
// filter, affinity ranking) and either dispatch the top pick, recycle
// the session, or record the gate that fired.
//
// Idempotent at the planner level: re-running the same Tick against
// unchanged inputs (clock frozen) produces the same Actions. Real
// I/O obviously diverges — the dispatcher's StartSession would emit
// duplicate sessions on re-run.
func (d *Daemon) Tick(ctx context.Context) TickResult {
	if err := d.validate(); err != nil {
		return TickResult{Err: err}
	}

	now := d.now()
	logger := d.logger()

	idle, err := d.Idle.ListIdle(ctx)
	if err != nil {
		return TickResult{Err: fmt.Errorf("autodispatch: ListIdle: %w", err)}
	}
	if len(idle) == 0 {
		return TickResult{}
	}

	ready, err := d.Ready.ReadySet(ctx)
	if err != nil {
		return TickResult{Err: fmt.Errorf("autodispatch: ReadySet: %w", err)}
	}

	live, err := d.Live.ListLive(ctx)
	if err != nil {
		return TickResult{Err: fmt.Errorf("autodispatch: ListLive: %w", err)}
	}

	// Pre-compute the bead↔bead + bead↔live workspace conflict
	// edges once — every idle session's conflict filter consults
	// the same edge set. Semantic conflicts are NOT computed here
	// (they require source analysis I/O); a follow-up bead can
	// thread them through without changing the public API.
	beadTargets := make([]planner.BeadTarget, 0, len(ready))
	for _, b := range ready {
		beadTargets = append(beadTargets, planner.BeadTarget{
			BeadID:       string(b.BeadID),
			Repository:   firstRepo(b.Repositories),
			Branch:       b.Branch,
			WorktreePath: b.WorktreePath,
		})
	}
	conflicts := planner.WorkspaceCollisions(beadTargets, live)
	conflictBeads := beadConflictMap(conflicts)

	_ = live // currently used only via the precomputed conflict map
	out := make([]Action, 0, len(idle))
	for _, sess := range idle {
		action := d.tickSession(ctx, sess, ready, conflicts, conflictBeads, now)
		out = append(out, action)
		if logger != nil {
			logger.Info("autodispatch.action",
				slog.String("session_id", action.SessionID),
				slog.String("bead_id", string(action.BeadID)),
				slog.String("outcome", string(action.Outcome)),
				slog.String("reason", action.Reason),
				slog.Float64("affinity_combined", action.Affinity.Combined),
			)
		}
	}
	return TickResult{Actions: out}
}

func (d *Daemon) tickSession(
	ctx context.Context,
	sess planner.OperationalContext,
	ready []ReadyBead,
	conflicts []planner.WorkspaceCollision,
	conflictBeads map[string]bool,
	now time.Time,
) Action {
	sessionID := ""
	if sess.Session != nil {
		sessionID = sess.Session.ID
	}
	if sessionID == "" {
		return Action{Outcome: OutcomeError, Reason: "session has no id"}
	}

	// Score every ready bead against this session — including the
	// conflict-blocked ones. The recycle median (rule 1) needs the
	// full distribution, not just the dispatchable subset; without
	// the blocked beads the top pick is by definition the max and
	// the rule can never fire.
	scoredAll := d.rankBeads(ready, sess)
	scoredEligible := filterScored(scoredAll, conflictBeads)
	if len(scoredEligible) == 0 {
		// No bead survives the conflict filter. Don't recycle —
		// nothing to dispatch into.
		return Action{
			SessionID: sessionID,
			Outcome:   OutcomeNoEligibleBead,
			Reason:    "no eligible bead after conflict filter",
		}
	}
	top := scoredEligible[0]

	// Recycle gate. Run BEFORE the rate gate so a session over the
	// recycle threshold is handed off even when the rate limit
	// would have blocked the dispatch — recycling the right
	// session matters more than micro-throttling.
	if d.shouldRecycle(sess, top, scoredAll) {
		if d.Recycler != nil {
			if err := d.Recycler.Recycle(ctx, sessionID); err != nil {
				return Action{
					SessionID: sessionID,
					Outcome:   OutcomeError,
					Reason:    fmt.Sprintf("recycle: %v", err),
				}
			}
		}
		return Action{
			SessionID: sessionID,
			Outcome:   OutcomeRecycled,
			Reason:    "recycle threshold",
			Affinity:  top.scores,
		}
	}

	// Kill switch + rate limit + concurrency cap.
	allowed, gateReason := d.Gate.AllowDispatch(sessionID, now)
	if !allowed {
		return Action{
			SessionID: sessionID,
			Outcome:   OutcomeBlockedByGate,
			Reason:    gateReason,
		}
	}

	// Sling.
	if err := d.Dispatcher.Dispatch(ctx, sessionID, top.bead.BeadID); err != nil {
		return Action{
			SessionID: sessionID,
			BeadID:    top.bead.BeadID,
			Outcome:   OutcomeError,
			Reason:    fmt.Sprintf("dispatch: %v", err),
		}
	}
	d.Gate.RecordDispatch(sessionID, now)

	decisionID := ""
	if d.Decisions != nil {
		dec := dispatch.Decision{
			BeadID:    top.bead.BeadID,
			DecidedAt: now,
			SessionID: sessionID,
			Mode:      dispatch.ModeAuto,
			Affinity:  top.scores,
			Conflicts: dispatch.ConflictSnapshot{Workspace: conflicts},
			ReadySet:  buildReadySetSnapshot(scoredAll, conflictBeads),
			CreatedAt: now,
		}
		if sess.Agent != nil {
			dec.AgentID = sess.Agent.ID
		}
		id, err := d.Decisions.Insert(ctx, dec)
		if err != nil {
			// The dispatch already happened — degrade to logging
			// the persistence failure rather than rolling it back.
			if l := d.logger(); l != nil {
				l.Warn("autodispatch.decisions.insert_failed",
					slog.String("session_id", sessionID),
					slog.String("bead_id", string(top.bead.BeadID)),
					slog.Any("err", err),
				)
			}
		} else {
			decisionID = id
		}
	}

	return Action{
		SessionID:  sessionID,
		BeadID:     top.bead.BeadID,
		Outcome:    OutcomeDispatched,
		Affinity:   top.scores,
		DecisionID: decisionID,
	}
}

// scoredBead pairs a bead with its computed affinity for the session
// being scored, plus the fairness boost. Sort key for ranking:
// effective() desc. Recycle uses scores.Combined (raw) so the
// fairness boost is purely a dispatch tie-breaker.
type scoredBead struct {
	bead   ReadyBead
	scores planner.AffinityScores
	boost  float64
}

// effective is the combined score the daemon ranks by — raw
// affinity plus the age-fairness boost. Used for ordering only;
// scores.Combined remains the canonical "what the affinity model
// said" value.
func (s scoredBead) effective() float64 {
	return s.scores.Combined + s.boost
}

func (d *Daemon) rankBeads(ready []ReadyBead, sess planner.OperationalContext) []scoredBead {
	fairness := d.fairness()
	out := make([]scoredBead, 0, len(ready))
	for _, b := range ready {
		scores := planner.Affinity(planner.AffinityBeadInputs{
			BeadID:       string(b.BeadID),
			Concepts:     b.Concepts,
			Targets:      b.Targets,
			Repositories: b.Repositories,
			Branch:       b.Branch,
		}, sess, d.Weights)
		out = append(out, scoredBead{
			bead:   b,
			scores: scores,
			boost:  fairness.Boost(b.Age),
		})
	}
	// Stable sort so equal-score ties resolve by ready-set order
	// (which is itself determined upstream).
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].effective() > out[j].effective()
	})
	return out
}

// filterScored drops beads whose ID appears in the conflict map.
// Preserves the input ordering so the returned slice's [0] is still
// the highest-affinity dispatchable pick.
func filterScored(in []scoredBead, conflictBeads map[string]bool) []scoredBead {
	out := make([]scoredBead, 0, len(in))
	for _, s := range in {
		if conflictBeads[string(s.bead.BeadID)] {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (d *Daemon) shouldRecycle(
	sess planner.OperationalContext,
	top scoredBead,
	all []scoredBead,
) bool {
	if sess.Health == nil {
		return false
	}
	others := make([]float64, 0, len(all))
	for _, s := range all {
		others = append(others, s.scores.Combined)
	}
	sessionConcepts := map[planner.ConceptTag]float64{}
	if sess.Profile != nil {
		sessionConcepts = sess.Profile.Concepts
	}
	dec := planner.ShouldRecycle(planner.RecycleInputs{
		Health: sess.Health,
		IncomingBead: planner.AffinityBeadInputs{
			BeadID:       string(top.bead.BeadID),
			Concepts:     top.bead.Concepts,
			Targets:      top.bead.Targets,
			Repositories: top.bead.Repositories,
			Branch:       top.bead.Branch,
		},
		IncomingAffinityScores: top.scores,
		ReadySetAffinities:     others,
		SessionConcepts:        sessionConcepts,
	})
	return dec.Recycle
}

func (d *Daemon) validate() error {
	if d.Idle == nil {
		return errors.New("autodispatch: Idle is required")
	}
	if d.Live == nil {
		return errors.New("autodispatch: Live is required")
	}
	if d.Ready == nil {
		return errors.New("autodispatch: Ready is required")
	}
	if d.Dispatcher == nil {
		return errors.New("autodispatch: Dispatcher is required")
	}
	if d.Gate == nil {
		return errors.New("autodispatch: Gate is required")
	}
	return nil
}

func (d *Daemon) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *Daemon) fairness() FairnessConfig {
	if d.Fairness != nil {
		return *d.Fairness
	}
	return DefaultFairness
}

func (d *Daemon) logger() *slog.Logger { return d.Logger }

// Run loops Tick on the given period until ctx is cancelled. Each
// tick's transport errors are logged but never returned — the loop
// is meant to survive transient orchestration faults. ctx
// cancellation is the only clean exit.
func (d *Daemon) Run(ctx context.Context, period time.Duration) error {
	if period <= 0 {
		return errors.New("autodispatch.Run: period must be > 0")
	}
	if err := d.validate(); err != nil {
		return err
	}
	t := time.NewTicker(period)
	defer t.Stop()
	logger := d.logger()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r := d.Tick(ctx)
			if r.Err != nil && logger != nil {
				logger.Warn("autodispatch.tick.error", slog.Any("err", r.Err))
			}
		}
	}
}

func firstRepo(repos []string) string {
	if len(repos) == 0 {
		return ""
	}
	return repos[0]
}

// beadConflictMap names every bead id that is conflict-adjacent to
// a currently-live session. Bead↔bead edges are ignored here — the
// daemon only filters against live-session conflicts; bead↔bead is
// the dispatch-grid's concern (the next tick re-evaluates anyway).
func beadConflictMap(edges []planner.WorkspaceCollision) map[string]bool {
	out := map[string]bool{}
	for _, e := range edges {
		if e.LiveSessionID == "" {
			continue
		}
		out[e.B] = true
	}
	return out
}

// buildReadySetSnapshot turns the scored ranking back into the
// dispatch.ReadySetEntry shape used in the persisted decision row.
// Includes the conflict-filtered-out beads so the retro can ask
// "of EVERY ready bead, was the chosen one actually best?"
func buildReadySetSnapshot(
	scored []scoredBead,
	conflictBeads map[string]bool,
) []dispatch.ReadySetEntry {
	out := make([]dispatch.ReadySetEntry, 0, len(scored))
	for _, s := range scored {
		out = append(out, dispatch.ReadySetEntry{
			BeadID:            s.bead.BeadID,
			AffinityCombined:  s.scores.Combined,
			WorkspaceConflict: conflictBeads[string(s.bead.BeadID)],
		})
	}
	return out
}
