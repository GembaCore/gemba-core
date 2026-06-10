package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/hooks"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// newSpecCloseCheckCmd wires `gemba spec close-check <spec.md> --epic
// <bead-id>` — the ASDD closure gate (gm-v0sp.12).
//
// Exit semantics:
//
//	0 — gate allows close (clean OR --force with --reason)
//	2 — gate refuses close; stdout lists the blocking T-NN stories
//	1 — usage error or I/O failure
//
// The CLI is the operator-facing entry point bd's pre-close hook script
// (scripts/bd-pre-close.sh) shells out to.
func newSpecCloseCheckCmd() *cobra.Command {
	var (
		lockPath string
		label    string
		epicID   string
		force    bool
		reason   string
		bdBin    string
	)
	cmd := &cobra.Command{
		Use:   "close-check <spec.md>",
		Short: "Closure gate: refuse to close an epic while T-NN stories remain open",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath := args[0]
			if strings.TrimSpace(epicID) == "" {
				return fmt.Errorf("--epic is required")
			}
			if force && strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--force requires --reason")
			}
			if lockPath == "" {
				lockPath = filepath.Join(filepath.Dir(specPath), ".spec.lock.json")
			}

			lister := reconcile.NewBeadLister(bdBin, nil)
			res, err := hooks.Check(cmd.Context(), specPath, lockPath, epicID, hooks.CheckOptions{
				BeadLister:  lister,
				Label:       label,
				Force:       force,
				ForceReason: reason,
			})
			if err != nil {
				return err
			}
			if res.Allow {
				if force {
					fmt.Fprintf(cmd.OutOrStdout(), "FORCE: closure gate bypassed (reason=%q, %d open stories)\n", res.Reason, len(res.OpenStories))
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "PASS: all mapped stories resolved")
				}
				return nil
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "REFUSE:", res.Reason)
			for _, s := range res.OpenStories {
				status := s.Status
				if status == "" {
					status = "unknown"
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "  blocker: anchor=%s bead=%s status=%s\n", s.Anchor, s.BeadID, status)
			}
			os.Exit(2)
			return nil
		},
	}
	cmd.Flags().StringVar(&lockPath, "lockfile", "", "path to .spec.lock.json (default: alongside spec)")
	cmd.Flags().StringVar(&label, "label", "", "bd label to query (default: spec:<spec_id>)")
	cmd.Flags().StringVar(&epicID, "epic", "", "bd ID of the epic the operator wants to close (required)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the gate; requires --reason and emits an audit event")
	cmd.Flags().StringVar(&reason, "reason", "", "operator reason recorded in audit trail (required with --force)")
	cmd.Flags().StringVar(&bdBin, "bd-bin", "", "bd binary path (default: resolve from PATH)")
	return cmd
}
