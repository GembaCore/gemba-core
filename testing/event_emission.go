package gembatesting

import (
	"context"
	"errors"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// gm-e3.6.3 — every state-changing adaptor method MUST emit a matching
// event on Subscribe within the adaptor's declared latency budget.
// This file holds the budget-selection + capability-gate helpers
// shared by orchestration_event_emission.go and
// workplane_event_emission.go. The probes themselves live in those
// per-plane files so each interface's mutation set keeps its own
// nearby fixtures.

// Default event-delivery budgets per domain.md §3.8 / §2.6:
//
//	SSE / push  → 250 ms (a UI freshness target the conformance
//	              suite enforces; see docs/adaptors/orchestration.md
//	              §latency budget and the Foolery prior-art note)
//	Poll        → 5 s   (slower fallback for adaptors that can only
//	              expose periodic deltas)
//
// Adaptors that don't declare an EventDelivery on their manifest fall
// through to the SSE/push budget — the strictest of the three — so a
// silently-misconfigured adaptor fails closed rather than passing on
// a generous default.
const (
	defaultEventBudgetPush time.Duration = 250 * time.Millisecond
	defaultEventBudgetPoll time.Duration = 5 * time.Second
)

// eventBudget picks the appropriate freshness budget for an
// orchestration adaptor's declared EventDelivery. The WorkPlane has
// no equivalent manifest field today; workplane probes default to the
// push budget directly. When the WorkPlane manifest grows an event-
// delivery declaration we'll route through this helper too.
func eventBudget(delivery core.EventDelivery) time.Duration {
	switch delivery {
	case core.EventDeliveryPoll:
		return defaultEventBudgetPoll
	case core.EventDeliverySSE, core.EventDeliveryPush, "":
		return defaultEventBudgetPush
	default:
		return defaultEventBudgetPush
	}
}

// isUnsupportedOrCapabilityDenied returns true when err signals "this
// method is intentionally unimplemented." Probes treat this as a
// skippable contract — an adaptor that opts out by manifest cannot be
// in violation of the event-emission rule for that method. Both the
// generic ErrUnsupported sentinel and the typed capability_denied
// AdaptorError land here so the harness handles old and new adaptors
// uniformly.
func isUnsupportedOrCapabilityDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, core.ErrUnsupported) {
		return true
	}
	if ae := core.AsAdaptorError(err); ae != nil {
		switch ae.Kind {
		case core.KindUnsupported, core.KindCapabilityDenied:
			return true
		}
	}
	return false
}

// waitForOrchestrationKind drains ch for an event whose Kind matches
// any of want, returning the matching event. Returns (zero-value, false)
// if budget elapses or the channel closes first. Drains-and-discards
// non-matching events so a noisy adaptor (heartbeats, cost samples) can
// still satisfy the probe.
func waitForOrchestrationKind(ctx context.Context, ch <-chan core.OrchestrationEvent, budget time.Duration, want ...string) (core.OrchestrationEvent, bool) {
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	wantSet := make(map[string]struct{}, len(want))
	for _, k := range want {
		wantSet[k] = struct{}{}
	}
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return core.OrchestrationEvent{}, false
			}
			if _, hit := wantSet[ev.Kind]; hit {
				return ev, true
			}
			// Non-matching event — keep draining.
		case <-deadline.C:
			return core.OrchestrationEvent{}, false
		case <-ctx.Done():
			return core.OrchestrationEvent{}, false
		}
	}
}

// waitForWorkPlaneKind is the WorkPlane analog of
// waitForOrchestrationKind.
func waitForWorkPlaneKind(ctx context.Context, ch <-chan core.WorkPlaneEvent, budget time.Duration, want ...string) (core.WorkPlaneEvent, bool) {
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	wantSet := make(map[string]struct{}, len(want))
	for _, k := range want {
		wantSet[k] = struct{}{}
	}
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return core.WorkPlaneEvent{}, false
			}
			if _, hit := wantSet[ev.Kind]; hit {
				return ev, true
			}
		case <-deadline.C:
			return core.WorkPlaneEvent{}, false
		case <-ctx.Done():
			return core.WorkPlaneEvent{}, false
		}
	}
}
