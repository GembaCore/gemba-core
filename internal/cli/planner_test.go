package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
)

func TestConflictsCmd_EmptyInputPrintsNoEdges(t *testing.T) {
	cmd := newConflictsCmd()
	cmd.SetIn(strings.NewReader(`{"beads":[]}`))
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := stdout.String()
	if !strings.Contains(body, "0 beads, 0 edges") {
		t.Errorf("missing zero-edge banner: %s", body)
	}
}

func TestConflictsCmd_TargetOverlapEmitsEdge(t *testing.T) {
	in := PlannerInput{Beads: []PlannerInputBead{
		{ID: "gm-1", GlobPatterns: []targets.Pattern{"src/auth.go"}},
		{ID: "gm-2", GlobPatterns: []targets.Pattern{"src/auth.go"}},
	}}
	body := runCmdGetOutput(t, newConflictsCmd(), in)
	if !strings.Contains(body, "gm-1 -- gm-2") {
		t.Errorf("expected gm-1↔gm-2 edge, got: %s", body)
	}
	if !strings.Contains(body, "target_overlap") {
		t.Errorf("expected target_overlap reason, got: %s", body)
	}
}

func TestConflictsCmd_WorkspaceCollisionEmitsEdge(t *testing.T) {
	in := PlannerInput{Beads: []PlannerInputBead{
		{ID: "gm-1", Repositories: []string{"gemba"}, Branch: "main"},
		{ID: "gm-2", Repositories: []string{"gemba"}, Branch: "main"},
	}}
	body := runCmdGetOutput(t, newConflictsCmd(), in)
	if !strings.Contains(body, "Workspace-collision detail") {
		t.Errorf("expected workspace-collision section: %s", body)
	}
	if !strings.Contains(body, "same repo+branch") {
		t.Errorf("expected workspace reason: %s", body)
	}
}

func TestConflictsCmd_LiveSessionFlagsBead(t *testing.T) {
	in := PlannerInput{
		Beads: []PlannerInputBead{
			{ID: "gm-1", Repositories: []string{"gemba"}, Branch: "main"},
		},
		SessionContexts: []planner.OperationalContext{{
			Session:   &core.Session{ID: "sess-active"},
			Workspace: &core.Workspace{Repository: "gemba", Branch: "main"},
		}},
	}
	body := runCmdGetOutput(t, newConflictsCmd(), in)
	if !strings.Contains(body, "live sess-active") {
		t.Errorf("expected live-session marker: %s", body)
	}
}

func TestConflictsCmd_BatchesPrinted(t *testing.T) {
	in := PlannerInput{Beads: []PlannerInputBead{
		{ID: "gm-1", GlobPatterns: []targets.Pattern{"a.go"}},
		{ID: "gm-2", GlobPatterns: []targets.Pattern{"b.go"}},
		{ID: "gm-3", GlobPatterns: []targets.Pattern{"a.go"}},
	}}
	body := runCmdGetOutput(t, newConflictsCmd(), in)
	if !strings.Contains(body, "Parallel-safe batches") {
		t.Errorf("expected batches section: %s", body)
	}
}

func TestConflictsCmd_JSONEnvelopeStable(t *testing.T) {
	in := PlannerInput{Beads: []PlannerInputBead{
		{ID: "gm-1", Repositories: []string{"gemba"}, Branch: "main"},
		{ID: "gm-2", Repositories: []string{"gemba"}, Branch: "main"},
	}}
	body := runCmdGetOutputWithFlags(t, newConflictsCmd(), in, []string{"--json"})
	var env PlannerJSONOut
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal --json output: %v\n%s", err, body)
	}
	if env.Conflicts == nil {
		t.Fatal("expected env.Conflicts to be non-nil")
	}
	if len(env.Conflicts.WorkspaceCollisions) == 0 {
		t.Errorf("expected workspace_collisions in --json output")
	}
}

