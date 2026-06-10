package enrichment

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// BeadSource yields the beads a backfill should consider. The
// interface stays small (one method) so production callers can wrap
// the bd CLI (see [BdJSONSource]) and tests pass a deterministic
// in-memory list.
//
// Iter calls yield for every bead in source order. Returning false
// from yield stops iteration cleanly. Errors during iteration
// surface to the Backfill caller; partial progress already saved
// before the error stays saved.
type BeadSource interface {
	Iter(ctx context.Context, yield func(BeadInput) bool) error
}

// BackfillOpts tunes the runner.
type BackfillOpts struct {
	// SkipExisting, when true, leaves beads alone whose Store.Load
	// returns anything other than ErrNotFound. Operator-pinned
	// enrichment never gets clobbered by a re-run; explicit
	// extraction-then-save (a fresh `gemba bead extract --merge`)
	// stays the way to refresh.
	SkipExisting bool

	// DryRun mirrors the `gemba bead extract --dry-run` semantic:
	// the runner walks every bead and produces the report, but
	// nothing is persisted.
	DryRun bool

	// Limit caps the number of beads considered. 0 → no cap.
	// Useful for a "first-N smoke" run before unleashing on the
	// whole workspace.
	Limit int

	// FilterRegex restricts the loop to bead ids whose canonical
	// form matches. Empty → no filter. Compiled by the runner so
	// callers can pass user-supplied strings directly.
	FilterRegex string
}

// BackfillReport summarises the run for operator-facing output.
// Errors are collected per-bead so a single misbehaving entry
// doesn't abort the whole loop — the report surfaces what happened
// and the operator can re-target with --filter.
type BackfillReport struct {
	Considered int
	Extracted  int
	Skipped    int
	Errors     []BackfillError
}

// BackfillError describes one bead that failed extraction or save.
// The bead id + the underlying error are kept so the operator can
// drill in.
type BackfillError struct {
	BeadID string
	Err    error
}

func (e BackfillError) Error() string {
	return e.BeadID + ": " + e.Err.Error()
}

// Backfill walks src, runs ext over each bead, and persists the
// result through store. Pure with respect to its inputs (same
// source + same extractor → same report).
//
// Errors from individual beads land in [BackfillReport.Errors]
// rather than aborting the run; only a context cancellation or a
// regex compile failure stops the loop. The returned error is
// non-nil only for those whole-run-fatal cases.
func Backfill(ctx context.Context, src BeadSource, ext Extractor, store Store, opts BackfillOpts) (BackfillReport, error) {
	if src == nil {
		return BackfillReport{}, errors.New("enrichment: Backfill requires a BeadSource")
	}
	if ext == nil {
		ext = NoopExtractor{}
	}
	if store == nil {
		return BackfillReport{}, errors.New("enrichment: Backfill requires a Store")
	}

	var filter *regexp.Regexp
	if strings.TrimSpace(opts.FilterRegex) != "" {
		re, err := regexp.Compile(opts.FilterRegex)
		if err != nil {
			return BackfillReport{}, fmt.Errorf("enrichment: Backfill filter regex: %w", err)
		}
		filter = re
	}

	report := BackfillReport{}
	processed := 0 // filter-passing beads we actually attempted
	err := src.Iter(ctx, func(in BeadInput) bool {
		if ctx.Err() != nil {
			return false
		}
		report.Considered++

		// Filter first — non-matching beads are reported as skipped
		// but don't count against Limit, so `--filter ^gm-s47n
		// --limit 5` reliably attempts the first five gm-s47n
		// beads instead of bailing after five total beads of any id.
		if filter != nil && !filter.MatchString(in.BeadID) {
			report.Skipped++
			return true
		}

		if opts.Limit > 0 && processed >= opts.Limit {
			return false
		}
		processed++

		if opts.SkipExisting {
			existing, loadErr := store.Load(ctx, in.BeadID)
			if loadErr == nil && !existing.IsZero() {
				report.Skipped++
				return true
			}
			// ErrNotFound or empty existing — extract.
		}

		out, err := ext.Extract(ctx, in)
		if err != nil {
			report.Errors = append(report.Errors, BackfillError{BeadID: in.BeadID, Err: err})
			return true
		}
		if out.IsZero() {
			// Extractor produced nothing useful — nothing to save.
			// Counted as considered-but-not-extracted; the report's
			// Extracted - Skipped split makes the empty case visible.
			return true
		}
		if opts.DryRun {
			report.Extracted++
			return true
		}
		out.BeadID = in.BeadID
		// Always stamp SourceBackfill — the extractor's own Source
		// describes its method (heuristic vs LLM), but inside the
		// backfill loop the more useful provenance is "produced by
		// the backfill" so the operator can grep `bead show`
		// output for backfilled vs operator-pinned vs interactively-
		// extracted entries.
		out.Source = SourceBackfill
		if err := store.Save(ctx, out); err != nil {
			report.Errors = append(report.Errors, BackfillError{BeadID: in.BeadID, Err: err})
			return true
		}
		report.Extracted++
		return true
	})
	if err != nil {
		return report, fmt.Errorf("enrichment: Backfill iter: %w", err)
	}
	return report, nil
}
