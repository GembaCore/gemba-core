package apply

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// --- stub mutator ---------------------------------------------------------

type stubMutator struct {
	mu           sync.Mutex
	failCreateAt int    // 1-indexed; 0 = never fail
	failUpdateAt int
	failCloseAt  int
	created      int
	updates      int
	closes       int
	createIDs    map[string]string // anchor -> assigned id
	rolledBack   bool
	rollbackArg  *Receipt
	// idempotency: skip anchor on replay
	seen map[string]bool
}

func newStubMutator() *stubMutator {
	return &stubMutator{createIDs: map[string]string{}, seen: map[string]bool{}}
}

func (s *stubMutator) Create(_ context.Context, op reconcile.Op) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen["create:"+op.Anchor] {
		return s.createIDs[op.Anchor], nil
	}
	s.created++
	if s.failCreateAt != 0 && s.created == s.failCreateAt {
		return "", errors.New("create boom")
	}
	id := "bd-" + op.Anchor
	s.createIDs[op.Anchor] = id
	s.seen["create:"+op.Anchor] = true
	return id, nil
}

func (s *stubMutator) Update(_ context.Context, op reconcile.Op) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen["update:"+op.Anchor] {
		return nil
	}
	s.updates++
	if s.failUpdateAt != 0 && s.updates == s.failUpdateAt {
		return errors.New("update boom")
	}
	s.seen["update:"+op.Anchor] = true
	return nil
}

func (s *stubMutator) Close(_ context.Context, op reconcile.Op, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen["close:"+op.Anchor] {
		return nil
	}
	s.closes++
	if s.failCloseAt != 0 && s.closes == s.failCloseAt {
		return errors.New("close boom")
	}
	s.seen["close:"+op.Anchor] = true
	return nil
}

func (s *stubMutator) Rollback(_ context.Context, r *Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rolledBack = true
	s.rollbackArg = r
	return nil
}

// --- helpers --------------------------------------------------------------

func planFixture() *reconcile.Plan {
	return &reconcile.Plan{
		SpecID:    "SP-001",
		Nonce:     "nonce-current",
		LockNonce: "nonce-current",
		Creates: []reconcile.Op{
			{Kind: reconcile.OpCreate, Anchor: "M-01", Title: "Foundations", Type: "milestone", ContentHash: "sha256:m01"},
			{Kind: reconcile.OpCreate, Anchor: "S-01", Title: "Login", Type: "story", Parent: "E-01", Priority: "p1", ContentHash: "sha256:s01"},
		},
		Updates: []reconcile.Op{
			{
				Kind: reconcile.OpUpdate, Anchor: "E-01", BeadID: "bd-existing-E01",
				Title: "New Epic Title", Type: "epic", ContentHash: "sha256:e01-new",
				FieldDiffs: map[string]reconcile.FieldChange{
					"title": {From: "Old Title", To: "New Epic Title"},
				},
			},
		},
		Orphans: []reconcile.Op{
			{Kind: reconcile.OpOrphan, Anchor: "S-99", BeadID: "bd-orphan", Title: "Removed", Type: "story", ContentHash: "sha256:s99"},
		},
	}
}

func emptyLock(nonce string) *lockfile.Lock {
	return &lockfile.Lock{
		SpecID: "SP-001", Nonce: nonce,
		Mappings: []lockfile.Mapping{
			{Anchor: "E-01", BeadID: "bd-existing-E01", ContentHash: "sha256:e01-old"},
			{Anchor: "S-99", BeadID: "bd-orphan", ContentHash: "sha256:s99"},
		},
	}
}

// --- tests ----------------------------------------------------------------

func TestApply_HappyPath_CreateUpdate_NoPrune(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	stub := newStubMutator()

	rec, err := Apply(context.Background(), plan, lock, Options{Mutator: stub})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.Created) != 2 {
		t.Fatalf("want 2 created, got %d", len(rec.Created))
	}
	if len(rec.Updated) != 1 {
		t.Fatalf("want 1 updated, got %d", len(rec.Updated))
	}
	if len(rec.Pruned) != 0 {
		t.Fatalf("want 0 pruned (prune=false), got %d", len(rec.Pruned))
	}
	if len(rec.SkippedOrphans) != 1 {
		t.Fatalf("want 1 skipped orphan, got %d", len(rec.SkippedOrphans))
	}
	// Lockfile should now contain new create mappings.
	got := map[string]string{}
	for _, m := range lock.Mappings {
		got[m.Anchor] = m.ContentHash
	}
	if got["M-01"] != "sha256:m01" {
		t.Errorf("M-01 mapping not staged: %v", lock.Mappings)
	}
	if got["E-01"] != "sha256:e01-new" {
		t.Errorf("E-01 hash not updated: %v", lock.Mappings)
	}
	// S-99 still present since prune disabled.
	if _, ok := got["S-99"]; !ok {
		t.Errorf("S-99 should remain when prune=false")
	}
}

