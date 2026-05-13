package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// stubBD records the args of every bd shellout and returns canned output
// per call. Used to verify the analyze --apply path without exec'ing bd.
type stubBD struct {
	mu    sync.Mutex
	calls [][]string
	// showLabels is what `bd show <id> --json` returns
	showLabels []string
	// nextID generator for `bd create`
	createSeq int
}

func (s *stubBD) exec(ctx context.Context, args ...string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string(nil), args...))
	if len(args) >= 1 && args[0] == "show" {
		// args: show <id> --json
		labels := s.showLabels
		if labels == nil {
			labels = []string{}
		}
		b := `{"labels":[`
		for i, l := range labels {
			if i > 0 {
				b += ","
			}
			b += `"` + l + `"`
		}
		b += `]}`
		return b, nil
	}
	if len(args) >= 1 && args[0] == "create" {
		s.createSeq++
		id := fmt.Sprintf("gm-test.%d", s.createSeq)
		return `{"id":"` + id + `"}`, nil
	}
	return "", nil
}

func TestSpecAnalyze_DryRunTable(t *testing.T) {
	t.Setenv("GEMBA_REJECTED_PAYLOAD", "")
	cmd := newSpecAnalyzeCmd()
	cmd.SetIn(strings.NewReader(`# Demo
## A
- [ ] task one
- [ ] [P0] task two
`))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--input", "-"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "source: heuristic") {
		t.Fatalf("missing source line: %s", out)
	}
	if !strings.Contains(out, "milestone") || !strings.Contains(out, "epic") || !strings.Contains(out, "story") {
		t.Fatalf("missing kinds in table: %s", out)
	}
	if !strings.Contains(out, "P0") {
		t.Fatalf("priority not surfaced: %s", out)
	}
}

func TestSpecAnalyze_ApplyRequiresConfirm(t *testing.T) {
	t.Setenv("GEMBA_REJECTED_PAYLOAD", "")
	cmd := newSpecAnalyzeCmd()
	cmd.SetIn(strings.NewReader("- [ ] a\n"))
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--input", "-", "--apply"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error when --apply lacks --confirm")
	}
	if !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpecAnalyze_ApplyShellsOutWithCumulativeLabels(t *testing.T) {
	t.Setenv("GEMBA_REJECTED_PAYLOAD", "")
	s := &stubBD{showLabels: []string{"gemba-remote", "architecture", "decision"}}
	orig := bdExec
	bdExec = s.exec
	t.Cleanup(func() { bdExec = orig })

	cmd := newSpecAnalyzeCmd()
	cmd.SetIn(strings.NewReader(`# Big rollout
## Epic A
- [ ] story A1
`))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--input", "-",
		"--apply", "--confirm",
		"--parent", "gm-v0sp",
		"--initiative", "gemba-remote",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, stderr.String())
	}
	// Expect: 1 show + 3 create calls (milestone, epic, story)
	if len(s.calls) != 4 {
		t.Fatalf("expected 4 bd calls, got %d: %+v", len(s.calls), s.calls)
	}
	if s.calls[0][0] != "show" || s.calls[0][1] != "gm-v0sp" {
		t.Fatalf("first call should be `show gm-v0sp`, got %+v", s.calls[0])
	}
	// Every create call must include the cumulative labels:
	//   parent labels (gemba-remote, architecture, decision) +
	//   kind label + analyzer-generated + initiative (gemba-remote already in)
	required := map[string]bool{
		"gemba-remote":       false,
		"architecture":       false,
		"analyzer-generated": false,
	}
	for _, args := range s.calls[1:] {
		for k := range required {
			required[k] = false
		}
		for i := 0; i < len(args); i++ {
			if args[i] == "--label" && i+1 < len(args) {
				if _, ok := required[args[i+1]]; ok {
					required[args[i+1]] = true
				}
			}
		}
		for k, v := range required {
			if !v {
				t.Fatalf("create call %+v missing required label %q", args, k)
			}
		}
	}
	// First create (milestone) parents to gm-v0sp (the external parent).
	if !containsPair(s.calls[1], "--parent", "gm-v0sp") {
		t.Fatalf("milestone should parent to gm-v0sp, got %+v", s.calls[1])
	}
	// Second create (epic) parents to the milestone id returned by stub (gm-test.1).
	if !containsPair(s.calls[2], "--parent", "gm-test.1") {
		t.Fatalf("epic should parent to gm-test.1, got %+v", s.calls[2])
	}
	// Third create (story) parents to the epic (gm-test.2).
	if !containsPair(s.calls[3], "--parent", "gm-test.2") {
		t.Fatalf("story should parent to gm-test.2, got %+v", s.calls[3])
	}
	// Title prefix sanity
	if !containsPair(s.calls[1], "--type", "milestone") {
		t.Fatalf("expected --type milestone in %+v", s.calls[1])
	}
}

func containsPair(args []string, flag, val string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
