// gm-s47n.5.2 — `gemba session-health` CLI.
//
// Operator-facing diagnostic surface for the SessionHealth snapshot
// (mike2's gm-s47n.5.1 ComputeHealth). For each input session,
// prints the three numbers — context_pressure, concept_drift,
// time_on_task — with advisory threshold flags from spec §4 Layer 4.
//
// Read-only by design (spec): the CLI surfaces signal so the
// operator can decide whether to recycle a session; it never
// auto-kills.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

// SessionHealthInput is the wire shape `gemba session-health`
// consumes. One entry per session under inspection. Optional
// bead_concepts seeds the drift-vector lookup — when omitted, the
// computation falls through to ConceptDrift=0 (treat as "no recent
// drift signal"), still useful for the other two numbers.
type SessionHealthInput struct {
	Sessions     []SessionHealthEntry                               `json:"sessions"`
	BeadConcepts map[core.WorkItemID]map[planner.ConceptTag]float64 `json:"bead_concepts,omitempty"`
	// Now overrides time.Now for deterministic test runs. RFC3339
	// string. Empty → live wall clock.
	Now string `json:"now,omitempty"`
}

// SessionHealthEntry is one (Session, Profile) pair. Profile is
// optional: a session that's still booting may have no profile
// row yet — TimeOnTask is meaningful from the session alone.
type SessionHealthEntry struct {
	Session *core.Session           `json:"session"`
	Profile *planner.SessionProfile `json:"profile,omitempty"`
}

// SessionHealthOut is the stable --json envelope. One row per
// input session, in the same order, with thresholds rendered as
// strings so consumers don't reimplement the rule table.
type SessionHealthOut struct {
	Rows []SessionHealthRow `json:"rows"`
}

type SessionHealthRow struct {
	SessionID         string  `json:"session_id"`
	AgentID           string  `json:"agent_id,omitempty"`
	ContextPressure   float64 `json:"context_pressure"`
	ConceptDrift      float64 `json:"concept_drift"`
	TimeOnTaskSeconds float64 `json:"time_on_task_seconds"`
	// PressureLevel and DriftLevel are "ok" | "warn" | "recycle"
	// per spec §4 Layer 4. The CLI carries the labels so a future
	// machine consumer doesn't need to duplicate the threshold
	// table.
	PressureLevel string `json:"pressure_level"`
	DriftLevel    string `json:"drift_level"`
}

// staticConceptLookup adapts a literal map → BeadConceptLookup so
// the CLI can drive ComputeHealth without wiring a real backend.
// Per-bead concept maps come from the input envelope's
// bead_concepts field; missing ids return (nil, nil) — that's the
// "no concepts known" path ComputeHealth tolerates.
type staticConceptLookup struct {
	byBead map[core.WorkItemID]map[planner.ConceptTag]float64
}

func (s staticConceptLookup) BeadConcepts(_ context.Context, id core.WorkItemID) (map[planner.ConceptTag]float64, error) {
	return s.byBead[id], nil
}

func newSessionHealthCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "session-health",
		Short: "Print SessionHealth snapshots for the given sessions",
		Long: `Read a SessionHealthInput JSON envelope from stdin (or --file)
and print the per-session SessionHealth snapshot: context_pressure,
concept_drift, time_on_task. Advisory threshold labels match spec
§4 Layer 4:

  context_pressure > 0.8 → recycle
  context_pressure > 0.6 → warn
  concept_drift    > 0.7 → recycle
  concept_drift    > 0.5 → warn

The CLI is read-only — it surfaces signal so the operator can
decide whether to ` + "`gt handoff`" + ` a session before its next bead.
It never auto-kills.

bead_concepts in the input envelope is optional; supply it when
you want concept_drift to be non-zero (the lookup feeds
ConceptDrift's last-N vector).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := readSessionHealthInput(cmd)
			if err != nil {
				return err
			}
			return runSessionHealth(cmd, in, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read SessionHealthInput from this file (default: stdin)")
	return cmd
}

func readSessionHealthInput(cmd *cobra.Command) (*SessionHealthInput, error) {
	path, _ := cmd.Flags().GetString("file")
	rd := cmd.InOrStdin()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		rd = f
	}
	var in SessionHealthInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode session-health input: %w", err)
	}
	return &in, nil
}

func runSessionHealth(cmd *cobra.Command, in *SessionHealthInput, asJSON bool) error {
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

	rows := make([]SessionHealthRow, 0, len(in.Sessions))
	for _, e := range in.Sessions {
		health, err := planner.ComputeHealth(ctx, e.Session, e.Profile, lookup, now)
		if err != nil {
			return fmt.Errorf("compute health: %w", err)
		}
		row := SessionHealthRow{}
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
		rows = append(rows, row)
	}

	// Stable order: by session id so equal inputs always render
	// the same way.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].SessionID < rows[j].SessionID })

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(SessionHealthOut{Rows: rows})
	}

	fmt.Fprintln(out, "session              agent           pressure   drift     time-on-task")
	for _, r := range rows {
		fmt.Fprintf(out, "%-20s %-15s %5.2f %-7s %5.2f %-7s %s\n",
			r.SessionID,
			r.AgentID,
			r.ContextPressure,
			levelTag(r.PressureLevel),
			r.ConceptDrift,
			levelTag(r.DriftLevel),
			formatDuration(time.Duration(r.TimeOnTaskSeconds*float64(time.Second))),
		)
	}
	return nil
}

func pressureLevel(p float64) string {
	switch {
	case math.IsNaN(p):
		return "ok"
	case p > 0.8:
		return "recycle"
	case p > 0.6:
		return "warn"
	default:
		return "ok"
	}
}

func driftLevel(d float64) string {
	switch {
	case math.IsNaN(d):
		return "ok"
	case d > 0.7:
		return "recycle"
	case d > 0.5:
		return "warn"
	default:
		return "ok"
	}
}

func levelTag(level string) string {
	switch level {
	case "warn":
		return "(warn)"
	case "recycle":
		return "(RECYCLE)"
	default:
		return ""
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
