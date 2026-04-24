package native

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

func openEvent(sessionID, escID, kind string) core.OrchestrationEvent {
	return core.OrchestrationEvent{
		ID:        escID,
		Kind:      "escalation_opened",
		SessionID: sessionID,
		At:        time.Now(),
		Payload: map[string]any{
			"escalation_kind": kind,
			"title":           "Run bash command",
		},
	}
}

func TestEscalationIndexOpenAndResolve(t *testing.T) {
	x := newEscalationIndex()
	x.handleEvent(openEvent("s1", "e1", "permission_prompt"))
	x.handleEvent(openEvent("s1", "e2", "hitl_approval"))

	pending := x.forSession("s1")
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}

	x.handleEvent(core.OrchestrationEvent{
		Kind:      "escalation_resolved",
		ID:        "e1",
		SessionID: "s1",
		At:        time.Now(),
	})
	pending = x.forSession("s1")
	if len(pending) != 1 {
		t.Errorf("want 1 pending after resolve, got %d", len(pending))
	}
	if pending[0].ID != "e2" {
		t.Errorf("wrong entry left: %q", pending[0].ID)
	}
}

func TestEscalationIndexUserMessageResolvesOldest(t *testing.T) {
	x := newEscalationIndex()
	// Two opens, first one is oldest.
	x.handleEvent(openEvent("s1", "e1", "permission_prompt"))
	time.Sleep(time.Millisecond)
	x.handleEvent(openEvent("s1", "e2", "permission_prompt"))

	x.handleEvent(core.OrchestrationEvent{
		Kind:      "user_message",
		SessionID: "s1",
		At:        time.Now(),
	})
	pending := x.forSession("s1")
	if len(pending) != 1 {
		t.Fatalf("user_message must resolve one, got %d left", len(pending))
	}
	// The oldest (e1) should be gone; e2 remains.
	if pending[0].ID != "e2" {
		t.Errorf("wrong entry resolved: %q remained", pending[0].ID)
	}
}

func TestEscalationIndexAllFilter(t *testing.T) {
	x := newEscalationIndex()
	x.handleEvent(openEvent("s1", "e1", "permission_prompt"))
	x.handleEvent(openEvent("s2", "e2", "hitl_approval"))

	all := x.all(core.EscalationFilter{})
	if len(all) != 2 {
		t.Errorf("want 2 total, got %d", len(all))
	}
	perm := x.all(core.EscalationFilter{Source: core.EscalationPermissionPrompt})
	if len(perm) != 1 || perm[0].ID != "e1" {
		t.Errorf("filter by permission_prompt: %+v", perm)
	}
}

func TestResolveEscalationSendsKeys(t *testing.T) {
	fb := newFakeBackend()
	p, sess := startForEnd(t, fb)
	// Simulate an escalation on this session.
	p.escalations.handleEvent(openEvent(sess.ID, "e1", "permission_prompt"))

	res := core.EscalationResolution{
		Kind:       core.ResolutionApprove,
		ResolvedAt: time.Now(),
	}
	out, err := p.ResolveEscalation(context.Background(), "e1", res, "")
	if err != nil {
		t.Fatalf("ResolveEscalation: %v", err)
	}
	if out.State != core.EscalationResolved {
		t.Errorf("state: got %q", out.State)
	}

	// Pending list should be empty now.
	pending, _ := p.ListPendingRequests(context.Background(), sess.ID)
	if len(pending) != 0 {
		t.Errorf("resolved entry should be gone, got %d", len(pending))
	}
}

func TestResolveEscalationUnknownReturnsNotFound(t *testing.T) {
	fb := newFakeBackend()
	p, _ := startForEnd(t, fb)
	_, err := p.ResolveEscalation(context.Background(), "nope", core.EscalationResolution{Kind: core.ResolutionApprove}, "")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindSessionNotFound {
		t.Errorf("want KindSessionNotFound, got %T: %v", err, err)
	}
}

func TestListPendingRequestsUnknownSession(t *testing.T) {
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry(), RepoRoot: initRepo(t)})
	_, err := p.ListPendingRequests(context.Background(), "no-such")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindSessionNotFound {
		t.Errorf("want KindSessionNotFound, got %T: %v", err, err)
	}
}

func TestComposeEscalationReply(t *testing.T) {
	cases := map[core.EscalationResolutionKind]string{
		core.ResolutionApprove: "yes",
		core.ResolutionDeny:    "no",
		core.ResolutionDefer:   "",
	}
	for k, want := range cases {
		got, err := composeEscalationReply(core.EscalationResolution{Kind: k})
		if err != nil {
			t.Errorf("%s: unexpected err %v", k, err)
			continue
		}
		if got != want {
			t.Errorf("%s: want %q got %q", k, want, got)
		}
	}
	// Modify requires string value.
	if _, err := composeEscalationReply(core.EscalationResolution{Kind: core.ResolutionModify}); err == nil {
		t.Error("modify with non-string should error")
	}
	got, err := composeEscalationReply(core.EscalationResolution{Kind: core.ResolutionModify, Value: "custom reply"})
	if err != nil || got != "custom reply" {
		t.Errorf("modify string: got (%q, %v)", got, err)
	}
}
