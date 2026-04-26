package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/concepts"
)

// newConceptsCmd builds the `gemba concepts` subcommand tree.
// Subcommands map onto the four gm-s47n.7.x beads:
//
//   bootstrap → .7.1 (initial vocabulary)
//   drift     → .7.2 (detector)
//   review / approve / reject → .7.3 (operator queue)
//   apply driven by approve   → .7.4 (historical rewrite)
//
// All subcommands use the workspace root's .gemba/concepts/ store
// (see internal/concepts/store.go for the layout).
func newConceptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "concepts",
		Short: "Manage the controlled vocabulary used by the planner",
		Long: `Concept vocabulary governance (gm-s47n.7).

The concept axis powers planner affinity scoring. This command tree
lets an operator bootstrap a starter vocabulary, surface drift
suggestions from current bead usage, and approve / reject the
suggested merges, renames, and deletes. See
docs/design/work-planning.md §6 for the full design.`,
	}
	cmd.AddCommand(
		newConceptsBootstrapCmd(),
		newConceptsListCmd(),
		newConceptsDriftCmd(),
		newConceptsReviewCmd(),
		newConceptsApproveCmd(),
		newConceptsRejectCmd(),
		newConceptsLogCmd(),
	)
	return cmd
}

func newConceptsBootstrapCmd() *cobra.Command {
	var max int
	var dryRun bool
	var workspace string
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Seed the vocabulary from observable workspace structure (gm-s47n.7.1)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			v, res, err := concepts.Bootstrap(cmd.Context(), root,
				concepts.DefaultSources(),
				concepts.BootstrapOpts{Max: max})
			if err != nil {
				return err
			}
			if !dryRun {
				if err := concepts.SaveVocabulary(root, v); err != nil {
					return err
				}
			}
			printBootstrapReport(cmd.OutOrStdout(), v, res, dryRun)
			return nil
		},
	}
	cmd.Flags().IntVar(&max, "max", 60, "maximum number of terms to bootstrap")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the would-be vocabulary without writing")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root (default: current dir)")
	return cmd
}

