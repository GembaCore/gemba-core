// dispatch_decisions persistence (gm-s47n.6.2).
//
// One row per pick. The retrospective (gm-s47n.8.x) joins these
// against scorer_grades on bead_id to grade predicted-vs-actual.

package dispatch

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

// Mode names who made the pick. The auto-dispatch daemon (when it
// lands, gm-s47n.6.3) writes ModeAuto; the coach UI writes ModeCoach.
type Mode string

const (
	ModeCoach Mode = "coach"
	ModeAuto  Mode = "auto"
)

// Decision is the persisted dispatch record. The JSON-encoded
// snapshot fields capture the planner's view at decision time so
// the retrospective can grade them later.
type Decision struct {
	// ID is a stable per-row identifier. The store generates one when
	// the caller leaves it empty so callers don't have to thread a UUID
	// through the dispatch path.
	ID string `json:"id,omitempty"`

	BeadID    core.WorkItemID `json:"bead_id"`
	DecidedAt time.Time       `json:"decided_at"`

	// SessionID + AgentID identify the session that received the
	// bead. Empty AgentID is tolerated (the dispatcher may write
	// before the session has resolved its agent).
	SessionID string       `json:"session_id,omitempty"`
	AgentID   core.AgentID `json:"agent_id,omitempty"`

	// DecidedBy is the operator id for coach picks; blank for auto.
	DecidedBy string `json:"decided_by,omitempty"`

	// Mode = "coach" | "auto". Defaults to ModeCoach when blank.
	Mode Mode `json:"mode,omitempty"`

	// Affinity carries the planner's predicted score breakdown for
	// the picked bead at decision time. AffinityCombined is mirrored
	// to its own column for sargable filters; the full breakdown
	// rides in affinity_json.
	Affinity planner.AffinityScores `json:"affinity"`

	// Conflicts captures every conflict edge the planner had visible
	// at decision time. Stored verbatim so the retrospective can ask
	// "did the coach pick into a known workspace collision?" without
	// recomputing the conflict graph from a stale snapshot.
	Conflicts ConflictSnapshot `json:"conflicts"`

	// ReadySet lists the alternative beads that were on the grid at
	// decision time. Required for "of the alternatives, was this
	// actually the highest-affinity pick?" — a question the
	// retrospective cannot answer without it.
	ReadySet []ReadySetEntry `json:"ready_set,omitempty"`

	// OperatorReason is the optional one-line explanation a coach-
	// mode picker can attach via `gemba dispatch --reason "..."`
	// (gm-v5z2.8 §7.5). Free-form; the calibration loop reads it
	// alongside (recommended_top_bead, picked_bead, score_delta)
	// to surface why an operator overrode the planner's top pick.
	// Empty for auto-mode dispatches and for coach picks where
	// the operator skipped the prompt.
	OperatorReason string `json:"operator_reason,omitempty"`

	// CreatedAt is when this row was written; distinct from
	// DecidedAt which is when the human pressed the button. The
	// store stamps it from now() when zero.
	CreatedAt time.Time `json:"created_at"`
}

// ConflictSnapshot bundles the workspace + semantic conflict edges
// the planner saw at decision time. Both slices are nil-safe and
// JSON-encoded into one column.
type ConflictSnapshot struct {
	Workspace []planner.WorkspaceCollision `json:"workspace,omitempty"`
	Semantic  []planner.SemanticConflict   `json:"semantic,omitempty"`
}

// ReadySetEntry is the dispatch-time view of one alternative bead.
// Only the bead id + the predicted combined affinity for the chosen
// session is required — that's enough for the retro to ask
// "could a different bead have predicted higher?"
type ReadySetEntry struct {
	BeadID            core.WorkItemID `json:"bead_id"`
	AffinityCombined  float64         `json:"affinity_combined"`
	WorkspaceConflict bool            `json:"workspace_conflict,omitempty"`
}

