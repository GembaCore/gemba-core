// Pure-function tests for the Intent + Match contract (gm-v5z2.3).

package intent

import (
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestIntent_IsZero(t *testing.T) {
	if !(Intent{}).IsZero() {
		t.Error("default Intent must be zero")
	}
	if (Intent{EpicID: "gm-e1"}).IsZero() {
		t.Error("Intent with EpicID is non-zero")
	}
	if (Intent{Label: "spa"}).IsZero() {
		t.Error("Intent with Label is non-zero")
	}
	if (Intent{BeadIDRegex: "^gm-"}).IsZero() {
		t.Error("Intent with BeadIDRegex is non-zero")
	}
}

func TestIntent_EffectiveDemotionFactor(t *testing.T) {
	if got := (Intent{}).EffectiveDemotionFactor(); got != DefaultDemotionFactor {
		t.Errorf("zero demotion → default; got %f", got)
	}
	if got := (Intent{DemotionFactor: 0.6}).EffectiveDemotionFactor(); got != 0.6 {
		t.Errorf("explicit demotion threaded through; got %f", got)
	}
	// Negative is treated as "use default" by the read path so a
	// stray sql NULL load can't accidentally zero scores.
	if got := (Intent{DemotionFactor: -1}).EffectiveDemotionFactor(); got != DefaultDemotionFactor {
		t.Errorf("negative demotion → default; got %f", got)
	}
}

func TestIntent_Validate_EmptyIsValid(t *testing.T) {
	if err := (Intent{}).Validate(); err != nil {
		t.Errorf("empty intent should validate (cleared state); got %v", err)
	}
}

func TestIntent_Validate_BadRegex(t *testing.T) {
	err := Intent{BeadIDRegex: "[unterminated"}.Validate()
	if err == nil {
		t.Fatal("invalid regex must reject")
	}
	if !strings.Contains(err.Error(), "regex") {
		t.Errorf("error message should call out the regex: %v", err)
	}
}

func TestIntent_Validate_NegativeDemotion(t *testing.T) {
	err := Intent{EpicID: "gm-e1", DemotionFactor: -0.1}.Validate()
	if err == nil {
		t.Fatal("negative demotion must reject on validate")
	}
}

func TestIntent_Validate_DemotionAboveOne(t *testing.T) {
	err := Intent{EpicID: "gm-e1", DemotionFactor: 1.5}.Validate()
	if err == nil {
		t.Fatal("demotion > 1 must reject (would promote out-of-intent)")
	}
}

func TestIntent_Validate_HappyPath(t *testing.T) {
	cases := []Intent{
		{EpicID: "gm-e1"},
		{Label: "spa"},
		{BeadIDRegex: "^gm-s47n\\."},
		{EpicID: "gm-e1", Label: "spa", DemotionFactor: 0.3},
	}
	for _, c := range cases {
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v; want nil", c, err)
		}
	}
}

// ── Match ────────────────────────────────────────────────────────

func cand(id, epic core.WorkItemID, labels ...string) Candidate {
	return Candidate{BeadID: id, EpicID: epic, Labels: labels}
}

func TestMatch_EmptyIntentMatchesEverything(t *testing.T) {
	if !Match(Intent{}, cand("gm-1", "gm-e1", "spa")) {
		t.Error("empty intent must match every candidate")
	}
	if !Match(Intent{}, cand("gm-2", "", "")) {
		t.Error("empty intent matches even epic-less candidates")
	}
}

func TestMatch_EpicID(t *testing.T) {
	in := Intent{EpicID: "gm-e1"}
	if !Match(in, cand("gm-1", "gm-e1")) {
		t.Error("same epic must match")
	}
	if Match(in, cand("gm-1", "gm-e2")) {
		t.Error("different epic must not match")
	}
	if Match(in, cand("gm-1", "")) {
		t.Error("epic-less candidate must not match an EpicID restrictor")
	}
}

func TestMatch_Label(t *testing.T) {
	in := Intent{Label: "spa"}
	if !Match(in, cand("gm-1", "gm-e1", "spa", "ui")) {
		t.Error("candidate carrying the label must match")
	}
	if Match(in, cand("gm-1", "gm-e1", "backend")) {
		t.Error("candidate without the label must not match")
	}
	if Match(in, cand("gm-1", "gm-e1")) {
		t.Error("label-less candidate must not match")
	}
}

func TestMatch_Regex(t *testing.T) {
	in := Intent{BeadIDRegex: "^gm-s47n\\.[0-9]+$"}
	if !Match(in, cand("gm-s47n.4", "")) {
		t.Error("matching id must match")
	}
	if Match(in, cand("gm-s47n.4.1", "")) {
		t.Errorf("nested id should NOT match the [0-9]+ pattern")
	}
	if Match(in, cand("gm-uipx.5", "")) {
		t.Error("non-matching id must not match")
	}
}

func TestMatch_MultipleRestrictorsAreANDed(t *testing.T) {
	in := Intent{EpicID: "gm-e1", Label: "spa"}
	if !Match(in, cand("gm-1", "gm-e1", "spa")) {
		t.Error("both restrictors satisfied → match")
	}
	if Match(in, cand("gm-1", "gm-e1", "backend")) {
		t.Error("epic ok but missing label → no match")
	}
	if Match(in, cand("gm-1", "gm-e2", "spa")) {
		t.Error("label ok but wrong epic → no match")
	}
}

func TestMatch_BadRegexFailsClosed(t *testing.T) {
	// Validate should have caught this upstream, but the run-time
	// path can't panic — fail closed (no match) instead.
	in := Intent{BeadIDRegex: "[unterminated"}
	if Match(in, cand("gm-1", "")) {
		t.Error("uncompilable regex must fail closed (no match)")
	}
}
