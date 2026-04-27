// Intent struct + Match (gm-v5z2.3).

package intent

import (
	"errors"
	"regexp"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// DefaultDemotionFactor is the multiplier applied to a candidate
// score when the candidate falls outside the operator-pinned intent.
// 0.4 means a 0.8 in-intent score beats a 1.0 out-of-intent score.
// Operators can override per intent.
const DefaultDemotionFactor = 0.4

// Intent is the operator's session-pinned focus directive. Three
// orthogonal restrictors AND together; at least one MUST be set
// for a valid intent. An empty Intent (no restrictors) is the
// "cleared" state.
//
// Stored as one row per session in the session_intents dolt table.
// Cleared on session end by the lifecycle hooks (gm-v5z2.7's
// integration); intents do NOT survive across sessions.
type Intent struct {
	SessionID string `json:"session_id"`

	EpicID      string `json:"epic_id,omitempty"`
	Label       string `json:"label,omitempty"`
	BeadIDRegex string `json:"bead_id_regex,omitempty"`

	// Rationale is freeform "why this focus" text the operator
	// supplies. Surfaces in the audit log and the coach UI.
	Rationale string `json:"rationale,omitempty"`

	// DemotionFactor is the multiplier applied to out-of-intent
	// candidate scores. Zero or negative defaults to
	// DefaultDemotionFactor on read.
	DemotionFactor float64 `json:"demotion_factor,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EffectiveDemotionFactor returns the demotion multiplier with the
// default applied. Reads should always go through this rather than
// touching DemotionFactor directly so a zero value (the default
// JSON unmarshal landing) doesn't silently zero out scores.
func (i Intent) EffectiveDemotionFactor() float64 {
	if i.DemotionFactor <= 0 {
		return DefaultDemotionFactor
	}
	return i.DemotionFactor
}

// IsZero reports whether the intent carries no restrictors. Treat
// it as "no focus directive set" — Match returns true for every
// candidate, and the selection layer skips its demotion pass.
func (i Intent) IsZero() bool {
	return i.EpicID == "" && i.Label == "" && i.BeadIDRegex == ""
}

// Validate enforces the "at least one restrictor" rule and that
// BeadIDRegex parses. Returns nil on the empty (cleared) intent —
// callers that want to forbid empties check IsZero themselves.
func (i Intent) Validate() error {
	if i.IsZero() {
		return nil
	}
	if i.BeadIDRegex != "" {
		if _, err := regexp.Compile(i.BeadIDRegex); err != nil {
			return errors.New("intent: bead_id_regex does not compile: " + err.Error())
		}
	}
	if i.DemotionFactor < 0 {
		return errors.New("intent: demotion_factor must be non-negative")
	}
	if i.DemotionFactor > 1 {
		return errors.New("intent: demotion_factor must be ≤ 1 (a value > 1 would PROMOTE out-of-intent work)")
	}
	return nil
}

// Candidate is the minimal projection of a WorkItem that Match
// needs. Keeping it narrow lets the selection layer feed in a
// pre-computed view of the candidate set without hauling the full
// core.WorkItem through every per-bead loop.
type Candidate struct {
	BeadID core.WorkItemID
	EpicID core.WorkItemID
	Labels []string
}

// Match reports whether c is in-intent. Empty intent matches
// everything (the cleared state). Multiple restrictors AND together.
//
// EpicID match: candidate.EpicID must equal intent.EpicID. Epic
// descendency (Intent says "gm-e3", candidate's epic is "gm-e3.5",
// gm-e3.5 is descendent of gm-e3) is the SELECTION layer's job —
// pre-resolve the epic to its descendant set before calling Match
// so this stays O(1) per candidate.
//
// Label match: candidate.Labels must contain intent.Label.
//
// BeadIDRegex match: regex must compile (validated upstream) and
// match the candidate's BeadID. A regex that fails to compile here
// (it shouldn't — Validate should have caught it) is treated as
// "no match" rather than panicking.
func Match(intent Intent, c Candidate) bool {
	if intent.IsZero() {
		return true
	}
	if intent.EpicID != "" {
		if string(c.EpicID) != intent.EpicID {
			return false
		}
	}
	if intent.Label != "" {
		if !containsString(c.Labels, intent.Label) {
			return false
		}
	}
	if intent.BeadIDRegex != "" {
		re, err := regexp.Compile(intent.BeadIDRegex)
		if err != nil {
			return false
		}
		if !re.MatchString(string(c.BeadID)) {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// AuditAction names the kind of change recorded in
// session_intent_audit. Used both as the database column value and
// the CLI's --json output discriminator.
type AuditAction string

const (
	AuditSet   AuditAction = "set"
	AuditClear AuditAction = "clear"
)

// AuditEntry is one row in session_intent_audit. Captures the full
// before/after intent state alongside the wall-clock timestamp so a
// retrospective can attribute "the session was focused on gm-e3
// when this bead landed" without re-reading the live row.
type AuditEntry struct {
	ID        int64       `json:"id"`
	SessionID string      `json:"session_id"`
	Action    AuditAction `json:"action"`
	Prior     *Intent     `json:"prior,omitempty"`
	Next      *Intent     `json:"next,omitempty"`
	At        time.Time   `json:"at"`
	// Actor names the operator (or service) that triggered the
	// change. Free-text; the CLI stamps "cli:<user>" when called
	// via gemba session focus, an SPA mutation lands "spa:<user>".
	Actor string `json:"actor,omitempty"`
}
