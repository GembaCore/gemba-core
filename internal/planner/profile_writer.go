// SessionProfile write hooks (gm-s47n.2.3).
//
// Two trigger points per spec §4 Layer 1:
//
//   * Bead claim (ReservationClaimed event) — declared concepts +
//     targets become a contribution at full weight; existing
//     profile weights age by one event step. The session shows
//     warm in the affinity scorer immediately.
//   * Bead completion (WorkItemClosed event) — actual concepts +
//     targets (from the retrospective when available; from the
//     bead's declared values otherwise) become a contribution at
//     full weight, the bead lands in last_beads, last_activity_at
//     stamps. Profile ages by one event step.
//
// V1 limitation: each event ages + merges independently, so a
// claim followed by a completion double-counts the bead's
// contribution by roughly 2x at the moment of completion. The
// retrospective (gm-s47n.8) can later land a fix-up pass that
// subtracts the claim's declared values once the actuals land —
// the math and storage are already in place via AgeProfile +
// MergeContribution. This is the right tradeoff for v1: getting
// a warm-during-the-bead signal is more valuable than the small
// double-count, and the inflation halves every five beads.

package planner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// ProfileWriter is the narrow interface adaptor-side hooks code
// against. *ProfileStore satisfies it; tests substitute a fake.
//
// RecordClaim and RecordCompletion both return the resulting
// SessionProfile so the caller can publish it (e.g. emit a
// session.profile.updated event in a future bead) without an extra
// read.
type ProfileWriter interface {
	RecordClaim(ctx context.Context, ev ClaimEvent) (*SessionProfile, error)
	RecordCompletion(ctx context.Context, ev CompletionEvent) (*SessionProfile, error)
	UpsertProfile(ctx context.Context, p *SessionProfile) error
}

// ClaimEvent is the input to RecordClaim — what the adaptor knows
// at bead-claim time. SessionID + BeadID are required; everything
// else is optional and absent values just contribute nothing.
type ClaimEvent struct {
	SessionID    string
	AssignmentID string
	AgentID      core.AgentID
	BeadID       core.WorkItemID
	// Concepts / Files come from the bead's declared targets +
	// concepts (gm-s47n.1 once it lands; provider's structured
	// extras until then).
	Concepts []ConceptTag
	Files    []string
	// HalfLife overrides DefaultDecayHalfLife per call so a future
	// per-rig setting can flow through without changing the API.
	HalfLife int
	// Now is injected for deterministic test runs; nil → time.Now.
	Now func() time.Time
}

// CompletionEvent is the input to RecordCompletion. ActualConcepts
// / ActualFiles come from the retrospective (.8.1) when available;
// callers without retrospective output pass declared values and
// take the small inflation hit described in the file header.
type CompletionEvent struct {
	SessionID      string
	AssignmentID   string
	AgentID        core.AgentID
	BeadID         core.WorkItemID
	ActualConcepts []ConceptTag
	ActualFiles    []string
	// TokensUsed / ContextWindowMax are optional snapshots the
	// adaptor may have observed at completion time (e.g. via the
	// agent's telemetry hook). Zero values leave the existing
	// profile fields untouched.
	TokensUsed       int
	ContextWindowMax int
	HalfLife         int
	Now              func() time.Time
}

var _ ProfileWriter = (*ProfileStore)(nil)

// RecordClaim ages the session's existing profile by one event
// step and merges the bead's declared concepts/files at full
// weight. Returns the resulting profile.
//
// Idempotency: this method does NOT check whether the same bead
// id was already claimed. Callers that re-deliver a claim event
// (network retry, double-submit, replay) get the contribution
// merged twice. The adaptor-level wiring is responsible for
// deduping at the event boundary; the planner's contract is
// "every call mutates the profile", consistent with the rest of
// the gm-s47n family.
func (s *ProfileStore) RecordClaim(ctx context.Context, ev ClaimEvent) (*SessionProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("planner.ProfileStore.RecordClaim: nil store")
	}
	if ev.SessionID == "" {
		return nil, fmt.Errorf("planner.ProfileStore.RecordClaim: empty SessionID")
	}
	now := pickNow(ev.Now)
	prior, err := s.GetProfile(ctx, ev.SessionID)
	if err != nil {
		return nil, fmt.Errorf("planner.ProfileStore.RecordClaim: read prior: %w", err)
	}
	next := agedClone(prior, ev.HalfLife)
	next.SessionID = ev.SessionID
	if ev.AssignmentID != "" {
		next.AssignmentID = ev.AssignmentID
	}
	if ev.AgentID != "" {
		next.AgentID = ev.AgentID
	}
	next.Concepts = MergeContribution(next.Concepts, ev.Concepts, 0)
	next.Files = MergeFileContribution(next.Files, ev.Files, 0)
	next.LastActivityAt = now
	if next.CreatedAt.IsZero() {
		next.CreatedAt = now
	}
	next.UpdatedAt = now
	if err := s.UpsertProfile(ctx, next); err != nil {
		return nil, err
	}
	return next, nil
}

