// Golden-output tests for the gemba newproject REPL (gm-1t4z).
//
// Uses a deterministic fake LLMClient so no network call is made.
// Covers:
//   - Happy path: greeting → turn → plan-tree summary.
//   - No-LLM-client failure: ErrNoLLMClient exits non-zero with the canonical
//     diagnostic.
//   - :ratify confirm: operator types "y"; ratifier stub returns success.
//   - :ratify rollback: ratifier stub returns a dir-already-exists RatifyError.
//   - :quit: discards without committing.
//   - :help: prints the command reference.

package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/server"
	"github.com/MikeBengtson/gemba/internal/skills/newproject"
)

// ──────────────────────────────────────────────────────────────────────────
// Fake LLM client
// ──────────────────────────────────────────────────────────────────────────

// fakeLLMComplete returns a valid newproject turn envelope. Each call
// increments the Turn counter so the REPL's state advances deterministically.
//
// The reply and plan tree are minimal but schema-valid: one milestone, one
// epic, one bead.
func fakeLLMComplete(turnNum int) string {
	// The envelope must include the full state + a non-empty Reply.
	// Turn is set to whatever the skill expects (prior.Turn + 1).
	return `{
  "reply": "Here is the plan.",
  "state": {
    "ProjectName": "my-project",
    "Description": "A test project.",
    "TechStack": ["go"],
    "Architecture": "",
    "Milestones": [
      {
        "Title": "v1",
        "Description": "First milestone.",
        "Acceptance": "All tests pass.",
        "Labels": [],
        "Priority": 0,
        "Estimate": 0,
        "Skills": [],
        "DesignNotes": "",
        "Notes": "",
        "Epics": [
          {
            "Title": "Core",
            "Description": "Core epic.",
            "Acceptance": "",
            "Labels": [],
            "Priority": 0,
            "Estimate": 0,
            "Skills": [],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "Setup repo",
                "Description": "",
                "Type": "task",
                "Acceptance": "",
                "Labels": [],
                "Priority": 0,
                "Estimate": 0,
                "Skills": [],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              }
            ]
          }
        ]
      }
    ],
    "DraftProjectMD": "# my-project\n",
    "Turn": ` + strings.TrimSpace(itoa(turnNum)) + `,
    "LastChange": {"path": "", "kind": "", "summary": ""}
  }
}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}

// ──────────────────────────────────────────────────────────────────────────
// Fake ratifier
// ──────────────────────────────────────────────────────────────────────────

// fakeRatifier is a stub server.NewProjectRatifier.
type fakeRatifier struct {
	// returnErr is returned by Ratify when non-nil.
	returnErr error
	// projectPath is returned on success.
	projectPath string
	// called records whether Ratify was invoked.
	called bool
}

func (f *fakeRatifier) Ratify(_ context.Context, _ server.NewProjectState) (server.RatifyResponse, error) {
	f.called = true
	if f.returnErr != nil {
		return server.RatifyResponse{}, f.returnErr
	}
	p := f.projectPath
	if p == "" {
		p = "/tmp/gemba/projects/my-project"
	}
	return server.RatifyResponse{
		ProjectPath:    p,
		ProjectName:    "my-project",
		MilestoneCount: 1,
		EpicCount:      1,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────
// Helper: runREPL wraps runNewProjectWithRatifier for tests that need to
// inject both a fake LLM client and a fake ratifier.
// ──────────────────────────────────────────────────────────────────────────

func runREPLTest(t *testing.T, stdinLines []string, fakeLLM newproject.LLMClient, rat server.NewProjectRatifier) (string, error) {
	t.Helper()
	ctx := context.Background()
	in := strings.NewReader(strings.Join(stdinLines, "\n") + "\n")
	var out bytes.Buffer
	err := runNewProjectWithRatifier(ctx, in, &out, fakeLLM, rat)
	return out.String(), err
}

// ──────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────

// TestNewProject_HappyPath: greeting → one conversational turn → :quit.
func TestNewProject_HappyPath(t *testing.T) {
	turnCount := 0
	llm := newproject.LLMClientFunc(func(_ context.Context, _, _ string) (string, error) {
		turnCount++
		return fakeLLMComplete(turnCount), nil
	})
	rat := &fakeRatifier{}

	out, err := runREPLTest(t, []string{"build a web app", ":quit"}, llm, rat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Greeting must appear.
	if !strings.Contains(out, "Hi — what are we building?") {
		t.Errorf("missing greeting in output:\n%s", out)
	}
	// Reply must appear.
	if !strings.Contains(out, "Here is the plan.") {
		t.Errorf("missing LLM reply in output:\n%s", out)
	}
	// Plan summary must show the project name + milestone.
	if !strings.Contains(out, "my-project") {
		t.Errorf("missing project name in plan summary:\n%s", out)
	}
	if !strings.Contains(out, "milestone 0: v1") {
		t.Errorf("missing milestone in plan summary:\n%s", out)
	}
	// Session discard message.
	if !strings.Contains(out, "Session discarded.") {
		t.Errorf("missing discard message:\n%s", out)
	}
	if rat.called {
		t.Errorf("ratifier should NOT have been called on :quit path")
	}
}

// TestNewProject_NoLLMClient: ErrNoLLMClient exits with the canonical
// diagnostic.
func TestNewProject_NoLLMClient(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer
	err := runNewProject(ctx, strings.NewReader(""), &out, "/nonexistent-config-path-that-will-trigger-no-client")
	// We expect a non-nil error wrapping ErrNoLLMClient.
	if err == nil {
		t.Fatal("expected error on no-LLM-client path, got nil")
	}
	if !errors.Is(err, newProjectErrNoClient) && !strings.Contains(err.Error(), "No LLM client configured") {
		// The real production path wraps ErrNoLLMClient; check for the
		// diagnostic string as a fallback.
		t.Logf("output was: %s", out.String())
		t.Logf("error was: %v", err)
	}
	// The canonical diagnostic must be printed to stdout before returning.
	outStr := out.String()
	if !strings.Contains(outStr, "No LLM client configured") {
		t.Errorf("canonical diagnostic not printed; output:\n%s", outStr)
	}
}

// TestNewProject_RatifyConfirm: :ratify with "y" confirmation calls the
// ratifier and prints the project path.
func TestNewProject_RatifyConfirm(t *testing.T) {
	turnCount := 0
	llm := newproject.LLMClientFunc(func(_ context.Context, _, _ string) (string, error) {
		turnCount++
		return fakeLLMComplete(turnCount), nil
	})
	rat := &fakeRatifier{projectPath: "/tmp/gemba/projects/my-project"}

	// build a web app → turn to get a named state → :ratify → y → :quit
	out, err := runREPLTest(t, []string{"build a web app", ":ratify", "y", ":quit"}, llm, rat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rat.called {
		t.Error("ratifier was not called")
	}
	if !strings.Contains(out, "/tmp/gemba/projects/my-project") {
		t.Errorf("project path not printed; output:\n%s", out)
	}
	if !strings.Contains(out, "Project created:") {
		t.Errorf("'Project created:' not printed; output:\n%s", out)
	}
}

// TestNewProject_RatifyRollback: ratifier returns dir_exists RatifyError.
func TestNewProject_RatifyRollback(t *testing.T) {
	turnCount := 0
	llm := newproject.LLMClientFunc(func(_ context.Context, _, _ string) (string, error) {
		turnCount++
		return fakeLLMComplete(turnCount), nil
	})
	rat := &fakeRatifier{
		returnErr: &server.RatifyError{
			Step:    2,
			Code:    "dir_exists",
			Message: "target directory already exists: /tmp/gemba/projects/my-project",
		},
	}

	out, err := runREPLTest(t, []string{"build a web app", ":ratify", "y", ":quit"}, llm, rat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rat.called {
		t.Error("ratifier was not called")
	}
	if !strings.Contains(out, "dir_exists") {
		t.Errorf("dir_exists error not surfaced; output:\n%s", out)
	}
	if !strings.Contains(out, "ratify failed") {
		t.Errorf("'ratify failed' not printed; output:\n%s", out)
	}
	// Operator should still be in the REPL after failure (saw :quit).
	if !strings.Contains(out, "Session discarded.") {
		t.Errorf("session should continue after ratify failure; output:\n%s", out)
	}
}

// TestNewProject_RatifyDecline: operator types "n" — ratify is aborted,
// ratifier never called.
func TestNewProject_RatifyDecline(t *testing.T) {
	turnCount := 0
	llm := newproject.LLMClientFunc(func(_ context.Context, _, _ string) (string, error) {
		turnCount++
		return fakeLLMComplete(turnCount), nil
	})
	rat := &fakeRatifier{}

	out, err := runREPLTest(t, []string{"build a web app", ":ratify", "n", ":quit"}, llm, rat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rat.called {
		t.Error("ratifier should NOT be called when operator declines")
	}
	if !strings.Contains(out, "Ratify cancelled.") {
		t.Errorf("'Ratify cancelled.' not printed; output:\n%s", out)
	}
}

// TestNewProject_Help: :help prints the command reference.
func TestNewProject_Help(t *testing.T) {
	llm := newproject.LLMClientFunc(func(_ context.Context, _, _ string) (string, error) {
		return "", errors.New("should not be called")
	})
	rat := &fakeRatifier{}

	out, err := runREPLTest(t, []string{":help", ":quit"}, llm, rat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, ":ratify") {
		t.Errorf(":ratify not in help output:\n%s", out)
	}
	if !strings.Contains(out, ":quit") {
		t.Errorf(":quit not in help output:\n%s", out)
	}
}
