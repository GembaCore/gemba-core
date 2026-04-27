package phase

import (
	"context"
	"errors"
	"sync"
	"time"
)

// WorkspaceState is the workspace-level Phase record. One instance per
// workspace; the Store interface persists the latest snapshot.
//
// History is the append-only audit trail; the most recent transition
// is the last element. Phase + Since track the current state and the
// time the workspace entered it (i.e. the last Transition.At, or the
// initial-construction time when History is empty).
type WorkspaceState struct {
	// Phase is the workspace's current Phase. Always valid by
	// construction — Advance rejects invalid targets and InitialState
	// rejects an invalid initial Phase.
	Phase Phase `json:"phase"`
	// Since is the time the workspace entered Phase. Equals the last
	// History entry's At, or the time InitialState was called when
	// History is empty.
	Since time.Time `json:"since"`
	// History is the append-only transition log, oldest first. Pure
	// audit data — Advance is the only function that grows it.
	History []Transition `json:"history,omitempty"`
}

// Clone returns a deep copy of s. Used by the in-memory Store so
// callers cannot mutate persisted state through a returned reference.
func (s WorkspaceState) Clone() WorkspaceState {
	out := WorkspaceState{
		Phase: s.Phase,
		Since: s.Since,
	}
	if len(s.History) > 0 {
		out.History = make([]Transition, len(s.History))
		copy(out.History, s.History)
	}
	return out
}

// InitialState returns a fresh WorkspaceState seeded at the given
// Phase with no transition history. The empty Phase normalises to
// PhaseIdeation so callers building a state from a zero-value config
// land somewhere defensible. An explicit-but-invalid Phase returns an
// error.
func InitialState(p Phase, now time.Time) (WorkspaceState, error) {
	if p == "" {
		p = PhaseIdeation
	}
	if err := p.Validate(); err != nil {
		return WorkspaceState{}, err
	}
	return WorkspaceState{
		Phase: p,
		Since: now.UTC(),
	}, nil
}

// Store is the persistence interface for WorkspaceState. All methods
// are context-aware so a future SQL-backed implementation can honour
// deadlines and cancellation. The in-memory implementation
// (NewMemoryStore) is the gm-jt9 deliverable; a SQL-backed Store
// lands when the Manager HITL session store does.
type Store interface {
	// Get returns the current WorkspaceState. Returns ErrNotFound if
	// no state has been seeded yet — callers should call Set with an
	// InitialState before the first Get.
	Get(ctx context.Context) (WorkspaceState, error)

	// Set replaces the persisted state with s. The state is stored as
	// a deep copy so callers may continue to mutate s afterwards
	// without affecting the store. Used by Advance to commit a new
	// transition and by boot-time seeding.
	Set(ctx context.Context, s WorkspaceState) error
}

// ErrNotFound is returned by Store.Get before the workspace state has
// been seeded. The HTTP layer returns 503 in that case; tests that
// don't seed a state can detect the no-state path with errors.Is.
var ErrNotFound = errors.New("phase: workspace state not initialised")

// MemoryStore is an in-process Store implementation. Safe for
// concurrent use; the SQL-backed Store will satisfy the same contract.
type MemoryStore struct {
	mu     sync.RWMutex
	loaded bool
	state  WorkspaceState
}

// NewMemoryStore returns an empty MemoryStore. Callers must call Set
// (typically with InitialState) before the first Get; until then Get
// returns ErrNotFound.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// NewSeededMemoryStore is sugar for "construct a memory store and
// seed it with the given initial state in one step". Returns the
// store and any validation error from InitialState.
func NewSeededMemoryStore(p Phase, now time.Time) (*MemoryStore, error) {
	s, err := InitialState(p, now)
	if err != nil {
		return nil, err
	}
	store := NewMemoryStore()
	_ = store.Set(context.Background(), s)
	return store, nil
}

// Get returns a copy of the persisted state. Returns ErrNotFound when
// Set has not been called yet.
func (m *MemoryStore) Get(_ context.Context) (WorkspaceState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.loaded {
		return WorkspaceState{}, ErrNotFound
	}
	return m.state.Clone(), nil
}

// Set replaces the persisted state with a deep copy of s.
func (m *MemoryStore) Set(_ context.Context, s WorkspaceState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s.Clone()
	m.loaded = true
	return nil
}
