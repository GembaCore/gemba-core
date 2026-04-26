// gemba-ask is the skill-authoring sentinel for surfacing operator
// attention from inside a dispatched session (gm-97w7.1). A Coach
// or Manager skill instructs the agent to call this binary whenever
// it wants to raise a question or a blocker:
//
//	gemba-ask --kind question --role coach   --text "Default to test key or fail hard?"
//	gemba-ask --kind blocker  --role manager --text "Need STRIPE_SECRET_KEY in env."
//
// The binary writes one Hook="GembaAsk" Frame to
// ~/.gemba/sessions/<session_id>.jsonl. The bridge tailer reads it,
// the translator emits an escalation_opened event with the kind /
// channel / urgency already stamped, and the native
// OrchestrationPlane's escalation index surfaces it on
// /api/escalations.
//
// Why a CLI (not transcript scraping, not an MCP tool):
//
//   - Mirrors the gemba-state pattern already proven in this
//     codebase. Same frame layout, same tailer, same
//     one-file-write-per-invocation simplicity.
//   - Structured capture at emit time — no markdown parsing, no
//     format drift risk.
//   - Works for any coding agent with shell access; not bound to
//     Claude's transcript.jsonl layout.
//   - MCP tool variant (in-process Agent SDK server) is a natural
//     follow-up using the same kind/channel/urgency shape.
//
// Design invariants (mirrors gemba-state / gemba-bridge):
//   - Zero state, zero network, one file write per invocation.
//   - Any failure goes to stderr + exit 1 so the operator sees
//     the problem directly (this binary is user-facing, not in a
//     hook chain).
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

// validKinds mirrors the tool-call-channel escalation kinds in
// core.EscalationKind. Keeping the literals in sync with the Go
// package is asserted by the cmd/gemba-ask test.
var validKinds = map[string]bool{
	"question": true,
	"blocker":  true,
}

// validRoles mirrors the Coach/Manager split in
// docs/design/persona-pppp.md. Enforced here so a skill author can't
// accidentally tag a blocker as coach-authored — the scanner would
// reject it downstream, but rejecting at the CLI gives a faster
// error for the operator.
var validRoles = map[string]bool{
	"coach":   true,
	"manager": true,
}

// validModes is the authoritative list of interaction_mode values
// (gm-97w7 / gm-bglh). Kept in lock-step with
// internal/adapter/native/bridge/skills.go InteractionMode.
var validModes = map[string]bool{
	"dangerous": true,
	"balanced":  true,
	"cautious":  true,
}

// Frame is identical to bridge.Frame / cmd/gemba-bridge Frame. Kept
// as a copy because cmd/gemba-ask must not import internal/ (Go's
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

// GembaAskPayload is the structured body of a GembaAsk frame. The
// translator reads these fields to build the escalation_opened
// event. Mode is captured at emit time so the translator can stamp
// Urgency without consulting live agents.toml — if the operator
// changes modes mid-session, older in-flight asks keep their
// original-mode urgency, which is the desired semantics.
type GembaAskPayload struct {
	Kind   string `json:"kind"` // question | blocker
	Role   string `json:"role"` // coach | manager
	Text   string `json:"text"`
	Mode   string `json:"mode"` // dangerous | balanced | cautious
	BeadID string `json:"bead_id,omitempty"`
	Title  string `json:"title,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gemba-ask:", err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gemba-ask", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kind := fs.String("kind", "", "question | blocker (required)")
	role := fs.String("role", "", "coach | manager (required)")
	text := fs.String("text", "", "the question or blocker body (required)")
	title := fs.String("title", "", "short operator-facing title (optional)")
	bead := fs.String("bead", "", "work item id this ask belongs to (optional but recommended)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional args: %v", fs.Args())
	}
	if *kind == "" || !validKinds[*kind] {
		return fmt.Errorf("--kind must be question | blocker; got %q", *kind)
	}
	if *role == "" || !validRoles[*role] {
		return fmt.Errorf("--role must be coach | manager; got %q", *role)
	}
	if *role == "coach" && *kind == "blocker" {
		// Coach-role never raises blockers (skill-authoring contract).
		// Fail fast at the CLI so the skill author sees the problem
		// immediately rather than having the downstream scanner drop
		// it silently.
		return errors.New("--role coach cannot raise --kind blocker (Coaches never block; see docs/design/skill-authoring-contract.md)")
	}
	if *text == "" {
		return errors.New("--text is required (the body the operator will read)")
	}

	sessionID := os.Getenv("GEMBA_SESSION_ID")
	if sessionID == "" {
		return errors.New("GEMBA_SESSION_ID not set — gemba-ask only runs inside a native-adaptor session")
	}
	agentType := os.Getenv("GEMBA_AGENT_TYPE") // advisory; blank OK

	mode := os.Getenv("GEMBA_INTERACTION_MODE")
	if mode == "" {
		// A session spawned before GEMBA_INTERACTION_MODE was part of
		// the env contract shouldn't be a hard failure — treat as
		// balanced (the documented default) and warn.
		fmt.Fprintln(stderr, "gemba-ask: GEMBA_INTERACTION_MODE not set; defaulting to balanced")
		mode = "balanced"
	}
	if !validModes[mode] {
		return fmt.Errorf("GEMBA_INTERACTION_MODE invalid: %q (want dangerous | balanced | cautious)", mode)
	}
	if mode == "dangerous" {
		// The profile forbids surfacing in dangerous mode. The
		// scanner also drops these, but again — fast fail at the
		// CLI gives the skill author a readable error.
		return fmt.Errorf("interaction_mode=dangerous does not allow gemba-ask — record an assumption in a commit-message note and proceed")
	}

	payload, err := json.Marshal(GembaAskPayload{
		Kind:   *kind,
		Role:   *role,
		Text:   *text,
		Mode:   mode,
		BeadID: *bead,
		Title:  *title,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	f := Frame{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		AgentType: agentType,
		Hook:      "GembaAsk",
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

// safeSessionID mirrors cmd/gemba-bridge / cmd/gemba-state helpers.
// Kept as a local copy because main-pkg binaries in cmd/ can't
// import internal/.
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
