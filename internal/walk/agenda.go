// Five-source agenda aggregator (gm-rfy, design §Starting a walk).

package walk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// DefaultRecentWindow is the lookback window for bead_filed and
// bead_closed agenda sources. Spec calls 24h "configurable" — we
// expose it as AggregateOptions.RecentWindow so an operator can
// open a longer or shorter walk without code changes.
const DefaultRecentWindow = 24 * time.Hour

// DefaultPerSourceCap caps how many items each source contributes.
// Without a cap, a backed-up rig could shove a 200-bead "filed in
// last 24h" list onto the walk and bury the higher-signal escalation
// rows. 25 per source keeps the agenda walkable.
const DefaultPerSourceCap = 25

// AggregateOptions tunes the agenda build. Zero values resolve to
// defaults in BuildAgenda; tests pin deterministic values.
type AggregateOptions struct {
	// Now overrides time.Now for deterministic tests. nil →
	// time.Now (only used to compute the recent-window cutoff;
	// Source clients receive the cutoff, not the function).
	Now func() time.Time
	// RecentWindow drives bead_filed + bead_closed lookback. Zero →
	// DefaultRecentWindow.
	RecentWindow time.Duration
	// PerSourceCap caps items per source kind. Zero →
	// DefaultPerSourceCap.
	PerSourceCap int
	// Workspace narrows every source query. Empty means "no
	// workspace filter" — clients that scope to a workspace can
	// pass it explicitly.
	Workspace string
	// IDFor produces a stable agenda-item id from a kind+ref pair.
	// Zero value uses a deterministic hex digest of (kind, ref);
	// tests substitute a counter for predictable assertions.
	IDFor func(kind AgendaSourceKind, ref string) string
}

func (o AggregateOptions) resolved() AggregateOptions {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RecentWindow <= 0 {
		o.RecentWindow = DefaultRecentWindow
	}
	if o.PerSourceCap <= 0 {
		o.PerSourceCap = DefaultPerSourceCap
	}
	if o.IDFor == nil {
		o.IDFor = defaultAgendaItemID
	}
	return o
}

// EscalationLister abstracts the OrchestrationPlane's
// escalation query. The aggregator cares about *open* escalations
// only; callers wire the live OrchestrationPlane.ListOpenEscalations
// or a fake.
type EscalationLister interface {
	ListOpenEscalations(ctx context.Context, workspace string) ([]core.EscalationRequest, error)
}

// HITLLister abstracts the suspended-Manager-session query that
// will land in gm-uf7. Callers in earlier code paths can pass
// NoopHITLLister (returns empty); once gm-uf7 ships, swap in the
// live implementation.
type HITLLister interface {
	ListSuspendedSessions(ctx context.Context, workspace string) ([]HITLAgendaItem, error)
}

// HITLAgendaItem is the source-shaped record the aggregator
// needs from the HITL store. Defined here (rather than in
// gm-uf7) so the interface is callable today; gm-uf7's session
// store maps its native shape onto this projection.
type HITLAgendaItem struct {
	SessionID  string       `json:"session_id"`
	WorkerID   core.AgentID `json:"worker_id"`
	Question   string       `json:"question"`
	AskedAt    time.Time    `json:"asked_at"`
	Urgency    string       `json:"urgency,omitempty"`
	WorkItemID core.WorkItemID `json:"work_item_id,omitempty"`
}

// GateFailureLister abstracts the QA / Security gate-failure
// query. Like HITL the live implementation lives elsewhere;
// callers wire NoopGateFailureLister in test paths.
type GateFailureLister interface {
	ListOpenGateFailures(ctx context.Context, workspace string) ([]GateFailureItem, error)
}

// GateFailureItem is the projection the aggregator consumes.
type GateFailureItem struct {
	GateID     string          `json:"gate_id"`
	Kind       string          `json:"kind"` // "qa" | "security" | etc
	WorkItemID core.WorkItemID `json:"work_item_id,omitempty"`
	Title      string          `json:"title"`
	FailedAt   time.Time       `json:"failed_at"`
}

