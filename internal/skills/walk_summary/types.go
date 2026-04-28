package walk_summary

import (
	"time"

	"github.com/MikeBengtson/gemba/internal/walk"
)

// ID is the canonical skill identifier referenced from the
// Documentarian persona's `skills` list.
const ID = "walk_summary"

// Input is the pure-function input for Generate. Walk is required;
// Now is used for the duration / "ended_at fallback" calculation
// when Walk.EndedAt is nil (a defensive case — End normally sets
// EndedAt before this skill fires). PathHint optionally overrides
// the derived docs/walks/<date>-<slug>.md path so the operator-
// review flow can dry-run a write into a scratch location.
type Input struct {
	Walk     walk.Walk
	Now      time.Time
	PathHint string
}

// Output is the result of Generate. RelativePath is the path the
// applier writes under the configured root (always uses forward
// slashes, no leading slash). Markdown is the full file body
// including the trailing newline.
type Output struct {
	RelativePath string
	Markdown     string
}

// decisionCounts is the per-kind tally produced by countDecisions.
// Exposed as a value type (not exported) so the templating helpers
// in summary.go can pass it through without leaking the shape to
// callers.
type decisionCounts struct {
	Ratify  int
	Modify  int
	Reject  int
	Defer   int
	Handoff int
}

// total returns the sum of all five buckets.
func (d decisionCounts) total() int {
	return d.Ratify + d.Modify + d.Reject + d.Defer + d.Handoff
}

// agendaCounts aggregates AgendaItem rows: by status (resolved vs.
// carried-forward) and by source kind. The seven-source layout
// matches walk.AgendaSourceKind exactly so a future kind addition
// shows up as an unmapped bucket here and prompts an update.
type agendaCounts struct {
	Total          int
	Resolved       int
	CarriedForward int // queued or deferred
	Dismissed      int
	BySource       map[walk.AgendaSourceKind]int
}
