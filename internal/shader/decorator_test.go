package shader_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/shader"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// NopShader wraps as identity: every call routes to the inner adaptor
// unchanged. Tests would silently pass even without the wrap, so the
// real assertion is that the wrap doesn't *change* anything.
func TestWrap_NopShader_IdentityOnReads(t *testing.T) {
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	want := core.WorkItem{ID: "gm-foo", Kind: "task", Title: "hello",
		Status: "open", StateCategory: core.StateBacklog}
	wp.GetFn = func(_ context.Context, _ core.WorkItemID) (core.WorkItem, error) {
		return want, nil
	}
	wp.ListFn = func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
		return []core.WorkItem{want}, nil
	}

	wrapped := shader.Wrap(wp, core.NopShader{})

	got, err := wrapped.GetWorkItem(context.Background(), "gm-foo")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get drift: %+v", got)
	}

	listed, err := wrapped.ListWorkItems(context.Background(), core.WorkItemFilter{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(listed) != 1 || !reflect.DeepEqual(listed[0], want) {
		t.Errorf("List drift: %+v", listed)
	}
}

// nil shader → defaults to NopShader. Callers don't need a special
// case in serve.go.
func TestWrap_NilShader_BehavesAsNop(t *testing.T) {
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.GetFn = func(_ context.Context, _ core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{ID: "gm-x", Kind: "task", Title: "raw"}, nil
	}
	wrapped := shader.Wrap(wp, nil)
	got, err := wrapped.GetWorkItem(context.Background(), "gm-x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "raw" {
		t.Fatalf("nil shader must pass through; got %q", got.Title)
	}
}

// programmableShader exists so the decorator tests can pin
// encode-then-decode round-tripping without depending on a concrete
// shader implementation.
type programmableShader struct {
	encode func(core.WriteOp, core.WorkItem) (core.WorkItem, error)
	decode func(core.WorkItem) (core.WorkItem, error)
}

func (s programmableShader) EncodeForWrite(_ context.Context, op core.WriteOp, w core.WorkItem) (core.WorkItem, error) {
	if s.encode == nil {
		return w, nil
	}
	return s.encode(op, w)
}
func (s programmableShader) DecodeFromRead(_ context.Context, w core.WorkItem) (core.WorkItem, error) {
	if s.decode == nil {
		return w, nil
	}
	return s.decode(w)
}
func (s programmableShader) Describe() core.ShaderManifest {
	return core.ShaderManifest{Name: "programmable"}
}

// CreateWorkItem encodes the input before write, then decodes the
// adaptor's returned item before handing it back. Both transforms
// must fire — exactly once each.
func TestWrap_Create_EncodesThenDecodes(t *testing.T) {
	var encodedSeenByAdaptor string
	var decodeCallCount int
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.CreateFn = func(_ context.Context, w core.WorkItem) (core.WorkItem, error) {
		encodedSeenByAdaptor = w.Title
		w.ID = "gm-1"
		return w, nil
	}
	sh := programmableShader{
		encode: func(op core.WriteOp, w core.WorkItem) (core.WorkItem, error) {
			if op != core.WriteCreate {
				t.Errorf("encode op = %q, want create", op)
			}
			w.Title = "ENC:" + w.Title
			return w, nil
		},
		decode: func(w core.WorkItem) (core.WorkItem, error) {
			decodeCallCount++
			w.Title = "DEC:" + w.Title
			return w, nil
		},
	}
	wrapped := shader.Wrap(wp, sh)

	out, err := wrapped.CreateWorkItem(context.Background(),
		core.WorkItem{Kind: "task", Title: "hello"})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if encodedSeenByAdaptor != "ENC:hello" {
		t.Errorf("adaptor saw %q, want ENC:hello", encodedSeenByAdaptor)
	}
	if out.Title != "DEC:ENC:hello" {
		t.Errorf("returned title %q, want DEC:ENC:hello", out.Title)
	}
	if decodeCallCount != 1 {
		t.Errorf("decode called %d times, want 1", decodeCallCount)
	}
}

// UpdateWorkItem with a Title patch must:
//  1. Get the current item (so the encoder has Kind context).
//  2. Encode the synthesized {current.Kind, patch.Title}.
//  3. Send the encoded title down to the adaptor.
//  4. Decode the adaptor's result before returning.
func TestWrap_Update_FetchesCurrentForKindContext(t *testing.T) {
	var sentToAdaptor string
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.GetFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{ID: id, Kind: "bug", Title: "old"}, nil
	}
	wp.UpdateFn = func(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		if patch.Title != nil {
			sentToAdaptor = *patch.Title
		}
		return core.WorkItem{ID: id, Kind: "bug", Title: *patch.Title}, nil
	}
	sh := programmableShader{
		encode: func(_ core.WriteOp, w core.WorkItem) (core.WorkItem, error) {
			// Kind must reach the encoder via the synthesized Get.
			if w.Kind != "bug" {
				t.Errorf("encode missing kind context; got %+v", w)
			}
			w.Title = "[" + w.Kind + "] " + w.Title
			return w, nil
		},
		decode: func(w core.WorkItem) (core.WorkItem, error) {
			// Strip the prefix the encoder added.
			if len(w.Title) > len(w.Kind)+3 {
				w.Title = w.Title[len(w.Kind)+3:]
			}
			return w, nil
		},
	}
	wrapped := shader.Wrap(wp, sh)

	newTitle := "renamed"
	out, err := wrapped.UpdateWorkItem(context.Background(), "gm-1",
		core.WorkItemPatch{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	if sentToAdaptor != "[bug] renamed" {
		t.Errorf("adaptor saw %q, want [bug] renamed", sentToAdaptor)
	}
	if out.Title != "renamed" {
		t.Errorf("returned title %q, want renamed", out.Title)
	}
}

// notifyingFake adds the optional core.WorkItemNotifier capability on
// top of the FakeWorkPlane so we can exercise the conditional-forward
// path in shader.Wrap. Production analogue is bd.WorkPlane.
type notifyingFake struct {
	*testadaptors.FakeWorkPlane
	called int
	wi     core.WorkItem
	kind   string
	err    error
	gotID  core.WorkItemID
	gotSrc string
}

func (n *notifyingFake) NotifyExternal(_ context.Context, id core.WorkItemID, source string) (core.WorkItem, string, error) {
	n.called++
	n.gotID = id
	n.gotSrc = source
	return n.wi, n.kind, n.err
}

// gm-jqwf: Wrap MUST forward the optional WorkItemNotifier capability
// when the inner adaptor implements it. Before the fix the wrapper
// hid the capability and POST /api/workitems/notify always returned
// 409 in production despite bd.WorkPlane implementing it.
func TestWrap_ForwardsWorkItemNotifier(t *testing.T) {
	inner := &notifyingFake{
		FakeWorkPlane: testadaptors.NewFakeWorkPlane(core.TransportAPI),
		wi:            core.WorkItem{ID: "gm-foo", Status: "open", StateCategory: core.StateStarted},
		kind:          core.WorkItemEventUpdated,
	}
	wrapped := shader.Wrap(inner, core.NopShader{})

	notifier, ok := wrapped.(core.WorkItemNotifier)
	if !ok {
		t.Fatalf("wrapper does not satisfy core.WorkItemNotifier")
	}
	wi, kind, err := notifier.NotifyExternal(context.Background(), "gm-foo", "bd-git-hook")
	if err != nil {
		t.Fatalf("NotifyExternal: %v", err)
	}
	if kind != core.WorkItemEventUpdated {
		t.Errorf("kind: want %q got %q", core.WorkItemEventUpdated, kind)
	}
	if wi.ID != "gm-foo" {
		t.Errorf("wi.ID: got %q", wi.ID)
	}
	if inner.called != 1 || inner.gotID != "gm-foo" || inner.gotSrc != "bd-git-hook" {
		t.Errorf("inner not called as expected: called=%d id=%q src=%q",
			inner.called, inner.gotID, inner.gotSrc)
	}
}

// gm-jqwf: an adaptor that doesn't implement WorkItemNotifier must
// NOT be exposed as one through the wrapper — that's how the handler
// 409s opt-out backends like the dolt read-only adaptor.
func TestWrap_OmitsWorkItemNotifierWhenInnerOptsOut(t *testing.T) {
	inner := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wrapped := shader.Wrap(inner, core.NopShader{})
	if _, ok := wrapped.(core.WorkItemNotifier); ok {
		t.Fatalf("bare FakeWorkPlane shouldn't surface as WorkItemNotifier through the wrapper")
	}
}

// Update without a Title patch skips the extra Get — keeps the hot
// status-change path single-round-trip.
func TestWrap_Update_NoTitle_SkipsGet(t *testing.T) {
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.GetFn = func(_ context.Context, _ core.WorkItemID) (core.WorkItem, error) {
		t.Fatal("Get must not be called when patch has no Title")
		return core.WorkItem{}, errors.New("unreachable")
	}
	wp.UpdateFn = func(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		return core.WorkItem{ID: id, Status: *patch.Status}, nil
	}
	wrapped := shader.Wrap(wp, core.NopShader{})

	closed := "closed"
	_, err := wrapped.UpdateWorkItem(context.Background(), "gm-1",
		core.WorkItemPatch{Status: &closed})
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
}
