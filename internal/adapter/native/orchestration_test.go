package native

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestDescribeReturnsManifest(t *testing.T) {
	p := New()
	m := p.Describe()
	if m.AdaptorID != AdaptorID {
		t.Errorf("AdaptorID: want %q got %q", AdaptorID, m.AdaptorID)
	}
	if m.Transport != core.TransportAPI {
		t.Errorf("Transport: want API, got %q", m.Transport)
	}
	if m.EventDelivery != core.EventDeliveryPush {
		t.Errorf("EventDelivery: want push, got %q", m.EventDelivery)
	}
	if got, want := len(m.WorkspaceKinds), 2; got != want {
		t.Errorf("WorkspaceKinds len: want %d got %d", want, got)
	}
	if m.DefaultWorkspaceKind != core.WorkspaceWorktree {
		t.Errorf("DefaultWorkspaceKind: want worktree, got %q", m.DefaultWorkspaceKind)
	}
}

func TestStubMethodsReturnUnsupported(t *testing.T) {
	p := New()
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"StartSession", func() error { _, e := p.StartSession(ctx, "a", core.SessionPrompt{}); return e }},
		{"EndSession", func() error {
			_, e := p.EndSession(ctx, "s", core.SessionEndMode(""), core.ConfirmNonce("n"))
			return e
		}},
		{"PeekSession", func() error { _, e := p.PeekSession(ctx, "s"); return e }},
		{"ListPendingRequests", func() error { _, e := p.ListPendingRequests(ctx, "s"); return e }},
		{"AcquireWorkspace", func() error { _, e := p.AcquireWorkspace(ctx, core.WorkspaceRequest{}); return e }},
		{"ReleaseWorkspace", func() error { return p.ReleaseWorkspace(ctx, "w") }},
		{"ClaimNextReady", func() error { _, e := p.ClaimNextReady(ctx, core.ReadyFilter{}, core.AgentRef{}); return e }},
	}
	for _, c := range cases {
		err := c.call()
		if err == nil {
			t.Errorf("%s: want error, got nil", c.name)
			continue
		}
		var aerr *core.AdaptorError
		if !errors.As(err, &aerr) || aerr.Kind != core.KindUnsupported {
			t.Errorf("%s: want AdaptorError{KindUnsupported}, got %T: %v", c.name, err, err)
		}
	}
}

func TestListAgentsReturnsEmptySliceNotNil(t *testing.T) {
	p := New()
	got, err := p.ListAgents(context.Background(), core.AgentFilter{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if got == nil {
		t.Error("ListAgents must return an empty slice, not nil")
	}
}

func TestSubscribeClosesOnContextDone(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()
	// Drain until close — must not deadlock.
	for range ch {
	}
}
