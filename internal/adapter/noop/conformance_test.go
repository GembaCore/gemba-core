package noop_test

import (
	"context"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/adapter/noop"
	gembatesting "github.com/MikeBengtson/gemba/testing"
)

// TestNoopWorkPlaneConformance wires the noop adaptor through the
// exported harness. This is both a smoke test for the harness and a
// worked example for third-party adaptor authors: clone this pattern,
// swap impl for your adaptor, and pass your own KnownMissingID.
func TestNoopWorkPlaneConformance(t *testing.T) {
	impl := noop.NewWorkPlane()
	gembatesting.RunWorkPlaneConformance(t, impl, &gembatesting.WorkPlaneFixture{
		KnownMissingID: core.WorkItemID("gemba/gemba/gm-does-not-exist"),
	})
}

func TestNoopOrchestrationConformance(t *testing.T) {
	impl := noop.NewOrchestrationPlane()
	gembatesting.RunOrchestrationConformance(t, impl, &gembatesting.OrchestrationFixture{
		KnownMissingSessionID: "sess-does-not-exist",
		SessionStarter: func(t *testing.T, adaptor core.OrchestrationPlaneAdaptor) (string, func()) {
			t.Helper()
			// The harness receives the adaptor through the interface, so
			// we recover the concrete type to mint an assignment before
			// calling StartSession. Real adaptors would seed assignments
			// via their backend's native API; the fixture callback is the
			// place for that bridge.
			op, ok := adaptor.(*noop.OrchestrationPlane)
			if !ok {
				t.Fatalf("SessionStarter: expected *noop.OrchestrationPlane, got %T", adaptor)
			}
			op.CreateAssignment(core.Assignment{
				ID:         "assign-conformance",
				WorkItemID: core.WorkItemID("gemba/gemba/gm-conformance"),
				AgentID:    core.AgentID("gemba/polecats/conformance"),
				Status:     core.AssignmentActive,
			})
			s, err := op.StartSession(context.Background(), "assign-conformance", core.SessionPrompt{})
			if err != nil {
				t.Fatalf("StartSession: %v", err)
			}
			return s.ID, nil
		},
	})
}
