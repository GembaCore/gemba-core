package native

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestParallelChangedEventShape(t *testing.T) {
	ev := parallelChanged("tmux:gm-abc.1:1714169123456789", "claude", 2, 3, 1)
	if ev.Kind != EventKindSessionParallelChanged {
		t.Fatalf("kind: got %q", ev.Kind)
	}
	if ev.SessionID == "" {
		t.Fatal("session id missing")
	}
	for _, k := range []string{"session_id", "agent_type", "in_flight", "max_parallel", "delta"} {
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
	emitted := parallelChanged(fixture.SessionID, "claude", 2, 3, 1)
	for _, k := range []string{"session_id", "agent_type", "in_flight", "max_parallel", "delta"} {
		if _, ok := fixture.Payload[k]; !ok {
			t.Errorf("fixture missing payload key %q", k)
		}
		if _, ok := emitted.Payload[k]; !ok {
			t.Errorf("emitter missing payload key %q", k)
		}
	}
}
