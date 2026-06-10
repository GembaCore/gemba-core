// Planner CLI surfaces (gm-s47n.4.5).
//
// Two diagnostic commands operators run to inspect planner output
// before (or instead of) wiring auto-dispatch:
//
//   gemba conflicts < input.json — print the conflict graph the
//     planner would compute over the given bead set, with per-edge
//     reasons and a parallel-safe batch suggestion.
//
//   gemba affinity < input.json  — print the per-(bead, session)
//     affinity matrix with the 5 sub-score breakdown and the
//     combined weighted score.
//
// Both commands accept the same JSON input shape (PlannerInput
// below). Reading from stdin keeps the CLI scriptable: the planner
// itself or operators with a curated bead list can pipe in. A
// future bead can wire these commands directly to the WorkPlane +
// OrchestrationPlane registries; for v1 the diagnostic surface
// stays input-driven so it works without a live server.
//
// --json on either command emits a structured envelope instead of
// the human-readable text. Stable wire shape — see PlannerJSONOut.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/conflicts"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
	"github.com/GembaCore/gemba-core/internal/sourceanalysis"
)

// PlannerInput is the wire shape both commands accept on stdin.
// Fields are optional so an operator can prepare just-conflicts
// input (no SessionContexts) or just-affinity input (no targets
// on the beads). The CLI only reads what each command needs.
type PlannerInput struct {
	// Beads are the candidate WorkItems the planner is reasoning
	// about. Each entry is the planner's projection — id + tags
	// + targets + repo affinity — derived from a real WorkItem
	// upstream.
	Beads []PlannerInputBead `json:"beads"`
	// SessionContexts are the live agent sessions the planner can
	// dispatch against. Pass them along even when only running
	// conflicts: workspace-collision detection cross-references
	// these so a ready bead routed to a worktree another session
	// is already writing in is flagged.
	SessionContexts []planner.OperationalContext `json:"session_contexts,omitempty"`
}

// PlannerInputBead captures every per-bead field the two CLI
// commands need. Targets are file paths (already glob-expanded by
// the caller); BeadTarget is the routing-collision input for
// workspace conflict.
type PlannerInputBead struct {
	ID           core.WorkItemID         `json:"id"`
	Concepts     []planner.ConceptTag    `json:"concepts,omitempty"`
	Targets      []string                `json:"targets,omitempty"`
	Repositories []string                `json:"repositories,omitempty"`
	Branch       string                  `json:"branch,omitempty"`
	WorktreePath string                  `json:"worktree_path,omitempty"`
	GlobPatterns []targets.Pattern       `json:"glob_patterns,omitempty"`
	SemanticHits []sourceanalysis.Target `json:"semantic_hits,omitempty"`
}

// newConflictsCmd builds `gemba conflicts`.
func newConflictsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "conflicts",
		Short: "Print the planner's conflict graph for a bead set",
		Long: `Read a PlannerInput JSON envelope from stdin, compute the
conflict graph (target overlap + workspace collision + optionally
semantic conflict), and print the per-edge reasons plus a parallel-
safe batch suggestion.

Inputs:
  --file (or stdin)  PlannerInput envelope. Beads[].GlobPatterns
                     drives target-overlap detection (gm-s47n.4.1);
                     Beads[].WorktreePath / Repositories / Branch
                     drive workspace-collision (gm-s47n.4.6);
                     Beads[].SemanticHits + Targets seed the
                     semantic-conflict adapter.
  SessionContexts    optional live operational contexts; used by
                     workspace-collision to surface bead↔live
                     edges.

Output:
  default            human-readable list of conflict edges with
                     reasons + a parallel-safe batch suggestion.
  --json             a stable JSON envelope (see PlannerJSONOut in
                     internal/cli/planner.go) for tooling pipelines.

The semantic-conflict detector is wired against a noop
SourceAnalysis backend by default — semantic edges only appear when
the operator has built a real binding (gm-s47n.3.3 GitNexus). The
graph silently degrades to target + workspace edges; the .json
output reports skipped detectors in its 'notices' field.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := readPlannerInput(cmd)
			if err != nil {
				return err
			}
			return runConflicts(cmd, in, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read PlannerInput from this file (default: stdin)")
	return cmd
}

// newAffinityCmd builds `gemba affinity`.
func newAffinityCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "affinity",
		Short: "Print per-(bead, session) affinity scores",
		Long: `Read a PlannerInput JSON envelope from stdin and print the
