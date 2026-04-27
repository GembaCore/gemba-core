package selection

import (
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/enrichment"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/claims"
	"github.com/MikeBengtson/gemba/internal/planner/intent"
	"github.com/MikeBengtson/gemba/internal/planner/runway"
	"github.com/MikeBengtson/gemba/internal/planner/scoring"
)

// ── Helpers ────────────────────────────────────────────────────

func sessCtx(id string) planner.OperationalContext {
	return planner.OperationalContext{
		Session:   &core.Session{ID: id, Status: core.SessionReady},
		Agent:     &core.AgentRef{ID: core.AgentID(id + "-agent")},
		Workspace: &core.Workspace{Repository: "gemba", Branch: "main"},
		Health:    &planner.SessionHealth{},
	}
}

func liveCtx(id, repo, branch string) planner.OperationalContext {
	return planner.OperationalContext{
		Session:   &core.Session{ID: id, Status: core.SessionWorking},
		Workspace: &core.Workspace{Repository: repo, Branch: branch},
	}
}

func bead(id string, opts ...func(*Candidate)) Candidate {
	c := Candidate{
		BeadID:         core.WorkItemID(id),
		Repositories:   []string{"other-repo"},
		Branch:         "main",
		DispatchStatus: enrichment.DispatchReady,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func withConcepts(t ...planner.ConceptTag) func(*Candidate) {
	return func(c *Candidate) { c.Concepts = t }
}
func withDispatch(s enrichment.DispatchStatus) func(*Candidate) {
	return func(c *Candidate) { c.DispatchStatus = s }
}
func withSize(s enrichment.EstimatedSize) func(*Candidate) {
	return func(c *Candidate) { c.EstimatedSize = s }
}
func withRepo(r string) func(*Candidate) {
	return func(c *Candidate) { c.Repositories = []string{r} }
}
func withEpic(e string) func(*Candidate) {
	return func(c *Candidate) { c.EpicID = core.WorkItemID(e) }
}
func withLabels(l ...string) func(*Candidate) {
	return func(c *Candidate) { c.Labels = l }
}
func withAge(d time.Duration) func(*Candidate) {
	return func(c *Candidate) { c.Age = d }
}

type fakeDeps struct {
	blocks map[core.WorkItemID][]core.WorkItemID
	closed map[core.WorkItemID]bool
}

func (f fakeDeps) Blocks(id core.WorkItemID) []core.WorkItemID { return f.blocks[id] }
func (f fakeDeps) IsOpen(id core.WorkItemID) bool              { return !f.closed[id] }

// ── Hard gates ─────────────────────────────────────────────────

func TestSelect_DispatchStatusFilterRejectsNonReady(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-ready"),
			bead("gm-blocked", withDispatch(enrichment.DispatchAwaitingDesign)),
			bead("gm-paused", withDispatch(enrichment.DispatchNotNow)),
		},
		Ctx: sessCtx("sess-1"),
	})
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
	dispatchable := dispatchableIDs(results)
	if len(dispatchable) != 1 || dispatchable[0] != "gm-ready" {
		t.Errorf("dispatchable = %v, want [gm-ready]", dispatchable)
	}
	for _, r := range results {
		if r.BeadID == "gm-blocked" || r.BeadID == "gm-paused" {
			if r.Reason != "dispatch_status" {
				t.Errorf("%s reason = %q, want dispatch_status", r.BeadID, r.Reason)
			}
		}
	}
}

func TestSelect_EmptyDispatchStatusTreatedAsReady(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{{BeadID: "gm-legacy", Repositories: []string{"other"}, Branch: "main"}},
		Ctx:        sessCtx("sess-1"),
	})
	if len(results) != 1 || results[0].Outcome != OutcomeDispatchable {
		t.Errorf("legacy bead with empty status should be dispatchable; got %+v", results[0])
	}
}

func TestSelect_OwnerClaimFilterRejectsClaimedByOther(t *testing.T) {
	idx := claims.NewIndex()
	idx.Set(claims.Claim{BeadID: "gm-claimed", SessionID: "sess-other", ClaimedAt: time.Now()})
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-claimed"),
			bead("gm-free"),
		},
		Ctx:        sessCtx("sess-1"),
		ClaimIndex: idx,
	})
	for _, r := range results {
		switch r.BeadID {
		case "gm-claimed":
			if r.Outcome != OutcomeRejected || r.Reason != "owner_claim" {
				t.Errorf("claimed bead should reject; got %+v", r)
			}
		case "gm-free":
			if r.Outcome != OutcomeDispatchable {
				t.Errorf("free bead should pass; got %+v", r)
			}
		}
	}
}

