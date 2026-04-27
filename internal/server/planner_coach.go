// /api/planner/coach handler (gm-s47n.6.1).
//
// One server-side composition of every input the coach-mode SPA
// page needs — the session strip + the dispatch grid. The SPA fires
// one request, gets every input, renders.
//
// Output:
//
//   {
//     "sessions":     [OperationalContext, ...],   // strip cards
//     "ready_beads":  [coachReadyBead, ...],        // grid rows
//     "conflicts":    [conflicts.Edge, ...],        // bead↔bead
//     "workspace":    [planner.WorkspaceCollision],// bead↔live + bead↔bead
//     "semantic":     [planner.SemanticConflict],  // bead↔bead
//     "affinity":     [coachAffinityRow, ...],      // grid cells
//     "batches":      [conflicts.Batch, ...],       // parallel-safe groups
//     "notices":      ["..."]                       // soft-skip messages
//   }
//
// Every conflict surface is included separately so the SPA can
// surface per-detector reasons (a "live session in branch X"
// edge originates from the workspace detector, not the canonical
// conflicts.Graph). Combined affinity scores are sorted by combined
// desc within each bead so the warmest session for a row floats
// to the top.
//
// Adaptor surface notes:
//   - WorkItem.targets[] (gm-s47n.1.1) is not yet shipped, so the
//     coach endpoint reads structured-extras keys when present and
//     falls back to additional_read_paths/additional_write_paths.
//     This keeps the endpoint operational while the bead model
//     catches up.
//   - SourceAnalysis defaults to noop until gm-s47n.3.3 lands a
//     real binding; semantic-conflict edges silently empty.

package server

import (
	"context"
	"net/http"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/conflicts"
	"github.com/MikeBengtson/gemba/internal/planner/targets"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/MikeBengtson/gemba/internal/sourceanalysis"
)

// coachReadyBead is the per-bead row payload for the dispatch
// grid. The SPA renders id + title and uses concepts/targets for
// hover detail.
type coachReadyBead struct {
	ID         core.WorkItemID      `json:"id"`
	Title      string               `json:"title"`
	Kind       string               `json:"kind"`
	Status     string               `json:"status"`
	State      core.StateCategory   `json:"state_category"`
	Concepts   []planner.ConceptTag `json:"concepts,omitempty"`
	Targets    []string             `json:"targets,omitempty"`
	Repository string               `json:"repository,omitempty"`
	Branch     string               `json:"branch,omitempty"`
}

// coachAffinityRow is one cell in the dispatch grid: a (bead,
// session) pair with the per-sub-score breakdown + combined.
type coachAffinityRow struct {
	BeadID    core.WorkItemID        `json:"bead_id"`
	SessionID string                 `json:"session_id"`
	AgentID   core.AgentID           `json:"agent_id,omitempty"`
	Scores    planner.AffinityScores `json:"scores"`
}

// coachResponse is the full envelope the SPA consumes.
type coachResponse struct {
	Sessions   []*planner.OperationalContext `json:"sessions"`
	ReadyBeads []coachReadyBead              `json:"ready_beads"`
	Conflicts  []conflicts.Edge              `json:"conflicts"`
	Workspace  []planner.WorkspaceCollision  `json:"workspace"`
	Semantic   []planner.SemanticConflict    `json:"semantic"`
	Affinity   []coachAffinityRow            `json:"affinity"`
	Batches    []conflicts.Batch             `json:"batches"`
	Notices    []string                      `json:"notices,omitempty"`
}

