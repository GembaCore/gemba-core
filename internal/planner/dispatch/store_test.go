// dispatch_decisions store tests (gm-s47n.6.2).
//
// Mirrors retro/store_test.go: sqlmock-pinned INSERT/SELECT shapes
// so a benign schema migration is loud rather than silent.

package dispatch

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

const fixedDispatchTime = "2026-04-26T16:30:00Z"

func mustParseDispatchTime(t *testing.T) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, fixedDispatchTime)
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

func sampleDecision(t *testing.T) Decision {
	t.Helper()
	decided := mustParseDispatchTime(t)
	return Decision{
		ID:        "d-1",
		BeadID:    "gm-1",
		DecidedAt: decided,
		SessionID: "sess-1",
		AgentID:   "mike2",
		DecidedBy: "operator-mike",
		Mode:      ModeCoach,
		Affinity: planner.AffinityScores{
			ConceptOverlap:  0.8,
			FileFamiliarity: 0.6,
			WorkspaceMatch:  1.0,
			Recency:         0.4,
			Headroom:        0.7,
			Combined:        0.7,
		},
		Conflicts: ConflictSnapshot{
			Workspace: []planner.WorkspaceCollision{
				{A: "gm-1", B: "gm-2", Reason: "same repo+branch"},
			},
		},
		ReadySet: []ReadySetEntry{
			{BeadID: "gm-1", AffinityCombined: 0.7},
			{BeadID: "gm-2", AffinityCombined: 0.5, WorkspaceConflict: true},
		},
		CreatedAt: decided,
	}
}

// ── Insert ───────────────────────────────────────────────────────

