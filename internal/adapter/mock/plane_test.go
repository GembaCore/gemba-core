package mock

import (
	"context"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

func TestNewOrchestrationPlane_Defaults(t *testing.T) {
	p := NewOrchestrationPlane(Config{ProjectDir: "/tmp/x"})
	d := p.Describe()
	if d.AdaptorID != "mock" {
		t.Errorf("AdaptorID = %q, want %q", d.AdaptorID, "mock")
	}
	if d.Transport != core.TransportAPI {
		t.Errorf("Transport = %q, want %q", d.Transport, core.TransportAPI)
	}
}

func TestPreseedPersonas_OneSessionPerPersona(t *testing.T) {
	p := NewOrchestrationPlane(Config{
		ProjectDir:      "/tmp/x",
		PreseedPersonas: []string{"acceptance-engineer", "documentarian"},
	})
	sessions, err := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 preseeded sessions, got %d", len(sessions))
	}
	personas := map[string]bool{}
	for _, s := range sessions {
		personas[s.Persona] = true
		if pid, _ := s.ProviderMetadata["pane_id"].(string); pid == "" {
			t.Errorf("preseeded session missing pane_id: %+v", s.ProviderMetadata)
		}
	}
	if !personas["acceptance-engineer"] || !personas["documentarian"] {
		t.Errorf("missing persona; got %v", personas)
	}
}

func TestStartSession_ReusesByPaneID(t *testing.T) {
	p := NewOrchestrationPlane(Config{
		ProjectDir:      t.TempDir(),
		PreseedPersonas: []string{"acceptance-engineer"},
	})
	// Discover the preseeded session's pane_id.
	sessions, _ := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	if len(sessions) != 1 {
		t.Fatalf("expected 1 preseeded; got %d", len(sessions))
	}
	preseededPaneID := sessions[0].ProviderMetadata["pane_id"].(string)
	preseededID := sessions[0].ID

	// StartSession with reuse_pane_id should reuse, not mint new.
	// We expect it to fail at the bd-show stage because there's no
	// real bd workspace, but the session-id returned should match
	// the preseeded one.
	prompt := core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":       "e2e0-m1",
			"gemba:persona_id":    "acceptance-engineer",
			"gemba:reuse_pane_id": preseededPaneID,
		},
	}
	sess, err := p.StartSession(context.Background(), "asn-1", prompt)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if sess.ID != preseededID {
		t.Errorf("expected reuse: got id %q, want %q", sess.ID, preseededID)
	}
}

func TestStartSession_RequiresBeadID(t *testing.T) {
	p := NewOrchestrationPlane(Config{ProjectDir: t.TempDir()})
	_, err := p.StartSession(context.Background(), "asn-1", core.SessionPrompt{})
	if err == nil {
		t.Fatal("expected validation error for missing bead_id")
	}
}

func TestEndSession_TransitionsToCompleted(t *testing.T) {
	p := NewOrchestrationPlane(Config{
		ProjectDir:      t.TempDir(),
		PreseedPersonas: []string{"acceptance-engineer"},
	})
	sessions, _ := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	id := sessions[0].ID

	got, err := p.EndSession(context.Background(), id, core.SessionEndCompleted, core.ConfirmNonce("nonce-1"))
	if err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if got.Status != core.SessionCompleted {
		t.Errorf("Status = %q, want SessionCompleted", got.Status)
	}
	if got.EndedAt == nil {
		t.Error("EndedAt must be set on completed session")
	}
}

