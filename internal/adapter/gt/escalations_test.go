package gt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

const sampleEscalateListJSON = `[
  {
    "id": "hq-n8mb",
    "title": "[HIGH] dolt diverged",
    "description": "Escalation ID: hq-wisp-9kd3\nSeverity: high\nFrom: gemba/crew/mike4\n\nReason: ...",
    "status": "open",
    "priority": 1,
    "issue_type": "task",
    "created_at": "2026-04-25T18:04:54Z",
    "updated_at": "2026-04-25T18:04:54Z",
    "created_by": "gemba/crew/mike4",
    "assignee": "mayor/",
    "labels": ["gt:escalation","severity:high","from:gemba/crew/mike4"]
  },
  {
    "id": "hq-2g50",
    "title": "[MEDIUM] noisy log",
    "description": "Escalation ID: hq-wisp-g7jg\nSeverity: medium\nFrom: deacon/dogs/alpha\n\n",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "created_at": "2026-04-26T09:00:00Z",
    "updated_at": "2026-04-26T09:00:00Z",
    "created_by": "deacon/dogs/alpha",
    "assignee": "mayor/",
    "labels": ["gt:escalation","severity:medium"]
  },
  {
    "id": "hq-old",
    "title": "[LOW] resolved",
    "description": "...",
    "status": "closed",
    "priority": 3,
    "issue_type": "task",
    "created_at": "2026-04-20T00:00:00Z",
    "updated_at": "2026-04-20T00:00:00Z",
    "created_by": "x",
    "labels": ["gt:escalation","severity:low"]
  }
]`

func newPlaneWithFakeEscalations(json string) *OrchestrationPlane {
	return &OrchestrationPlane{
		run: func(_ context.Context, args ...string) ([]byte, error) {
			if len(args) >= 2 && args[0] == "escalate" && args[1] == "list" {
				return []byte(json), nil
			}
			return nil, errors.New("unexpected gt command: " + strings.Join(args, " "))
		},
	}
}

func TestListOpenEscalations_MapsRowsAndDropsClosed(t *testing.T) {
	o := newPlaneWithFakeEscalations(sampleEscalateListJSON)
	got, err := o.ListOpenEscalations(context.Background(), core.EscalationFilter{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 open escalations; got %d", len(got))
	}
	if got[0].ID != "hq-n8mb" || got[1].ID != "hq-2g50" {
		t.Errorf("unexpected order: %v", []string{got[0].ID, got[1].ID})
	}
	for _, r := range got {
		if r.Source != core.EscalationOrchestratorPause {
			t.Errorf("Source = %q, want orchestrator_pause", r.Source)
		}
		if r.State != core.EscalationOpen {
			t.Errorf("State = %q, want open", r.State)
		}
	}
}

func TestListOpenEscalations_SeverityToUrgency(t *testing.T) {
	o := newPlaneWithFakeEscalations(sampleEscalateListJSON)
	got, _ := o.ListOpenEscalations(context.Background(), core.EscalationFilter{})
	if got[0].Urgency != core.UrgencyBlocking {
		t.Errorf("severity:high → blocking; got %q", got[0].Urgency)
	}
	if got[1].Urgency != core.UrgencyAdvisory {
		t.Errorf("severity:medium → advisory; got %q", got[1].Urgency)
	}
}

func TestListOpenEscalations_AgentIDFilter(t *testing.T) {
	o := newPlaneWithFakeEscalations(sampleEscalateListJSON)
	got, err := o.ListOpenEscalations(context.Background(), core.EscalationFilter{
		AgentID: "gemba/crew/mike4",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "hq-n8mb" {
		t.Errorf("agent filter: expected only hq-n8mb; got %+v", got)
	}
}

func TestListOpenEscalations_UrgencyFilter(t *testing.T) {
	o := newPlaneWithFakeEscalations(sampleEscalateListJSON)
	got, err := o.ListOpenEscalations(context.Background(), core.EscalationFilter{
		Urgency: core.UrgencyBlocking,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "hq-n8mb" {
		t.Errorf("urgency filter: expected only hq-n8mb; got %+v", got)
	}
}

func TestListOpenEscalations_EmptyOutputIsBenign(t *testing.T) {
	for _, body := range []string{"", "null", "[]"} {
		o := newPlaneWithFakeEscalations(body)
		got, err := o.ListOpenEscalations(context.Background(), core.EscalationFilter{})
		if err != nil {
			t.Errorf("body=%q: unexpected err: %v", body, err)
		}
		if len(got) != 0 {
			t.Errorf("body=%q: expected empty, got %d", body, len(got))
		}
	}
}

func TestListOpenEscalations_GTErrorBubblesUp(t *testing.T) {
	o := &OrchestrationPlane{
		run: func(_ context.Context, _ ...string) ([]byte, error) {
			return nil, errors.New("gt boom")
		},
	}
	if _, err := o.ListOpenEscalations(context.Background(), core.EscalationFilter{}); err == nil {
		t.Fatal("expected error to bubble up")
	}
}

func TestListOpenEscalations_BadJSONRejected(t *testing.T) {
	o := newPlaneWithFakeEscalations("not json")
	if _, err := o.ListOpenEscalations(context.Background(), core.EscalationFilter{}); err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestSeverityFromLabels_KnownAndMissing(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
	}{
		{[]string{"severity:high", "x"}, "high"},
		{[]string{"x", "severity:medium"}, "medium"},
		{[]string{"x"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		got := severityFromLabels(c.labels)
		if got != c.want {
			t.Errorf("labels=%v: got %q want %q", c.labels, got, c.want)
		}
	}
}

func TestManifestDeclaresOrchestratorPause(t *testing.T) {
	m := (&OrchestrationPlane{}).Describe()
	found := false
	for _, k := range m.EscalationKinds {
		if k == core.EscalationOrchestratorPause {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("manifest missing EscalationOrchestratorPause; got %v", m.EscalationKinds)
	}
}

// Used to verify that toEscalationRequest doesn't drop the
// CreatedAt timestamp during the round-trip.
func TestToEscalationRequest_PreservesCreatedAt(t *testing.T) {
	in := gtEscalation{
		ID:        "id-1",
		Title:     "t",
		CreatedAt: time.Date(2026, 4, 25, 18, 4, 54, 0, time.UTC),
		Labels:    []string{"severity:low"},
		CreatedBy: "x",
	}
	got := toEscalationRequest(in)
	if !got.CreatedAt.Equal(in.CreatedAt) {
		t.Errorf("CreatedAt drift: %v vs %v", got.CreatedAt, in.CreatedAt)
	}
}
