// Milestone auto-close (gm-mqiz).
//
// When a child of a milestone transitions to a closed StateCategory
// (completed/canceled) AND the milestone has no other open children
// left, the milestone auto-closes and an escalation.opened event
// fires through the events hub. This is the spec's locked behaviour
// (gm-b11z) — milestone state is its own bead's status, but
// "every child finished" is a deterministic enough signal that we
// flip it for the operator and notify rather than let a stale-open
// milestone clutter the surface.
//
// Best-effort: any failure is logged via slog and swallowed. The
// operator's original patch is what matters; auto-close is a
// courtesy on top.

package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/events"
)

// openStateCategories is the closure of "open" buckets — anything
// not in here is treated as closed for the purpose of the rollup.
// Mirrors the spec: backlog/unstarted/staged/started.
var openStateCategories = map[core.StateCategory]struct{}{
	core.StateBacklog:   {},
	core.StateUnstarted: {},
	core.StateStaged:    {},
	core.StateStarted:   {},
}

// isClosedCategory reports whether s belongs to the closed set
// (completed/canceled). Used to gate both "did this patch close the
// child?" and "is the milestone already closed (skip)?"
func isClosedCategory(s core.StateCategory) bool {
	_, open := openStateCategories[s]
	return !open && s != ""
}

// maybeAutoCloseMilestone is invoked after a successful UpdateWorkItem.
// `child` is the patched item as the adaptor returned it. If the item
// just landed in a closed state and is a parent_child child of an
// otherwise-empty (no open children) milestone, the milestone is
// patched closed and a notification fires.
//
// All errors are logged and swallowed — the caller's PATCH 200 ships
// regardless.
func (r *Router) maybeAutoCloseMilestone(ctx context.Context, wp core.WorkPlane, child core.WorkItem) {
	if wp == nil {
		return
	}
	if !isClosedCategory(child.StateCategory) {
		return
	}
	parentID := parentMilestoneID(child)
	if parentID == "" {
		return
	}
	// Re-read the parent to learn (a) whether it's actually a
	// milestone and (b) whether it's already closed (idempotent
	// skip — no duplicate notifications).
	parent, err := wp.GetWorkItem(ctx, parentID)
	if err != nil {
		slog.Warn("milestone autoclose: parent fetch failed",
			"child_id", child.ID, "parent_id", parentID, "err", err)
		return
	}
	if parent.Kind != core.KindMilestone {
		return
	}
	if isClosedCategory(parent.StateCategory) {
		return
	}
	// Count open children other than the just-closed `child`. The
	// milestone's Relationships carry parent_child edges with
	// From=milestone, To=child for every child the adaptor knows
	// about. If even one is still open, leave the milestone alone.
	hasOpen, err := milestoneHasOpenChildrenExcept(ctx, wp, parent, child.ID)
	if err != nil {
		slog.Warn("milestone autoclose: child enumeration failed",
			"milestone_id", parent.ID, "err", err)
		return
	}
	if hasOpen {
		return
	}
	// Mirror the just-closed child's terminal category onto the
	// milestone. Default to completed; only propagate canceled when
	// every closed child was canceled — an over-engineering trap.
	// Plain rule: completed wins, because "the milestone is done"
	// is the intent the operator most often wants surfaced.
	closed := core.StateCompleted
	patch := core.WorkItemPatch{StateCategory: &closed}
	updated, err := wp.UpdateWorkItem(ctx, parent.ID, patch)
	if err != nil {
		slog.Warn("milestone autoclose: patch failed",
			"milestone_id", parent.ID, "err", err)
		return
	}
	r.publishMilestoneClosedEscalation(ctx, updated, child.ID)
}

// parentMilestoneID returns the From side of a parent_child edge that
// points at `child` (To=child.ID). Empty when no parent edge is
// present. We don't pre-filter on parent kind here — the caller
// re-reads the parent and gates on KindMilestone so a misrouted
// edge between two non-milestone kinds doesn't accidentally trigger
// the rollup.
func parentMilestoneID(child core.WorkItem) core.WorkItemID {
	for _, rel := range child.Relationships {
		if rel.Kind == core.RelParentChild && rel.To == child.ID && rel.From != "" {
			return rel.From
		}
	}
	return ""
}

// milestoneHasOpenChildrenExcept reports whether `milestone` has any
// child (via parent_child edges with From=milestone) currently in an
// open state, excluding the work item `excluded` (which the caller
// just observed as closed but may not yet have round-tripped through
// the milestone's edge view in some adaptors).
func milestoneHasOpenChildrenExcept(ctx context.Context, wp core.WorkPlane, milestone core.WorkItem, excluded core.WorkItemID) (bool, error) {
	for _, rel := range milestone.Relationships {
		if rel.Kind != core.RelParentChild || rel.From != milestone.ID || rel.To == "" {
			continue
		}
		if rel.To == excluded {
			continue
		}
		ch, err := wp.GetWorkItem(ctx, rel.To)
		if err != nil {
			// Treat a missing child as "still open" — the safe
			// failure mode is to NOT auto-close (operator can
			// always close manually); silently closing on a
			// transient adaptor error would be the worse drift.
			return true, err
		}
		if !isClosedCategory(ch.StateCategory) {
			return true, nil
		}
	}
	return false, nil
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
