package planner

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

const fixedTime = "2026-04-26T15:00:00Z"

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func newMockStore(t *testing.T) (*ProfileStore, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	return NewProfileStore(db), mock, db
}

func TestProfileStore_NilStoreSafe(t *testing.T) {
	if NewProfileStore(nil) != nil {
		t.Errorf("NewProfileStore(nil) should return nil")
	}
	var s *ProfileStore
	got, err := s.GetProfile(context.Background(), "any")
	if err != nil || got != nil {
		t.Errorf("nil store should return (nil, nil); got %v, %v", got, err)
	}
}

func TestProfileStore_GetProfile_HappyPath(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	now := mustParseTime(t, fixedTime)
	rows := sqlmock.NewRows([]string{
		"session_id", "assignment_id", "agent_id",
		"concepts", "files",
		"tokens_used", "context_window_max", "context_pct",
		"last_beads",
		"last_activity_at", "created_at", "updated_at",
	}).AddRow(
		"sess-1", "asg-1", "alice",
		`{"auth":0.9,"spa-routing":0.4}`,
		`{"src/auth.go":0.6}`,
		12000, 200000, 0.06,
		`["gm-1","gm-2"]`,
		now, now, now,
	)
	mock.ExpectQuery("SELECT .+ FROM session_profiles WHERE session_id = ?").
		WithArgs("sess-1").
		WillReturnRows(rows)

	got, err := s.GetProfile(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil profile")
	}
	if got.SessionID != "sess-1" {
		t.Errorf("session_id = %q", got.SessionID)
	}
	if got.Concepts["auth"] != 0.9 {
		t.Errorf("concepts[auth] = %v", got.Concepts["auth"])
	}
	if got.Files["src/auth.go"] != 0.6 {
		t.Errorf("files = %+v", got.Files)
	}
	if len(got.LastBeads) != 2 || got.LastBeads[1] != "gm-2" {
		t.Errorf("last_beads = %+v", got.LastBeads)
	}
	if got.ContextPct != 0.06 {
		t.Errorf("context_pct = %v", got.ContextPct)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("updated_at = %v", got.UpdatedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestProfileStore_GetProfile_NotFoundReturnsNilNil(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectQuery("SELECT .+ FROM session_profiles WHERE session_id = ?").
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	got, err := s.GetProfile(context.Background(), "missing")
	if err != nil {
		t.Errorf("expected nil error for not-found; got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil profile for not-found; got %+v", got)
	}
}

func TestProfileStore_GetProfile_NullJSONColumnsTolerated(t *testing.T) {
	// A row with NULLs in concepts/files/last_beads should decode
	// to a profile with empty (nil) maps/slices — not error.
	s, mock, db := newMockStore(t)
	defer db.Close()

	now := mustParseTime(t, fixedTime)
	rows := sqlmock.NewRows([]string{
		"session_id", "assignment_id", "agent_id",
		"concepts", "files",
		"tokens_used", "context_window_max", "context_pct",
		"last_beads",
		"last_activity_at", "created_at", "updated_at",
	}).AddRow(
		"sess-empty", "asg-empty", "alice",
		nil, nil, 0, 0, 0.0, nil,
		now, now, now,
	)
	mock.ExpectQuery("SELECT .+ FROM session_profiles WHERE session_id = ?").
		WithArgs("sess-empty").
		WillReturnRows(rows)

	got, err := s.GetProfile(context.Background(), "sess-empty")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil profile")
	}
	if len(got.Concepts) != 0 || len(got.Files) != 0 || len(got.LastBeads) != 0 {
		t.Errorf("expected empty maps/slices; got %+v", got)
	}
}

func TestProfileStore_GetProfile_MalformedJSONErrors(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	now := mustParseTime(t, fixedTime)
	rows := sqlmock.NewRows([]string{
		"session_id", "assignment_id", "agent_id",
		"concepts", "files",
		"tokens_used", "context_window_max", "context_pct",
		"last_beads",
		"last_activity_at", "created_at", "updated_at",
	}).AddRow(
		"sess-bad", "asg-bad", "alice",
		"not json", `{"src/auth.go":0.6}`, 0, 0, 0.0, "[]",
		now, now, now,
	)
	mock.ExpectQuery("SELECT .+ FROM session_profiles WHERE session_id = ?").
		WithArgs("sess-bad").
		WillReturnRows(rows)

	_, err := s.GetProfile(context.Background(), "sess-bad")
	if err == nil {
		t.Fatal("expected error for malformed concepts JSON")
	}
}

func TestProfileStore_GetProfile_EmptyIDRejected(t *testing.T) {
	s, _, db := newMockStore(t)
	defer db.Close()

	_, err := s.GetProfile(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty sessionID")
	}
}

func TestProfileStore_MultiGet_PreservesOrder(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	now := mustParseTime(t, fixedTime)
	cols := []string{
		"session_id", "assignment_id", "agent_id",
		"concepts", "files",
		"tokens_used", "context_window_max", "context_pct",
		"last_beads",
		"last_activity_at", "created_at", "updated_at",
	}
	// Return rows out of input order — MultiGet must re-order.
	rows := sqlmock.NewRows(cols).
		AddRow("sess-c", "asg-c", "alice", nil, nil, 0, 0, 0.0, nil, now, now, now).
		AddRow("sess-a", "asg-a", "alice", nil, nil, 0, 0, 0.0, nil, now, now, now)
	mock.ExpectQuery("SELECT .+ FROM session_profiles WHERE session_id IN").
		WithArgs("sess-a", "sess-b", "sess-c").
		WillReturnRows(rows)

	got, err := s.MultiGet(context.Background(), []string{"sess-a", "sess-b", "sess-c"})
	if err != nil {
		t.Fatalf("MultiGet: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (one per input id), got %d", len(got))
	}
	if got[0] == nil || got[0].SessionID != "sess-a" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1] != nil {
		t.Errorf("got[1] (sess-b not in result) should be nil; got %+v", got[1])
	}
	if got[2] == nil || got[2].SessionID != "sess-c" {
		t.Errorf("got[2] = %+v", got[2])
	}
}

func TestProfileStore_EnsureSchema_RunsSchemaSQL(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS session_profiles").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestProfileStore_EnsureSchema_NilStoreErrors(t *testing.T) {
	var s *ProfileStore
	if err := s.EnsureSchema(context.Background()); err == nil {
		t.Fatal("expected error from nil store")
	}
}

// Compile-time check for the ProfileLookup contract — same as in
// profile_store.go but defended in tests too so a refactor that
// drops the assertion is caught.
var _ = func() {
	var _ ProfileLookup = (*ProfileStore)(nil)
	_ = errors.Is
}