func TestApply_HappyPath_Prune(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	stub := newStubMutator()

	rec, err := Apply(context.Background(), plan, lock, Options{Mutator: stub, Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Pruned) != 1 {
		t.Fatalf("want 1 pruned, got %d", len(rec.Pruned))
	}
	if len(rec.SkippedOrphans) != 0 {
		t.Fatalf("want 0 skipped, got %d", len(rec.SkippedOrphans))
	}
	// S-99 removed.
	for _, m := range lock.Mappings {
		if m.Anchor == "S-99" {
			t.Errorf("S-99 should be removed after prune")
		}
	}
}

func TestApply_Idempotent_Replay(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	stub := newStubMutator()

	if _, err := Apply(context.Background(), plan, lock, Options{Mutator: stub}); err != nil {
		t.Fatal(err)
	}
	createdBefore := stub.created
	updatedBefore := stub.updates

	// Replay the same plan: stub returns early on seen tuples.
	plan2 := planFixture()
	if _, err := Apply(context.Background(), plan2, &lockfile.Lock{SpecID: "SP-001", Nonce: "nonce-current"}, Options{Mutator: stub}); err != nil {
		t.Fatal(err)
	}
	if stub.created != createdBefore {
		t.Errorf("replay should not invoke new Create calls: was %d now %d", createdBefore, stub.created)
	}
	if stub.updates != updatedBefore {
		t.Errorf("replay should not invoke new Update calls: was %d now %d", updatedBefore, stub.updates)
	}
}

func TestApply_NonceStale_RefusesAndDoesNotMutate(t *testing.T) {
	plan := planFixture()
	plan.NonceStale = true
	plan.LockNonce = "nonce-old"
	lock := emptyLock("nonce-old")
	preMappings := append([]lockfile.Mapping(nil), lock.Mappings...)
	stub := newStubMutator()

	rec, err := Apply(context.Background(), plan, lock, Options{Mutator: stub})
	if !errors.Is(err, ErrNonceStale) {
		t.Fatalf("want ErrNonceStale, got %v", err)
	}
	if rec.RolledBack {
		t.Errorf("should not record RolledBack on stale-nonce refusal")
	}
	if stub.created != 0 || stub.updates != 0 || stub.closes != 0 {
		t.Errorf("no bd calls should fire when nonce stale; created=%d updates=%d closes=%d",
			stub.created, stub.updates, stub.closes)
	}
	if len(lock.Mappings) != len(preMappings) {
		t.Errorf("lockfile mutated despite nonce stale")
	}
}

func TestApply_MidFailure_TriggersRollback_AndLockfileUnchanged(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	preMappings := append([]lockfile.Mapping(nil), lock.Mappings...)
	stub := newStubMutator()
	stub.failUpdateAt = 1 // first update fails after 2 creates succeed

	rec, err := Apply(context.Background(), plan, lock, Options{Mutator: stub})
	if err == nil {
		t.Fatal("expected error from mid-apply failure")
	}
	if !stub.rolledBack {
		t.Errorf("expected Rollback to be invoked")
	}
	if !rec.RolledBack {
		t.Errorf("receipt should mark RolledBack=true")
	}
	if stub.rollbackArg == nil || len(stub.rollbackArg.Created) != 2 {
		t.Errorf("rollback should receive the 2 already-created entries; got %+v", stub.rollbackArg)
	}
	// Lockfile staged additions, but per the contract callers must NOT
	// save lock on error — verify Apply did not corrupt prior mappings'
	// content hashes for pre-existing anchors (E-01 should still be old).
	for _, m := range lock.Mappings {
		if m.Anchor == "E-01" && m.ContentHash != preMappings[0].ContentHash {
			t.Errorf("E-01 hash mutated despite rollback: %s vs %s", m.ContentHash, preMappings[0].ContentHash)
		}
	}
}

func TestApply_DryRun_NoMutatorCalls(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	stub := newStubMutator()

	rec, err := Apply(context.Background(), plan, lock, Options{Mutator: stub, DryRun: true, Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.DryRun {
		t.Error("receipt should mark DryRun")
	}
	if stub.created != 0 || stub.updates != 0 || stub.closes != 0 {
		t.Errorf("dry run should not invoke mutator; created=%d updates=%d closes=%d",
			stub.created, stub.updates, stub.closes)
	}
	if len(rec.Created) != 2 || len(rec.Updated) != 1 || len(rec.Pruned) != 1 {
		t.Errorf("dry run receipt should still tally ops")
	}
}

func TestApply_AuditEmissions(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	stub := newStubMutator()
	a := &capturingAuditor{}

	if _, err := Apply(context.Background(), plan, lock, Options{Mutator: stub, Auditor: a, Prune: true}); err != nil {
		t.Fatal(err)
	}
	gotStart, gotComplete := false, false
	opCount := 0
	for _, e := range a.events {
		switch e {
		case "spec.apply.start":
			gotStart = true
		case "spec.apply.complete":
			gotComplete = true
		case "spec.apply.op":
			opCount++
		}
	}
	if !gotStart || !gotComplete {
		t.Errorf("missing start/complete events: %v", a.events)
	}
	// 2 creates + 1 update + 1 prune
	if opCount != 4 {
		t.Errorf("want 4 op events, got %d: %v", opCount, a.events)
	}
}

func TestApply_AuditFailureDoesNotBlock(t *testing.T) {
	plan := planFixture()
	lock := emptyLock("nonce-current")
	stub := newStubMutator()
	a := &panickingAuditor{}
	_, err := Apply(context.Background(), plan, lock, Options{Mutator: stub, Auditor: a})
	if err != nil {
		t.Fatalf("audit panic should be swallowed: %v", err)
	}
}

func TestApply_NilPlan(t *testing.T) {
	_, err := Apply(context.Background(), nil, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "nil plan") {
		t.Fatalf("want nil-plan error, got %v", err)
	}
}

// --- auditor stubs --------------------------------------------------------

type capturingAuditor struct {
	events []string
}

func (c *capturingAuditor) Emit(_ context.Context, event string, _ map[string]any) {
	c.events = append(c.events, event)
}

type panickingAuditor struct{}

func (panickingAuditor) Emit(_ context.Context, _ string, _ map[string]any) { panic("boom") }
