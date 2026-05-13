package reconcile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

// --- helpers --------------------------------------------------------------

func newSpec(nonce string) *parser.Spec {
	return &parser.Spec{
		Frontmatter: parser.Frontmatter{
			SpecID: "SP-001",
			Nonce:  nonce,
		},
		Milestones: []parser.Milestone{
			{Anchor: "M-01", Title: "Foundations", Body: "initial milestone"},
		},
		Epics: []parser.Epic{
			{Anchor: "E-01", Title: "Bootstrap", Body: "first epic"},
		},
		Stories: []parser.Story{
			{Anchor: "S-01", Title: "Login", Parent: "E-01", Priority: "p1", AC: []string{"can sign in"}, Body: "story body"},
			{Anchor: "S-02", Title: "Logout", Parent: "E-01", Priority: "p2", AC: []string{"can sign out"}, Body: "second story"},
		},
	}
}

func lockFromSpec(spec *parser.Spec, beadPrefix string) *lockfile.Lock {
	lock := &lockfile.Lock{SpecID: spec.Frontmatter.SpecID, Nonce: spec.Frontmatter.Nonce}
	id := 100
	for _, m := range spec.Milestones {
		lock.Mappings = append(lock.Mappings, lockfile.Mapping{
			Anchor: m.Anchor, BeadID: idFor(beadPrefix, id), ContentHash: lockfile.HashMilestone(m),
		})
		id++
	}
	for _, e := range spec.Epics {
		lock.Mappings = append(lock.Mappings, lockfile.Mapping{
			Anchor: e.Anchor, BeadID: idFor(beadPrefix, id), ContentHash: lockfile.HashEpic(e),
		})
		id++
	}
	for _, s := range spec.Stories {
		lock.Mappings = append(lock.Mappings, lockfile.Mapping{
			Anchor: s.Anchor, BeadID: idFor(beadPrefix, id), ContentHash: lockfile.HashStory(s),
		})
		id++
	}
	return lock
}

func idFor(prefix string, n int) string {
	return prefix + "-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// --- table tests ----------------------------------------------------------

func TestDiff_AllCreatesWhenLockEmpty(t *testing.T) {
	spec := newSpec("nonce-1")
	plan, err := Diff(spec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.Creates), 4; got != want {
		t.Fatalf("creates: got %d want %d", got, want)
	}
	if len(plan.Updates) != 0 || len(plan.Orphans) != 0 {
		t.Fatal("expected no updates/orphans when lock is empty")
	}
	// Sorted by anchor.
	if plan.Creates[0].Anchor != "E-01" || plan.Creates[1].Anchor != "M-01" {
		t.Fatalf("creates not sorted by anchor: %#v", plan.Creates)
	}
}

func TestDiff_NoChangesIsEmptyPlan(t *testing.T) {
	spec := newSpec("nonce-2")
	lock := lockFromSpec(spec, "gm-test")
	plan, err := Diff(spec, nil, lock)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Fatalf("expected empty plan, got %+v", plan)
	}
}

func TestDiff_UpdateOnContentDrift(t *testing.T) {
	spec := newSpec("nonce-3")
	lock := lockFromSpec(spec, "gm-test")
	// Mutate a story so its hash drifts.
	spec.Stories[0].Title = "Sign in (renamed)"

	beads := []Bead{
		{ID: "gm-test-102", Title: "Login", Type: "story", Parent: "E-01", Priority: "p1", Status: "in_progress", Assignee: "alice"},
	}
	plan, err := Diff(spec, beads, lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Updates) != 1 {
		t.Fatalf("expected one update, got %d", len(plan.Updates))
	}
	up := plan.Updates[0]
	if up.Anchor != "S-01" || up.BeadID != "gm-test-102" {
		t.Fatalf("unexpected update: %+v", up)
	}
	if up.FieldDiffs["title"].From != "Login" || up.FieldDiffs["title"].To != "Sign in (renamed)" {
		t.Fatalf("title diff incorrect: %+v", up.FieldDiffs["title"])
	}
	// Status is bead-owned: informational only.
	if !up.FieldDiffs["status"].Informational {
		t.Fatal("status should be informational")
	}
	if !up.FieldDiffs["assignee"].Informational {
		t.Fatal("assignee should be informational")
	}
}

func TestDiff_OrphanWhenAnchorMissingFromSpec(t *testing.T) {
	spec := newSpec("nonce-4")
	lock := lockFromSpec(spec, "gm-test")
	// Drop S-02 from spec.
	spec.Stories = spec.Stories[:1]

	plan, err := Diff(spec, nil, lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(plan.Orphans))
	}
	o := plan.Orphans[0]
	if o.Anchor != "S-02" {
		t.Fatalf("orphan anchor: %s", o.Anchor)
	}
	if o.Rationale != "--prune" {
		t.Fatalf("orphan rationale: %s", o.Rationale)
	}
}

func TestDiff_NonceStalePropagates(t *testing.T) {
	spec := newSpec("spec-nonce")
	lock := lockFromSpec(spec, "gm-test")
	lock.Nonce = "old-nonce"

	// Force a drift so we have ops to inspect.
	spec.Stories[0].Title = "Renamed"

	plan, err := Diff(spec, nil, lock)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.NonceStale {
		t.Fatal("plan should be NonceStale when lock.Nonce != spec.Nonce")
	}
	for _, op := range plan.Updates {
		if !op.NonceStale {
			t.Fatalf("update op should carry NonceStale: %+v", op)
		}
	}
}

