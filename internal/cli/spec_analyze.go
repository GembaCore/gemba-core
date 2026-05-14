// spec_analyze.go — `gemba spec analyze` command tree.
//
// Reads a rejected tasks.md / todo.md payload (from --input, stdin, or
// the GEMBA_REJECTED_PAYLOAD env var the spec hook sets when it blocks
// a write), runs internal/spec/analyzer over it, prints the proposal as
// a table, and — when both --apply and --confirm are passed — materializes
// each ProposedBead via `bd create`.
//
// The shellout to bd intentionally re-lists every label cumulatively
// (gemba CLAUDE.md: bd does not auto-inherit). When --parent is provided,
// the analyzer first calls `bd show <parent>` to harvest that parent's
// labels and folds them into the cumulative list applied to every new
// bead, alongside an `analyzer-generated` marker.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/analyzer"
)

// bdExecutor abstracts shellout to `bd` so tests can inject a stub.
type bdExecutor func(ctx context.Context, args ...string) (stdout string, err error)

func defaultBDExecutor(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "bd", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("bd %s: %w: %s", strings.Join(args, " "), err, string(ee.Stderr))
		}
		return string(out), fmt.Errorf("bd %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// newSpecAnalyzeCmd builds the cobra subcommand. The bdExec injection
// point is exposed via a package-level var so tests can substitute it.
var bdExec bdExecutor = defaultBDExecutor

func newSpecAnalyzeCmd() *cobra.Command {
	var (
		input    string
		apply    bool
		confirm  bool
		parentID string
		initTag  string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a rejected tasks.md payload into proposed beads",
		Long: `Analyze a rejected tasks.md / todo.md payload (markdown body or
PreToolUse JSON) and emit a Proposal of milestones/epics/stories.

Input precedence: --input <file> > stdin > GEMBA_REJECTED_PAYLOAD.

With --apply --confirm, creates beads via 'bd create'. --parent <id>
makes the analyzer inherit that bead's full label set (cumulative per
gemba CLAUDE.md) and parent the top-level proposed bead to it.

LLM refinement is opt-in via GEMBA_ANALYZER_LLM=<endpoint>.`,
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			payload, err := readPayload(input, c.InOrStdin())
			if err != nil {
				return err
			}
			opts := analyzer.Options{
				LLMEndpoint: os.Getenv("GEMBA_ANALYZER_LLM"),
			}
			prop, err := analyzer.Analyze(ctx, payload, opts)
			if err != nil {
				return err
			}
			emitAnalyzerEvent(ctx, prop, parentID)

			if jsonOut {
				return json.NewEncoder(c.OutOrStdout()).Encode(prop)
			}
			renderProposal(c.OutOrStdout(), prop)

			if !apply {
				return nil
			}
			if !confirm {
				fmt.Fprintln(c.ErrOrStderr(), "refusing to apply without --confirm")
				return fmt.Errorf("--apply requires --confirm")
			}
			parentLabels, err := harvestParentLabels(ctx, parentID)
			if err != nil {
				return fmt.Errorf("harvest parent labels: %w", err)
			}
			return applyProposal(ctx, c.OutOrStdout(), prop, parentID, parentLabels, initTag)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "read payload from file path (use '-' or omit for stdin / env)")
	cmd.Flags().BoolVar(&apply, "apply", false, "create the proposed beads via bd create")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "REQUIRED with --apply — explicit acknowledgement")
	cmd.Flags().StringVar(&parentID, "parent", "", "existing bead id to parent the top-level proposed bead under (its labels are inherited cumulatively)")
	cmd.Flags().StringVar(&initTag, "initiative", "", "initiative label to add to every created bead (e.g. gemba-remote)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the proposal as JSON instead of a table")
	return cmd
}

// readPayload resolves the payload source per precedence rule.
func readPayload(input string, stdin io.Reader) (string, error) {
	if input != "" && input != "-" {
		b, err := os.ReadFile(input)
		if err != nil {
			return "", fmt.Errorf("read --input %s: %w", input, err)
		}
		return string(b), nil
	}
	// Try stdin only if it's not a TTY-ish empty source. We can't reliably
	// detect a TTY here without extra deps; instead we try reading and fall
	// back to the env var if we get nothing.
	if input == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		if len(strings.TrimSpace(string(b))) > 0 {
			return string(b), nil
		}
	} else {
		// Best-effort stdin slurp with no blocking guarantees: callers
		// that pipe content will populate it; interactive shells return
		// empty quickly via the cobra test harness.
		if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
			b, err := io.ReadAll(stdin)
			if err == nil && len(strings.TrimSpace(string(b))) > 0 {
				return string(b), nil
			}
		}
	}
	if env := os.Getenv("GEMBA_REJECTED_PAYLOAD"); strings.TrimSpace(env) != "" {
		return env, nil
	}
	return "", fmt.Errorf("no payload: pass --input, pipe stdin, or set GEMBA_REJECTED_PAYLOAD")
}

