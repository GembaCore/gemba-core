package analyzer

import (
	"context"
	"strings"
	"testing"
)

func TestHeuristic_FlatChecklist(t *testing.T) {
	in := `- [ ] add login form
- [x] wire up auth backend
- [ ] [P1] write integration tests`
	p, err := Analyze(context.Background(), in, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(p.Beads) != 4 {
		t.Fatalf("expected 1 synthetic milestone + 3 stories, got %d: %+v", len(p.Beads), p.Beads)
	}
	if p.Beads[0].Kind != KindMilestone || p.Beads[0].Title != "Imported tasks" {
		t.Fatalf("expected synthetic milestone, got %+v", p.Beads[0])
	}
	for i := 1; i < 4; i++ {
		if p.Beads[i].Kind != KindStory {
			t.Fatalf("bead %d expected story, got %s", i, p.Beads[i].Kind)
		}
		if p.Beads[i].Parent != "#0" {
			t.Fatalf("bead %d expected parent #0, got %q", i, p.Beads[i].Parent)
		}
	}
	if p.Beads[3].Priority != "P1" {
		t.Fatalf("expected P1 on third story, got %q", p.Beads[3].Priority)
	}
	if !strings.Contains(p.Beads[3].Title, "integration tests") {
		t.Fatalf("priority tag not stripped from title: %q", p.Beads[3].Title)
	}
}

func TestHeuristic_SectionedDoc(t *testing.T) {
	in := `# Q3 platform polish

## Auth hardening
- [ ] rotate signing keys
- [ ] [P0] expire stale sessions

## Billing
- [ ] add invoice export`
	p, err := Analyze(context.Background(), in, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// expected: milestone, epic, story, story, epic, story
	if len(p.Beads) != 6 {
		t.Fatalf("expected 6 beads, got %d: %+v", len(p.Beads), p.Beads)
	}
	if p.Beads[0].Kind != KindMilestone || !strings.Contains(p.Beads[0].Title, "Q3 platform polish") {
		t.Fatalf("milestone wrong: %+v", p.Beads[0])
	}
	if p.Beads[1].Kind != KindEpic || p.Beads[1].Parent != "#0" {
		t.Fatalf("epic[1] wrong: %+v", p.Beads[1])
	}
	if p.Beads[2].Parent != "#1" || p.Beads[2].Kind != KindStory {
		t.Fatalf("story[2] should parent to epic #1: %+v", p.Beads[2])
	}
	if p.Beads[3].Priority != "P0" {
		t.Fatalf("expected P0 on story[3], got %q", p.Beads[3].Priority)
	}
	if p.Beads[4].Kind != KindEpic || p.Beads[4].Parent != "#0" {
		t.Fatalf("epic[4] wrong: %+v", p.Beads[4])
	}
	if p.Beads[5].Kind != KindStory || p.Beads[5].Parent != "#4" {
		t.Fatalf("story[5] should parent to epic #4: %+v", p.Beads[5])
	}
}

func TestHeuristic_MixedMilestonePrefixAndProse(t *testing.T) {
	in := `Some intro prose that should be ignored.

Milestone: Pricing v2 rollout

random noise line

## Storefront
- [ ] price ladder UI

## Backend
- [ ] new pricing endpoint
- [ ] feature flag wiring`
	p, err := Analyze(context.Background(), in, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if p.Beads[0].Kind != KindMilestone || p.Beads[0].Title != "Pricing v2 rollout" {
		t.Fatalf("milestone wrong: %+v", p.Beads[0])
	}
	// Count epics + stories
	var epics, stories int
	for _, b := range p.Beads {
		switch b.Kind {
		case KindEpic:
			epics++
		case KindStory:
			stories++
		}
	}
	if epics != 2 || stories != 3 {
		t.Fatalf("expected 2 epics + 3 stories, got epics=%d stories=%d", epics, stories)
	}
	if p.Source != "heuristic" {
		t.Fatalf("expected source=heuristic, got %q", p.Source)
	}
}

func TestExtractMarkdown_UnwrapsHookPayload(t *testing.T) {
	raw := `{"tool_name":"Write","tool_input":{"file_path":"tasks.md","content":"# Hi\n- [ ] a"}}`
	got := extractMarkdown(raw)
	if !strings.Contains(got, "# Hi") {
		t.Fatalf("expected unwrap, got %q", got)
	}
}

func TestAnalyze_EmptyPayloadError(t *testing.T) {
	if _, err := Analyze(context.Background(), "   \n  ", Options{}); err == nil {
		t.Fatal("expected error on empty payload")
	}
}
