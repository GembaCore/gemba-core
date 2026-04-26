package persona

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/events"
)

// newDispatcherForBridge spins a Dispatcher seeded with one running
// consult so deliverSkillOutput / FanFromHub have a consult ID to
// route into. Returns the dispatcher and the registered consult.
func newDispatcherForBridge(t *testing.T) (*Dispatcher, *Consult) {
	t.Helper()
	log := NewAuditLog(t.TempDir())
	d := NewDispatcher(log,
		WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
		WithIDFunc(func() string { return "consult-test-1" }),
		WithWorkspaceDir(t.TempDir()),
	)
	skill := fakeSkill{id: "test-skill"}
	persona := &corepersona.Persona{
		ID:           "test-persona",
		Name:         "Test",
		Role:         "Tester",
		Variety:      corepersona.VarietyCoach,
		Scope:        corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:       []string{"test-skill"},
		SystemPrompt: "you are {{role}}",
	}
	c, err := d.Begin(BeginRequest{
		Persona:   persona,
		Skill:     skill,
		Workspace: "test-ws",
		RawInput:  json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return d, c
}

func TestDeliverSkillOutput_RoutesLinesIntoDispatcher(t *testing.T) {
	d, c := newDispatcherForBridge(t)
	ev := events.GembaEvent{
		ID:        "ev-1",
		Kind:      events.SkillOutputEmitted,
		SessionID: c.ID,
		Payload: map[string]any{
			"skill_id":   "test-skill",
			"line_count": 2,
			"lines": []any{
				json.RawMessage(`{"type":"strategy","note":"a"}`),
				json.RawMessage(`{"type":"recommendation","id":"gm-1"}`),
			},
		},
	}
	deliverSkillOutput(d, ev)

	got, ok := d.Get(c.ID)
	if !ok {
		t.Fatal("consult vanished after deliver")
	}
	if len(got.ValidatedLines) != 2 {
		t.Errorf("ValidatedLines = %d, want 2", len(got.ValidatedLines))
	}
	if len(got.LineErrors) != 0 {
		t.Errorf("LineErrors = %v; want zero (fakeSkill validates everything)", got.LineErrors)
	}
}

func TestDeliverSkillOutput_AcceptsLinesSerializedAsAnyMaps(t *testing.T) {
	// After a JSON round-trip through SSE the `lines` slot may arrive
	// as []any of map[string]any rather than []json.RawMessage. The
	// extractor must re-marshal each element so the dispatcher sees
	// a stable JSON envelope.
	d, c := newDispatcherForBridge(t)
	ev := events.GembaEvent{
		ID:        "ev-2",
		Kind:      events.SkillOutputEmitted,
		SessionID: c.ID,
		Payload: map[string]any{
			"skill_id": "test-skill",
			"lines": []any{
				map[string]any{"type": "strategy", "n": 1},
				map[string]any{"type": "summary", "n": 2},
			},
		},
	}
	deliverSkillOutput(d, ev)
	got, _ := d.Get(c.ID)
	if len(got.ValidatedLines) != 2 {
		t.Errorf("ValidatedLines = %d, want 2 (map roundtrip)", len(got.ValidatedLines))
	}
}

func TestDeliverSkillOutput_DropsEmptySessionID(t *testing.T) {
	d, c := newDispatcherForBridge(t)
	ev := events.GembaEvent{
		ID:        "ev-3",
		Kind:      events.SkillOutputEmitted,
		SessionID: "",
		Payload: map[string]any{
			"lines": []any{json.RawMessage(`{"a":1}`)},
		},
	}
	deliverSkillOutput(d, ev)
	got, _ := d.Get(c.ID)
	if len(got.ValidatedLines) != 0 {
		t.Errorf("empty SessionID should not affect any consult; got %d lines on %s", len(got.ValidatedLines), c.ID)
	}
}

func TestDeliverSkillOutput_DropsUnknownConsultID(t *testing.T) {
	d, c := newDispatcherForBridge(t)
	ev := events.GembaEvent{
		ID:        "ev-4",
		Kind:      events.SkillOutputEmitted,
		SessionID: "consult-not-registered",
		Payload: map[string]any{
			"lines": []any{json.RawMessage(`{"a":1}`)},
		},
	}
	deliverSkillOutput(d, ev) // must not panic / mutate
	got, _ := d.Get(c.ID)
	if len(got.ValidatedLines) != 0 {
		t.Errorf("unknown SessionID leaked into another consult; got %d lines", len(got.ValidatedLines))
	}
}

func TestDeliverSkillOutput_DropsMalformedPayload(t *testing.T) {
	d, c := newDispatcherForBridge(t)
	for name, payload := range map[string]map[string]any{
		"missing-lines":   {"skill_id": "x"},
		"non-array-lines": {"lines": "this should be an array"},
	} {
		t.Run(name, func(t *testing.T) {
			ev := events.GembaEvent{
				ID: "ev-malformed", Kind: events.SkillOutputEmitted,
				SessionID: c.ID, Payload: payload,
			}
			deliverSkillOutput(d, ev)
			got, _ := d.Get(c.ID)
			if len(got.ValidatedLines) != 0 || len(got.LineErrors) != 0 {
				t.Errorf("%s payload mutated consult: lines=%d errors=%d",
					name, len(got.ValidatedLines), len(got.LineErrors))
			}
		})
	}
}

func TestFanFromHub_DeliversThroughTheRealHub(t *testing.T) {
	d, c := newDispatcherForBridge(t)
	hub := events.NewHub(events.Config{})
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		FanFromHub(ctx, d, hub)
		close(done)
	}()

	// Wait for FanFromHub's Subscribe to land — Publish before
	// Subscribe is dropped silently by the hub, so racing the
	// goroutine spin-up would flake the test.
	for i := 0; i < 100 && hub.SubscriberCount() == 0; i++ {
		time.Sleep(time.Millisecond)
	}

	hub.Publish(events.GembaEvent{
		ID:        "ev-hub-1",
		Kind:      events.SkillOutputEmitted,
		SessionID: c.ID,
		Payload: map[string]any{
			"skill_id": "test-skill",
			"lines":    []any{json.RawMessage(`{"type":"strategy"}`)},
		},
	})

	// Poll for delivery — Subscribe is buffered, Publish is best-
	// effort async.
	deadline := time.Now().Add(time.Second)
	for {
		got, _ := d.Get(c.ID)
		if len(got.ValidatedLines) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("FanFromHub did not deliver event within 1s")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	<-done
}

func TestFanFromHub_NilDispatcherOrHubNoOps(t *testing.T) {
	// Defensive: serve.go calls FanFromHub unconditionally inside a
	// nil-checked branch, but tests / pre-config-load Routers may
	// invoke with nil. Must not panic; must return promptly.
	done := make(chan struct{})
	go func() {
		FanFromHub(context.Background(), nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("FanFromHub on nil inputs hung instead of returning")
	}
}

func TestExtractLinesPayload_FormatVariants(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{
			name: "RawMessage-array",
			in: []json.RawMessage{
				json.RawMessage(`{"a":1}`),
				json.RawMessage(`{"b":2}`),
			},
			want: 2,
		},
		{
			name: "any-array-of-RawMessage",
			in: []any{
				json.RawMessage(`{"a":1}`),
				json.RawMessage(`{"b":2}`),
			},
			want: 2,
		},
		{
			name: "any-array-of-maps",
			in: []any{
				map[string]any{"a": 1},
				map[string]any{"b": 2},
			},
			want: 2,
		},
		{
			name: "single-RawMessage-array-blob",
			in:   json.RawMessage(`[{"a":1},{"b":2}]`),
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractLinesPayload(map[string]any{"lines": tc.in})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("len = %d, want %d", len(got), tc.want)
			}
		})
	}
}