affinity matrix: one row per (bead, session) pair with the five
sub-score breakdown (concept_overlap / file_familiarity /
workspace_match / recency / headroom) and the combined weighted
score.

Default weights match the spec (0.30 / 0.20 / 0.20 / 0.15 / 0.15).
Pass --weights as a JSON object to override per call:

  gemba affinity --weights '{"concept_overlap":0.5,"file_familiarity":0.3,"workspace_match":0.1,"recency":0.05,"headroom":0.05}'

The combined score is always reported with the breakdown — never
present the scalar without the per-sub-score row.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := readPlannerInput(cmd)
			if err != nil {
				return err
			}
			weights, err := parseWeights(cmd)
			if err != nil {
				return err
			}
			return runAffinity(cmd, in, weights, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read PlannerInput from this file (default: stdin)")
	cmd.Flags().String("weights", "", "JSON object overriding the default AffinityWeights")
	return cmd
}

func readPlannerInput(cmd *cobra.Command) (*PlannerInput, error) {
	path, _ := cmd.Flags().GetString("file")
	rd := cmd.InOrStdin()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		rd = f
	}
	var in PlannerInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode planner input: %w", err)
	}
	return &in, nil
}

func parseWeights(cmd *cobra.Command) (*planner.AffinityWeights, error) {
	raw, _ := cmd.Flags().GetString("weights")
	if raw == "" {
		return nil, nil
	}
	var w planner.AffinityWeights
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return nil, fmt.Errorf("parse --weights: %w", err)
	}
	return &w, nil
}

// PlannerJSONOut is the stable shape for both --json modes. The
// command sets exactly one of Conflicts or Affinity per call.
type PlannerJSONOut struct {
	Conflicts *ConflictsOut `json:"conflicts,omitempty"`
	Affinity  *AffinityOut  `json:"affinity,omitempty"`
}

type ConflictsOut struct {
	Edges               []conflicts.Edge             `json:"edges"`
	Batches             []conflicts.Batch            `json:"batches"`
	WorkspaceCollisions []planner.WorkspaceCollision `json:"workspace_collisions"`
	SemanticConflicts   []planner.SemanticConflict   `json:"semantic_conflicts"`
	Notices             []string                     `json:"notices,omitempty"`
}

type AffinityOut struct {
	Rows []AffinityRow `json:"rows"`
}

type AffinityRow struct {
	BeadID    core.WorkItemID        `json:"bead_id"`
	SessionID string                 `json:"session_id"`
	AgentID   core.AgentID           `json:"agent_id,omitempty"`
	Scores    planner.AffinityScores `json:"scores"`
}

