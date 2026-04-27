// gm-vch2: RefineryRejectionLister tests — assert empty source
// contributes zero, populated source projects to EscalationRequest
// with Urgency=blocking, and workspace flows through.

package sources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

type fakeRefinerySource struct {
	got string
	rs  []RefineryRejection
	err error
}

func (f *fakeRefinerySource) ListOpenRefineryRejections(_ context.Context, ws string) ([]RefineryRejection, error) {
	f.got = ws
	return f.rs, f.err
}

func TestRefineryRejectionLister_EmptyContributesZero(t *testing.T) {
	l := NewRefineryRejectionLister(EmptyRefinerySource{})
	got, err := l.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d; want 0", len(got))
	}
}

func TestRefineryRejectionLister_ProjectsRejection(t *testing.T) {
	src := &fakeRefinerySource{rs: []RefineryRejection{
		{
			ID:         "pr-42",
			Title:      "PR #42 rejected",
			Prompt:     "tests failing on main; refinery cannot merge",
			WorkItemID: core.WorkItemID("gm-9"),
			AgentID:    core.AgentID("polecat-y"),
			CreatedAt:  time.Unix(1700000000, 0),
		},
	}}
	l := NewRefineryRejectionLister(src)
	got, err := l.ListOpenEscalations(context.Background(), "ws-beta")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if src.got != "ws-beta" {
		t.Errorf("workspace not propagated; got %q", src.got)
	}
	if len(got) != 1 {
		t.Fatalf("got %d; want 1", len(got))
	}
	r := got[0]
	if r.Source != core.EscalationRefineryRejection {
		t.Errorf("Source = %q; want %q", r.Source, core.EscalationRefineryRejection)
	}
	if r.Urgency != core.UrgencyBlocking {
		t.Errorf("Urgency = %q; want blocking (rejections halt the bead)", r.Urgency)
	}
	if r.ID != "refinery-pr-42" {
		t.Errorf("ID = %q; want refinery-pr-42", r.ID)
	}
	if r.WorkItemID != "gm-9" {
		t.Errorf("WorkItemID dropped: %q", r.WorkItemID)
	}
}

func TestRefineryRejectionLister_PropagatesError(t *testing.T) {
	wantErr := errors.New("refinery down")
	l := NewRefineryRejectionLister(&fakeRefinerySource{err: wantErr})
	_, err := l.ListOpenEscalations(context.Background(), "")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v; want %v", err, wantErr)
	}
}

func TestRefineryRejectionLister_NilSafe(t *testing.T) {
	if got := NewRefineryRejectionLister(nil); got != nil {
		t.Errorf("NewRefineryRejectionLister(nil) = %v; want nil", got)
	}
	var l *RefineryRejectionLister
	if _, err := l.ListOpenEscalations(context.Background(), ""); err != nil {
		t.Errorf("nil-receiver err: %v", err)
	}
}
