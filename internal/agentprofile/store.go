// agent_profiles persistence (gm-v5z2.2). Mirrors the dolt-backed
// shape internal/planner.ProfileStore exposes; the two stores
// share planner.SchemaSQL (one embed file ships every planner-
// owned table, including this one).

package agentprofile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

// Store persists AgentProfile rows. Wraps a *sql.DB the caller
// owns; nil db disables every method (callers branch on nil-store
// rather than feature-flagging).
type Store struct {
	db *sql.DB
}

// NewStore wraps the given *sql.DB. Returns nil when db is nil so
// the wiring stays branch-free at the call site.
func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

// EnsureSchema applies planner.SchemaSQL — the same DDL bundle
// the rest of the planner uses — so a process that brings up the
// agent-profile store also brings up session_profiles, scorer_
// grades, etc. Safe to call multiple times.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("agentprofile.Store.EnsureSchema: nil store")
	}
	if _, err := s.db.ExecContext(ctx, planner.SchemaSQL); err != nil {
		return fmt.Errorf("agentprofile.Store.EnsureSchema: %w", err)
	}
	return nil
}

const profileSelectColumns = `
	agent_id, concepts, files,
	lifetime_bead_count, last_activity_at,
	created_at, updated_at
`

// Get reads the profile for agentID. Returns (nil, nil) when no
// row exists — matches the planner readers' "absent ≠ error"
// convention.
func (s *Store) Get(ctx context.Context, agentID core.AgentID) (*AgentProfile, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if agentID == "" {
		return nil, errors.New("agentprofile.Store.Get: empty AgentID")
	}
	row := s.db.QueryRowContext(ctx,
		"SELECT "+profileSelectColumns+" FROM agent_profiles WHERE agent_id = ?",
		string(agentID),
	)
	return scanProfile(row)
}

// List returns every profile, sorted by agent id. The selection
// layer doesn't fan over this — it joins per-session — but the
// CLI's `gemba agent profile --all` and operator audits read it.
func (s *Store) List(ctx context.Context) ([]AgentProfile, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+profileSelectColumns+" FROM agent_profiles ORDER BY agent_id ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("agentprofile.Store.List: %w", err)
	}
	defer rows.Close()

	var out []AgentProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentprofile.Store.List: rows: %w", err)
	}
	return out, nil
}

// Upsert writes p verbatim. Used by tooling and tests that want
// to seed a profile without going through RecordCompletion. The
// retrospective hook should call RecordCompletion instead.
func (s *Store) Upsert(ctx context.Context, p *AgentProfile) error {
	if s == nil || s.db == nil {
		return errors.New("agentprofile.Store.Upsert: nil store")
	}
	if p == nil {
		return errors.New("agentprofile.Store.Upsert: nil profile")
	}
	if p.AgentID == "" {
		return errors.New("agentprofile.Store.Upsert: empty AgentID")
	}
	conceptsJSON, err := jsonOrNil(p.Concepts)
	if err != nil {
		return fmt.Errorf("encode concepts: %w", err)
	}
	filesJSON, err := jsonOrNil(p.Files)
	if err != nil {
		return fmt.Errorf("encode files: %w", err)
	}
	const stmt = `
		INSERT INTO agent_profiles (
			agent_id, concepts, files,
			lifetime_bead_count, last_activity_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			concepts = VALUES(concepts),
			files = VALUES(files),
			lifetime_bead_count = VALUES(lifetime_bead_count),
			last_activity_at = VALUES(last_activity_at),
			updated_at = VALUES(updated_at)`
	if _, err := s.db.ExecContext(ctx, stmt,
		string(p.AgentID), conceptsJSON, filesJSON,
		p.LifetimeBeadCount, p.LastActivityAt,
		p.CreatedAt, p.UpdatedAt,
	); err != nil {
		return fmt.Errorf("agentprofile.Store.Upsert: %w", err)
	}
	return nil
}

