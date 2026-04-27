package bd_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/bd"
	"github.com/MikeBengtson/gemba/internal/core"
)

// TestDoDExtractFromDescription locks the read-side DoD parse: a bead
// whose description contains a <!--gemba:dod--> block surfaces with
// the criteria + notes on WorkItem.DoD and a description that has the
// block stripped (so the SPA renders the human prose, not the raw
// pass-through markup).
func TestDoDExtractFromDescription(t *testing.T) {
	desc := "Pre-amble.\n\n" +
		"<!--gemba:dod-->\n" +
		"acceptance:\n" +
		"- first criterion\n" +
		"- second criterion\n" +
		"notes:\n" +
		"free-form notes\n" +
		"on multiple lines\n" +
		"<!--/gemba:dod-->\n\n" +
		"Trailing prose."
	fake := newFakeBd(t)
	fake.beads["gm-dod-r"] = &bd.Bead{
		ID:          "gm-dod-r",
		Title:       "dod read",
		Description: desc,
		Status:      "open",
		IssueType:   "task",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-dod-r"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.DoD == nil {
		t.Fatal("DoD is nil; expected populated DefinitionOfDone")
	}
	if got, want := len(wi.DoD.AcceptanceCriteria), 2; got != want {
		t.Fatalf("AcceptanceCriteria len=%d want %d (got %v)",
			got, want, wi.DoD.AcceptanceCriteria)
	}
	if wi.DoD.AcceptanceCriteria[0] != "first criterion" {
		t.Errorf("AC[0]=%q want %q", wi.DoD.AcceptanceCriteria[0], "first criterion")
	}
	if wi.DoD.AcceptanceCriteria[1] != "second criterion" {
		t.Errorf("AC[1]=%q want %q", wi.DoD.AcceptanceCriteria[1], "second criterion")
	}
	wantNotes := "free-form notes\non multiple lines"
	if wi.DoD.Notes != wantNotes {
		t.Errorf("Notes=%q want %q", wi.DoD.Notes, wantNotes)
	}
	// Description must have the block stripped — the SPA renders this
	// directly and we don't want it to display the raw delimiters.
	if strings.Contains(wi.Description, "gemba:dod") {
		t.Errorf("Description still contains DoD markup: %q", wi.Description)
	}
	if !strings.Contains(wi.Description, "Pre-amble.") {
		t.Errorf("Description lost the pre-amble: %q", wi.Description)
	}
	if !strings.Contains(wi.Description, "Trailing prose.") {
		t.Errorf("Description lost the trailing prose: %q", wi.Description)
	}
}

// TestDoDNoBlockYieldsNilDoD: a bead whose description has no DoD
// markup must surface DoD=nil and Description unchanged. We don't want
// every bead to mint an empty DefinitionOfDone the SPA then renders.
func TestDoDNoBlockYieldsNilDoD(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-no-dod"] = &bd.Bead{
		ID:          "gm-no-dod",
		Title:       "no dod",
		Description: "Just a regular description.\n\nNo DoD markers here.",
		Status:      "open",
		IssueType:   "task",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-no-dod"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.DoD != nil {
		t.Errorf("DoD=%+v; expected nil", wi.DoD)
	}
	if wi.Description != "Just a regular description.\n\nNo DoD markers here." {
		t.Errorf("Description mutated unexpectedly: %q", wi.Description)
	}
}

// TestDoDMalformedBlockSurvivesIntact: an open delimiter without a
// close delimiter is treated as "no block" so the operator can fix the
// description by hand. The DoD must be nil and the description must
// retain the partial markup verbatim — we won't half-strip on bad
// input.
func TestDoDMalformedBlockSurvivesIntact(t *testing.T) {
	desc := "Pre.\n<!--gemba:dod-->\nacceptance:\n- foo\nno close delim here"
	fake := newFakeBd(t)
	fake.beads["gm-bad-dod"] = &bd.Bead{
		ID:          "gm-bad-dod",
		Title:       "malformed",
		Description: desc,
		Status:      "open",
		IssueType:   "task",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-bad-dod"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.DoD != nil {
		t.Errorf("DoD=%+v; expected nil for malformed block", wi.DoD)
	}
	if wi.Description != desc {
		t.Errorf("Description mutated; got %q want %q", wi.Description, desc)
	}
}

