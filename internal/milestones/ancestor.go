// Milestone ancestor walking (gm-yyo8).
//
// Beads inherit milestone membership transitively through their
// parent_child chain (epic→milestone, story→epic→milestone, …). The
// preamble injects milestone context only when such an ancestor exists,
// so we need a reusable walker that finds it without dragging the rest
// of the work-graph into the request path. See gm-b11z for the design.
//
// This package deliberately lives outside internal/adapter/native/preamble
// so the renderer can stay WorkPlane-free (its package contract). The
// caller (start.go) resolves the milestone here and threads the result
// through preamble.Sources.

package milestones

import (
	"context"

	"github.com/GembaCore/gemba-core/core"
)

// maxAncestorDepth bounds the parent walk so a malformed graph (cycle
// surviving validation, deep tag chain) can't wedge a session start.
// 32 is well past anything legitimate — milestones sit at most one
// level above epics, which sit one level above stories.
const maxAncestorDepth = 32

// FindMilestoneAncestor walks parent_child edges upward from item and
// returns the first ancestor whose Kind is KindMilestone. Returns
// (nil, nil) when no milestone is reachable — that's the common case
// for unbound beads and is not an error.
//
// WorkPlane errors at any step short-circuit to (nil, err); the caller
// in start.go ignores the error and ships an unbound preamble rather
// than fail the session start.
func FindMilestoneAncestor(ctx context.Context, wp core.WorkPlane, item core.WorkItem) (*core.WorkItem, error) {
	if wp == nil {
		return nil, nil
	}
	if item.Kind == core.KindMilestone {
		// Self is the milestone — nothing to walk. Returning self
		// keeps the helper symmetric for callers that reach it via a
		// milestone-shaped surface (planner, evaluators).
		out := item
		return &out, nil
	}
	// Cycle guard: a cycle in parent_child shouldn't exist (validation
	// rejects them), but we'd rather degrade to "no ancestor" than spin.
	seen := map[core.WorkItemID]struct{}{item.ID: {}}
	current := item
	for depth := 0; depth < maxAncestorDepth; depth++ {
		parentID := parentOf(current)
		if parentID == "" {
			return nil, nil
		}
		if _, dup := seen[parentID]; dup {
			return nil, nil
		}
		seen[parentID] = struct{}{}
		parent, err := wp.GetWorkItem(ctx, parentID)
		if err != nil {
			return nil, err
		}
		if parent.Kind == core.KindMilestone {
			return &parent, nil
		}
		current = parent
	}
	return nil, nil
}

// parentOf picks the parent_child edge whose To matches the item itself
// (the convention adaptors use for inbound edges — see
// internal/adapter/bd/edges.go and web/src/components/board/epicHierarchy.ts).
// Returns "" when the item has no recognised parent edge. First match
// wins; multi-parent isn't a supported shape in core.
func parentOf(item core.WorkItem) core.WorkItemID {
	for _, r := range item.Relationships {
		if r.Kind == core.RelParentChild && r.To == item.ID && r.From != "" {
			return r.From
		}
	}
	return ""
}
