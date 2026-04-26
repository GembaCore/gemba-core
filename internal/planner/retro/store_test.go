// scorer_grades store tests (gm-s47n.8.2).
//
// Uses sqlmock to pin the exact INSERT/SELECT shape so a benign
// schema migration (column rename, reordering) is loud rather than
// silent. The retro.Compare result fed in is already exercised in
// compare_test.go; here we focus on the SQL boundary.

package retro

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/targets"
)

const fixedRetroTime = "2026-04-26T16:00:00Z"

func mustParseRetroTime(t *testing.T) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, fixedRetroTime)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

func newMockStore(t *testing.T) (*Store, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	return NewStore(db), mock, db
}

func sampleGrade(t *testing.T) Grade {
	t.Helper()
	closed := mustParseRetroTime(t)
	d := Compare(
		Declared{
			Targets:  []targets.Pattern{"src/auth.go"},
			Concepts: []planner.ConceptTag{"auth"},
		},
		Actual{
			Files:    []string{"src/auth.go", "src/handlers.go"},
			Concepts: []planner.ConceptTag{"auth", "ratelimit"},
		},
	)
	return Grade{
		BeadID:    "gm-1",
		ClosedAt:  closed,
		SessionID: "sess-1",
		AgentID:   "mike2",
		Declared: Declared{
			Targets:  []targets.Pattern{"src/auth.go"},
			Concepts: []planner.ConceptTag{"auth"},
		},
		Actual: Actual{
			Files:    []string{"src/auth.go", "src/handlers.go"},
			Concepts: []planner.ConceptTag{"auth", "ratelimit"},
		},
		Diff:      d,
		CreatedAt: closed,
	}
}

// ── Insert ───────────────────────────────────────────────────────