func TestSelect_OwnerClaimFilterAllowsOwnClaim(t *testing.T) {
	// A session can pick its own claimed bead.
	idx := claims.NewIndex()
	idx.Set(claims.Claim{BeadID: "gm-mine", SessionID: "sess-1", ClaimedAt: time.Now()})
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-mine")},
		Ctx:        sessCtx("sess-1"),
		ClaimIndex: idx,
	})
	if results[0].Outcome != OutcomeDispatchable {
		t.Errorf("self-claim should not block; got %+v", results[0])
	}
}

func TestSelect_ConflictFilterRejectsBeadsInLiveWorkspace(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-collides", withRepo("gemba")),
			bead("gm-clear", withRepo("other-repo")),
		},
		Ctx: sessCtx("sess-1"),
		LiveSessions: []planner.OperationalContext{
			liveCtx("sess-other", "gemba", "main"),
		},
	})
	for _, r := range results {
		switch r.BeadID {
		case "gm-collides":
			if r.Outcome != OutcomeRejected || r.Reason != "conflict" {
				t.Errorf("collides should reject; got %+v", r)
			}
		case "gm-clear":
			if r.Outcome != OutcomeDispatchable {
				t.Errorf("clear should pass; got %+v", r)
			}
		}
	}
}

func TestSelect_ConflictFilterIgnoresOwnLiveContext(t *testing.T) {
	// Live session is THIS session — its workspace is not a
	// conflict for itself.
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-here", withRepo("gemba"))},
		Ctx:        sessCtx("sess-1"),
		LiveSessions: []planner.OperationalContext{
			liveCtx("sess-1", "gemba", "main"),
		},
	})
	if results[0].Outcome != OutcomeDispatchable {
		t.Errorf("own session shouldn't conflict; got %+v", results[0])
	}
}

// ── Soft gates ─────────────────────────────────────────────────

func TestSelect_RunwayGateDemotesOversizedBeads(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-fits", withSize(enrichment.SizeSmall)),
			bead("gm-too-big", withSize(enrichment.SizeLarge)),
		},
		Ctx:    sessCtx("sess-1"),
		Runway: runway.Runway{Bucket: enrichment.SizeSmall},
	})
	var fits, oversized Result
	for _, r := range results {
		if r.BeadID == "gm-fits" {
			fits = r
		}
		if r.BeadID == "gm-too-big" {
			oversized = r
		}
	}
	if !oversized.Components.RunwayDemoted {
		t.Errorf("oversized bead should be demoted; got %+v", oversized.Components)
	}
	if oversized.Score >= fits.Score {
		t.Errorf("demoted score should be smaller: oversized=%v fits=%v", oversized.Score, fits.Score)
	}
}

func TestSelect_RunwayGateSkipsUnestimatedBeads(t *testing.T) {
	// EstimatedSize empty → don't penalise.
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-unknown")},
		Ctx:        sessCtx("sess-1"),
		Runway:     runway.Runway{Bucket: enrichment.SizeSmall},
	})
	if results[0].Components.RunwayDemoted {
		t.Error("unestimated bead must NOT be demoted by runway gate")
	}
}

func TestSelect_IntentGateDemotesOutOfFocusBeads(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-in", withEpic("gm-e3")),
			bead("gm-out", withEpic("gm-e9")),
		},
		Ctx:    sessCtx("sess-1"),
		Intent: intent.Intent{EpicID: "gm-e3", DemotionFactor: 0.3},
	})
	var in, out Result
	for _, r := range results {
		if r.BeadID == "gm-in" {
			in = r
		}
		if r.BeadID == "gm-out" {
			out = r
		}
	}
	if !out.Components.IntentDemoted {
		t.Errorf("out-of-intent should be demoted; got %+v", out.Components)
	}
	if in.Components.IntentDemoted {
		t.Errorf("in-intent should NOT be demoted; got %+v", in.Components)
	}
	if out.Score >= in.Score {
		t.Errorf("demotion should drop score: out=%v in=%v", out.Score, in.Score)
	}
}

func TestSelect_IntentGateNoOpWhenIntentZero(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-1", withEpic("gm-e3"))},
		Ctx:        sessCtx("sess-1"),
		Intent:     intent.Intent{}, // empty intent
	})
	if results[0].Components.IntentDemoted {
		t.Error("empty intent should not demote anyone")
	}
}