func TestInsert_HappyPath(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	d := sampleDecision(t)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO dispatch_decisions")).
		WithArgs(
			d.ID, string(d.BeadID), d.DecidedAt,
			d.SessionID, string(d.AgentID), d.DecidedBy, string(d.Mode),
			d.Affinity.Combined, sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			d.OperatorReason,
			d.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	id, err := store.Insert(context.Background(), d)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id != "d-1" {
		t.Errorf("returned id = %q, want d-1", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestInsert_GeneratesIDWhenBlank(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	d := sampleDecision(t)
	d.ID = ""

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO dispatch_decisions")).
		WithArgs(
			sqlmock.AnyArg(), string(d.BeadID), d.DecidedAt,
			d.SessionID, string(d.AgentID), d.DecidedBy, string(d.Mode),
			d.Affinity.Combined, sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			d.OperatorReason,
			d.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	id, err := store.Insert(context.Background(), d)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(id) != 32 {
		t.Errorf("generated id length = %d (%q), want 32 hex chars", len(id), id)
	}
}

func TestInsert_DefaultsModeToCoachWhenBlank(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	d := sampleDecision(t)
	d.Mode = ""

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO dispatch_decisions")).
		WithArgs(
			d.ID, string(d.BeadID), d.DecidedAt,
			d.SessionID, string(d.AgentID), d.DecidedBy, string(ModeCoach),
			d.Affinity.Combined, sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			d.OperatorReason,
			d.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if _, err := store.Insert(context.Background(), d); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestInsert_RejectsEmptyBeadID(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	d := sampleDecision(t)
	d.BeadID = ""
	if _, err := store.Insert(context.Background(), d); err == nil {
		t.Fatal("expected error for empty BeadID")
	}
}

func TestInsert_RejectsZeroDecidedAt(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	d := sampleDecision(t)
	d.DecidedAt = time.Time{}
	if _, err := store.Insert(context.Background(), d); err == nil {
		t.Fatal("expected error for zero DecidedAt")
	}
}

func TestInsert_NilStoreFailsLoudly(t *testing.T) {
	var store *Store
	if _, err := store.Insert(context.Background(), sampleDecision(t)); err == nil {
		t.Fatal("nil store must error rather than panic")
	}
}

// ── Get ──────────────────────────────────────────────────────────

func TestGet_ReturnsNilForMissingRow(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("d-missing").
		WillReturnError(sql.ErrNoRows)

	got, err := store.Get(context.Background(), "d-missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing row; got %+v", got)
	}
}

func TestGet_RejectsEmptyID(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	if _, err := store.Get(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestGet_DecodesRowRoundTrip(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	decided := mustParseDispatchTime(t)
	affinityJSON := `{"concept_overlap":0.8,"file_familiarity":0.6,"workspace_match":1,"recency":0.4,"headroom":0.7,"combined":0.7}`
	conflictsJSON := `{"workspace":[{"a":"gm-1","b":"gm-2","reason":"same repo+branch"}]}`
	readySetJSON := `[{"bead_id":"gm-1","affinity_combined":0.7},{"bead_id":"gm-2","affinity_combined":0.5,"workspace_conflict":true}]`

	rows := sqlmock.NewRows([]string{
		"id", "bead_id", "decided_at",
		"session_id", "agent_id", "decided_by", "mode",
		"affinity_combined", "affinity_json",
		"conflicts_json", "ready_set_json",
		"operator_reason",
		"created_at",
	}).AddRow(
		"d-1", "gm-1", decided,
		"sess-1", "mike2", "operator-mike", "coach",
		0.7, affinityJSON,
		conflictsJSON, readySetJSON,
		"more important than the top pick",
		decided,
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("d-1").
		WillReturnRows(rows)

	got, err := store.Get(context.Background(), "d-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil decision")
	}
	if got.ID != "d-1" || got.BeadID != "gm-1" {
		t.Errorf("identity columns: %+v", got)
	}
	if got.SessionID != "sess-1" || got.AgentID != "mike2" {
		t.Errorf("session/agent: %+v", got)
	}
	if got.Mode != ModeCoach {
		t.Errorf("mode = %q, want coach", got.Mode)
	}
	if got.Affinity.Combined != 0.7 || got.Affinity.ConceptOverlap != 0.8 {
		t.Errorf("affinity decode: %+v", got.Affinity)
	}
	if len(got.Conflicts.Workspace) != 1 || got.Conflicts.Workspace[0].A != "gm-1" {
		t.Errorf("conflicts decode: %+v", got.Conflicts)
	}
	if len(got.ReadySet) != 2 || !got.ReadySet[1].WorkspaceConflict {
		t.Errorf("ready_set decode: %+v", got.ReadySet)
	}
}

// ── List ─────────────────────────────────────────────────────────

func TestList_FiltersByBead(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	decided := mustParseDispatchTime(t)

	rows := sqlmock.NewRows([]string{
		"id", "bead_id", "decided_at",
		"session_id", "agent_id", "decided_by", "mode",
		"affinity_combined", "affinity_json",
		"conflicts_json", "ready_set_json",
		"operator_reason",
		"created_at",
	}).AddRow(
		"d-1", "gm-1", decided,
		"sess-1", "mike2", "", "coach",
		0.7, "", "", "",
		"",
		decided,
	)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE 1=1 AND bead_id = ?")).
		WithArgs("gm-1", 1000).
		WillReturnRows(rows)

	got, err := store.List(context.Background(), ListFilter{
		BeadID: core.WorkItemID("gm-1"),
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "d-1" {
		t.Errorf("expected 1 row d-1; got %+v", got)
	}
}

func TestList_FiltersBySession(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("AND session_id = ?")).
		WithArgs("sess-1", 1000).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "bead_id", "decided_at",
			"session_id", "agent_id", "decided_by", "mode",
			"affinity_combined", "affinity_json",
			"conflicts_json", "ready_set_json",
			"operator_reason",
			"created_at",
		}))

	got, err := store.List(context.Background(), ListFilter{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice; got %+v", got)
	}
}

func TestList_FiltersByMode(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("AND mode = ?")).
		WithArgs("auto", 1000).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "bead_id", "decided_at",
			"session_id", "agent_id", "decided_by", "mode",
			"affinity_combined", "affinity_json",
			"conflicts_json", "ready_set_json",
			"operator_reason",
			"created_at",
		}))

	if _, err := store.List(context.Background(), ListFilter{Mode: ModeAuto}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestList_AppliesAffinityThreshold(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("AND affinity_combined >= ?")).
		WithArgs(0.5, 1000).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "bead_id", "decided_at",
			"session_id", "agent_id", "decided_by", "mode",
			"affinity_combined", "affinity_json",
			"conflicts_json", "ready_set_json",
			"operator_reason",
			"created_at",
		}))

	_, err := store.List(context.Background(), ListFilter{MinAffinityCombined: 0.5})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestList_RespectsLimit(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("LIMIT ?")).
		WithArgs(5).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "bead_id", "decided_at",
			"session_id", "agent_id", "decided_by", "mode",
			"affinity_combined", "affinity_json",
			"conflicts_json", "ready_set_json",
			"operator_reason",
			"created_at",
		}))

	_, err := store.List(context.Background(), ListFilter{Limit: 5})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestList_OrdersNewestFirst(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY decided_at DESC")).
		WithArgs(1000).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "bead_id", "decided_at",
			"session_id", "agent_id", "decided_by", "mode",
			"affinity_combined", "affinity_json",
			"conflicts_json", "ready_set_json",
			"operator_reason",
			"created_at",
		}))

	if _, err := store.List(context.Background(), ListFilter{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

// ── EnsureSchema ────────────────────────────────────────────────

func TestEnsureSchema_RunsEmbeddedDDL(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestEnsureSchema_ContainsDispatchTable(t *testing.T) {
	if !strings.Contains(planner.SchemaSQL, "dispatch_decisions") {
		t.Error("schema must contain dispatch_decisions DDL")
	}
}
