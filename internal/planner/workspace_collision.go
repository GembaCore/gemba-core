// Workspace-collision detection (gm-s47n.4.6). One of the three
// inputs the .4.3 Conflicts() composer pulls together — sibling to
// the file-overlap algorithm in internal/planner/targets and the
// semantic-conflict detector in gm-s47n.4.2.
//
// The contract is narrow: given a set of candidate beads (each
// resolved to a routing target) and the currently-live operational
// contexts, return every workspace-collision edge. Two routing
// destinations collide when ANY of:
//
//   1. They share the same (repo, branch) pair. A worktree is one
//      working copy per branch; two writers on the same branch
//      serialize at the filesystem level regardless of file overlap.
//   2. They share the same canonicalised WorktreePath.
//
// Live cross-references emit bead↔live edges so a candidate bead
// routed to a worktree another session is already writing in gets
// flagged even when no other ready bead in the set conflicts on
// files. The .4.3 composer uses the LiveSessionID field to render
// "blocked by live session" reasons in the coach UI without doing
// its own session lookup.
//
// Pure function. Deterministic order (sorted by A then B then
// LiveSessionID). Spec: docs/design/work-planning.md §5.

package planner

import (
	"path/filepath"
	"sort"

	"github.com/MikeBengtson/gemba/core"
)

// BeadTarget is the planner-derived operational location for a
// ready bead — what (repo, branch, worktree_path) the bead would
// land in if dispatched. Callers derive it from the bead's
// repository / branch fields plus rig conventions; this package
// stays agnostic about the derivation so adaptors that route
// differently (one worktree per bead vs one per branch) can both
// feed in.
//
// WorktreePath is optional — leave empty when dispatch hasn't
// materialised a worktree yet. The collision check still fires on
// (repo, branch) alone in that case.
type BeadTarget struct {
	BeadID       string
	Repository   string
	Branch       string
	WorktreePath string
}

// WorkspaceCollision is a single edge in the workspace-conflict
// graph. A is always a bead id; B is either a bead id (bead↔bead
// collision) or a sentinel string when the right-hand side is a
// live session — in that case LiveSessionID names the session.
type WorkspaceCollision struct {
	// A and B are bead ids. For bead↔live edges, B is the bead
	// being blocked by the live session and A is left empty —
	// callers branch on LiveSessionID being non-empty rather than
	// on string sentinels.
	A string `json:"a,omitempty"`
	B string `json:"b"`

	// Reason is a one-line human explanation: "same repo+branch",
	// "same worktree_path", "live session in branch X", etc. The
	// .4.3 composer surfaces this verbatim in the coach UI; never
	// parse it back.
	Reason string `json:"reason"`

	// LiveSessionID is non-empty when this edge is a bead↔live
	// collision. The corresponding live OperationalContext can be
	// looked up via the planner's session resolver.
	LiveSessionID string `json:"live_session_id,omitempty"`
}

// WorkspaceCollisions returns every workspace-conflict edge induced
// by the bead set plus the live operational contexts. Output is
// sorted by (LiveSessionID, A, B) so equal inputs always produce
// equal outputs — important for change-detection up the stack.
//
// Two beads (or a bead + live ctx) collide when ANY of the
// following conditions hold:
//
//   - Same Repository AND same Branch (both non-empty on each side).
//     Empty Repository or Branch means "the planner couldn't infer
//     the routing target" — those entries are silently skipped
//     rather than treated as wildcard matches, so an under-specified
//     bead doesn't collide with everything.
//
//   - Same canonicalised WorktreePath (both non-empty). Comparison
//     uses filepath.Clean so /a/b and /a/b/ never miss; symlinks are
//     NOT resolved here — adaptors that need symlink-aware comparison
//     pre-canonicalise their inputs.
//
// Both relations are checked; either one alone is enough to emit
// an edge. Reason names which one fired (when both, "same repo+branch
// + same worktree_path").
//
// `live` carries the currently-active sessions. The function only
// reads .Session.ID + .Workspace; passing a partial OperationalContext
// (e.g. with nil Profile or Health) is fine.
func WorkspaceCollisions(beads []BeadTarget, live []OperationalContext) []WorkspaceCollision {
	out := make([]WorkspaceCollision, 0)

	for i := 0; i < len(beads); i++ {
		for j := i + 1; j < len(beads); j++ {
			if reason := collisionReason(beads[i], beads[j]); reason != "" {
				out = append(out, WorkspaceCollision{
					A:      beads[i].BeadID,
					B:      beads[j].BeadID,
					Reason: reason,
				})
			}
		}
	}

	for _, ctx := range live {
		// Skip entries without enough context to compare. A live
		// OperationalContext with nil Workspace just means the
		// session is still booting — no collision is computable.
		if ctx.Workspace == nil || ctx.Session == nil {
			continue
		}
		liveBead := BeadTarget{
			Repository:   ctx.Workspace.Repository,
			Branch:       ctx.Workspace.Branch,
			WorktreePath: core.WorkspaceWorktreePath(*ctx.Workspace),
		}
		for _, b := range beads {
			if reason := collisionReason(b, liveBead); reason != "" {
				out = append(out, WorkspaceCollision{
					B:             b.BeadID,
					Reason:        "live session: " + reason,
					LiveSessionID: ctx.Session.ID,
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].LiveSessionID != out[j].LiveSessionID {
			return out[i].LiveSessionID < out[j].LiveSessionID
		}
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	return out
}

// collisionReason returns the human-readable reason a pair
// collides, or "" when no relation fires. Centralising the rule
// here keeps bead↔bead and bead↔live paths reading the same way.
func collisionReason(x, y BeadTarget) string {
	repoBranch := x.Repository != "" && y.Repository != "" &&
		x.Branch != "" && y.Branch != "" &&
		x.Repository == y.Repository && x.Branch == y.Branch
	worktree := x.WorktreePath != "" && y.WorktreePath != "" &&
		filepath.Clean(x.WorktreePath) == filepath.Clean(y.WorktreePath)
	switch {
	case repoBranch && worktree:
		return "same repo+branch + same worktree_path"
	case repoBranch:
		return "same repo+branch"
	case worktree:
		return "same worktree_path"
	default:
		return ""
	}
}
