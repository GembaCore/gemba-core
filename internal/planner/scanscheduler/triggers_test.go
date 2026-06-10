package scanscheduler

import (
	"testing"
	"time"
)

func t0() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }

func TestEvaluateTriggers_PostMergeWaveFiresAtThreshold(t *testing.T) {
	now := t0()
	merges := []MergeEvent{
		{Repo: "gt", At: now.Add(-12 * time.Minute)},
		{Repo: "gt", At: now.Add(-10 * time.Minute)},
		{Repo: "gt", At: now.Add(-8 * time.Minute)},
		{Repo: "gt", At: now.Add(-6 * time.Minute)},
		{Repo: "gt", At: now.Add(-4 * time.Minute)},
	}
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt", RecentMerges: merges,
	}, DefaultConfig())
	if !hasTrigger(got, TriggerPostMergeWave) {
		t.Fatalf("expected PostMergeWave; got %v", kinds(got))
	}
}

func TestEvaluateTriggers_PostMergeWaveBelowThresholdSilent(t *testing.T) {
	now := t0()
	merges := []MergeEvent{
		{Repo: "gt", At: now.Add(-5 * time.Minute)},
		{Repo: "gt", At: now.Add(-2 * time.Minute)},
	}
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt", RecentMerges: merges,
	}, DefaultConfig())
	if hasTrigger(got, TriggerPostMergeWave) {
		t.Errorf("PostMergeWave fired below threshold: %v", got)
	}
}

func TestEvaluateTriggers_PostMergeWaveIgnoresOtherRepos(t *testing.T) {
	now := t0()
	// Five merges in 'other'; should not fire for 'gt'.
	merges := []MergeEvent{
		{Repo: "other", At: now.Add(-5 * time.Minute)},
		{Repo: "other", At: now.Add(-4 * time.Minute)},
		{Repo: "other", At: now.Add(-3 * time.Minute)},
		{Repo: "other", At: now.Add(-2 * time.Minute)},
		{Repo: "other", At: now.Add(-1 * time.Minute)},
	}
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt", RecentMerges: merges,
	}, DefaultConfig())
	if hasTrigger(got, TriggerPostMergeWave) {
		t.Errorf("wave on other-repo merges leaked into gt: %v", got)
	}
}

func TestEvaluateTriggers_ParallelCompletionBarrierFires(t *testing.T) {
	now := t0()
	got := EvaluateTriggers(PlannerState{
		Now:                now,
		Repo:               "gt",
		LastSuccessfulScan: now.Add(-30 * time.Minute),
		ParallelBatches: []BatchCompletion{
			{Repo: "gt", BatchID: "B-1", CompletedAt: now.Add(-5 * time.Minute), AllBeadsDone: true},
		},
	}, DefaultConfig())
	if !hasTrigger(got, TriggerParallelCompletionBarrier) {
		t.Errorf("expected barrier; got %v", kinds(got))
	}
}

func TestEvaluateTriggers_ParallelBarrierDoesNotFireWhenAlreadyScanned(t *testing.T) {
	now := t0()
	// Batch finished BEFORE the last successful scan — already
	// covered, no need to re-scan.
	got := EvaluateTriggers(PlannerState{
		Now:                now,
		Repo:               "gt",
		LastSuccessfulScan: now.Add(-2 * time.Minute),
		ParallelBatches: []BatchCompletion{
			{Repo: "gt", BatchID: "B-1", CompletedAt: now.Add(-30 * time.Minute), AllBeadsDone: true},
		},
	}, DefaultConfig())
	if hasTrigger(got, TriggerParallelCompletionBarrier) {
		t.Errorf("barrier fired despite scan-after-batch: %v", got)
	}
}

