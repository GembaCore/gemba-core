// gm-3jv: live wiring of the OrchestrationPlane into the walk
// agenda aggregator (gm-rfy) plus a Subscribe-stream bridge that
// republishes escalation.* events on the gemba events.Hub so an
// active walk can update its agenda live.

package sources

import (
	"context"
	"log/slog"
	"strings"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/walk"
)

// orchestrationLister is the minimal subset of
// core.OrchestrationPlaneAdaptor the live escalation lister calls.
// Defined here so tests don't have to stub the full 20+-method
// interface to verify the (workspace → ListOpenEscalations) bridge.
type orchestrationLister interface {
	ListOpenEscalations(ctx context.Context, f core.EscalationFilter) ([]core.EscalationRequest, error)
}

// OrchestrationPlaneEscalationLister adapts an
// OrchestrationPlaneAdaptor to walk.EscalationLister. Construct it
// in cmd/gemba serve (or wherever the host plane is bound) and
// pass it as walk.Sources.Escalations when starting a walk.
//
// Workspace gap (documented). The walk's EscalationLister
// interface takes a workspace string — this is the only signature
// the agenda aggregator can wire today, but core.EscalationFilter
// has no Workspace field (escalations are assignment-scoped in the
// core model). This lister therefore ignores the workspace
// argument and returns the full open set; downstream filtering
// happens in the SPA. Adding a Workspace field to EscalationFilter
// is a follow-up to gm-3jv: the OrchestrationPlane would need to
// know which assignment lives in which workspace, which is the
// declared-state / observed-state diff that gm-root §Novel-Mechanism
// §8 already exposes — wiring that join is the right place to
// scope the filter, not on this adapter.
type OrchestrationPlaneEscalationLister struct {
	plane orchestrationLister
}

// NewOrchestrationPlaneEscalationLister constructs the live
// escalation lister. plane MAY be nil — the result is then nil-safe
// and contributes zero escalations to any walk (matching the
// nil-Sources contract gm-rfy ships).
func NewOrchestrationPlaneEscalationLister(plane core.OrchestrationPlaneAdaptor) *OrchestrationPlaneEscalationLister {
	if plane == nil {
		return nil
	}
	return &OrchestrationPlaneEscalationLister{plane: plane}
}

// newListerFromMin is the test seam. Production callers use
// NewOrchestrationPlaneEscalationLister; tests pass a partial fake
// that only satisfies orchestrationLister.
func newListerFromMin(plane orchestrationLister) *OrchestrationPlaneEscalationLister {
	if plane == nil {
		return nil
	}
	return &OrchestrationPlaneEscalationLister{plane: plane}
}

// ListOpenEscalations satisfies walk.EscalationLister. The workspace
// argument is intentionally dropped — see the type doc.
func (l *OrchestrationPlaneEscalationLister) ListOpenEscalations(ctx context.Context, _ string) ([]core.EscalationRequest, error) {
	if l == nil || l.plane == nil {
		return nil, nil
	}
	return l.plane.ListOpenEscalations(ctx, core.EscalationFilter{})
}

// Compile-time: confirm the interface match.
var _ walk.EscalationLister = (*OrchestrationPlaneEscalationLister)(nil)

// ── Subscription bridge ─────────────────────────────────────────

// orchestrationSubscriber is the minimal subset of
// core.OrchestrationPlaneAdaptor needed to attach the bridge.
// Tests use a partial fake so the bridge contract is verifiable
// without spinning up a real adaptor.
type orchestrationSubscriber interface {
	Subscribe(ctx context.Context, f core.SubscribeFilter) (<-chan core.OrchestrationEvent, error)
	Describe() core.OrchestrationCapabilityManifest
}

// WalkEscalationChanged is the canonical Kind republished onto the
// events.Hub when the orchestration bridge sees an escalation
// state transition (raised, opened, resolved, canceled, expired).
// Subscribers — primarily an active walk's UI — refetch their
// agenda on receipt. The single Kind covers all transitions
// because the agenda is rebuilt from the open-set on each refresh;
// distinguishing add/remove client-side is unnecessary.
const WalkEscalationChanged events.Kind = "walk.escalation_changed"

// escalationKindPrefix is the OrchestrationEvent kind namespace
// the bridge listens on. Adaptors emit events such as
// "escalation.raised", "escalation.resolved", "escalation.canceled"
// (domain.md §3.5 + the OrchestrationPlane contract on
// ResolveEscalation). The prefix match is intentional — future
// kinds in the same namespace participate without a code change.
const escalationKindPrefix = "escalation."

