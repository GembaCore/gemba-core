// gemba-bridge is the structured-event shim the native orchestrator
// adaptor relies on (gm-native.7). Every time a supported agent fires
// a hook (Claude Code's SessionStart / PreToolUse / Notification /
// Stop, shell $PROMPT_COMMAND, etc.), the agent invokes this binary
// with the hook payload on stdin. The bridge:
//
//  1. Reads the hook payload from stdin (opaque JSON — never parsed).
//  2. Reads GEMBA_SESSION_ID and GEMBA_AGENT_TYPE from env.
//  3. Wraps the payload in a Frame with timestamp + event id.
//  4. Appends one line to ~/.gemba/sessions/<session_id>.jsonl via
//     O_APPEND (POSIX guarantees line atomicity for writes ≤PIPE_BUF).
//  5. Exits 0 — the caller's hook contract is preserved regardless
//     of whether the write succeeded (fs errors are logged to stderr).
//
// Design invariants:
//   - Zero state. Zero network. One file write per invocation.
//   - Latency budget: 20ms on a hot filesystem; 200ms worst case.
//   - Any failure short-circuits to exit 0 after a stderr warning so
//     Claude's hook chain never breaks because of Gemba.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// envelopeName is the stdin-payload wrapping key. Keeping the Claude
// hook event type as the top-level key would bleed agent-specific
// schema into the bridge; instead we stash the original payload as
// raw bytes under "payload" and let the server-side translator
// interpret per-agent-type.
type Frame struct {
	TS         string          `json:"ts"`
	SessionID  string          `json:"session_id"`
	AgentType  string          `json:"agent_type"`
	Hook       string          `json:"hook"`
	EventID    string          `json:"event_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	PayloadRaw string          `json:"payload_raw,omitempty"`
}

func main() {
	if err := run(os.Stdin, os.Stdout, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "gemba-bridge:", err)
	}
	// Always exit 0 — the caller's hook chain is more important than
	// whether we persisted the frame. (gm-eazw deny path is the one
	// exception: emitDeny writes hookSpecificOutput on stdout BEFORE
	// returning, so Claude's hook contract is still satisfied.)
}

func run(stdin io.Reader, stdout io.Writer, args []string) error {
	sessionID := os.Getenv("GEMBA_SESSION_ID")
	agentType := os.Getenv("GEMBA_AGENT_TYPE")
	if sessionID == "" {
		return fmt.Errorf("GEMBA_SESSION_ID not set")
	}

	// Hook name can be passed via arg, env, or the payload — accept
	// any. Env is the most robust since Claude's hook config can
	// inject it.
	hook := os.Getenv("GEMBA_HOOK_EVENT")
	if hook == "" && len(args) >= 2 {
		hook = args[1]
	}
	if hook == "" {
		hook = "unknown"
	}

	raw, err := io.ReadAll(io.LimitReader(bufio.NewReader(stdin), 1<<20))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// gm-eazw — Layer 2 enforcement. PreToolUse decisions happen here
	// so the deny response lands on stdout before Claude's tool call
	// fires. Other hooks fall through to the existing log path.
	if hook == "PreToolUse" && len(raw) > 0 {
		if allow, reason := enforcePreToolUse(raw, sessionID); !allow {
			if err := emitDeny(stdout, reason); err != nil {
				fmt.Fprintln(os.Stderr, "gemba-bridge: emit deny:", err)
			}
			// Continue logging the frame so the audit trail captures
			// the rejected call too.
		}
	}

	f := Frame{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		AgentType: agentType,
		Hook:      hook,
		EventID:   newEventID(),
	}
	// Prefer structured payload; fall back to raw string when the
	// input isn't valid JSON so we never drop data on the floor.
	if len(raw) > 0 {
		if json.Valid(raw) {
			f.Payload = json.RawMessage(raw)
		} else {
			f.PayloadRaw = string(raw)
		}
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

// sessionLogPath computes ~/.gemba/sessions/<safe_id>.jsonl. The
// GEMBA_SESSION_ID may contain path-unsafe characters (tmux's %N,
// colons, slashes); we replace them with `_` to keep the path shape
// predictable.
func sessionLogPath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".gemba", "sessions", safeSessionID(sessionID)+".jsonl"), nil
}

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

// newEventID returns a short random hex token. 16 hex chars is
// sufficient for local uniqueness and avoids pulling in the uuid
// dep for ~100 LOC binary.
func newEventID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