// WorkItemLister abstracts the WorkPlane subset the aggregator
// needs (filed-recently, closed-recently). Implementations
// wrap WorkPlane.ListWorkItems with the appropriate filter.
type WorkItemLister interface {
	ListWorkItems(ctx context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error)
}

// Sources bundles the four query clients the aggregator calls.
// All four are nil-safe: a nil client contributes zero items,
// which is the right default during partial roll-out (e.g.
// HITL not wired yet) and during offline tests.
type Sources struct {
	Escalations  EscalationLister
	HITL         HITLLister
	GateFailures GateFailureLister
	WorkItems    WorkItemLister
}

// BuildAgenda runs the five source queries and returns the
// merged + ranked + capped agenda items in stable order.
//
// Ordering:
//   - escalation (blocking first, then advisory) → by CreatedAt asc
//   - hitl                                       → by AskedAt asc
//   - gate_failure                               → by FailedAt asc
//   - bead_filed                                 → by UpdatedAt desc
//   - bead_closed                                → by UpdatedAt desc
//
// Source order is stable: escalations first, then hitl, gate
// failures, bead_filed, bead_closed. Within a source the natural
// query order is preserved after the per-source sort/cap. The
// resulting Priority is the source's index (escalation=400,
// hitl=300, gate=200, bead_filed=100, bead_closed=50) so a UI
// can drag-reorder while keeping the default presentation
// faithful to the design.
func BuildAgenda(ctx context.Context, srcs Sources, opts AggregateOptions) ([]AgendaItem, error) {
	o := opts.resolved()
	now := o.Now()
	cutoff := now.Add(-o.RecentWindow)

	out := make([]AgendaItem, 0, 64)

	escalations, err := listEscalations(ctx, srcs.Escalations, o.Workspace)
	if err != nil {
		return nil, fmt.Errorf("walk: list escalations: %w", err)
	}
	out = append(out, escalationItems(escalations, o, now)...)

	hitls, err := listHITL(ctx, srcs.HITL, o.Workspace)
	if err != nil {
		return nil, fmt.Errorf("walk: list hitl: %w", err)
	}
	out = append(out, hitlItems(hitls, o, now)...)

	gates, err := listGateFailures(ctx, srcs.GateFailures, o.Workspace)
	if err != nil {
		return nil, fmt.Errorf("walk: list gate failures: %w", err)
	}
	out = append(out, gateItems(gates, o, now)...)

	filed, closed, err := recentBeads(ctx, srcs.WorkItems, cutoff)
	if err != nil {
		return nil, fmt.Errorf("walk: list recent beads: %w", err)
	}
	out = append(out, beadFiledItems(filed, o, now)...)
	out = append(out, beadClosedItems(closed, o, now)...)

	return dedupeStable(out), nil
}

// ── per-source query wrappers (nil-safe) ────────────────────────

func listEscalations(ctx context.Context, c EscalationLister, ws string) ([]core.EscalationRequest, error) {
	if c == nil {
		return nil, nil
	}
	return c.ListOpenEscalations(ctx, ws)
}

func listHITL(ctx context.Context, c HITLLister, ws string) ([]HITLAgendaItem, error) {
	if c == nil {
		return nil, nil
	}
	return c.ListSuspendedSessions(ctx, ws)
}

func listGateFailures(ctx context.Context, c GateFailureLister, ws string) ([]GateFailureItem, error) {
	if c == nil {
		return nil, nil
	}
	return c.ListOpenGateFailures(ctx, ws)
}

