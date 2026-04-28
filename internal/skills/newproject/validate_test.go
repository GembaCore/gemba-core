package newproject

import (
	"strings"
	"testing"
)

// TestValidateState_HappyPath: a complete plan with every field
// populated passes validation -- this is the schema-completeness
// fixture.
func TestValidateState_HappyPath(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	if err := ValidateState(&s, true); err != nil {
		t.Fatalf("expected ok, got: %v", err)
	}
}

// TestValidateState_EmptyMilestonesAfterFirstTurn surfaces the
// "model lost the plan" regression at the right field path so
// the host can hand it to the model on retry.
func TestValidateState_EmptyMilestonesAfterFirstTurn(t *testing.T) {
	t.Parallel()

	s := EmptyState()
	s.Turn = 2
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on empty Milestones after turn 1")
	}
	if !strings.Contains(err.Error(), "Milestones") {
		t.Fatalf("error should mention Milestones, got: %v", err)
	}
}

// TestValidateState_EmptyMilestonesAllowedOnTurnZero: the model
// can ask a clarifying question on turn 0 without proposing a
// plan.
func TestValidateState_EmptyMilestonesAllowedOnTurnZero(t *testing.T) {
	t.Parallel()

	s := EmptyState()
	s.Turn = 1 // value the model emits; requireNonEmpty=false.
	if err := ValidateState(&s, false); err != nil {
		t.Fatalf("turn-0 empty plan should pass with requireNonEmpty=false, got: %v", err)
	}
}

// TestValidateState_EmptyEpicsRejected enforces "every milestone
// rolls up at least one epic". This is the architectural
// invariant the design doc pins.
func TestValidateState_EmptyEpicsRejected(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics = []DraftEpic{}
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on epic-less milestone")
	}
	if !strings.Contains(err.Error(), "Epics") {
		t.Fatalf("error should mention Epics, got: %v", err)
	}
}

// TestValidateState_EmptyBeadsRejected enforces "every epic rolls
// up at least one bead".
func TestValidateState_EmptyBeadsRejected(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads = []DraftBead{}
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on bead-less epic")
	}
	if !strings.Contains(err.Error(), "Beads") {
		t.Fatalf("error should mention Beads, got: %v", err)
	}
}

// TestValidateState_EmptyBeadTitleRejected: title is the one
// truly required free-text field; an empty bead title is the
// most-common skill regression.
func TestValidateState_EmptyBeadTitleRejected(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads[0].Title = "  "
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on empty title")
	}
	if !strings.Contains(err.Error(), "Title") {
		t.Fatalf("error should mention Title, got: %v", err)
	}
}

// TestValidateState_RejectsMilestoneTypeLabel: ratification adds
// type:milestone; the skill MUST NOT.
func TestValidateState_RejectsMilestoneTypeLabel(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Labels = append(s.Milestones[0].Labels, "type:milestone")
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on type:milestone emission")
	}
	if !strings.Contains(err.Error(), "type:milestone") {
		t.Fatalf("error should mention type:milestone, got: %v", err)
	}
}

// TestValidateState_RejectsBadBeadType: only the closed enum is
// allowed. A model that emits "story" gets caught here.
func TestValidateState_RejectsBadBeadType(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads[0].Type = "story"
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on unknown bead type")
	}
	if !strings.Contains(err.Error(), "Type") {
		t.Fatalf("error should mention Type, got: %v", err)
	}
}

// TestValidateState_DanglingDepRefRejected catches the most
// common ratify-time crash: a bead pointing at a deleted sibling.
func TestValidateState_DanglingDepRefRejected(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads[1].DependsOnRefs = []string{"milestone:0/epic:0/bead:99"}
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on dangling ref")
	}
	if !strings.Contains(err.Error(), "dangling") {
		t.Fatalf("error should describe the dangling ref, got: %v", err)
	}
}

// TestValidateState_SelfRefRejected: a bead can't depend on
// itself.
func TestValidateState_SelfRefRejected(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads[0].DependsOnRefs = []string{"milestone:0/epic:0/bead:0"}
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on self-ref")
	}
	if !strings.Contains(err.Error(), "self-reference") {
		t.Fatalf("error should describe self-reference, got: %v", err)
	}
}

// TestValidateState_BadRefShape catches refs that don't match
// the milestone/epic/bead grammar at all.
func TestValidateState_BadRefShape(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads[1].DependsOnRefs = []string{"bd-123"}
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on bd-... shaped ref")
	}
	if !strings.Contains(err.Error(), "milestone:") {
		t.Fatalf("error should describe expected shape, got: %v", err)
	}
}

