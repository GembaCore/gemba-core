package onboarder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/internal/skills/newproject"
)

// fakeClient is a deterministic test double for newproject.LLMClient.
// Mirrors the one in internal/skills/newproject/skill_test.go; we
// can't import it (it's _test) so we redeclare the same shape.
type fakeClient struct {
	mu        sync.Mutex
	responses []string
	calls     []fakeCall
	idx       int
	err       error
}

type fakeCall struct {
	system string
	user   string
}

func (f *fakeClient) Complete(_ context.Context, system, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{system: system, user: user})
	if f.err != nil {
		return "", f.err
	}
	if f.idx >= len(f.responses) {
		return "", errors.New("fakeClient: no more canned responses")
	}
	r := f.responses[f.idx]
	f.idx++
	return r, nil
}

// fullPlan returns a small valid plan tree the validator accepts.
// Stays in lock-step with the skill's wire shape; if the schema
// changes, this fixture is the canonical place to update.
func fullPlan() newproject.NewProjectState {
	return newproject.NewProjectState{
		ProjectName:  "demo",
		Description:  "Demo project",
		TechStack:    []string{"go"},
		Architecture: "Single binary",
		Milestones: []newproject.DraftMilestone{{
			Title:       "M1",
			Description: "milestone 1",
			Acceptance:  "M1 testable",
			Labels:      []string{"area:demo"},
			Priority:    0,
			Estimate:    60,
			Skills:      []string{"go"},
			Epics: []newproject.DraftEpic{{
				Title:       "E1",
				Description: "epic 1",
				Acceptance:  "E1 testable",
				Labels:      []string{},
				Priority:    0,
				Estimate:    30,
				Skills:      []string{"go"},
				Beads: []newproject.DraftBead{{
					Title:         "B1",
					Description:   "bead 1",
					Type:          "feature",
					Acceptance:    "B1 ok",
					Labels:        []string{},
					Priority:      0,
					Estimate:      15,
					Skills:        []string{"go"},
					DependsOnRefs: []string{},
					BlocksRefs:    []string{},
				}},
			}},
		}},
		DraftProjectMD: "# demo\n",
		Turn:           1,
		LastChange:     newproject.ChangeRef{Kind: newproject.ChangeKindAdded, Summary: "drafted m1"},
	}
}

// envelope encodes a plan + reply into the JSON envelope shape the
// skill's decoder expects.
func envelope(t *testing.T, state newproject.NewProjectState, reply string) string {
	t.Helper()
	b, err := json.Marshal(struct {
		State newproject.NewProjectState `json:"state"`
		Reply string                     `json:"reply"`
	}{State: state, Reply: reply})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(b)
}

// TestSpawn_HappyPath: a working resolver returns a Persona that
// can run conversational turns. Discard is a no-op and idempotent.
func TestSpawn_HappyPath(t *testing.T) {
	t.Parallel()

	resp1 := envelope(t, fullPlan(), "Drafted milestone 1.")
	plan2 := fullPlan()
	plan2.Milestones[0].Epics = append(plan2.Milestones[0].Epics, newproject.DraftEpic{
		Title:       "E2",
		Description: "epic 2",
		Acceptance:  "ok",
		Labels:      []string{},
		Skills:      []string{},
		// Every epic requires at least one bead per the validator
		// (epics roll up child beads). Single-bead is the minimum
		// valid shape.
		Beads: []newproject.DraftBead{{
			Title:         "B-new",
			Description:   "added in turn 2",
			Type:          "task",
			Acceptance:    "ok",
			Labels:        []string{},
			Skills:        []string{},
			DependsOnRefs: []string{},
			BlocksRefs:    []string{},
		}},
	})
	plan2.Turn = 2
	resp2 := envelope(t, plan2, "Added epic 2.")

	client := &fakeClient{responses: []string{resp1, resp2}}
	resolver := func(_ context.Context) (newproject.LLMClient, error) {
		return client, nil
	}

	p, err := Spawn(context.Background(), resolver)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if p == nil {
		t.Fatal("Spawn returned nil persona without error")
	}

	// First turn.
	got, err := p.Turn(context.Background(), newproject.EmptyState(), "build a TODO app")
	if err != nil {
		t.Fatalf("Turn 1: %v", err)
	}
	if got.State.Turn != 1 {
		t.Errorf("Turn 1 state turn: got %d, want 1", got.State.Turn)
	}

	// Second turn — uses the same Persona, demonstrating
	// "spawn → run multiple turns → discard".
	got2, err := p.Turn(context.Background(), got.State, "add an epic")
	if err != nil {
		t.Fatalf("Turn 2: %v", err)
	}
	if got2.State.Turn != 2 {
		t.Errorf("Turn 2 state turn: got %d, want 2", got2.State.Turn)
	}
	if len(got2.State.Milestones[0].Epics) != 2 {
		t.Errorf("expected 2 epics after turn 2; got %d", len(got2.State.Milestones[0].Epics))
	}

	// Discard is a no-op and idempotent.
	p.Discard()
	p.Discard()
}

