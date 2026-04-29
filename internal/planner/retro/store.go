// scorer_grades persistence (gm-s47n.8.2).
//
// The retrospective writer lands one row per (bead_id, closed_at).
// The store is intentionally narrow: Insert + Get + List. The close
// hook (gm-s47n.8.3) calls Insert; the queryable view (gm-s47n.8.4)
// calls List with a divergence-threshold filter.

package retro

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
)

// Grade is the persisted retrospective record. The Diff inside is
// the full comparator output; the top-level columns surface as
// indexed values for the query view.
type Grade struct {
	BeadID    core.WorkItemID `json:"bead_id"`
	ClosedAt  time.Time       `json:"closed_at"`
	SessionID string          `json:"session_id,omitempty"`
	AgentID   core.AgentID    `json:"agent_id,omitempty"`

	Declared Declared `json:"declared"`
	Actual   Actual   `json:"actual"`
	Diff     Diff     `json:"diff"`

	CreatedAt time.Time `json:"created_at"`
}

// TargetDivergence + ConceptDivergence accessors mirror the columns
// (which are denormalised from Grade.Diff so SQL filters can probe
// them without JSON_EXTRACT). Callers that already hold a Grade
// should prefer Grade.Diff directly; these are for query-result
// readability.
func (g Grade) TargetDivergence() float64  { return g.Diff.TargetDivergence }
func (g Grade) ConceptDivergence() float64 { return g.Diff.ConceptDivergence }

// Store persists scorer_grades rows. Wraps a *sql.DB pool the
// caller owns. Construction mirrors planner.ProfileStore: pass nil
// to disable the writer (callers can `if store != nil` rather than
// branch on a feature flag).
type Store struct {
	db *sql.DB
}

// NewStore wraps the given *sql.DB. Returns nil when db is nil so
// the close-hook wiring stays branch-free.
func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

// Insert writes one Grade row. Idempotent on (bead_id, closed_at):
// re-running the retro for the same bead at the same close
// timestamp overwrites the prior row's payload (declared/actual/
// divergence). A genuinely new retro at a later timestamp lands a
// fresh row so historical comparisons remain intact.
//
// CreatedAt is ignored on input; the store stamps it from now() —
// that column reflects when the retro RAN, distinct from
// closed_at which is when the bead landed.
func (s *Store) Insert(ctx context.Context, g Grade) error {
	if s == nil || s.db == nil {
		return errors.New("retro.Store.Insert: nil store")
	}
	if g.BeadID == "" {
		return errors.New("retro.Store.Insert: empty BeadID")
	}
	if g.ClosedAt.IsZero() {
		return errors.New("retro.Store.Insert: zero ClosedAt")
	}

	declTargets, err := jsonOrNil(g.Declared.Targets)
	if err != nil {
		return fmt.Errorf("encode declared.targets: %w", err)
	}
	declConcepts, err := jsonOrNil(g.Declared.Concepts)
	if err != nil {
		return fmt.Errorf("encode declared.concepts: %w", err)
	}
	actFiles, err := jsonOrNil(g.Actual.Files)
	if err != nil {
		return fmt.Errorf("encode actual.files: %w", err)
	}
	actConcepts, err := jsonOrNil(g.Actual.Concepts)
	if err != nil {
		return fmt.Errorf("encode actual.concepts: %w", err)
	}
	diffBytes, err := json.Marshal(g.Diff)
	if err != nil {
		return fmt.Errorf("encode diff: %w", err)
	}

	createdAt := g.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	const stmt = `
		INSERT INTO scorer_grades (
			bead_id, closed_at,
			session_id, agent_id,
			declared_targets, declared_concepts,
			actual_files, actual_concepts,
			target_divergence, concept_divergence,
			diff_json,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			session_id = VALUES(session_id),
			agent_id = VALUES(agent_id),
			declared_targets = VALUES(declared_targets),
			declared_concepts = VALUES(declared_concepts),
			actual_files = VALUES(actual_files),
			actual_concepts = VALUES(actual_concepts),
			target_divergence = VALUES(target_divergence),
			concept_divergence = VALUES(concept_divergence),
			diff_json = VALUES(diff_json),
			created_at = VALUES(created_at)`
	_, err = s.db.ExecContext(ctx, stmt,
		string(g.BeadID), g.ClosedAt,
		g.SessionID, string(g.AgentID),
		declTargets, declConcepts,
		actFiles, actConcepts,
		g.Diff.TargetDivergence, g.Diff.ConceptDivergence,
		string(diffBytes),
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("retro.Store.Insert: %w", err)
	}
	return nil
}

const gradeSelectColumns = `
	bead_id, closed_at,
	session_id, agent_id,
	declared_targets, declared_concepts,
	actual_files, actual_concepts,
	target_divergence, concept_divergence,
	diff_json,
	created_at
`

// Get reads the most-recent retro for the given bead. Returns
// (nil, nil) when no row exists — matches the rest of the planner's
// "absent ≠ error" reader convention.
func (s *Store) Get(ctx context.Context, beadID core.WorkItemID) (*Grade, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if beadID == "" {
		return nil, errors.New("retro.Store.Get: empty beadID")
	}
	row := s.db.QueryRowContext(ctx,
		"SELECT "+gradeSelectColumns+
			" FROM scorer_grades WHERE bead_id = ? ORDER BY closed_at DESC LIMIT 1",
		string(beadID),
	)
	return scanGrade(row)
}

