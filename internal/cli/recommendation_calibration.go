// gm-v5z2.8a — `gemba recommendation-calibration` CLI.
//
// Operator-facing diagnostic for the recommendation calibration
// loop (work-planning.md §7.5). Reads a JSON envelope of {samples,
// thresholds?} and prints either a per-signal text report or the
// Suggestion JSON the operator-review queue (gm-s47n.7.3) will
// consume once wired in.
//
// Offline only at MVP — sample collection from the live coach
// pick path lands in a follow-up bead. Today the operator pipes a
// curated sample set through stdin to see which signals the
// aggregator would flag.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner/reccalibration"
)

// RecommendationCalibrationInput is the wire shape `gemba
// recommendation-calibration` accepts on stdin or --file.
type RecommendationCalibrationInput struct {
	Samples    []RecommendationCalibrationSample `json:"samples"`
	Thresholds *reccalibration.Thresholds        `json:"thresholds,omitempty"`
}

// RecommendationCalibrationSample is one coach-mode pick row.
// Mirrors reccalibration.Sample but uses ISO-8601 strings for At
// to keep the wire format stable. Components are accepted as the
// inner Components struct directly — no separate wire type since
// it's already JSON-tag-clean.
type RecommendationCalibrationSample struct {
	BeadID                core.WorkItemID            `json:"bead_id,omitempty"`
	At                    string                     `json:"at,omitempty"`
	Mode                  string                     `json:"mode"`
	SessionID             string                     `json:"session_id,omitempty"`
	RecommendedTopBead    core.WorkItemID            `json:"recommended_top_bead"`
	RecommendedTopScore   float64                    `json:"recommended_top_score"`
	PickedBead            core.WorkItemID            `json:"picked_bead"`
	PickedScore           float64                    `json:"picked_score"`
	PickedRank            int                        `json:"picked_rank"`
	IntentSet             bool                       `json:"intent_set,omitempty"`
	OperatorReason        string                     `json:"operator_reason,omitempty"`
	RecommendedComponents *reccalibration.Components `json:"recommended_components,omitempty"`
	PickedComponents      *reccalibration.Components `json:"picked_components,omitempty"`
}

func newRecommendationCalibrationCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "recommendation-calibration",
		Short: "Aggregate coach-mode picks and surface systematic recommendation drift (gm-v5z2.8a)",
		Long: fmt.Sprintf(`Read a RecommendationCalibrationInput JSON envelope from stdin
(or --file) and print the per-signal calibration findings plus
the resolved thresholds. Auto-mode rows are silently filtered;
only coach-mode samples participate in the aggregate.

The aggregator surfaces four systematic patterns:

  override_top_3              operator picks outside the planner's
                              top-3 with a meaningful score delta;
                              the score mix is off

  systematic_intent_miss      when intent was set, operator
                              consistently picks outside top-3;
                              intent demotion factor is too lenient

  systematic_leverage_miss    operator prefers beads with higher
                              blocks-weight; the leverage weight
                              is under-tuned

  systematic_runway_overtrust operator overrides the runway gate's
                              demotion frequently; the runway
                              estimator is too pessimistic

A floor of %d coach-mode samples must be cleared before any flag
can flip — calibration never fires on noise.`, reccalibration.DefaultThresholds.MinSamples),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecommendationCalibration(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read RecommendationCalibrationInput from this file (default: stdin)")
	return cmd
}

func runRecommendationCalibration(cmd *cobra.Command, asJSON bool) error {
	in, err := readRecommendationCalibrationInput(cmd)
	if err != nil {
		return err
	}
	samples := make([]reccalibration.Sample, 0, len(in.Samples))
	for _, s := range in.Samples {
		at, perr := parseSampleTime(s.At)
		if perr != nil {
			return fmt.Errorf("sample bead=%s: %w", s.BeadID, perr)
		}
		samples = append(samples, reccalibration.Sample{
			BeadID:                s.BeadID,
			At:                    at,
			Mode:                  s.Mode,
			SessionID:             s.SessionID,
			RecommendedTopBead:    s.RecommendedTopBead,
			RecommendedTopScore:   s.RecommendedTopScore,
			PickedBead:            s.PickedBead,
			PickedScore:           s.PickedScore,
			PickedRank:            s.PickedRank,
			IntentSet:             s.IntentSet,
			OperatorReason:        s.OperatorReason,
			RecommendedComponents: s.RecommendedComponents,
			PickedComponents:      s.PickedComponents,
		})
	}

	suggestion := reccalibration.Aggregate(samples, in.Thresholds)

	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(suggestion)
	}
	return printRecommendationCalibrationText(out, suggestion)
}

func readRecommendationCalibrationInput(cmd *cobra.Command) (*RecommendationCalibrationInput, error) {
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
	var in RecommendationCalibrationInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode recommendation-calibration input: %w", err)
	}
	return &in, nil
}

func parseSampleTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid 'at' (want RFC3339): %w", err)
	}
	return t, nil
}

func printRecommendationCalibrationText(out io.Writer, s reccalibration.Suggestion) error {
	f := s.Findings
	fmt.Fprintf(out, "samples            : %d coach-mode\n", f.SampleCount)
	fmt.Fprintf(out, "overrides          : %d (%.0f%%)\n", f.OverrideCount, f.OverrideRate*100)
	fmt.Fprintf(out, "mean score Δ       : %.2f\n", f.MeanScoreDelta)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "signal                       rate   flagged")
	fmt.Fprintf(out, "%-28s %4.0f%%   %s\n", "override_top_3", f.OverrideTop3Rate*100, flagMark(f.OverrideTop3))
	fmt.Fprintf(out, "%-28s %4.0f%%   %s   (over %d intent samples)\n",
		"systematic_intent_miss", f.IntentMissRate*100, flagMark(f.SystematicIntentMiss), f.IntentSampleCount)
	fmt.Fprintf(out, "%-28s %4.0f%%   %s\n", "systematic_leverage_miss", f.LeverageMissRate*100, flagMark(f.SystematicLeverageMiss))
	fmt.Fprintf(out, "%-28s %4.0f%%   %s\n", "systematic_runway_overtrust", f.RunwayOvertrustRate*100, flagMark(f.SystematicRunwayOvertrust))

	if len(s.Notes) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "notes:")
		for _, n := range s.Notes {
			fmt.Fprintf(out, "  - %s\n", n)
		}
	}
	if !f.AnyFlag() && f.SampleCount >= s.Thresholds.MinSamples {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "no systematic recommendation drift — score mix looks well-tuned for this sample set")
	}
	return nil
}

func flagMark(b bool) string {
	if b {
		return "FLAG"
	}
	return "ok"
}
