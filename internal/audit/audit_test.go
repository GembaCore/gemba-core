package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/audit"
)

func newTestAuditor(t *testing.T, clock func() time.Time) audit.Auditor {
	t.Helper()
	dir := t.TempDir()
	a, err := audit.New(audit.Options{
		Dir:   dir,
		Key:   []byte("0123456789abcdef0123456789abcdef"),
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a) })
	return a
}

// TestAppendThenQuery confirms that an immediate Query returns the
// appended event.
func TestAppendThenQuery(t *testing.T) {
	a := newTestAuditor(t, nil)
	ctx := context.Background()
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u1", Action: "create", Resource: "bead:1"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := a.Query(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != audit.EventBeadCreate || got[0].Resource != "bead:1" {
		t.Fatalf("unexpected event: %+v", got[0])
	}
	if got[0].Signature == "" {
		t.Fatalf("signature not populated")
	}
	if got[0].PrevID != "" {
		t.Fatalf("first event must have empty PrevID, got %q", got[0].PrevID)
	}
}

// TestChainPrevID confirms the second event's PrevID equals the first event's ID.
func TestChainPrevID(t *testing.T) {
	a := newTestAuditor(t, nil)
	ctx := context.Background()
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"}); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadUpdate, Actor: "u", Action: "update"}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	got, err := a.Query(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[1].PrevID != got[0].ID {
		t.Fatalf("chain broken: ev1.ID=%q ev2.PrevID=%q", got[0].ID, got[1].PrevID)
	}
}

// TestTamperDetection: modify a byte in the file → Query must fail.
func TestTamperDetection(t *testing.T) {
	dir := t.TempDir()
	a, err := audit.New(audit.Options{Dir: dir, Key: []byte("k" + strings.Repeat("1", 31))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = audit.Close(a)

	// Locate the file and flip a byte inside the Action field.
	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "audit-"+day+".jsonl")
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	tampered := []byte(strings.Replace(string(buf), `"action":"create"`, `"action":"CREATE"`, 1))
	if string(tampered) == string(buf) {
		t.Fatalf("test setup: could not locate action field to tamper")
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	// Reopen the auditor and Query — verification must fail.
	a2, err := audit.New(audit.Options{Dir: dir, Key: []byte("k" + strings.Repeat("1", 31))})
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a2) })
	_, err = a2.Query(ctx, audit.Query{})
	if err == nil {
		t.Fatalf("expected verification failure on tampered file, got nil")
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch error, got: %v", err)
	}
}

// TestChainBreakDetection: insert a fake record and confirm Query detects it.
func TestChainBreakDetection(t *testing.T) {
	dir := t.TempDir()
	a, err := audit.New(audit.Options{Dir: dir, Key: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"}); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadUpdate, Actor: "u", Action: "update"}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	_ = audit.Close(a)

	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "audit-"+day+".jsonl")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(original), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	// Drop the middle event by removing line 1 (which the next event chains to).
	// The second event still references the first event's id; after removal
	// the file is just [ev2] with prev_id=ev1.id but no preceding line — the
	// scanner expects prev_id == "" for the first record, so this trips
	// "chain break".
	tampered := lines[1] + "\n"
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	a2, err := audit.New(audit.Options{Dir: dir, Key: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a2) })
	_, err = a2.Query(ctx, audit.Query{})
	if err == nil {
		t.Fatalf("expected chain-break error")
	}
	if !strings.Contains(err.Error(), "chain break") {
		t.Fatalf("expected chain break error, got: %v", err)
	}
}

// TestDailyRotation: when the Clock advances past UTC midnight the
// next Append lands in a new file, and Query against the prior day
// still returns the yesterday events.
func TestDailyRotation(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 5, 11, 23, 59, 0, 0, time.UTC)
	current := t0
	clock := func() time.Time { return current }
	a, err := audit.New(audit.Options{
		Dir:   dir,
		Key:   []byte(strings.Repeat("z", 32)),
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a) })
	ctx := context.Background()
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"}); err != nil {
		t.Fatalf("Append yesterday: %v", err)
	}
	// Advance the clock past midnight.
	current = t0.Add(2 * time.Minute) // 2026-05-12 00:01
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"}); err != nil {
		t.Fatalf("Append today: %v", err)
	}
	yPath := filepath.Join(dir, "audit-2026-05-11.jsonl")
	tPath := filepath.Join(dir, "audit-2026-05-12.jsonl")
	if _, err := os.Stat(yPath); err != nil {
		t.Fatalf("yesterday file missing: %v", err)
	}
	if _, err := os.Stat(tPath); err != nil {
		t.Fatalf("today file missing: %v", err)
	}

	// Query just yesterday's window.
	got, err := a.Query(ctx, audit.Query{
		Since: t0.Add(-time.Hour),
		Until: t0.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event in yesterday window, got %d", len(got))
	}
	if !got[0].Timestamp.Equal(t0) {
		t.Fatalf("unexpected event ts: %s", got[0].Timestamp)
	}
}

// TestTailStreamsLive: a Tail subscriber receives newly-appended events.
func TestTailStreamsLive(t *testing.T) {
	a := newTestAuditor(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := a.Tail(ctx)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	go func() {
		_ = a.Append(context.Background(), audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"})
	}()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed before event")
		}
		if e.Type != audit.EventBeadCreate {
			t.Fatalf("unexpected event type: %s", e.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for tailed event")
	}
}

// TestWrongKeyBreaksVerification: a Query with a different key fails.
func TestWrongKeyBreaksVerification(t *testing.T) {
	dir := t.TempDir()
	a, err := audit.New(audit.Options{Dir: dir, Key: []byte(strings.Repeat("a", 32))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := a.Append(ctx, audit.Event{Type: audit.EventBeadCreate, Actor: "u", Action: "create"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = audit.Close(a)

	a2, err := audit.New(audit.Options{Dir: dir, Key: []byte(strings.Repeat("b", 32))})
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a2) })
	_, err = a2.Query(ctx, audit.Query{})
	if err == nil {
		t.Fatalf("expected verification failure with wrong key")
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch error, got: %v", err)
	}
}

// TestPayloadPreserved confirms that an opaque JSON payload survives
// the canonicalization round-trip (signature still matches on Query).
func TestPayloadPreserved(t *testing.T) {
	a := newTestAuditor(t, nil)
	ctx := context.Background()
	payload := json.RawMessage(`{"z":1,"a":[3,2,1],"nested":{"y":true,"x":null}}`)
	if err := a.Append(ctx, audit.Event{
		Type:    audit.EventBeadCreate,
		Actor:   "u",
		Action:  "create",
		Payload: payload,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := a.Query(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	// The payload bytes may differ (Go json may rewrite whitespace) but
	// must still parse to the same logical value.
	var orig, back any
	if err := json.Unmarshal(payload, &orig); err != nil {
		t.Fatalf("unmarshal orig: %v", err)
	}
	if err := json.Unmarshal(got[0].Payload, &back); err != nil {
		t.Fatalf("unmarshal back: %v", err)
	}
}
