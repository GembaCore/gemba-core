// Package core: see doc.go for the overview.
package core

import (
	"encoding/json"
	"fmt"
)

// StateCategory is the adaptor-agnostic normalization of a work item's
// lifecycle position. Every adaptor maps its native statuses
// ("open", "in_progress", "done", "To Do", "In Review", ...) onto one of
// these five buckets so the UI can render a consistent Kanban board
// without knowing Beads-specific or Jira-specific vocabulary.
//
// The native status string is preserved on WorkItem.Status; StateCategory
// is strictly a secondary, normalized view of it.
type StateCategory string

const (
	// StateBacklog — formally tracked but not yet triaged into a sprint or
	// an active queue. Includes deferred, icebox, parking-lot style states.
	StateBacklog StateCategory = "backlog"

	// StateUnstarted — triaged and ready to pick up, but no one is working
	// on it yet. "open", "ready", "To Do".
	StateUnstarted StateCategory = "unstarted"

	// StateStarted — actively in progress. "in_progress", "In Review",
	// "Doing", "hooked", "pinned".
	StateStarted StateCategory = "started"

	// StateCompleted — finished successfully. "closed", "done", "merged".
	StateCompleted StateCategory = "completed"

	// StateCanceled — terminally closed without completion. "won't fix",
	// "duplicate", "canceled", "rejected".
	StateCanceled StateCategory = "canceled"
)

// validStates is the authoritative set used by Valid and UnmarshalJSON.
// Kept as a map for O(1) membership checks.
var validStates = map[StateCategory]struct{}{
	StateBacklog:   {},
	StateUnstarted: {},
	StateStarted:   {},
	StateCompleted: {},
	StateCanceled:  {},
}

// Valid reports whether s is one of the five canonical categories.
func (s StateCategory) Valid() bool {
	_, ok := validStates[s]
	return ok
}

// String satisfies fmt.Stringer and always returns the lowercase token.
func (s StateCategory) String() string { return string(s) }

// UnmarshalJSON rejects unknown category strings at decode time so that
// bad adaptor output surfaces as a parse error instead of silently
// painting the UI with an invalid state.
func (s *StateCategory) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	candidate := StateCategory(raw)
	if !candidate.Valid() {
		return fmt.Errorf("core: unknown StateCategory %q", raw)
	}
	*s = candidate
	return nil
}
