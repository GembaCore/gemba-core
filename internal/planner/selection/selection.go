// Selection layer (gm-v5z2.7). The integration point for WP2.0.

package selection

import (
	"fmt"
	"sort"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/enrichment"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/claims"
	"github.com/MikeBengtson/gemba/internal/planner/intent"
	"github.com/MikeBengtson/gemba/internal/planner/runway"
	"github.com/MikeBengtson/gemba/internal/planner/scoring"
)

// DefaultRunwayDemotion is the multiplier applied to a candidate
// whose estimated_size exceeds the session's runway bucket. Spec
// §4 Layer 5.2 step 4 pins this at 0.5.
const DefaultRunwayDemotion = 0.5

// DefaultFairnessBoostPerHour is the additive boost added per hour
// of bead age in the ready queue. Modest by default — the planner
// shouldn't dispatch the wrong bead just because it's old. Tunable
// via Weights.FairnessBoostPerHour.
const DefaultFairnessBoostPerHour = 0.02

// DefaultFairnessBoostMax caps the cumulative fairness boost so a
// permanently-stuck bead can't drown out the affinity signal.
const DefaultFairnessBoostMax = 0.20

// DefaultWeights mixes the three score components per spec §4
// Layer 3.3. Affinity carries the most weight (it's the primary
// "is this session primed?" signal); Leverage is the
// "what unblocks the most other work?" tilt; EpicAffinity is the
// "finish what you started" nudge.
//
// Sum = 1.0 so the combined score lands in [0, 1] before
// demotions and boosts.
var DefaultWeights = Weights{
	Affinity:             0.60,
	Leverage:             0.25,
	EpicAffinity:         0.15,
	RunwayDemotion:       DefaultRunwayDemotion,
	FairnessBoostPerHour: DefaultFairnessBoostPerHour,
	FairnessBoostMax:     DefaultFairnessBoostMax,
	LeverageK:            0, // 0 → scoring.DefaultLeverageK
}

// Weights tunes the score composition + the soft-gate magnitudes.
// Operators tune via per-rig settings. Zero values fall through
// to the defaults so a partial Weights is well-behaved.
type Weights struct {
	Affinity     float64 `json:"affinity"`
	Leverage     float64 `json:"leverage"`
	EpicAffinity float64 `json:"epic_affinity"`
	// RunwayDemotion multiplies the score when the bead is too
	// big for the session's runway. <=0 → DefaultRunwayDemotion.
	RunwayDemotion float64 `json:"runway_demotion"`
	// FairnessBoostPerHour is added per hour of bead Age. <=0 →
	// DefaultFairnessBoostPerHour. Set to 0 explicitly via a
	// negative value (e.g. -1) to disable; positive values opt in.
	FairnessBoostPerHour float64 `json:"fairness_boost_per_hour"`
	// FairnessBoostMax caps the cumulative fairness boost.
	// <=0 → DefaultFairnessBoostMax.
	FairnessBoostMax float64 `json:"fairness_boost_max"`
	// LeverageK overrides the saturation knob in
	// scoring.Leverage. <=0 → scoring.DefaultLeverageK.
	LeverageK float64 `json:"leverage_k"`
}

// Candidate is the per-bead view Selection consumes. Wider than
// the per-package scorer inputs because Selection has to thread
// every gate's metadata through one call.
type Candidate struct {
	BeadID       core.WorkItemID
	EpicID       core.WorkItemID // parent epic; empty when the bead has no epic
	Labels       []string
	Concepts     []planner.ConceptTag
	Targets      []string
	Repositories []string
	Branch       string
	WorktreePath string
	// DispatchStatus + EstimatedSize are the WP2.1 enrichment
	// fields. Empty DispatchStatus is treated as ready (legacy
	// beads aren't retroactively suppressed); empty
	// EstimatedSize means the runway gate cannot fire and the
	// bead is not demoted (don't penalise unestimated beads).
	DispatchStatus enrichment.DispatchStatus
	EstimatedSize  enrichment.EstimatedSize
	// Age is the bead's wall-clock age in the ready queue. Drives
	// the fairness boost; zero means "no age signal."
	Age time.Duration
}

