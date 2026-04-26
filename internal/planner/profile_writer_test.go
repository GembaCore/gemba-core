package planner

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
)

const writeFixedTime = "2026-04-26T16:00:00Z"

func mustParseWriteTime(t *testing.T) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, writeFixedTime)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

// writerNow returns a closure for ClaimEvent.Now /
// CompletionEvent.Now. Distinct from operational_context_test.go's
// fixedNow (zero-arg) to avoid the package-level name clash.
func writerNow(t *testing.T) func() time.Time {
	t.Helper()
	now := mustParseWriteTime(t)
	return func() time.Time { return now }
}

// expectGetMissing primes the mock to return sql.ErrNoRows for the
// initial profile read inside Record* — i.e. "no prior profile."
func expectGetMissing(mock sqlmock.Sqlmock, sessionID string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(sessionID).
		WillReturnError(sql.ErrNoRows)
}

// expectGetProfile primes the mock to return one row for the read.
func expectGetProfile(t *testing.T, mock sqlmock.Sqlmock, p SessionProfile) {
	t.Helper()
	now := mustParseWriteTime(t)
	conceptsJSON, _ := jsonOrNil(p.Concepts)
	filesJSON, _ := jsonOrNil(p.Files)
	lastBeadsJSON, _ := jsonOrNil(p.LastBeads)
	cn := nullStringValue(conceptsJSON)
	fn := nullStringValue(filesJSON)
	ln := nullStringValue(lastBeadsJSON)
	rows := sqlmock.NewRows([]string{
		"session_id", "assignment_id", "agent_id",
		"concepts", "files",
		"tokens_used", "context_window_max", "context_pct",
		"last_beads",
		"last_activity_at", "created_at", "updated_at",
	}).AddRow(
		p.SessionID, p.AssignmentID, string(p.AgentID),
		cn, fn,
		p.TokensUsed, p.ContextWindowMax, p.ContextPct,
		ln,
		now, now, now,
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(p.SessionID).
		WillReturnRows(rows)
}

func nullStringValue(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func expectUpsert(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_profiles")).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestRecordClaim_FromEmptyProfileMergesAtFullWeight(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	expectGetMissing(mock, "sess-1")
	expectUpsert(mock)

	got, err := s.RecordClaim(context.Background(), ClaimEvent{
		SessionID:    "sess-1",
		AssignmentID: "asg-1",
		AgentID:      "alice",
		BeadID:       "gm-1",
		Concepts:     []ConceptTag{"auth"},
		Files:        []string{"src/auth.go"},
		HalfLife:     5,
		Now:          writerNow(t),
	})
	if err != nil {
		t.Fatalf("RecordClaim: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil profile")
	}
	almostEqual(t, "auth at full weight", got.Concepts["auth"], 1.0)
	almostEqual(t, "src/auth.go at full weight", got.Files["src/auth.go"], 1.0)
	if got.AssignmentID != "asg-1" {
		t.Errorf("assignment_id = %q", got.AssignmentID)
	}
	if !got.LastActivityAt.Equal(mustParseWriteTime(t)) {
		t.Errorf("last_activity_at = %v", got.LastActivityAt)
	}
	if !got.CreatedAt.Equal(mustParseWriteTime(t)) {
		t.Errorf("created_at = %v (must stamp on first write)", got.CreatedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestRecordClaim_AgesPriorProfileBeforeMerge(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	prior := SessionProfile{
		SessionID:    "sess-1",
		AssignmentID: "asg-prior",
		AgentID:      "alice",
		Concepts:     map[ConceptTag]float64{"auth": 1.0},
		Files:        map[string]float64{"src/auth.go": 1.0},
	}
	expectGetProfile(t, mock, prior)
	expectUpsert(mock)

	got, err := s.RecordClaim(context.Background(), ClaimEvent{
		SessionID: "sess-1",
		BeadID:    "gm-2",
		Concepts:  []ConceptTag{"auth"}, // same tag → ages prior, then adds 1.0
		HalfLife:  5,
		Now:       writerNow(t),
	})
	if err != nil {
		t.Fatalf("RecordClaim: %v", err)
	}
	// Prior 1.0 aged once at h=5 → 0.5^(1/5) ≈ 0.8706. Plus new
	// contribution at full weight = 1.0. Sum ≈ 1.8706.
	want := math.Pow(0.5, 1.0/5.0) + 1.0
	almostEqual(t, "aged + merged auth", got.Concepts["auth"], want)
}

func TestRecordCompletion_AppendsToLastBeadsRing(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	prior := SessionProfile{
		SessionID: "sess-1",
		LastBeads: []core.WorkItemID{"gm-a", "gm-b", "gm-c", "gm-d", "gm-e"},
	}
	expectGetProfile(t, mock, prior)
	expectUpsert(mock)

	got, err := s.RecordCompletion(context.Background(), CompletionEvent{
		SessionID: "sess-1",
		BeadID:    "gm-f",
		HalfLife:  5,
		Now:       writerNow(t),
	})
	if err != nil {
		t.Fatalf("RecordCompletion: %v", err)
	}
	if len(got.LastBeads) != LastBeadsRingSize {
		t.Errorf("ring size = %d, want %d", len(got.LastBeads), LastBeadsRingSize)
	}
	if got.LastBeads[len(got.LastBeads)-1] != "gm-f" {
		t.Errorf("newest entry = %q, want gm-f", got.LastBeads[len(got.LastBeads)-1])
	}
	if got.LastBeads[0] != "gm-b" {
		t.Errorf("oldest dropped: ring head = %q, want gm-b", got.LastBeads[0])
	}
}

func TestRecordCompletion_PopulatesContextPctFromTokens(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	expectGetMissing(mock, "sess-1")
	expectUpsert(mock)

	got, err := s.RecordCompletion(context.Background(), CompletionEvent{
		SessionID:        "sess-1",
		BeadID:           "gm-1",
		TokensUsed:       50_000,
		ContextWindowMax: 200_000,
		Now:              writerNow(t),
	})
	if err != nil {
		t.Fatalf("RecordCompletion: %v", err)
	}
	almostEqual(t, "context_pct", got.ContextPct, 0.25)
	if got.TokensUsed != 50000 {
		t.Errorf("tokens_used = %d", got.TokensUsed)
	}
}

func TestRecordCompletion_TokensUsedZeroLeavesPriorIntact(t *testing.T) {
	s, mock, db := newMockStore(t)
	defer db.Close()

	prior := SessionProfile{
		SessionID:        "sess-1",
		TokensUsed:       100_000,
		ContextWindowMax: 200_000,
		ContextPct:       0.5,
	}
	expectGetProfile(t, mock, prior)
	expectUpsert(mock)

	got, err := s.RecordCompletion(context.Background(), CompletionEvent{
		SessionID: "sess-1",
		BeadID:    "gm-1",
		Now:       writerNow(t),
	})
	if err != nil {
		t.Fatalf("RecordCompletion: %v", err)
	}
	if got.TokensUsed != 100_000 {
		t.Errorf("zero-value should not overwrite prior tokens; got %d", got.TokensUsed)
	}
}

func TestRecordClaim_NilStoreErrors(t *testing.T) {
	var s *ProfileStore
	if _, err := s.RecordClaim(context.Background(), ClaimEvent{SessionID: "x"}); err == nil {
		t.Fatal("expected error from nil store")
	}
}

func TestRecordClaim_EmptySessionIDRejected(t *testing.T) {
	s, _, db := newMockStore(t)
	defer db.Close()
	_, err := s.RecordClaim(context.Background(), ClaimEvent{})
	if err == nil {
		t.Fatal("expected error on empty SessionID")
	}
}

func TestUpsertProfile_NilProfileRejected(t *testing.T) {
	s, _, db := newMockStore(t)
	defer db.Close()
	if err := s.UpsertProfile(context.Background(), nil); err == nil {
		t.Fatal("expected error on nil profile")
	}
}

func TestUpsertProfile_EmptySessionIDRejected(t *testing.T) {
	s, _, db := newMockStore(t)
	defer db.Close()
	if err := s.UpsertProfile(context.Background(), &SessionProfile{}); err == nil {
		t.Fatal("expected error on empty SessionID")
	}
}

func TestProfileWriter_InterfaceSatisfied(t *testing.T) {
	// Compile-time check defended in tests too.
	var _ ProfileWriter = (*ProfileStore)(nil)
	_ = errors.Is
}

func TestAppendRing_KeepsNewestAtTail(t *testing.T) {
	got := appendRing([]core.WorkItemID{"a", "b", "c"}, "d", 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[2] != "d" {
		t.Errorf("newest = %q, want d", got[2])
	}
	if got[0] != "b" {
		t.Errorf("oldest dropped: head = %q, want b", got[0])
	}
}

func TestAppendRing_ZeroMaxKeepsAll(t *testing.T) {
	got := appendRing(nil, "a", 0)
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("unexpected: %+v", got)
	}
}
