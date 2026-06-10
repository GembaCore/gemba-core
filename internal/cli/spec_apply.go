package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/apply"
	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/parser"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// newSpecApplyCmd wires `gemba spec apply <spec.md>`.
//
// Workflow:
//  1. Parse spec.md and load sibling .spec.lock.json (or --lockfile).
//  2. List existing beads (real `bd` or --bd-stub for tests).
//  3. Run reconcile.Diff. Refuse if Plan.NonceStale unless --force-nonce.
//  4. Snapshot the spec (unless --no-snapshot).
//  5. Call apply.Apply with a BDMutator (or stub mutator on dry-run).
//  6. On success, atomically save the updated lockfile.
//  7. Emit human or JSON receipt.
func newSpecApplyCmd() *cobra.Command {
	var (
		jsonOut    bool
		label      string
		bdStub     string
		lockPath   string
		dryRun     bool
		prune      bool
		forceNonce bool
		noSnapshot bool
		applyLog   string
	)
	cmd := &cobra.Command{
		Use:   "apply <spec.md>",
		Short: "Apply the reconcile plan to bd (nonce-gated, atomic, rolls back on failure)",
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
			} else {
				lock = &lockfile.Lock{SpecID: spec.Frontmatter.SpecID, Nonce: spec.Frontmatter.Nonce}
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

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			beads, err := lister.List(ctx, lbl)
			if err != nil {
				return fmt.Errorf("list beads: %w", err)
			}

			plan, err := reconcile.Diff(spec, beads, lock)
			if err != nil {
				return err
			}

			if plan.NonceStale && !forceNonce {
				return fmt.Errorf("nonce stale (lock=%s spec=%s); rerun `gemba spec diff` and re-mint, or pass --force-nonce",
					plan.LockNonce, plan.Nonce)
			}
			if forceNonce {
				plan.NonceStale = false
			}

			snapPath := specPath
			if noSnapshot {
				snapPath = ""
			}

			opts := apply.Options{
				DryRun:       dryRun,
				Prune:        prune,
				Logger:       slog.Default(),
				SnapshotPath: snapPath,
			}
			if !dryRun {
				opts.Mutator = &apply.BDMutator{
					Nonce:   plan.Nonce,
					LogPath: applyLog,
				}
			}

			receipt, applyErr := apply.Apply(ctx, plan, lock, opts)

			// Lockfile save only happens on success. On error, the
			// lock object may carry staged mutations — do NOT save it.
			var saveErr error
			if applyErr == nil && !dryRun {
				lock.Nonce = plan.Nonce
				lock.AppliedAt = time.Now().UTC()
				saveErr = lockfile.Save(lp, lock)
			}

			if jsonOut {
				data, _ := json.MarshalIndent(receipt, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			} else {
				printReceipt(cmd, receipt)
			}

			if applyErr != nil {
				if errors.Is(applyErr, apply.ErrNonceStale) {
					return applyErr
				}
				return fmt.Errorf("apply failed: %w", applyErr)
			}
			if saveErr != nil {
				return fmt.Errorf("save lockfile: %w", saveErr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the Receipt as JSON")
	cmd.Flags().StringVar(&label, "label", "", "bd label to query (default: spec:<dir-name>)")
	cmd.Flags().StringVar(&bdStub, "bd-stub", "", "path to a JSON file used in place of `bd list` (tests)")
	cmd.Flags().StringVar(&lockPath, "lockfile", "", "explicit path to .spec.lock.json (default: sibling of spec.md)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "walk the plan without invoking bd")
	cmd.Flags().BoolVar(&prune, "prune", false, "close beads orphaned from the spec")
	cmd.Flags().BoolVar(&forceNonce, "force-nonce", false, "bypass nonce-stale refusal (dangerous)")
	cmd.Flags().BoolVar(&noSnapshot, "no-snapshot", false, "skip the pre-apply spec snapshot")
	cmd.Flags().StringVar(&applyLog, "apply-log", "", "path to the (nonce, anchor) sidecar log (default: .gemba/.apply.log.jsonl)")
	return cmd
}

func printReceipt(cmd *cobra.Command, r *apply.Receipt) {
	out := cmd.OutOrStdout()
	if r.DryRun {
		fmt.Fprintln(out, "dry-run: no mutations performed")
	}
	if r.SnapshotPath != "" {
		fmt.Fprintln(out, "snapshot:", r.SnapshotPath)
	}
	fmt.Fprintf(out, "nonce: %s\n", r.Nonce)
	fmt.Fprintf(out, "summary: %d created, %d updated, %d pruned, %d skipped-orphans, %d errors\n",
		len(r.Created), len(r.Updated), len(r.Pruned), len(r.SkippedOrphans), len(r.Errors))
	for _, c := range r.Created {
		fmt.Fprintf(out, "  create %s -> %s %s\n", c.Anchor, c.BeadID, c.Title)
	}
	for _, u := range r.Updated {
		fmt.Fprintf(out, "  update %s (%s) %s\n", u.Anchor, u.BeadID, u.Title)
	}
	for _, p := range r.Pruned {
		fmt.Fprintf(out, "  prune  %s (%s)\n", p.Anchor, p.BeadID)
	}
	for _, s := range r.SkippedOrphans {
		fmt.Fprintf(out, "  skip   %s (%s) — %s\n", s.Anchor, s.BeadID, s.Note)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(out, "  error  %s %s: %s\n", e.Kind, e.Anchor, e.Err)
	}
	if r.RolledBack {
		fmt.Fprintln(out, "rolled back: prior successful ops reversed")
	}
}
