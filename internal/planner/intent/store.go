// session_intents persistence + audit log (gm-v5z2.3).
//
// Three operations make up the surface:
//
//   Set: write or replace the intent for a session and append a
//   "set" audit row. The prior intent (if any) lands in the audit
//   row's prior_json so a retrospective can replay focus changes.
//
//   Clear: delete the intent row and append a "clear" audit row.
//   No-op when no row exists; the audit row still lands so an
//   operator's "I tried to clear" intent is visible in the log.
//
//   Get / List / Audit: read paths the CLI and selection layer
//   share. Get returns (nil, nil) for no-row sessions matching the
//   "absent ≠ error" convention the rest of the planner uses.

package intent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MikeBengtson/gemba/internal/planner"
)

// Store persists session intents + the audit log. Wraps a *sql.DB
// pool the caller owns; nil db disables the writer (callers branch
// on `if store != nil` rather than feature-flag).
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

// EnsureSchema applies the embedded planner.SchemaSQL DDL —
// idempotent CREATE TABLE IF NOT EXISTS for the intent tables
// alongside the rest of the planner schema. Safe to call multiple
// times; the rest of the planner has its own EnsureSchema and
// they share the embedded SQL file so duplicate calls are fine.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("intent.Store.EnsureSchema: nil store")
	}
	if _, err := s.db.ExecContext(ctx, planner.SchemaSQL); err != nil {
		return fmt.Errorf("intent.Store.EnsureSchema: %w", err)
	}
	return nil
}

// SetInput is the input to Set. SessionID is required; the
// restrictor fields validate via Intent.Validate before any SQL
// fires. Now is injected for deterministic test runs.
type SetInput struct {
	SessionID      string
	EpicID         string
	Label          string
	BeadIDRegex    string
	Rationale      string
	DemotionFactor float64
	Actor          string
	Now            func() time.Time
}

// Set writes (or replaces) the intent for SessionID and appends a
// "set" audit row. Returns the resulting Intent so the caller can
// echo the canonical form (e.g. with the default demotion applied).
func (s *Store) Set(ctx context.Context, in SetInput) (*Intent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("intent.Store.Set: nil store")
	}
	if in.SessionID == "" {
		return nil, errors.New("intent.Store.Set: empty SessionID")
	}
	candidate := Intent{
		SessionID:      in.SessionID,
		EpicID:         in.EpicID,
		Label:          in.Label,
		BeadIDRegex:    in.BeadIDRegex,
		Rationale:      in.Rationale,
		DemotionFactor: in.DemotionFactor,
	}
	if candidate.IsZero() {
		return nil, errors.New("intent.Store.Set: at least one of epic_id / label / bead_id_regex must be set")
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if in.Now != nil {
		now = in.Now()
	}

	prior, err := s.Get(ctx, in.SessionID)
	if err != nil {
		return nil, fmt.Errorf("intent.Store.Set: read prior: %w", err)
	}
	candidate.UpdatedAt = now
	if prior != nil {
		candidate.CreatedAt = prior.CreatedAt
	} else {
		candidate.CreatedAt = now
	}

	const upsert = `
		INSERT INTO session_intents (
			session_id, epic_id, label, bead_id_regex, rationale,
			demotion_factor, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			epic_id = VALUES(epic_id),
			label = VALUES(label),
			bead_id_regex = VALUES(bead_id_regex),
			rationale = VALUES(rationale),
			demotion_factor = VALUES(demotion_factor),
			updated_at = VALUES(updated_at)`
	if _, err := s.db.ExecContext(ctx, upsert,
		candidate.SessionID, candidate.EpicID, candidate.Label,
		candidate.BeadIDRegex, candidate.Rationale,
		candidate.DemotionFactor, candidate.CreatedAt, candidate.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("intent.Store.Set: upsert: %w", err)
	}
	if err := s.appendAudit(ctx, AuditEntry{
		SessionID: in.SessionID,
		Action:    AuditSet,
		Prior:     prior,
		Next:      &candidate,
		At:        now,
		Actor:     in.Actor,
	}); err != nil {
		return nil, fmt.Errorf("intent.Store.Set: audit: %w", err)
	}
	return &candidate, nil
}

