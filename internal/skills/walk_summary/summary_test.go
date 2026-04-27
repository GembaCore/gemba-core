package walk_summary

import (
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/walk"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// fixedNow returns a deterministic clock so tests don't drift on
// time.Now() during render. Picked late in the day so the date
// arithmetic survives a TZ shift to the previous calendar day in
// any zone west of UTC.
func fixedNow() time.Time { return mustTime("2026-04-26T22:00:00Z") }

func TestGenerate_RejectsEmptyID(t *testing.T) {
	_, err := Generate(Input{Walk: walk.Walk{}, Now: fixedNow()})
	if err == nil {
		t.Fatal("expected error on empty walk id")
	}
}

func TestGenerate_DerivesPathFromLabel(t *testing.T) {
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:        "walk-001",
		Label:     "Sprint 47 retro",
		Workspace: "gemba",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
		Status:    walk.StatusCompleted,
	}
	out, err := Generate(Input{Walk: w, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got, want := out.RelativePath, "docs/walks/2026-04-26-sprint-47-retro.md"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestGenerate_LabelEmptyFallsBackToID(t *testing.T) {
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:        "walk-xyz",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
	}
	out, err := Generate(Input{Walk: w, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasSuffix(out.RelativePath, "-walk-xyz.md") {
		t.Errorf("path = %q, want suffix -walk-xyz.md", out.RelativePath)
	}
}

func TestGenerate_PathHintOverrides(t *testing.T) {
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:        "walk-001",
		Label:     "ignored",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
	}
	out, err := Generate(Input{Walk: w, Now: fixedNow(), PathHint: "/scratch/dryrun.md"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.RelativePath != "scratch/dryrun.md" {
		t.Errorf("path = %q, want scratch/dryrun.md", out.RelativePath)
	}
}

func TestGenerate_DecisionMatrix(t *testing.T) {
	ended := mustTime("2026-04-26T21:30:00Z")
	type want struct {
		hasRatify  bool
		hasModify  bool
		hasReject  bool
		hasDefer   bool
		hasHandoff bool
	}
	cases := []struct {
		name      string
		decisions []walk.WalkDecision
		want      want
	}{
		{"no decisions", nil, want{}},
		{"only ratify", []walk.WalkDecision{
			{Kind: walk.DecisionRatify}, {Kind: walk.DecisionRatify},
		}, want{hasRatify: true}},
		{"all five kinds", []walk.WalkDecision{
			{Kind: walk.DecisionRatify},
			{Kind: walk.DecisionModify},
			{Kind: walk.DecisionReject},
			{Kind: walk.DecisionDefer},
			{Kind: walk.DecisionHandoff},
		}, want{true, true, true, true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Generate(Input{
				Walk: walk.Walk{
					ID:        "walk-001",
					StartedAt: mustTime("2026-04-26T20:00:00Z"),
					EndedAt:   &ended,
					Decisions: tc.decisions,
				},
				Now: fixedNow(),
			})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			body := out.Markdown
			// Every report has the header line for every kind, so
			// the matrix really tests the narrative — present iff
			// the count > 0.
			if got := strings.Contains(body, "ratifying"); got != tc.want.hasRatify {
				t.Errorf("narrative ratify=%v, want %v", got, tc.want.hasRatify)
			}
			if got := strings.Contains(body, "modifying"); got != tc.want.hasModify {
				t.Errorf("narrative modify=%v, want %v", got, tc.want.hasModify)
			}
			if got := strings.Contains(body, "rejecting"); got != tc.want.hasReject {
				t.Errorf("narrative reject=%v, want %v", got, tc.want.hasReject)
			}
			if got := strings.Contains(body, "deferring"); got != tc.want.hasDefer {
				t.Errorf("narrative defer=%v, want %v", got, tc.want.hasDefer)
			}
			if got := strings.Contains(body, "handing off"); got != tc.want.hasHandoff {
				t.Errorf("narrative handoff=%v, want %v", got, tc.want.hasHandoff)
			}
		})
	}
}

func TestGenerate_AgendaCarriedForward(t *testing.T) {
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:        "walk-001",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
		Agenda: []walk.AgendaItem{
			{ID: "a1", Status: walk.AgendaDecided, Source: walk.AgendaSource{Kind: walk.SourceEscalation}},
			{ID: "a2", Status: walk.AgendaQueued, Source: walk.AgendaSource{Kind: walk.SourceHITL}},
			{ID: "a3", Status: walk.AgendaDeferred, Source: walk.AgendaSource{Kind: walk.SourceHITL}},
			{ID: "a4", Status: walk.AgendaDismissed, Source: walk.AgendaSource{Kind: walk.SourceUser}},
		},
	}
	out, err := Generate(Input{Walk: w, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := out.Markdown
	if !strings.Contains(body, "Total items: 4") {
		t.Errorf("missing total: %s", body)
	}
	if !strings.Contains(body, "Carried forward: 2") {
		t.Errorf("missing carried-forward count: %s", body)
	}
	if !strings.Contains(body, "Resolved: 1") {
		t.Errorf("missing resolved count: %s", body)
	}
	if !strings.Contains(body, "Dismissed: 1") {
		t.Errorf("missing dismissed count: %s", body)
	}
	// Per-source: hitl=2, escalation=1, user=1. Stable order.
	if !strings.Contains(body, "- escalation: 1") {
		t.Errorf("missing source breakdown: %s", body)
	}
	if !strings.Contains(body, "- hitl: 2") {
		t.Errorf("missing source breakdown: %s", body)
	}
	if !strings.Contains(body, "carry forward to the next walk") {
		t.Errorf("narrative missing carry-forward phrase: %s", body)
	}
}

func TestGenerate_BeadsAndCost(t *testing.T) {
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:           "walk-001",
		Label:        "cost demo",
		StartedAt:    mustTime("2026-04-26T20:00:00Z"),
		EndedAt:      &ended,
		BeadsTouched: []core.WorkItemID{"gm-001", "gm-002"},
		Cost:         walk.Cost{TokensIn: 1234, TokensOut: 567, Dollars: 0.0421},
	}
	out, err := Generate(Input{Walk: w, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := out.Markdown
	if !strings.Contains(body, "`gm-001`") || !strings.Contains(body, "`gm-002`") {
		t.Errorf("missing bead ids: %s", body)
	}
	if !strings.Contains(body, "$0.0421") {
		t.Errorf("missing dollars: %s", body)
	}
	if !strings.Contains(body, "1234 in / 567 out tokens") {
		t.Errorf("missing token breakdown: %s", body)
	}
	if !strings.Contains(body, "Work touched 2 beads.") {
		t.Errorf("missing narrative beads phrase: %s", body)
	}
}

func TestGenerate_EmptyWalkProducesGracefulNarrative(t *testing.T) {
	ended := mustTime("2026-04-26T20:00:30Z")
	w := walk.Walk{
		ID:        "walk-empty",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
	}
	out, err := Generate(Input{Walk: w, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(out.Markdown, "ended without any agenda items or decisions") {
		t.Errorf("missing empty-walk narrative: %s", out.Markdown)
	}
	if !strings.Contains(out.Markdown, "_None._") {
		t.Errorf("expected empty-beads placeholder: %s", out.Markdown)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{5 * time.Minute, "5m"},
		{61 * time.Minute, "1h1m"},
		{2 * time.Hour, "2h"},
		{-5 * time.Second, "0s"},
	}
	for _, tc := range cases {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestJoinClauses(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
		{[]string{"a", "b", "c", "d"}, "a, b, c, and d"},
	}
	for _, tc := range cases {
		got := joinClauses(tc.in)
		if got != tc.want {
			t.Errorf("joinClauses(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
