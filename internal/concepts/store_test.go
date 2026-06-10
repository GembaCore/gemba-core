package concepts

import (
	"reflect"
	"testing"
	"time"
)

func TestSaveLoadVocabulary_RoundTrip(t *testing.T) {
	root := t.TempDir()
	v := &Vocabulary{}
	v.Add(Term{Name: "auth", Source: "bootstrap:go-packages"})
	v.Add(Term{Name: "react-query", Source: "operator", Aliases: []string{"rq"}})
	if err := SaveVocabulary(root, v); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadVocabulary(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Terms) != 2 {
		t.Fatalf("loaded %d terms, want 2", len(loaded.Terms))
	}
	if got := termNames(loaded); !reflect.DeepEqual(got, []string{"auth", "react-query"}) {
		t.Errorf("loaded names = %v", got)
	}
}

func TestLoadVocabulary_MissingFileReturnsEmpty(t *testing.T) {
	root := t.TempDir() // no .gemba/concepts/ in here
	v, err := LoadVocabulary(root)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(v.Terms) != 0 {
		t.Errorf("expected empty vocab; got %+v", v.Terms)
	}
}

func TestSaveSuggestionsLoadSuggestions_RoundTrip(t *testing.T) {
	root := t.TempDir()
	list := &SuggestionList{
		Suggestions: []Suggestion{
			{ID: "s-1", Kind: KindMerge, From: "a", To: "b", Status: StatusPending},
		},
	}
	if err := SaveSuggestions(root, list); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSuggestions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Suggestions) != 1 || loaded.Suggestions[0].ID != "s-1" {
		t.Errorf("round-trip failed: %+v", loaded.Suggestions)
	}
}

func TestAppendDecision_AppendsAndReads(t *testing.T) {
	root := t.TempDir()
	d1 := Decision{
		SuggestionID: "s-1", Kind: KindMerge,
		From: "a", To: "b", Action: "approved", By: "op",
	}
	d2 := Decision{
		SuggestionID: "s-2", Kind: KindDelete,
		From: "stale", Action: "rejected", By: "op", Reason: "still-needed",
	}
	if err := AppendDecision(root, d1); err != nil {
		t.Fatal(err)
	}
	if err := AppendDecision(root, d2); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDecisions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(got))
	}
	// Both decisions should have At stamped (server-side) — confirm
	// AppendDecision filled it for the d1 entry that had a zero At.
	if got[0].At.IsZero() || got[1].At.IsZero() {
		t.Errorf("decisions should have At stamped: %+v", got)
	}
	// Order preserved (append-only).
	if got[0].SuggestionID != "s-1" || got[1].SuggestionID != "s-2" {
		t.Errorf("decision order broken: %+v", got)
	}
	// Reading from a fresh tempdir returns nil cleanly.
	other := t.TempDir()
	empty, err := ReadDecisions(other)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("expected empty log; got %+v", empty)
	}
}

func TestAppendDecision_PreservesExplicitAt(t *testing.T) {
	root := t.TempDir()
	when := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := AppendDecision(root, Decision{
		SuggestionID: "s-1", Kind: KindMerge,
		Action: "approved", By: "op", At: when,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadDecisions(root)
	if !got[0].At.Equal(when) {
		t.Errorf("explicit At not preserved: %v", got[0].At)
	}
}
