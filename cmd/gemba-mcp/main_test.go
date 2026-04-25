package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Each test uses its own temp HOME so the session log doesn't leak
// between cases. The handlers write to
// <HOME>/.gemba/sessions/<sanitised session id>.jsonl.

func setupSession(t *testing.T, mode string) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GEMBA_SESSION_ID", "sess-42")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	t.Setenv("GEMBA_INTERACTION_MODE", mode)
	return filepath.Join(tmp, ".gemba", "sessions", "sess-42.jsonl")
}

func readSoleFrame(t *testing.T, path string) Frame {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 frame, got %d: %q", len(lines), string(body))
	}
	var f Frame
	if err := json.Unmarshal([]byte(lines[0]), &f); err != nil {
		t.Fatalf("unmarshal frame: %v; line=%q", err, lines[0])
	}
	return f
}

// gm-97w7.2: ask_question happy path — Coach in balanced mode.
func TestAskQuestionHappyPath(t *testing.T) {
	path := setupSession(t, "balanced")
	res, _, err := handleAskQuestion(context.Background(), &mcp.CallToolRequest{}, AskQuestionInput{
		Role:   "coach",
		Text:   "Default to test key or fail hard?",
		BeadID: "gm-42",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("want success result; got %+v", res)
	}
	f := readSoleFrame(t, path)
	if f.Hook != "GembaAsk" {
		t.Errorf("hook: got %q", f.Hook)
	}
	var p askPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if p.Kind != "question" || p.Role != "coach" || p.Mode != "balanced" || p.BeadID != "gm-42" {
		t.Errorf("payload: %+v", p)
	}
}

// raise_blocker happy path — Manager in balanced mode.
func TestRaiseBlockerHappyPath(t *testing.T) {
	path := setupSession(t, "balanced")
	res, _, err := handleRaiseBlocker(context.Background(), &mcp.CallToolRequest{}, RaiseBlockerInput{
		Role: "manager",
		Text: "Need STRIPE_SECRET_KEY.",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("want success; got error result %+v", res)
	}
	f := readSoleFrame(t, path)
	var p askPayload
	_ = json.Unmarshal(f.Payload, &p)
	if p.Kind != "blocker" || p.Role != "manager" {
		t.Errorf("payload kind/role wrong: %+v", p)
	}
}

// Coach cannot raise a blocker. The MCP tool returns IsError=true so
// the agent gets a visible failure; nothing lands in the log.
func TestRaiseBlockerCoachRejected(t *testing.T) {
	path := setupSession(t, "balanced")
	res, _, err := handleRaiseBlocker(context.Background(), &mcp.CallToolRequest{}, RaiseBlockerInput{
		Role: "coach",
		Text: "this is illegal",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError=true for coach+blocker")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("session log should not exist after rejected call")
	}
}

// Dangerous mode forbids ask / blocker. CLI rejects too; parity here.
func TestAskQuestionDangerousModeRejected(t *testing.T) {
	setupSession(t, "dangerous")
	res, _, err := handleAskQuestion(context.Background(), &mcp.CallToolRequest{}, AskQuestionInput{
		Role: "coach", Text: "hi",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError=true in dangerous mode")
	}
	if !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "dangerous") {
		t.Errorf("error message should mention dangerous: %q",
			res.Content[0].(*mcp.TextContent).Text)
	}
}

// report_state happy path.
func TestReportStateHappyPath(t *testing.T) {
	path := setupSession(t, "balanced")
	res, _, err := handleReportState(context.Background(), &mcp.CallToolRequest{}, ReportStateInput{
		State:  "working",
		BeadID: "gm-42",
	})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: err=%v res=%+v", err, res)
	}
	f := readSoleFrame(t, path)
	if f.Hook != "GembaState" {
		t.Errorf("hook: %q", f.Hook)
	}
	var p statePayload
	_ = json.Unmarshal(f.Payload, &p)
	if p.State != "working" || p.BeadID != "gm-42" {
		t.Errorf("state payload: %+v", p)
	}
}

// Invalid state name → IsError; dangerous mode does NOT block state
// reports (only ask/blocker).
func TestReportStateInvalidState(t *testing.T) {
	setupSession(t, "dangerous")
	res, _, err := handleReportState(context.Background(), &mcp.CallToolRequest{}, ReportStateInput{
		State: "bogus",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError for bogus state")
	}
}

// report_state IS allowed in dangerous mode — state reporting is
// purely observational and doesn't surface to the operator as a
// prompt.
func TestReportStateAllowedInDangerous(t *testing.T) {
	path := setupSession(t, "dangerous")
	res, _, err := handleReportState(context.Background(), &mcp.CallToolRequest{}, ReportStateInput{
		State: "working", BeadID: "gm-42",
	})
	if err != nil || res.IsError {
		t.Fatalf("state should be allowed in dangerous: err=%v res=%+v", err, res)
	}
	_ = readSoleFrame(t, path) // asserts frame written
}

// Missing GEMBA_SESSION_ID → handler returns IsError, doesn't panic.
func TestAskQuestionMissingSessionID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GEMBA_SESSION_ID", "")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")
	res, _, err := handleAskQuestion(context.Background(), &mcp.CallToolRequest{}, AskQuestionInput{
		Role: "coach", Text: "x",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError when GEMBA_SESSION_ID missing")
	}
}

// emit_skill_output happy path — writes one frame containing the full
// lines array. Mode is irrelevant here (skill output is the model's
// primary output channel, not an interruption).
func TestEmitSkillOutputHappyPath(t *testing.T) {
	path := setupSession(t, "balanced")
	res, _, err := handleEmitSkillOutput(context.Background(), &mcp.CallToolRequest{}, EmitSkillOutputInput{
		SkillID: "epic_order",
		Lines: []any{
			map[string]any{"type": "strategy", "model": "claude-opus-4-7"},
			map[string]any{"type": "recommendation", "rank": 0, "epic_id": "gm-e3"},
			map[string]any{"type": "summary"},
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("want success result; got %+v", res)
	}
	f := readSoleFrame(t, path)
	if f.Hook != "GembaSkillOutput" {
		t.Errorf("hook: got %q, want GembaSkillOutput", f.Hook)
	}
	var p skillOutputPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if p.SkillID != "epic_order" {
		t.Errorf("skill_id = %q, want epic_order", p.SkillID)
	}
	if len(p.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(p.Lines))
	}
	// Each line must round-trip: the model emits objects, the frame
	// stores raw JSON, the consumer decodes back to typed lines.
	var first map[string]any
	if err := json.Unmarshal(p.Lines[0], &first); err != nil {
		t.Fatalf("line[0]: %v", err)
	}
	if first["type"] != "strategy" {
		t.Errorf("line[0].type = %v, want strategy", first["type"])
	}
}

// emit_skill_output rejects empty skill_id — the dispatcher would have
// no way to pick the right ValidateOutputLine.
func TestEmitSkillOutputRejectsEmptySkillID(t *testing.T) {
	path := setupSession(t, "balanced")
	res, _, err := handleEmitSkillOutput(context.Background(), &mcp.CallToolRequest{}, EmitSkillOutputInput{
		SkillID: "",
		Lines:   []any{map[string]any{"type": "strategy"}},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError on empty skill_id")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("session log should not exist after rejected call")
	}
}

// emit_skill_output rejects empty lines — an emission with no content
// is meaningless and would produce a zero-row audit entry.
func TestEmitSkillOutputRejectsEmptyLines(t *testing.T) {
	setupSession(t, "balanced")
	res, _, err := handleEmitSkillOutput(context.Background(), &mcp.CallToolRequest{}, EmitSkillOutputInput{
		SkillID: "epic_order",
		Lines:   []any{},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError on empty lines")
	}
}

// emit_skill_output IS allowed in dangerous mode — structured output
// is the model's primary deliverable, not an operator interruption.
func TestEmitSkillOutputAllowedInDangerousMode(t *testing.T) {
	path := setupSession(t, "dangerous")
	res, _, err := handleEmitSkillOutput(context.Background(), &mcp.CallToolRequest{}, EmitSkillOutputInput{
		SkillID: "epic_order",
		Lines:   []any{map[string]any{"type": "strategy"}},
	})
	if err != nil || res.IsError {
		t.Fatalf("emit should be allowed in dangerous: err=%v res=%+v", err, res)
	}
	_ = readSoleFrame(t, path)
}

// emit_skill_output requires GEMBA_SESSION_ID — same env contract as
// every other tool.
func TestEmitSkillOutputMissingSessionID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GEMBA_SESSION_ID", "")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")
	res, _, err := handleEmitSkillOutput(context.Background(), &mcp.CallToolRequest{}, EmitSkillOutputInput{
		SkillID: "epic_order",
		Lines:   []any{map[string]any{"type": "strategy"}},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want IsError when GEMBA_SESSION_ID missing")
	}
}

// Parity check: the frame shape produced by the MCP server is
// structurally identical to what the CLI would emit for the same
// semantic call. Downstream (bridge translator, escalation index)
// must see no difference — that's the whole point of the unified
// channel=tool_call treatment.
func TestMCPAndCLIFrameShapesMatchStructure(t *testing.T) {
	path := setupSession(t, "balanced")
	_, _, _ = handleAskQuestion(context.Background(), &mcp.CallToolRequest{}, AskQuestionInput{
		Role: "coach", Text: "x", BeadID: "gm-42", Title: "quick Q",
	})
	f := readSoleFrame(t, path)
	if f.Hook != "GembaAsk" {
		t.Errorf("hook mismatch: %q want GembaAsk (same as CLI)", f.Hook)
	}
	var p askPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	// Every field the CLI populates must be present.
	if p.Kind != "question" {
		t.Errorf("kind: %q", p.Kind)
	}
	if p.Role != "coach" {
		t.Errorf("role: %q", p.Role)
	}
	if p.Mode != "balanced" {
		t.Errorf("mode: %q", p.Mode)
	}
	if p.Text != "x" {
		t.Errorf("text: %q", p.Text)
	}
	if p.BeadID != "gm-42" {
		t.Errorf("bead_id: %q", p.BeadID)
	}
	if p.Title != "quick Q" {
		t.Errorf("title: %q", p.Title)
	}
}
