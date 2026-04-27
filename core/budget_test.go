package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestTokenBudgetTier(t *testing.T) {
	b := TokenBudget{Limit: 1000, Inform: 500, Warn: 750, Stop: 900}
	cases := []struct {
		used int64
		want BudgetTier
	}{
		{0, ""},
		{499, ""},
		{500, BudgetInform},
		{749, BudgetInform},
		{750, BudgetWarn},
		{899, BudgetWarn},
		{900, BudgetStop},
		{1000, BudgetStop},
	}
	for _, c := range cases {
		b.Used = c.used
		if got := b.Tier(); got != c.want {
			t.Errorf("used=%d: Tier()=%q want %q", c.used, got, c.want)
		}
	}
}

func TestTokenBudgetRemaining(t *testing.T) {
	b := TokenBudget{Limit: 100, Used: 30}
	if got := b.Remaining(); got != 70 {
		t.Errorf("Remaining=%d want 70", got)
	}
	b.Used = 500 // overconsumed
	if got := b.Remaining(); got != 0 {
		t.Errorf("Remaining over-limit=%d want 0", got)
	}
}

func TestTokenBudgetValidate(t *testing.T) {
	ok := TokenBudget{Limit: 100, Inform: 10, Warn: 50, Stop: 80}
	if err := ok.Validate(); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}

	bad := []TokenBudget{
		{Limit: -1},
		{Limit: 100, Inform: 60, Warn: 50, Stop: 80},  // inform > warn
		{Limit: 100, Inform: 10, Warn: 50, Stop: 120}, // stop > limit
	}
	for i, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("case %d: expected validation error, got nil", i)
		}
	}
}

func TestTokenBudgetJSONRejectsInvalid(t *testing.T) {
	var b TokenBudget
	if err := json.Unmarshal([]byte(`{"limit":100,"inform":90,"warn":50,"stop":80,"used":0}`), &b); err == nil {
		t.Error("expected decode error for inform > warn")
	}
}

func TestSprintRoundTrip(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(14 * 24 * time.Hour)
	sp := Sprint{
		ID:        "sp-2026-w14",
		Name:      "Sprint 14",
		StartsAt:  start,
		EndsAt:    end,
		Goal:      "Land core contracts",
		WorkItems: []WorkItemID{"gemba/gemba/gm-e3.1", "gemba/gemba/gm-e3.2"},
		Budget: &TokenBudget{
			Limit: 10_000_000, Used: 100, Inform: 5_000_000,
			Warn: 7_500_000, Stop: 9_000_000,
		},
	}
	data, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round Sprint
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(sp, round) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", round, sp)
	}
}

func TestDefinitionOfDoneRoundTrip(t *testing.T) {
	d := DefinitionOfDone{
		AcceptanceCriteria: []string{"godoc present", "tests green"},
		Notes:              "informational-only",
		Version:            "v1",
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round DefinitionOfDone
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(d, round) {
		t.Errorf("round trip mismatch: %+v vs %+v", round, d)
	}
}
