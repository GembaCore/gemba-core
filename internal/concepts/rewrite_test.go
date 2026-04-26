package concepts

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

func TestApplyMerge_RewritesEveryHistoricalBead(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for id, concepts := range map[string][]string{
		"b1": {"rq", "auth"},
		"b2": {"rq"},
		"b3": {"react-query"},
		"b4": {"unrelated"},
	} {
		_ = store.Set(ctx, id, concepts)
	}
	n, err := ApplyMerge(ctx, store, "rq", "react-query")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("expected 2 beads changed; got %d", n)
	}
	got, _ := store.List(ctx)
	want := map[string][]string{
		"b1": {"react-query", "auth"},
		"b2": {"react-query"},
		"b3": {"react-query"},
		"b4": {"unrelated"},
	}
	for _, b := range got {
		w, ok := want[b.BeadID]
		if !ok {
			t.Errorf("unexpected bead %s", b.BeadID)
			continue
		}
		if !reflect.DeepEqual(sortedCopy(b.Concepts), sortedCopy(w)) {
			t.Errorf("bead %s = %v, want %v", b.BeadID, b.Concepts, w)
		}
	}
}

func TestApplyMerge_DedupesWhenBothPresent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Set(ctx, "b1", []string{"rq", "react-query", "auth"})
	n, err := ApplyMerge(ctx, store, "rq", "react-query")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 changed (dedup); got %d", n)
	}
	got, _ := store.List(ctx)
	if !reflect.DeepEqual(sortedCopy(got[0].Concepts), []string{"auth", "react-query"}) {
		t.Errorf("dedup wrong: %v", got[0].Concepts)
	}
}

func TestApplyDelete_DropsTermFromEveryBead(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Set(ctx, "b1", []string{"abandoned", "auth"})
	_ = store.Set(ctx, "b2", []string{"abandoned"})
	_ = store.Set(ctx, "b3", []string{"auth"})
	n, err := ApplyDelete(ctx, store, "abandoned")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("expected 2 beads changed; got %d", n)
	}
	got, _ := store.List(ctx)
	for _, b := range got {
		for _, c := range b.Concepts {
			if c == "abandoned" {
				t.Errorf("bead %s still carries abandoned: %v", b.BeadID, b.Concepts)
			}
		}
	}
}

func TestApplyMerge_RejectsSameTerm(t *testing.T) {
	store := NewMemoryStore()
	if _, err := ApplyMerge(context.Background(), store, "foo", "foo"); err == nil {
		t.Error("merge from foo to foo must error")
	}
}

func TestApplyDecision_MergeMaterializesBothSides(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "rq"})
	v.Add(Term{Name: "react-query"})
	store := NewMemoryStore()
	_ = store.Set(context.Background(), "b1", []string{"rq", "auth"})

	list := &SuggestionList{}
	list.Add(Suggestion{
		ID: "s-1", Kind: KindMerge,
		From: "rq", To: "react-query",
		Status: StatusPending,
	})

	dec, err := ApplyDecision(context.Background(), v, list, store, "s-1", "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if dec.BeadsChanged != 1 {
		t.Errorf("BeadsChanged = %d, want 1", dec.BeadsChanged)
	}
	if dec.By != "operator@example.com" {
		t.Errorf("Decision.By not propagated")
	}
	// Vocabulary side: rq retired, react-query carries rq as alias.
	rq, _ := v.Find("rq")
	if !rq.Retired {
		t.Error("rq should be retired in vocabulary")
	}
	rk, _ := v.Find("react-query")
	if !contains(rk.Aliases, "rq") {
		t.Errorf("react-query should carry rq as alias: %+v", rk.Aliases)
	}
	// Suggestion is now approved.
	if list.Suggestions[0].Status != StatusApproved {
		t.Errorf("suggestion status = %s, want approved", list.Suggestions[0].Status)
	}
}

func TestApplyDecision_DeleteRetiresAndRewrites(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "stale"})
	store := NewMemoryStore()
	_ = store.Set(context.Background(), "b1", []string{"stale", "auth"})

	list := &SuggestionList{}
	list.Add(Suggestion{ID: "s-2", Kind: KindDelete, From: "stale", Status: StatusPending})

	dec, err := ApplyDecision(context.Background(), v, list, store, "s-2", "op")
	if err != nil {
		t.Fatal(err)
	}
	if dec.BeadsChanged != 1 {
		t.Errorf("BeadsChanged = %d, want 1", dec.BeadsChanged)
	}
	stale, _ := v.Find("stale")
	if !stale.Retired {
		t.Error("stale should be retired")
	}
	got, _ := store.List(context.Background())
	for _, c := range got[0].Concepts {
		if c == "stale" {
			t.Errorf("bead still carries stale: %v", got[0].Concepts)
		}
	}
}

func TestApplyDecision_RejectsDoubleApply(t *testing.T) {
	v := &Vocabulary{}
	store := NewMemoryStore()
	list := &SuggestionList{
		Suggestions: []Suggestion{
			{ID: "s-3", Kind: KindDelete, From: "x", Status: StatusApproved},
		},
	}
	_, err := ApplyDecision(context.Background(), v, list, store, "s-3", "op")
	if err == nil || err != ErrSuggestionDecided {
		t.Errorf("re-applying decided suggestion must return ErrSuggestionDecided, got %v", err)
	}
}

func TestRejectDecision_StampsAuditEntry(t *testing.T) {
	list := &SuggestionList{}
	list.Add(Suggestion{ID: "s-4", Kind: KindMerge, From: "a", To: "b", Status: StatusPending})
	dec, err := RejectDecision(list, "s-4", "op", "kept-distinct-on-purpose")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != string(StatusRejected) || dec.Reason != "kept-distinct-on-purpose" {
		t.Errorf("Decision = %+v", dec)
	}
	if list.Suggestions[0].Status != StatusRejected {
		t.Errorf("suggestion not flipped to rejected")
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
