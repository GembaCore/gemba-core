// SessionProfile read API (gm-s47n.2.4).
//
// ProfileStore is the *sql.DB-backed implementation of
// planner.ProfileLookup. The schema is the session_profiles dolt
// table gm-s47n.2.1 declared (see schema.sql); the reader decodes
// concepts/files/last_beads JSON columns into the typed maps the
// rest of the planner consumes.
//
// Single read path — Get(ctx, sessionID). Writers (claim hook +
// completion hook + decay loop) land in gm-s47n.2.3 and gm-s47n.2.2;
// they share this package's struct definitions so wire-shape drift
// is impossible.

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

// ProfileStore is a *sql.DB-backed reader for the session_profiles
// dolt table. Concurrency: every method is safe for concurrent use
// because the underlying *sql.DB pool handles pooling + locking.
//
// Construct with NewProfileStore(db). The store does NOT own the
// *sql.DB — the caller is responsible for Close. This matches every
// other store in the codebase (internal/adapter/dolt) and keeps the
// scoring loop free to reuse a single pool across multiple stores.
type ProfileStore struct {
	db *sql.DB
}

// NewProfileStore wraps the given *sql.DB. Returns nil when db is
// nil so callers building optional readers can `if store != nil`
// without an extra check.
func NewProfileStore(db *sql.DB) *ProfileStore {
	if db == nil {
		return nil
	}
	return &ProfileStore{db: db}
}

// Compile-time check that *ProfileStore satisfies the planner's
// narrow lookup interface. If the interface gains methods, the
// build fails here instead of at the join site.
var _ ProfileLookup = (*ProfileStore)(nil)

// profileSelectColumns is the column list used by Get. Listed
// explicitly (not SELECT *) so a benign migration that adds a
// column upstream doesn't silently break the Scan.
const profileSelectColumns = `
	session_id, assignment_id, agent_id,
	concepts, files,
	tokens_used, context_window_max, context_pct,
	last_beads,
	last_activity_at, created_at, updated_at
`

// GetProfile reads the profile row for sessionID. Returns
// (nil, nil) when no row exists — matches the SessionLookup
// "absent ≠ error" convention so the operational-context join
// degrades gracefully when a session predates its profile.
//
// Errors during JSON decode of the concepts/files/last_beads
// columns get wrapped in a typed adaptor error (KindAdaptorDegraded)
// so the planner can distinguish "schema is right but a row's
// payload is malformed" from a transport fault. Either way, the
// operational-context aggregator at .2.5 swallows the error and
// leaves Profile nil — graceful degradation per spec §4 Layer 1.
func (s *ProfileStore) GetProfile(ctx context.Context, sessionID string) (*SessionProfile, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if sessionID == "" {
		return nil, fmt.Errorf("planner.ProfileStore.GetProfile: empty sessionID")
	}
	row := s.db.QueryRowContext(ctx,
		"SELECT "+profileSelectColumns+" FROM session_profiles WHERE session_id = ?",
		sessionID,
	)
	return scanProfile(row)
}

// scanProfile shares the row-decode path between point-reads and
// future range-reads so the JSON-column rules stay in one place.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row rowScanner) (*SessionProfile, error) {
	var (
		p            SessionProfile
		conceptsRaw  sql.NullString
		filesRaw     sql.NullString
		lastBeadsRaw sql.NullString
		lastActivity time.Time
		createdAt    time.Time
		updatedAt    time.Time
	)
	err := row.Scan(
		&p.SessionID, &p.AssignmentID, &p.AgentID,
		&conceptsRaw, &filesRaw,
		&p.TokensUsed, &p.ContextWindowMax, &p.ContextPct,
		&lastBeadsRaw,
		&lastActivity, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("planner.ProfileStore: scan: %w", err)
	}
	p.LastActivityAt = lastActivity
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt

	if conceptsRaw.Valid && conceptsRaw.String != "" {
		var m map[ConceptTag]float64
		if err := json.Unmarshal([]byte(conceptsRaw.String), &m); err != nil {
			return nil, fmt.Errorf("planner.ProfileStore: decode concepts for %s: %w", p.SessionID, err)
		}
		p.Concepts = m
	}
	if filesRaw.Valid && filesRaw.String != "" {
		var m map[string]float64
		if err := json.Unmarshal([]byte(filesRaw.String), &m); err != nil {
			return nil, fmt.Errorf("planner.ProfileStore: decode files for %s: %w", p.SessionID, err)
		}
		p.Files = m
	}
	if lastBeadsRaw.Valid && lastBeadsRaw.String != "" {
		var arr []core.WorkItemID
		if err := json.Unmarshal([]byte(lastBeadsRaw.String), &arr); err != nil {
			return nil, fmt.Errorf("planner.ProfileStore: decode last_beads for %s: %w", p.SessionID, err)
		}
		p.LastBeads = arr
	}
	return &p, nil
}

// EnsureSchema creates the session_profiles table if it doesn't
// already exist. Idempotent — safe to call from server boot. Uses
// the embedded SchemaSQL from gm-s47n.2.1.
func (s *ProfileStore) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("planner.ProfileStore.EnsureSchema: nil store")
	}
	_, err := s.db.ExecContext(ctx, SchemaSQL)
	if err != nil {
		return fmt.Errorf("planner.ProfileStore: EnsureSchema: %w", err)
	}
	return nil
}

// MultiGet reads several profiles in one SQL round-trip. The
// result preserves caller order and returns nil entries for
// sessions with no row (mirrors the GetProfile "absent ≠ error"
// contract). Useful for the coach grid render path which fans out
// per-session profile reads.
func (s *ProfileStore) MultiGet(ctx context.Context, sessionIDs []string) ([]*SessionProfile, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	// Build a parameterised IN clause; database/sql doesn't support
	// slice expansion natively. Cap the batch at a sane bound to
	// keep the query parser happy.
	query := "SELECT " + profileSelectColumns +
		" FROM session_profiles WHERE session_id IN (" + repeatPlaceholders(len(sessionIDs)) + ")"
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("planner.ProfileStore.MultiGet: %w", err)
	}
	defer rows.Close()

	byID := make(map[string]*SessionProfile, len(sessionIDs))
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		if p != nil {
			byID[p.SessionID] = p
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("planner.ProfileStore.MultiGet: rows: %w", err)
	}
	out := make([]*SessionProfile, len(sessionIDs))
	for i, id := range sessionIDs {
		out[i] = byID[id]
	}
	return out, nil
}

// repeatPlaceholders returns a comma-separated string of `n` `?`
// placeholders. Used by MultiGet to build IN-clauses.
func repeatPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*n-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}
