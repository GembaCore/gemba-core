package epic_order

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

// fakeWP captures UpdateWorkItem calls and returns a canned
// response. Implements just the WorkPlane methods the applier
// touches; everything else panics so a regression that calls into
// other methods surfaces immediately.
type fakeWP struct {
	core.WorkPlane // embed for the methods we don't implement
	calls          []fakeUpdateCall
	updateResp     core.WorkItem
	updateErr      error
}

type fakeUpdateCall struct {
	id    core.WorkItemID
	patch core.WorkItemPatch
}

func (f *fakeWP) UpdateWorkItem(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
	f.calls = append(f.calls, fakeUpdateCall{id, patch})
	if f.updateErr != nil {
		return core.WorkItem{}, f.updateErr
	}
	return f.updateResp, nil
}

func recommendation(epicID, path string, body json.RawMessage) *RecommendationLine {
	return &RecommendationLine{
		Type:       LineRecommendation,
		Rank:       1,
		EpicID:     core.WorkItemID(epicID),
		Confidence: 0.9,
		Rationale:  "test",
		SuggestedAction: &SuggestedAction{
			Verb: "PATCH",
			Path: path,
			Body: body,
		},
	}
}

func TestApplier_NewApplierNilWorkPlaneReturnsNil(t *testing.T) {
	if NewApplier(nil) != nil {
		t.Error("NewApplier(nil) must return nil so the dispatcher's record-only fallback fires")
	}
}

func TestApplier_HappyPathIssuesPatch(t *testing.T) {
	wp := &fakeWP{updateResp: core.WorkItem{ID: "gm-1", Title: "first"}}
	a := NewApplier(wp)
	rec := recommendation("gm-1", "/api/work-items/gm-1", json.RawMessage(`{"priority":1}`))
	res, err := a.Apply(context.Background(), rec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(wp.calls) != 1 {
		t.Fatalf("UpdateWorkItem calls = %d, want 1", len(wp.calls))
	}
	got := wp.calls[0]
	if got.id != "gm-1" {
		t.Errorf("id = %q, want gm-1", got.id)
	}
	if got.patch.Priority == nil || *got.patch.Priority != 1 {
		t.Errorf("patch.Priority = %v, want 1", got.patch.Priority)
	}
	if !strings.Contains(res.Detail, "gm-1") {
		t.Errorf("Detail does not mention id: %q", res.Detail)
	}
	if len(res.Body) == 0 {
		t.Error("Body empty; want marshaled WorkItem")
	}
}

func TestApplier_AcceptsBothCanonicalPaths(t *testing.T) {
	for _, path := range []string{"/api/work-items/gm-1", "/api/workitems/gm-1"} {
		t.Run(path, func(t *testing.T) {
			wp := &fakeWP{updateResp: core.WorkItem{ID: "gm-1"}}
			a := NewApplier(wp)
			if _, err := a.Apply(context.Background(), recommendation("gm-1", path, nil)); err != nil {
				t.Errorf("path %s rejected: %v", path, err)
			}
		})
	}
}

func TestApplier_RejectsNonRecommendationLine(t *testing.T) {
	a := NewApplier(&fakeWP{})
	cases := []any{
		&StrategyLine{Type: LineStrategy},
		&WarningLine{Type: LineWarning},
		&SummaryLine{Type: LineSummary},
		nil,
		"not a struct",
	}
	for _, line := range cases {
		_, err := a.Apply(context.Background(), line)
		if err == nil {
			t.Errorf("Apply(%T) accepted; want error", line)
		}
	}
}

func TestApplier_RejectsRecommendationWithoutSuggestedAction(t *testing.T) {
	a := NewApplier(&fakeWP{})
	rec := &RecommendationLine{Type: LineRecommendation, EpicID: "gm-1", Rationale: "x", Confidence: 0.9}
	_, err := a.Apply(context.Background(), rec)
	if err == nil || !strings.Contains(err.Error(), "no suggested_action") {
		t.Errorf("err = %v; want 'no suggested_action'", err)
	}
}

func TestApplier_RejectsNonPATCHVerb(t *testing.T) {
	a := NewApplier(&fakeWP{})
	for _, verb := range []string{"POST", "DELETE", "GET", "PUT", ""} {
		rec := recommendation("gm-1", "/api/work-items/gm-1", nil)
		rec.SuggestedAction.Verb = verb
		_, err := a.Apply(context.Background(), rec)
		if err == nil || !strings.Contains(err.Error(), "verb") {
			t.Errorf("verb %q: err = %v; want verb-rejection", verb, err)
		}
	}
}

func TestApplier_RejectsPathIDMismatch(t *testing.T) {
	a := NewApplier(&fakeWP{})
	rec := recommendation("gm-1", "/api/work-items/gm-99", nil)
	_, err := a.Apply(context.Background(), rec)
	if err == nil || !strings.Contains(err.Error(), "path id") {
		t.Errorf("err = %v; want path-id mismatch", err)
	}
}

func TestApplier_RejectsBadPath(t *testing.T) {
	a := NewApplier(&fakeWP{})
	for _, path := range []string{"", "https://evil.example.com/x", "/api/sessions/gm-1", "gm-1"} {
		rec := recommendation("gm-1", path, nil)
		_, err := a.Apply(context.Background(), rec)
		if err == nil {
			t.Errorf("path %q accepted; want rejection", path)
		}
	}
}

func TestApplier_RejectsMalformedBody(t *testing.T) {
	a := NewApplier(&fakeWP{})
	rec := recommendation("gm-1", "/api/work-items/gm-1", json.RawMessage(`not json`))
	_, err := a.Apply(context.Background(), rec)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v; want body-decode error", err)
	}
}

func TestApplier_PropagatesWorkPlaneError(t *testing.T) {
	want := errors.New("status transition rejected")
	wp := &fakeWP{updateErr: want}
	a := NewApplier(wp)
	rec := recommendation("gm-1", "/api/work-items/gm-1", json.RawMessage(`{"priority":1}`))
	_, err := a.Apply(context.Background(), rec)
	if err == nil || !errors.Is(err, want) {
		t.Errorf("err = %v; want wrapping %v", err, want)
	}
}
