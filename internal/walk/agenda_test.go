package walk

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// Fake source clients — minimal, deterministic.

type fakeEscalations struct {
	rows []core.EscalationRequest
	err  error
}

func (f *fakeEscalations) ListOpenEscalations(_ context.Context, _ string) ([]core.EscalationRequest, error) {
	return f.rows, f.err
}

type fakeHITL struct {
	rows []HITLAgendaItem
	err  error
}

func (f *fakeHITL) ListSuspendedSessions(_ context.Context, _ string) ([]HITLAgendaItem, error) {
	return f.rows, f.err
}

type fakeGate struct {
	rows []GateFailureItem
	err  error
}

func (f *fakeGate) ListOpenGateFailures(_ context.Context, _ string) ([]GateFailureItem, error) {
	return f.rows, f.err
}

type fakeWorkItems struct {
	calls   []core.WorkItemFilter
	filed   []core.WorkItem
	closed  []core.WorkItem
	listErr error
}

func (f *fakeWorkItems) ListWorkItems(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
	f.calls = append(f.calls, filter)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(filter.StateCategory) == 0 {
		return nil, nil
	}
	switch filter.StateCategory[0] {
	case core.StateCompleted:
		return f.closed, nil
	default:
		return f.filed, nil
	}
}

// counterIDFor produces predictable agenda ids for assertions.
func counterIDFor() func(AgendaSourceKind, string) string {
	n := 0
	return func(kind AgendaSourceKind, ref string) string {
		n++
		return fmt.Sprintf("%s-%d", kind, n)
	}
}

func mkOpts(now time.Time) AggregateOptions {
	return AggregateOptions{
		Now:          func() time.Time { return now },
		RecentWindow: 24 * time.Hour,
		PerSourceCap: 25,
		Workspace:    "ws-default",
		IDFor:        counterIDFor(),
	}
}

// ── nil-safe sources ────────────────────────────────────────────

func TestBuildAgenda_AllSourcesNilReturnsEmpty(t *testing.T) {
	got, err := BuildAgenda(context.Background(), Sources{}, mkOpts(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty agenda, got %d items", len(got))
	}
}

// ── escalation source ───────────────────────────────────────────

func TestBuildAgenda_EscalationsBlockingFirst(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	srcs := Sources{
		Escalations: &fakeEscalations{
			rows: []core.EscalationRequest{
				{ID: "e-advisory-old", Title: "advisory old", Urgency: core.UrgencyAdvisory, CreatedAt: now.Add(-3 * time.Hour)},
				{ID: "e-blocking-new", Title: "blocking new", Urgency: core.UrgencyBlocking, CreatedAt: now.Add(-time.Hour)},
				{ID: "e-blocking-old", Title: "blocking old", Urgency: core.UrgencyBlocking, CreatedAt: now.Add(-2 * time.Hour)},
			},
		},
	}
	got, err := BuildAgenda(context.Background(), srcs, mkOpts(now))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d: %+v", len(got), got)
	}
	if got[0].Source.Ref != "e-blocking-old" {
		t.Errorf("blocking-old should sort first; got %q", got[0].Source.Ref)
	}
	if got[1].Source.Ref != "e-blocking-new" {
		t.Errorf("blocking-new second; got %q", got[1].Source.Ref)
	}
	if got[2].Source.Ref != "e-advisory-old" {
		t.Errorf("advisory last; got %q", got[2].Source.Ref)
	}
	// All from escalation source.
	for _, a := range got {
		if a.Source.Kind != SourceEscalation {
			t.Errorf("expected SourceEscalation, got %q", a.Source.Kind)
		}
		if !a.IsQueued() {
			t.Errorf("new agenda items must be queued; got %s", a.Status)
		}
	}
}

func TestBuildAgenda_EscalationErrorsBubbleUp(t *testing.T) {
	srcs := Sources{Escalations: &fakeEscalations{err: errors.New("boom")}}
	_, err := BuildAgenda(context.Background(), srcs, mkOpts(time.Now()))
	if err == nil {
		t.Fatal("expected error to bubble up")
	}
}

// ── HITL source ─────────────────────────────────────────────────

func TestBuildAgenda_HITLSortedByAskedAt(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	srcs := Sources{
		HITL: &fakeHITL{
			rows: []HITLAgendaItem{
				{SessionID: "s2", Question: "newer", AskedAt: now.Add(-time.Hour)},
				{SessionID: "s1", Question: "older", AskedAt: now.Add(-3 * time.Hour)},
			},
		},
	}
	got, err := BuildAgenda(context.Background(), srcs, mkOpts(now))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 items; got %d", len(got))
	}
	if got[0].Source.Ref != "s1" {
		t.Errorf("oldest HITL first; got %q", got[0].Source.Ref)
	}
	if got[0].Source.Kind != SourceHITL {
		t.Errorf("kind = %q, want hitl", got[0].Source.Kind)
	}
}

