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

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/dispatch"
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
//
// Inline claim model (the default — gm-e3.8): a Dispatch call IS the
// atomic claim. When two sessions race on the same bead, the loser's
// Dispatch returns an error matching core.IsAlreadyClaimedError. The
// daemon treats that as a soft skip and picks the next candidate.
type SessionDispatcher interface {
	Dispatch(ctx context.Context, sessionID string, beadID core.WorkItemID) error
}

// TwoPhaseDispatcher is the optional dispatch shape adaptors with
// ClaimModelTwoPhase satisfy: claim a TTL'd reservation, then
// convert it into a session. No in-tree adaptor declares this model
// today; the interface lives so a future adaptor can opt in without
// rewriting the daemon's dispatch loop.
//
// Implementations MUST atomically reserve and return a
// core.Reservation pointer. A nil reservation with no error means
// "no work available right now" — the daemon records OutcomeNoEligibleBead
// and moves on. ConvertReservation either turns the reservation into
// a live session OR releases it (after a non-recoverable error).
type TwoPhaseDispatcher interface {
	ClaimReservation(ctx context.Context, sessionID string, beadID core.WorkItemID) (*core.Reservation, error)
	ConvertReservation(ctx context.Context, sessionID string, reservation *core.Reservation) error
	ReleaseReservation(ctx context.Context, reservationID string) error
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
	// OutcomeBelowFloor is the auto-dispatch floor gate (spec §8.1
	// / gm-s47n.12). The daemon refuses to dispatch when the top
	// pick's combined affinity is below the floor — protects against
	// low-confidence picks. Per-pool config carries the floor; the
	// rig-level default is 0.5.
	OutcomeBelowFloor Outcome = "below_floor"
	// OutcomeNoPersona is the persona-routing-cascade refusal
	// (spec §3.2 / gm-s47n.12). The daemon does not dispatch a
	// bead whose persona did not resolve via any of the three
	// layers (bead extras → routing.<kind> → default_persona).
	// The bead is left for manual drag.
	OutcomeNoPersona Outcome = "no_persona"
	// OutcomeAlreadyClaimed is the inline-claim soft-skip outcome
	// (gm-e3.8 / spec §4 Layer 5 claim-model gate). Recorded on each
	// candidate that loses the inline claim race; the daemon then
	// tries the next candidate from the ranked list. A run that
	// exhausts the per-tick retry budget without a successful claim
	// emits the final OutcomeAlreadyClaimed action so the operator
	// can observe the pile-up.
	OutcomeAlreadyClaimed Outcome = "already_claimed"
)

