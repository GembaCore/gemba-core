package persona

import (
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/walk"
)

func TestWalkPromptExtension_EmptyIDReturnsEmpty(t *testing.T) {
	// Non-walk consults must pay nothing. An empty id signals
	// "no active walk" (per walk.IDFromContext semantics) and
	// the helper returns empty so the caller's append is a no-op.
	if got := WalkPromptExtension(""); got != "" {
		t.Errorf("WalkPromptExtension(\"\") = %q, want empty", got)
	}
	if got := WalkPromptExtension("   "); got != "" {
		t.Errorf("WalkPromptExtension(\"   \") = %q, want empty (whitespace-only treated as empty)", got)
	}
}

func TestWalkPromptExtension_ContainsCoreFraming(t *testing.T) {
	got := WalkPromptExtension("walk-123")
	wants := []string{
		"Gemba walk",
		"one agenda item at a time",
		"Frame",
		"Summarize",
		"Propose",
		"Yield",
		"SuggestedAction",
		"Resume context:",
		"walk-123",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("walk extension missing %q:\n%s", w, got)
		}
	}
}

func TestWalkPromptExtension_RendersWalkIDInline(t *testing.T) {
	got := WalkPromptExtension("walk-abc-42")
	if !strings.Contains(got, "Active walk id: walk-abc-42") {
		t.Errorf("expected inline walk id label, got:\n%s", got)
	}
}

func TestCompose_WalkInactiveBypassesExtension(t *testing.T) {
	// PM persona must continue working as-is for non-walk
	// consults — the extension fires only when WalkID is set.
	req := ComposeRequest{
		Persona:  samplePersona(),
		Skill:    fakeSkill{id: "epic_order", prompt: "rank epics"},
		Template: TemplateValues{WorkspaceName: "Gemba"},
		Input:    map[string]any{"k": "v"},
	}
	got, err := Compose(req)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if strings.Contains(got.System, "Gemba walk mode") {
		t.Errorf("non-walk consult must not include walk fragment:\n%s", got.System)
	}
}

func TestCompose_WalkActiveAppendsExtension(t *testing.T) {
	req := ComposeRequest{
		Persona:  samplePersona(),
		Skill:    fakeSkill{id: "epic_order", prompt: "rank epics"},
		Template: TemplateValues{WorkspaceName: "Gemba"},
		Input:    map[string]any{"k": "v"},
		WalkID:   "walk-99",
	}
	got, err := Compose(req)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if !strings.Contains(got.System, "Gemba walk mode") {
		t.Errorf("walk consult must include walk fragment:\n%s", got.System)
	}
	if !strings.Contains(got.System, "Active walk id: walk-99") {
		t.Errorf("walk consult must include walk id label:\n%s", got.System)
	}
	// Persona base prompt is preserved verbatim before the
	// fragment — the extension is additive, never destructive.
	if !strings.Contains(got.System, "You are Gemba's") {
		t.Errorf("walk consult dropped persona base prompt:\n%s", got.System)
	}
	// Base prompt must come before the walk fragment so the
	// model reads the persona's identity before its mode.
	baseIdx := strings.Index(got.System, "You are Gemba's")
	walkIdx := strings.Index(got.System, "Gemba walk mode")
	if baseIdx < 0 || walkIdx < 0 || baseIdx >= walkIdx {
		t.Errorf("walk fragment must come after persona base prompt (base=%d, walk=%d):\n%s",
			baseIdx, walkIdx, got.System)
	}
}

func TestRenderResumePrompt_AlwaysEmitsPreambleAndDivider(t *testing.T) {
	w := walk.Walk{
		ID:          "walk-1",
		Workspace:   "gemba",
		InitiatedBy: "mike",
		Status:      walk.StatusPaused,
		StartedAt:   time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
	}
	got := RenderResumePrompt(w, "let's keep going")
	if !strings.HasPrefix(got, "Resume context:") {
		t.Errorf("output must start with preamble, got:\n%s", got)
	}
	if !strings.Contains(got, "---\n") {
		t.Errorf("output must contain divider, got:\n%s", got)
	}
	if !strings.Contains(got, "let's keep going") {
		t.Errorf("output must contain user message, got:\n%s", got)
	}
	// Synopsis is BuildResumeContext output — pin a load-bearing
	// substring to confirm the helper actually delegated.
	if !strings.Contains(got, "Walk walk-1") {
		t.Errorf("output missing BuildResumeContext synopsis, got:\n%s", got)
	}
}

func TestRenderResumePrompt_EmptyUserMsg(t *testing.T) {
	// "Pick up where you left off" with no new content is a
	// real path — operator clicks Resume and the persona
	// produces the next agenda-item frame on its own.
	w := walk.Walk{
		ID:        "walk-1",
		Workspace: "gemba",
		Status:    walk.StatusPaused,
		StartedAt: time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
	}
	got := RenderResumePrompt(w, "")
	if !strings.HasPrefix(got, "Resume context:") {
		t.Errorf("output must start with preamble even with empty user msg, got:\n%s", got)
	}
	if !strings.Contains(got, "---\n") {
		t.Errorf("output must contain divider even with empty user msg, got:\n%s", got)
	}
	// No user message body after the divider beyond the trailing newline.
	tail := strings.SplitN(got, "---\n", 2)
	if len(tail) == 2 && strings.TrimSpace(tail[1]) != "" {
		t.Errorf("empty user msg should leave nothing after divider, got %q", tail[1])
	}
}

func TestRenderResumePrompt_TrimsUserMsgWhitespace(t *testing.T) {
	w := walk.Walk{
		ID:        "walk-1",
		Workspace: "gemba",
		Status:    walk.StatusPaused,
		StartedAt: time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
	}
	got := RenderResumePrompt(w, "   \n\thello   ")
	if !strings.Contains(got, "hello\n") {
		t.Errorf("user msg whitespace not trimmed:\n%s", got)
	}
	if strings.Contains(got, "\thello") {
		t.Errorf("leading whitespace leaked through:\n%s", got)
	}
}

func TestRenderResumePrompt_IncludesTranscriptTail(t *testing.T) {
	w := walk.Walk{
		ID:          "walk-1",
		Workspace:   "gemba",
		InitiatedBy: "mike",
		Status:      walk.StatusPaused,
		StartedAt:   time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
		Transcript: []walk.WalkTurn{
			{
				At:      time.Date(2026, 4, 26, 10, 5, 0, 0, time.UTC),
				Speaker: walk.SpeakerUser,
				Content: "let's tackle the polecat escalation first",
			},
			{
				At:        time.Date(2026, 4, 26, 10, 6, 0, 0, time.UTC),
				Speaker:   walk.SpeakerPersona,
				PersonaID: "project-manager",
				Content:   "agenda item 1: polecat onyx dirty worktree",
			},
		},
	}
	got := RenderResumePrompt(w, "ok continue")
	if !strings.Contains(got, "polecat escalation") {
		t.Errorf("transcript tail must appear in synopsis:\n%s", got)
	}
	if !strings.Contains(got, "agenda item 1") {
		t.Errorf("persona turn must appear in synopsis:\n%s", got)
	}
}
