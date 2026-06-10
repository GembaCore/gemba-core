package server

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
)

func plannerSchema() string { return planner.SchemaSQL }

func TestRecycleRowFromEvent_Happy(t *testing.T) {
	at := time.Date(2026, 4, 29, 18, 0, 0, 0, time.UTC)
	ev := core.OrchestrationEvent{
		Kind: "session.recycled",
		At:   at,
		Payload: map[string]any{
			"prior_session_id": "sess-old",
			"new_session_id":   "sess-new",
			"pane_id":          "pane-7",
			"persona_id":       "engineer-claude",
			"agent_type":       "claude",
			"reason":           "context_pressure",
		},
	}
	row := recycleRowFromEvent(ev)
	if row == nil {
		t.Fatalf("row is nil")
	}
	if row.PriorSessionID != "sess-old" {
		t.Errorf("prior = %q", row.PriorSessionID)
	}
	if row.NewSessionID != "sess-new" {
		t.Errorf("new = %q", row.NewSessionID)
	}
	if row.PaneID != "pane-7" {
		t.Errorf("pane = %q", row.PaneID)
	}
	if row.PoolKey != "engineer-claude" {
		t.Errorf("pool_key = %q (no rig in payload → persona only)", row.PoolKey)
	}
	if row.Reason != "context_pressure" {
		t.Errorf("reason = %q", row.Reason)
	}
	if !row.RecycledAt.Equal(at) {
		t.Errorf("recycled_at = %v", row.RecycledAt)
	}
}

func TestRecycleRowFromEvent_DefaultReason(t *testing.T) {
	ev := core.OrchestrationEvent{
		Kind: "session.recycled",
		At:   time.Now().UTC(),
		Payload: map[string]any{
			"prior_session_id": "a",
			"new_session_id":   "b",
		},
	}
	row := recycleRowFromEvent(ev)
	if row == nil || row.Reason != "explicit" {
		t.Errorf("missing reason should default to 'explicit'; got %+v", row)
	}
}

func TestRecycleRowFromEvent_RigInPayloadJoinsKey(t *testing.T) {
	ev := core.OrchestrationEvent{
		Payload: map[string]any{
			"prior_session_id": "a",
			"new_session_id":   "b",
			"persona_id":       "engineer-claude",
			"rig":              "gemba",
		},
	}
	row := recycleRowFromEvent(ev)
	if row.PoolKey != "gemba:engineer-claude" {
		t.Errorf("pool_key = %q, want gemba:engineer-claude", row.PoolKey)
	}
}

func TestRecycleRowFromEvent_MissingIDsRejected(t *testing.T) {
	ev := core.OrchestrationEvent{
		Payload: map[string]any{"persona_id": "engineer-claude"},
	}
	if row := recycleRowFromEvent(ev); row != nil {
		t.Errorf("missing ids should yield nil; got %+v", row)
	}
}

func TestRecycleWriter_Insert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_recycles")).
		WithArgs(
			sqlmock.AnyArg(),   // id
			"engineer-claude",  // pool_key (no rig)
			"pane-9",           // pane_id
			"sess-old",         // prior_session_id
			"sess-new",         // new_session_id
			"context_pressure", // reason
			"",                 // prior_profile_json
			sqlmock.AnyArg(),   // recycled_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	w := NewRecycleWriter(db)
	err = w.Insert(context.Background(), core.OrchestrationEvent{
		Kind: "session.recycled",
		At:   time.Now().UTC(),
		Payload: map[string]any{
			"prior_session_id": "sess-old",
			"new_session_id":   "sess-new",
			"pane_id":          "pane-9",
			"persona_id":       "engineer-claude",
			"reason":           "context_pressure",
		},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestRecycleWriter_RunDeliversToInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO session_recycles")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	w := NewRecycleWriter(db)
	ch := make(chan core.OrchestrationEvent, 1)
	ch <- core.OrchestrationEvent{
		Kind: "session.recycled",
		At:   time.Now().UTC(),
		Payload: map[string]any{
			"prior_session_id": "a",
			"new_session_id":   "b",
		},
	}
	close(ch)
	w.Run(context.Background(), ch)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestRecycleWriter_NilDBRunIsNoop(t *testing.T) {
	w := NewRecycleWriter(nil)
	ch := make(chan core.OrchestrationEvent, 1)
	close(ch)
	w.Run(context.Background(), ch) // should return immediately
}

func TestRecycleWriter_InsertWithoutDB(t *testing.T) {
	w := NewRecycleWriter(nil)
	err := w.Insert(context.Background(), core.OrchestrationEvent{})
	if err == nil {
		t.Error("expected error on nil db")
	}
}

// Compile-time check that errors import stays needed (the writer uses
// errors.As internally — the test exercises only the row-shape path).
var _ = errors.New

// TestSessionRecyclesSchemaEmbedded pins the new table on the
// embedded planner.SchemaSQL DDL. A benign schema rename that drops
// the table would break dolt-mode session_recycles audit trail
// silently — this test surfaces it loudly. gm-s47n.12 §10.3.
func TestSessionRecyclesSchemaEmbedded(t *testing.T) {
	if !strings.Contains(plannerSchema(), "session_recycles") {
		t.Errorf("planner.SchemaSQL missing session_recycles table")
	}
}
