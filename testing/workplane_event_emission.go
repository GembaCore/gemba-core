package gembatesting

import (
	"context"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// gm-e3.6.3 — Group D probes for the WorkPlane: every state-changing
// method MUST emit a matching WorkPlaneEvent on Subscribe within the
// adaptor's declared latency budget. Probes here are capability-gated:
// adaptors whose Subscribe returns ErrUnsupported skip the whole group;
// adaptors that capability-deny a specific method (AttachEvidence under
// EvidenceSynthesisRequired=false, for instance) skip just that probe.
//
// The error message on failure is intentionally tagged
// "mutation_without_event" so an operator running `gemba adaptor test`
// against a half-implemented adaptor sees the contract violated by name.

// probeCreateWorkItemEmitsEvent: CreateWorkItem on a subscribed channel
// MUST yield a workitem_created event (or canonical fallback) within the
// freshness budget. A successful Create with no event is the
// mutation_without_event failure mode the bead names explicitly.
func probeCreateWorkItemEmitsEvent(t probeT, impl core.WorkPlane) {
	t.Helper()
	subscribeOrSkip(t, impl, func(t probeT, ch <-chan core.WorkPlaneEvent) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		id := conformanceWorkItemID("event-emission-create")
		wi := core.WorkItem{
			ID:            id,
			Title:         "conformance: create emits event",
			Status:        "open",
			StateCategory: core.StateUnstarted,
			UpdatedAt:     time.Now(),
		}
		_, err := impl.CreateWorkItem(ctx, wi)
		if err != nil {
			t.Fatalf("CreateWorkItem: %v", err)
		}
		_, ok := waitForWorkPlaneKind(ctx, ch, defaultEventBudgetPush, core.WorkItemEventCreated)
		if !ok {
			t.Errorf("mutation_without_event: CreateWorkItem returned success but no %q event observed within %v",
				core.WorkItemEventCreated, defaultEventBudgetPush)
		}
	})
}

// probeUpdateWorkItemEmitsEvent: UpdateWorkItem MUST emit
// workitem_updated within budget. Skips when the adaptor capability-
// denies UpdateWorkItem (e.g. read-only export adaptor).
//
// Uses the id the backend hands back from CreateWorkItem rather than
// the caller-supplied id — bd assigns from its own prefix pool and
// ignores the caller's value, so we must round-trip through created.ID
// to find the bead in the subsequent update.
func probeUpdateWorkItemEmitsEvent(t probeT, impl core.WorkPlane) {
	t.Helper()
	subscribeOrSkip(t, impl, func(t probeT, ch <-chan core.WorkPlaneEvent) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		id := conformanceWorkItemID("event-emission-update")
		created, err := impl.CreateWorkItem(ctx, core.WorkItem{
			ID:            id,
			Title:         "before",
			Status:        "open",
			StateCategory: core.StateUnstarted,
			UpdatedAt:     time.Now(),
		})
		if err != nil {
			t.Fatalf("CreateWorkItem (seed): %v", err)
		}
		if created.ID == "" {
			t.Fatal("CreateWorkItem (seed): adaptor returned empty ID; cannot drive Update probe")
		}
		// Drain the create event so it doesn't satisfy the update probe
		// by accident — the budget-clock starts fresh on the patch call.
		_, _ = waitForWorkPlaneKind(ctx, ch, defaultEventBudgetPush, core.WorkItemEventCreated)

		title := "after"
		patched, err := impl.UpdateWorkItem(ctx, created.ID, core.WorkItemPatch{Title: &title})
		if isUnsupportedOrCapabilityDenied(err) {
			return // adaptor opts out of UpdateWorkItem; nothing to assert
		}
		if err != nil {
			t.Fatalf("UpdateWorkItem: %v", err)
		}
		if patched.Title != title {
			// Defensive — a passing CRUD probe already covers this; we
			// only re-check so a mocked adaptor that lies about success
			// doesn't shadow the event-emission failure with a confusing
			// mismatch.
			t.Fatalf("UpdateWorkItem: title=%q want %q", patched.Title, title)
		}
		_, ok := waitForWorkPlaneKind(ctx, ch, defaultEventBudgetPush,
			core.WorkItemEventUpdated, core.WorkItemEventClosed)
		if !ok {
			t.Errorf("mutation_without_event: UpdateWorkItem returned success but no %q/%q event observed within %v",
				core.WorkItemEventUpdated, core.WorkItemEventClosed, defaultEventBudgetPush)
		}
	})
}

// probeAttachEvidenceEmitsEvent: AttachEvidence MUST emit
// workitem_evidence_attached within budget. Capability-gated against
// the adaptor's manifest (OpAttachEvidence): denied → skip.
func probeAttachEvidenceEmitsEvent(t probeT, impl core.WorkPlane) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m, err := impl.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if d := core.CheckCapability(m, core.OpAttachEvidence); !d.Allowed {
		// Manifest opts out of AttachEvidence; the contract doesn't
		// require an event for a method the adaptor declares it can't
		// service. Skip silently — the harness's group-level skip
		// reporting is in runner.go.
		return
	}
	subscribeOrSkip(t, impl, func(t probeT, ch <-chan core.WorkPlaneEvent) {
		ctxInner, cancelInner := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelInner()
		id := conformanceWorkItemID("event-emission-evidence")
		created, err := impl.CreateWorkItem(ctxInner, core.WorkItem{
			ID:            id,
			Title:         "conformance: evidence emits event",
			Status:        "open",
			StateCategory: core.StateUnstarted,
			UpdatedAt:     time.Now(),
		})
		if err != nil {
			t.Fatalf("CreateWorkItem (seed): %v", err)
		}
		if created.ID == "" {
			t.Fatal("CreateWorkItem (seed): adaptor returned empty ID")
		}
		_, _ = waitForWorkPlaneKind(ctxInner, ch, defaultEventBudgetPush, core.WorkItemEventCreated)

		err = impl.AttachEvidence(ctxInner, created.ID, core.Evidence{
			ID:         "conformance-evidence-emission",
			Kind:       core.EvidenceKind("test"),
			Source:     "gembatesting",
			Summary:    "conformance probe",
			CapturedAt: time.Now(),
		})
		if isUnsupportedOrCapabilityDenied(err) {
			return
		}
		if err != nil {
			t.Fatalf("AttachEvidence: %v", err)
		}
		_, ok := waitForWorkPlaneKind(ctxInner, ch, defaultEventBudgetPush,
			core.WorkItemEventEvidenceAttached)
		if !ok {
			t.Errorf("mutation_without_event: AttachEvidence returned success but no %q event observed within %v",
				core.WorkItemEventEvidenceAttached, defaultEventBudgetPush)
		}
	})
}

// subscribeOrSkip opens a Subscribe stream against impl and runs body
// against it. ErrUnsupported / capability-denied → skip the body
// silently (the WorkPlane interface itself permits read-only adaptors
// per the workplane.go Subscribe doc). Any other Subscribe error is a
// hard failure since the adaptor implied event support but couldn't
// deliver. Closes the stream on body return.
func subscribeOrSkip(t probeT, impl core.WorkPlane, body func(t probeT, ch <-chan core.WorkPlaneEvent)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := impl.Subscribe(ctx, core.WorkPlaneSubscribeFilter{})
	if isUnsupportedOrCapabilityDenied(err) {
		return
	}
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if ch == nil {
		t.Fatal("Subscribe: returned nil channel without ErrUnsupported")
	}
	body(t, ch)
}