// TestSpawn_NoClient: a resolver returning ErrNoLLMClient surfaces
// as the canonical diagnostic; IsNoClient classifies it.
func TestSpawn_NoClient(t *testing.T) {
	t.Parallel()

	resolver := func(_ context.Context) (newproject.LLMClient, error) {
		return nil, ErrNoLLMClient
	}
	_, err := Spawn(context.Background(), resolver)
	if err == nil {
		t.Fatal("expected error when resolver reports no client")
	}
	if !IsNoClient(err) {
		t.Errorf("IsNoClient: false; want true (err=%v)", err)
	}
	if !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("expected diagnostic to mention config.toml; got %q", err.Error())
	}
}

// TestSpawn_NilResolver: defensive — a nil resolver is rejected up
// front rather than dereferenced.
func TestSpawn_NilResolver(t *testing.T) {
	t.Parallel()
	_, err := Spawn(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil resolver")
	}
}

// TestSpawn_NilClientFromResolver: a resolver that returns
// (nil, nil) is treated as no-client.
func TestSpawn_NilClientFromResolver(t *testing.T) {
	t.Parallel()
	r := func(_ context.Context) (newproject.LLMClient, error) {
		return nil, nil
	}
	_, err := Spawn(context.Background(), r)
	if !IsNoClient(err) {
		t.Errorf("expected IsNoClient on (nil,nil) resolver result; got %v", err)
	}
}

// TestSpawnWithClient: bypassing the resolver. Used by tests and
// terminal interactive mode.
func TestSpawnWithClient(t *testing.T) {
	t.Parallel()
	client := &fakeClient{responses: []string{envelope(t, fullPlan(), "ok")}}
	p, err := SpawnWithClient(client)
	if err != nil {
		t.Fatalf("SpawnWithClient: %v", err)
	}
	if p == nil {
		t.Fatal("nil persona")
	}

	if _, err := SpawnWithClient(nil); !IsNoClient(err) {
		t.Errorf("expected IsNoClient for nil client; got %v", err)
	}
}

// TestTurn_ValidationRetryOnce: the first response is malformed
// (decode-fail); the persona retries once with the validator
// complaint embedded in the user message; the second response is
// valid; the operator sees a clean success.
func TestTurn_ValidationRetryOnce(t *testing.T) {
	t.Parallel()

	good := envelope(t, fullPlan(), "Drafted.")
	// Malformed: trailing prose after the JSON object trips the
	// strict decoder, which the skill flags as a ValidationError.
	bad := good + " trailing prose that breaks strict decoding"

	client := &fakeClient{responses: []string{bad, good}}
	p, err := SpawnWithClient(client)
	if err != nil {
		t.Fatalf("SpawnWithClient: %v", err)
	}

	got, err := p.Turn(context.Background(), newproject.EmptyState(), "build a thing")
	if err != nil {
		t.Fatalf("Turn after retry: %v", err)
	}
	if got.State.Turn != 1 {
		t.Errorf("turn: got %d, want 1", got.State.Turn)
	}

	// Confirm the retry actually fired (two Complete calls).
	if len(client.calls) != 2 {
		t.Fatalf("expected 2 Complete calls (initial + retry); got %d", len(client.calls))
	}
	// The retry's user prompt MUST include the retry hint and the
	// original operator message.
	retryUser := client.calls[1].user
	if !strings.Contains(retryUser, "[onboarder retry]") {
		t.Errorf("retry user prompt missing retry banner; got: %s", retryUser)
	}
	if !strings.Contains(retryUser, "build a thing") {
		t.Errorf("retry user prompt missing original operator message; got: %s", retryUser)
	}
}

// TestTurn_ValidationFailsTwice: when both attempts fail validation
// the wrapped error preserves IsValidationError so the HTTP layer
// can distinguish from transport errors.
func TestTurn_ValidationFailsTwice(t *testing.T) {
	t.Parallel()

	bad := envelope(t, fullPlan(), "ok") + " trailing prose"
	client := &fakeClient{responses: []string{bad, bad}}
	p, err := SpawnWithClient(client)
	if err != nil {
		t.Fatalf("SpawnWithClient: %v", err)
	}

	_, err = p.Turn(context.Background(), newproject.EmptyState(), "x")
	if err == nil {
		t.Fatal("expected error after two validation failures")
	}
	if !newproject.IsValidationError(err) {
		t.Errorf("expected IsValidationError on persistent failure; got %T %v", err, err)
	}
}

// TestTurn_TransportErrorNotRetried: a transport-class error
// (network, auth) bubbles up immediately without a retry.
func TestTurn_TransportErrorNotRetried(t *testing.T) {
	t.Parallel()

	client := &fakeClient{err: errors.New("network: dial timeout")}
	p, err := SpawnWithClient(client)
	if err != nil {
		t.Fatalf("SpawnWithClient: %v", err)
	}
	_, err = p.Turn(context.Background(), newproject.EmptyState(), "x")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if newproject.IsValidationError(err) {
		t.Errorf("transport error misclassified as validation: %v", err)
	}
	if len(client.calls) != 1 {
		t.Errorf("transport error should not retry; got %d calls", len(client.calls))
	}
}