// MaxSoftSkipRetriesPerTick caps how many "already claimed" candidates
// the daemon will skip past before giving up on the current tick. Bound
// keeps a misbehaving cluster of beads from blowing up a single tick
// (gm-e3.8). A failing dispatch on a non-already-claimed error still
// short-circuits — the bound only applies to soft-skip retries.
const MaxSoftSkipRetriesPerTick = 3

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

	// ClaimModel selects the dispatch path (gm-e3.8). Empty defaults
	// to core.ClaimModelInline — every adaptor in tree today declares
	// inline, so the default keeps pre-gm-e3.8 behavior identical.
	// Set ClaimModel = core.ClaimModelTwoPhase to route through
	// TwoPhase below; the wiring is dormant in tree but exercised by
	// the conformance harness so future adaptors can opt in.
	ClaimModel core.ClaimModel
	// TwoPhase implements the ClaimNextReady → StartSession chain
	// for adaptors that declare ClaimModelTwoPhase. Required when
	// ClaimModel == core.ClaimModelTwoPhase; ignored otherwise.
	TwoPhase TwoPhaseDispatcher

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

	// AutoDispatchFloor is the minimum Layer 5 Selection score
	// below which the daemon refuses to dispatch (spec §8.1).
	// Zero means "no floor" — every dispatchable pick goes through.
	// Operators tune per-pool via [pool.<rig>.<persona>] floor =
	// 0.4 with the rig-level default at [pool] default_floor = 0.5.
	AutoDispatchFloor float64
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
	_ = top // retained for the recycle gate below

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

	// Auto-dispatch floor (spec §8.1). Below the floor the daemon
	// declines to dispatch — the bead will sit in the ready set
	// until a higher-affinity session asks for it or an operator
	// drags it manually. Zero floor disables the gate.
	if d.AutoDispatchFloor > 0 && top.scores.Combined < d.AutoDispatchFloor {
		return Action{
			SessionID: sessionID,
			BeadID:    top.bead.BeadID,
			Outcome:   OutcomeBelowFloor,
			Reason:    "score below floor",
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

	// Branch on the manifest's claim model (gm-e3.8). Inline is the
	// default and matches every in-tree adaptor (gt sling, native
	// pane spawn, noop). TwoPhase is dormant in tree but reachable
	// via the manifest gate so future adaptors can opt in.
	switch d.ClaimModel.Resolved() {
	case core.ClaimModelTwoPhase:
		return d.dispatchTwoPhase(ctx, sessionID, sess, scoredEligible, scoredAll, conflicts, conflictBeads, now)
	default:
		return d.dispatchInline(ctx, sessionID, sess, scoredEligible, scoredAll, conflicts, conflictBeads, now)
	}
}

// dispatchInline runs the inline-claim path: pick the top candidate,
// call Dispatcher.Dispatch (which IS the atomic claim), and on
// core.IsAlreadyClaimedError soft-skip to the next candidate. Bounded
// by MaxSoftSkipRetriesPerTick so a misbehaving cluster of beads
// can't blow up a single tick (gm-e3.8).
func (d *Daemon) dispatchInline(
	ctx context.Context,
	sessionID string,
	sess planner.OperationalContext,
	scoredEligible []scoredBead,
	scoredAll []scoredBead,
	conflicts []planner.WorkspaceCollision,
	conflictBeads map[string]bool,
	now time.Time,
) Action {
	var lastClaimed Action
	skips := 0
	for i, cand := range scoredEligible {
		if i > 0 && skips >= MaxSoftSkipRetriesPerTick {
			// Exhausted the retry budget. Surface the most recent
			// already-claimed action so the operator can observe
			// the pile-up; the bead the daemon settled on is the
			// last candidate it actually attempted to dispatch.
			if lastClaimed.Outcome != "" {
				lastClaimed.Reason = fmt.Sprintf("retry budget exhausted after %d soft skips", skips)
				return lastClaimed
			}
			break
		}
		err := d.Dispatcher.Dispatch(ctx, sessionID, cand.bead.BeadID)
		if err == nil {
			d.Gate.RecordDispatch(sessionID, now)
			decisionID := d.recordDecision(ctx, sessionID, sess, cand, scoredAll, conflicts, conflictBeads, now)
			return Action{
				SessionID:  sessionID,
				BeadID:     cand.bead.BeadID,
				Outcome:    OutcomeDispatched,
				Affinity:   cand.scores,
				DecisionID: decisionID,
			}
		}
		if core.IsAlreadyClaimedError(err) {
			skips++
			lastClaimed = Action{
				SessionID: sessionID,
				BeadID:    cand.bead.BeadID,
				Outcome:   OutcomeAlreadyClaimed,
				Reason:    "another session won the inline claim race",
				Affinity:  cand.scores,
			}
			if l := d.logger(); l != nil {
				l.Info("autodispatch.soft_skip.already_claimed",
					slog.String("session_id", sessionID),
					slog.String("bead_id", string(cand.bead.BeadID)),
					slog.Int("skip_index", skips),
				)
			}
			continue
		}
		// Non-already-claimed error: treat as terminal for this tick.
		return Action{
			SessionID: sessionID,
			BeadID:    cand.bead.BeadID,
			Outcome:   OutcomeError,
			Reason:    fmt.Sprintf("dispatch: %v", err),
		}
	}
	// We walked the entire candidate list without a successful
	// dispatch. If at least one already-claimed soft skip fired,
	// surface that — it's the most informative outcome. Otherwise
	// the candidate list was empty (we already returned NoEligibleBead
	// upstream so this is unreachable in practice).
	if lastClaimed.Outcome != "" {
		return lastClaimed
	}
	return Action{
		SessionID: sessionID,
		Outcome:   OutcomeNoEligibleBead,
		Reason:    "no candidate accepted dispatch",
	}
}

// dispatchTwoPhase routes through the TwoPhaseDispatcher: claim a
// reservation, then convert it. No in-tree adaptor declares
// ClaimModelTwoPhase today; this path is here so the manifest gate
// is wired end-to-end. The path is deliberately narrow — claim,
// convert, done. A failed conversion releases the reservation so
// a TTL doesn't leak. Soft-skip on already-claimed mirrors the
// inline path; the bound is identical.
func (d *Daemon) dispatchTwoPhase(
	ctx context.Context,
	sessionID string,
	sess planner.OperationalContext,
	scoredEligible []scoredBead,
	scoredAll []scoredBead,
	conflicts []planner.WorkspaceCollision,
	conflictBeads map[string]bool,
	now time.Time,
) Action {
	if d.TwoPhase == nil {
		return Action{
			SessionID: sessionID,
			Outcome:   OutcomeError,
			Reason:    "claim_model=two_phase but TwoPhase dispatcher not configured",
		}
	}
	var lastClaimed Action
	skips := 0
	for i, cand := range scoredEligible {
		if i > 0 && skips >= MaxSoftSkipRetriesPerTick {
			if lastClaimed.Outcome != "" {
				lastClaimed.Reason = fmt.Sprintf("retry budget exhausted after %d soft skips", skips)
				return lastClaimed
			}
			break
		}
		reservation, err := d.TwoPhase.ClaimReservation(ctx, sessionID, cand.bead.BeadID)
		if err != nil {
			if core.IsAlreadyClaimedError(err) {
				skips++
				lastClaimed = Action{
					SessionID: sessionID,
					BeadID:    cand.bead.BeadID,
					Outcome:   OutcomeAlreadyClaimed,
					Reason:    "another session won the reservation race",
					Affinity:  cand.scores,
				}
				continue
			}
			return Action{
				SessionID: sessionID,
				BeadID:    cand.bead.BeadID,
				Outcome:   OutcomeError,
				Reason:    fmt.Sprintf("claim_reservation: %v", err),
			}
		}
		if reservation == nil {
			// "No work right now" from the reservation surface — the
			// adaptor declined to mint one. Skip ahead.
			continue
		}
		if err := d.TwoPhase.ConvertReservation(ctx, sessionID, reservation); err != nil {
			// Best-effort release so the TTL doesn't leak.
			_ = d.TwoPhase.ReleaseReservation(ctx, reservation.ID)
			return Action{
				SessionID: sessionID,
				BeadID:    cand.bead.BeadID,
				Outcome:   OutcomeError,
				Reason:    fmt.Sprintf("convert_reservation: %v", err),
			}
		}
		d.Gate.RecordDispatch(sessionID, now)
		decisionID := d.recordDecision(ctx, sessionID, sess, cand, scoredAll, conflicts, conflictBeads, now)
		return Action{
			SessionID:  sessionID,
			BeadID:     cand.bead.BeadID,
			Outcome:    OutcomeDispatched,
			Affinity:   cand.scores,
			DecisionID: decisionID,
		}
	}
	if lastClaimed.Outcome != "" {
		return lastClaimed
	}
	return Action{
		SessionID: sessionID,
		Outcome:   OutcomeNoEligibleBead,
		Reason:    "no candidate accepted reservation",
	}
}

// recordDecision persists the dispatch decision when a Decisions
// store is bound. Best-effort: a persistence failure logs and
// returns the empty id — the dispatch itself already happened, so
// rolling it back would be worse than a missing audit row.
func (d *Daemon) recordDecision(
	ctx context.Context,
	sessionID string,
	sess planner.OperationalContext,
	cand scoredBead,
	scoredAll []scoredBead,
	conflicts []planner.WorkspaceCollision,
	conflictBeads map[string]bool,
	now time.Time,
) string {
	if d.Decisions == nil {
		return ""
	}
	dec := dispatch.Decision{
		BeadID:    cand.bead.BeadID,
		DecidedAt: now,
		SessionID: sessionID,
		Mode:      dispatch.ModeAuto,
		Affinity:  cand.scores,
		Conflicts: dispatch.ConflictSnapshot{Workspace: conflicts},
		ReadySet:  buildReadySetSnapshot(scoredAll, conflictBeads),
		CreatedAt: now,
	}
	if sess.Agent != nil {
		dec.AgentID = sess.Agent.ID
	}
	id, err := d.Decisions.Insert(ctx, dec)
	if err != nil {
		if l := d.logger(); l != nil {
			l.Warn("autodispatch.decisions.insert_failed",
				slog.String("session_id", sessionID),
				slog.String("bead_id", string(cand.bead.BeadID)),
				slog.Any("err", err),
			)
		}
		return ""
	}
	return id
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
