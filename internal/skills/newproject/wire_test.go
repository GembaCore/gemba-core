package newproject

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWireShape_PascalCaseKeys is the lockstep guard between the
// Go and TS sides: web/src/api/newproject.ts declares the field
// names as PascalCase (Title, Milestones, ...). If a future
// refactor flips a struct tag to lowerCamel, the SPA's typed
// stubs stop matching the wire and the e2e suite breaks at the
// JSON boundary.
//
// We assert presence of the known PascalCase keys for every
// schema type. Failures here mean the Go side drifted.
func TestWireShape_PascalCaseKeys(t *testing.T) {
	t.Parallel()

	s := fullPlan()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	body := string(b)

	for _, key := range []string{
		`"ProjectName":`, `"Description":`, `"TechStack":`,
		`"Architecture":`, `"Milestones":`, `"DraftProjectMD":`,
		`"Turn":`, `"LastChange":`,
		// DraftMilestone
		`"Title":`, `"Acceptance":`, `"Labels":`, `"Priority":`,
		`"Estimate":`, `"Skills":`, `"DesignNotes":`, `"Notes":`,
		`"Epics":`,
		// DraftEpic + DraftBead
		`"Beads":`, `"Type":`, `"DependsOnRefs":`, `"BlocksRefs":`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("expected key %s in serialized state, got: %s", key, body)
		}
	}
	// ChangeRef is camelCase on the wire (path/kind/summary) --
	// matches the TS shape verbatim.
	for _, key := range []string{`"path":`, `"kind":`, `"summary":`} {
		if !strings.Contains(body, key) {
			t.Errorf("expected ChangeRef key %s, got: %s", key, body)
		}
	}
}

// TestWireShape_RoundTrip confirms a state survives JSON
// marshal+unmarshal with every field intact. The "no missing
// keys" contract relies on this -- a struct field with no JSON
// tag and a default value would silently get dropped.
func TestWireShape_RoundTrip(t *testing.T) {
	t.Parallel()

	want := fullPlan()
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got NewProjectState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ProjectName != want.ProjectName {
		t.Errorf("ProjectName: got %q, want %q", got.ProjectName, want.ProjectName)
	}
	if got.Turn != want.Turn {
		t.Errorf("Turn: got %d, want %d", got.Turn, want.Turn)
	}
	if len(got.Milestones) != len(want.Milestones) {
		t.Fatalf("Milestones len: got %d, want %d", len(got.Milestones), len(want.Milestones))
	}
	gb := got.Milestones[0].Epics[0].Beads[1]
	wb := want.Milestones[0].Epics[0].Beads[1]
	if gb.Title != wb.Title {
		t.Errorf("Bead title: got %q, want %q", gb.Title, wb.Title)
	}
	if len(gb.DependsOnRefs) != len(wb.DependsOnRefs) {
		t.Errorf("DependsOnRefs: got %v, want %v", gb.DependsOnRefs, wb.DependsOnRefs)
	}
	if got.LastChange.Path != want.LastChange.Path {
		t.Errorf("LastChange.Path: got %q, want %q", got.LastChange.Path, want.LastChange.Path)
	}
}

// TestWireShape_EmptyStateSerialization confirms EmptyState
// round-trips without losing the empty-array fields. A naive
// `nil` slice would marshal as `null` -- which matches Go but
// breaks TS's `string[]` type.
func TestWireShape_EmptyStateSerialization(t *testing.T) {
	t.Parallel()

	s := EmptyState()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(b)

	for _, want := range []string{
		`"TechStack":[]`,
		`"Milestones":[]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("EmptyState should serialize empty arrays, missing %s in: %s", want, body)
		}
	}
	// And nil/null must NOT appear -- that's the regression
	// guard. We allow `null` only inside ChangeRef (it doesn't
	// have any nullable fields, so this is purely defensive).
	if strings.Contains(body, "null") {
		t.Errorf("EmptyState contains null where empty array/string was expected: %s", body)
	}
}
