package native

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// fakeWorkPlane captures AttachEvidence calls for verification. Other
// methods panic so we catch any accidental surface expansion.
type fakeWorkPlane struct {
	mu      sync.Mutex
	attachd []attachCall
}

type attachCall struct {
	id core.WorkItemID
	ev core.Evidence
}

func (f *fakeWorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	return core.CapabilityManifest{}, nil
}
func (f *fakeWorkPlane) ListWorkItems(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
	return nil, nil
}
func (f *fakeWorkPlane) GetWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error) {
	return core.WorkItem{}, nil
}
func (f *fakeWorkPlane) CreateWorkItem(context.Context, core.WorkItem) (core.WorkItem, error) {
	panic("not expected")
}
func (f *fakeWorkPlane) UpdateWorkItem(context.Context, core.WorkItemID, core.WorkItemPatch) (core.WorkItem, error) {
	panic("not expected")
}
func (f *fakeWorkPlane) AttachEvidence(_ context.Context, id core.WorkItemID, ev core.Evidence) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attachd = append(f.attachd, attachCall{id: id, ev: ev})
	return nil
}
func (f *fakeWorkPlane) ListSprints(context.Context) ([]core.Sprint, error) { return nil, nil }
func (f *fakeWorkPlane) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, nil
}
func (f *fakeWorkPlane) Subscribe(context.Context, core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	return nil, core.ErrUnsupported
}

func (f *fakeWorkPlane) calls() []attachCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]attachCall, len(f.attachd))
	copy(out, f.attachd)
	return out
}

func toolUseEvent(sessionID, tool, cmd string) core.OrchestrationEvent {
	return core.OrchestrationEvent{
		ID:        "e-" + cmd,
		Kind:      "tool_use",
		SessionID: sessionID,
		At:        time.Now(),
		Payload: map[string]any{
			"phase":     "pre",
			"tool_name": tool,
			"args": map[string]any{
				"command": cmd,
			},
		},
	}
}

func TestCorrelateBdCloseAttachesEvidence(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	ctx := context.Background()

	c.Handle(ctx, toolUseEvent("s1", "Bash", "bd close gm-foo"))

	calls := wp.calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 AttachEvidence call, got %d", len(calls))
	}
	if calls[0].id != "gm-foo" {
		t.Errorf("bead id: want gm-foo, got %q", calls[0].id)
	}
	if calls[0].ev.Source != "native:session:s1" {
		t.Errorf("source: got %q", calls[0].ev.Source)
	}
	if calls[0].ev.Kind != core.EvidenceLog {
		t.Errorf("kind: got %q", calls[0].ev.Kind)
	}
}

func TestCorrelateDedupesWithinWindow(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	ctx := context.Background()

	c.Handle(ctx, toolUseEvent("s1", "Bash", "bd show gm-foo"))
	c.Handle(ctx, toolUseEvent("s1", "Bash", "bd update gm-foo --priority 1"))
	c.Handle(ctx, toolUseEvent("s1", "Bash", "bd close gm-foo"))

	if got := len(wp.calls()); got != 1 {
		t.Errorf("want 1 call within dedupe window, got %d", got)
	}
}

func TestCorrelateSeparateSessionsDontDedupe(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	ctx := context.Background()

	c.Handle(ctx, toolUseEvent("s1", "Bash", "bd close gm-foo"))
	c.Handle(ctx, toolUseEvent("s2", "Bash", "bd close gm-foo"))

	if got := len(wp.calls()); got != 2 {
		t.Errorf("want 2 calls (different sessions), got %d", got)
	}
}

func TestCorrelateIgnoresNonBashTools(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	c.Handle(context.Background(), toolUseEvent("s1", "Read", "bd close gm-foo"))
	if got := len(wp.calls()); got != 0 {
		t.Errorf("Read tool must not correlate: %d calls", got)
	}
}

func TestCorrelateIgnoresNonBdCommands(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	c.Handle(context.Background(), toolUseEvent("s1", "Bash", "ls -la"))
	c.Handle(context.Background(), toolUseEvent("s1", "Bash", "git status"))
	if got := len(wp.calls()); got != 0 {
		t.Errorf("non-bd commands must not correlate: %d calls", got)
	}
}

func TestCorrelateHandlesLeadingEnvVars(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	c.Handle(context.Background(), toolUseEvent("s1", "Bash", "BD_JSON=1 bd close gm-foo"))
	if got := len(wp.calls()); got != 1 {
		t.Errorf("want 1 call despite env-var prefix, got %d", got)
	}
}

func TestCorrelateHandlesFlagsBeforeSubcommand(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	c.Handle(context.Background(), toolUseEvent("s1", "Bash", "bd --json close gm-foo"))
	if got := len(wp.calls()); got != 1 {
		t.Errorf("want 1 call with leading flag, got %d", got)
	}
}

func TestCorrelateNilWorkPlaneNoOp(t *testing.T) {
	// Zero-config adaptor: correlation is a no-op, but Handle must
	// not panic.
	c := NewCorrelator(nil)
	c.Handle(context.Background(), toolUseEvent("s1", "Bash", "bd close gm-foo"))
}

func TestCorrelateConsumeChannel(t *testing.T) {
	wp := &fakeWorkPlane{}
	c := NewCorrelator(wp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan core.OrchestrationEvent, 2)
	events <- toolUseEvent("s1", "Bash", "bd close gm-foo")
	events <- toolUseEvent("s2", "Bash", "bd close gm-bar")
	close(events)

	c.Consume(ctx, events)

	if got := len(wp.calls()); got != 2 {
		t.Errorf("Consume: want 2 calls, got %d", got)
	}
}
