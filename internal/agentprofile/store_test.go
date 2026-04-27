// Store tests for agent_profiles (gm-v5z2.2). sqlmock pins the
// SQL boundary; the comparator + AgeByDays live in types_test.go.

package agentprofile

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

const fixedNow = "2026-04-26T20:00:00Z"

func mustParseFixed(t *testing.T) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, fixedNow)
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

// expectGetMissing primes the mock to return sql.ErrNoRows for the
// initial profile read inside RecordCompletion — i.e. "no prior
// profile."
func expectGetMissing(mock sqlmock.Sqlmock, agentID string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(agentID).
		WillReturnError(sql.ErrNoRows)
}

// expectGetProfile primes the mock to return one row with the given
// fields. Used by the "prior exists" code paths.
func expectGetProfile(mock sqlmock.Sqlmock, p AgentProfile) {
	conceptsJSON, _ := jsonOrNil(p.Concepts)
	filesJSON, _ := jsonOrNil(p.Files)
	rows := sqlmock.NewRows([]string{
		"agent_id", "concepts", "files",
		"lifetime_bead_count", "last_activity_at",
		"created_at", "updated_at",
	}).AddRow(
		string(p.AgentID), nullStringValue(conceptsJSON), nullStringValue(filesJSON),
		p.LifetimeBeadCount, p.LastActivityAt,
		p.CreatedAt, p.UpdatedAt,
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(string(p.AgentID)).
		WillReturnRows(rows)
}

// nullStringValue maps sql.NullString to the value sqlmock expects
// for the matching ColumnDef. Valid → string; !Valid → nil so the
// row is rendered as NULL.
func nullStringValue(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

// ── Get / List ──────────────────────────────────────────────────

func TestGet_AbsentRowReturnsNilNil(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	expectGetMissing(mock, "agent-x")
	got, err := store.Get(context.Background(), "agent-x")
	if err != nil || got != nil {
		t.Errorf("absent row: got (%+v, %v); want (nil, nil)", got, err)
	}
}

func TestGet_DecodesRow(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	expectGetProfile(mock, AgentProfile{
		AgentID:           "mike4",
		Concepts:          map[planner.ConceptTag]float64{"auth": 0.8},
		Files:             map[string]float64{"src/auth.go": 0.5},
		LifetimeBeadCount: 12,
		LastActivityAt:    now,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	got, err := store.Get(context.Background(), "mike4")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.AgentID != "mike4" {
		t.Fatalf("decoded shape: %+v", got)
	}
	if got.Concepts["auth"] != 0.8 || got.Files["src/auth.go"] != 0.5 {
		t.Errorf("weights not threaded: %+v", got)
	}
	if got.LifetimeBeadCount != 12 {
		t.Errorf("lifetime count: %d", got.LifetimeBeadCount)
	}
}

func TestList_OrdersByAgentID(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	rows := sqlmock.NewRows([]string{
		"agent_id", "concepts", "files",
		"lifetime_bead_count", "last_activity_at",
		"created_at", "updated_at",
	}).
		AddRow("mike2", nil, nil, int64(3), now, now, now).
		AddRow("mike4", nil, nil, int64(7), now, now, now)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).WillReturnRows(rows)

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].AgentID != "mike2" || got[1].AgentID != "mike4" {
		t.Errorf("List ordering: %+v", got)
	}
}

// ── RecordCompletion ────────────────────────────────────────────

func TestRecordCompletion_FreshProfile_MergesAtFullWeight(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	expectGetMissing(mock, "mike-fresh")
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO agent_profiles")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := store.RecordCompletion(context.Background(), CompletionEvent{
		AgentID:  "mike-fresh",
		Concepts: []planner.ConceptTag{"auth"},
		Files:    []string{"src/auth.go"},
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RecordCompletion: %v", err)
	}
	// First-bead scale is 1/(0+1) = 1.0 — full weight, by design.
	if math.Abs(got.Concepts["auth"]-1.0) > 1e-9 {
		t.Errorf("first-bead scale should be 1.0; got %f", got.Concepts["auth"])
	}
	if math.Abs(got.Files["src/auth.go"]-1.0) > 1e-9 {
		t.Errorf("first-bead file scale should be 1.0; got %f", got.Files["src/auth.go"])
	}
	if got.LifetimeBeadCount != 1 {
		t.Errorf("lifetime count after first bead: %d", got.LifetimeBeadCount)
	}
}

func TestRecordCompletion_PriorAged(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)
	weekAgo := now.Add(-14 * 24 * time.Hour)

	expectGetProfile(mock, AgentProfile{
		AgentID:           "mike4",
		Concepts:          map[planner.ConceptTag]float64{"auth": 1.0},
		LifetimeBeadCount: 9, // next bead is the 10th → scale 1/10 = 0.1
		LastActivityAt:    weekAgo,
		CreatedAt:         weekAgo,
		UpdatedAt:         weekAgo,
	})
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO agent_profiles")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := store.RecordCompletion(context.Background(), CompletionEvent{
		AgentID:  "mike4",
		Concepts: []planner.ConceptTag{"auth", "session"},
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RecordCompletion: %v", err)
	}
	// 14 days = one half-life → prior auth (1.0) ages to 0.5,
	// then merges with 0.1 contribution → 0.6.
	if math.Abs(got.Concepts["auth"]-0.6) > 1e-9 {
		t.Errorf("aged+merged auth: got %f want 0.6", got.Concepts["auth"])
	}
	// Fresh tag landed at scale 1/10 = 0.1.
	if math.Abs(got.Concepts["session"]-0.1) > 1e-9 {
		t.Errorf("new tag should land at scale 0.1; got %f", got.Concepts["session"])
	}
	if got.LifetimeBeadCount != 10 {
		t.Errorf("lifetime count: got %d want 10", got.LifetimeBeadCount)
	}
	if !got.LastActivityAt.Equal(now) {
		t.Errorf("last_activity_at not stamped to now: %v", got.LastActivityAt)
	}
}