func TestAffinityCmd_NoSessionsPrintsHeaderOnly(t *testing.T) {
	in := PlannerInput{Beads: []PlannerInputBead{{ID: "gm-1"}}}
	body := runCmdGetOutput(t, newAffinityCmd(), in)
	if !strings.Contains(body, "concept") {
		t.Errorf("expected header row: %s", body)
	}
	if strings.Contains(body, "gm-1 ") {
		t.Errorf("no sessions seeded → no rows expected: %s", body)
	}
}

func TestAffinityCmd_EmitsRowPerBeadSessionPair(t *testing.T) {
	profile := &planner.SessionProfile{
		Concepts:  map[planner.ConceptTag]float64{"auth": 1.0},
		LastBeads: []core.WorkItemID{"gm-prev"},
	}
	in := PlannerInput{
		Beads: []PlannerInputBead{
			{
				ID:           "gm-1",
				Concepts:     []planner.ConceptTag{"auth"},
				Repositories: []string{"gemba"},
				Branch:       "main",
			},
		},
		SessionContexts: []planner.OperationalContext{{
			Session:   &core.Session{ID: "sess-warm"},
			Workspace: &core.Workspace{Repository: "gemba", Branch: "main"},
			Profile:   profile,
			Health:    &planner.SessionHealth{ContextPressure: 0.1},
		}},
	}
	body := runCmdGetOutput(t, newAffinityCmd(), in)
	if !strings.Contains(body, "gm-1") {
		t.Errorf("expected gm-1 row: %s", body)
	}
	if !strings.Contains(body, "sess-warm") {
		t.Errorf("expected sess-warm in row: %s", body)
	}
}

func TestAffinityCmd_JSONEnvelopeStable(t *testing.T) {
	in := PlannerInput{
		Beads: []PlannerInputBead{{ID: "gm-1", Concepts: []planner.ConceptTag{"auth"}}},
		SessionContexts: []planner.OperationalContext{{
			Session: &core.Session{ID: "sess-1"},
		}},
	}
	body := runCmdGetOutputWithFlags(t, newAffinityCmd(), in, []string{"--json"})
	var env PlannerJSONOut
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	if env.Affinity == nil || len(env.Affinity.Rows) != 1 {
		t.Fatalf("expected 1 row, got %+v", env.Affinity)
	}
	if env.Affinity.Rows[0].SessionID != "sess-1" {
		t.Errorf("session_id = %q", env.Affinity.Rows[0].SessionID)
	}
}

func TestAffinityCmd_CustomWeightsParsed(t *testing.T) {
	in := PlannerInput{
		Beads: []PlannerInputBead{{ID: "gm-1"}},
		SessionContexts: []planner.OperationalContext{{
			Session: &core.Session{ID: "sess-1"},
			Health:  &planner.SessionHealth{ContextPressure: 0.1},
		}},
	}
	// Set headroom to 1.0 weight; everything else 0. Combined
	// should equal Headroom (which is 1.0 with low pressure).
	body := runCmdGetOutputWithFlags(t, newAffinityCmd(), in, []string{
		"--json",
		"--weights", `{"concept_overlap":0,"file_familiarity":0,"workspace_match":0,"recency":0,"headroom":1}`,
	})
	var env PlannerJSONOut
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	row := env.Affinity.Rows[0]
	if row.Scores.Combined < 0.99 {
		t.Errorf("custom weights should drive combined to ~1.0, got %v", row.Scores.Combined)
	}
}

// runCmdGetOutput executes the cobra command with `in` as stdin
// JSON and returns the captured stdout.
func runCmdGetOutput(t *testing.T, cmd *cobra.Command, in PlannerInput) string {
	t.Helper()
	return runCmdGetOutputWithFlags(t, cmd, in, nil)
}

// runCmdGetOutputWithFlags is the same with extra CLI flags.
func runCmdGetOutputWithFlags(t *testing.T, cmd *cobra.Command, in PlannerInput, args []string) string {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	cmd.SetIn(bytes.NewReader(body))
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	if args != nil {
		cmd.SetArgs(args)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, stdout.String())
	}
	return stdout.String()
}
