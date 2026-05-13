package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/lint"
)

// newSpecLintCmd wires `gemba spec lint <spec.md>`.
//
// Constitution is resolved from --constitution if given, otherwise discovered
// by walking up from the spec (and optional --project) for `.gemba` or
// `.specify`. Table output by default; --json for machine consumers.
func newSpecLintCmd() *cobra.Command {
	var conPath, projectRoot string
	var jsonOut, strict bool
	cmd := &cobra.Command{
		Use:   "lint <spec.md>",
		Short: "Lint a spec.md against the project constitution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			cp := conPath
			if cp == "" {
				cp = locateConstitution(projectRoot, specPath)
			}
			findings, err := lint.Lint(specPath, cp)
			if err != nil {
				return err
			}
			if jsonOut {
				if findings == nil {
					findings = []lint.Finding{}
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(findings); err != nil {
					return err
				}
			} else {
				if len(findings) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "ok: no findings")
				} else {
					tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
					fmt.Fprintln(tw, "SEVERITY\tRULE\tANCHOR\tMESSAGE")
					for _, f := range findings {
						sev := f.Severity
						if sev == "" {
							sev = "error"
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", sev, f.Rule, f.Anchor, f.Message)
					}
					tw.Flush()
				}
			}
			if strict {
				for _, f := range findings {
					if f.Severity == "" || f.Severity == "error" {
						os.Exit(1)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&conPath, "constitution", "", "explicit path to constitution.md (default: discover from spec)")
	cmd.Flags().StringVar(&projectRoot, "project", "", "project root for constitution discovery")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a table")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit nonzero when any error-severity finding exists")
	return cmd
}

// locateConstitution searches upward from specPath (or projectRoot when given)
// for a recognized constitution location. Returns "" when none is found.
func locateConstitution(projectRoot, specPath string) string {
	var starts []string
	if projectRoot != "" {
		if abs, err := filepath.Abs(projectRoot); err == nil {
			starts = append(starts, abs)
		}
	}
	starts = append(starts, filepath.Dir(specPath))
	candidates := []string{
		filepath.Join(".gemba", "constitution.md"),
		filepath.Join(".specify", "memory", "constitution.md"),
	}
	for _, start := range starts {
		dir := start
		for {
			for _, c := range candidates {
				p := filepath.Join(dir, c)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}
