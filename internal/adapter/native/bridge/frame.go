// Package bridge parses the jsonl files gemba-bridge writes and
// translates each frame into a core.OrchestrationEvent. One tailer
// goroutine runs per live session; the fan-out channel it writes to
// feeds both the adaptor's in-memory escalation index and the
// server's SSE hub.
//
// Translator functions are per-agent-type (claude, shell-only, etc.)
// so adding a new agent type is a file drop, not a core change.
package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Frame is the shape gemba-bridge writes — must stay in lock-step
// with cmd/gemba-bridge/main.go's Frame struct.
type Frame struct {
	TS         string          `json:"ts"`
	SessionID  string          `json:"session_id"`
	AgentType  string          `json:"agent_type"`
	Hook       string          `json:"hook"`
	EventID    string          `json:"event_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	PayloadRaw string          `json:"payload_raw,omitempty"`
}

// ParseLine decodes one jsonl line. Returns the Frame plus a bool
// indicating whether the line was parseable; a false bool is how
// the tailer distinguishes "malformed, drop + log" from "empty line,
// skip silently."
func ParseLine(line []byte) (Frame, bool) {
	line = trimLineBytes(line)
	if len(line) == 0 {
		return Frame{}, false
	}
	var f Frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Frame{}, false
	}
	if f.SessionID == "" || f.Hook == "" {
		return Frame{}, false
	}
	return f, true
}

func trimLineBytes(b []byte) []byte {
	// Strip trailing newline/CR without pulling in bytes.TrimRight +
	// cutset allocation.
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

// LogPath returns the on-disk path gemba-bridge writes its frames
// to for the given session id. Kept in one place so the tailer
// and the bridge agree on layout without sharing code.
func LogPath(sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("bridge: session id required")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("bridge: user home dir: %w", err)
	}
	return filepath.Join(home, ".gemba", "sessions", SafeSessionID(sessionID)+".jsonl"), nil
}

// SafeSessionID mirrors cmd/gemba-bridge/main.go:safeSessionID so
// both sides agree on which file a session's frames land in.
func SafeSessionID(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-', c == '.', c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
