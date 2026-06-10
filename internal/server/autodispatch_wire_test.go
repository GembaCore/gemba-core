package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/autodispatch"
)

// fakeOrchestration is a minimal stub satisfying the slice of
// OrchestrationPlaneAdaptor the wire actually consumes:
// ListSessions, StartSession, RecycleSession.
type fakeOrchestration struct {
	core.OrchestrationPlaneAdaptor
	sessions       []core.Session
	listErr        error
	startErr       error
	startCalls     []startCall
	recycleErr     error
	recycleCalls   []string
	recycleSession core.Session
}

type startCall struct {
	assignmentID string
	prompt       core.SessionPrompt
}

func (f *fakeOrchestration) ListSessions(_ context.Context, filter core.SessionFilter) ([]core.Session, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(filter.Status) == 0 {
		return f.sessions, nil
	}
	out := []core.Session{}
	for _, s := range f.sessions {
		for _, want := range filter.Status {
			if s.Status == want {
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeOrchestration) StartSession(_ context.Context, assignmentID string, p core.SessionPrompt) (core.Session, error) {
	f.startCalls = append(f.startCalls, startCall{assignmentID: assignmentID, prompt: p})
	if f.startErr != nil {
		return core.Session{}, f.startErr
	}
	return core.Session{ID: "new-" + assignmentID}, nil
}

func (f *fakeOrchestration) RecycleSession(_ context.Context, sessionID string) (core.Session, error) {
	f.recycleCalls = append(f.recycleCalls, sessionID)
	if f.recycleErr != nil {
		return core.Session{}, f.recycleErr
	}
	return f.recycleSession, nil
}

// fakeWorkPlane returns the configured items on ListWorkItems, ignores
// any filter beyond StateCategory matching.
type fakeWorkPlane struct {
	core.WorkPlane
	items   []core.WorkItem
	listErr error
}

func (f *fakeWorkPlane) ListWorkItems(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(filter.StateCategory) == 0 {
		return f.items, nil
	}
	out := []core.WorkItem{}
	for _, it := range f.items {
		for _, want := range filter.StateCategory {
			if it.StateCategory == want {
				out = append(out, it)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeWorkPlane) GetWorkItem(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
	for _, it := range f.items {
		if it.ID == id {
			return it, nil
		}
	}
	return core.WorkItem{}, core.ErrNotFound
}

// ── IdleSessionLister ──

func TestIdleLister_FiltersByPersona(t *testing.T) {
	op := &fakeOrchestration{
		sessions: []core.Session{
			{ID: "a", Status: core.SessionReady, Persona: "engineer-claude"},
			{ID: "b", Status: core.SessionReady, Persona: "pm-claude"},
			{ID: "c", Status: core.SessionWorking, Persona: "engineer-claude"},
		},
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	got, err := w.IdleLister().ListIdle(context.Background())
	if err != nil {
		t.Fatalf("ListIdle: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 idle, got %d", len(got))
	}
	if got[0].Session.ID != "a" {
		t.Errorf("expected sess a; got %s", got[0].Session.ID)
	}
}

func TestIdleLister_SkipsCodexReadySessionsForColdStart(t *testing.T) {
	op := &fakeOrchestration{
		sessions: []core.Session{
			{ID: "a", Status: core.SessionReady, Persona: "engineer-codex"},
		},
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-codex"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "codex")
	got, err := w.IdleLister().ListIdle(context.Background())
	if err != nil {
		t.Fatalf("ListIdle: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("codex ready sessions should not be reused; got %+v", got)
	}
}

// ── LiveSessionLister ──

func TestLiveLister_AcrossAllPersonas(t *testing.T) {
	op := &fakeOrchestration{
		sessions: []core.Session{
			{ID: "x", Status: core.SessionWorking, Persona: "engineer-claude"},
			{ID: "y", Status: core.SessionPrompting, Persona: "pm-claude"}, // different persona — still wanted
			{ID: "z", Status: core.SessionReady, Persona: "engineer-claude"},
		},
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	got, err := w.LiveLister().ListLive(context.Background())
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 live (across personas), got %d", len(got))
	}
}

// ── ReadySetReader ──

func TestReadyReader_AppliesPersonaCascade(t *testing.T) {
	cfg := config.PoolConfig{
		DefaultPersona: "engineer-claude",
		Routing:        map[string]string{"epic": "pm-claude"},
	}
	wp := &fakeWorkPlane{
		items: []core.WorkItem{
			{ID: "gm-1", Kind: "bug", StateCategory: core.StateUnstarted},
			{ID: "gm-2", Kind: "epic", StateCategory: core.StateUnstarted}, // routes to pm-claude (excluded)
			{ID: "gm-3", Kind: "bug", StateCategory: core.StateUnstarted,
				Custom: map[string]any{"persona": "pm-claude"}}, // bead override → pm-claude (excluded)
			{ID: "gm-4", Kind: "feature", StateCategory: core.StateUnstarted}, // default → engineer-claude (kept)
		},
	}
	w := NewPoolWiring(nil, wp,
		config.ResolvedPool{Persona: "engineer-claude"},
		cfg, planner.OperationalContextReaders{}, "claude")
	beads, err := w.ReadyReader().ReadySet(context.Background())
	if err != nil {
		t.Fatalf("ReadySet: %v", err)
	}
	want := map[core.WorkItemID]bool{"gm-1": true, "gm-4": true}
	if len(beads) != len(want) {
		t.Fatalf("got %d beads, want %d", len(beads), len(want))
	}
	for _, b := range beads {
		if !want[b.BeadID] {
			t.Errorf("unexpected bead %q (should not have routed to engineer-claude)", b.BeadID)
		}
	}
}

func TestReadyReader_DropsSoftBlocked(t *testing.T) {
	cfg := config.PoolConfig{DefaultPersona: "engineer-claude"}
	wp := &fakeWorkPlane{
		items: []core.WorkItem{
			{ID: "gm-1", Kind: "bug", StateCategory: core.StateUnstarted, DispatchStatus: core.DispatchAwaitingDesign},
			{ID: "gm-2", Kind: "bug", StateCategory: core.StateUnstarted},
		},
	}
	w := NewPoolWiring(nil, wp,
		config.ResolvedPool{Persona: "engineer-claude"},
		cfg, planner.OperationalContextReaders{}, "claude")
	beads, _ := w.ReadyReader().ReadySet(context.Background())
	if len(beads) != 1 || beads[0].BeadID != "gm-2" {
		t.Errorf("soft-blocked bead should be dropped; got %+v", beads)
	}
}

func TestReadyReader_DropsNonRunnableAndBlocked(t *testing.T) {
	cfg := config.PoolConfig{DefaultPersona: "engineer-claude"}
	wp := &fakeWorkPlane{
		items: []core.WorkItem{
			{ID: "gm-m1", Kind: core.KindMilestone, StateCategory: core.StateUnstarted},
			{ID: "gm-e1", Kind: "epic", StateCategory: core.StateUnstarted},
			{ID: "gm-blocker", Kind: "task", StateCategory: core.StateUnstarted},
			{ID: "gm-blocked", Kind: "task", StateCategory: core.StateUnstarted,
				Relationships: []core.Relationship{{Kind: core.RelBlocks, From: "gm-blocker", To: "gm-blocked"}}},
			{ID: "gm-ready", Kind: "task", StateCategory: core.StateUnstarted},
		},
	}
	w := NewPoolWiring(nil, wp,
		config.ResolvedPool{Persona: "engineer-claude"},
		cfg, planner.OperationalContextReaders{}, "claude")
	beads, err := w.ReadyReader().ReadySet(context.Background())
	if err != nil {
		t.Fatalf("ReadySet: %v", err)
	}
	want := map[core.WorkItemID]bool{"gm-blocker": true, "gm-ready": true}
	if len(beads) != len(want) {
		t.Fatalf("got %d beads, want %d: %+v", len(beads), len(want), beads)
	}
	for _, b := range beads {
		if !want[b.BeadID] {
			t.Errorf("unexpected bead %q", b.BeadID)
		}
	}
}

func TestReadyReader_NoPersonaResolutionDrops(t *testing.T) {
	cfg := config.PoolConfig{} // no default, no routing
	wp := &fakeWorkPlane{
		items: []core.WorkItem{
			{ID: "gm-1", Kind: "bug", StateCategory: core.StateUnstarted},
		},
	}
	w := NewPoolWiring(nil, wp,
		config.ResolvedPool{Persona: "engineer-claude"},
		cfg, planner.OperationalContextReaders{}, "claude")
	beads, _ := w.ReadyReader().ReadySet(context.Background())
	if len(beads) != 0 {
		t.Errorf("unrouted bead should be dropped; got %+v", beads)
	}
}

// ── SessionDispatcher ──

func TestDispatcher_StartsSessionWithReusePane(t *testing.T) {
	op := &fakeOrchestration{
		sessions: []core.Session{
			{ID: "sess-1", Status: core.SessionReady, Persona: "engineer-claude",
				ProviderMetadata: map[string]any{"pane_id": "pane-77"}},
		},
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	if err := w.Dispatcher().Dispatch(context.Background(), "sess-1", "gm-9"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(op.startCalls) != 1 {
		t.Fatalf("want 1 StartSession call, got %d", len(op.startCalls))
	}
	c := op.startCalls[0]
	if c.assignmentID != "gm-9" {
		t.Errorf("assignmentID = %q, want gm-9", c.assignmentID)
	}
	if c.prompt.Extension["gemba:reuse_pane_id"] != "pane-77" {
		t.Errorf("reuse_pane_id = %v, want pane-77", c.prompt.Extension["gemba:reuse_pane_id"])
	}
	if c.prompt.Extension["gemba:autodispatch"] != "1" {
		t.Errorf("autodispatch flag missing")
	}
	if c.prompt.Extension["gemba:persona_id"] != "engineer-claude" {
		t.Errorf("persona_id = %v", c.prompt.Extension["gemba:persona_id"])
	}
	if c.prompt.Extension["gemba:agent_type"] != "claude" {
		t.Errorf("agent_type = %v", c.prompt.Extension["gemba:agent_type"])
	}
	if c.prompt.Extension["gemba:nonce"] == nil || c.prompt.Extension["gemba:nonce"] == "" {
		t.Errorf("nonce should be populated")
	}
}

func TestDispatcher_ColdStartsSessionWithoutReusePane(t *testing.T) {
	op := &fakeOrchestration{}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-codex"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "codex")
	sessionID, err := w.Dispatcher().(autodispatch.ColdStarter).StartCold(context.Background(), "gm-10")
	if err != nil {
		t.Fatalf("StartCold: %v", err)
	}
	if sessionID != "new-gm-10" {
		t.Errorf("sessionID = %q, want new-gm-10", sessionID)
	}
	if len(op.startCalls) != 1 {
		t.Fatalf("want 1 StartSession call, got %d", len(op.startCalls))
	}
	c := op.startCalls[0]
	if c.prompt.Extension["gemba:reuse_pane_id"] != nil {
		t.Errorf("reuse_pane_id should be absent on cold start")
	}
	if c.prompt.Extension["gemba:persona_id"] != "engineer-codex" {
		t.Errorf("persona_id = %v", c.prompt.Extension["gemba:persona_id"])
	}
	if c.prompt.Extension["gemba:agent_type"] != "codex" {
		t.Errorf("agent_type = %v", c.prompt.Extension["gemba:agent_type"])
	}
	if c.prompt.Extension["gemba:autodispatch"] != "1" {
		t.Errorf("autodispatch flag missing")
	}
	if c.prompt.Extension["gemba:nonce"] == nil || c.prompt.Extension["gemba:nonce"] == "" {
		t.Errorf("nonce should be populated")
	}
}

func TestDispatcher_NoPaneIsError(t *testing.T) {
	op := &fakeOrchestration{
		sessions: []core.Session{
			{ID: "sess-1", Status: core.SessionReady, Persona: "engineer-claude"},
		},
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	err := w.Dispatcher().Dispatch(context.Background(), "sess-1", "gm-9")
	if err == nil {
		t.Errorf("expected error when pane_id missing")
	}
}

// ── SessionRecycler ──

func TestRecycler_TolerateUnsupported(t *testing.T) {
	op := &fakeOrchestration{
		recycleErr: core.NewAdaptorError(core.KindUnsupported, "no recycle"),
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	err := w.Recycler().Recycle(context.Background(), "sess-1")
	if err != nil {
		t.Errorf("KindUnsupported should be tolerated; got %v", err)
	}
	if len(op.recycleCalls) != 1 {
		t.Errorf("recycle call should still happen")
	}
}

func TestRecycler_PropagatesOtherErrors(t *testing.T) {
	op := &fakeOrchestration{
		recycleErr: errors.New("dolt down"),
	}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	err := w.Recycler().Recycle(context.Background(), "sess-1")
	if err == nil {
		t.Errorf("non-Unsupported error should propagate")
	}
}

func TestRecycler_HappyPath(t *testing.T) {
	op := &fakeOrchestration{recycleSession: core.Session{ID: "new-sess"}}
	w := NewPoolWiring(op, nil,
		config.ResolvedPool{Persona: "engineer-claude"},
		config.PoolConfig{}, planner.OperationalContextReaders{}, "claude")
	if err := w.Recycler().Recycle(context.Background(), "sess-1"); err != nil {
		t.Errorf("recycle: %v", err)
	}
	if len(op.recycleCalls) != 1 || op.recycleCalls[0] != "sess-1" {
		t.Errorf("recycle calls = %v", op.recycleCalls)
	}
}

// Unused import guard for time (referenced in fakeOrchestration's
// shape via core.Session). The struct embeds core.Session which
// uses time; this var keeps the import used even if a future refactor
// removes a field reference.
var _ = time.Time{}