// StartOrchestrationBridge subscribes to the OrchestrationPlane's
// event stream and republishes escalation.* events as
// walk.escalation_changed frames on the gemba events.Hub. Returns
// when the subscription is established (the pump runs in a
// goroutine until ctx is cancelled or the upstream channel
// closes).
//
// Nil-safe: if plane or hub is nil, returns immediately and the
// bridge is a no-op. A Subscribe error logs at warn and returns;
// a deployment without a subscribable adaptor degrades to the
// polling path the SPA already supports.
//
// Idempotency: the bridge does NOT dedupe — the events.Hub fan-out
// is already a non-blocking, single-fire-per-subscriber surface,
// and adaptors are contractually obliged to emit one event per
// escalation state transition. A retry storm on the upstream side
// is a contract violation, not something this bridge papers over.
func StartOrchestrationBridge(ctx context.Context, plane core.OrchestrationPlaneAdaptor, hub *events.Hub) {
	if plane == nil || hub == nil {
		return
	}
	startOrchestrationBridge(ctx, plane, hub)
}

// startOrchestrationBridge is the test seam — accepts the smaller
// orchestrationSubscriber interface so tests can plug a minimal
// fake without implementing the full adaptor surface.
func startOrchestrationBridge(ctx context.Context, plane orchestrationSubscriber, hub *events.Hub) {
	ch, err := plane.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		slog.Warn("walk: orchestration bridge Subscribe failed; agenda will not refresh on upstream events",
			"err", err)
		return
	}
	adaptorID := plane.Describe().AdaptorID
	go pumpOrchestrationBridge(ctx, ch, hub, adaptorID)
}

// pumpOrchestrationBridge is the per-event loop. Exits on ctx
// cancellation or upstream close. Public-symbol-free so callers
// don't get a handle they could leak.
func pumpOrchestrationBridge(ctx context.Context, in <-chan core.OrchestrationEvent, hub *events.Hub, adaptorID string) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			if !isEscalationEvent(ev.Kind) {
				continue
			}
			hub.Publish(translateEscalationEvent(adaptorID, ev))
		}
	}
}

// isEscalationEvent reports whether an OrchestrationEvent's Kind
// participates in the escalation lifecycle namespace.
func isEscalationEvent(kind string) bool {
	return strings.HasPrefix(kind, escalationKindPrefix)
}

// translateEscalationEvent shapes an OrchestrationEvent into the
// canonical walk.escalation_changed GembaEvent. Scope hooks
// (assignment, session, agent, work_item) ride through; the
// payload retains the upstream kind so subscribers that care
// about the specific transition (resolved vs raised) can inspect
// it without losing the canonical-event property.
func translateEscalationEvent(adaptorID string, ev core.OrchestrationEvent) events.GembaEvent {
	payload := map[string]any{}
	for k, v := range ev.Payload {
		payload[k] = v
	}
	payload["upstream_kind"] = ev.Kind
	out := events.GembaEvent{
		ID:           ev.ID,
		Kind:         WalkEscalationChanged,
		At:           ev.At,
		Source:       events.Source{Plane: events.PlaneOrchestration, AdaptorID: adaptorID},
		AssignmentID: ev.AssignmentID,
		SessionID:    ev.SessionID,
		AgentID:      string(ev.AgentID),
		Payload:      payload,
	}
	if wid, ok := ev.Payload["work_item_id"].(string); ok {
		out.WorkItemID = wid
	}
	return out
}

// ── Stub sources (TODO — see doc.go) ────────────────────────────

// StubWitnessLister is the placeholder for the witness-finding
// escalation source. Witness currently surfaces findings via mail
// rather than as core.EscalationRequest; once the witness rig
// emits escalation.raised events (or a mail→escalation bridge
// lands), swap this for a live implementation. Tracked as a
// follow-up to gm-3jv.
//
// Returns an empty slice so a Sources struct that wires this stub
// is the same shape as one that wires a future live lister.
func StubWitnessLister(_ context.Context, _ string) ([]core.EscalationRequest, error) {
	// TODO(gm-3jv-followup): wire witness mail → EscalationRequest.
	return nil, nil
}

// StubRefineryRejectionLister is the placeholder for the refinery
// merge-rejection source. Refinery currently writes a beads
// comment on rejection without an orchestration event; the
// follow-up needs an EscalationKind "refinery_rejection" plus a
// way for the refinery rig to publish through the
// OrchestrationPlane (or a side-channel adaptor).
func StubRefineryRejectionLister(_ context.Context, _ string) ([]core.EscalationRequest, error) {
	// TODO(gm-3jv-followup): wire refinery rejection → EscalationRequest.
	return nil, nil
}

// StubBeadsDegradedLister is the placeholder for synthesizing
// escalations from beads-daemon degraded health (gm-b1). Today the
// signal is exposed via /api/adaptors and /api/adaptors/stream as
// a HealthBus state — promoting it to a walk agenda item needs a
// translator that mints a stable EscalationRequest.ID per
// degraded-state span (so a still-degraded daemon doesn't surface
// a fresh agenda row on every walk).
func StubBeadsDegradedLister(_ context.Context, _ string) ([]core.EscalationRequest, error) {
	// TODO(gm-3jv-followup): translate adaptor HealthBus degraded → EscalationRequest.
	return nil, nil
}
