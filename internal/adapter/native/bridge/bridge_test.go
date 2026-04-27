package bridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

func TestParseLineValid(t *testing.T) {
	raw := `{"ts":"2026-04-24T10:00:00Z","session_id":"s1","agent_type":"claude","hook":"SessionStart","event_id":"e1"}` + "\n"
	f, ok := ParseLine([]byte(raw))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if f.SessionID != "s1" || f.Hook != "SessionStart" {
		t.Errorf("unexpected: %+v", f)
	}
}

func TestParseLineRejectsMissingRequired(t *testing.T) {
	cases := [][]byte{
		[]byte(`{}`),
		[]byte(`{"hook":"Stop"}`),     // no session_id
		[]byte(`{"session_id":"s1"}`), // no hook
		[]byte(``),
		[]byte(`not-json`),
	}
	for _, c := range cases {
		if _, ok := ParseLine(c); ok {
			t.Errorf("want rejected, got accepted: %q", c)
		}
	}
}

func TestSafeSessionIDMatchesBridge(t *testing.T) {
	// Must stay lock-step with cmd/gemba-bridge/main.go:safeSessionID.
	got := SafeSessionID("tmux:%12/abc")
	if got != "tmux__12_abc" {
		t.Errorf("SafeSessionID: got %q want %q", got, "tmux__12_abc")
	}
}

func TestTranslateClaudeSessionStart(t *testing.T) {
	evs := translateClaude(Frame{
		SessionID: "s1", Hook: "SessionStart", EventID: "e1",
	})
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].Kind != "session_transition" {
		t.Errorf("kind: got %q", evs[0].Kind)
	}
	if evs[0].Payload["status"] != "running" {
		t.Errorf("status: %+v", evs[0].Payload)
	}
}

func TestTranslateClaudeStop(t *testing.T) {
	evs := translateClaude(Frame{SessionID: "s1", Hook: "Stop"})
	if evs[0].Kind != "session_transition" || evs[0].Payload["status"] != "completed" {
		t.Errorf("unexpected stop event: %+v", evs[0])
	}
}

func TestTranslateClaudeNotificationClassifiesHITL(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"type": "elicitation", "question": "ok?"})
	evs := translateClaude(Frame{
		SessionID: "s1", Hook: "Notification", Payload: payload,
	})
	if evs[0].Kind != "escalation_opened" {
		t.Errorf("kind: %q", evs[0].Kind)
	}
	if evs[0].Payload["escalation_kind"] != string(core.EscalationHITLApproval) {
		t.Errorf("classification: got %v", evs[0].Payload["escalation_kind"])
	}
}

func TestTranslateClaudeNotificationDefaultsPermissionPrompt(t *testing.T) {
	evs := translateClaude(Frame{SessionID: "s1", Hook: "Notification"})
	if evs[0].Payload["escalation_kind"] != string(core.EscalationPermissionPrompt) {
		t.Errorf("default classification wrong: %v", evs[0].Payload["escalation_kind"])
	}
}

func TestTranslateClaudeGembaStateEmitsSessionStateReported(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"state": "working", "bead_id": "gm-42"})
	evs := translateClaude(Frame{
		SessionID: "s1", Hook: "GembaState", EventID: "e-gs-1", Payload: payload,
	})
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].Kind != "session_state_reported" {
		t.Errorf("kind: got %q, want session_state_reported", evs[0].Kind)
	}
	if evs[0].Payload["state"] != "working" {
		t.Errorf("state payload: got %v", evs[0].Payload["state"])
	}
	if evs[0].Payload["bead_id"] != "gm-42" {
		t.Errorf("bead_id payload: got %v", evs[0].Payload["bead_id"])
	}
}

