// gemba-mcp is the MCP-tool sibling of gemba-ask + gemba-state
// (gm-97w7.2). It exposes four tools Claude Code (or any other
// MCP-capable agent) can call with schema-validated arguments
// instead of shelling out to the CLI binaries:
//
//	ask_question(role, text, bead_id?, title?)
//	raise_blocker(role, text, bead_id?, title?)   # role MUST be "manager"
//	report_state(state, bead_id?, note?)
//	emit_skill_output(skill_id, lines)             # gm-twp2
//
// emit_skill_output is the structured-output channel for persona
// consults. A persona-spawned session calls it with the array of
// skill-defined output lines (e.g. strategy + recommendations +
// summary for the PM's epic_order skill); the dispatcher tails the
// session log, runs each line through skill.ValidateOutputLine, and
// persists the PersonaConsultRecord audit row.
//
// Why an MCP server in addition to the CLIs:
//
//   - The CLIs stay as the fallback for shell-only agents.
//   - MCP tools appear natively in the agent's trace (no Bash wrap)
//     and get schema-validated arg handling from the SDK.
//   - The same (kind, channel=tool_call, urgency) shape lands on the
//     session log, so the bridge translator and escalation index
//     don't care which surface produced the frame.
//
// Install path: `gemba install-bridge` registers this binary as an
// MCP server under `.claude/settings.local.json` alongside the hook
// stanza (gm-native.7 extended in this bead). The MCP + hook
// installs are additive — both run for the same session.
//
// Design invariants (mirrors gemba-ask / gemba-state):
//
//   - Zero state. Zero network. One file write per tool invocation.
//   - Any failure in the handler returns a typed MCP error so the
//     agent sees the problem immediately.
//   - Frame shape MUST stay lock-step with
//     internal/adapter/native/bridge/frame.go Frame.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Frame is identical to bridge.Frame / cmd/gemba-bridge Frame /
// cmd/gemba-ask Frame / cmd/gemba-state Frame. Four copies exist
// because cmd/ binaries cannot import internal/; deduping lives
// behind a future move of the shared shape into its own package.
type Frame struct {
	TS         string          `json:"ts"`
	SessionID  string          `json:"session_id"`
	AgentType  string          `json:"agent_type"`
	Hook       string          `json:"hook"`
	EventID    string          `json:"event_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	PayloadRaw string          `json:"payload_raw,omitempty"`
}

// askPayload mirrors cmd/gemba-ask GembaAskPayload.
type askPayload struct {
	Kind   string `json:"kind"`
	Role   string `json:"role"`
	Text   string `json:"text"`
	Mode   string `json:"mode"`
	BeadID string `json:"bead_id,omitempty"`
	Title  string `json:"title,omitempty"`
}

// statePayload mirrors cmd/gemba-state GembaStatePayload.
type statePayload struct {
	State  string `json:"state"`
	BeadID string `json:"bead_id,omitempty"`
	Note   string `json:"note,omitempty"`
}

// skillOutputPayload is the body of a GembaSkillOutput frame. The
// dispatcher reads `skill_id` to pick the right
// persona.Skill.ValidateOutputLine, then walks `lines` in order. Lines
// are kept as raw JSON so this binary stays import-isolated from the
// internal/skills/* packages — every skill defines its own line shape
// and this server is intentionally generic.
type skillOutputPayload struct {
	SkillID string            `json:"skill_id"`
	Lines   []json.RawMessage `json:"lines"`
}

// ---------- tool input schemas (SDK infers JSON schema from these) --

// AskQuestionInput is the schema every ask_question caller must match.
// The SDK's AddTool generates a JSON schema from the jsonschema tags
// and rejects malformed args before the handler runs.
type AskQuestionInput struct {
	Role   string `json:"role" jsonschema:"coach | manager — the variety of skill raising this"`
	Text   string `json:"text" jsonschema:"the question body the operator reads"`
	BeadID string `json:"bead_id,omitempty" jsonschema:"work item id this question belongs to"`
	Title  string `json:"title,omitempty" jsonschema:"short one-liner preceding the text"`
}

// RaiseBlockerInput is like AskQuestionInput but role is constrained
// at the handler level to "manager" (schema can't express a
// per-tool conditional enum cleanly). Coaches calling raise_blocker
// get a structured tool-call error.
type RaiseBlockerInput struct {
	Role   string `json:"role" jsonschema:"must be \"manager\" — Coaches never raise blockers"`
	Text   string `json:"text" jsonschema:"the blocker body; name the specific thing you need"`
	BeadID string `json:"bead_id,omitempty" jsonschema:"work item id this blocker belongs to"`
	Title  string `json:"title,omitempty" jsonschema:"short one-liner preceding the text"`
}

// ReportStateInput mirrors gemba-state's positional + flag API.
type ReportStateInput struct {
	State  string `json:"state" jsonschema:"initializing | ready | working | prompting | stalled | bead-done"`
	BeadID string `json:"bead_id,omitempty" jsonschema:"bead id; recommended for state=working"`
	Note   string `json:"note,omitempty" jsonschema:"free-form note for the operator log"`
}

// EmitSkillOutputInput is the schema every emit_skill_output caller
// must match. The line shape varies per skill (the model is told the
// shape via its skill prompt); this binary only checks that skill_id
// is present and lines is non-empty. Per-line schema enforcement
// happens in the dispatcher via skill.ValidateOutputLine — bad lines
// land in the audit log as warning entries rather than failing the
// tool call (which would lose every preceding good line in the same
// emission).
type EmitSkillOutputInput struct {
	SkillID string `json:"skill_id" jsonschema:"the skill id this emission belongs to (e.g. epic_order)"`
	Lines   []any  `json:"lines" jsonschema:"ordered array of skill-defined output lines (e.g. strategy first, then recommendations, then summary)"`
}

var validStates = map[string]bool{
	"initializing": true,
	"ready":        true,
	"working":      true,
	"prompting":    true,
	"stalled":      true,
	"bead-done":    true,
}
var validRoles = map[string]bool{"coach": true, "manager": true}
var validModes = map[string]bool{"dangerous": true, "balanced": true, "cautious": true}

func main() {
	if err := run(); err != nil {
		// Errors here are fatal — no session log written, no MCP
		// server started. Log to stderr so the launcher sees it.
		log.Fatal("gemba-mcp: ", err)
	}
}

func run() error {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "gemba",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "ask_question",
		Description: "Surface a non-blocking question to the operator. Use for decisions " +
			"that have a reasonable default but would benefit from operator judgment.",
	}, handleAskQuestion)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "raise_blocker",
		Description: "Surface a blocker that halts the agent until the operator responds. " +
			"Manager role only — Coaches cannot raise blockers.",
	}, handleRaiseBlocker)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "report_state",
		Description: "Report the agent's observable session status. Mirrors the gemba-state CLI. " +
			"Call at every state boundary (ready / working / prompting / stalled / bead-done).",
	}, handleReportState)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "emit_skill_output",
		Description: "Emit a persona skill's structured output. Use this for skill consults " +
			"(e.g. the PM's epic_order ranking). Pass skill_id plus an ordered array of " +
			"line objects whose shape is defined by that skill's documented schema. " +
			"The dispatcher validates each line and persists the audit-log row.",
	}, handleEmitSkillOutput)

	return srv.Run(context.Background(), &mcp.StdioTransport{})
}

// ---------- handlers -------------------------------------------------

func handleAskQuestion(
	_ context.Context, _ *mcp.CallToolRequest, in AskQuestionInput,
) (*mcp.CallToolResult, any, error) {
	if err := writeAsk("question", in.Role, in.Text, in.BeadID, in.Title); err != nil {
		return toolError(err), nil, nil
	}
	return toolOK("question surfaced"), nil, nil
}

func handleRaiseBlocker(
	_ context.Context, _ *mcp.CallToolRequest, in RaiseBlockerInput,
) (*mcp.CallToolResult, any, error) {
	if in.Role != "manager" {
		return toolError(fmt.Errorf("raise_blocker requires role=manager; got %q (Coaches never raise blockers)", in.Role)), nil, nil
	}
	if err := writeAsk("blocker", in.Role, in.Text, in.BeadID, in.Title); err != nil {
		return toolError(err), nil, nil
	}
	return toolOK("blocker surfaced"), nil, nil
}

func handleReportState(
	_ context.Context, _ *mcp.CallToolRequest, in ReportStateInput,
) (*mcp.CallToolResult, any, error) {
	if !validStates[in.State] {
		return toolError(fmt.Errorf("invalid state %q; want one of initializing|ready|working|prompting|stalled|bead-done", in.State)), nil, nil
	}
	if err := writeState(in.State, in.BeadID, in.Note); err != nil {
		return toolError(err), nil, nil
	}
	return toolOK("state reported: " + in.State), nil, nil
}

func handleEmitSkillOutput(
	_ context.Context, _ *mcp.CallToolRequest, in EmitSkillOutputInput,
) (*mcp.CallToolResult, any, error) {
	if err := writeSkillOutput(in.SkillID, in.Lines); err != nil {
		return toolError(err), nil, nil
	}
	return toolOK(fmt.Sprintf("skill output emitted: skill_id=%s lines=%d", in.SkillID, len(in.Lines))), nil, nil
}

// ---------- frame writers (mirror the CLIs) ------------------------

// writeAsk shares the validation + frame shape with cmd/gemba-ask so
// the translator's GembaAsk branch sees identical data regardless of
// which surface the agent used.
func writeAsk(kind, role, text, beadID, title string) error {
	if !validRoles[role] {
		return fmt.Errorf("role must be coach | manager; got %q", role)
	}
	if role == "coach" && kind == "blocker" {
		return errors.New("role=coach cannot raise kind=blocker (Coaches never block)")
	}
	if text == "" {
		return errors.New("text is required")
	}
	sessionID, agentType, mode, err := envContext()
	if err != nil {
		return err
	}
	if mode == "dangerous" {
		return errors.New("interaction_mode=dangerous does not allow ask/blocker — record an assumption and proceed")
	}
	payload, err := json.Marshal(askPayload{
		Kind: kind, Role: role, Text: text, Mode: mode, BeadID: beadID, Title: title,
	})
	if err != nil {
		return err
	}
	return writeFrame("GembaAsk", sessionID, agentType, payload)
}

func writeState(state, beadID, note string) error {
	sessionID, agentType, _, err := envContext()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(statePayload{
		State: state, BeadID: beadID, Note: note,
	})
	if err != nil {
		return err
	}
	return writeFrame("GembaState", sessionID, agentType, payload)
}

// writeSkillOutput composes a GembaSkillOutput frame and appends it to
// the session log. The model is allowed (per skill prompt) to call
// emit_skill_output more than once per session — each call writes one
// frame, and the dispatcher concatenates frames in arrival order. This
// keeps long emissions resilient: a model that paginates across two
// calls still produces a coherent ordered stream.
func writeSkillOutput(skillID string, lines []any) error {
	if skillID == "" {
		return errors.New("skill_id is required")
	}
	if len(lines) == 0 {
		return errors.New("lines must not be empty")
	}
	sessionID, agentType, _, err := envContext()
	if err != nil {
		return err
	}
	rawLines := make([]json.RawMessage, len(lines))
	for i, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			return fmt.Errorf("marshal line %d: %w", i, err)
		}
		rawLines[i] = b
	}
	payload, err := json.Marshal(skillOutputPayload{
		SkillID: skillID,
		Lines:   rawLines,
	})
	if err != nil {
		return err
	}
	return writeFrame("GembaSkillOutput", sessionID, agentType, payload)
}

func writeFrame(hook, sessionID, agentType string, payload json.RawMessage) error {
	f := Frame{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		AgentType: agentType,
		Hook:      hook,
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

// ---------- helpers -------------------------------------------------

// envContext pulls GEMBA_SESSION_ID (required), GEMBA_AGENT_TYPE
// (advisory), and GEMBA_INTERACTION_MODE (defaulted to balanced when
// absent, matching gemba-ask). An invalid mode is a hard error so a
// typo in agents.toml surfaces fast.
func envContext() (sessionID, agentType, mode string, err error) {
	sessionID = os.Getenv("GEMBA_SESSION_ID")
	if sessionID == "" {
		return "", "", "", errors.New("GEMBA_SESSION_ID not set — gemba-mcp only runs inside a native-adaptor session")
	}
	agentType = os.Getenv("GEMBA_AGENT_TYPE")
	mode = os.Getenv("GEMBA_INTERACTION_MODE")
	if mode == "" {
		mode = "balanced"
	}
	if !validModes[mode] {
		return "", "", "", fmt.Errorf("GEMBA_INTERACTION_MODE invalid: %q", mode)
	}
	return sessionID, agentType, mode, nil
}

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

func newEventID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func toolOK(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
