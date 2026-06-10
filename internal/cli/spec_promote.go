// gemba spec promote — surface bd mail decision threads as a unified diff
// the operator can review and `git apply` manually. Per ASDD DoD: we never
// mutate the spec file in place. Git is the journal.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/parser"
	"github.com/GembaCore/gemba-core/internal/spec/promote"
)

func newSpecPromoteCmd() *cobra.Command {
	var mailID string
	var edit bool
	var apply bool

	cmd := &cobra.Command{
		Use:   "promote <spec.md>",
		Short: "Surface bd-mail decisions as a PR-style diff (no in-place commit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			data, err := os.ReadFile(specPath)
			if err != nil {
				return err
			}
			spec, err := parser.Parse(data)
			if err != nil {
				return err
			}
			slug := deriveSpecSlug(specPath, spec)

			if apply {
				fmt.Fprintln(cmd.ErrOrStderr(),
					"refusing: apply is manual — pipe to `git apply`")
				os.Exit(1)
				return nil
			}

			ctx := context.Background()
			cands, err := promote.Discover(ctx, slug, promote.BDMailLister{})
			if err != nil {
				return err
			}
			promote.SortCandidates(cands)

			if mailID == "" {
				if len(cands) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no candidate mail threads for spec-decision:"+slug)
					return nil
				}
				for _, c := range cands {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", c.MailID, c.Timestamp, c.Title)
				}
				return nil
			}

			var pick *promote.Candidate
			for i := range cands {
				if cands[i].MailID == mailID {
					pick = &cands[i]
					break
				}
			}
			if pick == nil {
				return fmt.Errorf("mail id %q not found among spec-decision:%s candidates", mailID, slug)
			}

			// Use a path relative to the cwd for the diff's a/ and b/
			// headers — that's what `git apply` expects.
			relPath, err := filepath.Rel(mustGetwd(), specPath)
			if err != nil || strings.HasPrefix(relPath, "..") {
				relPath = specPath
			}
			diff, err := promote.Propose(relPath, data, spec, *pick)
			if err != nil {
				return err
			}

			if edit {
				tmp, err := os.CreateTemp("", "spec-promote-*.diff")
				if err != nil {
					return err
				}
				if _, err := tmp.WriteString(diff); err != nil {
					_ = tmp.Close()
					return err
				}
				if err := tmp.Close(); err != nil {
					return err
				}
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vi"
				}
				ed := exec.Command(editor, tmp.Name())
				ed.Stdin = os.Stdin
				ed.Stdout = os.Stdout
				ed.Stderr = os.Stderr
				if err := ed.Run(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), tmp.Name())
				return nil
			}

			_, _ = fmt.Fprint(cmd.OutOrStdout(), diff)
			return nil
		},
	}

	cmd.Flags().StringVar(&mailID, "mail-id", "", "specific bd-mail id to promote (omit to list candidates)")
	cmd.Flags().BoolVar(&edit, "edit", false, "write the diff to a temp file and open $EDITOR (no in-place write)")
	cmd.Flags().BoolVar(&apply, "apply", false, "(refused) apply is manual — pipe to `git apply`")
	return cmd
}

// deriveSpecSlug picks the spec slug from spec_id frontmatter when present,
// otherwise falls back to the parent directory name of spec.md. This matches
// the `gemba spec new <slug>` layout where specs live at specs/<slug>/spec.md.
func deriveSpecSlug(specPath string, spec *parser.Spec) string {
	if spec != nil && spec.Frontmatter.SpecID != "" {
		return spec.Frontmatter.SpecID
	}
	return filepath.Base(filepath.Dir(specPath))
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
