package bridge

import (
	"reflect"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestExtractSections_QuestionsAndBlockers(t *testing.T) {
	md := `I'll start the work.

## Questions

1. Should I default to the test key?
2. Does the webhook path need to be idempotent?

## Blockers

1. I need the prod Stripe key to finish.

Closing thoughts.`

	got := ExtractSections(md)
	want := []SkillSection{
		{Kind: core.EscalationQuestion, Items: []string{
			"Should I default to the test key?",
			"Does the webhook path need to be idempotent?",
		}},
		{Kind: core.EscalationBlocker, Items: []string{
			"I need the prod Stripe key to finish.",
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extract mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestExtractSections_OnlyLastOccurrenceWins(t *testing.T) {
	// The agent drafts a Questions section mid-response, then rewrites
	// it. Only the final list should surface.
	md := `First draft, to throw away:

## Questions

1. Old question that should NOT surface.

Let me think again.

## Questions

1. Final question that SHOULD surface.`

	got := ExtractSections(md)
	if len(got) != 1 {
		t.Fatalf("want 1 section, got %d: %+v", len(got), got)
	}
	if got[0].Items[0] != "Final question that SHOULD surface." {
		t.Fatalf("unexpected item: %q", got[0].Items[0])
	}
}

func TestExtractSections_NoSectionsReturnsNil(t *testing.T) {
	md := "Just prose. No sections here."
	got := ExtractSections(md)
	if got != nil {
		t.Fatalf("no headings should return nil; got %+v", got)
	}
}

func TestExtractSections_IgnoresBulletListsUnderHeading(t *testing.T) {
	// Narrative under the heading must not be gobbled as items — the
	// scanner only matches numbered-list entries. Prevents accidental
	// escalations from a skill that writes prose there.
	md := `## Questions

- bullet one
- bullet two

Some narrative.

## Blockers

1. Real blocker here.`

	got := ExtractSections(md)
	if len(got) != 1 {
		t.Fatalf("want 1 section (blockers only); got %+v", got)
	}
	if got[0].Kind != core.EscalationBlocker {
		t.Fatalf("want blocker; got %v", got[0].Kind)
	}
}

func TestExtractSections_IgnoresAlternativeHeadings(t *testing.T) {
	md := `### Questions

1. This shouldn't count.

## Asks

1. Neither should this.`
	got := ExtractSections(md)
	if got != nil {
		t.Fatalf("heading variants must not match; got %+v", got)
	}
}

func TestExtractSections_FoldsContinuationLines(t *testing.T) {
	md := `## Blockers

1. I need the prod Stripe key to finish the webhook path.
   Please set STRIPE_SECRET_KEY in the worktree env and resume.`
	got := ExtractSections(md)
	if len(got) != 1 || len(got[0].Items) != 1 {
		t.Fatalf("unexpected shape: %+v", got)
	}
	want := "I need the prod Stripe key to finish the webhook path. " +
		"Please set STRIPE_SECRET_KEY in the worktree env and resume."
	if got[0].Items[0] != want {
		t.Fatalf("continuation fold:\n got=%q\nwant=%q", got[0].Items[0], want)
	}
}

func TestExtractSections_BlankLineWithinSection(t *testing.T) {
	// A blank line between items must not end the section.
	md := `## Questions

1. First.

2. Second.`
	got := ExtractSections(md)
	if len(got) != 1 || len(got[0].Items) != 2 {
		t.Fatalf("blank-line-within-section: %+v", got)
	}
}

func TestClassify_MatrixCoverage(t *testing.T) {
	cases := []struct {
		mode     InteractionMode
		kind     core.EscalationKind
		wantEmit bool
		wantUrg  core.EscalationUrgency
	}{
		// dangerous: both drop
		{ModeDangerous, core.EscalationQuestion, false, ""},
		{ModeDangerous, core.EscalationBlocker, false, ""},
		// balanced: question advisory, blocker blocking
		{ModeBalanced, core.EscalationQuestion, true, core.UrgencyAdvisory},
		{ModeBalanced, core.EscalationBlocker, true, core.UrgencyBlocking},
		// cautious: everything blocks
		{ModeCautious, core.EscalationQuestion, true, core.UrgencyBlocking},
		{ModeCautious, core.EscalationBlocker, true, core.UrgencyBlocking},
	}
	for _, c := range cases {
		t.Run(string(c.mode)+"/"+string(c.kind), func(t *testing.T) {
			urg, emit := Classify(c.kind, c.mode)
			if emit != c.wantEmit {
				t.Fatalf("emit: got %v, want %v", emit, c.wantEmit)
			}
			if urg != c.wantUrg {
				t.Fatalf("urgency: got %q, want %q", urg, c.wantUrg)
			}
		})
	}
}

func TestClassify_UnknownModeFallsBackToBalanced(t *testing.T) {
	// An operator typo in agents.toml shouldn't silently drop
	// escalations — treat as balanced (the documented default).
	urg, emit := Classify(core.EscalationBlocker, InteractionMode("lolcat"))
	if !emit || urg != core.UrgencyBlocking {
		t.Fatalf("unknown-mode fallback broke: emit=%v urg=%q", emit, urg)
	}
	urg, emit = Classify(core.EscalationQuestion, InteractionMode(""))
	if !emit || urg != core.UrgencyAdvisory {
		t.Fatalf("unknown-mode question fallback broke: emit=%v urg=%q", emit, urg)
	}
}
