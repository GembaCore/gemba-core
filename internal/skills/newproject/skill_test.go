package newproject

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeClient is a deterministic LLMClient. The test seeds it with
// a sequence of canned responses; each Complete call pops the next
// one and asserts the system / user prompts pass through unchanged.
type fakeClient struct {
	responses []string
	calls     []fakeCall
	idx       int
}

type fakeCall struct {
	system string
	user   string
}

func (f *fakeClient) Complete(_ context.Context, system, user string) (string, error) {
	f.calls = append(f.calls, fakeCall{system: system, user: user})
	if f.idx >= len(f.responses) {
		return "", errors.New("fakeClient: no more canned responses")
	}
	r := f.responses[f.idx]
	f.idx++
	return r, nil
}

// errorClient always returns the given transport error. Used to
// confirm Run distinguishes infra failures from validation
// failures.
type errorClient struct{ err error }

func (e *errorClient) Complete(_ context.Context, _ string, _ string) (string, error) {
	return "", e.err
}

// fullPlan is a small valid plan used across happy-path tests. One
// milestone, one epic, two beads, one intra-tree dep.
func fullPlan() NewProjectState {
	return NewProjectState{
		ProjectName:  "demo",
		Description:  "Demo project",
		TechStack:    []string{"go"},
		Architecture: "Single binary",
		Milestones: []DraftMilestone{{
			Title:       "M1",
			Description: "milestone 1",
			Acceptance:  "M1 is testable",
			Labels:      []string{"area:demo"},
			Priority:    0,
			Estimate:    60,
			Skills:      []string{"go"},
			DesignNotes: "",
			Notes:       "",
			Epics: []DraftEpic{{
				Title:       "E1",
				Description: "epic 1",
				Acceptance:  "E1 testable",
				Labels:      []string{},
				Priority:    0,
				Estimate:    30,
				Skills:      []string{"go"},
				DesignNotes: "",
				Notes:       "",
				Beads: []DraftBead{
					{
						Title:         "B1",
						Description:   "bead 1",
						Type:          "feature",
						Acceptance:    "B1 ok",
						Labels:        []string{},
						Priority:      0,
						Estimate:      15,
						Skills:        []string{"go"},
						DesignNotes:   "",
						Notes:         "",
						DependsOnRefs: []string{},
						BlocksRefs:    []string{},
					},
					{
						Title:         "B2",
						Description:   "bead 2",
						Type:          "task",
						Acceptance:    "B2 ok",
						Labels:        []string{},
						Priority:      1,
						Estimate:      15,
						Skills:        []string{"go"},
						DesignNotes:   "",
						Notes:         "",
						DependsOnRefs: []string{"milestone:0/epic:0/bead:0"},
						BlocksRefs:    []string{},
					},
				},
			}},
		}},
		DraftProjectMD: "# demo\n",
		Turn:           1,
		LastChange:     ChangeRef{Path: "", Kind: ChangeKindAdded, Summary: "drafted m1"},
	}
}

// envelopeJSON marshals state + reply into the wire envelope the
// LLM is supposed to produce. Tests use this so they don't have
// to hand-write JSON.
func envelopeJSON(t *testing.T, state NewProjectState, reply string) string {
	t.Helper()
	env := turnEnvelope{State: state, Reply: reply}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(b)
}