func newConceptsListCmd() *cobra.Command {
	var workspace string
	var includeRetired bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List vocabulary terms",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			v, err := concepts.LoadVocabulary(root)
			if err != nil {
				return err
			}
			terms := v.Active()
			if includeRetired {
				terms = v.Terms
			}
			out := cmd.OutOrStdout()
			if len(terms) == 0 {
				fmt.Fprintln(out, "(no terms — run `gemba concepts bootstrap`)")
				return nil
			}
			for _, t := range terms {
				suffix := ""
				if t.Retired {
					suffix = " [RETIRED]"
				}
				if len(t.Aliases) > 0 {
					suffix += " (aliases: " + strings.Join(t.Aliases, ", ") + ")"
				}
				fmt.Fprintf(out, "%s\t%s%s\n", t.Name, t.Source, suffix)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root (default: current dir)")
	cmd.Flags().BoolVar(&includeRetired, "all", false, "include retired terms")
	return cmd
}

func newConceptsDriftCmd() *cobra.Command {
	var workspace string
	var apply bool
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Run the drift detector and add suggestions to the review queue (gm-s47n.7.2)",
		Long: `Reads bead concept usage through the wired BeadConceptStore (none
in core gemba today; this command no-ops cleanly until gm-s47n.1.1
lands the production wiring) and emits suggestions to the review
queue. Pure / idempotent — running twice on the same input doesn't
duplicate suggestions.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			store := loadBeadStore(root)
			if store == nil {
				fmt.Fprintln(cmd.OutOrStdout(),
					"no BeadConceptStore wired (gm-s47n.1.1 follow-up); drift detector is a no-op")
				return nil
			}
			beads, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			d := concepts.DetectDrift(beads, concepts.DefaultDriftOpts())
			list, err := concepts.LoadSuggestions(root)
			if err != nil {
				return err
			}
			fresh := concepts.SuggestionsFromDrift(d, list.Suggestions)
			for _, s := range fresh {
				list.Add(s)
			}
			if apply {
				if err := concepts.SaveSuggestions(root, list); err != nil {
					return err
				}
			}
			printDriftReport(cmd.OutOrStdout(), d, fresh, apply)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().BoolVar(&apply, "apply", false, "persist new suggestions to the review queue")
	return cmd
}

func newConceptsReviewCmd() *cobra.Command {
	var workspace string
	var status string
	cmd := &cobra.Command{
		Use:   "review",
		Short: "List pending suggestions in the review queue (gm-s47n.7.3)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			list, err := concepts.LoadSuggestions(root)
			if err != nil {
				return err
			}
			var subset []concepts.Suggestion
			switch status {
			case "pending":
				subset = list.Pending()
			case "approved":
				subset = list.Approved()
			case "rejected":
				subset = list.Rejected()
			default:
				return fmt.Errorf("unknown --status %q (want pending|approved|rejected)", status)
			}
			out := cmd.OutOrStdout()
			if len(subset) == 0 {
				fmt.Fprintln(out, "(none)")
				return nil
			}
			for _, s := range subset {
				switch s.Kind {
				case concepts.KindMerge, concepts.KindRename:
					fmt.Fprintf(out, "%s\t%s\t%s → %s\t%s\n",
						s.ID, s.Kind, s.From, s.To, s.Reason)
				case concepts.KindDelete:
					fmt.Fprintf(out, "%s\t%s\t%s\t%s\n",
						s.ID, s.Kind, s.From, s.Reason)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().StringVar(&status, "status", "pending", "filter by status: pending|approved|rejected")
	return cmd
}

func newConceptsApproveCmd() *cobra.Command {
	var workspace string
	var by string
	cmd := &cobra.Command{
		Use:   "approve <suggestion-id>",
		Short: "Apply an approved suggestion (rewrites historical beads, gm-s47n.7.4)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			v, err := concepts.LoadVocabulary(root)
			if err != nil {
				return err
			}
			list, err := concepts.LoadSuggestions(root)
			if err != nil {
				return err
			}
			store := loadBeadStore(root)
			if store == nil {
				// Without a wired store we can still flip the
				// vocabulary side; the historical rewrite count is 0.
				store = concepts.NewMemoryStore()
			}
			operator := by
			if operator == "" {
				operator = currentOperator()
			}
			dec, err := concepts.ApplyDecision(cmd.Context(), v, list, store, args[0], operator)
			if err != nil {
				return err
			}
			if err := concepts.SaveVocabulary(root, v); err != nil {
				return err
			}
			if err := concepts.SaveSuggestions(root, list); err != nil {
				return err
			}
			if err := concepts.AppendDecision(root, dec); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "approved %s (%s); %d bead(s) rewritten\n",
				dec.SuggestionID, dec.Kind, dec.BeadsChanged)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().StringVar(&by, "by", "", "operator id (default: $USER)")
	return cmd
}

func newConceptsRejectCmd() *cobra.Command {
	var workspace string
	var by string
	var reason string
	cmd := &cobra.Command{
		Use:   "reject <suggestion-id>",
		Short: "Reject a suggestion without applying it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			list, err := concepts.LoadSuggestions(root)
			if err != nil {
				return err
			}
			operator := by
			if operator == "" {
				operator = currentOperator()
			}
			dec, err := concepts.RejectDecision(list, args[0], operator, reason)
			if err != nil {
				return err
			}
			if err := concepts.SaveSuggestions(root, list); err != nil {
				return err
			}
			if err := concepts.AppendDecision(root, dec); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rejected %s\n", dec.SuggestionID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().StringVar(&by, "by", "", "operator id (default: $USER)")
	cmd.Flags().StringVar(&reason, "reason", "", "human-readable rejection rationale")
	return cmd
}

func newConceptsLogCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show the append-only decisions audit log",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			entries, err := concepts.ReadDecisions(root)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(out, "(no decisions yet)")
				return nil
			}
			for _, d := range entries {
				when := d.At.Format(time.RFC3339)
				switch d.Kind {
				case concepts.KindMerge, concepts.KindRename:
					fmt.Fprintf(out, "%s\t%s\t%s\t%s → %s\tby=%s\tbeads=%d\n",
						when, d.Action, d.Kind, d.From, d.To, d.By, d.BeadsChanged)
				case concepts.KindDelete:
					fmt.Fprintf(out, "%s\t%s\t%s\t%s\tby=%s\tbeads=%d\n",
						when, d.Action, d.Kind, d.From, d.By, d.BeadsChanged)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	return cmd
}

func resolveWorkspace(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	return os.Getwd()
}

func currentOperator() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "operator"
}

// loadBeadStore is the integration shim that returns a production
// BeadConceptStore once gm-s47n.1.1 lands a WorkPlane-backed
// implementation. Returning nil today is the documented fallback —
// CLI commands branch on it and skip the bead-touching half.
func loadBeadStore(_ string) concepts.BeadConceptStore {
	return nil
}

func printBootstrapReport(w io.Writer, v *concepts.Vocabulary, res *concepts.BootstrapResult, dryRun bool) {
	mode := "saved"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(w, "Bootstrapped %d term(s) [%s]\n", res.Total, mode)
	for _, b := range res.BySource {
		fmt.Fprintf(w, "  %s: %d\n", b.Source, b.Count)
	}
	if res.Skipped > 0 {
		fmt.Fprintf(w, "  (skipped %d candidate(s) — Max cap reached)\n", res.Skipped)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(w, "  warning: %s\n", e.Error())
	}
	fmt.Fprintln(w, "")
	for _, t := range v.Terms {
		fmt.Fprintf(w, "  %s\t%s\n", t.Name, t.Source)
	}
}

func printDriftReport(w io.Writer, d concepts.Drift, fresh []concepts.Suggestion, persisted bool) {
	mode := "preview"
	if persisted {
		mode = "persisted"
	}
	fmt.Fprintf(w, "Drift report [%s]\n", mode)
	fmt.Fprintf(w, "  near-duplicates: %d\n", len(d.NearDuplicates))
	fmt.Fprintf(w, "  singletons: %d\n", len(d.Singletons))
	fmt.Fprintf(w, "  new suggestions: %d\n\n", len(fresh))
	for _, s := range fresh {
		switch s.Kind {
		case concepts.KindMerge, concepts.KindRename:
			fmt.Fprintf(w, "  %s\t%s\t%s → %s\t%s\n",
				s.ID, s.Kind, s.From, s.To, s.Reason)
		case concepts.KindDelete:
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
				s.ID, s.Kind, s.From, s.Reason)
		}
	}
}

// _ keeps context.Background available for future subcommands that
// don't have a Cobra cmd context handy.
var _ = context.Background
