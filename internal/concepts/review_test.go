package concepts

import (
	"errors"
	"testing"
)

func TestSuggestionList_AddIdempotent(t *testing.T) {
	l := &SuggestionList{}
	s := Suggestion{ID: "s-1", Kind: KindMerge, From: "rq", To: "react-query", Status: StatusPending}
	if !l.Add(s) {
		t.Fatal("first Add should succeed")
	}
	// Same kind/from/to → rejected as duplicate.
	if l.Add(Suggestion{ID: "s-2", Kind: KindMerge, From: "rq", To: "react-query", Status: StatusPending}) {
		t.Error("duplicate suggestion should not be added")
	}
}

func TestSuggestionList_AddPostRejectionAllowed(t *testing.T) {
	// A rejected suggestion shouldn't block a re-proposal — the
	// operator's earlier "no" was about that instance, not the idea.
	l := &SuggestionList{}
	first := Suggestion{ID: "s-1", Kind: KindMerge, From: "a", To: "b", Status: StatusRejected}
	l.Suggestions = append(l.Suggestions, first)
	second := Suggestion{ID: "s-2", Kind: KindMerge, From: "a", To: "b", Status: StatusPending}
	if !l.Add(second) {
		t.Error("re-proposing after rejection should succeed")
	}
}

func TestSuggestionList_MarkRejectsDoubleDecision(t *testing.T) {
	l := &SuggestionList{}
	l.Add(Suggestion{ID: "s-1", Kind: KindDelete, From: "stale", Status: StatusPending})
	if err := l.Mark("s-1", StatusApproved); err != nil {
		t.Fatal(err)
	}
	err := l.Mark("s-1", StatusRejected)
	if !errors.Is(err, ErrSuggestionDecided) {
		t.Errorf("re-marking decided suggestion must error with ErrSuggestionDecided, got %v", err)
	}
}

func TestSuggestionList_MarkUnknownIDIsTypedError(t *testing.T) {
	l := &SuggestionList{}
	err := l.Mark("missing", StatusApproved)
	if !errors.Is(err, ErrSuggestionNotFound) {
		t.Errorf("Mark on missing id should return ErrSuggestionNotFound, got %v", err)
	}
}

func TestSuggestionsFromDrift_MergeSuggestion(t *testing.T) {
	d := Drift{
		NearDuplicates: []NearDuplicate{
			{A: "rq", B: "react-query", Jaccard: 0.95, UsesA: 4, UsesB: 7},
		},
	}
	out := SuggestionsFromDrift(d, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 suggestion; got %d", len(out))
	}
	s := out[0]
	if s.Kind != KindMerge {
		t.Errorf("kind = %q, want merge", s.Kind)
	}
	// Higher-use term wins as the surviving canonical.
	if s.From != "rq" || s.To != "react-query" {
		t.Errorf("merge direction = %s→%s, want rq→react-query", s.From, s.To)
	}
}

func TestSuggestionsFromDrift_DedupesAgainstExisting(t *testing.T) {
	d := Drift{
		NearDuplicates: []NearDuplicate{
			{A: "rq", B: "react-query", Jaccard: 0.95, UsesA: 4, UsesB: 7},
		},
	}
	existing := []Suggestion{
		{ID: "s-old", Kind: KindMerge, From: "rq", To: "react-query", Status: StatusPending},
	}
	out := SuggestionsFromDrift(d, existing)
	if len(out) != 0 {
		t.Errorf("existing pending suggestion should suppress dup; got %+v", out)
	}
}

func TestSuggestionsFromDrift_SingletonDelete(t *testing.T) {
	d := Drift{
		Singletons: []Singleton{{Term: "abandoned", BeadID: "b1"}},
	}
	out := SuggestionsFromDrift(d, nil)
	if len(out) != 1 || out[0].Kind != KindDelete || out[0].From != "abandoned" {
		t.Errorf("unexpected suggestions: %+v", out)
	}
}

func TestNewSuggestionID_StableShape(t *testing.T) {
	id := NewSuggestionID()
	if len(id) != 10 || id[:2] != "s-" {
		t.Errorf("id = %q, want s-XXXXXXXX", id)
	}
}