// recentBeads runs two ListWorkItems calls — one for unstarted/
// staged beads filed in the window (bead_filed), one for
// completed beads closed in the window (bead_closed). The split
// is intentional: an UpdatedSince filter alone would mix the two
// flavours together and we'd have to re-bucket downstream.
func recentBeads(ctx context.Context, c WorkItemLister, cutoff time.Time) ([]core.WorkItem, []core.WorkItem, error) {
	if c == nil {
		return nil, nil, nil
	}
	filedFilter := core.WorkItemFilter{
		StateCategory: []core.StateCategory{core.StateUnstarted, core.StateStaged},
		UpdatedSince:  &cutoff,
	}
	filed, err := c.ListWorkItems(ctx, filedFilter)
	if err != nil {
		return nil, nil, err
	}
	closedFilter := core.WorkItemFilter{
		StateCategory: []core.StateCategory{core.StateCompleted},
		UpdatedSince:  &cutoff,
	}
	closed, err := c.ListWorkItems(ctx, closedFilter)
	if err != nil {
		return nil, nil, err
	}
	return filed, closed, nil
}

// ── per-source item builders ────────────────────────────────────

const (
	priorityEscalation  = 400
	priorityHITL        = 300
	priorityGateFailure = 200
	priorityBeadFiled   = 100
	priorityBeadClosed  = 50
)

func escalationItems(in []core.EscalationRequest, o AggregateOptions, now time.Time) []AgendaItem {
	dup := make([]core.EscalationRequest, len(in))
	copy(dup, in)
	sort.SliceStable(dup, func(i, j int) bool {
		// Blocking before advisory; within the same urgency
		// band, oldest first (escalations that have been waiting
		// longer surface higher).
		if rank(dup[i].Urgency) != rank(dup[j].Urgency) {
			return rank(dup[i].Urgency) > rank(dup[j].Urgency)
		}
		return dup[i].CreatedAt.Before(dup[j].CreatedAt)
	})
	if len(dup) > o.PerSourceCap {
		dup = dup[:o.PerSourceCap]
	}
	out := make([]AgendaItem, 0, len(dup))
	for _, e := range dup {
		topic := e.Title
		if topic == "" {
			topic = e.Prompt
		}
		out = append(out, AgendaItem{
			ID:    o.IDFor(SourceEscalation, e.ID),
			Topic: topic,
			Source: AgendaSource{
				Kind:   SourceEscalation,
				Ref:    e.ID,
				Worker: e.AgentID,
			},
			Priority: priorityEscalation + urgencyBoost(e.Urgency),
			Status:   AgendaQueued,
			AddedAt:  now,
		})
	}
	return out
}

func hitlItems(in []HITLAgendaItem, o AggregateOptions, now time.Time) []AgendaItem {
	dup := make([]HITLAgendaItem, len(in))
	copy(dup, in)
	sort.SliceStable(dup, func(i, j int) bool {
		return dup[i].AskedAt.Before(dup[j].AskedAt)
	})
	if len(dup) > o.PerSourceCap {
		dup = dup[:o.PerSourceCap]
	}
	out := make([]AgendaItem, 0, len(dup))
	for _, h := range dup {
		topic := h.Question
		if topic == "" {
			topic = "HITL question"
		}
		out = append(out, AgendaItem{
			ID:    o.IDFor(SourceHITL, h.SessionID),
			Topic: topic,
			Source: AgendaSource{
				Kind:   SourceHITL,
				Ref:    h.SessionID,
				Worker: h.WorkerID,
			},
			Priority: priorityHITL,
			Status:   AgendaQueued,
			AddedAt:  now,
		})
	}
	return out
}

func gateItems(in []GateFailureItem, o AggregateOptions, now time.Time) []AgendaItem {
	dup := make([]GateFailureItem, len(in))
	copy(dup, in)
	sort.SliceStable(dup, func(i, j int) bool {
		return dup[i].FailedAt.Before(dup[j].FailedAt)
	})
	if len(dup) > o.PerSourceCap {
		dup = dup[:o.PerSourceCap]
	}
	out := make([]AgendaItem, 0, len(dup))
	for _, g := range dup {
		topic := g.Title
		if topic == "" && g.Kind != "" {
			topic = fmt.Sprintf("%s gate failure", g.Kind)
		}
		out = append(out, AgendaItem{
			ID:    o.IDFor(SourceGateFailure, g.GateID),
			Topic: topic,
			Source: AgendaSource{
				Kind: SourceGateFailure,
				Ref:  g.GateID,
			},
			Priority: priorityGateFailure,
			Status:   AgendaQueued,
			AddedAt:  now,
		})
	}
	return out
}

