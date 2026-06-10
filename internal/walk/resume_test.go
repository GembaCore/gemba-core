package walk

import (
	"strings"
	"testing"
	"time"
)

func TestBuildResumeContext_BasicShape(t *testing.T) {
	w := Walk{
		ID:             "walk-1",
		Workspace:      "ws-x",
		Label:          "morning review",
		InitiatedBy:    "user-mike",
		PrimaryPersona: "project-manager",
		StartedAt:      time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
		Status:         StatusPaused,
		Agenda: []AgendaItem{
			{ID: "i1", Topic: "review escalation A", Status: AgendaQueued},
			{ID: "i2", Topic: "active topic", Status: AgendaActive, Source: AgendaSource{Kind: SourceEscalation}},
			{ID: "i3", Topic: "decided", Status: AgendaDecided},
		},
		Decisions: []WalkDecision{
			{AgendaItemID: "i3", Kind: DecisionRatify, DecidedAt: time.Now(), DecidedBy: "user-mike"},
		},
		Cost: Cost{TokensIn: 100, TokensOut: 50, Dollars: 0.05},
	}
	got := BuildResumeContext(w, ResumeOptions{})
	if !strings.Contains(got, "walk-1") {
		t.Errorf("missing id: %s", got)
	}
	if !strings.Contains(got, "paused") {
		t.Errorf("missing status: %s", got)
	}
	if !strings.Contains(got, "morning review") {
		t.Errorf("missing label: %s", got)
	}
	if !strings.Contains(got, "1 queued") || !strings.Contains(got, "1 active") || !strings.Contains(got, "1 decided") {
		t.Errorf("missing agenda counts: %s", got)
	}
	if !strings.Contains(got, "active topic") {
		t.Errorf("missing active item topic: %s", got)
	}
	if !strings.Contains(got, "1 ratify") {
		t.Errorf("missing decision counts: %s", got)
	}
	if !strings.Contains(got, "$0.05") {
		t.Errorf("missing cost: %s", got)
	}
}

func TestBuildResumeContext_TruncatesContentToMaxRunes(t *testing.T) {
	long := strings.Repeat("a", 500)
	w := Walk{
		ID:        "walk-1",
		Workspace: "ws",
		Status:    StatusPaused,
		Transcript: []WalkTurn{
			{Speaker: SpeakerUser, Content: long, At: time.Now()},
		},
	}
	got := BuildResumeContext(w, ResumeOptions{MaxContentRunes: 50})
	if !strings.Contains(got, "…") {
		t.Errorf("expected truncation marker; got %s", got)
	}
	if strings.Contains(got, long) {
		t.Errorf("content not truncated")
	}
}

func TestBuildResumeContext_TailLimitedToRecentTurns(t *testing.T) {
	turns := make([]WalkTurn, 12)
	for i := range turns {
		turns[i] = WalkTurn{
			Speaker: SpeakerUser,
			Content: "turn " + string(rune('a'+i)),
			At:      time.Now().Add(time.Duration(i) * time.Minute),
		}
	}
	w := Walk{ID: "w", Workspace: "ws", Status: StatusPaused, Transcript: turns}
	got := BuildResumeContext(w, ResumeOptions{RecentTurns: 3})
	// First 9 turns must NOT appear in the synopsis.
	for i := 0; i < 9; i++ {
		needle := "turn " + string(rune('a'+i))
		if strings.Contains(got, needle) {
			t.Errorf("synopsis should drop early turn %q: %s", needle, got)
		}
	}
	// Last 3 must appear.
	for i := 9; i < 12; i++ {
		needle := "turn " + string(rune('a'+i))
		if !strings.Contains(got, needle) {
			t.Errorf("synopsis missing tail turn %q: %s", needle, got)
		}
	}
}

func TestTruncRunes_HandlesUnicode(t *testing.T) {
	got := truncRunes("héllo wörld", 6)
	if got != "héllo…" {
		t.Errorf("truncRunes unicode: got %q", got)
	}
}