func renderProposal(w io.Writer, p *analyzer.Proposal) {
	fmt.Fprintf(w, "source: %s\n", p.Source)
	fmt.Fprintf(w, "%d proposed beads:\n\n", len(p.Beads))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\tKIND\tPARENT\tPRIO\tTITLE")
	for _, b := range p.Beads {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			b.LocalRef, b.Kind, dashIfEmpty(b.Parent), dashIfEmpty(b.Priority), b.Title)
	}
	_ = tw.Flush()
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// harvestParentLabels shells out to `bd show <parent> --json` and pulls
// the labels array. Best-effort: empty parentID returns nil.
func harvestParentLabels(ctx context.Context, parentID string) ([]string, error) {
	if parentID == "" {
		return nil, nil
	}
	out, err := bdExec(ctx, "show", parentID, "--json")
	if err != nil {
		return nil, err
	}
	var v struct {
		Labels []string `json:"labels"`
	}
	if jerr := json.Unmarshal([]byte(out), &v); jerr != nil {
		// Some bd builds wrap the bead in {"bead":{...}}; try that shape.
		var wrap struct {
			Bead struct {
				Labels []string `json:"labels"`
			} `json:"bead"`
		}
		if jerr2 := json.Unmarshal([]byte(out), &wrap); jerr2 == nil {
			return wrap.Bead.Labels, nil
		}
		return nil, fmt.Errorf("parse bd show JSON: %w", jerr)
	}
	return v.Labels, nil
}

// applyProposal walks the proposal in order and shells out to bd create
// for each entry. Local refs ("#N") in Parent are rewritten to the bead
// id returned by the prior create.
func applyProposal(ctx context.Context, w io.Writer, p *analyzer.Proposal, parentID string, parentLabels []string, initiative string) error {
	createdIDs := make(map[string]string, len(p.Beads)) // localRef → bead id

	for i := range p.Beads {
		b := p.Beads[i]
		// Resolve parent id: external parentID for top-level beads,
		// or createdIDs[localRef] for inner beads.
		resolvedParent := ""
		switch {
		case b.Parent == "" && parentID != "":
			resolvedParent = parentID
		case b.Parent != "":
			if id, ok := createdIDs[b.Parent]; ok {
				resolvedParent = id
			} else if parentID != "" {
				resolvedParent = parentID
			}
		}

		labels := analyzerMergeLabels(parentLabels, b.Labels, []string{
			string(b.Kind),
			"analyzer-generated",
		})
		if initiative != "" {
			labels = analyzerMergeLabels(labels, []string{initiative})
		}

		args := []string{"create",
			"--type", string(b.Kind),
			"--title", titleWithPrefix(b.Kind, b.Title),
		}
		if resolvedParent != "" {
			args = append(args, "--parent", resolvedParent)
		}
		if b.Priority != "" {
			args = append(args, "--priority", b.Priority)
		}
		for _, l := range labels {
			args = append(args, "--label", l)
		}

		out, err := bdExec(ctx, args...)
		if err != nil {
			return fmt.Errorf("create bead %s (%s): %w", b.LocalRef, b.Title, err)
		}
		id := extractBeadID(out)
		createdIDs[b.LocalRef] = id
		fmt.Fprintf(w, "created %s -> %s (%s)\n", b.LocalRef, id, b.Title)
	}
	return nil
}

// titleWithPrefix ensures the bead title carries the mandatory schema
// prefix from gemba CLAUDE.md ("Milestone: …", "Epic: …", "Story: …").
func titleWithPrefix(k analyzer.Kind, title string) string {
	prefix := ""
	switch k {
	case analyzer.KindMilestone:
		prefix = "Milestone: "
	case analyzer.KindEpic:
		prefix = "Epic: "
	case analyzer.KindStory:
		prefix = "Story: "
	}
	if strings.HasPrefix(title, prefix) {
		return title
	}
	return prefix + title
}

// analyzerMergeLabels deduplicates and sorts label slices.
func analyzerMergeLabels(sets ...[]string) []string {
	seen := map[string]struct{}{}
	for _, s := range sets {
		for _, v := range s {
			if v == "" {
				continue
			}
			seen[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// extractBeadID is a best-effort scrape of `bd create` output. bd
// typically prints something like "Created issue: gm-v0sp.21.1" or
// JSON; we hunt for an id-looking token rather than depending on a
// specific shape.
func extractBeadID(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	// Try JSON shape first.
	var j struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &j); err == nil && j.ID != "" {
		return j.ID
	}
	// Else look for a token like gm-xxxx[.N]+
	for _, tok := range strings.Fields(out) {
		t := strings.Trim(tok, ":,.()")
		if strings.HasPrefix(t, "gm-") || strings.HasPrefix(t, "bd-") {
			return t
		}
	}
	return out // fall through — caller still logs it
}

// emitAnalyzerEvent surfaces a spec.analyzer.proposal event. CLI-side
// there is no Auditor in scope, so this routes through slog per the
// gm-v0sp.21 contract. Failures are non-blocking.
func emitAnalyzerEvent(_ context.Context, prop *analyzer.Proposal, parentID string) {
	defer func() {
		_ = recover() // never block on slog
	}()
	slog.Info("spec.analyzer.proposal",
		slog.String("event", "spec.analyzer.proposal"),
		slog.Int("beads", len(prop.Beads)),
		slog.String("source", prop.Source),
		slog.String("parent", parentID),
		slog.String("ts", time.Now().UTC().Format(time.RFC3339Nano)),
	)
}