// Inputs bundles the entire snapshot Select reads. Pure: every
// field is a value or a value-shaped reader; nothing here triggers
// I/O in the gate sequence.
type Inputs struct {
	// Candidates is the ready set the operator wants Selection to
	// rank. Selection does not pull beads itself — the dispatch
	// path materialises the ready set first.
	Candidates []Candidate

	// Ctx is the operational context for the SESSION asking
	// "what should I take next?" Drives Affinity sub-scores +
	// the workspace-conflict graph.
	Ctx planner.OperationalContext

	// LiveSessions is every other session currently working a
	// bead — the workspace-conflict input. Empty slice = no
	// other live work; hard filter passes for everyone.
	LiveSessions []planner.OperationalContext

	// Intent is the operator-pinned focus directive for the
	// asking session. Zero / nil = no focus active; the intent
	// gate is a no-op.
	Intent intent.Intent

	// Runway is the asking session's runway snapshot. Empty /
	// zero-value bucket = unconstrained; the runway gate is a
	// no-op.
	Runway runway.Runway

	// EpicStreak is the asking session's recent-close epic
	// streak (gm-v5z2.5). Drives EpicAffinity. Empty = no
	// streak signal; EpicAffinity returns 0 for every candidate.
	EpicStreak scoring.EpicStreak

	// ClaimIndex is the per-rig claim_index (gm-v5z2.6). nil =
	// no other live claims (test fixtures, fresh boot); the
	// owner-claim filter passes for every candidate.
	ClaimIndex *claims.Index

	// Dependencies is the dependency graph view Leverage walks.
	// nil → Leverage scores 0 for every candidate.
	Dependencies scoring.DependencyView

	// Weights tunes score composition + soft gates. Zero value
	// uses DefaultWeights.
	Weights Weights

	// Now overrides time.Now for deterministic tests. nil →
	// time.Now (only used for fairness-boost arithmetic — no
	// other clock reads in the gate sequence).
	Now func() time.Time
}

// Result is the per-candidate output. One Result per Candidate
// regardless of whether it survived the hard filters; rejected
// candidates carry Outcome=OutcomeRejected + the gate that fired
// in Reason. Selection's caller filters by Outcome to render the
// "dispatchable" subset.
type Result struct {
	BeadID core.WorkItemID `json:"bead_id"`
	// Outcome is "dispatched" (passed every hard filter) or
	// "rejected" (a hard filter excluded the bead).
	Outcome Outcome `json:"outcome"`
	// Reason names the gate that produced Outcome. For
	// dispatched candidates it's empty; for rejected candidates
	// it's the gate id (dispatch_status, owner_claim, conflict).
	Reason string `json:"reason,omitempty"`
	// Score is the final composed value the dispatch order is
	// keyed on. Zero for rejected candidates.
	Score float64 `json:"score"`
	// Components carries the raw inputs to Score so the audit
	// log + coach UI can render "why" without recomputing.
	Components Components `json:"components"`
	// Justification is a per-gate / per-component log of what
	// Selection did. Coach mode renders verbatim; auto-dispatch
	// stamps it on the dispatch event.
	Justification []string `json:"justification"`
}

// Outcome is the per-result classification.
type Outcome string

const (
	OutcomeDispatchable Outcome = "dispatchable"
	OutcomeRejected     Outcome = "rejected"
)

// Components is the score breakdown — what Selection multiplied
// and added to land at Score. Surfaced so a future calibration
// loop (gm-v5z2.8) can grade individual components against the
// retrospective without re-running Selection.
type Components struct {
	Affinity      float64                `json:"affinity"`
	AffinityFull  planner.AffinityScores `json:"affinity_full"`
	Leverage      float64                `json:"leverage"`
	LeverageWeight int                   `json:"leverage_weight"`
	EpicAffinity  float64                `json:"epic_affinity"`
	// PreGateScore is the weighted sum of the three components
	// before runway/intent/fairness gates fire. Helpful when the
	// retrospective wants to ask "did the gates demote a bead the
	// raw score correctly identified?"
	PreGateScore   float64 `json:"pre_gate_score"`
	RunwayDemoted  bool    `json:"runway_demoted,omitempty"`
	IntentDemoted  bool    `json:"intent_demoted,omitempty"`
	FairnessBoost  float64 `json:"fairness_boost,omitempty"`
}

