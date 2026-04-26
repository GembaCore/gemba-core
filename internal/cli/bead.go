package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/concepts"
	"github.com/MikeBengtson/gemba/internal/enrichment"
)

// newBeadCmd builds the `gemba bead` subtree (gm-s47n.1.3). Provides
// the operator surface for viewing and overriding the targets[] +
// concepts[] enrichment a bead carries — the data layer the planner
// reads when scoring conflicts and affinity.
//
// Until gm-s47n.1.1 lands the WorkItem.targets / .concepts schema
// the storage backend is a per-bead JSON file under
// .gemba/enrichment/. The CLI surface is the same either way; only
// the [enrichment.Store] implementation flips.
func newBeadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bead",
		Short: "View and override per-bead planner enrichment (targets + concepts)",
		Long: `Operator surface for the gm-s47n.1 enrichment data the planner
reads off every bead.

  - targets[]: path globs the bead is expected to write. Drives
    the conflict scorer's collision detection (work-planning §2.1).
  - concepts[]: vocabulary tags from the controlled set governed
    by gm-s47n.7 (manage via "gemba concepts"). Drives the affinity
    scorer's session-priming heuristic (work-planning §2.2).

Storage today: <workspace>/.gemba/enrichment/<bead-id>.json.
Rewires to bd extras when gm-s47n.1.1 lands the WorkItem schema.`,
	}
	cmd.AddCommand(
		newBeadShowCmd(),
		newBeadListCmd(),
		newBeadTargetsCmd(),
		newBeadConceptsCmd(),
		newBeadExtractCmd(),
	)
	return cmd
}

// ── show / list ────────────────────────────────────────────────────

func newBeadShowCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "show <bead-id>",
		Short: "Print the targets + concepts recorded for a bead",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			store := enrichment.NewFileStore(root, nil)
			e, err := store.Load(cmd.Context(), args[0])
			if errors.Is(err, enrichment.ErrNotFound) {
				fmt.Fprintln(cmd.OutOrStdout(), "(no enrichment recorded)")
				return nil
			}
			if err != nil {
				return err
			}
			printEnrichment(cmd.OutOrStdout(), e)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root (default: current dir)")
	return cmd
}

func newBeadListCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every bead with recorded enrichment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			store := enrichment.NewFileStore(root, nil)
			ids, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(ids) == 0 {
				fmt.Fprintln(out, "(no enriched beads yet)")
				return nil
			}
			for _, id := range ids {
				fmt.Fprintln(out, id)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	return cmd
}

// ── extract ───────────────────────────────────────────────────────

