package native

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/core"
)

func TestParallelChangedEventShape(t *testing.T) {
	ev := parallelChanged("%42", "tmux:gm-abc.1:1714169123456789", "claude", 2, 3, 1)
	if ev.Kind != EventKindSessionParallelChanged {
		t.Fatalf("kind: got %q", ev.Kind)
	}
	if ev.SessionID == "" {
		t.Fatal("session id missing")
	}
	for _, k := range []string{"pane_id", "session_id", "agent_type", "in_flight", "max_parallel", "delta"} {
		if _, ok := ev.Payload[k]; !ok {
			t.Errorf("payload key %q missing", k)
		}
	}
	if got := ev.Payload["in_flight"].(int); got != 2 {
		t.Errorf("in_flight: got %v", got)
	}
}

// TestParallelChangedFixtureMatchesEmitter is the contract-handshake
// test: the JSON fixture WS-B mocks against MUST stay shaped like what
// parallelChanged emits. Drift breaks the SPA pill consumer.
func TestParallelChangedFixtureMatchesEmitter(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "session_parallel_changed.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture core.OrchestrationEvent
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if fixture.Kind != EventKindSessionParallelChanged {
		t.Fatalf("fixture kind: %q", fixture.Kind)
	}
	emitted := parallelChanged("%42", fixture.SessionID, "claude", 2, 3, 1)
	for _, k := range []string{"pane_id", "session_id", "agent_type", "in_flight", "max_parallel", "delta"} {
		if _, ok := fixture.Payload[k]; !ok {
			t.Errorf("fixture missing payload key %q", k)
		}
		if _, ok := emitted.Payload[k]; !ok {
			t.Errorf("emitter missing payload key %q", k)
		}
	}
}

// parallelRegistry returns a registry where "claude" supports up to 2
// concurrent beads per session, used by the intra-parallelism dispatch
// tests.
func parallelRegistry() agents.Registry {
	return agents.Registry{
		Agents: []agents.AgentType{
			{
				Name: "claude", Binary: "claude",
				Preamble: agents.PreambleClaudeMD, Hooks: agents.HookClaudeCode,
				IntraParallel: true, MaxParallel: 2,
			},
		},
	}
}

func TestStartSessionReusePaneSharesPaneAndCounts(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("first StartSession: %v", err)
	}
	pane1, _ := first.ProviderMetadata["pane_id"].(string)
	if pane1 == "" {
		t.Fatal("first session missing pane_id")
	}
	if got := p.PaneInFlight(pane1); got != 1 {
		t.Errorf("first PaneInFlight: want 1 got %d", got)
	}
	if len(fb.spawnCalls) != 1 {
		t.Errorf("first spawn count: want 1 got %d", len(fb.spawnCalls))
	}

	// Second bead reuses the pane.
	second, err := p.StartSession(context.Background(), "a2",
		freshPrompt("gm-bar", map[string]any{extKeyReusePaneID: pane1}))
	if err != nil {
		t.Fatalf("second StartSession (reuse): %v", err)
	}
	pane2, _ := second.ProviderMetadata["pane_id"].(string)
	if pane2 != pane1 {
		t.Errorf("reuse pane_id: want %q got %q", pane1, pane2)
	}
	if got := p.PaneInFlight(pane1); got != 2 {
		t.Errorf("after-reuse PaneInFlight: want 2 got %d", got)
	}
	if len(fb.spawnCalls) != 1 {
		t.Errorf("reuse should NOT spawn a new pane; got %d spawn calls", len(fb.spawnCalls))
	}
	if reused, _ := second.ProviderMetadata["reuse_pane"].(bool); !reused {
		t.Error("reuse_pane metadata flag missing on second session")
	}
}