// Select runs the gate sequence + score composition. Returns one
// Result per Candidate, sorted: dispatchable first by Score desc,
// then rejected by BeadID asc (stable for the audit log).
//
// Pure: same inputs always produce the same output. Time-
// dependence comes from Inputs.Now / Candidate.Age.
func Select(in Inputs) []Result {
	w := resolveWeights(in.Weights)
	now := in.Now
	if now == nil {
		now = time.Now
	}
	askingSession := ""
	if in.Ctx.Session != nil {
		askingSession = in.Ctx.Session.ID
	}

	conflictBeads := workspaceConflictBeads(in.Candidates, in.LiveSessions, askingSession)

	out := make([]Result, 0, len(in.Candidates))
	for _, c := range in.Candidates {
		out = append(out, selectOne(c, in, w, conflictBeads, askingSession, now))
	}

	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case out[i].Outcome == OutcomeDispatchable && out[j].Outcome != OutcomeDispatchable:
			return true
		case out[i].Outcome != OutcomeDispatchable && out[j].Outcome == OutcomeDispatchable:
			return false
		case out[i].Outcome == OutcomeDispatchable:
			return out[i].Score > out[j].Score
		default:
			return out[i].BeadID < out[j].BeadID
		}
	})
	return out
}

func selectOne(
	c Candidate,
	in Inputs,
	w Weights,
	conflictBeads map[core.WorkItemID]string,
	askingSession string,
	now func() time.Time,
) Result {
	r := Result{BeadID: c.BeadID, Outcome: OutcomeDispatchable}

	// Hard gates first — short-circuit so we don't waste cycles
	// scoring a candidate the gates would reject.
	if !enrichment.DispatchStatusOrDefault(c.DispatchStatus).IsReady() {
		r.Outcome = OutcomeRejected
		r.Reason = "dispatch_status"
		r.Justification = append(r.Justification,
			fmt.Sprintf("rejected: dispatch_status=%s (only 'ready' beads are candidates)", c.DispatchStatus))
		return r
	}
	if in.ClaimIndex != nil && in.ClaimIndex.SoftConflict(c.BeadID, askingSession) {
		claim, _ := in.ClaimIndex.Get(c.BeadID)
		r.Outcome = OutcomeRejected
		r.Reason = "owner_claim"
		r.Justification = append(r.Justification,
			fmt.Sprintf("rejected: claimed by %s since %s", claim.SessionID, claim.ClaimedAt.UTC().Format(time.RFC3339)))
		return r
	}
	if peer, blocked := conflictBeads[c.BeadID]; blocked {
		r.Outcome = OutcomeRejected
		r.Reason = "conflict"
		r.Justification = append(r.Justification,
			fmt.Sprintf("rejected: workspace conflict with live session %s", peer))
		return r
	}

	// Score composition.
	affinity := planner.Affinity(planner.AffinityBeadInputs{
		BeadID:       string(c.BeadID),
		Concepts:     c.Concepts,
		Targets:      c.Targets,
		Repositories: c.Repositories,
		Branch:       c.Branch,
	}, in.Ctx, nil)
	r.Components.AffinityFull = affinity
	r.Components.Affinity = affinity.Combined

	leverage := scoring.LeverageResult{}
	if in.Dependencies != nil {
		leverage = scoring.Leverage(c.BeadID, in.Dependencies, w.LeverageK)
	}
	r.Components.Leverage = leverage.Score
	r.Components.LeverageWeight = leverage.Weight
	if leverage.Weight > 0 {
		r.Justification = append(r.Justification,
			fmt.Sprintf("leverage: blocks %d downstream beads (score %.2f)", leverage.Weight, leverage.Score))
	}

	epicScore := scoring.EpicAffinity(c.EpicID, in.EpicStreak)
	r.Components.EpicAffinity = epicScore
	if epicScore > 0 {
		r.Justification = append(r.Justification,
			fmt.Sprintf("epic_affinity: in current streak %s (score %.2f)", in.EpicStreak.CurrentEpicID, epicScore))
	}

	preGate := w.Affinity*affinity.Combined + w.Leverage*leverage.Score + w.EpicAffinity*epicScore
	r.Components.PreGateScore = preGate
	r.Justification = append(r.Justification,
		fmt.Sprintf("score_pre_gate: %.2f = %.2f*affinity %.2f + %.2f*leverage %.2f + %.2f*epic %.2f",
			preGate,
			w.Affinity, affinity.Combined,
			w.Leverage, leverage.Score,
			w.EpicAffinity, epicScore))

	score := preGate

	// Runway gate (soft) — demote oversized beads.
	if !in.Runway.FitsBead(c.EstimatedSize) {
		demote := w.RunwayDemotion
		score *= demote
		r.Components.RunwayDemoted = true
		r.Justification = append(r.Justification,
			fmt.Sprintf("demoted ×%.2f: bead size %s exceeds session runway %s",
				demote, c.EstimatedSize, in.Runway.Bucket))
	}

	// Intent gate (soft) — demote out-of-intent.
	if !in.Intent.IsZero() {
		match := intent.Match(in.Intent, intent.Candidate{
			BeadID: c.BeadID,
			EpicID: c.EpicID,
			Labels: c.Labels,
		})
		if !match {
			demote := in.Intent.EffectiveDemotionFactor()
			score *= demote
			r.Components.IntentDemoted = true
			r.Justification = append(r.Justification,
				fmt.Sprintf("demoted ×%.2f: out-of-intent (focus %s)",
					demote, intentSummary(in.Intent)))
		}
	}

	// Fairness boost (soft) — additive, capped.
	boost := fairnessBoost(c.Age, w)
	if boost > 0 {
		score += boost
		r.Components.FairnessBoost = boost
		r.Justification = append(r.Justification,
			fmt.Sprintf("boosted +%.2f: age %s in ready queue", boost, c.Age.Round(time.Minute)))
	}

	r.Score = score
	r.Justification = append(r.Justification, fmt.Sprintf("score: %.2f", score))
	return r
}

