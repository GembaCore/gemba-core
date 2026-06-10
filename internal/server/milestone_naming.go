// Milestone auto-numbering (gm-lw6h).
//
// Milestones are named with a `M<n>` prefix where <n> is unpadded
// ascending digits (M1, M2, ..., M10, ...). Numbering is per-project
// (one WorkPlane = one project, see core/types.go WorkItemID).
// An optional operator suffix follows the prefix separated by a single
// space, e.g. "M1 Beta release".
//
// This file holds the shared logic; both the HTTP create handler and
// the newproject ratify path call it so the prefix is applied
// consistently regardless of how a milestone enters the system.

package server

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// milestonePrefixRe matches an existing M<n> prefix at the start of a
// title. Group 1 is the number. Tolerates leading whitespace.
var milestonePrefixRe = regexp.MustCompile(`^\s*M(\d+)\b`)

// extractMilestoneNumber returns the number from a milestone title's
// `M<n>` prefix, or 0 if the title doesn't carry one. Used to compute
// the next free number across existing milestones.
func extractMilestoneNumber(title string) int {
	m := milestonePrefixRe.FindStringSubmatch(title)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// nextMilestoneNumber returns the next M-number to assign given the set
// of existing milestone titles. Always >= 1; gaps are not reused —
// numbering is monotonic so historical references stay stable even
// after a milestone is closed and removed from active surfaces.
func nextMilestoneNumber(titles []string) int {
	max := 0
	for _, t := range titles {
		if n := extractMilestoneNumber(t); n > max {
			max = n
		}
	}
	return max + 1
}

// applyMilestonePrefix returns title with a `M<n>` prefix applied. If
// the title already carries any M<digits> prefix, it is left unchanged
// (the operator-supplied number wins; useful for migrations and
// tests). An empty/whitespace title produces just `M<n>`.
func applyMilestonePrefix(title string, n int) string {
	trimmed := strings.TrimSpace(title)
	if milestonePrefixRe.MatchString(trimmed) {
		return trimmed
	}
	prefix := "M" + strconv.Itoa(n)
	if trimmed == "" {
		return prefix
	}
	return prefix + " " + trimmed
}

// existingMilestoneTitles lists the titles of every milestone the
// WorkPlane currently knows about. Used to seed nextMilestoneNumber.
// Returns an empty slice on adaptor error so the caller falls back to
// numbering from M1 — the alternative (failing the create) feels
// worse than potentially renumbering a milestone the operator can fix
// by editing the title.
func existingMilestoneTitles(ctx context.Context, wp core.WorkPlane) []string {
	if wp == nil {
		return nil
	}
	items, err := wp.ListWorkItems(ctx, core.WorkItemFilter{
		Kinds: []string{core.KindMilestone},
		Limit: defaultListLimit,
	})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}