// CompletionEvent is the input to RecordCompletion — what the
// retrospective hook knows when a bead lands. Concepts/Files come
// from the bead's actual (post-merge) values; ContributionScale,
// when zero, defaults to 1 / max(LifetimeBeadCount+1, 1) so a
// single bead doesn't dominate a long-running agent's profile.
type CompletionEvent struct {
	AgentID  core.AgentID
	BeadID   core.WorkItemID
	Concepts []planner.ConceptTag
	Files    []string

	// HalfLifeDays overrides DefaultDecayHalfLifeDays per call.
	HalfLifeDays float64
	// ContributionScale, when set > 0, overrides the default
	// 1/(lifetime+1) scaling. Tests pass 1.0 for predictable
	// arithmetic; production leaves it 0 and accepts the default.
	ContributionScale float64
	// Now is injected for deterministic test runs. nil → time.Now.
	Now func() time.Time
}

// RecordCompletion ages the existing profile by the days elapsed
// since LastActivityAt, scales the bead's contribution by
// 1/(lifetime+1), merges, bumps the lifetime counter, and stamps
// LastActivityAt = Now. Returns the resulting profile so callers
// can echo / publish without an extra read.
func (s *Store) RecordCompletion(ctx context.Context, ev CompletionEvent) (*AgentProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("agentprofile.Store.RecordCompletion: nil store")
	}
	if ev.AgentID == "" {
		return nil, errors.New("agentprofile.Store.RecordCompletion: empty AgentID")
	}
	now := time.Now().UTC()
	if ev.Now != nil {
		now = ev.Now()
	}
	prior, err := s.Get(ctx, ev.AgentID)
	if err != nil {
		return nil, fmt.Errorf("read prior: %w", err)
	}

	next := AgentProfile{AgentID: ev.AgentID}
	if prior != nil {
		next = *prior
		days := daysBetween(now, prior.LastActivityAt)
		next.Concepts = AgeByDays(prior.Concepts, days, ev.HalfLifeDays)
		next.Files = AgeFilesByDays(prior.Files, days, ev.HalfLifeDays)
	} else {
		next.CreatedAt = now
	}

	scale := ev.ContributionScale
	if scale <= 0 {
		// Prior count + 1 (the bead landing now) so the very first
		// bead lands at weight 1.0, the second at 0.5, third at
		// 1/3, etc. A single bead can't dominate; over time the
		// profile averages out across the agent's history.
		scale = 1.0 / float64(next.LifetimeBeadCount+1)
	}
	next.Concepts = planner.MergeContribution(next.Concepts, ev.Concepts, scale)
	next.Files = planner.MergeFileContribution(next.Files, ev.Files, scale)
	next.LifetimeBeadCount++
	next.LastActivityAt = now
	next.UpdatedAt = now
	if next.CreatedAt.IsZero() {
		next.CreatedAt = now
	}

	if err := s.Upsert(ctx, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

// jsonOrNil mirrors planner.jsonOrNil: empty maps land as SQL
// NULL so a fresh row stays compact rather than carrying an
// empty "{}" placeholder.
func jsonOrNil(v any) (sql.NullString, error) {
	switch m := v.(type) {
	case map[planner.ConceptTag]float64:
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
	default:
		bs, err := json.Marshal(v)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row rowScanner) (*AgentProfile, error) {
	var (
		p             AgentProfile
		agentID       string
		conceptsRaw   sql.NullString
		filesRaw      sql.NullString
		lastActivity  time.Time
		createdAt     time.Time
		updatedAt     time.Time
		lifetimeCount int64
	)
	err := row.Scan(
		&agentID, &conceptsRaw, &filesRaw,
		&lifetimeCount, &lastActivity,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentprofile.Store: scan: %w", err)
	}
	p.AgentID = core.AgentID(agentID)
	p.LifetimeBeadCount = lifetimeCount
	p.LastActivityAt = lastActivity
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	if conceptsRaw.Valid && conceptsRaw.String != "" {
		var m map[planner.ConceptTag]float64
		if err := json.Unmarshal([]byte(conceptsRaw.String), &m); err != nil {
			return nil, fmt.Errorf("decode concepts for %s: %w", agentID, err)
		}
		p.Concepts = m
	}
	if filesRaw.Valid && filesRaw.String != "" {
		var m map[string]float64
		if err := json.Unmarshal([]byte(filesRaw.String), &m); err != nil {
			return nil, fmt.Errorf("decode files for %s: %w", agentID, err)
		}
		p.Files = m
	}
	return &p, nil
}
