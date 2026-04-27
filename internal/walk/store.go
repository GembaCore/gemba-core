// Walk persistence (gm-rfy, design §Resumability + audit).
//
// The Store interface is the integration point for Manager
// HITL session storage (gm-uf7). gm-rfy ships a thread-safe
// in-memory implementation; the SQL-backed store lands when
// gm-uf7 does, satisfying the same interface.

package walk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// ErrNotFound is returned by Store.Get / Update / End when the
// requested walk id is unknown.
var ErrNotFound = errors.New("walk: not found")

// ErrTerminal is returned when an operation tries to mutate a
// walk that has already reached a terminal state. The walk
// package never resurrects completed walks — the audit log
// would lie if it did.
var ErrTerminal = errors.New("walk: walk is terminal")

// StartParams captures the inputs Store.Start needs. Workspace
// is required (the agenda aggregator + ResumeContext both key
// off it); InitiatedBy is required (audit trail demands a
// caller).
type StartParams struct {
	Workspace         string
	Label             string
	InitiatedBy       core.AgentID
	PrimaryPersona    string
	AssistingPersonas []string
	InitialAgenda     []AgendaItem
	Now               time.Time
}

// Store is the persistence interface. All methods are context-
// aware so a future SQL-backed implementation can honour
// deadlines + cancellation.
//
// Mutation methods (AppendTurn, AppendDecision, etc.) accept a
// pre-built record rather than constructing it from primitives
// — keeps the store narrow and lets the caller decide what
// transcripts / decisions look like in domain terms.
type Store interface {
	// Start creates a new walk in StatusActive, stamps an id,
	// and returns the persisted Walk.
	Start(ctx context.Context, p StartParams) (Walk, error)

	// Get returns the walk by id. Returns ErrNotFound when
	// unknown.
	Get(ctx context.Context, id ID) (Walk, error)

	// List returns walks matching filter, sorted StartedAt
	// desc. Empty filter returns every walk.
	List(ctx context.Context, filter ListFilter) ([]Walk, error)

	// AddAgendaItem appends an item to the walk's agenda. The
	// caller assigns AddedAt + AddedBy + the source-stamped id;
	// the store rejects duplicates by id (idempotent on retry).
	AddAgendaItem(ctx context.Context, id ID, item AgendaItem) (Walk, error)

	// UpdateAgendaItemStatus moves one item between buckets.
	// Refuses to move an item whose Decision has been recorded
	// (decided items are terminal).
	UpdateAgendaItemStatus(ctx context.Context, id ID, itemID string, status AgendaItemStatus) (Walk, error)

	// AppendTurn appends to Transcript. The caller stamps At;
	// the store assigns the turn id when empty.
	AppendTurn(ctx context.Context, id ID, turn WalkTurn) (Walk, error)

	// AppendDecision appends to Decisions and marks the
	// referenced AgendaItem with Status=AgendaDecided +
	// the decision pointer. The caller stamps DecidedAt /
	// DecidedBy.
	AppendDecision(ctx context.Context, id ID, decision WalkDecision) (Walk, error)

	// AddCost increments the running cost. Pure addition;
	// idempotency is the caller's responsibility (the persona
	// dispatcher may pass dedup keys in a future revision).
	AddCost(ctx context.Context, id ID, delta Cost) (Walk, error)

	// SetResumeContext writes the LLM-condensed resume summary.
	// Typically called at pause time.
	SetResumeContext(ctx context.Context, id ID, summary string) (Walk, error)

	// Pause moves StatusActive → StatusPaused. Idempotent: a
	// paused walk pauses again as a no-op.
	Pause(ctx context.Context, id ID) (Walk, error)

	// Resume moves StatusPaused → StatusActive. Returns
	// ErrTerminal when the walk has already ended.
	Resume(ctx context.Context, id ID) (Walk, error)

	// End closes the walk in StatusCompleted (or
	// StatusAbandoned for outcome="abandoned"). Stamps EndedAt
	// from the caller-provided clock.
	End(ctx context.Context, id ID, outcome Status, endedAt time.Time) (Walk, error)
}

// ListFilter narrows Store.List. Zero values mean "no filter".
type ListFilter struct {
	Workspace string
	Statuses  []Status
	// Limit caps the result. 0 → no cap.
	Limit int
}

// Match reports whether the walk satisfies the filter. Helper
// for in-memory + future stores that fall back to client-side
// filtering when the backend can't express the predicate.
func (f ListFilter) Match(w Walk) bool {
	if f.Workspace != "" && f.Workspace != w.Workspace {
		return false
	}
	if len(f.Statuses) > 0 {
		ok := false
		for _, s := range f.Statuses {
			if s == w.Status {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// MemoryStore is an in-memory Store. Safe for concurrent use;
// every method takes the lock for the full operation. Designed
// for tests + early integration before the SQL store ships.
type MemoryStore struct {
	mu     sync.Mutex
	walks  map[ID]*Walk
	NewID  func() ID // injectable for deterministic tests
	NewTID func() string
}

// NewMemoryStore returns an empty MemoryStore with default id
// generators.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		walks:  make(map[ID]*Walk),
		NewID:  defaultWalkID,
		NewTID: defaultTurnID,
	}
}

func (s *MemoryStore) Start(_ context.Context, p StartParams) (Walk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Workspace == "" {
		return Walk{}, errors.New("walk: Start requires Workspace")
	}
	if p.InitiatedBy == "" {
		return Walk{}, errors.New("walk: Start requires InitiatedBy")
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	w := Walk{
		ID:                s.NewID(),
		Workspace:         p.Workspace,
		Label:             p.Label,
		InitiatedBy:       p.InitiatedBy,
		PrimaryPersona:    p.PrimaryPersona,
		AssistingPersonas: append([]string{}, p.AssistingPersonas...),
		StartedAt:         now,
		Status:            StatusActive,
		Agenda:            append([]AgendaItem{}, p.InitialAgenda...),
	}
	s.walks[w.ID] = &w
	return cloneWalk(w), nil
}

func (s *MemoryStore) Get(_ context.Context, id ID) (Walk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.walks[id]
	if !ok {
		return Walk{}, ErrNotFound
	}
	return cloneWalk(*w), nil
}

func (s *MemoryStore) List(_ context.Context, filter ListFilter) ([]Walk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Walk, 0, len(s.walks))
	for _, w := range s.walks {
		if !filter.Match(*w) {
			continue
		}
		out = append(out, cloneWalk(*w))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryStore) AddAgendaItem(_ context.Context, id ID, item AgendaItem) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		for _, existing := range w.Agenda {
			if existing.ID == item.ID {
				return nil // idempotent retry
			}
		}
		w.Agenda = append(w.Agenda, item)
		return nil
	})
}

