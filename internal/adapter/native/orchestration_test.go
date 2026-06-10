package native

import (
	"context"
	"errors"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
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

// TestListAgentsSurfacesRegistry asserts the native plane surfaces its
// loaded agents.toml roster as AgentRefs so the SPA's agent-type picker
// (and the Board's drag-to-In-Progress auto-dispatch) populates.
func TestListAgentsSurfacesRegistry(t *testing.T) {
	reg := agents.Registry{Agents: []agents.AgentType{
		{Name: "claude", Binary: "claude", Preamble: agents.PreambleClaudeMD, Hooks: agents.HookClaudeCode},
		{Name: "shell-only", Binary: "zsh", Preamble: agents.PreambleStdoutBanner, Hooks: agents.HookPromptCommand},
	}}
	p := NewWithConfig(Config{Registry: reg})
	got, err := p.ListAgents(context.Background(), core.AgentFilter{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 agents, got %d", len(got))
	}
	if got[0].Name != "claude" || got[0].Kind != core.AgentKindAgent {
		t.Errorf("agent[0]: %+v", got[0])
	}
	if got[0].Dialect == nil || *got[0].Dialect != "claude" {
		t.Errorf("agent[0].Dialect: want 'claude', got %v", got[0].Dialect)
	}
	if got[1].Name != "shell-only" {
		t.Errorf("agent[1]: %+v", got[1])
	}
}

// TestSessionIO_DefaultUnsupported pins the Phase A invariant that the
// native plane embeds core.UnsupportedSessionIO and therefore returns
// KindUnsupported from all three session-IO trio methods until Phase B
// overrides them. Catches the case where a future change removes the
// embed but forgets to add a real implementation.
func TestSessionIO_DefaultUnsupported(t *testing.T) {
	p := New()
	ctx := context.Background()
	const sid = "sid-phase-a"

	cases := []struct {
		name string
		call func() error
	}{
		{"SendInput", func() error {
			return p.SendInput(ctx, sid, core.SessionInput{Mode: core.InputLiteral, Keys: "x"})
		}},
		{"ResizeSession", func() error {
			return p.ResizeSession(ctx, sid, 80, 24)
		}},
		{"StreamSession", func() error {
			ch, err := p.StreamSession(ctx, sid)
			if ch != nil {
				t.Errorf("StreamSession returned non-nil channel; want nil")
			}
			return err
		}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s: want error, got nil", c.name)
			}
			var aerr *core.AdaptorError
			if !errors.As(err, &aerr) || aerr.Kind != core.KindUnsupported {
				t.Errorf("%s: want AdaptorError{KindUnsupported}, got %T: %v", c.name, err, err)
			}
		})
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
