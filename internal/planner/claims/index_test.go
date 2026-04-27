package claims

import (
	"sync"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

func sampleClaim(bead core.WorkItemID, sess string, at time.Time) Claim {
	return Claim{BeadID: bead, SessionID: sess, ClaimedAt: at}
}

// ── Set / Get / Clear ────────────────────────────────────────

func TestSet_StoresAndGetsBack(t *testing.T) {
	i := NewIndex()
	at := mustParse(t, "2026-04-26T20:00:00Z")
	i.Set(sampleClaim("gm-1", "sess-A", at))

	got, ok := i.Get("gm-1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.SessionID != "sess-A" || !got.ClaimedAt.Equal(at) {
		t.Errorf("got %+v", got)
	}
}

func TestSet_RejectsEmptyFields(t *testing.T) {
	i := NewIndex()
	i.Set(Claim{})                    // empty bead and session
	i.Set(Claim{BeadID: "gm-1"})      // empty session
	i.Set(Claim{SessionID: "sess-A"}) // empty bead
	if i.Len() != 0 {
		t.Errorf("partial claims should be ignored; len=%d", i.Len())
	}
}

func TestSet_ReplacesExistingClaim(t *testing.T) {
	i := NewIndex()
	at1 := mustParse(t, "2026-04-26T20:00:00Z")
	at2 := mustParse(t, "2026-04-26T21:00:00Z")
	i.Set(sampleClaim("gm-1", "sess-A", at1))
	i.Set(sampleClaim("gm-1", "sess-B", at2))

	got, _ := i.Get("gm-1")
	if got.SessionID != "sess-B" || !got.ClaimedAt.Equal(at2) {
		t.Errorf("expected replacement; got %+v", got)
	}
	if i.Len() != 1 {
		t.Errorf("len = %d, want 1", i.Len())
	}
}

func TestClear_RemovesEntry(t *testing.T) {
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now()))
	i.Clear("gm-1")
	if _, ok := i.Get("gm-1"); ok {
		t.Error("expected miss after clear")
	}
}

func TestClear_IsIdempotent(t *testing.T) {
	i := NewIndex()
	i.Clear("gm-never-existed")
	i.Clear("")
	// no panic, no error — index is unchanged
	if i.Len() != 0 {
		t.Errorf("idempotent clear should leave empty; len=%d", i.Len())
	}
}

func TestGet_MissReturnsFalse(t *testing.T) {
	i := NewIndex()
	if _, ok := i.Get("gm-missing"); ok {
		t.Error("expected miss")
	}
}

// ── Build (bulk hydrate) ─────────────────────────────────────

func TestBuild_ReplacesEntireIndex(t *testing.T) {
	i := NewIndex()
	now := time.Now()
	i.Set(sampleClaim("gm-old", "sess-X", now))
	i.Build([]Claim{
		sampleClaim("gm-1", "sess-A", now),
		sampleClaim("gm-2", "sess-B", now),
	})
	if _, ok := i.Get("gm-old"); ok {
		t.Error("Build must drop pre-existing entries not in snapshot")
	}
	if i.Len() != 2 {
		t.Errorf("len = %d, want 2", i.Len())
	}
}

func TestBuild_FiltersInvalidEntries(t *testing.T) {
	i := NewIndex()
	now := time.Now()
	i.Build([]Claim{
		sampleClaim("gm-1", "sess-A", now),
		{BeadID: "gm-2"},      // missing session
		{SessionID: "sess-B"}, // missing bead
	})
	if i.Len() != 1 {
		t.Errorf("len = %d, want 1 (invalid filtered)", i.Len())
	}
}

func TestBuild_NilSnapshotClearsIndex(t *testing.T) {
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now()))
	i.Build(nil)
	if i.Len() != 0 {
		t.Errorf("nil snapshot should clear; len=%d", i.Len())
	}
}

// ── SoftConflict ──────────────────────────────────────────────

func TestSoftConflict_TrueForOtherSession(t *testing.T) {
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now()))
	if !i.SoftConflict("gm-1", "sess-B") {
		t.Error("expected conflict from sess-B's perspective")
	}
}

func TestSoftConflict_FalseForClaimingSession(t *testing.T) {
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now()))
	if i.SoftConflict("gm-1", "sess-A") {
		t.Error("claiming session should not conflict with its own claim")
	}
}

func TestSoftConflict_FalseForUnclaimedBead(t *testing.T) {
	i := NewIndex()
	if i.SoftConflict("gm-untouched", "sess-A") {
		t.Error("unclaimed bead must not conflict")
	}
}

func TestSoftConflict_EmptySessionTreatsAllAsConflict(t *testing.T) {
	// "the planner doesn't know who's asking" — every claim
	// looks like a conflict.
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now()))
	if !i.SoftConflict("gm-1", "") {
		t.Error("empty session id should treat claim as conflict")
	}
}

// ── Snapshot ──────────────────────────────────────────────────