func TestDiff_NilSpecErrors(t *testing.T) {
	if _, err := Diff(nil, nil, nil); err == nil {
		t.Fatal("expected error on nil spec")
	}
}

// --- JSON marshaling ------------------------------------------------------

func TestPlan_MarshalJSON_StableAndVersioned(t *testing.T) {
	spec := newSpec("nonce-j")
	plan, err := Diff(spec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("MarshalJSON not deterministic")
	}
	if !strings.Contains(string(a), `"gemba.reconcile.v1"`) || !strings.Contains(string(a), `"$schema"`) {
		t.Fatalf("missing schema version in output:\n%s", string(a))
	}
}

// --- property: idempotence -----------------------------------------------

// TestDiff_Idempotence applies every create/update from a Plan into the
// existing bead set + lockfile, then re-plans, and asserts the second plan
// is empty.
func TestDiff_Idempotence(t *testing.T) {
	cases := []struct {
		name string
		mut  func(spec *parser.Spec, lock *lockfile.Lock, beads *[]Bead)
	}{
		{
			name: "all creates from empty lock",
			mut: func(spec *parser.Spec, lock *lockfile.Lock, beads *[]Bead) {
				lock.Mappings = nil
				*beads = nil
			},
		},
		{
			name: "single update on title drift",
			mut: func(spec *parser.Spec, lock *lockfile.Lock, beads *[]Bead) {
				spec.Stories[0].Title = "Renamed"
				*beads = []Bead{{ID: "gm-test-102", Title: "Login", Type: "story", Parent: "E-01", Priority: "p1"}}
			},
		},
		{
			name: "mixed creates + update",
			mut: func(spec *parser.Spec, lock *lockfile.Lock, beads *[]Bead) {
				// Drop one mapping so it shows up as a create.
				lock.Mappings = lock.Mappings[:len(lock.Mappings)-1]
				spec.Stories[0].Body = "different body"
				*beads = []Bead{{ID: "gm-test-102", Title: "Login", Type: "story", Parent: "E-01", Priority: "p1"}}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := newSpec("nonce-idem")
			lock := lockFromSpec(spec, "gm-test")
			var beads []Bead
			tc.mut(spec, lock, &beads)

			plan, err := Diff(spec, beads, lock)
			if err != nil {
				t.Fatal(err)
			}

			// Simulate apply: every Create adds a mapping (synthetic id) +
			// a matching bead; every Update overwrites the lockfile hash
			// and bead fields.
			applyPlan(plan, lock, &beads)

			plan2, err := Diff(spec, beads, lock)
			if err != nil {
				t.Fatal(err)
			}
			if !plan2.Empty() {
				t.Fatalf("second plan not empty after apply:\n%+v", plan2)
			}
		})
	}
}

// applyPlan mutates lock + beads as if the apply step had run. This is the
// minimal apply needed to validate idempotence; it is NOT the real apply
// implementation (that lives in gm-v0sp.6 and shells out to bd).
func applyPlan(plan *Plan, lock *lockfile.Lock, beads *[]Bead) {
	nextID := 9000
	for _, op := range plan.Creates {
		id := "gm-synth-" + itoa(nextID)
		nextID++
		lock.Mappings = append(lock.Mappings, lockfile.Mapping{
			Anchor: op.Anchor, BeadID: id, ContentHash: op.ContentHash,
		})
		*beads = append(*beads, Bead{
			ID: id, Title: op.Title, Type: op.Type, Parent: op.Parent, Priority: op.Priority,
		})
	}
	for _, op := range plan.Updates {
		for i := range lock.Mappings {
			if lock.Mappings[i].Anchor == op.Anchor {
				lock.Mappings[i].ContentHash = op.ContentHash
				break
			}
		}
		for i := range *beads {
			if (*beads)[i].ID == op.BeadID {
				(*beads)[i].Title = op.Title
				(*beads)[i].Type = op.Type
				(*beads)[i].Parent = op.Parent
				(*beads)[i].Priority = op.Priority
				break
			}
		}
	}
	// Orphans intentionally not pruned — apply chooses whether to honor
	// the rationale. Idempotence still holds because orphans stay orphans
	// across re-plan.
	_ = context.Background()
}

// --- bead lister parsing --------------------------------------------------

func TestParseBeadList_Array(t *testing.T) {
	data := []byte(`[{"id":"gm-1","title":"x","type":"story"}]`)
	out, err := parseBeadList(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "gm-1" {
		t.Fatalf("unexpected parse: %+v", out)
	}
}

func TestParseBeadList_IssuesWrapper(t *testing.T) {
	data := []byte(`{"issues":[{"id":"gm-2","title":"y","type":"epic"}]}`)
	out, err := parseBeadList(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "gm-2" {
		t.Fatalf("unexpected parse: %+v", out)
	}
}

func TestStaticBeadLister_IgnoresLabel(t *testing.T) {
	s := &StaticBeadLister{Beads: []Bead{{ID: "x"}}}
	got, err := s.List(context.Background(), "any-label")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d beads", len(got))
	}
}
