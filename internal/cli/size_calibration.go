// gm-v5z2.8b — `gemba size-calibration` CLI.
//
// Operator-facing diagnostic for the bead-size calibration loop
// (work-planning.md §7.6). Reads a JSON envelope of {samples: [
// {bead_id, predicted, actual_seconds, author?}, ...]} and prints
// the per-bucket drift report plus the suggested
// SizeHeuristicThresholds when any bucket exceeds the drift
// trigger.
//
// Offline only at MVP — sample collection from the live retro
// pipeline + the operator-review queue lands in a follow-up bead.
// Today the operator pipes a curated sample set through stdin to
// see what the calibrator would suggest.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/enrichment"
	"github.com/GembaCore/gemba-core/internal/planner/sizecalibration"
)

// SizeCalibrationInput is the wire shape `gemba size-calibration`
// accepts on stdin. Samples carry actual_seconds rather than a
// time.Duration so JSON is unambiguous; the CLI converts at the
// boundary.
type SizeCalibrationInput struct {
	Samples []SizeCalibrationSample `json:"samples"`
	// Targets is optional — when omitted the aggregator uses
	// sizecalibration.DefaultBucketTargets.
	Targets *SizeCalibrationTargets `json:"targets,omitempty"`
	// CurrentThresholds is optional — when omitted the aggregator
	// uses enrichment.DefaultSizeThresholds.
	CurrentThresholds *enrichment.SizeHeuristicThresholds `json:"current_thresholds,omitempty"`
}

type SizeCalibrationSample struct {
	BeadID        core.WorkItemID          `json:"bead_id,omitempty"`
	Predicted     enrichment.EstimatedSize `json:"predicted"`
	ActualSeconds float64                  `json:"actual_seconds"`
	Author        string                   `json:"author,omitempty"`
}

type SizeCalibrationTargets struct {
	SmallSeconds  float64 `json:"small_seconds"`
	MediumSeconds float64 `json:"medium_seconds"`
	LargeSeconds  float64 `json:"large_seconds"`
}

func newSizeCalibrationCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "size-calibration",
		Short: "Aggregate bead-size samples and suggest threshold shifts (gm-v5z2.8b)",
		Long: `Read a SizeCalibrationInput JSON envelope from stdin (or --file)
and print the per-bucket drift report plus the suggested
SizeHeuristicThresholds when any bucket exceeds the drift trigger.

The aggregator groups samples by predicted size, computes the
median actual duration, and compares against the per-bucket
target. Drift > 2x in either direction (work-planning.md §7.6)
contributes a delta to the bucket boundary above:

  small + high drift  → lower SmallMax  (promote borderline beads to medium)
  small + low drift   → raise SmallMax  (keep borderline beads in small)
  medium + high drift → lower MediumMax (promote to large)
  medium + low drift  → raise MediumMax

Buckets with fewer than ` + fmt.Sprintf("%d", sizecalibration.MinSamplesPerBucket) + ` samples are reported as
"insufficient" and never contribute to a threshold shift.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSizeCalibration(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read SizeCalibrationInput from this file (default: stdin)")
	return cmd
}

func runSizeCalibration(cmd *cobra.Command, asJSON bool) error {
	in, err := readSizeCalibrationInput(cmd)
	if err != nil {
		return err
	}
	samples := make([]sizecalibration.Sample, 0, len(in.Samples))
	for _, s := range in.Samples {
		samples = append(samples, sizecalibration.Sample{
			BeadID:    s.BeadID,
			Predicted: s.Predicted,
			Actual:    time.Duration(s.ActualSeconds * float64(time.Second)),
			Author:    s.Author,
		})
	}
	var targets *sizecalibration.BucketTargets
	if in.Targets != nil {
		targets = &sizecalibration.BucketTargets{
			Small:  time.Duration(in.Targets.SmallSeconds * float64(time.Second)),
			Medium: time.Duration(in.Targets.MediumSeconds * float64(time.Second)),
			Large:  time.Duration(in.Targets.LargeSeconds * float64(time.Second)),
		}
	}
	suggestion := sizecalibration.Aggregate(samples, targets, in.CurrentThresholds)

	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(suggestion)
	}
	return printSizeCalibrationText(out, suggestion)
}

func readSizeCalibrationInput(cmd *cobra.Command) (*SizeCalibrationInput, error) {
	path, _ := cmd.Flags().GetString("file")
	rd := cmd.InOrStdin()
	if path != "" && path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		rd = f
	}
	var in SizeCalibrationInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode size-calibration input: %w", err)
	}
	return &in, nil
}

func printSizeCalibrationText(out io.Writer, s sizecalibration.Suggestion) error {
	fmt.Fprintln(out, "bucket   samples  median(actual)   target    direction      reason")
	for _, r := range s.Reports {
		fmt.Fprintf(out, "%-8s %-8d %-16s %-9s %-14s %s\n",
			string(r.Bucket),
			r.SampleCount,
			formatDurationSec(r.MedianActualSeconds),
			formatDurationSec(r.TargetSeconds),
			r.Direction,
			r.Reason,
		)
	}
	if s.NewThresholds != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "suggested threshold shift:")
		fmt.Fprintf(out, "  current : SmallMax=%d  MediumMax=%d\n", s.Current.SmallMax, s.Current.MediumMax)
		fmt.Fprintf(out, "  new     : SmallMax=%d  MediumMax=%d\n",
			s.NewThresholds.SmallMax, s.NewThresholds.MediumMax)
	} else if s.AnyDrift() {
		fmt.Fprintln(out, "\ndrift detected but only in the large bucket (no boundary above to shift)")
	} else {
		fmt.Fprintln(out, "\nno drift — heuristic looks well-calibrated for this sample set")
	}
	return nil
}

func formatDurationSec(secs float64) string {
	if secs <= 0 {
		return "—"
	}
	d := time.Duration(secs * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24.0)
}
