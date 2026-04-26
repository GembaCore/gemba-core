package concepts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"
)

// SuggestionKind enumerates the closed set of changes the queue can
// surface. Add a new kind here, in [ApplyDecision], and add the
// corresponding [Vocabulary] / [BeadConceptStore] handler.
type SuggestionKind string

const (
	KindMerge  SuggestionKind = "merge"
	KindRename SuggestionKind = "rename"
	KindDelete SuggestionKind = "delete"
)

// SuggestionStatus tracks the operator's decision lifecycle.
type SuggestionStatus string

const (
	StatusPending  SuggestionStatus = "pending"
	StatusApproved SuggestionStatus = "approved"
	StatusRejected SuggestionStatus = "rejected"
)

// Suggestion is a proposed vocabulary change. The drift detector
// emits these (status=pending); the operator approves or rejects;
// the apply path materializes approved changes through the
// vocabulary + the bead store.
type Suggestion struct {
	ID        string           `json:"id"`
	Kind      SuggestionKind   `json:"kind"`
	From      string           `json:"from,omitempty"`
	To        string           `json:"to,omitempty"`
	Reason    string           `json:"reason"`
	Source    string           `json:"source"` // "drift:near-duplicate" | "drift:singleton" | "operator"
	CreatedAt time.Time        `json:"created_at"`
	Status    SuggestionStatus `json:"status"`
}

// Decision is one append-only entry in the decisions log.
type Decision struct {
	SuggestionID string         `json:"suggestion_id"`
	Kind         SuggestionKind `json:"kind"`
	From         string         `json:"from,omitempty"`
	To           string         `json:"to,omitempty"`
	Action       string         `json:"action"` // "approved" | "rejected"
	Reason       string         `json:"reason,omitempty"`
	By           string         `json:"by"`
	BeadsChanged int            `json:"beads_changed,omitempty"`
	At           time.Time      `json:"at"`
}

// NewSuggestionID mints a short hex id stable enough for an operator
// to type. Collisions inside one workspace are vanishingly unlikely
// (8 hex chars = 4 bytes of entropy).
func NewSuggestionID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "s-" + hex.EncodeToString(b[:])
}

// SuggestionsFromDrift converts a drift report into pending
// suggestions. Idempotent against the existing list — a near-
// duplicate that's already in the queue (same Kind + From + To,
// regardless of order) doesn't get a second entry.
func SuggestionsFromDrift(d Drift, existing []Suggestion) []Suggestion {
	have := make(map[string]bool, len(existing))
	for _, s := range existing {
		have[suggestionKey(s.Kind, s.From, s.To)] = true
	}
	out := []Suggestion{}
	now := time.Now().UTC()
	for _, nd := range d.NearDuplicates {
		from, to := nd.A, nd.B
		if nd.UsesA > nd.UsesB {
			from, to = nd.B, nd.A
		}
		key := suggestionKey(KindMerge, from, to)
		if have[key] {
			continue
		}
		out = append(out, Suggestion{
			ID:        NewSuggestionID(),
			Kind:      KindMerge,
			From:      from,
			To:        to,
			Reason:    formatNearDuplicateReason(nd),
			Source:    "drift:near-duplicate",
			CreatedAt: now,
			Status:    StatusPending,
		})
		have[key] = true
	}
	for _, s := range d.Singletons {
		key := suggestionKey(KindDelete, s.Term, "")
		if have[key] {
			continue
		}
		out = append(out, Suggestion{
			ID:        NewSuggestionID(),
			Kind:      KindDelete,
			From:      s.Term,
			Reason:    formatSingletonReason(s),
			Source:    "drift:singleton",
			CreatedAt: now,
			Status:    StatusPending,
		})
		have[key] = true
	}
	return out
}

// Pending returns the slice of pending suggestions in stable order
// (by Kind, then From, then To). Callers wanting all statuses iterate
// the SuggestionList directly.
func (l *SuggestionList) Pending() []Suggestion {
	return l.byStatus(StatusPending)
}

// Approved / Rejected accessors mirror Pending.
func (l *SuggestionList) Approved() []Suggestion { return l.byStatus(StatusApproved) }
func (l *SuggestionList) Rejected() []Suggestion { return l.byStatus(StatusRejected) }

func (l *SuggestionList) byStatus(s SuggestionStatus) []Suggestion {
	out := make([]Suggestion, 0)
	for _, sug := range l.Suggestions {
		if sug.Status == s {
			out = append(out, sug)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// Find returns the suggestion with the given id (and a found bool).
func (l *SuggestionList) Find(id string) (*Suggestion, bool) {
	for i := range l.Suggestions {
		if l.Suggestions[i].ID == id {
			return &l.Suggestions[i], true
		}
	}
	return nil, false
}

// Add appends a suggestion. No-op when the (kind, from, to) tuple is
// already pending or approved — rejected suggestions don't block a
// re-proposal because the operator's earlier "no" was about that
// instance, not the entire idea.
func (l *SuggestionList) Add(s Suggestion) bool {
	key := suggestionKey(s.Kind, s.From, s.To)
	for _, existing := range l.Suggestions {
		if existing.Status == StatusRejected {
			continue
		}
		if suggestionKey(existing.Kind, existing.From, existing.To) == key {
			return false
		}
	}
	l.Suggestions = append(l.Suggestions, s)
	return true
}

// Mark updates a suggestion's status. Returns ErrSuggestionNotFound
// when the id doesn't match. Only pending suggestions can transition;
// re-marking a decided suggestion is an error so an operator can't
// silently flip a historical decision.
func (l *SuggestionList) Mark(id string, status SuggestionStatus) error {
	s, ok := l.Find(id)
	if !ok {
		return ErrSuggestionNotFound
	}
	if s.Status != StatusPending {
		return ErrSuggestionDecided
	}
	s.Status = status
	return nil
}

// ErrSuggestionNotFound / ErrSuggestionDecided are sentinel errors
// the CLI checks via errors.Is so it can map them to user-facing
// messages without string parsing.
var (
	ErrSuggestionNotFound = errors.New("concepts: suggestion not found")
	ErrSuggestionDecided  = errors.New("concepts: suggestion already decided")
)

func suggestionKey(kind SuggestionKind, from, to string) string {
	return string(kind) + ":" + from + "->" + to
}

func formatNearDuplicateReason(nd NearDuplicate) string {
	return "near-duplicate co-occurrence: jaccard=" + ftoa(nd.Jaccard) + " uses=" + itoa(nd.UsesA) + "/" + itoa(nd.UsesB)
}

func formatSingletonReason(s Singleton) string {
	if s.ClosedAt != nil {
		return "singleton on closed bead " + s.BeadID + " (dormant " + itoa(s.DormantFor) + "d)"
	}
	return "singleton on " + s.BeadID
}

func ftoa(f float64) string {
	// Two-decimal rounding good enough for human reasons.
	n := int(f*100 + 0.5)
	whole := n / 100
	frac := n % 100
	return itoa(whole) + "." + pad2(frac)
}

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

func pad2(i int) string {
	if i < 10 {
		return "0" + itoa(i)
	}
	return itoa(i)
}