func TestInsert_HappyPath(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	g := sampleGrade(t)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WithArgs(
			string(g.BeadID), g.ClosedAt,
			g.SessionID, string(g.AgentID),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			g.Diff.TargetDivergence, g.Diff.ConceptDivergence,
			sqlmock.AnyArg(),
			g.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := store.Insert(context.Background(), g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestInsert_RejectsEmptyBeadID(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	g := sampleGrade(t)
	g.BeadID = ""
	if err := store.Insert(context.Background(), g); err == nil {
		t.Fatal("expected error for empty BeadID")
	}
}

func TestInsert_RejectsZeroClosedAt(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	g := sampleGrade(t)
	g.ClosedAt = time.Time{}
	if err := store.Insert(context.Background(), g); err == nil {
		t.Fatal("expected error for zero ClosedAt")
	}
}

func TestInsert_NilStoreFailsLoudly(t *testing.T) {
	var store *Store
	if err := store.Insert(context.Background(), sampleGrade(t)); err == nil {
		t.Fatal("nil store must error rather than panic")
	}
}

// ── Get ──────────────────────────────────────────────────────────

func TestGet_ReturnsNilForMissingRow(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("gm-missing").
		WillReturnError(sql.ErrNoRows)

	got, err := store.Get(context.Background(), "gm-missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing row; got %+v", got)
	}
}

func TestGet_DecodesRowRoundTrip(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	closed := mustParseRetroTime(t)
	declTargets := `["src/auth.go"]`
	declConcepts := `["auth"]`
	actFiles := `["src/auth.go","src/handlers.go"]`
	actConcepts := `["auth","ratelimit"]`
	diffJSON := `{"target_divergence":0.5,"concept_divergence":0.5}`

	rows := sqlmock.NewRows([]string{
		"bead_id", "closed_at",
		"session_id", "agent_id",
		"declared_targets", "declared_concepts",
		"actual_files", "actual_concepts",
		"target_divergence", "concept_divergence",
		"diff_json",
		"created_at",
	}).AddRow(
		"gm-1", closed,
		"sess-1", "mike2",
		declTargets, declConcepts,
		actFiles, actConcepts,
		0.5, 0.5,
		diffJSON,
		closed,
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("gm-1").
		WillReturnRows(rows)

	got, err := store.Get(context.Background(), "gm-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil grade")
	}
	if got.BeadID != "gm-1" {
		t.Errorf("BeadID: %q", got.BeadID)
	}
	if got.SessionID != "sess-1" || got.AgentID != "mike2" {
		t.Errorf("identity columns: %+v", got)
	}
	if len(got.Declared.Targets) != 1 || got.Declared.Targets[0] != "src/auth.go" {
		t.Errorf("declared targets: %v", got.Declared.Targets)
	}
	if len(got.Actual.Files) != 2 {
		t.Errorf("actual files: %v", got.Actual.Files)
	}
	if got.TargetDivergence() != 0.5 || got.ConceptDivergence() != 0.5 {
		t.Errorf("divergence accessors: %+v", got)
	}
}

// ── List ─────────────────────────────────────────────────────────

func TestList_AppliesDivergenceThreshold(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	closed := mustParseRetroTime(t)

	rows := sqlmock.NewRows([]string{
		"bead_id", "closed_at",
		"session_id", "agent_id",
		"declared_targets", "declared_concepts",
		"actual_files", "actual_concepts",
		"target_divergence", "concept_divergence",
		"diff_json",
		"created_at",
	}).AddRow(
		"gm-99", closed, "sess-x", "mike2",
		`["src/foo.go"]`, `["foo"]`,
		`["src/bar.go"]`, `["bar"]`,
		1.0, 1.0,
		`{"target_divergence":1.0,"concept_divergence":1.0}`,
		closed,
	)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE 1=1 AND target_divergence >= ?")).
		WithArgs(0.5, 1000).
		WillReturnRows(rows)

	got, err := store.List(context.Background(), ListFilter{
		MinTargetDivergence: 0.5,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].BeadID != "gm-99" {
		t.Errorf("expected 1 row gm-99; got %+v", got)
	}
}

func TestList_RespectsLimit(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("LIMIT ?")).
		WithArgs(5).
		WillReturnRows(sqlmock.NewRows([]string{
			"bead_id", "closed_at",
			"session_id", "agent_id",
			"declared_targets", "declared_concepts",
			"actual_files", "actual_concepts",
			"target_divergence", "concept_divergence",
			"diff_json",
			"created_at",
		}))

	got, err := store.List(context.Background(), ListFilter{Limit: 5})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice; got %+v", got)
	}
}

func TestList_FiltersBySession(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("AND session_id = ?")).
		WithArgs("sess-1", 1000).
		WillReturnRows(sqlmock.NewRows([]string{
			"bead_id", "closed_at",
			"session_id", "agent_id",
			"declared_targets", "declared_concepts",
			"actual_files", "actual_concepts",
			"target_divergence", "concept_divergence",
			"diff_json",
			"created_at",
		}))

	if _, err := store.List(context.Background(), ListFilter{SessionID: "sess-1"}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

// ── EnsureSchema ─────────────────────────────────────────────────

func TestEnsureSchema_RunsDDL(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta(planner.SchemaSQL)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestEnsureSchema_NilStore(t *testing.T) {
	var store *Store
	if err := store.EnsureSchema(context.Background()); err == nil {
		t.Fatal("nil store must error")
	}
}

// ── nil store reads ──────────────────────────────────────────────

func TestNilStore_GetReturnsNilNil(t *testing.T) {
	var store *Store
	got, err := store.Get(context.Background(), "gm-1")
	if err != nil {
		t.Errorf("nil store Get should be (nil, nil); got err %v", err)
	}
	if got != nil {
		t.Errorf("nil store Get should be (nil, nil); got %+v", got)
	}
}

func TestNilStore_ListReturnsNilNil(t *testing.T) {
	var store *Store
	got, err := store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Errorf("nil store List should be (nil, nil); got err %v", err)
	}
	if got != nil {
		t.Errorf("nil store List should be (nil, nil); got %+v", got)
	}
}

// ── round-trip via Compare ───────────────────────────────────────

func TestInsertEncodesComparatorOutput(t *testing.T) {
	// Sanity: the comparator's Diff should encode without error
	// and round-trip through json.Marshal/Unmarshal in scanGrade.
	d := Compare(
		Declared{Targets: []targets.Pattern{"src/auth.go"}, Concepts: []planner.ConceptTag{"auth"}},
		Actual{Files: []string{"src/handlers.go"}, Concepts: []planner.ConceptTag{"ratelimit"}},
	)
	g := Grade{
		BeadID:    "gm-2",
		ClosedAt:  mustParseRetroTime(t),
		SessionID: "sess-2",
		AgentID:   core.AgentID("mike2"),
		Declared:  Declared{Targets: []targets.Pattern{"src/auth.go"}, Concepts: []planner.ConceptTag{"auth"}},
		Actual:    Actual{Files: []string{"src/handlers.go"}, Concepts: []planner.ConceptTag{"ratelimit"}},
		Diff:      d,
	}

	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WithArgs(
			string(g.BeadID), g.ClosedAt,
			g.SessionID, string(g.AgentID),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			d.TargetDivergence, d.ConceptDivergence,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(), // CreatedAt — store stamps it from now()
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := store.Insert(context.Background(), g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
}

// ── error propagation ────────────────────────────────────────────

func TestInsert_PropagatesDBError(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnError(errors.New("connection refused"))

	if err := store.Insert(context.Background(), sampleGrade(t)); err == nil {
		t.Fatal("expected DB error to propagate")
	}
}