// Store persists dispatch_decisions rows. Mirrors retro.Store: pass
// nil to disable so callers can `if store != nil` without feature
// flags.
type Store struct {
	db *sql.DB
}

// NewStore wraps the given *sql.DB. Returns nil when db is nil so
// the dispatch wiring stays branch-free.
func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

// Insert writes one Decision row. Generates an ID when the caller
// leaves it blank. Returns the stored row's ID so the HTTP handler
// can surface it back to the SPA for later cross-reference.
//
// CreatedAt is stamped from now() when zero; DecidedAt is required —
// the dispatcher knows when the pick happened, the store does not.
func (s *Store) Insert(ctx context.Context, d Decision) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("dispatch.Store.Insert: nil store")
	}
	if d.BeadID == "" {
		return "", errors.New("dispatch.Store.Insert: empty BeadID")
	}
	if d.DecidedAt.IsZero() {
		return "", errors.New("dispatch.Store.Insert: zero DecidedAt")
	}
	if d.ID == "" {
		id, err := newID()
		if err != nil {
			return "", fmt.Errorf("dispatch.Store.Insert: %w", err)
		}
		d.ID = id
	}
	if d.Mode == "" {
		d.Mode = ModeCoach
	}

	affinityJSON, err := json.Marshal(d.Affinity)
	if err != nil {
		return "", fmt.Errorf("encode affinity: %w", err)
	}
	conflictsJSON, err := json.Marshal(d.Conflicts)
	if err != nil {
		return "", fmt.Errorf("encode conflicts: %w", err)
	}
	readySetJSON, err := encodeReadySet(d.ReadySet)
	if err != nil {
		return "", fmt.Errorf("encode ready_set: %w", err)
	}

	createdAt := d.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	const stmt = `
		INSERT INTO dispatch_decisions (
			id, bead_id, decided_at,
			session_id, agent_id, decided_by, mode,
			affinity_combined, affinity_json,
			conflicts_json, ready_set_json,
			operator_reason,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, stmt,
		d.ID, string(d.BeadID), d.DecidedAt,
		d.SessionID, string(d.AgentID), d.DecidedBy, string(d.Mode),
		d.Affinity.Combined, string(affinityJSON),
		string(conflictsJSON), readySetJSON,
		d.OperatorReason,
		createdAt,
	)
	if err != nil {
		return "", fmt.Errorf("dispatch.Store.Insert: %w", err)
	}
	return d.ID, nil
}

const decisionSelectColumns = `
	id, bead_id, decided_at,
	session_id, agent_id, decided_by, mode,
	affinity_combined, affinity_json,
	conflicts_json, ready_set_json,
	operator_reason,
	created_at
