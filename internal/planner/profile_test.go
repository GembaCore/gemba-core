package planner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// SessionProfile round-trips through JSON without losing or
// fabricating fields. Empty maps + zero-value scalars MUST drop out
// (omitempty) so an empty profile serialises to a tight envelope.
func TestSessionProfile_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	in := SessionProfile{
		SessionID:        "sess-1",
		AssignmentID:     "asg-1",
		AgentID:          "gemba/crew/mike",
		Concepts:         map[ConceptTag]float64{"auth": 1.0, "spa-routing": 0.25},
		Files:            map[string]float64{"web/src/App.tsx": 0.5},
		TokensUsed:       12000,
		ContextWindowMax: 200000,
		ContextPct:       0.06,
		LastBeads:        []core.WorkItemID{"gm-1", "gm-2", "gm-3"},
		LastActivityAt:   now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	bs, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out SessionProfile
	if err := json.Unmarshal(bs, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SessionID != in.SessionID {
		t.Errorf("session_id round-trip lost: got %q", out.SessionID)
	}
	if got := out.Concepts["auth"]; got != 1.0 {
		t.Errorf("concept weight lost: got %v", got)
	}
	if len(out.LastBeads) != 3 {
		t.Errorf("last_beads round-trip lost: got %d entries", len(out.LastBeads))
	}
	if !out.LastActivityAt.Equal(in.LastActivityAt) {
		t.Errorf("last_activity_at lost precision: got %v", out.LastActivityAt)
	}
}

// Empty profile serialises to a tight envelope — every omitempty
// field actually drops. Catches accidental zero-value emission that
// would bloat the wire payload (the planner reads this on every
// dispatch decision).
func TestSessionProfile_EmptyEnvelopeIsTight(t *testing.T) {
	in := SessionProfile{
		SessionID:      "sess-empty",
		AssignmentID:   "asg-empty",
		AgentID:        "gemba/crew/mike",
		LastActivityAt: time.Unix(0, 0).UTC(),
		CreatedAt:      time.Unix(0, 0).UTC(),
		UpdatedAt:      time.Unix(0, 0).UTC(),
	}
	bs, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(bs)
	for _, k := range []string{"concepts", "files", "tokens_used", "context_window_max", "context_pct", "last_beads"} {
		if strings.Contains(body, "\""+k+"\"") {
			t.Errorf("empty profile leaked %q field: %s", k, body)
		}
	}
}

// LastBeadsRingSize stays at 5 unless the spec changes — defended
// here so a casual edit shows up in code review.
func TestLastBeadsRingSize_Constant(t *testing.T) {
	if LastBeadsRingSize != 5 {
		t.Errorf("LastBeadsRingSize = %d; spec §4 Layer 1 pins it at 5", LastBeadsRingSize)
	}
	if DefaultDecayHalfLife != 5 {
		t.Errorf("DefaultDecayHalfLife = %d; spec pins default at 5 bead events", DefaultDecayHalfLife)
	}
}

// OperationalContext serialises with omitempty on every join — a
// session with no profile yet (transient state) MUST not emit a
// "profile":null line that downstream consumers might trip over.
func TestOperationalContext_EmitsOnlyPopulatedJoins(t *testing.T) {
	ctx := OperationalContext{
		Session: &core.Session{ID: "sess-only", AssignmentID: "asg-only"},
	}
	bs, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(bs)
	for _, k := range []string{"agent", "workspace", "assignment", "profile", "health"} {
		if strings.Contains(body, "\""+k+"\":") {
			t.Errorf("OperationalContext leaked unpopulated %q: %s", k, body)
		}
	}
	if !strings.Contains(body, "\"session\":") {
		t.Errorf("OperationalContext dropped the populated session field: %s", body)
	}
}

// SchemaSQL is embedded — this test guards that the embed actually
// pulled the file in (a missing file would silently embed an empty
// string and crash migrations later).
func TestSchemaSQL_EmbeddedAndContainsTableName(t *testing.T) {
	if SchemaSQL == "" {
		t.Fatal("SchemaSQL is empty; the //go:embed directive failed")
	}
	if !strings.Contains(SchemaSQL, "session_profiles") {
		t.Errorf("SchemaSQL doesn't reference session_profiles table: %s", SchemaSQL)
	}
	for _, col := range []string{"concepts", "files", "context_pct", "last_beads", "last_activity_at"} {
		if !strings.Contains(SchemaSQL, col) {
			t.Errorf("SchemaSQL missing column %q", col)
		}
	}
}