func TestEvaluateTriggers_ParallelBarrierDoesNotFireOnPartialBatch(t *testing.T) {
	now := t0()
	got := EvaluateTriggers(PlannerState{
		Now:                now,
		Repo:               "gt",
		LastSuccessfulScan: now.Add(-2 * time.Hour),
		ParallelBatches: []BatchCompletion{
			{Repo: "gt", BatchID: "B-1", CompletedAt: now.Add(-5 * time.Minute), AllBeadsDone: false},
		},
	}, DefaultConfig())
	if hasTrigger(got, TriggerParallelCompletionBarrier) {
		t.Errorf("barrier fired on partial batch: %v", got)
	}
}

func TestEvaluateTriggers_WallClockFloorRequiresMergesInInterval(t *testing.T) {
	now := t0()
	// 5h since last scan → past the 4h floor — but no merges in
	// the interval, so the floor stays quiet.
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt",
		LastSuccessfulScan: now.Add(-5 * time.Hour),
	}, DefaultConfig())
	if hasTrigger(got, TriggerWallClockFloor) {
		t.Errorf("floor fired with no merges in interval: %v", got)
	}

	// Same gap but with one merge — fires.
	got = EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt",
		LastSuccessfulScan: now.Add(-5 * time.Hour),
		RecentMerges: []MergeEvent{
			{Repo: "gt", At: now.Add(-2 * time.Hour)},
		},
	}, DefaultConfig())
	if !hasTrigger(got, TriggerWallClockFloor) {
		t.Errorf("floor did not fire with merges in interval: %v", got)
	}
}

func TestEvaluateTriggers_DriftFiresWhenBackendReportsCommitsAhead(t *testing.T) {
	now := t0()
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt",
		LastSuccessfulScan: now.Add(-5 * time.Minute), // recent
		Freshness: &Freshness{
			Repo:          "gt",
			IndexedAt:     now.Add(-5 * time.Minute),
			HeadCommit:    "abc",
			IndexedCommit: "def",
			CommitsAhead:  3,
		},
	}, DefaultConfig())
	if !hasTrigger(got, TriggerDriftSignal) {
		t.Errorf("drift did not fire with HEAD ahead of indexed commit: %v", got)
	}
}

func TestEvaluateTriggers_DriftFiresWhenIndexedAtIsZero(t *testing.T) {
	now := t0()
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt",
		Freshness: &Freshness{Repo: "gt"}, // zero IndexedAt
	}, DefaultConfig())
	if !hasTrigger(got, TriggerDriftSignal) {
		t.Errorf("drift did not fire on zero IndexedAt: %v", got)
	}
}

func TestEvaluateTriggers_DriftIgnoresOtherRepoFreshness(t *testing.T) {
	now := t0()
	got := EvaluateTriggers(PlannerState{
		Now: now, Repo: "gt",
		Freshness: &Freshness{Repo: "other", CommitsAhead: 100},
	}, DefaultConfig())
	if hasTrigger(got, TriggerDriftSignal) {
		t.Errorf("drift on other-repo freshness leaked into gt: %v", got)
	}
}

func TestConfig_ResolvedFillsZeroDefaults(t *testing.T) {
	cfg := Config{}.resolved()
	if cfg.PostMergeMinCount != 5 {
		t.Errorf("PostMergeMinCount default = %d, want 5", cfg.PostMergeMinCount)
	}
	if cfg.MinScanInterval != 10*time.Minute {
		t.Errorf("MinScanInterval default = %s, want 10m", cfg.MinScanInterval)
	}

	override := Config{PostMergeMinCount: 3}.resolved()
	if override.PostMergeMinCount != 3 {
		t.Errorf("explicit PostMergeMinCount overridden by default")
	}
	if override.MinScanInterval != 10*time.Minute {
		t.Errorf("explicit override clobbered other defaults")
	}
}

func hasTrigger(ts []Trigger, k TriggerKind) bool {
	for _, t := range ts {
		if t.Kind == k {
			return true
		}
	}
	return false
}

func kinds(ts []Trigger) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Kind.String()
	}
	return out
}