func TestEndSession_UnknownReturnsNotFound(t *testing.T) {
	p := NewOrchestrationPlane(Config{ProjectDir: t.TempDir()})
	_, err := p.EndSession(context.Background(), "not-real", core.SessionEndCompleted, core.ConfirmNonce("n"))
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestRecycleSession_ReturnsToReady(t *testing.T) {
	p := NewOrchestrationPlane(Config{
		ProjectDir:      t.TempDir(),
		PreseedPersonas: []string{"acceptance-engineer"},
	})
	sessions, _ := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	id := sessions[0].ID

	// Force the session to a non-terminal-non-ready state, then recycle.
	p.mu.Lock()
	s := p.sessions[id]
	s.Status = core.SessionWorking
	p.sessions[id] = s
	p.mu.Unlock()

	got, err := p.RecycleSession(context.Background(), id)
	if err != nil {
		t.Fatalf("RecycleSession: %v", err)
	}
	if got.Status != core.SessionReady {
		t.Errorf("Status = %q, want SessionReady", got.Status)
	}
}

func TestRecycleSession_RefusesTerminalState(t *testing.T) {
	p := NewOrchestrationPlane(Config{
		ProjectDir:      t.TempDir(),
		PreseedPersonas: []string{"acceptance-engineer"},
	})
	sessions, _ := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	id := sessions[0].ID

	p.mu.Lock()
	s := p.sessions[id]
	s.Status = core.SessionFailed
	p.sessions[id] = s
	p.mu.Unlock()

	_, err := p.RecycleSession(context.Background(), id)
	if err == nil {
		t.Fatal("expected validation error for terminal-state recycle")
	}
}

func TestListSessions_FiltersByStatus(t *testing.T) {
	p := NewOrchestrationPlane(Config{
		ProjectDir:      t.TempDir(),
		PreseedPersonas: []string{"acceptance-engineer", "documentarian"},
	})
	got, _ := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionWorking},
	})
	if len(got) != 0 {
		t.Errorf("expected 0 working sessions; got %d", len(got))
	}
	got2, _ := p.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	if len(got2) != 2 {
		t.Errorf("expected 2 ready sessions; got %d", len(got2))
	}
}

func TestSyntheticEscalationLifecycle(t *testing.T) {
	p := NewOrchestrationPlane(Config{ProjectDir: t.TempDir()})
	esc := core.EscalationRequest{
		ID:         "synth-1",
		Source:     core.EscalationHITLApproval,
		Urgency:    core.UrgencyBlocking,
		WorkItemID: "gm-1",
		Title:      "Need approval",
		Prompt:     "Need approval",
	}
	if err := p.InjectSyntheticEscalation(esc); err != nil {
		t.Fatalf("InjectSyntheticEscalation: %v", err)
	}
	open, err := p.ListOpenEscalations(context.Background(), core.EscalationFilter{})
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	if len(open) != 1 || open[0].ID != "synth-1" || open[0].State != core.EscalationOpen {
		t.Fatalf("open escalations = %+v", open)
	}
	resolved, err := p.ResolveEscalation(context.Background(), "synth-1", core.EscalationResolution{
		Kind: core.ResolutionApprove,
	}, "")
	if err != nil {
		t.Fatalf("ResolveEscalation: %v", err)
	}
	if resolved.State != core.EscalationResolved {
		t.Fatalf("resolved state = %q", resolved.State)
	}
	open, _ = p.ListOpenEscalations(context.Background(), core.EscalationFilter{})
	if len(open) != 0 {
		t.Fatalf("resolved escalation should not be open: %+v", open)
	}
}

func TestInterfaceConformance(t *testing.T) {
	var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)
}

// TestSessionIO_DefaultUnsupported pins the Phase A invariant that the
// mock plane embeds core.UnsupportedSessionIO and therefore returns
// KindUnsupported from all three session-IO trio methods. Mock will
// never grow real session-IO — it's a test fixture — so this
// assertion is the long-term contract, not a Phase B placeholder.
func TestSessionIO_DefaultUnsupported(t *testing.T) {
	p := NewOrchestrationPlane(Config{ProjectDir: "/tmp/x"})
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
			ae := core.AsAdaptorError(err)
			if ae == nil || ae.Kind != core.KindUnsupported {
				t.Errorf("%s: want AdaptorError{KindUnsupported}, got %T: %v", c.name, err, err)
			}
		})
	}
}
