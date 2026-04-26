package enrichment

import (
	"context"
	"errors"
	"testing"
)

// fakeExtractor is a deterministic Extractor used in backfill tests.
// emit controls per-bead output so tests can assert dedup, error
// surfacing, etc.
type fakeExtractor struct {
	emit map[string]Enrichment
	fail map[string]error
}

func (f *fakeExtractor) Extract(_ context.Context, in BeadInput) (Enrichment, error) {
	if err, ok := f.fail[in.BeadID]; ok {
		return Enrichment{}, err
	}
	if e, ok := f.emit[in.BeadID]; ok {
		e.BeadID = in.BeadID
		return e, nil
	}
	return Enrichment{BeadID: in.BeadID}, nil
}

func TestBackfill_HappyPathWritesEveryBead(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-1", Title: "a", Body: "b"},
		{BeadID: "gm-2", Title: "c", Body: "d"},
	}}
	store := NewMemoryStore(nil)
	ext := &fakeExtractor{emit: map[string]Enrichment{
		"gm-1": {Targets: []string{"a.go"}},
		"gm-2": {Concepts: []string{"auth"}},
	}}
	rep, err := Backfill(context.Background(), src, ext, store, BackfillOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Considered != 2 || rep.Extracted != 2 || len(rep.Errors) != 0 {
		t.Errorf("report = %+v, want 2/2/0", rep)
	}
	for _, id := range []string{"gm-1", "gm-2"} {
		got, err := store.Load(context.Background(), id)
		if err != nil {
			t.Errorf("missing %s: %v", id, err)
			continue
		}
		if got.Source != SourceBackfill {
			t.Errorf("%s.Source = %q, want SourceBackfill", id, got.Source)
		}
	}
}

func TestBackfill_SkipExistingPreservesOperatorPins(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-1"}, {BeadID: "gm-2"},
	}}
	store := NewMemoryStore(nil)
	// gm-1 already has operator-pinned enrichment; backfill must
	// not clobber it.
	_ = store.Save(context.Background(), Enrichment{
		BeadID:   "gm-1",
		Targets:  []string{"keep.go"},
		Concepts: []string{"operator-set"},
		Source:   SourceOperator,
	})
	ext := &fakeExtractor{emit: map[string]Enrichment{
		"gm-1": {Targets: []string{"would-overwrite.go"}},
		"gm-2": {Targets: []string{"new.go"}},
	}}
	_, err := Backfill(context.Background(), src, ext, store,
		BackfillOpts{SkipExisting: true})
	if err != nil {
		t.Fatal(err)
	}
	got1, _ := store.Load(context.Background(), "gm-1")
	if !contains(got1.Targets, "keep.go") {
		t.Errorf("operator pin overwritten: %v", got1.Targets)
	}
	if contains(got1.Targets, "would-overwrite.go") {
		t.Errorf("backfill should have skipped gm-1; got %v", got1.Targets)
	}
	got2, _ := store.Load(context.Background(), "gm-2")
	if !contains(got2.Targets, "new.go") {
		t.Errorf("backfill should have written gm-2; got %+v", got2)
	}
}

func TestBackfill_DryRunDoesNotPersist(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{{BeadID: "gm-1"}}}
	store := NewMemoryStore(nil)
	ext := &fakeExtractor{emit: map[string]Enrichment{
		"gm-1": {Targets: []string{"a.go"}},
	}}
	rep, err := Backfill(context.Background(), src, ext, store,
		BackfillOpts{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Extracted != 1 {
		t.Errorf("Extracted = %d, want 1", rep.Extracted)
	}
	if _, err := store.Load(context.Background(), "gm-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("dry-run must not persist; got %v", err)
	}
}

func TestBackfill_LimitCapsExtracted(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-1"}, {BeadID: "gm-2"}, {BeadID: "gm-3"},
	}}
	store := NewMemoryStore(nil)
	rep, err := Backfill(context.Background(), src,
		&fakeExtractor{emit: map[string]Enrichment{
			"gm-1": {Targets: []string{"a"}},
			"gm-2": {Targets: []string{"b"}},
			"gm-3": {Targets: []string{"c"}},
		}}, store, BackfillOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Limit caps the number of beads we attempt to extract — so
	// only 2 land in the store. Considered counts every iteration
	// (the runner has to walk past them to check the filter).
	if rep.Extracted != 2 {
		t.Errorf("Limit=2 should cap Extracted; got %d", rep.Extracted)
	}
	ids, _ := store.List(context.Background())
	if len(ids) != 2 {
		t.Errorf("expected 2 saved beads; got %d", len(ids))
	}
}