`

// Get reads a single Decision by ID. Returns (nil, nil) when no
// row matches — matches the planner's "absent ≠ error" convention.
func (s *Store) Get(ctx context.Context, id string) (*Decision, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if id == "" {
		return nil, errors.New("dispatch.Store.Get: empty id")
	}
	row := s.db.QueryRowContext(ctx,
		"SELECT "+decisionSelectColumns+
			" FROM dispatch_decisions WHERE id = ?",
		id,
	)
	return scanDecision(row)
}

// ListFilter narrows List output. Zero-value = no filtering.
type ListFilter struct {
	// BeadID restricts to decisions for this bead. Empty = all
	// beads. The retrospective uses this to find every dispatch
	// for a bead that has just landed.
	BeadID core.WorkItemID
	// SessionID restricts to decisions that picked for this
	// session. Empty = all sessions.
	SessionID string
	// Mode restricts to coach or auto. Empty = both modes.
	Mode Mode
	// MinAffinityCombined keeps rows with affinity_combined >= this
	// value. Defaults to 0 (no filter).
	MinAffinityCombined float64
	// Limit caps the result count. <=0 means no cap; List
	// applies a hard ceiling of 1000 to keep the unbounded path
	// from accidentally pulling the whole table.
	Limit int
}

// List returns rows matching f, newest decision first.
func (s *Store) List(ctx context.Context, f ListFilter) ([]Decision, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	const hardCap = 1000
	limit := f.Limit
	if limit <= 0 || limit > hardCap {
		limit = hardCap
	}

	q := "SELECT " + decisionSelectColumns + " FROM dispatch_decisions WHERE 1=1"
	args := []any{}
	if f.BeadID != "" {
		q += " AND bead_id = ?"
		args = append(args, string(f.BeadID))
	}
	if f.SessionID != "" {
		q += " AND session_id = ?"
		args = append(args, f.SessionID)
	}
	if f.Mode != "" {
		q += " AND mode = ?"
		args = append(args, string(f.Mode))
	}
	if f.MinAffinityCombined > 0 {
		q += " AND affinity_combined >= ?"
		args = append(args, f.MinAffinityCombined)
	}
	q += " ORDER BY decided_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("dispatch.Store.List: %w", err)
	}
	defer rows.Close()

	var out []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, *d)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dispatch.Store.List: rows: %w", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDecision(row rowScanner) (*Decision, error) {
	var (
		d             Decision
		beadID        string
		agentID       string
		mode          string
		affinityJSON  sql.NullString
		conflictsJSON sql.NullString
		readySetJSON  sql.NullString
		decidedAt     time.Time
		createdAt     time.Time
		combined      float64
	)
	var operatorReason sql.NullString
	err := row.Scan(
		&d.ID, &beadID, &decidedAt,
		&d.SessionID, &agentID, &d.DecidedBy, &mode,
		&combined, &affinityJSON,
		&conflictsJSON, &readySetJSON,
		&operatorReason,
		&createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dispatch.Store: scan: %w", err)
	}
	d.BeadID = core.WorkItemID(beadID)
	d.AgentID = core.AgentID(agentID)
	d.Mode = Mode(mode)
	d.DecidedAt = decidedAt
	d.CreatedAt = createdAt
	d.Affinity.Combined = combined
	if operatorReason.Valid {
		d.OperatorReason = operatorReason.String
	}

	if affinityJSON.Valid && affinityJSON.String != "" {
		if err := json.Unmarshal([]byte(affinityJSON.String), &d.Affinity); err != nil {
			return nil, fmt.Errorf("decode affinity for %s: %w", d.ID, err)
		}
	}
	if conflictsJSON.Valid && conflictsJSON.String != "" {
		if err := json.Unmarshal([]byte(conflictsJSON.String), &d.Conflicts); err != nil {
			return nil, fmt.Errorf("decode conflicts for %s: %w", d.ID, err)
		}
	}
	if readySetJSON.Valid && readySetJSON.String != "" {
		if err := json.Unmarshal([]byte(readySetJSON.String), &d.ReadySet); err != nil {
			return nil, fmt.Errorf("decode ready_set for %s: %w", d.ID, err)
		}
	}
	return &d, nil
}

// EnsureSchema applies the embedded planner.SchemaSQL DDL. Safe to
// call multiple times; idempotent CREATE TABLE IF NOT EXISTS.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("dispatch.Store.EnsureSchema: nil store")
	}
	_, err := s.db.ExecContext(ctx, planner.SchemaSQL)
	if err != nil {
		return fmt.Errorf("dispatch.Store.EnsureSchema: %w", err)
	}
	return nil
}

func encodeReadySet(rs []ReadySetEntry) (sql.NullString, error) {
	if len(rs) == 0 {
		return sql.NullString{}, nil
	}
	bs, err := json.Marshal(rs)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(bs), Valid: true}, nil
}

// newID returns a 128-bit hex identifier. crypto/rand is fine here:
// dispatch is human-paced (a coach pick per several minutes) and the
// auto-dispatch daemon's loop is rate-limited per spec §5 — neither
// path is a hot loop.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