// Clear deletes the intent row for SessionID and appends a "clear"
// audit row. Idempotent: clearing an already-cleared session
// returns nil + still writes the audit row.
func (s *Store) Clear(ctx context.Context, sessionID string, actor string, nowFn func() time.Time) error {
	if s == nil || s.db == nil {
		return errors.New("intent.Store.Clear: nil store")
	}
	if sessionID == "" {
		return errors.New("intent.Store.Clear: empty SessionID")
	}
	now := time.Now().UTC()
	if nowFn != nil {
		now = nowFn()
	}
	prior, err := s.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("intent.Store.Clear: read prior: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM session_intents WHERE session_id = ?", sessionID,
	); err != nil {
		return fmt.Errorf("intent.Store.Clear: delete: %w", err)
	}
	if err := s.appendAudit(ctx, AuditEntry{
		SessionID: sessionID,
		Action:    AuditClear,
		Prior:     prior,
		Next:      nil,
		At:        now,
		Actor:     actor,
	}); err != nil {
		return fmt.Errorf("intent.Store.Clear: audit: %w", err)
	}
	return nil
}

const intentSelectColumns = `
	session_id, epic_id, label, bead_id_regex, rationale,
	demotion_factor, created_at, updated_at
`

// Get reads the live intent for sessionID. Returns (nil, nil) when
// no row exists — matches the rest of the planner's reader
// "absent ≠ error" convention.
func (s *Store) Get(ctx context.Context, sessionID string) (*Intent, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if sessionID == "" {
		return nil, errors.New("intent.Store.Get: empty SessionID")
	}
	row := s.db.QueryRowContext(ctx,
		"SELECT "+intentSelectColumns+" FROM session_intents WHERE session_id = ?",
		sessionID,
	)
	return scanIntent(row)
}

// List returns every live intent. Used by the CLI's
// `gemba session focus list` and by the selection layer's per-tick
// snapshot read.
func (s *Store) List(ctx context.Context) ([]Intent, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+intentSelectColumns+" FROM session_intents ORDER BY session_id ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("intent.Store.List: %w", err)
	}
	defer rows.Close()

	var out []Intent
	for rows.Next() {
		i, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		if i != nil {
			out = append(out, *i)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intent.Store.List: rows: %w", err)
	}
	return out, nil
}

// Audit returns the audit history for sessionID, newest first.
// limit ≤ 0 returns the full history; the caller's responsibility
// to set a reasonable bound for hot paths.
func (s *Store) Audit(ctx context.Context, sessionID string, limit int) ([]AuditEntry, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if sessionID == "" {
		return nil, errors.New("intent.Store.Audit: empty SessionID")
	}
	q := `
		SELECT id, session_id, action, prior_json, next_json, actor, at
		FROM session_intent_audit
		WHERE session_id = ?
		ORDER BY at DESC, id DESC
	`
	args := []any{sessionID}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("intent.Store.Audit: %w", err)
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var (
			e         AuditEntry
			priorJSON sql.NullString
			nextJSON  sql.NullString
		)
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.Action,
			&priorJSON, &nextJSON, &e.Actor, &e.At,
		); err != nil {
			return nil, fmt.Errorf("intent.Store.Audit: scan: %w", err)
		}
		if priorJSON.Valid && priorJSON.String != "" {
			var i Intent
			if err := json.Unmarshal([]byte(priorJSON.String), &i); err != nil {
				return nil, fmt.Errorf("intent.Store.Audit: decode prior_json: %w", err)
			}
			e.Prior = &i
		}
		if nextJSON.Valid && nextJSON.String != "" {
			var i Intent
			if err := json.Unmarshal([]byte(nextJSON.String), &i); err != nil {
				return nil, fmt.Errorf("intent.Store.Audit: decode next_json: %w", err)
			}
			e.Next = &i
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intent.Store.Audit: rows: %w", err)
	}
	return out, nil
}

func (s *Store) appendAudit(ctx context.Context, e AuditEntry) error {
	priorJSON, err := nullableJSON(e.Prior)
	if err != nil {
		return err
	}
	nextJSON, err := nullableJSON(e.Next)
	if err != nil {
		return err
	}
	const stmt = `
		INSERT INTO session_intent_audit (
			session_id, action, prior_json, next_json, actor, at
		) VALUES (?, ?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, stmt,
		e.SessionID, string(e.Action), priorJSON, nextJSON, e.Actor, e.At,
	)
	return err
}

func nullableJSON(i *Intent) (sql.NullString, error) {
	if i == nil {
		return sql.NullString{}, nil
	}
	bs, err := json.Marshal(i)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(bs), Valid: true}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIntent(row rowScanner) (*Intent, error) {
	var (
		i         Intent
		rationale sql.NullString
		createdAt time.Time
		updatedAt time.Time
	)
	err := row.Scan(
		&i.SessionID, &i.EpicID, &i.Label, &i.BeadIDRegex, &rationale,
		&i.DemotionFactor, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("intent.Store: scan: %w", err)
	}
	if rationale.Valid {
		i.Rationale = rationale.String
	}
	i.CreatedAt = createdAt
	i.UpdatedAt = updatedAt
	return &i, nil
}
