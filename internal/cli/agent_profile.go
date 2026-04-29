// gm-v5z2.2 — `gemba agent profile` CLI.
//
// Read-only diagnostic surface for the persistent agent profile
// (work-planning.md §4 Layer 1.2). Operators run it to inspect
// what the planner thinks an agent is good at — useful when
// debugging why a particular bead got routed where, or before a
// `gt handoff` to confirm the new session will inherit a useful
// warm starting point.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/agentprofile"
	"github.com/GembaCore/gemba-core/internal/planner"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Read agent-level planner state",
		Long: `Subcommands inspect agent-level planner state. Today the only
surface is 'profile' (gm-v5z2.2 persistent agent profile); the
operator-facing knobs land alongside future signals (workspace
assignment, owner-claim) in their own subcommands.`,
	}
	cmd.AddCommand(newAgentProfileCmd())
	return cmd
}

func newAgentProfileCmd() *cobra.Command {
	var (
		asJSON bool
		topN   int
		all    bool
	)
	cmd := &cobra.Command{
		Use:   "profile [agent-id]",
		Short: "Print the persistent profile for an agent (concepts + files + decay)",
		Long: `Print the persistent profile for an agent — the recency-decayed
concept and file weights the planner uses to seed a fresh session
post-handoff (work-planning.md §4 Layer 1.2).

The single-agent form requires <agent-id>; pass --all to dump the
full registry. Default output is the top-N concepts + top-N files
plus the lifetime bead count and last-activity timestamp; --json
emits the stable wire envelope.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, db, err := openAgentProfileStore(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if all {
				profiles, err := store.List(ctx)
				if err != nil {
					return err
				}
				return renderAgentProfiles(cmd.OutOrStdout(), profiles, topN, asJSON)
			}
			if len(args) != 1 {
				return fmt.Errorf("requires <agent-id> (or pass --all to dump every profile)")
			}
			profile, err := store.Get(ctx, core.AgentID(args[0]))
			if err != nil {
				return err
			}
			if profile == nil {
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"profile": nil})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "no profile recorded for agent %s\n", args[0])
				return nil
			}
			return renderAgentProfiles(cmd.OutOrStdout(), []agentprofile.AgentProfile{*profile}, topN, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON")
	cmd.Flags().IntVar(&topN, "top", 10, "show at most N entries per concept / file list (text mode)")
	cmd.Flags().BoolVar(&all, "all", false, "dump every recorded agent profile")
	cmd.Flags().String("dolt-url", "",
		"Dolt URL (e.g. mysql://root@127.0.0.1:3307/gemba) — required")
	return cmd
}

func openAgentProfileStore(cmd *cobra.Command) (*agentprofile.Store, *sql.DB, error) {
	url, _ := cmd.Flags().GetString("dolt-url")
	if url == "" {
		// Allow the persistent flag to land via parents, mirroring
		// session_focus.go's resolver.
		for c := cmd.Parent(); c != nil; c = c.Parent() {
			if v, _ := c.Flags().GetString("dolt-url"); v != "" {
				url = v
				break
			}
		}
	}
	if url == "" {
		return nil, nil, fmt.Errorf("--dolt-url is required")
	}
	dsn, err := doltDSN(url)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, nil, err
	}
	return agentprofile.NewStore(db), db, nil
}

func renderAgentProfiles(out interface{ Write(p []byte) (int, error) }, profiles []agentprofile.AgentProfile, topN int, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"profiles": profiles})
	}
	if len(profiles) == 0 {
		fmt.Fprintln(out, "no agent profiles recorded")
		return nil
	}
	for i, p := range profiles {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "agent: %s\n", p.AgentID)
		fmt.Fprintf(out, "  lifetime beads:    %d\n", p.LifetimeBeadCount)
		fmt.Fprintf(out, "  last activity:     %s\n", formatProfileTime(p.LastActivityAt))
		fmt.Fprintf(out, "  decay half-life:   %.0f days\n", agentprofile.DefaultDecayHalfLifeDays)
		if len(p.Concepts) > 0 {
			fmt.Fprintln(out, "  top concepts:")
			for _, e := range topConceptsRanked(p.Concepts, topN) {
				fmt.Fprintf(out, "    %-24s %.3f\n", e.tag, e.weight)
			}
		}
		if len(p.Files) > 0 {
			fmt.Fprintln(out, "  top files:")
			for _, e := range topFilesRanked(p.Files, topN) {
				fmt.Fprintf(out, "    %-32s %.3f\n", e.path, e.weight)
			}
		}
	}
	return nil
}

func formatProfileTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

type weightedConcept struct {
	tag    planner.ConceptTag
	weight float64
}

type weightedFile struct {
	path   string
	weight float64
}

func topConceptsRanked(in map[planner.ConceptTag]float64, n int) []weightedConcept {
	out := make([]weightedConcept, 0, len(in))
	for k, v := range in {
		out = append(out, weightedConcept{tag: k, weight: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].weight != out[j].weight {
			return out[i].weight > out[j].weight
		}
		return out[i].tag < out[j].tag
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func topFilesRanked(in map[string]float64, n int) []weightedFile {
	out := make([]weightedFile, 0, len(in))
	for k, v := range in {
		out = append(out, weightedFile{path: k, weight: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].weight != out[j].weight {
			return out[i].weight > out[j].weight
		}
		return out[i].path < out[j].path
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