func TestStartSessionReuseEnforcesMaxParallel(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-1", nil))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	paneID, _ := first.ProviderMetadata["pane_id"].(string)

	if _, err := p.StartSession(context.Background(), "a2",
		freshPrompt("gm-2", map[string]any{extKeyReusePaneID: paneID})); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Third should be refused — max_parallel=2.
	_, err = p.StartSession(context.Background(), "a3",
		freshPrompt("gm-3", map[string]any{extKeyReusePaneID: paneID}))
	if err == nil {
		t.Fatal("third reuse should be refused at cap")
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T: %v", err, err)
	}
	if got := p.PaneInFlight(paneID); got != 2 {
		t.Errorf("PaneInFlight after refused dispatch: want 2 got %d", got)
	}
}

func TestStartSessionReuseUnknownPaneRejected(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	_, err := p.StartSession(context.Background(), "a1",
		freshPrompt("gm-1", map[string]any{extKeyReusePaneID: "%nope"}))
	if err == nil {
		t.Fatal("reuse of unknown pane should fail")
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T: %v", err, err)
	}
}

func TestStartSessionEmitsParallelChanged(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := p.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	collected := make(chan core.OrchestrationEvent, 8)
	go func() {
		for ev := range events {
			if ev.Kind == EventKindSessionParallelChanged {
				collected <- ev
			}
		}
	}()

	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-1", nil))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	paneID, _ := first.ProviderMetadata["pane_id"].(string)

	if _, err := p.StartSession(context.Background(), "a2",
		freshPrompt("gm-2", map[string]any{extKeyReusePaneID: paneID})); err != nil {
		t.Fatalf("second: %v", err)
	}

	// Drain two +1 events.
	for i := 1; i <= 2; i++ {
		ev := mustReceive(t, collected)
		if got := ev.Payload["in_flight"]; got != i {
			t.Errorf("event %d in_flight: want %d got %v", i, i, got)
		}
		if got := ev.Payload["delta"]; got != 1 {
			t.Errorf("event %d delta: want 1 got %v", i, got)
		}
		if got, _ := ev.Payload["pane_id"].(string); got != paneID {
			t.Errorf("event %d pane_id: want %q got %q", i, paneID, got)
		}
	}
}

// TestThreeBeadsTwoSessionsRouting is the DoD fixture (gm-root.16.3):
// max_parallel=2, three ready beads. The first two co-locate in pane A;
// the third overflows to pane B. The event stream contains exactly
// three +1 deltas, two on pane A (in_flight 1, 2) and one on pane B
// (in_flight 1).
//
// The dispatcher policy lives outside this adaptor; the test models
// the policy inline (try reuse first, on cap-exceeded fall back to
// fresh spawn) to prove the OrchestrationPlane primitives are
// sufficient for a router to implement it.
func TestThreeBeadsTwoSessionsRouting(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := p.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	collected := make(chan core.OrchestrationEvent, 16)
	go func() {
		for ev := range events {
			if ev.Kind == EventKindSessionParallelChanged {
				collected <- ev
			}
		}
	}()

	dispatch := func(beadID string, candidatePane string) (core.Session, error) {
		extras := map[string]any{}
		if candidatePane != "" {
			extras[extKeyReusePaneID] = candidatePane
		}
		s, err := p.StartSession(context.Background(), "a-"+beadID, freshPrompt(beadID, extras))
		if err != nil && candidatePane != "" {
			// Cap exceeded — fall back to fresh spawn.
			s, err = p.StartSession(context.Background(), "a-"+beadID, freshPrompt(beadID, nil))
		}
		return s, err
	}

	s1, err := dispatch("gm-1", "")
	if err != nil {
		t.Fatalf("bead 1: %v", err)
	}
	paneA, _ := s1.ProviderMetadata["pane_id"].(string)

	s2, err := dispatch("gm-2", paneA)
	if err != nil {
		t.Fatalf("bead 2: %v", err)
	}
	if got, _ := s2.ProviderMetadata["pane_id"].(string); got != paneA {
		t.Errorf("bead 2 should co-locate in pane A; got %q", got)
	}

	s3, err := dispatch("gm-3", paneA)
	if err != nil {
		t.Fatalf("bead 3: %v", err)
	}
	paneB, _ := s3.ProviderMetadata["pane_id"].(string)
	if paneB == paneA {
		t.Error("bead 3 should overflow to a new pane, but stayed in pane A")
	}

	if got := p.PaneInFlight(paneA); got != 2 {
		t.Errorf("pane A in_flight: want 2 got %d", got)
	}
	if got := p.PaneInFlight(paneB); got != 1 {
		t.Errorf("pane B in_flight: want 1 got %d", got)
	}
	if len(fb.spawnCalls) != 2 {
		t.Errorf("spawn calls: want 2 (panes A + B), got %d", len(fb.spawnCalls))
	}

	// Three +1 events expected: paneA→1, paneA→2, paneB→1.
	type tick struct {
		pane     string
		inFlight int
	}
	var ticks []tick
	for i := 0; i < 3; i++ {
		ev := mustReceive(t, collected)
		ticks = append(ticks, tick{
			pane:     ev.Payload["pane_id"].(string),
			inFlight: ev.Payload["in_flight"].(int),
		})
	}
	want := []tick{
		{paneA, 1},
		{paneA, 2},
		{paneB, 1},
	}
	for i, w := range want {
		if ticks[i] != w {
			t.Errorf("tick %d: want %+v got %+v", i, w, ticks[i])
		}
	}
}