func TestBackfill_LimitAndFilterCompose(t *testing.T) {
	// The combination operators reach for: a regex narrows scope
	// to a slice, then --limit caps that slice. The limit must
	// apply AFTER the filter so the cap doesn't consume non-
	// matching beads at the head of the list.
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-other-aaa"},
		{BeadID: "gm-other-bbb"},
		{BeadID: "gm-s47n-1"},
		{BeadID: "gm-s47n-2"},
		{BeadID: "gm-s47n-3"},
	}}
	rep, err := Backfill(context.Background(), src,
		&fakeExtractor{emit: map[string]Enrichment{
			"gm-s47n-1": {Targets: []string{"a"}},
			"gm-s47n-2": {Targets: []string{"b"}},
			"gm-s47n-3": {Targets: []string{"c"}},
		}}, NewMemoryStore(nil),
		BackfillOpts{Limit: 2, FilterRegex: "^gm-s47n"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Extracted != 2 {
		t.Errorf("filter+limit should yield 2 extracted; got %+v", rep)
	}
}

func TestBackfill_FilterRegexNarrowsScope(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-s47n-aaa"}, {BeadID: "gm-other-bbb"}, {BeadID: "gm-s47n-ccc"},
	}}
	store := NewMemoryStore(nil)
	rep, err := Backfill(context.Background(), src,
		&fakeExtractor{emit: map[string]Enrichment{
			"gm-s47n-aaa": {Targets: []string{"a"}},
			"gm-other-bbb": {Targets: []string{"b"}},
			"gm-s47n-ccc": {Targets: []string{"c"}},
		}}, store, BackfillOpts{FilterRegex: "^gm-s47n"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Extracted != 2 || rep.Skipped != 1 {
		t.Errorf("filter report = %+v, want 2 extracted / 1 skipped", rep)
	}
}

func TestBackfill_BadRegexErrorsBeforeIter(t *testing.T) {
	_, err := Backfill(context.Background(), &MemoryBeadSource{}, NoopExtractor{},
		NewMemoryStore(nil), BackfillOpts{FilterRegex: "([invalid"})
	if err == nil {
		t.Error("malformed regex must error before iteration")
	}
}

func TestBackfill_EmptyExtractorOutputDoesNotPersist(t *testing.T) {
	// An extractor that legitimately finds nothing for a bead
	// shouldn't pollute the store with empty {targets:[],concepts:[]}
	// rows. The "explicit-clear" gesture is reserved for the CLI.
	src := &MemoryBeadSource{Beads: []BeadInput{{BeadID: "gm-1"}}}
	store := NewMemoryStore(nil)
	rep, err := Backfill(context.Background(), src, NoopExtractor{}, store, BackfillOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Extracted != 0 {
		t.Errorf("noop extractor produced no signal; Extracted should be 0, got %d", rep.Extracted)
	}
	if _, err := store.Load(context.Background(), "gm-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty extraction should leave the store untouched; got %v", err)
	}
}

func TestBackfill_ExtractorErrorRecordsAndContinues(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-1"}, {BeadID: "gm-2"},
	}}
	store := NewMemoryStore(nil)
	rep, err := Backfill(context.Background(),
		src,
		&fakeExtractor{
			fail: map[string]error{"gm-1": errors.New("boom")},
			emit: map[string]Enrichment{"gm-2": {Targets: []string{"a.go"}}},
		},
		store, BackfillOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Extracted != 1 || len(rep.Errors) != 1 {
		t.Errorf("expected 1 extracted + 1 error; got %+v", rep)
	}
	if rep.Errors[0].BeadID != "gm-1" {
		t.Errorf("wrong bead in error: %v", rep.Errors[0])
	}
}

func TestBackfill_NilSourceErrors(t *testing.T) {
	if _, err := Backfill(context.Background(), nil, NoopExtractor{},
		NewMemoryStore(nil), BackfillOpts{}); err == nil {
		t.Error("nil source must error")
	}
}

func TestBackfill_NilStoreErrors(t *testing.T) {
	if _, err := Backfill(context.Background(), &MemoryBeadSource{}, NoopExtractor{}, nil,
		BackfillOpts{}); err == nil {
		t.Error("nil store must error")
	}
}

func TestBackfill_ContextCancelStopsLoop(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-1"}, {BeadID: "gm-2"},
	}}
	store := NewMemoryStore(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel — first iter check should bail.
	rep, err := Backfill(ctx, src, NoopExtractor{}, store, BackfillOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Considered > 1 {
		t.Errorf("cancelled context should stop after the first considered bead; got %d", rep.Considered)
	}
}

func TestBdJSONSource_StubExecParses(t *testing.T) {
	calls := 0
	src := &BdJSONSource{
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls++
			if name != "bd" {
				t.Errorf("expected bd binary; got %q", name)
			}
			if args[0] != "list" || args[1] != "--json" {
				t.Errorf("unexpected args: %v", args)
			}
			return []byte(`[
				{"id":"gm-1","title":"first","description":"body 1"},
				{"id":"gm-2","title":"second","description":"body 2"}
			]`), nil
		},
	}
	collected := []BeadInput{}
	err := src.Iter(context.Background(), func(in BeadInput) bool {
		collected = append(collected, in)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 bd call; got %d", calls)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 beads; got %d", len(collected))
	}
	if collected[0].Title != "first" || collected[0].Body != "body 1" {
		t.Errorf("decoded wrong: %+v", collected[0])
	}
}

func TestBdJSONSource_IncludeAllAddsFlag(t *testing.T) {
	src := &BdJSONSource{
		IncludeAll: true,
		exec: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if !contains(args, "--all") {
				t.Errorf("expected --all flag; got %v", args)
			}
			return []byte("[]"), nil
		},
	}
	_ = src.Iter(context.Background(), func(BeadInput) bool { return true })
}

func TestBdJSONSource_EmptyOutputYieldsNothing(t *testing.T) {
	src := &BdJSONSource{
		exec: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte(""), nil
		},
	}
	count := 0
	err := src.Iter(context.Background(), func(BeadInput) bool {
		count++
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected no yields on empty bd output; got %d", count)
	}
}

func TestBdJSONSource_ExecFailureSurfacesError(t *testing.T) {
	src := &BdJSONSource{
		exec: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("subprocess explosion")
		},
	}
	err := src.Iter(context.Background(), func(BeadInput) bool { return true })
	if err == nil {
		t.Fatal("expected error to surface")
	}
}

func TestMemoryBeadSource_StopsWhenYieldReturnsFalse(t *testing.T) {
	src := &MemoryBeadSource{Beads: []BeadInput{
		{BeadID: "gm-1"}, {BeadID: "gm-2"}, {BeadID: "gm-3"},
	}}
	count := 0
	err := src.Iter(context.Background(), func(_ BeadInput) bool {
		count++
		return count < 2
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("yield-stop ignored; got %d iterations", count)
	}
}
