package concepts

import (
	"context"
	"fmt"
	"sort"
)

// BeadConceptStore is the integration boundary between the concepts
// package and the WorkItem.concepts schema landing in gm-s47n.1.1.
// Production wiring (a thin adapter over WorkPlane) is scheduled for
// that bead; until then the in-memory implementation in this file
// powers tests and CLI dry-runs.
type BeadConceptStore interface {
	// List returns every bead's id and current concept set.
	List(ctx context.Context) ([]BeadConcepts, error)

	// Set replaces the concept set on the named bead. The slice is
	// owned by the caller after return — implementations that need to
	// retain it should copy.
	Set(ctx context.Context, beadID string, concepts []string) error
}

// MemoryStore is the in-memory BeadConceptStore. Production-grade —
// the CLI uses it for dry-runs and the test suite uses it
// everywhere. The historical-rewrite math is the same regardless of
// which store sits behind the interface.
type MemoryStore struct {
	beads map[string][]string
}

// NewMemoryStore returns an empty store. Callers seed via Set.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{beads: make(map[string][]string)}
}

// List implements [BeadConceptStore].
func (s *MemoryStore) List(_ context.Context) ([]BeadConcepts, error) {
	out := make([]BeadConcepts, 0, len(s.beads))
	ids := make([]string, 0, len(s.beads))
	for id := range s.beads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		concepts := append([]string(nil), s.beads[id]...)
		out = append(out, BeadConcepts{BeadID: id, Concepts: concepts})
	}
	return out, nil
}

// Set implements [BeadConceptStore].
func (s *MemoryStore) Set(_ context.Context, beadID string, concepts []string) error {
	if beadID == "" {
		return fmt.Errorf("concepts: MemoryStore.Set requires a bead id")
	}
	s.beads[beadID] = append([]string(nil), concepts...)
	return nil
}

// ApplyMerge rewrites every bead whose concept set contains `from` so
// it now contains `to` instead. Beads already carrying both terms
// drop the `from` (no double entry). Returns the count of beads
// changed.
func ApplyMerge(ctx context.Context, store BeadConceptStore, from, to string) (int, error) {
	canonFrom := Normalize(from)
	canonTo := Normalize(to)
	if canonFrom == "" || canonTo == "" {
		return 0, fmt.Errorf("concepts: ApplyMerge requires non-empty from/to")
	}
	if canonFrom == canonTo {
		return 0, fmt.Errorf("concepts: ApplyMerge from and to are the same: %q", canonFrom)
	}
	beads, err := store.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("concepts: ApplyMerge list beads: %w", err)
	}
	changed := 0
	for _, b := range beads {
		next, mutated := replaceConcept(b.Concepts, canonFrom, canonTo)
		if !mutated {
			continue
		}
		if err := store.Set(ctx, b.BeadID, next); err != nil {
			return changed, fmt.Errorf("concepts: ApplyMerge set %s: %w", b.BeadID, err)
		}
		changed++
	}
	return changed, nil
}

// ApplyRename changes every occurrence of `from` to `to` across every
// bead. Identical mechanics to ApplyMerge — the difference is in the
// vocabulary layer (Rename keeps the term as the surviving one,
// Merge collapses two pre-existing terms). Beads carrying both terms
// dedup to a single `to`.
func ApplyRename(ctx context.Context, store BeadConceptStore, from, to string) (int, error) {
	return ApplyMerge(ctx, store, from, to)
}

// ApplyDelete drops `term` from every bead that has it. Returns the
// count of beads changed.
func ApplyDelete(ctx context.Context, store BeadConceptStore, term string) (int, error) {
	canon := Normalize(term)
	if canon == "" {
		return 0, fmt.Errorf("concepts: ApplyDelete requires non-empty term")
	}
	beads, err := store.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("concepts: ApplyDelete list beads: %w", err)
	}
	changed := 0
	for _, b := range beads {
		next, mutated := removeConcept(b.Concepts, canon)
		if !mutated {
			continue
		}
		if err := store.Set(ctx, b.BeadID, next); err != nil {
			return changed, fmt.Errorf("concepts: ApplyDelete set %s: %w", b.BeadID, err)
		}
		changed++
	}
	return changed, nil
}

// ApplyDecision is the CLI-facing entry point. It looks up the
// suggestion, marks it approved on the in-memory list, applies the
// vocabulary side and the bead-side rewrite, and returns the count
// of beads changed for the audit log. Caller is responsible for
// persisting the vocabulary + suggestion list afterwards.
func ApplyDecision(
	ctx context.Context,
	v *Vocabulary,
	list *SuggestionList,
	store BeadConceptStore,
	id string,
	by string,
) (Decision, error) {
	s, ok := list.Find(id)
	if !ok {
		return Decision{}, ErrSuggestionNotFound
	}
	if s.Status != StatusPending {
		return Decision{}, ErrSuggestionDecided
	}

	dec := Decision{
		SuggestionID: s.ID,
		Kind:         s.Kind,
		From:         s.From,
		To:           s.To,
		Action:       string(StatusApproved),
		By:           by,
	}

	switch s.Kind {
	case KindMerge:
		if _, err := v.Merge(s.From, s.To); err != nil {
			return Decision{}, fmt.Errorf("concepts: vocabulary merge: %w", err)
		}
		n, err := ApplyMerge(ctx, store, s.From, s.To)
		if err != nil {
			return Decision{}, err
		}
		dec.BeadsChanged = n
	case KindRename:
		if _, err := v.Rename(s.From, s.To); err != nil {
			return Decision{}, fmt.Errorf("concepts: vocabulary rename: %w", err)
		}
		n, err := ApplyRename(ctx, store, s.From, s.To)
		if err != nil {
			return Decision{}, err
		}
		dec.BeadsChanged = n
	case KindDelete:
		if !v.Retire(s.From) {
			return Decision{}, fmt.Errorf("concepts: vocabulary delete: term not found: %s", s.From)
		}
		n, err := ApplyDelete(ctx, store, s.From)
		if err != nil {
			return Decision{}, err
		}
		dec.BeadsChanged = n
	default:
		return Decision{}, fmt.Errorf("concepts: unknown suggestion kind %q", s.Kind)
	}

	s.Status = StatusApproved
	return dec, nil
}

// RejectDecision marks a suggestion rejected and returns the
// audit-log entry.
func RejectDecision(list *SuggestionList, id, by, reason string) (Decision, error) {
	s, ok := list.Find(id)
	if !ok {
		return Decision{}, ErrSuggestionNotFound
	}
	if s.Status != StatusPending {
		return Decision{}, ErrSuggestionDecided
	}
	s.Status = StatusRejected
	return Decision{
		SuggestionID: s.ID,
		Kind:         s.Kind,
		From:         s.From,
		To:           s.To,
		Action:       string(StatusRejected),
		Reason:       reason,
		By:           by,
	}, nil
}

// replaceConcept replaces the first occurrence of `from` with `to`,
// dedup'ing if `to` is already present. Returns the new slice and a
// bool reporting whether anything changed.
func replaceConcept(in []string, from, to string) ([]string, bool) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	mutated := false
	for _, c := range in {
		canon := Normalize(c)
		if canon == from {
			canon = to
			mutated = true
		}
		if seen[canon] {
			mutated = true // we collapsed a duplicate
			continue
		}
		seen[canon] = true
		out = append(out, canon)
	}
	return out, mutated
}

func removeConcept(in []string, term string) ([]string, bool) {
	out := make([]string, 0, len(in))
	mutated := false
	for _, c := range in {
		if Normalize(c) == term {
			mutated = true
			continue
		}
		out = append(out, c)
	}
	return out, mutated
}
