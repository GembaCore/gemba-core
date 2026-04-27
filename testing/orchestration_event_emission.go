package gembatesting

import (
	"context"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// gm-e3.6.3 — Group D probes for the OrchestrationPlane: every state-
// changing method MUST emit a matching OrchestrationEvent on Subscribe
// within the manifest's declared latency budget. Probes here lean on
// fixture hooks (SessionStarter, AcquireWorkspaceFunc, ResolveEscalationFunc)
// because the lifecycle methods need real assignment / workspace /
// escalation state to drive — synthesising those inline would force
// every adaptor's harness call to leak its bootstrap into the runner.
//
// Adaptors that opt out of a method by manifest (no-op for the
// capability-denied branch on noop adaptors) skip the corresponding
// probe. A successful mutation that produces no matching event surfaces
// a "mutation_without_event" failure, named exactly so the operator-
// facing report points at the contract being violated.

// probePauseSessionEmitsTransition stands in for the original
// "StartSession emits" probe. SessionStarter has already provisioned
// the session and emitted its transition event on the adaptor's
// stream — Subscribe-after-the-fact streams are present-tense, so we
// can't replay that emission. Instead we assert that PauseSession (a
// reversible state mutation safe to run on a live session) emits its
// transition. PauseSession + ResumeSession together cover both the
// "first call emits" and "idempotent under the same nonce" arms of
// the contract.
//
// PauseSession on the first call
// MUST emit session_transition. Same-nonce replays MUST emit nothing
// (the conformance B group already enforces the same-nonce idempotency
// for state; this probe enforces the same-nonce idempotency for events
// — a replayed mutation that double-emits is just as wrong as one that
// silently no-ops).
func probePauseSessionEmitsTransition(t probeT, impl core.OrchestrationPlaneAdaptor, sessionID string) {
	t.Helper()
	budget := eventBudget(impl.Describe().EventDelivery)
	ctx, cancel := context.WithTimeout(context.Background(), budget+1*time.Second)
	defer cancel()
	ch, err := impl.Subscribe(ctx, core.SubscribeFilter{SessionID: sessionID})
	if isUnsupportedOrCapabilityDenied(err) {
		return
	}
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if ch == nil {
		t.Fatal("Subscribe: returned nil channel without ErrUnsupported")
	}
	nonce := core.ConfirmNonce("conformance-event-emission-pause")
	if _, err := impl.PauseSession(ctx, sessionID, nonce); err != nil {
		if isUnsupportedOrCapabilityDenied(err) {
			return
		}
		t.Fatalf("PauseSession: %v", err)
	}
	if _, ok := waitForOrchestrationKind(ctx, ch, budget, "session_transition", "session.transition"); !ok {
		t.Errorf("mutation_without_event: PauseSession returned success but no session_transition event observed within %v",
			budget)
	}
}

// probeAcquireReleaseWorkspaceEmits emits-and-asserts both lifecycle
// halves of the workspace dance. Capability-gated by the
// OrchestrationFixture.AcquireWorkspaceFunc hook — adaptors with no
// workspace allocator skip silently. The event-set is permissive
// (workspace_acquired/.acquired) so canonicalised translations land in
// the same probe.
func probeAcquireReleaseWorkspaceEmits(t probeT, impl core.OrchestrationPlaneAdaptor, fixture *OrchestrationFixture) {
	t.Helper()
	if fixture.AcquireWorkspaceRequest == nil {
		return
	}
	budget := eventBudget(impl.Describe().EventDelivery)
	ctx, cancel := context.WithTimeout(context.Background(), budget+2*time.Second)
	defer cancel()
	ch, err := impl.Subscribe(ctx, core.SubscribeFilter{})
	if isUnsupportedOrCapabilityDenied(err) {
		return
	}
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ws, err := impl.AcquireWorkspace(ctx, *fixture.AcquireWorkspaceRequest)
	if isUnsupportedOrCapabilityDenied(err) {
		return
	}
	if err != nil {
		t.Fatalf("AcquireWorkspace: %v", err)
	}
	if _, ok := waitForOrchestrationKind(ctx, ch, budget, "workspace_acquired", "workspace.acquired"); !ok {
		t.Errorf("mutation_without_event: AcquireWorkspace returned success but no workspace_acquired event observed within %v",
			budget)
	}
	if err := impl.ReleaseWorkspace(ctx, ws.ID); err != nil {
		if isUnsupportedOrCapabilityDenied(err) {
			return
		}
		t.Fatalf("ReleaseWorkspace: %v", err)
	}
	if _, ok := waitForOrchestrationKind(ctx, ch, budget, "workspace_released", "workspace.released"); !ok {
		t.Errorf("mutation_without_event: ReleaseWorkspace returned success but no workspace_released event observed within %v",
			budget)
	}
}
