// claim_index implementation (gm-v5z2.6).

package claims

import (
	"sort"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// Claim is one entry in the index — "session S has been working
// bead B since time T." A bead can have at most one Claim at a
// time; Set on an already-claimed bead replaces the entry (the
// adaptor's session lifecycle is what enforces single-claim
// invariants upstream — the index just reflects the latest
// observation).
type Claim struct {
	BeadID    core.WorkItemID `json:"bead_id"`
	SessionID string          `json:"session_id"`
	ClaimedAt time.Time       `json:"claimed_at"`
}

// Index is the per-rig claim_index. Cheap in-memory map with an
// embedded RWMutex; safe for concurrent reads from the planner
// hot path while the orchestration plane writes on session
// lifecycle events.
type Index struct {
	mu     sync.RWMutex
	byBead map[core.WorkItemID]Claim
}

// NewIndex returns an empty Index. Callers seed it via Build (bulk
// hydration on startup) or Set (live event-driven updates).
func NewIndex() *Index {
	return &Index{byBead: make(map[core.WorkItemID]Claim)}
}

// Build replaces the entire index with the given snapshot. Used on
// startup (initial hydration from session_assignments) and on
// reconciliation passes when the orchestration plane has reason
// to believe the in-memory view drifted.
//
// A nil / empty snapshot clears the index entirely — callers that
// want a "merge" semantic should call Set per entry instead.
func (i *Index) Build(snapshot []Claim) {
	if i == nil {
		return
	}
	next := make(map[core.WorkItemID]Claim, len(snapshot))
	for _, c := range snapshot {
		if c.BeadID == "" || c.SessionID == "" {
			continue
		}
		next[c.BeadID] = c
	}
	i.mu.Lock()
	i.byBead = next
	i.mu.Unlock()
}

// Set records a single claim. Empty BeadID or SessionID is a no-op
// — the orchestration plane sometimes emits partial events during
// shutdown / restart and the index should ignore those rather
// than poison itself.
func (i *Index) Set(c Claim) {
	if i == nil || c.BeadID == "" || c.SessionID == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.byBead == nil {
		i.byBead = make(map[core.WorkItemID]Claim)
	}
	i.byBead[c.BeadID] = c
}

// Clear removes a bead's claim. No-op when the bead has no claim
// — the orchestration plane may emit duplicate session-end events
// during recovery and Clear must be idempotent.
func (i *Index) Clear(beadID core.WorkItemID) {
	if i == nil || beadID == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.byBead, beadID)
}

// Get returns the claim for beadID and whether one exists. The
// returned Claim is a value copy; callers may inspect freely.
func (i *Index) Get(beadID core.WorkItemID) (Claim, bool) {
	if i == nil {
		return Claim{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	c, ok := i.byBead[beadID]
	return c, ok
}

// Len returns the current number of claims. Useful for the
// observability surface (gemba session-status, future metrics).
func (i *Index) Len() int {
	if i == nil {
		return 0
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.byBead)
}

// SoftConflict is the Selection gate predicate (work-planning §4
// Layer 5.2 gate 2). Returns true when the bead is claimed by
// SOME OTHER session — i.e. the bead is excluded from this
// session's selection.
//
// A claim by THIS session is NOT a conflict (the claiming session
// keeps the bead). An unclaimed bead is also not a conflict.
//
// Edge case: empty sessionID is treated as "the planner doesn't
// know who's asking" — every claim looks like a conflict in that
// case. Callers in the dispatch path should always thread the
// real session id through.
func (i *Index) SoftConflict(beadID core.WorkItemID, sessionID string) bool {
	c, ok := i.Get(beadID)
	if !ok {
		return false
	}
	return c.SessionID != sessionID
}

// Snapshot returns every current claim sorted by bead id (stable
// for tests / audit log diffing). The slice is a copy — callers
// may mutate it freely.
func (i *Index) Snapshot() []Claim {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	out := make([]Claim, 0, len(i.byBead))
	for _, c := range i.byBead {
		out = append(out, c)
	}
	i.mu.RUnlock()
	sort.Slice(out, func(a, b int) bool { return out[a].BeadID < out[b].BeadID })
	return out
}

// Stale returns every claim whose ClaimedAt is older than now-ttl.
// Used by the reaper: a session that ended without emitting a
// session-end event leaves a stale claim that would otherwise
// block other sessions forever.
//
// ttl <= 0 returns no rows (the reaper opted out). now.IsZero()
// returns no rows (don't mass-evict on a misconfigured timer).
//
// Output is sorted by ClaimedAt ascending so the reaper sees the
// oldest stale claims first.
func (i *Index) Stale(now time.Time, ttl time.Duration) []Claim {
	if i == nil || ttl <= 0 || now.IsZero() {
		return nil
	}
	cutoff := now.Add(-ttl)
	i.mu.RLock()
	out := make([]Claim, 0)
	for _, c := range i.byBead {
		if c.ClaimedAt.Before(cutoff) {
			out = append(out, c)
		}
	}
	i.mu.RUnlock()
	sort.Slice(out, func(a, b int) bool { return out[a].ClaimedAt.Before(out[b].ClaimedAt) })
	return out
}

// Reap clears every claim returned by Stale. Convenience for the
// orchestration plane's periodic timer. Returns the slice of
// reaped claims so the caller can log them.
func (i *Index) Reap(now time.Time, ttl time.Duration) []Claim {
	stale := i.Stale(now, ttl)
	for _, c := range stale {
		i.Clear(c.BeadID)
	}
	return stale
}