func TestSelect_FairnessBoostAddsToOldBeads(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-fresh", withAge(0)),
			bead("gm-old", withAge(5*time.Hour)),
		},
		Ctx: sessCtx("sess-1"),
		Weights: Weights{
			Affinity: 0.6, Leverage: 0.25, EpicAffinity: 0.15,
			FairnessBoostPerHour: 0.05, FairnessBoostMax: 0.30,
		},
	})
	var fresh, old Result
	for _, r := range results {
		if r.BeadID == "gm-fresh" {
			fresh = r
		}
		if r.BeadID == "gm-old" {
			old = r
		}
	}
	if old.Components.FairnessBoost <= 0 {
		t.Errorf("old bead should get a boost; got %v", old.Components.FairnessBoost)
	}
	if fresh.Components.FairnessBoost != 0 {
		t.Errorf("fresh bead must not get a boost; got %v", fresh.Components.FairnessBoost)
	}
	if old.Score <= fresh.Score {
		t.Errorf("boost should lift score: old=%v fresh=%v", old.Score, fresh.Score)
	}
}

func TestSelect_FairnessBoostHonoursMax(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-ancient", withAge(100*time.Hour))},
		Ctx:        sessCtx("sess-1"),
		Weights: Weights{
			Affinity: 0.6, Leverage: 0.25, EpicAffinity: 0.15,
			FairnessBoostPerHour: 0.05, FairnessBoostMax: 0.30,
		},
	})
	if results[0].Components.FairnessBoost != 0.30 {
		t.Errorf("boost should cap at max; got %v", results[0].Components.FairnessBoost)
	}
}

func TestSelect_FairnessBoostDisabledByNegativeWeight(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-old", withAge(5*time.Hour))},
		Ctx:        sessCtx("sess-1"),
		Weights:    Weights{Affinity: 0.6, Leverage: 0.25, EpicAffinity: 0.15, FairnessBoostPerHour: -1},
	})
	if results[0].Components.FairnessBoost != 0 {
		t.Errorf("negative per-hour weight should disable boost; got %v", results[0].Components.FairnessBoost)
	}
}

// ── Score composition ──────────────────────────────────────────

func TestSelect_LeverageContributesToScore(t *testing.T) {
	deps := fakeDeps{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-leverage": {"gm-blocked-a", "gm-blocked-b"},
	}}
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-leaf"),
			bead("gm-leverage"),
		},
		Ctx:          sessCtx("sess-1"),
		Dependencies: deps,
	})
	var leaf, lever Result
	for _, r := range results {
		if r.BeadID == "gm-leaf" {
			leaf = r
		}
		if r.BeadID == "gm-leverage" {
			lever = r
		}
	}
	if lever.Components.Leverage <= leaf.Components.Leverage {
		t.Errorf("lever should outscore leaf on leverage: lever=%v leaf=%v",
			lever.Components.Leverage, leaf.Components.Leverage)
	}
	if lever.Components.LeverageWeight != 2 {
		t.Errorf("leverage weight = %d, want 2", lever.Components.LeverageWeight)
	}
}

func TestSelect_EpicAffinityContributesToScore(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-sibling", withEpic("gm-e3")),
			bead("gm-stranger", withEpic("gm-e9")),
		},
		Ctx:        sessCtx("sess-1"),
		EpicStreak: scoring.EpicStreak{CurrentEpicID: "gm-e3", ContiguousCount: 1},
	})
	var sib, str Result
	for _, r := range results {
		if r.BeadID == "gm-sibling" {
			sib = r
		}
		if r.BeadID == "gm-stranger" {
			str = r
		}
	}
	if sib.Components.EpicAffinity <= 0 {
		t.Errorf("sibling should score on epic affinity; got %v", sib.Components.EpicAffinity)
	}
	if str.Components.EpicAffinity != 0 {
		t.Errorf("stranger should score 0 on epic affinity; got %v", str.Components.EpicAffinity)
	}
}

// ── Ordering ───────────────────────────────────────────────────

func TestSelect_DispatchableSortedByScoreDesc(t *testing.T) {
	deps := fakeDeps{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-high": {"gm-x", "gm-y", "gm-z", "gm-w"},
	}}
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-low"),
			bead("gm-high"),
		},
		Ctx:          sessCtx("sess-1"),
		Dependencies: deps,
	})
	if results[0].BeadID != "gm-high" {
		t.Errorf("higher score should sort first; got %+v", results)
	}
}

func TestSelect_RejectedComeAfterDispatchableInOutput(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-rejected", withDispatch(enrichment.DispatchNotNow)),
			bead("gm-ok"),
		},
		Ctx: sessCtx("sess-1"),
	})
	if results[0].Outcome != OutcomeDispatchable {
		t.Errorf("dispatchable should come first; got %+v", results)
	}
	if results[1].Outcome != OutcomeRejected {
		t.Errorf("rejected should come last; got %+v", results)
	}
}