// ── gate failure source ─────────────────────────────────────────

func TestBuildAgenda_GateFailureFallsBackToKindTopic(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	srcs := Sources{
		GateFailures: &fakeGate{
			rows: []GateFailureItem{
				{GateID: "g1", Kind: "qa", FailedAt: now.Add(-time.Hour)},
			},
		},
	}
	got, _ := BuildAgenda(context.Background(), srcs, mkOpts(now))
	if len(got) != 1 || got[0].Topic != "qa gate failure" {
		t.Errorf("expected synthesised qa topic; got %+v", got)
	}
}

// ── recent beads source: filed + closed ─────────────────────────

func TestBuildAgenda_RecentBeadsSplitByStateCategory(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	wi := &fakeWorkItems{
		filed: []core.WorkItem{
			{ID: "gm-1", Title: "filed bead", UpdatedAt: now.Add(-time.Hour)},
		},
		closed: []core.WorkItem{
			{ID: "gm-2", Title: "closed bead", UpdatedAt: now.Add(-2 * time.Hour)},
		},
	}
	srcs := Sources{WorkItems: wi}
	got, err := BuildAgenda(context.Background(), srcs, mkOpts(now))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 items; got %d: %+v", len(got), got)
	}
	if got[0].Source.Kind != SourceBeadFiled || got[0].Source.Ref != "gm-1" {
		t.Errorf("first item should be bead_filed gm-1; got %+v", got[0])
	}
	if got[1].Source.Kind != SourceBeadClosed || got[1].Source.Ref != "gm-2" {
		t.Errorf("second item should be bead_closed gm-2; got %+v", got[1])
	}
	// Two distinct ListWorkItems calls: one for filed, one for closed.
	if len(wi.calls) != 2 {
		t.Fatalf("expected 2 ListWorkItems calls, got %d", len(wi.calls))
	}
	if wi.calls[1].StateCategory[0] != core.StateCompleted {
		t.Errorf("second call should filter on completed; got %v", wi.calls[1].StateCategory)
	}
}

func TestBuildAgenda_RecentBeadsHonoursWindow(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	wi := &fakeWorkItems{}
	srcs := Sources{WorkItems: wi}
	opts := mkOpts(now)
	opts.RecentWindow = 6 * time.Hour
	_, _ = BuildAgenda(context.Background(), srcs, opts)
	if len(wi.calls) == 0 {
		t.Fatal("expected at least one ListWorkItems call")
	}
	cutoff := *wi.calls[0].UpdatedSince
	want := now.Add(-6 * time.Hour)
	if !cutoff.Equal(want) {
		t.Errorf("cutoff = %s, want %s", cutoff, want)
	}
}

// ── per-source cap ──────────────────────────────────────────────

func TestBuildAgenda_PerSourceCapApplies(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	rows := make([]core.EscalationRequest, 30)
	for i := range rows {
		rows[i] = core.EscalationRequest{
			ID:        fmt.Sprintf("e-%02d", i),
			Title:     fmt.Sprintf("esc %d", i),
			Urgency:   core.UrgencyAdvisory,
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
		}
	}
	srcs := Sources{Escalations: &fakeEscalations{rows: rows}}
	opts := mkOpts(now)
	opts.PerSourceCap = 5
	got, _ := BuildAgenda(context.Background(), srcs, opts)
	if len(got) != 5 {
		t.Errorf("cap = 5, got %d items", len(got))
	}
}

// ── dedup across sources ────────────────────────────────────────

func TestBuildAgenda_DedupesByKindAndRef(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	// Two escalations with the SAME id (degenerate but possible
	// across paginated reads). Aggregator must dedupe.
	srcs := Sources{
		Escalations: &fakeEscalations{
			rows: []core.EscalationRequest{
				{ID: "e1", Title: "first", Urgency: core.UrgencyBlocking, CreatedAt: now.Add(-time.Hour)},
				{ID: "e1", Title: "dup", Urgency: core.UrgencyBlocking, CreatedAt: now.Add(-time.Hour)},
			},
		},
	}
	got, _ := BuildAgenda(context.Background(), srcs, mkOpts(now))
	if len(got) != 1 {
		t.Errorf("dedup: expected 1 item, got %d", len(got))
	}
}

// ── default id helper ───────────────────────────────────────────

func TestDefaultAgendaItemID_StableForSameInput(t *testing.T) {
	a := defaultAgendaItemID(SourceEscalation, "abc")
	b := defaultAgendaItemID(SourceEscalation, "abc")
	if a != b {
		t.Errorf("expected stable id; got %q vs %q", a, b)
	}
}

func TestDefaultAgendaItemID_RandomFallbackForEmptyRef(t *testing.T) {
	a := defaultAgendaItemID(SourceUser, "")
	b := defaultAgendaItemID(SourceUser, "")
	if a == b {
		t.Errorf("empty ref should produce distinct ids; both %q", a)
	}
}