// RecordCompletion handles the bead-finished event. Same age +
// merge as RecordClaim plus:
//   - bead id appended to last_beads ring (capped at LastBeadsRingSize)
//   - tokens_used / context_window_max overwritten when the event
//     supplies them (zero leaves the existing snapshot intact)
//   - context_pct recomputed when both numerator and denominator
//     are observable
func (s *ProfileStore) RecordCompletion(ctx context.Context, ev CompletionEvent) (*SessionProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("planner.ProfileStore.RecordCompletion: nil store")
	}
	if ev.SessionID == "" {
		return nil, fmt.Errorf("planner.ProfileStore.RecordCompletion: empty SessionID")
	}
	now := pickNow(ev.Now)
	prior, err := s.GetProfile(ctx, ev.SessionID)
	if err != nil {
		return nil, fmt.Errorf("planner.ProfileStore.RecordCompletion: read prior: %w", err)
	}
	next := agedClone(prior, ev.HalfLife)
	next.SessionID = ev.SessionID
	if ev.AssignmentID != "" {
		next.AssignmentID = ev.AssignmentID
	}
	if ev.AgentID != "" {
		next.AgentID = ev.AgentID
	}
	next.Concepts = MergeContribution(next.Concepts, ev.ActualConcepts, 0)
	next.Files = MergeFileContribution(next.Files, ev.ActualFiles, 0)
	if ev.BeadID != "" {
		next.LastBeads = appendRing(next.LastBeads, ev.BeadID, LastBeadsRingSize)
	}
	if ev.TokensUsed > 0 {
		next.TokensUsed = ev.TokensUsed
	}
	if ev.ContextWindowMax > 0 {
		next.ContextWindowMax = ev.ContextWindowMax
	}
	if next.ContextWindowMax > 0 {
		next.ContextPct = float64(next.TokensUsed) / float64(next.ContextWindowMax)
	}
	next.LastActivityAt = now
	if next.CreatedAt.IsZero() {
		next.CreatedAt = now
	}
	next.UpdatedAt = now
	if err := s.UpsertProfile(ctx, next); err != nil {
		return nil, err
	}
	return next, nil
}

// UpsertProfile writes or replaces the row for p.SessionID. Upsert
// semantics via INSERT ... ON DUPLICATE KEY UPDATE — works against
// dolt's MySQL surface. Encodes concepts / files / last_beads as
// JSON columns matching schema.sql.
func (s *ProfileStore) UpsertProfile(ctx context.Context, p *SessionProfile) error {
	if s == nil || s.db == nil {
		return errors.New("planner.ProfileStore.UpsertProfile: nil store")
	}
	if p == nil {
		return errors.New("planner.ProfileStore.UpsertProfile: nil profile")
	}
	if p.SessionID == "" {
		return errors.New("planner.ProfileStore.UpsertProfile: empty SessionID")
	}
	conceptsJSON, err := jsonOrNil(p.Concepts)
	if err != nil {
		return fmt.Errorf("encode concepts: %w", err)
	}
	filesJSON, err := jsonOrNil(p.Files)
	if err != nil {
		return fmt.Errorf("encode files: %w", err)
	}
	lastBeadsJSON, err := jsonOrNil(p.LastBeads)
	if err != nil {
		return fmt.Errorf("encode last_beads: %w", err)
	}

	const stmt = `
		INSERT INTO session_profiles (
			session_id, assignment_id, agent_id,
			concepts, files,
			tokens_used, context_window_max, context_pct,
			last_beads,
			last_activity_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			assignment_id = VALUES(assignment_id),
			agent_id = VALUES(agent_id),
			concepts = VALUES(concepts),
			files = VALUES(files),
			tokens_used = VALUES(tokens_used),
			context_window_max = VALUES(context_window_max),
			context_pct = VALUES(context_pct),
			last_beads = VALUES(last_beads),
			last_activity_at = VALUES(last_activity_at),
			updated_at = VALUES(updated_at)`
	_, err = s.db.ExecContext(ctx, stmt,
		p.SessionID, p.AssignmentID, string(p.AgentID),
		conceptsJSON, filesJSON,
		p.TokensUsed, p.ContextWindowMax, p.ContextPct,
		lastBeadsJSON,
		p.LastActivityAt, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("planner.ProfileStore.UpsertProfile: %w", err)
	}
	return nil
}

// agedClone returns a fresh SessionProfile with the prior's
// concepts / files aged by one event step. Other fields copy
// across unchanged. nil prior yields a zero-value profile.
func agedClone(prior *SessionProfile, halfLife int) *SessionProfile {
	if prior == nil {
		return &SessionProfile{}
	}
	out := *prior
	out.Concepts = AgeProfile(prior.Concepts, halfLife)
	out.Files = AgeFileProfile(prior.Files, halfLife)
	// LastBeads is a ring buffer, not a decayed weight set —
	// don't age it; the writer either appends (completion) or
	// leaves it untouched (claim).
	out.LastBeads = append([]core.WorkItemID(nil), prior.LastBeads...)
	return &out
}

// appendRing pushes id onto the tail of ring, capped at max
// entries (oldest dropped). Newest is always last so the affinity
// scorer's "most recent bead" lookup is ring[len-1].
func appendRing(ring []core.WorkItemID, id core.WorkItemID, max int) []core.WorkItemID {
	// Fresh backing array so the caller's `ring` slice cannot be
	// stomped on a high-capacity append.
	out := make([]core.WorkItemID, 0, len(ring)+1)
	out = append(out, ring...)
	out = append(out, id)
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func pickNow(fn func() time.Time) time.Time {
	if fn != nil {
		return fn()
	}
	return time.Now()
}

// jsonOrNil encodes v as JSON, returning sql.NullString — nil
// (NULL in the column) for zero-value inputs so the row stays
// tight rather than carrying "{}" / "[]" placeholders.
func jsonOrNil(v any) (sql.NullString, error) {
	switch m := v.(type) {
	case map[ConceptTag]float64:
		if len(m) == 0 {
			return sql.NullString{}, nil
		}
		bs, err := json.Marshal(m)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	case map[string]float64:
		if len(m) == 0 {
			return sql.NullString{}, nil
		}
		bs, err := json.Marshal(m)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	case []core.WorkItemID:
		if len(m) == 0 {
			return sql.NullString{}, nil
		}
		bs, err := json.Marshal(m)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	default:
		bs, err := json.Marshal(v)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	}
}
