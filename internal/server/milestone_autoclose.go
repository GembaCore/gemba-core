// Wrapper state reconciliation (gm-mqiz, gm-1o9n).
//
// Epic and milestone beads are wrappers: their visible board state is
// derived from the runnable leaf work under them. A wrapper is:
//
//   - started when any leaf is started, or when some leaves are closed
//     and others remain open (a cascade is underway);
//   - completed when every runnable leaf is completed/canceled;
//   - otherwise the most advanced open state visible among the leaves.
//
// Best-effort: failures are logged and swallowed. The operator's
// original patch is what matters; reconciliation is a courtesy on top.

package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/events"
)

func isWrapperKind(kind string) bool {
	return kind == core.KindMilestone || kind == "epic"
}

// reconcileWrapperAncestors is invoked after a successful UpdateWorkItem.
// `changed` is the patched item as the adaptor returned it. Any epic or
// milestone ancestors are reconciled from the leaves below them, then
// their ancestors are reconciled in turn.
func (r *Router) reconcileWrapperAncestors(ctx context.Context, wp core.WorkPlane, changed core.WorkItem) {
	if wp == nil {
		return
	}
	r.reconcileParents(ctx, wp, changed, changed.ID, map[core.WorkItemID]bool{})
}

func (r *Router) reconcileParents(
	ctx context.Context,
	wp core.WorkPlane,
	child core.WorkItem,
	lastChanged core.WorkItemID,
	seen map[core.WorkItemID]bool,
) {
	for _, parentID := range parentIDs(child) {
		if parentID == "" || seen[parentID] {
			continue
		}
		seen[parentID] = true

		parent, err := wp.GetWorkItem(ctx, parentID)
		if err != nil {
			slog.Warn("wrapper reconcile: parent fetch failed",
				"child_id", child.ID, "parent_id", parentID, "err", err)
			continue
		}
		if !isWrapperKind(parent.Kind) {
			continue
		}

		next, ok, err := deriveWrapperState(ctx, wp, parent, map[core.WorkItemID]bool{})
		if err != nil {
			slog.Warn("wrapper reconcile: derive failed",
				"wrapper_id", parent.ID, "err", err)
			continue
		}
		if !ok {
			continue
		}
		updated := parent
		if parent.StateCategory != next {
			patch := core.WorkItemPatch{StateCategory: &next}
			updated, err = wp.UpdateWorkItem(ctx, parent.ID, patch)
			if err != nil {
				slog.Warn("wrapper reconcile: patch failed",
					"wrapper_id", parent.ID, "next_state", next, "err", err)
				continue
			}
			if parent.Kind == core.KindMilestone && next == core.StateCompleted {
				r.publishMilestoneClosedEscalation(ctx, updated, lastChanged)
			}
		}
		r.reconcileParents(ctx, wp, updated, lastChanged, seen)
	}
}

func parentIDs(child core.WorkItem) []core.WorkItemID {
	out := []core.WorkItemID{}
	for _, rel := range child.Relationships {
		if rel.Kind == core.RelParentChild && rel.To == child.ID && rel.From != "" {
			out = append(out, rel.From)
		}
	}
	return out
}

func childIDs(parent core.WorkItem) []core.WorkItemID {
	out := []core.WorkItemID{}
	for _, rel := range parent.Relationships {
		if rel.Kind == core.RelParentChild && rel.From == parent.ID && rel.To != "" {
			out = append(out, rel.To)
		}
	}
	return out
}

type wrapperRollup struct {
	total    int
	backlog  int
	nextUp   int
	staged   int
	started  int
	terminal int
}

func deriveWrapperState(
	ctx context.Context,
	wp core.WorkPlane,
	wrapper core.WorkItem,
	seen map[core.WorkItemID]bool,
) (core.StateCategory, bool, error) {
	rollup, err := collectLeafRollup(ctx, wp, wrapper, seen)
	if err != nil {
		return "", false, err
	}
	if rollup.total == 0 {
		return "", false, nil
	}
	return rollup.derivedState(), true, nil
}

func collectLeafRollup(
	ctx context.Context,
	wp core.WorkPlane,
	wrapper core.WorkItem,
	seen map[core.WorkItemID]bool,
) (wrapperRollup, error) {
	var out wrapperRollup
	if seen[wrapper.ID] {
		return out, nil
	}
	seen[wrapper.ID] = true

	for _, id := range childIDs(wrapper) {
		if id == "" || seen[id] {
			continue
		}
		ch, err := wp.GetWorkItem(ctx, id)
		if err != nil {
			return out, err
		}
		if isWrapperKind(ch.Kind) {
			nested, err := collectLeafRollup(ctx, wp, ch, seen)
			if err != nil {
				return out, err
			}
			out.add(nested)
			continue
		}
		out.addLeaf(ch.StateCategory)
	}
	return out, nil
}

func (r *wrapperRollup) add(other wrapperRollup) {
	r.total += other.total
	r.backlog += other.backlog
	r.nextUp += other.nextUp
	r.staged += other.staged
	r.started += other.started
	r.terminal += other.terminal
}

func (r *wrapperRollup) addLeaf(state core.StateCategory) {
	r.total++
	switch state {
	case core.StateStarted:
		r.started++
	case core.StateStaged:
		r.staged++
	case core.StateUnstarted:
		r.nextUp++
	case core.StateBacklog:
		r.backlog++
	case core.StateCompleted, core.StateCanceled:
		r.terminal++
	}
}

func (r wrapperRollup) derivedState() core.StateCategory {
	if r.total > 0 && r.terminal == r.total {
		return core.StateCompleted
	}
	if r.started > 0 || r.terminal > 0 {
		return core.StateStarted
	}
	if r.staged > 0 {
		return core.StateStaged
	}
	if r.nextUp > 0 {
		return core.StateUnstarted
	}
	if r.backlog > 0 {
		return core.StateBacklog
	}
	return core.StateUnstarted
}

// publishMilestoneClosedEscalation fans an escalation.opened event
// through the events hub when a milestone has just been auto-closed.
// We piggyback on EscalationOpened (the canonical "operator should
// look at this" signal) rather than minting a new Kind; payload
// carries enough context (source=milestone_autoclosed, last_child)
// for subscribers that want to discriminate.
//
// Hub is optional — many tests construct a Router without subscribing
// to /events. A nil hub is a no-op so the auto-close path stays a
// pure side-effect-on-WorkPlane operation under those tests.
func (r *Router) publishMilestoneClosedEscalation(ctx context.Context, milestone core.WorkItem, lastChild core.WorkItemID) {
	if r == nil || r.eventsHub == nil {
		return
	}
	ev := events.GembaEvent{
		ID:         "milestone-autoclose-" + string(milestone.ID),
		Kind:       events.EscalationOpened,
		At:         time.Now().UTC(),
		Source:     events.Source{Plane: events.PlaneWorkPlane, AdaptorID: "gemba.milestones"},
		WorkItemID: string(milestone.ID),
		Payload: map[string]any{
			"reason":          "milestone_autoclosed",
			"milestone_id":    string(milestone.ID),
			"milestone_title": milestone.Title,
			"last_child_id":   string(lastChild),
		},
	}
	r.eventsHub.Publish(events.WithTraceID(ctx, ev))
}