func newBeadExtractCmd() *cobra.Command {
	var (
		workspace string
		title     string
		body      string
		spec      string
		bodyFile  string
		specFile  string
		dryRun    bool
		merge     bool
	)
	cmd := &cobra.Command{
		Use:   "extract <bead-id>",
		Short: "Extract a starter targets/concepts enrichment from bead text (gm-s47n.1.2)",
		Long: `Run the configured Extractor over a bead's title + body + linked
spec and stash the result as the bead's enrichment.

The default extractor is the network-free HeuristicExtractor: it
mines path-shaped tokens (backtick-fenced or bareword with a
recognized prefix) and matches the supplied vocabulary against the
text. An LLM-backed extractor will land behind the same Extractor
interface; the CLI surface stays unchanged.

By default --merge=false (a fresh extraction replaces any prior
enrichment, sourced as 'llm' for retrospective grading purposes).
Pass --merge to UNION the new extraction with the existing
enrichment instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			beadID := args[0]

			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("read --body-file: %w", err)
				}
				body = string(b)
			}
			if specFile != "" {
				b, err := os.ReadFile(specFile)
				if err != nil {
					return fmt.Errorf("read --spec-file: %w", err)
				}
				spec = string(b)
			}

			vocab := loadVocabularyTerms(root)
			extracted, err := enrichment.HeuristicExtractor{}.Extract(cmd.Context(),
				enrichment.BeadInput{
					BeadID:     beadID,
					Title:      title,
					Body:       body,
					Spec:       spec,
					Vocabulary: vocab,
				})
			if err != nil {
				return err
			}

			store := enrichment.NewFileStore(root, nil)
			final := extracted
			if merge {
				existing, loadErr := store.Load(cmd.Context(), beadID)
				if loadErr != nil && !errors.Is(loadErr, enrichment.ErrNotFound) {
					return loadErr
				}
				final = mergeEnrichments(existing, extracted)
			}
			final.BeadID = beadID
			if final.Source == "" {
				final.Source = enrichment.SourceLLM
			}

			if !dryRun {
				if err := store.Save(cmd.Context(), final); err != nil {
					return err
				}
			}

			out := cmd.OutOrStdout()
			if dryRun {
				fmt.Fprintln(out, "(dry-run; not persisted)")
			}
			printEnrichment(out, final)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().StringVar(&title, "title", "", "bead title")
	cmd.Flags().StringVar(&body, "body", "", "bead body / description")
	cmd.Flags().StringVar(&spec, "spec", "", "linked spec body (extra searchable text)")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read --body from a file path")
	cmd.Flags().StringVar(&specFile, "spec-file", "", "read --spec from a file path")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be extracted without writing")
	cmd.Flags().BoolVar(&merge, "merge", false, "union with existing enrichment instead of replacing")
	return cmd
}

// loadVocabularyTerms returns the canonical names + aliases of the
// active vocabulary so the heuristic extractor can match against
// every form a writer might use. Empty slice when no vocabulary is
// bootstrapped — extractor degrades cleanly.
func loadVocabularyTerms(root string) []string {
	v, err := concepts.LoadVocabulary(root)
	if err != nil || v == nil {
		return nil
	}
	terms := v.Active()
	out := make([]string, 0, len(terms)*2)
	for _, t := range terms {
		out = append(out, t.Name)
		out = append(out, t.Aliases...)
	}
	return out
}

// mergeEnrichments unions the extracted result on top of an
// existing one. Operator-stamped concepts/targets stay; the
// extractor never demotes the Source.
func mergeEnrichments(existing, extracted enrichment.Enrichment) enrichment.Enrichment {
	out := existing
	for _, t := range extracted.Targets {
		out = out.AddTarget(t)
	}
	for _, c := range extracted.Concepts {
		out = out.AddConcept(c)
	}
	if existing.Source == "" {
		out.Source = extracted.Source
	}
	return out
}

// ── targets ───────────────────────────────────────────────────────

func newBeadTargetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "Manage the targets[] glob list on a bead",
	}
	cmd.AddCommand(
		newBeadTargetsActionCmd("add", "add a target glob to the bead",
			func(e enrichment.Enrichment, vs []string) enrichment.Enrichment {
				for _, v := range vs {
					e = e.AddTarget(v)
				}
				return e
			}, true),
		newBeadTargetsActionCmd("rm", "remove a target glob from the bead",
			func(e enrichment.Enrichment, vs []string) enrichment.Enrichment {
				for _, v := range vs {
					e = e.RemoveTarget(v)
				}
				return e
			}, true),
		newBeadTargetsActionCmd("set", "replace the bead's target list (empty = clear)",
			func(e enrichment.Enrichment, vs []string) enrichment.Enrichment {
				return e.SetTargets(vs)
			}, false),
	)
	return cmd
}

func newBeadTargetsActionCmd(verb, short string, apply func(enrichment.Enrichment, []string) enrichment.Enrichment, requireValues bool) *cobra.Command {
	var workspace, by string
	cmd := &cobra.Command{
		Use:   verb + " <bead-id> [glob...]",
		Short: short,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("expected <bead-id> argument")
			}
			if requireValues && len(args) < 2 {
				return fmt.Errorf("expected at least one glob argument")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			store := enrichment.NewFileStore(root, nil)
			beadID := args[0]
			rest := args[1:]
			e := loadOrFresh(cmd.Context(), store, beadID, by)
			next := apply(e, rest)
			next.BeadID = beadID
			if next.Source == "" {
				next.Source = enrichment.SourceOperator
			}
			if err := store.Save(cmd.Context(), next); err != nil {
				return err
			}
			printEnrichment(cmd.OutOrStdout(), next)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().StringVar(&by, "by", "", "operator id (default: $USER)")
	return cmd
}

// ── concepts ──────────────────────────────────────────────────────

func newBeadConceptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "concepts",
		Short: "Manage the concepts[] vocabulary tags on a bead",
	}
	cmd.AddCommand(
		newBeadConceptsActionCmd("add", "add a concept tag (validated against vocabulary)",
			func(e enrichment.Enrichment, vs []string) enrichment.Enrichment {
				for _, v := range vs {
					e = e.AddConcept(v)
				}
				return e
			}, true),
		newBeadConceptsActionCmd("rm", "remove a concept tag",
			func(e enrichment.Enrichment, vs []string) enrichment.Enrichment {
				for _, v := range vs {
					e = e.RemoveConcept(v)
				}
				return e
			}, true),
		newBeadConceptsActionCmd("set", "replace the bead's concept list (empty = clear)",
			func(e enrichment.Enrichment, vs []string) enrichment.Enrichment {
				return e.SetConcepts(vs)
			}, false),
	)
	return cmd
}

func newBeadConceptsActionCmd(verb, short string, apply func(enrichment.Enrichment, []string) enrichment.Enrichment, requireValues bool) *cobra.Command {
	var workspace, by string
	var force bool
	cmd := &cobra.Command{
		Use:   verb + " <bead-id> [tag...]",
		Short: short,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("expected <bead-id> argument")
			}
			if requireValues && len(args) < 2 {
				return fmt.Errorf("expected at least one tag argument")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveWorkspace(workspace)
			if err != nil {
				return err
			}
			beadID := args[0]
			tags := args[1:]
			// Vocabulary check: warn on unknown tags so the operator
			// can spot typos without blocking the edit. Bootstrapped
			// only when the caller is adding tags; rm / set-clear
			// never need vocabulary validation.
			if verb != "rm" && len(tags) > 0 {
				// cmd.ErrOrStderr is the SetErr-aware writer; the
				// confusingly-named OutOrStderr returns the SetOut
				// writer with an os.Stderr default, which mixes the
				// warning into the bead-show output a script may be
				// parsing.
				warnUnknownConcepts(cmd.ErrOrStderr(), root, tags, force)
			}
			store := enrichment.NewFileStore(root, nil)
			e := loadOrFresh(cmd.Context(), store, beadID, by)
			next := apply(e, tags)
			next.BeadID = beadID
			if next.Source == "" {
				next.Source = enrichment.SourceOperator
			}
			if err := store.Save(cmd.Context(), next); err != nil {
				return err
			}
			printEnrichment(cmd.OutOrStdout(), next)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "workspace root")
	cmd.Flags().StringVar(&by, "by", "", "operator id (default: $USER)")
	cmd.Flags().BoolVar(&force, "force", false, "suppress unknown-concept warnings")
	return cmd
}

// ── helpers ───────────────────────────────────────────────────────

func loadOrFresh(ctx context.Context, store enrichment.Store, beadID, by string) enrichment.Enrichment {
	e, err := store.Load(ctx, beadID)
	if errors.Is(err, enrichment.ErrNotFound) {
		_ = by // by stays available for future audit-log routing
		return enrichment.Enrichment{BeadID: beadID, Source: enrichment.SourceOperator}
	}
	if err != nil {
		// Print to stderr but proceed with a fresh enrichment so a
		// corrupt file doesn't permanently lock out edits.
		fmt.Fprintf(os.Stderr, "warning: load %s: %v (starting fresh)\n", beadID, err)
		return enrichment.Enrichment{BeadID: beadID, Source: enrichment.SourceOperator}
	}
	return e
}

func warnUnknownConcepts(w io.Writer, workspace string, tags []string, force bool) {
	if force {
		return
	}
	v, err := concepts.LoadVocabulary(workspace)
	if err != nil || len(v.Terms) == 0 {
		// Vocabulary not bootstrapped yet — nothing to warn against.
		return
	}
	for _, t := range tags {
		canon := concepts.Normalize(t)
		if canon == "" {
			continue
		}
		if _, ok := v.Find(canon); ok {
			continue
		}
		fmt.Fprintf(w, "warning: %q is not in the vocabulary (run `gemba concepts list` or pass --force)\n", canon)
	}
}

func printEnrichment(w io.Writer, e enrichment.Enrichment) {
	fmt.Fprintf(w, "bead: %s\n", e.BeadID)
	if e.Source != "" {
		fmt.Fprintf(w, "source: %s\n", e.Source)
	}
	if !e.UpdatedAt.IsZero() {
		fmt.Fprintf(w, "updated: %s\n", e.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	if e.IsZero() {
		fmt.Fprintln(w, "(no targets, no concepts)")
		return
	}
	if len(e.Targets) > 0 {
		fmt.Fprintln(w, "targets:")
		for _, t := range e.Targets {
			fmt.Fprintln(w, "  "+t)
		}
	}
	if len(e.Concepts) > 0 {
		fmt.Fprintln(w, "concepts:")
		for _, c := range e.Concepts {
			fmt.Fprintln(w, "  "+c)
		}
	}
}

// quietJoin keeps the command examples readable; not used in
// production paths but kept here so the docstring sample stays
// runnable copy-paste.
func quietJoin(in []string) string { return strings.Join(in, " ") }

var _ = quietJoin