// TestRun_FirstTurn_HappyPath confirms the canonical first-turn
// flow: empty prior state in, full plan + reply out, Turn
// incremented to 1.
func TestRun_FirstTurn_HappyPath(t *testing.T) {
	t.Parallel()

	state := fullPlan()
	state.Turn = 99 // model emits whatever; Run forces to prior+1.
	resp := envelopeJSON(t, state, "Drafted milestone 1.")

	client := &fakeClient{responses: []string{resp}}
	prior := EmptyState()

	got, err := Run(context.Background(), prior, "I want to build a demo", client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.State.Turn != 1 {
		t.Fatalf("Turn: got %d, want 1", got.State.Turn)
	}
	if got.Reply == "" {
		t.Fatal("Reply must not be empty")
	}
	if len(got.State.Milestones) != 1 {
		t.Fatalf("Milestones: got %d, want 1", len(got.State.Milestones))
	}
	if len(client.calls) != 1 {
		t.Fatalf("Complete calls: got %d, want 1", len(client.calls))
	}
	if !strings.Contains(client.calls[0].user, "I want to build a demo") {
		t.Fatal("user prompt missing operator message")
	}
	// System prompt should reference the schema by name -- the
	// few-shots embed the schema labels directly.
	if !strings.Contains(client.calls[0].system, "DraftMilestone") {
		t.Fatal("system prompt missing DraftMilestone schema reference")
	}
}

// TestRun_FirstTurn_AllowsEmptyPlan: turn 0 -> turn 1 with a model
// that just asked a clarifying question (no milestones yet) is
// allowed.
func TestRun_FirstTurn_AllowsEmptyPlan(t *testing.T) {
	t.Parallel()

	state := EmptyState()
	state.Turn = 1
	state.LastChange = ChangeRef{Kind: ""}
	resp := envelopeJSON(t, state, "What kind of project?")

	client := &fakeClient{responses: []string{resp}}
	got, err := Run(context.Background(), EmptyState(), "", client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.State.Turn != 1 {
		t.Fatalf("Turn: got %d, want 1", got.State.Turn)
	}
	if len(got.State.Milestones) != 0 {
		t.Fatalf("Milestones: got %d, want 0", len(got.State.Milestones))
	}
}

// TestRun_AfterFirstTurn_RequiresMilestones: turn 1 -> turn 2
// with an empty plan is a regression and must fail validation.
func TestRun_AfterFirstTurn_RequiresMilestones(t *testing.T) {
	t.Parallel()

	state := EmptyState()
	state.Turn = 2
	resp := envelopeJSON(t, state, "Lost the plan, oops.")

	client := &fakeClient{responses: []string{resp}}
	prior := fullPlan() // Turn=1 already.

	_, err := Run(context.Background(), prior, "more", client)
	if err == nil {
		t.Fatal("expected validation error on empty plan after turn 1")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

// TestRun_MidConversationEdit verifies the diff-rederivation
// pattern: the operator asks to drop epic 0; the model emits an
// updated plan minus that epic, with LastChange describing the
// removal.
func TestRun_MidConversationEdit(t *testing.T) {
	t.Parallel()

	prior := fullPlan()
	// Add a second epic so the rename has somewhere to go.
	prior.Milestones[0].Epics = append(prior.Milestones[0].Epics, DraftEpic{
		Title:       "E2",
		Description: "epic 2",
		Acceptance:  "E2",
		Labels:      []string{},
		Priority:    1,
		Estimate:    30,
		Skills:      []string{"go"},
		DesignNotes: "",
		Notes:       "",
		Beads: []DraftBead{{
			Title:         "B3",
			Description:   "bead 3",
			Type:          "feature",
			Acceptance:    "B3 ok",
			Labels:        []string{},
			Priority:      1,
			Estimate:      15,
			Skills:        []string{"go"},
			DesignNotes:   "",
			Notes:         "",
			DependsOnRefs: []string{},
			BlocksRefs:    []string{},
		}},
	})

	// Model removes epic 0; its child beads are gone too. The
	// surviving E2 ref-walks remain valid because B3's
	// DependsOnRefs was empty.
	updated := fullPlan()
	updated.Milestones[0].Epics = updated.Milestones[0].Epics[:0] // empty out
	updated.Milestones[0].Epics = append(updated.Milestones[0].Epics, prior.Milestones[0].Epics[1])
	updated.Turn = 2
	updated.LastChange = ChangeRef{
		Path:    "milestone:0/epic:0",
		Kind:    ChangeKindRemoved,
		Summary: "Dropped epic E1",
	}
	resp := envelopeJSON(t, updated, "Dropped epic 1.")

	client := &fakeClient{responses: []string{resp}}
	got, err := Run(context.Background(), prior, "drop epic 1", client)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got.State.Milestones[0].Epics) != 1 {
		t.Fatalf("Epics after drop: got %d, want 1", len(got.State.Milestones[0].Epics))
	}
	if got.State.LastChange.Kind != ChangeKindRemoved {
		t.Fatalf("LastChange.Kind: got %q, want %q", got.State.LastChange.Kind, ChangeKindRemoved)
	}
	if got.State.LastChange.Path != "milestone:0/epic:0" {
		t.Fatalf("LastChange.Path: got %q, want milestone:0/epic:0", got.State.LastChange.Path)
	}
	if got.State.Turn != 2 {
		t.Fatalf("Turn: got %d, want 2", got.State.Turn)
	}
}

// TestRun_ValidationFailureSurfacedAsRetryable confirms that a
// model emitting an invalid plan returns an error
// IsValidationError can detect, and that the host (in a higher
// layer) can therefore retry rather than poison the tree.
func TestRun_ValidationFailureSurfacedAsRetryable(t *testing.T) {
	t.Parallel()

	bad := fullPlan()
	bad.Milestones[0].Epics[0].Beads[0].Title = "" // empty title.
	resp := envelopeJSON(t, bad, "here")

	client := &fakeClient{responses: []string{resp}}
	_, err := Run(context.Background(), EmptyState(), "go", client)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Beads[0].Title") {
		t.Fatalf("error should pinpoint the field, got: %v", err)
	}
}

// TestRun_TransportErrorIsNotValidation confirms the host can
// distinguish skill bugs (validation) from infra failures
// (transport) by error type.
func TestRun_TransportErrorIsNotValidation(t *testing.T) {
	t.Parallel()

	want := errors.New("rate limited")
	_, err := Run(context.Background(), EmptyState(), "go", &errorClient{err: want})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if IsValidationError(err) {
		t.Fatalf("transport error should not be ValidationError, got: %v", err)
	}
	if !errors.Is(err, want) {
		t.Fatalf("error should wrap the transport cause, got: %v", err)
	}
}

// TestRun_NilClient guards the "host forgot to inject a client"
// case so it surfaces as a clear error rather than a panic.
func TestRun_NilClient(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), EmptyState(), "go", nil)
	if err == nil {
		t.Fatal("expected error on nil client")
	}
}

