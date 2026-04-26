package enrichment

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMemoryStore_LoadMissingReturnsErrNotFound(t *testing.T) {
	s := NewMemoryStore(nil)
	_, err := s.Load(context.Background(), "gm-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing bead should return ErrNotFound; got %v", err)
	}
}

func TestMemoryStore_SaveLoadRoundTrip(t *testing.T) {
	s := NewMemoryStore(nil)
	want := Enrichment{
		BeadID:   "gm-1",
		Targets:  []string{"a.go"},
		Concepts: []string{"auth"},
		Source:   SourceOperator,
	}
	if err := s.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(context.Background(), "gm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("Save should stamp UpdatedAt when the input leaves it zero")
	}
	got.UpdatedAt = time.Time{} // ignore for comparison
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMemoryStore_SaveCopiesSoCallerCantMutate(t *testing.T) {
	s := NewMemoryStore(nil)
	in := Enrichment{BeadID: "gm-1", Targets: []string{"a.go"}}
	if err := s.Save(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Targets[0] = "MUTATED"

	got, _ := s.Load(context.Background(), "gm-1")
	if got.Targets[0] == "MUTATED" {
		t.Errorf("MemoryStore.Save aliases the caller's slice: %v", got.Targets)
	}
}

func TestMemoryStore_RequiresBeadID(t *testing.T) {
	s := NewMemoryStore(nil)
	if err := s.Save(context.Background(), Enrichment{}); err == nil {
		t.Error("Save without BeadID must error")
	}
	if _, err := s.Load(context.Background(), ""); err == nil {
		t.Error("Load with empty id must error")
	}
}

func TestMemoryStore_ListSortedAscending(t *testing.T) {
	s := NewMemoryStore(nil)
	for _, id := range []string{"b", "c", "a"} {
		_ = s.Save(context.Background(), Enrichment{BeadID: id})
	}
	got, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("List = %v, want [a b c]", got)
	}
}

func TestMemoryStore_EmptyEnrichmentStillPersists(t *testing.T) {
	// Save({BeadID, no targets, no concepts}) must persist so a
	// later Load returns the empty value rather than ErrNotFound.
	// This is how "explicitly cleared" differs from "never set."
	s := NewMemoryStore(nil)
	_ = s.Save(context.Background(), Enrichment{BeadID: "gm-1"})
	got, err := s.Load(context.Background(), "gm-1")
	if err != nil {
		t.Errorf("explicit-clear Save should persist; Load got %v", err)
	}
	if !got.IsZero() {
		t.Errorf("explicitly-cleared bead should round-trip empty; got %+v", got)
	}
}

func TestMemoryStore_ConcurrentWrites(t *testing.T) {
	// Lightweight race smoke. The store advertises concurrent-safe;
	// run with -race to catch real issues.
	s := NewMemoryStore(nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Save(context.Background(), Enrichment{
				BeadID:  "gm-" + itoa(i),
				Targets: []string{"a.go"},
			})
		}(i)
	}
	wg.Wait()
	got, _ := s.List(context.Background())
	if len(got) != 20 {
		t.Errorf("expected 20 saved beads, got %d", len(got))
	}
}

func TestFileStore_RoundTripAndAtomicity(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root, nil)
	want := Enrichment{
		BeadID:   "gemba/gemba/gm-1",
		Targets:  []string{"internal/auth/"},
		Concepts: []string{"auth"},
		Source:   SourceOperator,
	}
	if err := s.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(context.Background(), want.BeadID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BeadID != want.BeadID {
		t.Errorf("round-trip lost id: %s", got.BeadID)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be stamped")
	}
}

func TestFileStore_PathEscapesSlashedIDs(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root, nil)
	if err := s.Save(context.Background(), Enrichment{BeadID: "gemba/gemba/gm-1"}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "gemba/gemba/gm-1" {
		t.Errorf("List should round-trip slashed ids: %v", ids)
	}
}

func TestFileStore_LoadMissingIsErrNotFound(t *testing.T) {
	s := NewFileStore(t.TempDir(), nil)
	_, err := s.Load(context.Background(), "gm-nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file should return ErrNotFound; got %v", err)
	}
}

func TestFileStore_ListEmptyDirReturnsNil(t *testing.T) {
	s := NewFileStore(t.TempDir(), nil)
	got, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("missing dir should List as nil; got %v", got)
	}
}

func TestFileStore_FixedNowHookStamps(t *testing.T) {
	when := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	s := NewFileStore(t.TempDir(), func() time.Time { return when })
	_ = s.Save(context.Background(), Enrichment{BeadID: "gm-1"})
	got, _ := s.Load(context.Background(), "gm-1")
	if !got.UpdatedAt.Equal(when) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, when)
	}
}

// itoa is the same tiny helper review.go uses; copied here so tests
// stay free of cross-package imports.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
