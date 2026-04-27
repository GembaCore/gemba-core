package walk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestStore() *MemoryStore {
	s := NewMemoryStore()
	var n int64
	s.NewID = func() ID {
		i := atomic.AddInt64(&n, 1)
		return ID(fmt.Sprintf("walk-%03d", i))
	}
	var t int64
	s.NewTID = func() string {
		i := atomic.AddInt64(&t, 1)
		return fmt.Sprintf("turn-%03d", i)
	}
	return s
}

func startWalk(t *testing.T, s *MemoryStore, agenda ...AgendaItem) Walk {
	t.Helper()
	w, err := s.Start(context.Background(), StartParams{
		Workspace:     "ws",
		InitiatedBy:   "user-mike",
		PrimaryPersona: "project-manager",
		InitialAgenda:  agenda,
		Now:            time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return w
}

// ── Start invariants ───────────────────────────────────────────

func TestMemoryStore_StartRequiresWorkspaceAndInitiator(t *testing.T) {
	s := newTestStore()
	cases := []struct {
		name string
		p    StartParams
	}{
		{"no workspace", StartParams{InitiatedBy: "u"}},
		{"no initiator", StartParams{Workspace: "ws"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Start(context.Background(), tc.p); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestMemoryStore_StartActiveByDefault(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	if w.Status != StatusActive {
		t.Errorf("status = %s, want active", w.Status)
	}
	if w.ID == "" {
		t.Error("ID not stamped")
	}
	if w.StartedAt.IsZero() {
		t.Error("StartedAt not stamped")
	}
}

// ── Get / List ─────────────────────────────────────────────────

func TestMemoryStore_GetReturnsClone(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s, AgendaItem{ID: "i1", Status: AgendaQueued})
	got, err := s.Get(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Mutate the returned slice; the store's copy must NOT change.
	got.Agenda[0].Status = AgendaDecided
	got2, _ := s.Get(context.Background(), w.ID)
	if got2.Agenda[0].Status != AgendaQueued {
		t.Errorf("Get returned mutable slice — internal state corrupted: %+v", got2.Agenda[0])
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	s := newTestStore()
	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound; got %v", err)
	}
}

func TestMemoryStore_ListByWorkspaceAndStatus(t *testing.T) {
	s := newTestStore()
	w1 := startWalk(t, s)
	w2 := startWalk(t, s)
	if _, err := s.End(context.Background(), w1.ID, StatusCompleted, time.Now()); err != nil {
		t.Fatalf("End: %v", err)
	}
	all, err := s.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List all = %d, want 2", len(all))
	}
	active, _ := s.List(context.Background(), ListFilter{Statuses: []Status{StatusActive}})
	if len(active) != 1 || active[0].ID != w2.ID {
		t.Errorf("active filter: got %+v, want [%s]", active, w2.ID)
	}
}

// ── AddAgendaItem idempotent ───────────────────────────────────

func TestMemoryStore_AddAgendaItemIdempotent(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	item := AgendaItem{ID: "i1", Status: AgendaQueued}
	w, err := s.AddAgendaItem(context.Background(), w.ID, item)
	if err != nil {
		t.Fatalf("AddAgendaItem: %v", err)
	}
	w, err = s.AddAgendaItem(context.Background(), w.ID, item)
	if err != nil {
		t.Fatalf("idempotent retry failed: %v", err)
	}
	if len(w.Agenda) != 1 {
		t.Errorf("expected 1 agenda item after retry; got %d", len(w.Agenda))
	}
}

// ── UpdateAgendaItemStatus ─────────────────────────────────────

func TestMemoryStore_UpdateAgendaItemStatusBlocksAfterDecision(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s, AgendaItem{ID: "i1", Status: AgendaQueued})
	_, err := s.AppendDecision(context.Background(), w.ID, WalkDecision{
		AgendaItemID: "i1",
		Kind:         DecisionRatify,
		DecidedAt:    time.Now(),
		DecidedBy:    "user",
	})
	if err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	if _, err := s.UpdateAgendaItemStatus(context.Background(), w.ID, "i1", AgendaQueued); err == nil {
		t.Error("expected error when re-status'ing a decided item")
	}
}

// ── AppendTurn ─────────────────────────────────────────────────

func TestMemoryStore_AppendTurnStampsIDAndTime(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	got, err := s.AppendTurn(context.Background(), w.ID, WalkTurn{
		Speaker: SpeakerUser,
		Content: "hi",
	})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	if len(got.Transcript) != 1 {
		t.Fatalf("expected 1 turn; got %d", len(got.Transcript))
	}
	if got.Transcript[0].ID == "" {
		t.Error("turn ID not stamped")
	}
	if got.Transcript[0].At.IsZero() {
		t.Error("turn At not stamped")
	}
}

// ── AppendDecision links agenda + decision ─────────────────────

func TestMemoryStore_AppendDecisionMarksAgendaItem(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s, AgendaItem{ID: "i1", Status: AgendaQueued})
	got, err := s.AppendDecision(context.Background(), w.ID, WalkDecision{
		AgendaItemID: "i1",
		Kind:         DecisionRatify,
		DecidedAt:    time.Now(),
		DecidedBy:    "user",
	})
	if err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	if got.Agenda[0].Status != AgendaDecided {
		t.Errorf("expected agenda item decided; got %s", got.Agenda[0].Status)
	}
	if got.Agenda[0].Decision == nil {
		t.Error("agenda item Decision pointer not set")
	}
	if len(got.Decisions) != 1 {
		t.Errorf("Decisions slice not appended; got %d", len(got.Decisions))
	}
}

func TestMemoryStore_AppendDecisionDeferStaysOnAgenda(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s, AgendaItem{ID: "i1", Status: AgendaQueued})
	got, err := s.AppendDecision(context.Background(), w.ID, WalkDecision{
		AgendaItemID: "i1",
		Kind:         DecisionDefer,
		DecidedAt:    time.Now(),
		DecidedBy:    "user",
	})
	if err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	if got.Agenda[0].Status != AgendaDeferred {
		t.Errorf("defer kind should mark item deferred; got %s", got.Agenda[0].Status)
	}
}

func TestMemoryStore_AppendDecisionRequiresKnownItem(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	_, err := s.AppendDecision(context.Background(), w.ID, WalkDecision{
		AgendaItemID: "missing",
		Kind:         DecisionRatify,
		DecidedAt:    time.Now(),
		DecidedBy:    "user",
	})
	if err == nil {
		t.Error("expected error when decision references missing item")
	}
}

// ── Cost ───────────────────────────────────────────────────────

func TestMemoryStore_AddCostAccumulates(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	_, _ = s.AddCost(context.Background(), w.ID, Cost{TokensIn: 5, Dollars: 0.01})
	got, _ := s.AddCost(context.Background(), w.ID, Cost{TokensIn: 10, Dollars: 0.02})
	if got.Cost.TokensIn != 15 || got.Cost.Dollars < 0.029 {
		t.Errorf("Cost not accumulated: %+v", got.Cost)
	}
}

// ── Pause / Resume ─────────────────────────────────────────────

func TestMemoryStore_PauseResumeCycle(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	got, err := s.Pause(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if got.Status != StatusPaused {
		t.Errorf("status after Pause = %s, want paused", got.Status)
	}
	got, err = s.Resume(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got.Status != StatusActive {
		t.Errorf("status after Resume = %s, want active", got.Status)
	}
}

func TestMemoryStore_TerminalRefusesMutation(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	if _, err := s.End(context.Background(), w.ID, StatusCompleted, time.Now()); err != nil {
		t.Fatalf("End: %v", err)
	}
	cases := map[string]func() error{
		"AddAgendaItem":          func() error { _, e := s.AddAgendaItem(context.Background(), w.ID, AgendaItem{ID: "x", Status: AgendaQueued}); return e },
		"UpdateAgendaItemStatus": func() error { _, e := s.UpdateAgendaItemStatus(context.Background(), w.ID, "x", AgendaQueued); return e },
		"AppendTurn":             func() error { _, e := s.AppendTurn(context.Background(), w.ID, WalkTurn{Content: "x"}); return e },
		"Pause":                  func() error { _, e := s.Pause(context.Background(), w.ID); return e },
		"Resume":                 func() error { _, e := s.Resume(context.Background(), w.ID); return e },
		"End again":              func() error { _, e := s.End(context.Background(), w.ID, StatusCompleted, time.Now()); return e },
	}
	for name, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s on terminal walk should fail", name)
		}
	}
}

// ── concurrency smoke ──────────────────────────────────────────

func TestMemoryStore_ConcurrentAppendsSafe(t *testing.T) {
	s := newTestStore()
	w := startWalk(t, s)
	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = s.AppendTurn(context.Background(), w.ID, WalkTurn{
				Speaker: SpeakerUser,
				Content: fmt.Sprintf("t%d", i),
			})
		}(i)
	}
	wg.Wait()
	got, _ := s.Get(context.Background(), w.ID)
	if len(got.Transcript) != N {
		t.Errorf("expected %d turns, got %d (race may have lost writes)", N, len(got.Transcript))
	}
}