// TestDoDCreateRoundTrip exercises the write path: a CreateWorkItem
// with a populated DoD must serialize the block into bd's description,
// and the materialized record (via the same toWorkItem read path) must
// surface the DoD back unchanged. This is the heart of the gm-e6.4
// DoD: "Round-trip preserves both acceptance_criteria and notes."
func TestDoDCreateRoundTrip(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	in := core.WorkItem{
		Title:       "round-trip me",
		Kind:        "task",
		Description: "Some prose.",
		DoD: &core.DefinitionOfDone{
			AcceptanceCriteria: []string{
				"all tests green",
				"docs updated",
			},
			Notes: "informational only — never blocks state transitions",
		},
	}
	out, err := impl.CreateWorkItem(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if out.DoD == nil {
		t.Fatal("DoD is nil on Create round-trip")
	}
	if got := out.DoD.AcceptanceCriteria; len(got) != 2 ||
		got[0] != "all tests green" || got[1] != "docs updated" {
		t.Errorf("AcceptanceCriteria=%v want [all tests green, docs updated]", got)
	}
	if out.DoD.Notes != "informational only — never blocks state transitions" {
		t.Errorf("Notes=%q", out.DoD.Notes)
	}
	// Description survives the round-trip verbatim (the DoD was
	// embedded into bd's stored description and stripped back out on
	// read; the surrounding prose should be exactly what the caller
	// passed in).
	if !strings.Contains(out.Description, "Some prose.") {
		t.Errorf("Description lost the user prose: %q", out.Description)
	}
	if strings.Contains(out.Description, "gemba:dod") {
		t.Errorf("Description leaks DoD markup: %q", out.Description)
	}

	// Sanity check: the underlying Bead in the fake's store DOES carry
	// the embedded block (proves the write path actually serialized the
	// DoD into the bd description, not just stashed it on the WorkItem).
	stored, ok := fake.beads[string(nativeIDFromWorkItem(out.ID))]
	if !ok {
		t.Fatalf("stored bead not found for %q", out.ID)
	}
	if !strings.Contains(stored.Description, "<!--gemba:dod-->") {
		t.Errorf("stored bd description missing DoD open delim: %q", stored.Description)
	}
	if !strings.Contains(stored.Description, "<!--/gemba:dod-->") {
		t.Errorf("stored bd description missing DoD close delim: %q", stored.Description)
	}
	if !strings.Contains(stored.Description, "all tests green") {
		t.Errorf("stored bd description missing AC: %q", stored.Description)
	}
}

// TestDoDUpdateOnlyDoDPreservesDescription locks the case-2 patch
// path: when WorkItemPatch.Description is nil and only DoD is set, the
// adaptor must re-read the existing description, swap the DoD block,
// and push back — leaving the surrounding prose intact. Beads users
// editing the description outside Gemba must not lose their DoD, and
// vice versa.
func TestDoDUpdateOnlyDoDPreservesDescription(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-update"] = &bd.Bead{
		ID:          "gm-update",
		Title:       "patch dod only",
		Description: "Operator wrote this prose by hand in `bd`.",
		Status:      "open",
		IssueType:   "task",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	id := core.WorkItemID("gemba/gemba/gm-update")
	dod := &core.DefinitionOfDone{
		AcceptanceCriteria: []string{"green tests"},
		Notes:              "shipped",
	}
	out, err := impl.UpdateWorkItem(context.Background(), id, core.WorkItemPatch{
		DoD: dod,
	})
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	if out.DoD == nil {
		t.Fatal("DoD is nil after update")
	}
	if len(out.DoD.AcceptanceCriteria) != 1 || out.DoD.AcceptanceCriteria[0] != "green tests" {
		t.Errorf("AC=%v want [green tests]", out.DoD.AcceptanceCriteria)
	}
	if out.DoD.Notes != "shipped" {
		t.Errorf("Notes=%q want shipped", out.DoD.Notes)
	}
	// Operator's prose survives — that's the inverse half of "users
	// editing outside Gemba do not lose their DoD".
	if !strings.Contains(out.Description, "Operator wrote this prose by hand") {
		t.Errorf("operator prose lost: %q", out.Description)
	}

	// The stored bd row must now carry the embedded block adjacent to
	// the prose, in that order (prose first, block second), so a `bd
	// show` outside Gemba renders cleanly.
	stored := fake.beads["gm-update"]
	if !strings.Contains(stored.Description, "Operator wrote this prose by hand") {
		t.Errorf("stored desc lost prose: %q", stored.Description)
	}
	if !strings.Contains(stored.Description, "<!--gemba:dod-->") {
		t.Errorf("stored desc missing DoD block: %q", stored.Description)
	}
}

// TestDoDUpdateClearsBlockWhenDoDIsEmpty: passing a DoD with no
// criteria and no notes should remove any existing block. The path is
// "set DoD to nil-effective" because core.WorkItemPatch can't
// distinguish nil from empty-pointer cleanly otherwise.
func TestDoDUpdateClearsBlockWhenDoDIsEmpty(t *testing.T) {
	fake := newFakeBd(t)
	// Seed with an existing DoD block.
	fake.beads["gm-clear"] = &bd.Bead{
		ID:    "gm-clear",
		Title: "clear it",
		Description: "Prose.\n\n<!--gemba:dod-->\nacceptance:\n- foo\n" +
			"<!--/gemba:dod-->",
		Status:    "open",
		IssueType: "task",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	id := core.WorkItemID("gemba/gemba/gm-clear")
	// Empty DoD → embedDoD strips the block.
	out, err := impl.UpdateWorkItem(context.Background(), id, core.WorkItemPatch{
		DoD: &core.DefinitionOfDone{},
	})
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	if out.DoD != nil {
		t.Errorf("DoD=%+v; expected nil after clearing", out.DoD)
	}
	stored := fake.beads["gm-clear"]
	if strings.Contains(stored.Description, "gemba:dod") {
		t.Errorf("stored desc still has DoD markup: %q", stored.Description)
	}
	if !strings.Contains(stored.Description, "Prose.") {
		t.Errorf("stored desc lost prose: %q", stored.Description)
	}
}

// TestDoDIdempotentRepeatedRoundTrips: extract → embed → extract must
// reach a fixed point. A second update with the same DoD on a bead
// already carrying that DoD must not mutate the stored description in
// any meaningful way (no block duplication, no progressive whitespace
// drift).
func TestDoDIdempotentRepeatedRoundTrips(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	wi, err := impl.CreateWorkItem(context.Background(), core.WorkItem{
		Title:       "idempotency",
		Kind:        "task",
		Description: "Body.",
		DoD: &core.DefinitionOfDone{
			AcceptanceCriteria: []string{"a", "b"},
			Notes:              "stable notes",
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	first := fake.beads[string(nativeIDFromWorkItem(wi.ID))].Description

	// Patch with the same DoD; expect identical stored description.
	_, err = impl.UpdateWorkItem(context.Background(), wi.ID, core.WorkItemPatch{
		DoD: wi.DoD,
	})
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	second := fake.beads[string(nativeIDFromWorkItem(wi.ID))].Description
	if first != second {
		t.Errorf("description drifted across idempotent updates:\nfirst:  %q\nsecond: %q", first, second)
	}
	// And exactly one DoD block (not two appended back-to-back).
	if n := strings.Count(second, "<!--gemba:dod-->"); n != 1 {
		t.Errorf("expected exactly 1 DoD open delim, got %d in %q", n, second)
	}
}

// nativeIDFromWorkItem strips the "gemba/gemba/" prefix the adaptor
// prepends so tests can index the fake's beads map by the bare bd id.
// Mirrors the adaptor's internal nativeID helper.
func nativeIDFromWorkItem(id core.WorkItemID) core.WorkItemID {
	s := string(id)
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return core.WorkItemID(s[i+1:])
	}
	return id
}