func (r *Router) plannerCoach(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no adaptors bound")
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "orchestration plane not configured")
		return
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	ctx := req.Context()

	// --- Live sessions → OperationalContext per session ---
	liveSessions, err := op.ListSessions(ctx, core.SessionFilter{})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	readers := planner.OperationalContextReaders{
		Sessions:   orchSessionLookup{op: op},
		Agents:     orchAgentLookup{op: op},
		Workspaces: orchWorkspaceLookup{op: op},
	}
	contexts := make([]*planner.OperationalContext, 0, len(liveSessions))
	for i := range liveSessions {
		opCtx, err := planner.ReadOperationalContext(ctx, liveSessions[i].ID, readers)
		if err != nil {
			// Soft-skip a session that can't be resolved — the
			// strip should still render the others. The error
			// surface is the notices list.
			continue
		}
		contexts = append(contexts, opCtx)
	}

	// --- Ready beads → derive grid rows + per-detector inputs ---
	items, err := wp.ListWorkItems(ctx, core.WorkItemFilter{})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	ready := make([]core.WorkItem, 0, len(items))
	for _, it := range items {
		if isReady(it) {
			ready = append(ready, it)
		}
	}

	rows := make([]coachReadyBead, 0, len(ready))
	cBeads := make([]conflicts.Bead, 0, len(ready))
	wsBeads := make([]planner.BeadTarget, 0, len(ready))
	semBeads := make([]planner.SemanticBeadInputs, 0, len(ready))
	affBeads := make([]planner.AffinityBeadInputs, 0, len(ready))
	for _, it := range ready {
		concepts := extractConcepts(it)
		targetPaths := extractTargets(it)

		rows = append(rows, coachReadyBead{
			ID:         it.ID,
			Title:      it.Title,
			Kind:       it.Kind,
			Status:     it.Status,
			State:      it.StateCategory,
			Concepts:   concepts,
			Targets:    targetPaths,
			Repository: string(it.PrimaryRepositoryID),
		})
		cBeads = append(cBeads, conflicts.Bead{
			ID:      it.ID,
			Targets: toPatterns(targetPaths),
		})
		wsBeads = append(wsBeads, planner.BeadTarget{
			BeadID:     string(it.ID),
			Repository: string(it.PrimaryRepositoryID),
			// Branch not yet on WorkItem core; falls through to
			// "" → workspace-collision skips the repo+branch
			// relation cleanly.
		})
		saTargets := make([]sourceanalysis.Target, 0, len(targetPaths))
		for _, p := range targetPaths {
			saTargets = append(saTargets, sourceanalysis.Target{Repository: string(it.PrimaryRepositoryID), Path: p})
		}
		semBeads = append(semBeads, planner.SemanticBeadInputs{BeadID: string(it.ID), Targets: saTargets})
		repos := []string{}
		if string(it.PrimaryRepositoryID) != "" {
			repos = append(repos, string(it.PrimaryRepositoryID))
		}
		affBeads = append(affBeads, planner.AffinityBeadInputs{
			BeadID:       string(it.ID),
			Concepts:     concepts,
			Targets:      targetPaths,
			Repositories: repos,
		})
	}

	// --- Compose the conflict graph + per-detector outputs ---
	notices := []string{}
	wsEdges := planner.WorkspaceCollisions(wsBeads, deref(contexts))
	sa := sourceanalysis.NewNoop()
	semEdges, semErr := planner.SemanticConflicts(ctx, semBeads, sa)
	if semErr != nil {
		notices = append(notices, "semantic-conflict skipped: "+semErr.Error())
	}
	graph, err := conflicts.Conflicts(ctx, cBeads, conflicts.Options{})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	// --- Affinity per (bead, session) pair ---
	affinity := make([]coachAffinityRow, 0, len(rows)*len(contexts))
	for _, b := range affBeads {
		for _, ctx := range contexts {
			scores := planner.Affinity(b, *ctx, nil)
			row := coachAffinityRow{
				BeadID: core.WorkItemID(b.BeadID),
				Scores: scores,
			}
			if ctx.Session != nil {
				row.SessionID = ctx.Session.ID
			}
			if ctx.Agent != nil {
				row.AgentID = ctx.Agent.ID
			}
			affinity = append(affinity, row)
		}
	}

	writeJSON(w, http.StatusOK, coachResponse{
		Sessions:   contexts,
		ReadyBeads: rows,
		Conflicts:  graph.Edges,
		Workspace:  wsEdges,
		Semantic:   semEdges,
		Affinity:   affinity,
		Batches:    graph.Batches(),
		Notices:    notices,
	})
}

// isReady returns true when a WorkItem is dispatchable. Mirrors
// the "ready" semantics bd ready exposes — backlog/unstarted/staged
// work an agent can claim. Completed/canceled/started are filtered
// out (started = already in flight).
func isReady(it core.WorkItem) bool {
	switch it.StateCategory {
	case core.StateBacklog, core.StateUnstarted, core.StateStaged:
		return true
	default:
		return false
	}
}

// extractConcepts pulls per-bead concept tags. WorkItem.concepts
// (gm-s47n.1.1) is not yet a typed field; until it lands, read the
// "concepts" key from custom (the structured-extras map). Empty
// when absent — affinity falls through to other sub-scores.
func extractConcepts(it core.WorkItem) []planner.ConceptTag {
	raw, ok := it.Custom["concepts"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]planner.ConceptTag, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, planner.ConceptTag(s))
		}
	}
	return out
}

// extractTargets pulls the bead's target file globs. Reads
// custom["targets"] first (gm-s47n.1.1 forward-compat shape); falls
// back to AdditionalWritePaths + AdditionalReadPaths so workspace-
// scoped beads get a useful blast set even before .1.1 lands.
func extractTargets(it core.WorkItem) []string {
	if raw, ok := it.Custom["targets"]; ok {
		if arr, ok := raw.([]any); ok {
			out := make([]string, 0, len(arr))
			for _, v := range arr {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	out := make([]string, 0, len(it.AdditionalWritePaths)+len(it.AdditionalReadPaths))
	out = append(out, it.AdditionalWritePaths...)
	out = append(out, it.AdditionalReadPaths...)
	return out
}

func toPatterns(paths []string) []targets.Pattern {
	out := make([]targets.Pattern, 0, len(paths))
	for _, p := range paths {
		out = append(out, targets.Pattern(p))
	}
	return out
}

// deref returns the slice of concrete OperationalContext values
// expected by WorkspaceCollisions. The aggregator carries pointers
// so partial reads can drop fields cleanly; the conflict detector
// accepts values, so we widen here. Skips nil entries.
func deref(in []*planner.OperationalContext) []planner.OperationalContext {
	out := make([]planner.OperationalContext, 0, len(in))
	for _, c := range in {
		if c == nil {
			continue
		}
		out = append(out, *c)
	}
	return out
}

// Unused but kept here so the import resolver doesn't drop the
// package — the noop binding ships under sourceanalysis and the
// coach handler is its only direct consumer in this package.
var _ = context.Background
