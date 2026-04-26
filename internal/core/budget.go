// Package core: see doc.go for the overview.
package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// Sprint is a time-boxed container of work that carries an optional
// TokenBudget. A sprint is not required — adaptors without Scrum-shaped
// vocabulary (GitHub Issues, raw beads-only setups) may simply omit
// WorkItem.SprintID. The locked DoD at gm-root / DD-14 keeps Sprint and
// TokenBudget live in v1 with three-tier enforcement.
type Sprint struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	StartsAt  time.Time    `json:"starts_at"`
	EndsAt    time.Time    `json:"ends_at"`
	Goal      string       `json:"goal,omitempty"`
	WorkItems []WorkItemID `json:"work_items,omitempty"`
	Budget    *TokenBudget `json:"budget,omitempty"`
}

// BudgetTier names the three enforcement thresholds a TokenBudget
// carries. They fire in order as consumption grows. "stop" is not a
// hard-block by core; the orchestration adaptor decides what "stop"
// means for its own runtime (pause spawns, refuse new runs, etc).
type BudgetTier string

const (
	// BudgetInform — the soft notice tier. UI surfaces a hint.
	BudgetInform BudgetTier = "inform"
	// BudgetWarn — the louder tier. UI surfaces a warning banner.
	BudgetWarn BudgetTier = "warn"
	// BudgetStop — the blocking tier. Orchestrator should refuse new work.
	BudgetStop BudgetTier = "stop"
)

// TokenBudget is a consumable budget denominated in tokens. Thresholds
// are absolute token counts, not percentages, so adaptors with native
// cost telemetry don't have to round-trip through floats. Must hold
// 0 <= Inform <= Warn <= Stop <= Limit; see Validate.
//
// Used is the current cumulative consumption. Transport/read calls set
// it; the structure carries no accumulator logic itself.
type TokenBudget struct {
	Limit  int64 `json:"limit"`
	Used   int64 `json:"used"`
	Inform int64 `json:"inform"`
	Warn   int64 `json:"warn"`
	Stop   int64 `json:"stop"`
}

// Tier reports which threshold the current Used value has crossed. It
// returns the empty string when Used has not yet reached Inform.
func (b TokenBudget) Tier() BudgetTier {
	switch {
	case b.Used >= b.Stop:
		return BudgetStop
	case b.Used >= b.Warn:
		return BudgetWarn
	case b.Used >= b.Inform:
		return BudgetInform
	default:
		return ""
	}
}

// Remaining returns Limit - Used, clamped at zero.
func (b TokenBudget) Remaining() int64 {
	if b.Used >= b.Limit {
		return 0
	}
	return b.Limit - b.Used
}

// Validate enforces the threshold ordering invariant. Callers should
// run it when accepting budget configs from users or adaptor manifests.
func (b TokenBudget) Validate() error {
	if b.Limit < 0 || b.Used < 0 || b.Inform < 0 || b.Warn < 0 || b.Stop < 0 {
		return fmt.Errorf("core: TokenBudget values must be non-negative")
	}
	if b.Inform > b.Warn || b.Warn > b.Stop || b.Stop > b.Limit {
		return fmt.Errorf(
			"core: TokenBudget thresholds must satisfy "+
				"inform<=warn<=stop<=limit, got inform=%d warn=%d stop=%d limit=%d",
			b.Inform, b.Warn, b.Stop, b.Limit)
	}
	return nil
}

// UnmarshalJSON wraps the default decode with a Validate check so that
// invalid budgets surface at the adaptor boundary rather than later in
// the UI where a negative threshold would be much harder to diagnose.
func (b *TokenBudget) UnmarshalJSON(data []byte) error {
	type raw TokenBudget
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	tmp := TokenBudget(r)
	if err := tmp.Validate(); err != nil {
		return err
	}
	*b = tmp
	return nil
}
