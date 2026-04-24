package events

import (
	"context"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestFromWorkPlaneEventCanonicalisesKinds(t *testing.T) {
	at := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want Kind
	}{
		{core.WorkItemEventCreated, WorkItemCreated},
		{core.WorkItemEventUpdated, WorkItemUpdated},
		{core.WorkItemEventClosed, WorkItemClosed},
		{core.WorkItemEventEvidenceAttached, WorkItemEvidence},
		{"future_kind_noone_knows_about", Kind("workplane.future_kind_noone_knows_about")},
	}
	for _, tc := range cases {
		got := FromWorkPlaneEvent("bd", core.WorkPlaneEvent{
			ID:         "e-1",
			Kind:       tc.in,
			At:         at,
			WorkItemID: "gemba/gemba/gm-foo",
			Payload:    map[string]any{"after": "x"},
		})
		if got.Kind != tc.want {
			t.Errorf("kind %q → %q, want %q", tc.in, got.Kind, tc.want)
		}
		if got.Source.Plane != PlaneWorkPlane || got.Source.AdaptorID != "bd" {
			t.Errorf("source lost: %+v", got.Source)
		}
		if got.WorkItemID != "gemba/gemba/gm-foo" {
			t.Errorf("work_item_id lost: %q", got.WorkItemID)
		}
		if got.At != at {
			t.Errorf("at lost: %v", got.At)
		}
	}
}

func TestHubAttachWorkPlaneStream(t *testing.T) {
	h := NewHub(Config{})
	defer h.Close()

	in := make(chan core.WorkPlaneEvent, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.AttachWorkPlaneStream(ctx, "bd", in)

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	out := h.Subscribe(subCtx, Filter{Kinds: []Kind{WorkItemUpdated, WorkItemClosed}})

	in <- core.WorkPlaneEvent{
		ID: "1", Kind: core.WorkItemEventUpdated, At: time.Now(),
		WorkItemID: "gm-foo",
	}
	in <- core.WorkPlaneEvent{
		ID: "2", Kind: core.WorkItemEventCreated, At: time.Now(),
		WorkItemID: "gm-bar",
	} // filtered out — kind workitem.created

	select {
	case got := <-out:
		if got.Kind != WorkItemUpdated || got.Source.AdaptorID != "bd" {
			t.Errorf("got %+v", got)
		}
		if got.WorkItemID != "gm-foo" {
			t.Errorf("scope: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for workitem.updated event")
	}

	// no more matching events should arrive
	select {
	case got, ok := <-out:
		if ok {
			t.Errorf("unexpected extra event: %+v", got)
		}
	case <-time.After(50 * time.Millisecond):
	}
}