func (s *MemoryStore) UpdateAgendaItemStatus(_ context.Context, id ID, itemID string, status AgendaItemStatus) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		for i := range w.Agenda {
			if w.Agenda[i].ID != itemID {
				continue
			}
			if w.Agenda[i].Decision != nil {
				return errors.New("walk: cannot move agenda item after decision")
			}
			w.Agenda[i].Status = status
			return nil
		}
		return fmt.Errorf("walk: agenda item %q not found", itemID)
	})
}

func (s *MemoryStore) AppendTurn(_ context.Context, id ID, turn WalkTurn) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		if turn.ID == "" {
			turn.ID = s.NewTID()
		}
		if turn.At.IsZero() {
			turn.At = time.Now()
		}
		w.Transcript = append(w.Transcript, turn)
		return nil
	})
}

func (s *MemoryStore) AppendDecision(_ context.Context, id ID, decision WalkDecision) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		if decision.AgendaItemID == "" {
			return errors.New("walk: decision requires AgendaItemID")
		}
		matched := false
		for i := range w.Agenda {
			if w.Agenda[i].ID != decision.AgendaItemID {
				continue
			}
			d := decision
			w.Agenda[i].Decision = &d
			w.Agenda[i].Status = AgendaDecided
			if decision.Kind == DecisionDefer {
				w.Agenda[i].Status = AgendaDeferred
			}
			matched = true
			break
		}
		if !matched {
			return fmt.Errorf("walk: decision references unknown agenda item %q", decision.AgendaItemID)
		}
		w.Decisions = append(w.Decisions, decision)
		return nil
	})
}

func (s *MemoryStore) AddCost(_ context.Context, id ID, delta Cost) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		w.Cost = w.Cost.Add(delta)
		return nil
	})
}

func (s *MemoryStore) SetResumeContext(_ context.Context, id ID, summary string) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		w.ResumeContext = summary
		return nil
	})
}

func (s *MemoryStore) Pause(_ context.Context, id ID) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		w.Status = StatusPaused
		return nil
	})
}

func (s *MemoryStore) Resume(_ context.Context, id ID) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		w.Status = StatusActive
		return nil
	})
}

func (s *MemoryStore) End(_ context.Context, id ID, outcome Status, endedAt time.Time) (Walk, error) {
	return s.mutate(id, func(w *Walk) error {
		if w.Status.IsTerminal() {
			return ErrTerminal
		}
		if outcome != StatusCompleted && outcome != StatusAbandoned {
			return fmt.Errorf("walk: End requires terminal outcome, got %q", outcome)
		}
		if endedAt.IsZero() {
			endedAt = time.Now()
		}
		w.Status = outcome
		w.EndedAt = &endedAt
		return nil
	})
}

func (s *MemoryStore) mutate(id ID, fn func(*Walk) error) (Walk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.walks[id]
	if !ok {
		return Walk{}, ErrNotFound
	}
	if err := fn(w); err != nil {
		return Walk{}, err
	}
	return cloneWalk(*w), nil
}

// cloneWalk returns an independent copy so callers can't mutate
// our internal state by holding the returned struct. Slices are
// the only mutable bits — copy them defensively.
func cloneWalk(in Walk) Walk {
	out := in
	if in.Agenda != nil {
		out.Agenda = make([]AgendaItem, len(in.Agenda))
		copy(out.Agenda, in.Agenda)
		// Decision pointers point to per-row WalkDecision values
		// stored separately; reuse the pointers — the store's own
		// copy is already a safe clone of the WalkDecision value.
	}
	if in.Transcript != nil {
		out.Transcript = make([]WalkTurn, len(in.Transcript))
		copy(out.Transcript, in.Transcript)
	}
	if in.Decisions != nil {
		out.Decisions = make([]WalkDecision, len(in.Decisions))
		copy(out.Decisions, in.Decisions)
	}
	if in.AssistingPersonas != nil {
		out.AssistingPersonas = append([]string{}, in.AssistingPersonas...)
	}
	if in.BeadsTouched != nil {
		out.BeadsTouched = append([]core.WorkItemID{}, in.BeadsTouched...)
	}
	return out
}

func defaultWalkID() ID {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return ID("walk-" + hex.EncodeToString(b[:]))
}

func defaultTurnID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "turn-" + hex.EncodeToString(b[:])
}
