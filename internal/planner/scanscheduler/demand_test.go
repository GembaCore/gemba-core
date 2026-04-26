package scanscheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMustScanBeforeDispatch_NoSemanticHistorySkips(t *testing.T) {
	s, _ := newSchedAt(t0())
	d := s.MustScanBeforeDispatch(context.Background(), DispatchDemandState{
		Now: t0(), Repo: "gt", CandidatesHaveSemanticHistory: false,
	})
	if d.Required {
		t.Errorf("Required=true with no history; expected false")
	}
}

func TestMustScanBeforeDispatch_StaleIndexBlocks(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	fr.mu.Lock()
	fr.freshness["gt"] = Freshness{
		Repo:      "gt",
		IndexedAt: now.Add(-2 * time.Hour), // > 30m default threshold
	}
	fr.mu.Unlock()
	d := s.MustScanBeforeDispatch(context.Background(), DispatchDemandState{
		Now: now, Repo: "gt", CandidatesHaveSemanticHistory: true,
	})
	if !d.Required {
		t.Errorf("Required=false with stale index + history; expected true")
	}
}

func TestMustScanBeforeDispatch_FreshIndexAllowsDispatch(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	fr.mu.Lock()
	fr.freshness["gt"] = Freshness{
		Repo:          "gt",
		IndexedAt:     now.Add(-2 * time.Minute),
		HeadCommit:    "abc",
		IndexedCommit: "abc",
	}
	fr.mu.Unlock()
	d := s.MustScanBeforeDispatch(context.Background(), DispatchDemandState{
		Now: now, Repo: "gt", CandidatesHaveSemanticHistory: true,
	})
	if d.Required {
		t.Errorf("Required=true with fresh index; expected false")
	}
}

func TestMustScanBeforeDispatch_FreshnessProbeErrorIsTreatedAsStale(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	fr.mu.Lock()
	fr.freshErr["gt"] = errors.New("network down")
	fr.mu.Unlock()
	d := s.MustScanBeforeDispatch(context.Background(), DispatchDemandState{
		Now: now, Repo: "gt", CandidatesHaveSemanticHistory: true,
	})
	if !d.Required {
		t.Errorf("Required=false on freshness probe error; expected true (fail closed)")
	}
}
