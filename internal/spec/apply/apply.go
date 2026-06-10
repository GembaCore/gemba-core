package apply

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
	"github.com/GembaCore/gemba-core/internal/spec/snapshot"
)

// ErrNonceStale is returned by Apply when the plan's nonce no longer
// matches the lockfile. Callers must re-run reconcile.Diff against the
// freshly-saved spec before retrying.
var ErrNonceStale = errors.New("apply: plan nonce stale; refusing to mutate")

// Options controls Apply behaviour.
type Options struct {
	// DryRun, when true, walks the plan and produces a Receipt without
	// invoking the Mutator.
	DryRun bool
	// Prune, when true, processes Plan.Orphans (close beads). Otherwise
	// orphans land in Receipt.SkippedOrphans untouched.
	Prune bool
	// Mutator is the BeadMutator that executes side effects. Required
	// unless DryRun.
	Mutator BeadMutator
	// Auditor receives spec.apply.* events. Failures here are swallowed.
	Auditor Auditor
	// Logger receives slog records. Defaults to slog.Default().
	Logger *slog.Logger
	// SnapshotPath, when non-empty, is the spec path to snapshot before
	// any mutation. Snapshot failure aborts Apply (the pre-apply safety
	// net is mandatory when requested).
	SnapshotPath string
}

// Result is one applied (or skipped) op recorded on the Receipt.
type Result struct {
	Anchor  string            `json:"anchor"`
	BeadID  string            `json:"bead_id,omitempty"`
	Kind    string            `json:"kind"`
	Title   string            `json:"title,omitempty"`
	Note    string            `json:"note,omitempty"`
	Reverse map[string]string `json:"-"` // populated for Updated entries — used by Rollback
}

// Error is one failed op.
type Error struct {
	Anchor string `json:"anchor"`
	BeadID string `json:"bead_id,omitempty"`
	Kind   string `json:"kind"`
	Err    string `json:"error"`
}

// Receipt is the apply outcome. The CLI prints it (human or JSON), and
// the Mutator's Rollback consumes it on failure.
type Receipt struct {
	Nonce          string   `json:"nonce"`
	SnapshotPath   string   `json:"snapshot_path,omitempty"`
	DryRun         bool     `json:"dry_run,omitempty"`
	Created        []Result `json:"created,omitempty"`
	Updated        []Result `json:"updated,omitempty"`
	Pruned         []Result `json:"pruned,omitempty"`
	SkippedOrphans []Result `json:"skipped_orphans,omitempty"`
	Errors         []Error  `json:"errors,omitempty"`
	RolledBack     bool     `json:"rolled_back,omitempty"`
}

