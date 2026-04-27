// gm-vch2: WitnessLister tests — assert empty source contributes
// zero, populated source projects to EscalationRequest correctly,
// workspace flows through, and id is stable.

package sources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

type fakeWitnessSource struct {
	got      string
	findings []WitnessFinding
	err      error
}

func (f *fakeWitnessSource) ListOpenWitnessFindings(_ context.Context, ws string) ([]WitnessFinding, error) {
	f.got = ws
	return f.findings, f.err
}

func TestWitnessLister_EmptySourceContributesZero(t *testing.T) {
	l := NewWitnessLister(EmptyWitnessSource{})
	got, err := l.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d items; want 0", len(got))
	}
}

func TestWitnessLister_ProjectsFinding(t *testing.T) {
	src := &fakeWitnessSource{findings: []WitnessFinding{
		{
			ID:         "wf-1",
			Title:      "Suspicious tool use",
			Prompt:     "agent ran rm -rf without permission scope",
			WorkItemID: core.WorkItemID("gm-77"),
			AgentID:    core.AgentID("polecat-x"),
			Urgency:    core.UrgencyBlocking,
			CreatedAt:  time.Unix(1700000000, 0),
		},
	}}
	l := NewWitnessLister(src)
	got, err := l.ListOpenEscalations(context.Background(), "ws-alpha")
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	if src.got != "ws-alpha" {
		t.Errorf("workspace not propagated; got %q want ws-alpha", src.got)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items; want 1", len(got))
	}
	r := got[0]
	if r.Source != core.EscalationWitnessFinding {
		t.Errorf("Source = %q; want %q", r.Source, core.EscalationWitnessFinding)
	}
	if r.ID != "witness-wf-1" {
		t.Errorf("ID = %q; want witness-wf-1", r.ID)
	}
	if r.Urgency != core.UrgencyBlocking {
		t.Errorf("Urgency = %q; want blocking", r.Urgency)
	}
	if r.WorkItemID != "gm-77" || r.AgentID != "polecat-x" {
		t.Errorf("scope hooks dropped: wid=%q agent=%q", r.WorkItemID, r.AgentID)
	}
	if r.Title != "Suspicious tool use" {
		t.Errorf("Title not propagated: %q", r.Title)
	}
}

func TestWitnessLister_DefaultsTitleAndUrgency(t *testing.T) {
	src := &fakeWitnessSource{findings: []WitnessFinding{
		{ID: "wf-2", Prompt: "raw"},
	}}
	l := NewWitnessLister(src)
	got, _ := l.ListOpenEscalations(context.Background(), "")
	if got[0].Title != "Witness finding" {
		t.Errorf("Title = %q; want default", got[0].Title)
	}
	if got[0].Urgency != core.UrgencyAdvisory {
		t.Errorf("Urgency = %q; want default advisory", got[0].Urgency)
	}
}

func TestWitnessLister_FallbackIDStable(t *testing.T) {
	src := &fakeWitnessSource{findings: []WitnessFinding{
		{Title: "x", Prompt: "y"},
	}}
	l := NewWitnessLister(src)
	a, _ := l.ListOpenEscalations(context.Background(), "")
	b, _ := l.ListOpenEscalations(context.Background(), "")
	if a[0].ID != b[0].ID {
		t.Errorf("fallback id rolled across calls: %q vs %q", a[0].ID, b[0].ID)
	}
}

func TestWitnessLister_PropagatesError(t *testing.T) {
	wantErr := errors.New("witness offline")
	l := NewWitnessLister(&fakeWitnessSource{err: wantErr})
	_, err := l.ListOpenEscalations(context.Background(), "")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v; want %v", err, wantErr)
	}
}

func TestWitnessLister_NilSafe(t *testing.T) {
	if got := NewWitnessLister(nil); got != nil {
		t.Errorf("NewWitnessLister(nil) = %v; want nil", got)
	}
	var l *WitnessLister
	if _, err := l.ListOpenEscalations(context.Background(), ""); err != nil {
		t.Errorf("nil-receiver err: %v", err)
	}
}

func TestWitnessFindingSourceFunc(t *testing.T) {
	called := false
	src := WitnessFindingSourceFunc(func(_ context.Context, ws string) ([]WitnessFinding, error) {
		called = true
		if ws != "ws-x" {
			t.Errorf("ws = %q; want ws-x", ws)
		}
		return []WitnessFinding{{ID: "x"}}, nil
	})
	got, err := src.ListOpenWitnessFindings(context.Background(), "ws-x")
	if err != nil || !called || len(got) != 1 {
		t.Errorf("func adapter misbehaved: called=%v err=%v len=%d", called, err, len(got))
	}
}