func TestEndSessionPreservesPaneWhenSiblingsRemain(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	paneID, _ := first.ProviderMetadata["pane_id"].(string)
	second, err := p.StartSession(context.Background(), "a2",
		freshPrompt("gm-2", map[string]any{extKeyReusePaneID: paneID}))
	if err != nil {
		t.Fatal(err)
	}

	// End the first session. Pane MUST stay alive for the second.
	if _, err := p.EndSession(context.Background(), first.ID, core.SessionEndCompleted, ""); err != nil {
		t.Fatalf("end first: %v", err)
	}
	if len(fb.killCalls) != 0 {
		t.Errorf("pane killed prematurely while sibling still active: %v", fb.killCalls)
	}
	if got := p.PaneInFlight(paneID); got != 1 {
		t.Errorf("PaneInFlight after first end: want 1 got %d", got)
	}

	// End the second — now the pane should tear down.
	if _, err := p.EndSession(context.Background(), second.ID, core.SessionEndCompleted, ""); err != nil {
		t.Fatalf("end second: %v", err)
	}
	if len(fb.killCalls) != 1 {
		t.Errorf("last-out pane teardown didn't kill: %v", fb.killCalls)
	}
	if got := p.PaneInFlight(paneID); got != 0 {
		t.Errorf("PaneInFlight after last end: want 0 got %d", got)
	}
}

func TestEndSessionEmitsParallelChangedDecrement(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: parallelRegistry(), RepoRoot: repo})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := p.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	collected := make(chan core.OrchestrationEvent, 8)
	go func() {
		for ev := range events {
			if ev.Kind == EventKindSessionParallelChanged {
				collected <- ev
			}
		}
	}()

	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	mustReceive(t, collected) // +1 from start

	if _, err := p.EndSession(context.Background(), first.ID, core.SessionEndCompleted, ""); err != nil {
		t.Fatal(err)
	}
	ev := mustReceive(t, collected)
	if got := ev.Payload["delta"]; got != -1 {
		t.Errorf("end delta: want -1 got %v", got)
	}
	if got := ev.Payload["in_flight"]; got != 0 {
		t.Errorf("end in_flight: want 0 got %v", got)
	}
}

func mustReceive(t *testing.T, ch <-chan core.OrchestrationEvent) core.OrchestrationEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before event arrived")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for parallel_changed event")
		return core.OrchestrationEvent{}
	}
}