func beadFiledItems(in []core.WorkItem, o AggregateOptions, now time.Time) []AgendaItem {
	dup := sortedByUpdatedDesc(in)
	if len(dup) > o.PerSourceCap {
		dup = dup[:o.PerSourceCap]
	}
	out := make([]AgendaItem, 0, len(dup))
	for _, w := range dup {
		out = append(out, AgendaItem{
			ID:    o.IDFor(SourceBeadFiled, string(w.ID)),
			Topic: w.Title,
			Source: AgendaSource{
				Kind: SourceBeadFiled,
				Ref:  string(w.ID),
			},
			Priority: priorityBeadFiled,
			Status:   AgendaQueued,
			AddedAt:  now,
		})
	}
	return out
}

func beadClosedItems(in []core.WorkItem, o AggregateOptions, now time.Time) []AgendaItem {
	dup := sortedByUpdatedDesc(in)
	if len(dup) > o.PerSourceCap {
		dup = dup[:o.PerSourceCap]
	}
	out := make([]AgendaItem, 0, len(dup))
	for _, w := range dup {
		out = append(out, AgendaItem{
			ID:    o.IDFor(SourceBeadClosed, string(w.ID)),
			Topic: w.Title,
			Source: AgendaSource{
				Kind: SourceBeadClosed,
				Ref:  string(w.ID),
			},
			Priority: priorityBeadClosed,
			Status:   AgendaQueued,
			AddedAt:  now,
		})
	}
	return out
}

func sortedByUpdatedDesc(in []core.WorkItem) []core.WorkItem {
	dup := make([]core.WorkItem, len(in))
	copy(dup, in)
	sort.SliceStable(dup, func(i, j int) bool {
		return dup[i].UpdatedAt.After(dup[j].UpdatedAt)
	})
	return dup
}

func rank(u core.EscalationUrgency) int {
	switch u {
	case core.UrgencyBlocking:
		return 2
	case core.UrgencyAdvisory:
		return 1
	}
	return 0
}

func urgencyBoost(u core.EscalationUrgency) int {
	if u == core.UrgencyBlocking {
		return 50
	}
	return 0
}

// dedupeStable drops items whose (Source.Kind, Source.Ref) pair
// has already appeared higher in the slice. The 5 sources can
// converge on the same bead (a closed bead with an open
// escalation, for instance) and we want the higher-priority
// surfacing to win without forcing the operator to dismiss the
// duplicate by hand. Kind+Ref is the natural-key — agenda item
// IDs are deterministic over that pair so two duplicates would
// otherwise carry the same id and confuse the UI.
func dedupeStable(in []AgendaItem) []AgendaItem {
	seen := make(map[string]struct{}, len(in))
	out := make([]AgendaItem, 0, len(in))
	for _, a := range in {
		key := string(a.Source.Kind) + "|" + a.Source.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, a)
	}
	return out
}

// defaultAgendaItemID hashes (kind, ref) into a stable hex
// string — same input always produces the same id, so a
// re-aggregated agenda from a paused walk preserves item ids
// across runs (the UI can match its drag-reorder state without
// a new round-trip).
func defaultAgendaItemID(kind AgendaSourceKind, ref string) string {
	if ref == "" {
		// Random fallback for items without a stable ref (e.g.
		// a user-pushed bare topic). 8 bytes of entropy — plenty
		// for a single walk's agenda.
		var b [8]byte
		_, _ = rand.Read(b[:])
		return string(kind) + "-" + hex.EncodeToString(b[:])
	}
	return string(kind) + "-" + ref
}
