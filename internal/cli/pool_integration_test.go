package cli

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/planner/autodispatch"
)

// fakeOpForIntegration implements the slice of OrchestrationPlaneAdaptor
// that the daemon's wiring actually consumes during Tick.
type fakeOpForIntegration struct {
	core.OrchestrationPlaneAdaptor
	mu          sync.Mutex
	sessions    []core.Session
	startCalls  []startCall
	startReturn core.Session
}

type startCall struct {
	assignmentID string
	prompt       core.SessionPrompt
}

func (f *fakeOpForIntegration) ListSessions(_ context.Context, filter core.SessionFilter) ([]core.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func (f *fakeOpForIntegration) StartSession(_ context.Context, assignmentID string, p core.SessionPrompt) (core.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls = append(f.startCalls, startCall{assignmentID: assignmentID, prompt: p})
	if f.startReturn.ID == "" {
		f.startReturn = core.Session{ID: "new-" + assignmentID}
	}
	return f.startReturn, nil
}

func (f *fakeOpForIntegration) RecycleSession(_ context.Context, _ string) (core.Session, error) {
	return core.Session{}, core.NewAdaptorError(core.KindUnsupported, "fake: no recycle")
}

type fakeWPForIntegration struct {
	core.WorkPlane
	items []core.WorkItem
}

func (f *fakeWPForIntegration) ListWorkItems(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
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

// TestPoolDaemon_FullDispatchPath wires the full chain: pool config →
// daemon → adapter wiring → (fake OrchestrationPlane + WorkPlane) →
// dispatch landed.
func TestPoolDaemon_FullDispatchPath(t *testing.T) {
	op := &fakeOpForIntegration{
		sessions: []core.Session{
			{
				ID:        "sess-idle",
				Status:    core.SessionReady,
				Persona:   "engineer-claude",
				StartedAt: time.Now(), // avoid epoch → huge time-on-task
				ProviderMetadata: map[string]any{
					"pane_id": "pane-42",
				},
			},
		},
	}
	wp := &fakeWPForIntegration{
		items: []core.WorkItem{
			{ID: "gm-1", Kind: "bug", StateCategory: core.StateUnstarted, Concepts: []string{"auth"}},
		},
	}
	resolved := config.ResolvedPool{
		Rig:           "test",
		Persona:       "engineer-claude",
		AgentType:     "claude",
		SizeEffective: 1,
		Floor:         0, // disabled — accept any score
	}
	poolCfg := config.PoolConfig{DefaultPersona: "engineer-claude"}

	daemon, err := buildDaemon(op, wp, resolved, poolCfg)
	if err != nil {
		t.Fatalf("buildDaemon: %v", err)
	}
	r := daemon.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(r.Actions))
	}
	if r.Actions[0].Outcome != autodispatch.OutcomeDispatched {
		t.Errorf("outcome = %q, want dispatched", r.Actions[0].Outcome)
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if len(op.startCalls) != 1 {
		t.Fatalf("StartSession calls = %d, want 1", len(op.startCalls))
	}
	c := op.startCalls[0]
	if c.assignmentID != "gm-1" {
		t.Errorf("assignmentID = %q", c.assignmentID)
	}
	if c.prompt.Extension["gemba:reuse_pane_id"] != "pane-42" {
		t.Errorf("reuse_pane_id missing or wrong: %v", c.prompt.Extension)
	}
	if c.prompt.Extension["gemba:autodispatch"] != "1" {
		t.Errorf("autodispatch flag missing")
	}
}

// TestPoolDaemon_RunHonorsContext confirms the daemon's Run loop
// terminates promptly when ctx cancels — required for graceful
// server shutdown.
func TestPoolDaemon_RunHonorsContext(t *testing.T) {
	op := &fakeOpForIntegration{}
	wp := &fakeWPForIntegration{}
	resolved := config.ResolvedPool{
		Rig: "t", Persona: "p", AgentType: "claude", SizeEffective: 1,
	}
	d, err := buildDaemon(op, wp, resolved, config.PoolConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, 50*time.Millisecond) }()
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop after ctx cancel")
	}
}