// ListFilter narrows List output. Zero-value = no filtering.
type ListFilter struct {
	// MinTargetDivergence keeps rows with target_divergence >=
	// this value. Defaults to 0 (no filter).
	MinTargetDivergence float64
	// MinConceptDivergence keeps rows with concept_divergence >=
	// this value. Defaults to 0 (no filter).
	MinConceptDivergence float64
	// SessionID restricts to retros for this session. Empty = no
	// filter.
	SessionID string
	// Limit caps the result count. <=0 means no cap; List
	// applies a hard ceiling of 1000 to keep the unbounded path
	// from accidentally pulling the whole table.
	Limit int
}

// List returns rows matching f, newest first. Suitable for the
// queryable retrospective view (gm-s47n.8.4): "show me the last
// 50 beads where target divergence > 0.5".
func (s *Store) List(ctx context.Context, f ListFilter) ([]Grade, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	const hardCap = 1000
	limit := f.Limit
	if limit <= 0 || limit > hardCap {
		limit = hardCap
	}

	q := "SELECT " + gradeSelectColumns + " FROM scorer_grades WHERE 1=1"
	args := []any{}
	if f.MinTargetDivergence > 0 {
		q += " AND target_divergence >= ?"
		args = append(args, f.MinTargetDivergence)
	}
	if f.MinConceptDivergence > 0 {
		q += " AND concept_divergence >= ?"
		args = append(args, f.MinConceptDivergence)
	}
	if f.SessionID != "" {
		q += " AND session_id = ?"
		args = append(args, f.SessionID)
	}
	q += " ORDER BY closed_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("retro.Store.List: %w", err)
	}
	defer rows.Close()

	var out []Grade
	for rows.Next() {
		g, err := scanGrade(rows)
		if err != nil {
			return nil, err
		}
		if g != nil {
			out = append(out, *g)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retro.Store.List: rows: %w", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGrade(row rowScanner) (*Grade, error) {
	var (
		g            Grade
		beadID       string
		agentID      string
		declTargets  sql.NullString
		declConcepts sql.NullString
		actFiles     sql.NullString
		actConcepts  sql.NullString
		diffJSON     sql.NullString
		closedAt     time.Time
		createdAt    time.Time
		targetDiv    float64
		conceptDiv   float64
	)
	err := row.Scan(
		&beadID, &closedAt,
		&g.SessionID, &agentID,
		&declTargets, &declConcepts,
		&actFiles, &actConcepts,
		&targetDiv, &conceptDiv,
		&diffJSON,
		&createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("retro.Store: scan: %w", err)
	}
	g.BeadID = core.WorkItemID(beadID)
	g.AgentID = core.AgentID(agentID)
	g.ClosedAt = closedAt
	g.CreatedAt = createdAt

	if declTargets.Valid && declTargets.String != "" {
		var arr []targets.Pattern
		if err := json.Unmarshal([]byte(declTargets.String), &arr); err != nil {
			return nil, fmt.Errorf("decode declared.targets for %s: %w", beadID, err)
		}
		g.Declared.Targets = arr
	}
	if declConcepts.Valid && declConcepts.String != "" {
		var arr []planner.ConceptTag
		if err := json.Unmarshal([]byte(declConcepts.String), &arr); err != nil {
			return nil, fmt.Errorf("decode declared.concepts for %s: %w", beadID, err)
		}
		g.Declared.Concepts = arr
	}
	if actFiles.Valid && actFiles.String != "" {
		var arr []string
		if err := json.Unmarshal([]byte(actFiles.String), &arr); err != nil {
			return nil, fmt.Errorf("decode actual.files for %s: %w", beadID, err)
		}
		g.Actual.Files = arr
	}
	if actConcepts.Valid && actConcepts.String != "" {
		var arr []planner.ConceptTag
		if err := json.Unmarshal([]byte(actConcepts.String), &arr); err != nil {
			return nil, fmt.Errorf("decode actual.concepts for %s: %w", beadID, err)
		}
		g.Actual.Concepts = arr
	}
	if diffJSON.Valid && diffJSON.String != "" {
		if err := json.Unmarshal([]byte(diffJSON.String), &g.Diff); err != nil {
			return nil, fmt.Errorf("decode diff for %s: %w", beadID, err)
		}
	} else {
		// Recompute the divergence numbers from the columns so a
		// row written via SQL by hand still surfaces the metrics.
		g.Diff.TargetDivergence = targetDiv
		g.Diff.ConceptDivergence = conceptDiv
	}
	return &g, nil
}

// EnsureSchema applies the embedded planner.SchemaSQL DDL —
// idempotent CREATE TABLE IF NOT EXISTS for both session_profiles
// and scorer_grades. Safe to call multiple times.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("retro.Store.EnsureSchema: nil store")
	}
	_, err := s.db.ExecContext(ctx, planner.SchemaSQL)
	if err != nil {
		return fmt.Errorf("retro.Store.EnsureSchema: %w", err)
	}
	return nil
}

// jsonOrNil mirrors planner.jsonOrNil but operates on slice types
// the retro store needs. Returns NULL for empty inputs so the row
// stays compact.
func jsonOrNil(v any) (sql.NullString, error) {
	switch m := v.(type) {
	case []targets.Pattern:
		if len(m) == 0 {
			return sql.NullString{}, nil
		}
		bs, err := json.Marshal(m)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	case []planner.ConceptTag:
		if len(m) == 0 {
			return sql.NullString{}, nil
		}
		bs, err := json.Marshal(m)
		if err != nil {
			return sql.NullString{}, err
		}
		return sql.NullString{String: string(bs), Valid: true}, nil
	case []string:
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