// gm-97w7.1: gemba-ask CLI frames translate to a fully-stamped
// escalation_opened event.
func TestTranslateClaudeGembaAskQuestionBalanced(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"kind":    "question",
		"role":    "coach",
		"text":    "Default to test key or fail hard?",
		"mode":    "balanced",
		"bead_id": "gm-42",
	})
	evs := translateClaude(Frame{
		SessionID: "s1", Hook: "GembaAsk", EventID: "e-ask-1", Payload: payload,
	})
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Kind != "escalation_opened" {
		t.Errorf("kind: %q want escalation_opened", ev.Kind)
	}
	if ev.Payload["escalation_kind"] != string(core.EscalationQuestion) {
		t.Errorf("escalation_kind: %v", ev.Payload["escalation_kind"])
	}
	if ev.Payload["channel"] != string(core.ChannelToolCall) {
		t.Errorf("channel: %v", ev.Payload["channel"])
	}
	if ev.Payload["urgency"] != string(core.UrgencyAdvisory) {
		t.Errorf("urgency: %v want advisory (balanced/question)", ev.Payload["urgency"])
	}
	if ev.Payload["role"] != "coach" {
		t.Errorf("role: %v", ev.Payload["role"])
	}
	if ev.Payload["bead_id"] != "gm-42" {
		t.Errorf("bead_id: %v", ev.Payload["bead_id"])
	}
}

// Manager blocker in balanced mode → blocking.
func TestTranslateClaudeGembaAskBlockerBalanced(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"kind": "blocker",
		"role": "manager",
		"text": "Need STRIPE_SECRET_KEY.",
		"mode": "balanced",
	})
	evs := translateClaude(Frame{
		SessionID: "s1", Hook: "GembaAsk", EventID: "e-ask-2", Payload: payload,
	})
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].Payload["urgency"] != string(core.UrgencyBlocking) {
		t.Errorf("urgency: %v want blocking", evs[0].Payload["urgency"])
	}
	if evs[0].Payload["escalation_kind"] != string(core.EscalationBlocker) {
		t.Errorf("escalation_kind: %v", evs[0].Payload["escalation_kind"])
	}
}

// Dangerous mode drops the event — the profile forbids surfacing.
func TestTranslateClaudeGembaAskDangerousModeDrops(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"kind": "question", "role": "coach",
		"text": "x", "mode": "dangerous",
	})
	evs := translateClaude(Frame{SessionID: "s1", Hook: "GembaAsk", Payload: payload})
	if len(evs) != 0 {
		t.Fatalf("dangerous-mode ask must not surface; got %+v", evs)
	}
}

// Coach cannot raise blockers — even if a hand-crafted frame
// claims it did, the translator drops.
func TestTranslateClaudeGembaAskCoachBlockerDropped(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"kind": "blocker", "role": "coach",
		"text": "x", "mode": "balanced",
	})
	evs := translateClaude(Frame{SessionID: "s1", Hook: "GembaAsk", Payload: payload})
	if len(evs) != 0 {
		t.Fatalf("coach-blocker must be dropped; got %+v", evs)
	}
}

// Passthrough translator picks up GembaAsk too (shell-only agents).
func TestTranslatePassthroughGembaAsk(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"kind": "question", "role": "coach",
		"text": "x", "mode": "balanced",
	})
	evs := translatePassthrough(Frame{SessionID: "s1", Hook: "GembaAsk", Payload: payload})
	if len(evs) != 1 || evs[0].Kind != "escalation_opened" {
		t.Errorf("passthrough dropped GembaAsk: %+v", evs)
	}
}

// gm-twp2: GembaSkillOutput frame produces a skill_output_emitted
// event with skill_id, line_count, and verbatim raw lines. The
// translator stays generic — per-line validation belongs to the
// dispatcher.
func TestTranslateClaudeGembaSkillOutput(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"skill_id": "epic_order",
		"lines": []any{
			map[string]any{"type": "strategy", "model": "claude-opus-4-7"},
			map[string]any{"type": "summary"},
		},
	})
	evs := translateClaude(Frame{
		SessionID: "s1", Hook: "GembaSkillOutput", Payload: payload, EventID: "ev1",
	})
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].Kind != "skill_output_emitted" {
		t.Errorf("kind = %q, want skill_output_emitted", evs[0].Kind)
	}
	if evs[0].Payload["skill_id"] != "epic_order" {
		t.Errorf("skill_id payload = %v", evs[0].Payload["skill_id"])
	}
	if got, ok := evs[0].Payload["line_count"].(int); !ok || got != 2 {
		t.Errorf("line_count = %v, want 2", evs[0].Payload["line_count"])
	}
	lines, ok := evs[0].Payload["lines"].([]any)
	if !ok {
		t.Fatalf("lines payload = %T, want []any", evs[0].Payload["lines"])
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
}