func TestRecordCompletion_ContributionScaleOverride(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)
	expectGetMissing(mock, "mike-test")
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO agent_profiles")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := store.RecordCompletion(context.Background(), CompletionEvent{
		AgentID:           "mike-test",
		Concepts:          []planner.ConceptTag{"x"},
		ContributionScale: 0.25,
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RecordCompletion: %v", err)
	}
	if math.Abs(got.Concepts["x"]-0.25) > 1e-9 {
		t.Errorf("explicit scale ignored; got %f", got.Concepts["x"])
	}
}

func TestRecordCompletion_RejectsEmptyAgentID(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	if _, err := store.RecordCompletion(context.Background(), CompletionEvent{}); err == nil {
		t.Fatal("empty AgentID must error")
	}
}

func TestRecordCompletion_NilStoreErrors(t *testing.T) {
	var store *Store
	if _, err := store.RecordCompletion(context.Background(), CompletionEvent{AgentID: "x"}); err == nil {
		t.Fatal("nil store must error")
	}
}

// ── Upsert ──────────────────────────────────────────────────────

func TestUpsert_HappyPath(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO agent_profiles")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.Upsert(context.Background(), &AgentProfile{
		AgentID:           "mike4",
		Concepts:          map[planner.ConceptTag]float64{"auth": 0.8},
		LifetimeBeadCount: 5,
		LastActivityAt:    now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestUpsert_RejectsEmptyAgentID(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	if err := store.Upsert(context.Background(), &AgentProfile{}); err == nil {
		t.Fatal("empty AgentID must error")
	}
}

func TestUpsert_PropagatesDBError(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO agent_profiles")).
		WillReturnError(errors.New("connection refused"))
	err := store.Upsert(context.Background(), &AgentProfile{
		AgentID: core.AgentID("mike4"),
	})
	if err == nil {
		t.Fatal("expected DB error to propagate")
	}
}

// ── EnsureSchema / nil ──────────────────────────────────────────

func TestEnsureSchema_RunsDDL(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestNilStore_GetReturnsNilNil(t *testing.T) {
	var store *Store
	got, err := store.Get(context.Background(), "x")
	if got != nil || err != nil {
		t.Errorf("nil store Get must be (nil, nil); got (%+v, %v)", got, err)
	}
}

func TestNilStore_ListReturnsNilNil(t *testing.T) {
	var store *Store
	got, err := store.List(context.Background())
	if got != nil || err != nil {
		t.Errorf("nil store List must be (nil, nil); got (%+v, %v)", got, err)
	}
}

func TestNilStore_UpsertErrors(t *testing.T) {
	var store *Store
	if err := store.Upsert(context.Background(), &AgentProfile{AgentID: "x"}); err == nil {
		t.Fatal("nil store Upsert must error")
	}
}