// TestRun_RejectsMissingReply ensures the host sees a structured
// failure when the model omits the conversational reply.
func TestRun_RejectsMissingReply(t *testing.T) {
	t.Parallel()

	state := fullPlan()
	resp := envelopeJSON(t, state, "")

	client := &fakeClient{responses: []string{resp}}
	_, err := Run(context.Background(), EmptyState(), "go", client)
	if err == nil {
		t.Fatal("expected validation error for empty reply")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

// TestDecodeEnvelope_StripsCodeFence: the model occasionally
// disobeys the "no fences" rule. Confirm we tolerate a single
// leading ```json wrapper.
func TestDecodeEnvelope_StripsCodeFence(t *testing.T) {
	t.Parallel()

	state := fullPlan()
	body := envelopeJSON(t, state, "ok")
	wrapped := "```json\n" + body + "\n```"

	client := &fakeClient{responses: []string{wrapped}}
	got, err := Run(context.Background(), EmptyState(), "go", client)
	if err != nil {
		t.Fatalf("Run with fenced response: %v", err)
	}
	if got.State.Turn != 1 {
		t.Fatalf("Turn: got %d, want 1", got.State.Turn)
	}
}

// TestDecodeEnvelope_RejectsTrailingProse: the model emits the
// JSON envelope plus a chatty postscript; we reject that as a
// validation error.
func TestDecodeEnvelope_RejectsTrailingProse(t *testing.T) {
	t.Parallel()

	state := fullPlan()
	body := envelopeJSON(t, state, "ok") + "\n\nThanks!"

	client := &fakeClient{responses: []string{body}}
	_, err := Run(context.Background(), EmptyState(), "go", client)
	if err == nil {
		t.Fatal("expected error for trailing prose")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

// TestDecodeEnvelope_RejectsUnknownFields: a model that
// hallucinates an extra top-level key fails strict decoding.
func TestDecodeEnvelope_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	body := `{"state": {}, "reply": "x", "extra": true}`
	client := &fakeClient{responses: []string{body}}
	_, err := Run(context.Background(), EmptyState(), "go", client)
	if err == nil {
		t.Fatal("expected error on unknown field")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}
