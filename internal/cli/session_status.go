// gm-v5z2.4 — `gemba session-status` CLI.
//
// Composes session-health + runway estimate into one operator-
// facing snapshot. Read-only (same posture as session-health):
// surfaces signal so the operator can decide when to recycle a
// session and what kind of bead to feed it next.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/runway"
)

// SessionStatusInput is the wire shape `gemba session-status`
// consumes. Mirrors SessionHealthInput plus a per-session
// calibration override so the operator can replay
// promised-vs-actual data without rebuilding the input from
// scratch.
type SessionStatusInput struct {
	Sessions     []SessionStatusEntry                               `json:"sessions"`
	BeadConcepts map[core.WorkItemID]map[planner.ConceptTag]float64 `json:"bead_concepts,omitempty"`
	// Now overrides time.Now for deterministic test runs.
	Now string `json:"now,omitempty"`
}

// SessionStatusEntry is one (Session, Profile, Calibration) tuple.
// Calibration is optional; zero falls through to runway's default.
type SessionStatusEntry struct {
	Session     *core.Session           `json:"session"`
	Profile     *planner.SessionProfile `json:"profile,omitempty"`
	Calibration float64                 `json:"calibration,omitempty"`
}

// SessionStatusOut is the stable --json envelope. One row per
// input session in the same order.
type SessionStatusOut struct {
	Rows []SessionStatusRow `json:"rows"`
}

type SessionStatusRow struct {
	SessionID         string        `json:"session_id"`
	AgentID           string        `json:"agent_id,omitempty"`
	ContextPressure   float64       `json:"context_pressure"`
	ConceptDrift      float64       `json:"concept_drift"`
	TimeOnTaskSeconds float64       `json:"time_on_task_seconds"`
	PressureLevel     string        `json:"pressure_level"`
	DriftLevel        string        `json:"drift_level"`
	Runway            runway.Runway `json:"runway"`
}

func newSessionStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "session-status",
		Short: "Print SessionHealth + runway estimate for the given sessions (gm-v5z2.4)",
		Long: `Read a SessionStatusInput JSON envelope from stdin (or --file)
and print, per session: the three SessionHealth numbers
(context_pressure, concept_drift, time_on_task) AND the runway
estimate (small | medium | large) the planner's selection layer
will compare against bead.estimated_size.

Runway recipe (work-planning §4 Layer 5.1):
  headroom        = 1 - context_pressure
  drift_penalty   = 0.5 * concept_drift
  score           = (headroom - drift_penalty) * calibration
  bucket          = small (<0.30) | medium (<0.65) | large

calibration defaults to 1.0 (on-track); supply the per-session
scalar in the input envelope to model promised-vs-actual cycles.

Read-only by design — surfaces signal so the operator can decide
how to feed each session next; never auto-kills.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := readSessionStatusInput(cmd)
			if err != nil {
				return err
			}
			return runSessionStatus(cmd, in, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read SessionStatusInput from this file (default: stdin)")
	return cmd
}

func readSessionStatusInput(cmd *cobra.Command) (*SessionStatusInput, error) {
	path, _ := cmd.Flags().GetString("file")
	var rd io.Reader
	if path == "" || path == "-" {
		rd = cmd.InOrStdin()
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		rd = f
	}
	var in SessionStatusInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode session-status input: %w", err)
	}
	return &in, nil
}

func runSessionStatus(cmd *cobra.Command, in *SessionStatusInput, asJSON bool) error {
	out := cmd.OutOrStdout()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now
	if in.Now != "" {
		t, err := time.Parse(time.RFC3339, in.Now)
		if err != nil {
			return fmt.Errorf("parse 'now': %w", err)
		}
		now = func() time.Time { return t }
	}
	var lookup planner.BeadConceptLookup
	if len(in.BeadConcepts) > 0 {
		lookup = staticConceptLookup{byBead: in.BeadConcepts}
	}

	rows := make([]SessionStatusRow, 0, len(in.Sessions))
	for _, e := range in.Sessions {
		health, err := planner.ComputeHealth(ctx, e.Session, e.Profile, lookup, now)
		if err != nil {
			return fmt.Errorf("compute health: %w", err)
		}
		row := SessionStatusRow{}
		if e.Session != nil {
			row.SessionID = e.Session.ID
			row.AgentID = string(e.Session.AgentID)
		}
		if health != nil {
			row.ContextPressure = health.ContextPressure
			row.ConceptDrift = health.ConceptDrift
			row.TimeOnTaskSeconds = health.TimeOnTask.Seconds()
			row.PressureLevel = pressureLevel(health.ContextPressure)
			row.DriftLevel = driftLevel(health.ConceptDrift)
		}
		row.Runway = runway.Estimate(runway.Inputs{
			Health:      health,
			Calibration: e.Calibration,
		})
		rows = append(rows, row)
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(SessionStatusOut{Rows: rows})
	}
	return printSessionStatusText(out, rows)
}

func printSessionStatusText(w io.Writer, rows []SessionStatusRow) error {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no sessions in input")
		return nil
	}
	fmt.Fprintln(w, "session              agent           pressure   drift     ttask    runway")
	for _, r := range rows {
		fmt.Fprintf(w, "%-20s %-15s %5.2f %-7s %5.2f %-7s %4.0fs   %-6s %5.2f\n",
			r.SessionID,
			r.AgentID,
			r.ContextPressure, levelTag(r.PressureLevel),
			r.ConceptDrift, levelTag(r.DriftLevel),
			r.TimeOnTaskSeconds,
			string(r.Runway.Bucket), r.Runway.Score,
		)
	}
	return nil
}
