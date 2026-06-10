// gemba-state is the deterministic state-transition sentinel for
// native-adaptor sessions (gm-cdph / gm-97w7). The agent (Claude or
// any CLI) invokes it via its Bash tool at every state boundary:
//
//	gemba-state ready                    # session is idle
//	gemba-state working --bead gm-123    # bead dispatched, work in flight
//	gemba-state prompting                # asking the operator a question
//
// The binary writes one Hook="GembaState" Frame to
// ~/.gemba/sessions/<session_id>.jsonl. The bridge tailer picks it
// up, the translator (internal/adapter/native/bridge/translate.go)
// emits a session_state_reported event, and the native
// OrchestrationPlane observer mutates the session's Status.
//
// Design invariants (mirrors gemba-bridge):
//   - Zero state, zero network, one file write per invocation.
//   - Any failure goes to stderr + exit 1 so the operator sees the
//     problem directly (this binary is user-facing, not hook chain).
//   - Frame shape MUST stay in lock-step with
//     internal/adapter/native/bridge/frame.go Frame.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// validStates mirrors core.SessionStatus observable values (gm-d044)
// minus the terminal ones. Terminal transitions go through
// EndSession, not gemba-state.
//
// "bead-done" is a special pseudo-state introduced for the session
// pool lifecycle (gm-s47n.11, docs/design/session-pool.md §4.2). It
// is NOT a SessionStatus; it is a bead-boundary signal that the
// agent has finished a bead and is going idle as a pool member. The
// bridge translates it to a session_state_reported event with
// state="bead-done", which the native adaptor maps to SessionReady
// (after running a worktree-cleanliness check, §5.4.2).
var validStates = map[string]bool{
	"initializing": true,
	"ready":        true,
	"working":      true,
	"prompting":    true,
	"stalled":      true,
	"bead-done":    true,
}

// Frame is identical to bridge.Frame / cmd/gemba-bridge Frame. Kept
// as a copy because cmd/gemba-state must not import internal/ (Go's
// module layout rule).
type Frame struct {
	TS         string          `json:"ts"`
	SessionID  string          `json:"session_id"`
	AgentType  string          `json:"agent_type"`
	Hook       string          `json:"hook"`
	EventID    string          `json:"event_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	PayloadRaw string          `json:"payload_raw,omitempty"`
}

// GembaStatePayload is the structured body of a GembaState frame. The
// translator reads these fields to build the session_state_reported
// event.
type GembaStatePayload struct {
	State  string `json:"state"`
	BeadID string `json:"bead_id,omitempty"`
	Note   string `json:"note,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gemba-state:", err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gemba-state", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bead := fs.String("bead", "", "bead id (optional; recommended for --state=working)")
	note := fs.String("note", "", "free-form note for the operator log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("usage: gemba-state <state> [--bead <id>] [--note <text>]")
	}
	state := rest[0]
	if !validStates[state] {
		return fmt.Errorf("unknown state %q; valid: initializing, ready, working, prompting, stalled, bead-done", state)
	}

	sessionID := os.Getenv("GEMBA_SESSION_ID")
	if sessionID == "" {
		return errors.New("GEMBA_SESSION_ID not set — gemba-state only runs inside a native-adaptor session")
	}
	agentType := os.Getenv("GEMBA_AGENT_TYPE") // advisory; blank OK

	payload, err := json.Marshal(GembaStatePayload{
		State:  state,
		BeadID: *bead,
		Note:   *note,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	f := Frame{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		AgentType: agentType,
		Hook:      "GembaState",
		EventID:   newEventID(),
		Payload:   payload,
	}
	line, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	line = append(line, '\n')

	path, err := sessionLogPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer fh.Close()
	if _, err := fh.Write(line); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func sessionLogPath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".gemba", "sessions", safeSessionID(sessionID)+".jsonl"), nil
}

// safeSessionID mirrors cmd/gemba-bridge's helper. Keep in sync.
func safeSessionID(id string) string {
	out := make([]byte, 0, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-', c == '.', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func newEventID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