// workspaceConflictBeads computes the bead → live-session-id map
// the conflict gate consults. Reuses planner.WorkspaceCollisions
// — the same edge function the auto-dispatch daemon already
// trusts. A conflict only fires when the live session is NOT the
// asking session (a session can pick a bead in its own active
// workspace).
func workspaceConflictBeads(
	candidates []Candidate,
	live []planner.OperationalContext,
	askingSession string,
) map[core.WorkItemID]string {
	if len(candidates) == 0 || len(live) == 0 {
		return nil
	}
	beadTargets := make([]planner.BeadTarget, 0, len(candidates))
	for _, c := range candidates {
		beadTargets = append(beadTargets, planner.BeadTarget{
			BeadID:       string(c.BeadID),
			Repository:   firstRepo(c.Repositories),
			Branch:       c.Branch,
			WorktreePath: c.WorktreePath,
		})
	}
	others := make([]planner.OperationalContext, 0, len(live))
	for _, l := range live {
		if l.Session != nil && l.Session.ID == askingSession {
			continue // the asking session does not conflict with itself
		}
		others = append(others, l)
	}
	edges := planner.WorkspaceCollisions(beadTargets, others)
	out := map[core.WorkItemID]string{}
	for _, e := range edges {
		if e.LiveSessionID == "" {
			continue
		}
		out[core.WorkItemID(e.B)] = e.LiveSessionID
	}
	return out
}

func firstRepo(repos []string) string {
	if len(repos) == 0 {
		return ""
	}
	return repos[0]
}

func resolveWeights(in Weights) Weights {
	w := DefaultWeights
	if in.Affinity > 0 || in.Leverage > 0 || in.EpicAffinity > 0 {
		w.Affinity = in.Affinity
		w.Leverage = in.Leverage
		w.EpicAffinity = in.EpicAffinity
	}
	if in.RunwayDemotion > 0 {
		w.RunwayDemotion = in.RunwayDemotion
	}
	// Negative explicitly disables; positive opts in.
	switch {
	case in.FairnessBoostPerHour < 0:
		w.FairnessBoostPerHour = 0
	case in.FairnessBoostPerHour > 0:
		w.FairnessBoostPerHour = in.FairnessBoostPerHour
	}
	if in.FairnessBoostMax > 0 {
		w.FairnessBoostMax = in.FairnessBoostMax
	}
	if in.LeverageK > 0 {
		w.LeverageK = in.LeverageK
	}
	return w
}

func fairnessBoost(age time.Duration, w Weights) float64 {
	if w.FairnessBoostPerHour <= 0 || age <= 0 {
		return 0
	}
	v := w.FairnessBoostPerHour * age.Hours()
	if w.FairnessBoostMax > 0 && v > w.FairnessBoostMax {
		return w.FairnessBoostMax
	}
	return v
}

func intentSummary(i intent.Intent) string {
	switch {
	case i.EpicID != "":
		return "epic=" + i.EpicID
	case i.Label != "":
		return "label=" + i.Label
	case i.BeadIDRegex != "":
		return "regex=" + i.BeadIDRegex
	default:
		return "(empty)"
	}
}