func TestSnapshot_SortedByBeadID(t *testing.T) {
	i := NewIndex()
	now := time.Now()
	i.Set(sampleClaim("gm-z", "sess-A", now))
	i.Set(sampleClaim("gm-a", "sess-B", now))
	i.Set(sampleClaim("gm-m", "sess-C", now))
	got := i.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []core.WorkItemID{"gm-a", "gm-m", "gm-z"}
	for idx := range want {
		if got[idx].BeadID != want[idx] {
			t.Errorf("[%d] = %s, want %s", idx, got[idx].BeadID, want[idx])
		}
	}
}

func TestSnapshot_IsCopySafeToMutate(t *testing.T) {
	i := NewIndex()
	now := time.Now()
	i.Set(sampleClaim("gm-1", "sess-A", now))
	got := i.Snapshot()
	got[0].SessionID = "tampered"
	live, _ := i.Get("gm-1")
	if live.SessionID == "tampered" {
		t.Error("Snapshot mutation leaked back into the index")
	}
}

// ── Stale + Reap ──────────────────────────────────────────────

func TestStale_FiltersOlderThanCutoff(t *testing.T) {
	i := NewIndex()
	now := mustParse(t, "2026-04-26T20:00:00Z")
	i.Set(sampleClaim("gm-fresh", "sess-A", now.Add(-30*time.Minute)))
	i.Set(sampleClaim("gm-stale", "sess-B", now.Add(-5*time.Hour)))
	got := i.Stale(now, 2*time.Hour)
	if len(got) != 1 || got[0].BeadID != "gm-stale" {
		t.Errorf("got %+v", got)
	}
}

func TestStale_OldestFirst(t *testing.T) {
	i := NewIndex()
	now := mustParse(t, "2026-04-26T20:00:00Z")
	i.Set(sampleClaim("gm-old", "sess-A", now.Add(-10*time.Hour)))
	i.Set(sampleClaim("gm-older", "sess-B", now.Add(-20*time.Hour)))
	got := i.Stale(now, time.Hour)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].BeadID != "gm-older" {
		t.Errorf("oldest must come first; got %+v", got)
	}
}

func TestStale_NoOpWhenTTLZero(t *testing.T) {
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now().Add(-100*time.Hour)))
	if got := i.Stale(time.Now(), 0); len(got) != 0 {
		t.Errorf("ttl=0 should opt out; got %+v", got)
	}
}

func TestStale_NoOpWhenNowZero(t *testing.T) {
	i := NewIndex()
	i.Set(sampleClaim("gm-1", "sess-A", time.Now()))
	if got := i.Stale(time.Time{}, time.Hour); len(got) != 0 {
		t.Errorf("zero now should opt out; got %+v", got)
	}
}

func TestReap_ClearsStaleClaims(t *testing.T) {
	i := NewIndex()
	now := mustParse(t, "2026-04-26T20:00:00Z")
	i.Set(sampleClaim("gm-fresh", "sess-A", now.Add(-30*time.Minute)))
	i.Set(sampleClaim("gm-stale", "sess-B", now.Add(-5*time.Hour)))
	reaped := i.Reap(now, 2*time.Hour)
	if len(reaped) != 1 || reaped[0].BeadID != "gm-stale" {
		t.Errorf("reaped = %+v", reaped)
	}
	if _, ok := i.Get("gm-stale"); ok {
		t.Error("stale entry should be cleared after reap")
	}
	if _, ok := i.Get("gm-fresh"); !ok {
		t.Error("fresh entry should survive reap")
	}
}

// ── Concurrency ───────────────────────────────────────────────

func TestIndex_ConcurrentReadsAndWritesDontRace(t *testing.T) {
	// Run with -race to catch a missing lock.
	i := NewIndex()
	var wg sync.WaitGroup
	const N = 200
	for k := 0; k < N; k++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := core.WorkItemID(string(rune('a' + n%26)))
			i.Set(Claim{BeadID: id, SessionID: "s", ClaimedAt: time.Now()})
			_ = i.SoftConflict(id, "other")
			_, _ = i.Get(id)
			if n%3 == 0 {
				i.Clear(id)
			}
			_ = i.Snapshot()
		}(k)
	}
	wg.Wait()
}

// ── nil receiver ──────────────────────────────────────────────

func TestNilIndex_AllMethodsNoOpOrZero(t *testing.T) {
	var i *Index
	if i.Len() != 0 {
		t.Error("nil index Len should be 0")
	}
	if _, ok := i.Get("gm-1"); ok {
		t.Error("nil index Get should miss")
	}
	if i.SoftConflict("gm-1", "sess-A") {
		t.Error("nil index SoftConflict should be false")
	}
	if got := i.Snapshot(); got != nil {
		t.Errorf("nil index Snapshot should be nil; got %+v", got)
	}
	// Set / Clear / Build / Reap on nil must not panic.
	i.Set(Claim{BeadID: "gm-1", SessionID: "sess-A", ClaimedAt: time.Now()})
	i.Clear("gm-1")
	i.Build(nil)
	if got := i.Reap(time.Now(), time.Hour); got != nil {
		t.Errorf("nil index Reap should be nil; got %+v", got)
	}
}