func runConflicts(cmd *cobra.Command, in *PlannerInput, asJSON bool) error {
	out := cmd.OutOrStdout()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	notices := []string{}

	// Build the inputs the three detectors need from the same
	// PlannerInput envelope. Each conversion is local + pure.
	cBeads := make([]conflicts.Bead, 0, len(in.Beads))
	for _, b := range in.Beads {
		cBeads = append(cBeads, conflicts.Bead{ID: b.ID, Targets: b.GlobPatterns})
	}

	wsBeads := make([]planner.BeadTarget, 0, len(in.Beads))
	for _, b := range in.Beads {
		repo := ""
		if len(b.Repositories) > 0 {
			repo = b.Repositories[0]
		}
		wsBeads = append(wsBeads, planner.BeadTarget{
			BeadID:       string(b.ID),
			Repository:   repo,
			Branch:       b.Branch,
			WorktreePath: b.WorktreePath,
		})
	}

	semBeads := make([]planner.SemanticBeadInputs, 0, len(in.Beads))
	for _, b := range in.Beads {
		ts := make([]sourceanalysis.Target, 0, len(b.SemanticHits)+len(b.Targets))
		ts = append(ts, b.SemanticHits...)
		for _, p := range b.Targets {
			ts = append(ts, sourceanalysis.Target{Path: p})
		}
		semBeads = append(semBeads, planner.SemanticBeadInputs{
			BeadID:  string(b.ID),
			Targets: ts,
		})
	}

	// Compose the three detector outputs.
	wsEdges := planner.WorkspaceCollisions(wsBeads, in.SessionContexts)

	sa := sourceanalysis.NewNoop() // gm-s47n.3.3 lands the GitNexus binding; CLI uses noop today.
	semEdges, semErr := planner.SemanticConflicts(ctx, semBeads, sa)
	if semErr != nil {
		notices = append(notices, "semantic-conflict skipped: "+semErr.Error())
	}

	// Run the .4.3 composer for the canonical Graph + Batches.
	graph, err := conflicts.Conflicts(ctx, cBeads, conflicts.Options{})
	if err != nil {
		return fmt.Errorf("conflicts: %w", err)
	}
	batches := graph.Batches()

	if asJSON {
		env := PlannerJSONOut{Conflicts: &ConflictsOut{
			Edges:               graph.Edges,
			Batches:             batches,
			WorkspaceCollisions: wsEdges,
			SemanticConflicts:   semEdges,
			Notices:             notices,
		}}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	}

	// Human-readable form.
	fmt.Fprintf(out, "Conflict graph (%d beads, %d edges)\n", len(graph.Beads), len(graph.Edges))
	if len(graph.Edges) == 0 {
		fmt.Fprintln(out, "  no edges — every bead can dispatch in parallel")
	}
	for _, e := range graph.Edges {
		reasons := make([]string, 0, len(e.Reasons))
		for _, r := range e.Reasons {
			s := r.Kind.String()
			if r.Detail != "" {
				s += " (" + r.Detail + ")"
			}
			reasons = append(reasons, s)
		}
		fmt.Fprintf(out, "  %s -- %s : %s\n", e.From, e.To, strings.Join(reasons, ", "))
	}

	if len(wsEdges) > 0 {
		fmt.Fprintf(out, "\nWorkspace-collision detail (%d edges)\n", len(wsEdges))
		for _, w := range wsEdges {
			if w.LiveSessionID != "" {
				fmt.Fprintf(out, "  %s (live %s) : %s\n", w.B, w.LiveSessionID, w.Reason)
				continue
			}
			fmt.Fprintf(out, "  %s -- %s : %s\n", w.A, w.B, w.Reason)
		}
	}

	if len(semEdges) > 0 {
		fmt.Fprintf(out, "\nSemantic-conflict detail (%d edges)\n", len(semEdges))
		for _, s := range semEdges {
			fmt.Fprintf(out, "  %s -- %s : %s\n", s.A, s.B, s.Reason)
		}
	}

	for _, n := range notices {
		fmt.Fprintf(out, "\nnotice: %s\n", n)
	}

	fmt.Fprintf(out, "\nParallel-safe batches (%d)\n", len(batches))
	for i, b := range batches {
		fmt.Fprintf(out, "  batch %d: %s\n", i+1, joinIDs(b.Beads))
	}
	return nil
}

func runAffinity(cmd *cobra.Command, in *PlannerInput, weights *planner.AffinityWeights, asJSON bool) error {
	out := cmd.OutOrStdout()

	rows := make([]AffinityRow, 0, len(in.Beads)*len(in.SessionContexts))
	for _, b := range in.Beads {
		bead := planner.AffinityBeadInputs{
			BeadID:       string(b.ID),
			Concepts:     b.Concepts,
			Targets:      b.Targets,
			Repositories: b.Repositories,
			Branch:       b.Branch,
		}
		for _, ctx := range in.SessionContexts {
			scores := planner.Affinity(bead, ctx, weights)
			row := AffinityRow{
				BeadID: b.ID,
				Scores: scores,
			}
			if ctx.Session != nil {
				row.SessionID = ctx.Session.ID
			}
			if ctx.Agent != nil {
				row.AgentID = ctx.Agent.ID
			}
			rows = append(rows, row)
		}
	}

	// Stable sort: by bead id then by combined score desc so the
	// most-primed session for each bead floats to the top.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].BeadID != rows[j].BeadID {
			return rows[i].BeadID < rows[j].BeadID
		}
		return rows[i].Scores.Combined > rows[j].Scores.Combined
	})

	if asJSON {
		env := PlannerJSONOut{Affinity: &AffinityOut{Rows: rows}}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	}

	fmt.Fprintln(out, "bead              session         agent              concept  files  ws    recency headroom combined")
	for _, r := range rows {
		fmt.Fprintf(out, "%-18s %-15s %-18s %5.2f   %5.2f  %4.2f   %5.2f    %5.2f    %5.2f\n",
			r.BeadID, r.SessionID, r.AgentID,
			r.Scores.ConceptOverlap, r.Scores.FileFamiliarity,
			r.Scores.WorkspaceMatch, r.Scores.Recency, r.Scores.Headroom,
			r.Scores.Combined)
	}
	return nil
}

func joinIDs(ids []core.WorkItemID) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return strings.Join(out, ", ")
}