// Empty payload → no event. Defensive: a hand-crafted frame with no
// content must not produce a phantom event.
func TestTranslateClaudeGembaSkillOutputEmpty(t *testing.T) {
	if evs := translateClaude(Frame{
		SessionID: "s1", Hook: "GembaSkillOutput",
	}); len(evs) != 0 {
		t.Errorf("empty-payload skill output must drop; got %+v", evs)
	}
}

// Missing skill_id → drop. The dispatcher needs a skill id to pick
// the right ValidateOutputLine; an emission without one is unusable.
func TestTranslateClaudeGembaSkillOutputMissingSkillID(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"lines": []any{map[string]any{"type": "strategy"}},
	})
	if evs := translateClaude(Frame{
		SessionID: "s1", Hook: "GembaSkillOutput", Payload: payload,
	}); len(evs) != 0 {
		t.Errorf("missing skill_id must drop; got %+v", evs)
	}
}

// Passthrough translator picks up GembaSkillOutput too — agent-type-
// agnostic, mirrors GembaAsk / GembaState.
func TestTranslatePassthroughGembaSkillOutput(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"skill_id": "epic_order",
		"lines":    []any{map[string]any{"type": "strategy"}},
	})
	evs := translatePassthrough(Frame{
		SessionID: "s1", Hook: "GembaSkillOutput", Payload: payload,
	})
	if len(evs) != 1 || evs[0].Kind != "skill_output_emitted" {
		t.Errorf("passthrough dropped GembaSkillOutput: %+v", evs)
	}
}

func TestTranslatePassthroughGembaStateEmitsSessionStateReported(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"state": "ready"})
	evs := translatePassthrough(Frame{
		SessionID: "s1", Hook: "GembaState", Payload: payload,
	})
	if len(evs) != 1 || evs[0].Kind != "session_state_reported" {
		t.Errorf("passthrough translator dropped GembaState: %+v", evs)
	}
}

func TestTailEmitsEventsWhenFileAppearsLater(t *testing.T) {
	home := t.TempDir()
	sessionID := "s1"
	sessDir := filepath.Join(home, ".gemba", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	path, err := LogPath(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan core.OrchestrationEvent, 10)
	tail := NewTail(path, "claude", events)
	tail.pollInterval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go tail.Run(ctx)

	// Write a frame after tail starts.
	time.Sleep(40 * time.Millisecond)
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	frame := `{"ts":"2026-04-24T10:00:00Z","session_id":"s1","agent_type":"claude","hook":"SessionStart","event_id":"e1"}` + "\n"
	if _, err := fh.WriteString(frame); err != nil {
		t.Fatal(err)
	}
	_ = fh.Close()

	select {
	case ev := <-events:
		if ev.Kind != "session_transition" {
			t.Errorf("unexpected kind: %q", ev.Kind)
		}
	case <-ctx.Done():
		t.Fatal("expected event within deadline")
	}
}

func TestTailSkipsMalformedLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "log.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	events := make(chan core.OrchestrationEvent, 10)
	tail := NewTail(path, "claude", events)
	tail.pollInterval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Write malformed + valid frames interleaved.
	data := "not json\n" +
		`{"session_id":"s1","hook":"SessionStart","event_id":"e1"}` + "\n" +
		"another bad line\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	go tail.Run(ctx)

	select {
	case ev := <-events:
		if ev.Kind != "session_transition" {
			t.Errorf("kind: %q", ev.Kind)
		}
	case <-ctx.Done():
		t.Fatal("expected event from valid frame")
	}

	// Verify no extra events.
	select {
	case ev := <-events:
		t.Errorf("unexpected extra event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestFanoutRegisterUnregister(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fo := NewFanout()
	defer fo.Close()

	ctx := context.Background()
	if err := fo.Register(ctx, "s1", "claude"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fo.Unregister("s1")
	// Second Unregister must be a no-op, not panic.
	fo.Unregister("s1")
}
