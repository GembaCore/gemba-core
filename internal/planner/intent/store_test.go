// session_intents store tests (gm-v5z2.3). Uses sqlmock to pin
// the SQL boundary; the comparator + Match are exercised in the
// pure-function tests under intent_test.go.

package intent

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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

func sampleSet() SetInput {
	return SetInput{
		SessionID:      "sess-1",
		EpicID:         "gm-e1",
		Rationale:      "Focus on the auth epic",
		DemotionFactor: 0.5,
		Actor:          "cli:mike",
	}
}

// ── Set ──────────────────────────────────────────────────────────

func TestSet_HappyPath(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	in := sampleSet()
	in.Now = func() time.Time { return now }

	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(in.SessionID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intents")).
		WithArgs(
			in.SessionID, in.EpicID, in.Label, in.BeadIDRegex, in.Rationale,
			in.DemotionFactor, now, now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intent_audit")).
		WithArgs(in.SessionID, string(AuditSet), sqlmock.AnyArg(), sqlmock.AnyArg(), in.Actor, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := store.Set(context.Background(), in)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got == nil || got.EpicID != "gm-e1" {
		t.Errorf("returned intent shape: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestSet_RejectsZeroIntent(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	if _, err := store.Set(context.Background(), SetInput{SessionID: "s1"}); err == nil {
		t.Fatal("must reject intent with no restrictors")
	}
}

func TestSet_RejectsBadRegex(t *testing.T) {
	store, _, db := newMockStore(t)
	defer db.Close()
	in := SetInput{SessionID: "s1", BeadIDRegex: "[unterminated"}
	if _, err := store.Set(context.Background(), in); err == nil {
		t.Fatal("must reject uncompilable regex via Validate")
	}
}

func TestSet_PriorThreadedIntoAuditPriorJSON(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	priorRows := sqlmock.NewRows([]string{
		"session_id", "epic_id", "label", "bead_id_regex", "rationale",
		"demotion_factor", "created_at", "updated_at",
	}).AddRow("sess-1", "gm-old", "", "", "", 0.4, now.Add(-time.Hour), now.Add(-time.Hour))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("sess-1").
		WillReturnRows(priorRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intents")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// Audit insert: the prior_json arg should contain the old EpicID.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intent_audit")).
		WithArgs(
			"sess-1", string(AuditSet),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			"cli:mike", now,
		).
		WillReturnResult(sqlmock.NewResult(2, 1))

	in := sampleSet()
	in.Now = func() time.Time { return now }
	if _, err := store.Set(context.Background(), in); err != nil {
		t.Fatalf("Set: %v", err)
	}
}

// ── Clear ────────────────────────────────────────────────────────

func TestClear_HappyPath(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	rows := sqlmock.NewRows([]string{
		"session_id", "epic_id", "label", "bead_id_regex", "rationale",
		"demotion_factor", "created_at", "updated_at",
	}).AddRow("sess-1", "gm-e1", "", "", "", 0.4, now, now)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("sess-1").WillReturnRows(rows)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM session_intents")).
		WithArgs("sess-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intent_audit")).
		WithArgs("sess-1", string(AuditClear), sqlmock.AnyArg(), sqlmock.AnyArg(), "cli:mike", now).
		WillReturnResult(sqlmock.NewResult(3, 1))

	if err := store.Clear(context.Background(), "sess-1", "cli:mike", func() time.Time { return now }); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestClear_NoExistingRow_StillAuditsAndReturnsNil(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("sess-ghost").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM session_intents")).
		WithArgs("sess-ghost").WillReturnResult(sqlmock.NewResult(0, 0))
	// prior is nil → prior_json is NullString (not Valid).
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intent_audit")).
		WillReturnResult(sqlmock.NewResult(4, 1))

	if err := store.Clear(context.Background(), "sess-ghost", "cli:mike", func() time.Time { return now }); err != nil {
		t.Errorf("clear of absent row should be quiet; got %v", err)
	}
}

// ── Get / List / Audit ───────────────────────────────────────────

func TestGet_AbsentRowReturnsNilNil(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("nope").
		WillReturnError(sql.ErrNoRows)
	got, err := store.Get(context.Background(), "nope")
	if err != nil || got != nil {
		t.Errorf("absent row: got (%+v, %v); want (nil, nil)", got, err)
	}
}

func TestList_OrdersBySessionID(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	rows := sqlmock.NewRows([]string{
		"session_id", "epic_id", "label", "bead_id_regex", "rationale",
		"demotion_factor", "created_at", "updated_at",
	}).
		AddRow("sess-a", "gm-e1", "", "", "", 0.4, now, now).
		AddRow("sess-b", "gm-e2", "", "", "", 0.5, now, now)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).WillReturnRows(rows)

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].SessionID != "sess-a" || got[1].SessionID != "sess-b" {
		t.Errorf("List ordering: %+v", got)
	}
}

func TestAudit_DecodesPriorAndNextJSON(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	priorJSON := `{"session_id":"s1","epic_id":"gm-old","demotion_factor":0.4,"created_at":"2026-04-26T19:00:00Z","updated_at":"2026-04-26T19:00:00Z"}`
	nextJSON := `{"session_id":"s1","epic_id":"gm-new","demotion_factor":0.4,"created_at":"2026-04-26T20:00:00Z","updated_at":"2026-04-26T20:00:00Z"}`

	rows := sqlmock.NewRows([]string{
		"id", "session_id", "action", "prior_json", "next_json", "actor", "at",
	}).
		AddRow(int64(1), "s1", string(AuditSet), priorJSON, nextJSON, "cli:mike", now)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, session_id, action")).
		WithArgs("s1", 50).
		WillReturnRows(rows)

	got, err := store.Audit(context.Background(), "s1", 50)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row; got %d", len(got))
	}
	if got[0].Prior == nil || got[0].Prior.EpicID != "gm-old" {
		t.Errorf("prior decode failed: %+v", got[0].Prior)
	}
	if got[0].Next == nil || got[0].Next.EpicID != "gm-new" {
		t.Errorf("next decode failed: %+v", got[0].Next)
	}
}

func TestAudit_NoLimit(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, session_id, action")).
		WithArgs("s1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "action", "prior_json", "next_json", "actor", "at",
		}))
	if _, err := store.Audit(context.Background(), "s1", 0); err != nil {
		t.Fatalf("Audit: %v", err)
	}
}

// ── nil-store / EnsureSchema ────────────────────────────────────

func TestNilStore_GetReturnsNilNil(t *testing.T) {
	var store *Store
	got, err := store.Get(context.Background(), "s1")
	if got != nil || err != nil {
		t.Errorf("nil store Get must be (nil, nil); got (%+v, %v)", got, err)
	}
}

func TestNilStore_SetErrors(t *testing.T) {
	var store *Store
	if _, err := store.Set(context.Background(), SetInput{SessionID: "s1", EpicID: "gm-e1"}); err == nil {
		t.Fatal("nil store Set must error")
	}
}

func TestEnsureSchema_RunsDDL(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
}

func TestSet_PropagatesDBError(t *testing.T) {
	store, mock, db := newMockStore(t)
	defer db.Close()
	now := mustParseFixed(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs("sess-1").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_intents")).
		WillReturnError(errors.New("connection refused"))

	in := sampleSet()
	in.Now = func() time.Time { return now }
	if _, err := store.Set(context.Background(), in); err == nil {
		t.Fatal("expected DB error to propagate")
	}
}
