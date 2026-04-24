package bridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
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