// ── Justification + components ─────────────────────────────────

func TestSelect_JustificationCarriesScoreBreakdown(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-1", withConcepts("auth"))},
		Ctx:        sessCtx("sess-1"),
	})
	joined := strings.Join(results[0].Justification, "\n")
	if !strings.Contains(joined, "score_pre_gate") {
		t.Errorf("justification missing pre-gate breakdown:\n%s", joined)
	}
	if !strings.Contains(joined, "score:") {
		t.Errorf("justification missing final score line:\n%s", joined)
	}
}

func TestSelect_JustificationNamesGateThatRejected(t *testing.T) {
	results := Select(Inputs{
		Candidates: []Candidate{bead("gm-x", withDispatch(enrichment.DispatchNotNow))},
		Ctx:        sessCtx("sess-1"),
	})
	if !strings.Contains(strings.Join(results[0].Justification, "\n"), "dispatch_status") {
		t.Errorf("rejection justification should name the gate:\n%v", results[0].Justification)
	}
}

func TestSelect_DeterministicAcrossRuns(t *testing.T) {
	// Same inputs, two calls — outputs must be byte-identical.
	in := Inputs{
		Candidates: []Candidate{
			bead("gm-a", withConcepts("auth"), withAge(time.Hour)),
			bead("gm-b", withConcepts("billing")),
			bead("gm-c", withDispatch(enrichment.DispatchAwaitingDesign)),
		},
		Ctx: sessCtx("sess-1"),
	}
	a := Select(in)
	b := Select(in)
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].BeadID != b[i].BeadID || a[i].Score != b[i].Score {
			t.Errorf("[%d] mismatch: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// ── Spec scenario ──────────────────────────────────────────────

func TestSelect_FullStackScenario(t *testing.T) {
	// One session, six candidates exercising every gate.
	idx := claims.NewIndex()
	idx.Set(claims.Claim{BeadID: "gm-claimed", SessionID: "sess-other", ClaimedAt: time.Now()})

	deps := fakeDeps{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-leverage": {"gm-d1", "gm-d2", "gm-d3"},
	}}

	results := Select(Inputs{
		Candidates: []Candidate{
			bead("gm-blocked", withDispatch(enrichment.DispatchAwaitingDesign)),
			bead("gm-claimed"),
			bead("gm-collides", withRepo("gemba")),
			bead("gm-leverage", withConcepts("auth")),
			bead("gm-sibling", withEpic("gm-e3")),
			bead("gm-out-of-focus", withEpic("gm-e9")),
		},
		Ctx: sessCtx("sess-1"),
		LiveSessions: []planner.OperationalContext{
			liveCtx("sess-other", "gemba", "main"),
		},
		ClaimIndex:   idx,
		Intent:       intent.Intent{EpicID: "gm-e3"},
		EpicStreak:   scoring.EpicStreak{CurrentEpicID: "gm-e3", ContiguousCount: 1},
		Dependencies: deps,
	})

	dispatched := dispatchableIDs(results)
	if !contains(dispatched, "gm-leverage") || !contains(dispatched, "gm-sibling") || !contains(dispatched, "gm-out-of-focus") {
		t.Errorf("expected leverage + sibling + out-of-focus dispatchable; got %v", dispatched)
	}
	for _, r := range results {
		switch r.BeadID {
		case "gm-blocked":
			if r.Reason != "dispatch_status" {
				t.Errorf("gm-blocked reason = %q", r.Reason)
			}
		case "gm-claimed":
			if r.Reason != "owner_claim" {
				t.Errorf("gm-claimed reason = %q", r.Reason)
			}
		case "gm-collides":
			if r.Reason != "conflict" {
				t.Errorf("gm-collides reason = %q", r.Reason)
			}
		}
	}

	// gm-sibling (in-intent + epic streak) should outscore
	// gm-out-of-focus (intent demotion).
	var sib, oof Result
	for _, r := range results {
		if r.BeadID == "gm-sibling" {
			sib = r
		}
		if r.BeadID == "gm-out-of-focus" {
			oof = r
		}
	}
	if sib.Score <= oof.Score {
		t.Errorf("sibling+streak should outrank out-of-focus: sib=%v oof=%v", sib.Score, oof.Score)
	}
}

// ── helpers ────────────────────────────────────────────────────

func dispatchableIDs(rs []Result) []core.WorkItemID {
	out := []core.WorkItemID{}
	for _, r := range rs {
		if r.Outcome == OutcomeDispatchable {
			out = append(out, r.BeadID)
		}
	}
	return out
}

func contains(set []core.WorkItemID, v core.WorkItemID) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}