// Apply executes plan against the BeadMutator, atomically gating on the
// spec nonce.
//
// Ordering:
//  1. Refuse if plan.NonceStale.
//  2. Take a snapshot (if SnapshotPath is set).
//  3. Process Creates, then Updates, then Orphans (when Prune).
//  4. On any mid-apply error, invoke Mutator.Rollback and return.
//  5. On full success, mutate `lock` in place so the caller can save it.
//
// The lockfile is only mutated when every op succeeds — atomicity at the
// spec-reconcile boundary.
func Apply(ctx context.Context, plan *reconcile.Plan, lock *lockfile.Lock, opts Options) (*Receipt, error) {
	if plan == nil {
		return nil, errors.New("apply: nil plan")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	receipt := &Receipt{Nonce: plan.Nonce, DryRun: opts.DryRun}

	if plan.NonceStale {
		emit(ctx, opts.Auditor, "spec.apply.failed", map[string]any{
			"spec_id": plan.SpecID, "nonce": plan.Nonce, "reason": "nonce_stale",
		})
		return receipt, ErrNonceStale
	}

	if !opts.DryRun && opts.Mutator == nil {
		return receipt, errors.New("apply: Mutator required for non-DryRun")
	}

	if opts.SnapshotPath != "" {
		snap, err := snapshot.Take(opts.SnapshotPath)
		if err != nil {
			return receipt, fmt.Errorf("apply: snapshot: %w", err)
		}
		receipt.SnapshotPath = snap
	}

	emit(ctx, opts.Auditor, "spec.apply.start", map[string]any{
		"spec_id": plan.SpecID,
		"nonce":   plan.Nonce,
		"creates": len(plan.Creates),
		"updates": len(plan.Updates),
		"orphans": len(plan.Orphans),
		"dry_run": opts.DryRun,
		"prune":   opts.Prune,
	})

	// Cache lockfile mapping by anchor for in-place mutation on success.
	mappingIdx := map[string]int{}
	if lock != nil {
		for i, m := range lock.Mappings {
			mappingIdx[m.Anchor] = i
		}
	}

	// rollback helper
	rollback := func(cause error) {
		if opts.DryRun || opts.Mutator == nil {
			return
		}
		if err := opts.Mutator.Rollback(ctx, receipt); err != nil {
			logger.Error("apply: rollback returned error", "err", err)
		}
		receipt.RolledBack = true
		emit(ctx, opts.Auditor, "spec.apply.failed", map[string]any{
			"spec_id": plan.SpecID, "nonce": plan.Nonce, "error": cause.Error(),
		})
	}

	// --- creates ---
	for _, op := range plan.Creates {
		var id string
		if !opts.DryRun {
			var err error
			id, err = opts.Mutator.Create(ctx, op)
			if err != nil {
				receipt.Errors = append(receipt.Errors, Error{Anchor: op.Anchor, Kind: "create", Err: err.Error()})
				rollback(err)
				return receipt, err
			}
		}
		res := Result{Anchor: op.Anchor, BeadID: id, Kind: "create", Title: op.Title}
		receipt.Created = append(receipt.Created, res)
		emit(ctx, opts.Auditor, "spec.apply.op", map[string]any{
			"kind": "create", "anchor": op.Anchor, "bead_id": id, "spec_id": plan.SpecID, "nonce": plan.Nonce,
		})

		// Stage lockfile mapping (only commit on full success below).
		if lock != nil {
			lock.Mappings = append(lock.Mappings, lockfile.Mapping{
				Anchor: op.Anchor, BeadID: id, ContentHash: op.ContentHash,
			})
			mappingIdx[op.Anchor] = len(lock.Mappings) - 1
		}
	}

	// --- updates ---
	for _, op := range plan.Updates {
		reverse := map[string]string{}
		for k, fc := range op.FieldDiffs {
			if fc.Informational {
				continue
			}
			reverse[k] = fc.From
		}
		if !opts.DryRun {
			if err := opts.Mutator.Update(ctx, op); err != nil {
				receipt.Errors = append(receipt.Errors, Error{Anchor: op.Anchor, BeadID: op.BeadID, Kind: "update", Err: err.Error()})
				rollback(err)
				return receipt, err
			}
		}
		receipt.Updated = append(receipt.Updated, Result{
			Anchor: op.Anchor, BeadID: op.BeadID, Kind: "update", Title: op.Title, Reverse: reverse,
		})
		emit(ctx, opts.Auditor, "spec.apply.op", map[string]any{
			"kind": "update", "anchor": op.Anchor, "bead_id": op.BeadID, "spec_id": plan.SpecID, "nonce": plan.Nonce,
		})
		if lock != nil {
			if i, ok := mappingIdx[op.Anchor]; ok {
				lock.Mappings[i].ContentHash = op.ContentHash
			}
		}
	}

	// --- orphans ---
	for _, op := range plan.Orphans {
		if !opts.Prune {
			receipt.SkippedOrphans = append(receipt.SkippedOrphans, Result{
				Anchor: op.Anchor, BeadID: op.BeadID, Kind: "orphan", Title: op.Title,
				Note: "prune not requested",
			})
			continue
		}
		if !opts.DryRun {
			reason := "spec-prune: anchor no longer in spec (nonce=" + plan.Nonce + ")"
			if err := opts.Mutator.Close(ctx, op, reason); err != nil {
				receipt.Errors = append(receipt.Errors, Error{Anchor: op.Anchor, BeadID: op.BeadID, Kind: "close", Err: err.Error()})
				rollback(err)
				return receipt, err
			}
		}
		receipt.Pruned = append(receipt.Pruned, Result{
			Anchor: op.Anchor, BeadID: op.BeadID, Kind: "prune", Title: op.Title,
		})
		emit(ctx, opts.Auditor, "spec.apply.op", map[string]any{
			"kind": "prune", "anchor": op.Anchor, "bead_id": op.BeadID, "spec_id": plan.SpecID, "nonce": plan.Nonce,
		})
		if lock != nil {
			if i, ok := mappingIdx[op.Anchor]; ok {
				// remove
				lock.Mappings = append(lock.Mappings[:i], lock.Mappings[i+1:]...)
				// rebuild index
				mappingIdx = map[string]int{}
				for j, m := range lock.Mappings {
					mappingIdx[m.Anchor] = j
				}
			}
		}
	}

	emit(ctx, opts.Auditor, "spec.apply.complete", map[string]any{
		"spec_id":         plan.SpecID,
		"nonce":           plan.Nonce,
		"created":         len(receipt.Created),
		"updated":         len(receipt.Updated),
		"pruned":          len(receipt.Pruned),
		"skipped_orphans": len(receipt.SkippedOrphans),
	})
	return receipt, nil
}

// emit forwards an event to the Auditor without ever blocking apply on
// the audit channel.
func emit(ctx context.Context, a Auditor, event string, payload map[string]any) {
	if a == nil {
		return
	}
	defer func() { _ = recover() }()
	a.Emit(ctx, event, payload)
}
