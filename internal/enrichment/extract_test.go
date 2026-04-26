package enrichment

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestNoopExtractor_StampsBeadIDAndSource(t *testing.T) {
	got, err := NoopExtractor{}.Extract(context.Background(), BeadInput{BeadID: "gm-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.BeadID != "gm-1" {
		t.Errorf("BeadID not propagated: %+v", got)
	}
	if got.Source != SourceLLM {
		t.Errorf("Source = %q, want SourceLLM", got.Source)
	}
	if !got.IsZero() {
		t.Errorf("noop should produce no targets/concepts: %+v", got)
	}
}

func TestHeuristicExtractor_FencedPathsAlwaysWin(t *testing.T) {
	in := BeadInput{
		BeadID: "gm-1",
		Title:  "Fix auth bug",
		Body:   "Touches `internal/auth/auth.go` and `web/src/Topbar.tsx`.",
	}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	want := []string{"internal/auth/auth.go", "web/src/Topbar.tsx"}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %v, want %v", got.Targets, want)
	}
}

func TestHeuristicExtractor_BarePathsRequirePrefix(t *testing.T) {
	in := BeadInput{
		BeadID: "gm-1",
		Body:   "Edits internal/auth/auth.go and v1.2.3 (a version, not a file)",
	}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	if !contains(got.Targets, "internal/auth/auth.go") {
		t.Errorf("expected internal/ path; got %v", got.Targets)
	}
	if contains(got.Targets, "v1.2.3") {
		t.Errorf("version string leaked into targets: %v", got.Targets)
	}
}

func TestHeuristicExtractor_RejectsVersionTokensInsideFences(t *testing.T) {
	// `v1.2.3` is a single segment without an extension — looksLikePath
	// rejects it.
	in := BeadInput{BeadID: "gm-1", Body: "Bumped to `v1.2.3`."}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	if len(got.Targets) != 0 {
		t.Errorf("version token should not be a target: %v", got.Targets)
	}
}

func TestHeuristicExtractor_PrefixOverrideSubsetsBareMatches(t *testing.T) {
	in := BeadInput{
		BeadID: "gm-1",
		Body:   "Touch internal/auth/auth.go and ops/deploy/script.sh",
	}
	// Limit the operator's prefix set to ops/. The internal path should
	// disappear from bare matches; only ops/ remains.
	h := HeuristicExtractor{PathPrefixes: []string{"ops/"}}
	got, _ := h.Extract(context.Background(), in)
	if contains(got.Targets, "internal/auth/auth.go") {
		t.Errorf("internal/ should be filtered out by prefix override: %v", got.Targets)
	}
	if !contains(got.Targets, "ops/deploy/script.sh") {
		t.Errorf("ops/ should pass; got %v", got.Targets)
	}
}

func TestHeuristicExtractor_MaxTargetsCaps(t *testing.T) {
	in := BeadInput{
		BeadID: "gm-1",
		Body: strings.Join([]string{
			"`internal/a/a.go`", "`internal/b/b.go`", "`internal/c/c.go`",
			"`internal/d/d.go`", "`internal/e/e.go`",
		}, " "),
	}
	got, _ := HeuristicExtractor{MaxTargets: 2}.Extract(context.Background(), in)
	if len(got.Targets) != 2 {
		t.Errorf("MaxTargets=2 should cap; got %d (%v)", len(got.Targets), got.Targets)
	}
}

func TestHeuristicExtractor_ConceptsFromVocabulary(t *testing.T) {
	in := BeadInput{
		BeadID:     "gm-1",
		Title:      "Auth flow rewrite",
		Body:       "Migrating to react-query and dropping the old SWR client.",
		Vocabulary: []string{"auth", "react-query", "dolt", "swr"},
	}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	want := []string{"auth", "react-query", "swr"}
	sort.Strings(want)
	if !reflect.DeepEqual(got.Concepts, want) {
		t.Errorf("Concepts = %v, want %v", got.Concepts, want)
	}
	if contains(got.Concepts, "dolt") {
		t.Errorf("dolt was not in the corpus; should not have matched: %v", got.Concepts)
	}
}

func TestHeuristicExtractor_ConceptMatchesAreWordBoundary(t *testing.T) {
	// "auth" is in the vocabulary but the corpus only mentions
	// "author" — must NOT match.
	in := BeadInput{
		BeadID:     "gm-1",
		Body:       "Author of the design doc agrees.",
		Vocabulary: []string{"auth"},
	}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	if len(got.Concepts) != 0 {
		t.Errorf("auth must not match author: %v", got.Concepts)
	}
}

func TestHeuristicExtractor_ConceptCaseInsensitive(t *testing.T) {
	in := BeadInput{
		BeadID:     "gm-1",
		Body:       "AUTH refactor; React Query swap.",
		Vocabulary: []string{"auth", "react-query"},
	}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	if !contains(got.Concepts, "auth") {
		t.Errorf("AUTH should match auth; got %v", got.Concepts)
	}
	if !contains(got.Concepts, "react-query") {
		t.Errorf("'React Query' should match react-query: %v", got.Concepts)
	}
}

func TestHeuristicExtractor_EmptyVocabularyMeansNoConcepts(t *testing.T) {
	in := BeadInput{BeadID: "gm-1", Body: "auth react-query dolt"}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	if len(got.Concepts) != 0 {
		t.Errorf("no vocabulary → no concepts; got %v", got.Concepts)
	}
}

func TestHeuristicExtractor_DeduplicatesAcrossSpecAndBody(t *testing.T) {
	in := BeadInput{
		BeadID:     "gm-1",
		Body:       "Touches `internal/auth/auth.go`. Concept: auth.",
		Spec:       "See `internal/auth/auth.go` (auth subsystem).",
		Vocabulary: []string{"auth"},
	}
	got, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	count := 0
	for _, t := range got.Targets {
		if t == "internal/auth/auth.go" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("path mentioned twice should land once: %v", got.Targets)
	}
	if len(got.Concepts) != 1 || got.Concepts[0] != "auth" {
		t.Errorf("auth mentioned twice should land once: %v", got.Concepts)
	}
}

func TestHeuristicExtractor_StampsSourceLLM(t *testing.T) {
	got, _ := HeuristicExtractor{}.Extract(context.Background(),
		BeadInput{BeadID: "gm-1", Body: "no signal"})
	if got.Source != SourceLLM {
		t.Errorf("Source = %q, want SourceLLM (heuristic = automated)", got.Source)
	}
}

func TestHeuristicExtractor_ConceptOrderStable(t *testing.T) {
	// Ordering must be deterministic so retrospective grading
	// (gm-s47n.8) can compare runs without false positives.
	in := BeadInput{
		BeadID:     "gm-1",
		Body:       "auth react-query dolt",
		Vocabulary: []string{"dolt", "auth", "react-query"},
	}
	first, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	second, _ := HeuristicExtractor{}.Extract(context.Background(), in)
	if !reflect.DeepEqual(first.Concepts, second.Concepts) {
		t.Errorf("nondeterministic concepts: %v vs %v", first.Concepts, second.Concepts)
	}
}
