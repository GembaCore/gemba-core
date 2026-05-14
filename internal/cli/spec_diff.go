package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/parser"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// newSpecDiffCmd wires `gemba spec diff <spec.md>`.
//
// It is a pure planner — no bd mutations, no disk writes. The CLI:
//  1. parses spec.md,
//  2. loads the sibling .spec.lock.json (if any),
//  3. calls reconcile.Diff with either the real bd lister or a test stub
//     (--bd-stub <path-to-json>),
//  4. emits either a human table or JSON.
func newSpecDiffCmd() *cobra.Command {
	var jsonOut bool
	var label string
	var bdStub string
	var lockPath string
	cmd := &cobra.Command{
		Use:   "diff <spec.md>",
		Short: "Plan the create/update/orphan diff between spec.md and bd (read-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			specBytes, err := os.ReadFile(specPath)
			if err != nil {
				return fmt.Errorf("read spec: %w", err)
			}
			spec, err := parser.Parse(specBytes)
			if err != nil {
				return fmt.Errorf("parse spec: %w", err)
			}

			lp := lockPath
			if lp == "" {
				lp = filepath.Join(filepath.Dir(specPath), ".spec.lock.json")
			}
			var lock *lockfile.Lock
			if _, err := os.Stat(lp); err == nil {
				lock, err = lockfile.Load(lp)
				if err != nil {
					return fmt.Errorf("load lockfile: %w", err)
				}
			}

			lbl := label
			if lbl == "" {
				slug := strings.TrimSuffix(filepath.Base(filepath.Dir(specPath)), "/")
				lbl = "spec:" + slug
			}

			var lister reconcile.BeadLister
			if bdStub != "" {
				stubBytes, err := os.ReadFile(bdStub)
				if err != nil {
					return fmt.Errorf("read --bd-stub: %w", err)
				}
				beads, err := stubBeads(stubBytes)
				if err != nil {
					return err
				}
				lister = &reconcile.StaticBeadLister{Beads: beads}
			} else {
				lister = reconcile.NewBeadLister("", nil)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			beads, err := lister.List(ctx, lbl)
			if err != nil {
				return fmt.Errorf("list beads: %w", err)
			}

			plan, err := reconcile.Diff(spec, beads, lock)
			if err != nil {
				return err
			}

			if jsonOut {
				data, err := plan.MarshalJSON()
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}
			return renderPlanTable(cmd, plan)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit Plan JSON ($schema gemba.reconcile.v1)")
	cmd.Flags().StringVar(&label, "label", "", "bd label to query (default: spec:<dir-name>)")
	cmd.Flags().StringVar(&bdStub, "bd-stub", "", "path to a JSON file used in place of `bd list` (tests)")
	cmd.Flags().StringVar(&lockPath, "lockfile", "", "explicit path to .spec.lock.json (default: sibling of spec.md)")
	return cmd
}

func stubBeads(data []byte) ([]reconcile.Bead, error) {
	var arr []reconcile.Bead
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	var wrap struct {
		Issues []reconcile.Bead `json:"issues"`
	}
	if err := json.Unmarshal(data, &wrap); err == nil {
		return wrap.Issues, nil
	}
	return nil, fmt.Errorf("--bd-stub JSON not recognized (expected array or {\"issues\":[...]})")
}

func renderPlanTable(cmd *cobra.Command, plan *reconcile.Plan) error {
	out := cmd.OutOrStdout()
	if plan.NonceStale {
		fmt.Fprintf(out, "warning: nonce stale (lock=%s spec=%s) — apply will refuse\n",
			plan.LockNonce, plan.Nonce)
	}
	if plan.Empty() {
		fmt.Fprintln(out, "ok: no changes")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tANCHOR\tBEAD\tTYPE\tTITLE\tREASON")
	emit := func(ops []reconcile.Op) {
		for _, op := range ops {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				op.Kind, op.Anchor, op.BeadID, op.Type, op.Title, op.Reason)
		}
	}
	emit(plan.Creates)
	emit(plan.Updates)
	emit(plan.Orphans)
	tw.Flush()
	fmt.Fprintf(out, "summary: %d create, %d update, %d orphan\n",
		len(plan.Creates), len(plan.Updates), len(plan.Orphans))
	return nil
}