// TestValidateState_BadChangeRefKind catches a model that emits
// a kind outside the closed enum.
func TestValidateState_BadChangeRefKind(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.LastChange.Kind = "fixed"
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on bad change kind")
	}
	if !strings.Contains(err.Error(), "kind") {
		t.Fatalf("error should describe the kind field, got: %v", err)
	}
}

// TestValidateState_BadChangeRefPath: path must match the
// milestone[/epic[/bead]] grammar when non-empty.
func TestValidateState_BadChangeRefPath(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.LastChange.Path = "epic:0"
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on bad change path")
	}
}

// TestValidateState_NilSliceRejected: every slice field MUST be
// the empty slice, not nil. JSON round-tripping otherwise drops
// the key, which breaks the SPA's "no missing keys" assumption.
func TestValidateState_NilSliceRejected(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Epics[0].Beads[0].DependsOnRefs = nil
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on nil DependsOnRefs")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("error should describe the nil-vs-empty contract, got: %v", err)
	}
}

// TestValidateState_PriorityOutOfRange: 0..4 inclusive; anything
// else is rejected.
func TestValidateState_PriorityOutOfRange(t *testing.T) {
	t.Parallel()

	for _, p := range []int{-1, 5, 99} {
		s := fullPlan()
		s.Milestones[0].Priority = p
		err := ValidateState(&s, true)
		if err == nil {
			t.Fatalf("priority %d: expected error", p)
		}
	}
}

// TestValidateState_NegativeEstimate: 0 is unestimated;
// negatives are rejected.
func TestValidateState_NegativeEstimate(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	s.Milestones[0].Estimate = -10
	err := ValidateState(&s, true)
	if err == nil {
		t.Fatal("expected error on negative estimate")
	}
}

// TestSweepDanglingRefs verifies the in-place ref sweep removes
// the right entries and leaves the surviving graph intact.
func TestSweepDanglingRefs(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	// Plant three problems on B2: a self-ref, a dangling ref,
	// and a valid ref. Only the valid one should survive.
	s.Milestones[0].Epics[0].Beads[1].DependsOnRefs = []string{
		"milestone:0/epic:0/bead:1", // self
		"milestone:0/epic:0/bead:0", // valid
		"milestone:9/epic:9/bead:9", // dangling
	}

	removed := SweepDanglingRefs(&s)
	if removed != 2 {
		t.Fatalf("removed: got %d, want 2", removed)
	}
	got := s.Milestones[0].Epics[0].Beads[1].DependsOnRefs
	if len(got) != 1 || got[0] != "milestone:0/epic:0/bead:0" {
		t.Fatalf("surviving refs: got %v, want [milestone:0/epic:0/bead:0]", got)
	}

	// Re-sweep is a no-op.
	if again := SweepDanglingRefs(&s); again != 0 {
		t.Fatalf("re-sweep: got %d, want 0", again)
	}
}

// TestEmptyState_NoMissingKeys is the schema-completeness
// guard: every slice / struct field is non-nil so JSON
// round-tripping preserves the "no missing keys" contract.
func TestEmptyState_NoMissingKeys(t *testing.T) {
	t.Parallel()

	s := EmptyState()
	if s.TechStack == nil {
		t.Fatal("TechStack must be []")
	}
	if s.Milestones == nil {
		t.Fatal("Milestones must be []")
	}
	if err := ValidateState(&s, false); err != nil {
		t.Fatalf("EmptyState should pass validation with requireNonEmpty=false: %v", err)
	}
}

// TestParseRef covers the helper used by SweepDanglingRefs and
// ValidateState's ref-walk.
func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref        string
		mi, ei, bi int
	}{
		{"milestone:0/epic:1/bead:2", 0, 1, 2},
		{"milestone:10/epic:0/bead:0", 10, 0, 0},
		{"milestone:0/epic:0", 0, 0, -1},
	}
	for _, tc := range tests {
		mi, ei, bi := parseRef(tc.ref)
		if mi != tc.mi || ei != tc.ei || bi != tc.bi {
			t.Fatalf("parseRef(%q): got (%d,%d,%d), want (%d,%d,%d)", tc.ref, mi, ei, bi, tc.mi, tc.ei, tc.bi)
		}
	}
}

// TestValidationErrorString confirms the user-facing string is
// shaped the way the dispatcher expects ("newproject: <field>:
// <message>").
func TestValidationErrorString(t *testing.T) {
	t.Parallel()

	got := (&ValidationError{Field: "x", Message: "y"}).Error()
	want := "newproject: x: y"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = (&ValidationError{Message: "structural failure"}).Error()
	want = "newproject: structural failure"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
