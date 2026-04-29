package gembatesting_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/noop"
	gembatesting "github.com/GembaCore/gemba-core/testing"
)

// TestRunWorkPlaneProbes_NoopGreen is the programmatic mirror of the
// testing.T-based TestNoopWorkPlaneConformance: RunWorkPlaneProbes must
// produce a green Report for the noop adaptor. Protects the CLI path.
func TestRunWorkPlaneProbes_NoopGreen(t *testing.T) {
	report := gembatesting.RunWorkPlaneProbes(noop.NewWorkPlane(), &gembatesting.WorkPlaneFixture{
		KnownMissingID: core.WorkItemID("gemba/gemba/gm-does-not-exist"),
	})
	if !report.Passed() {
		t.Fatalf("noop work plane probes must be green; report=%+v", report)
	}
	// A–F are always present; G–K land with gm-l26, gated by manifest
	// fields the noop adaptor doesn't advertise — they surface as
	// NotApplicable rather than being dropped.
	if got := len(report.Groups); got != 11 {
		t.Errorf("want 11 groups (A–K), got %d", got)
	}
}

func TestRunOrchestrationProbes_NoopGreen(t *testing.T) {
	impl := noop.NewOrchestrationPlane()
	var seq int
	report := gembatesting.RunOrchestrationProbes(impl, &gembatesting.OrchestrationFixture{
		KnownMissingSessionID: "sess-does-not-exist",
		ProgrammaticSessionStarter: func(a core.OrchestrationPlaneAdaptor) (string, func(), error) {
			op := a.(*noop.OrchestrationPlane)
			seq++
			id := fmt.Sprintf("assign-prog-%d", seq)
			op.CreateAssignment(core.Assignment{
				ID:         id,
				WorkItemID: core.WorkItemID("gemba/gemba/gm-prog"),
				AgentID:    core.AgentID("gemba/polecats/prog"),
				Status:     core.AssignmentActive,
			})
			s, err := op.StartSession(context.Background(), id, core.SessionPrompt{})
			if err != nil {
				return "", nil, err
			}
			return s.ID, nil, nil
		},
	})
	if !report.Passed() {
		t.Fatalf("noop orchestration probes must be green; report=%+v", report)
	}
}

// TestRunProbes_SurfacesMissingFixtureAsSkip asserts the programmatic
// runner treats a missing fixture as Skipped, not Failed. CI relies on
// this so "no KnownMissingID in my config" doesn't look like a contract
// violation.
func TestRunProbes_SurfacesMissingFixtureAsSkip(t *testing.T) {
	report := gembatesting.RunWorkPlaneProbes(noop.NewWorkPlane(), nil)
	if !report.Passed() {
		t.Fatalf("report with only skipped probes should still Pass(); %+v", report)
	}
	_, _, skipped := report.Totals()
	if skipped == 0 {
		t.Errorf("want at least one skipped probe when fixture is nil, got 0")
	}
}

// TestRunProbes_FailuresMarkReportNotPassed checks the fail path: pass
// a broken WorkPlane whose Describe returns an invalid manifest and
// confirm Report.Passed() flips to false.
func TestRunProbes_FailuresMarkReportNotPassed(t *testing.T) {
	report := gembatesting.RunWorkPlaneProbes(&brokenWorkPlane{}, nil)
	if report.Passed() {
		t.Fatalf("broken workplane must not pass; %+v", report)
	}
	_, failed, _ := report.Totals()
	if failed == 0 {
		t.Errorf("want at least one failure, got 0")
	}
}

type brokenWorkPlane struct{}

func (brokenWorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	// Invalid: empty StateMap fails Validate.
	return core.CapabilityManifest{
		AdaptorName:     "broken",
		ProtocolVersion: core.ProtocolVersion,
		Transport:       core.TransportJSONL,
	}, nil
}
func (brokenWorkPlane) ListWorkItems(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
	return nil, nil
}
func (brokenWorkPlane) GetWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error) {
	return core.WorkItem{}, nil
}
func (brokenWorkPlane) CreateWorkItem(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
	return wi, nil
}
func (brokenWorkPlane) UpdateWorkItem(_ context.Context, _ core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
	return core.WorkItem{}, nil
}
func (brokenWorkPlane) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error {
	return nil
}
func (brokenWorkPlane) ListSprints(context.Context) ([]core.Sprint, error) { return nil, nil }
func (brokenWorkPlane) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, nil
}
func (brokenWorkPlane) Subscribe(context.Context, core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	return nil, core.ErrUnsupported
}
