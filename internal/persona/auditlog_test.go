package persona

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
)

func sampleRecord(id string, started time.Time) corepersona.PersonaConsultRecord {
	return corepersona.PersonaConsultRecord{
		ID:        id,
		PersonaID: "project-manager",
		SkillID:   "epic_order",
		Workspace: "gemba",
		StartedAt: started,
		EndedAt:   started.Add(2 * time.Second),
		Request:   json.RawMessage(`{"workspace":"gemba"}`),
		Response:  json.RawMessage(`{"lines":[]}`),
		Tokens:    corepersona.TokenUsage{In: 100, Out: 50},
		Dollars:   0.0123,
		Model:     "claude-opus-4-7",
		LatencyMs: 4210,
	}
}

func TestAuditLog_AppendAndGet(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	rec := sampleRecord("c1", now)

	if err := log.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := log.Get("c1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != rec.ID || got.PersonaID != rec.PersonaID || got.SkillID != rec.SkillID {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Tokens.In != 100 || got.Tokens.Out != 50 {
		t.Errorf("tokens lost: %+v", got.Tokens)
	}
	if got.Dollars != rec.Dollars {
		t.Errorf("dollars: %v want %v", got.Dollars, rec.Dollars)
	}
}

func TestAuditLog_GetOnDate(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	rec := sampleRecord("c2", day)
	if err := log.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := log.GetOnDate("c2", day)
	if err != nil {
		t.Fatalf("GetOnDate: %v", err)
	}
	if got.ID != "c2" {
		t.Errorf("got %q, want c2", got.ID)
	}

	// Wrong day → fs.ErrNotExist.
	_, err = log.GetOnDate("c2", day.AddDate(0, 0, 1))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v, want fs.ErrNotExist", err)
	}
}

func TestAuditLog_GetMissing(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	_, err := log.Get("nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist wrap", err)
	}
}

func TestAuditLog_OverwriteSameID(t *testing.T) {
	// Appending a record whose ID already exists overwrites — the
	// most recent state wins (e.g. operator applies a SuggestedAction
	// after the fact, AppliedIdx grows).
	log := NewAuditLog(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	rec := sampleRecord("c3", day)
	if err := log.Append(rec); err != nil {
		t.Fatalf("first Append: %v", err)
	}

	rec.AppliedIdx = []int{0, 2}
	rec.Dollars = 0.025
	if err := log.Append(rec); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	got, err := log.Get("c3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.AppliedIdx) != 2 || got.AppliedIdx[0] != 0 || got.AppliedIdx[1] != 2 {
		t.Errorf("applied_idx not updated: %+v", got.AppliedIdx)
	}
	if got.Dollars != 0.025 {
		t.Errorf("dollars not updated: %v", got.Dollars)
	}
}

func TestAuditLog_AppendValidates(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	good := sampleRecord("c1", time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC))

	cases := []struct {
		name    string
		mutate  func(r *corepersona.PersonaConsultRecord)
		wantSub string
	}{
		{
			name:    "empty id",
			mutate:  func(r *corepersona.PersonaConsultRecord) { r.ID = "" },
			wantSub: "id must not be empty",
		},
		{
			name:    "zero started_at",
			mutate:  func(r *corepersona.PersonaConsultRecord) { r.StartedAt = time.Time{} },
			wantSub: "started_at",
		},
		{
			name:    "empty persona_id",
			mutate:  func(r *corepersona.PersonaConsultRecord) { r.PersonaID = "" },
			wantSub: "persona_id",
		},
		{
			name:    "empty skill_id",
			mutate:  func(r *corepersona.PersonaConsultRecord) { r.SkillID = "" },
			wantSub: "skill_id",
		},
		{
			name:    "id with slash",
			mutate:  func(r *corepersona.PersonaConsultRecord) { r.ID = "evil/path" },
			wantSub: "path separators",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := good
			c.mutate(&r)
			err := log.Append(r)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestAuditLog_ListNewestFirst(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	d1 := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 4, 25, 11, 0, 0, 0, time.UTC) // same day as d2

	for i, when := range []time.Time{d1, d2, d3} {
		rec := sampleRecord(string(rune('a'+i)), when)
		if err := log.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	got, err := log.List(ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}
	// Newest day first; within day, lexical-reverse means 'c' before
	// 'b' (the dispatcher will generate time-monotonic ids; this
	// orders fine for ULID/KSUID-style ids).
	if got[0].ID != "c" || got[1].ID != "b" || got[2].ID != "a" {
		t.Errorf("order = %v %v %v, want c b a", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestAuditLog_ListFilters(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	rPM := sampleRecord("pm-1", day)
	rDocs := sampleRecord("docs-1", day)
	rDocs.PersonaID = "documentarian"
	rDocs.SkillID = "docs_edit"
	if err := log.Append(rPM); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(rDocs); err != nil {
		t.Fatal(err)
	}

	// Filter by persona.
	got, err := log.List(ListFilter{PersonaID: "documentarian"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "docs-1" {
		t.Errorf("persona filter = %+v", got)
	}

	// Filter by skill.
	got, err = log.List(ListFilter{SkillID: "epic_order"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "pm-1" {
		t.Errorf("skill filter = %+v", got)
	}

	// Filter by both — nothing matches.
	got, err = log.List(ListFilter{PersonaID: "project-manager", SkillID: "docs_edit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("intersection should be empty: %+v", got)
	}
}

func TestAuditLog_ListDateRange(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	for _, day := range []time.Time{
		time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
	} {
		rec := sampleRecord("c"+day.Format("0102"), day)
		if err := log.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Inclusive on both ends.
	got, err := log.List(ListFilter{
		Since: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("date range = %d records, want 2", len(got))
	}
}

func TestAuditLog_ListLimit(t *testing.T) {
	log := NewAuditLog(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := log.Append(sampleRecord(id, day)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := log.List(ListFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("limit = %d, want 2", len(got))
	}
}

func TestAuditLog_ListEmptyRoot(t *testing.T) {
	// Root that doesn't exist returns empty, not an error — fresh
	// installations must not fail to render /insights/personas.
	log := NewAuditLog(filepath.Join(t.TempDir(), "no-such"))
	got, err := log.List(ListFilter{})
	if err != nil {
		t.Errorf("missing root should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d records, want 0", len(got))
	}
}

func TestAuditLog_AtomicWrite(t *testing.T) {
	// After a successful Append, the partition directory contains
	// exactly one .json file (the final) and no .tmp files.
	log := NewAuditLog(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	if err := log.Append(sampleRecord("c1", day)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	dir := log.dirFor(day)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("found stale temp file: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1", len(entries))
	}
}

func TestAuditLog_DefaultRoot(t *testing.T) {
	// DefaultRoot is non-empty and lives under either $HOME/.gemba/persona
	// or /tmp/gemba-persona/ (the fallback). We don't assert HOME here
	// because tests can run as a user with no real home.
	got := DefaultRoot()
	if got == "" {
		t.Error("DefaultRoot returned empty string")
	}
	if !strings.Contains(got, "gemba") {
		t.Errorf("DefaultRoot = %q, expected to contain 'gemba'", got)
	}
}

func TestAuditLog_RootGetter(t *testing.T) {
	tmp := t.TempDir()
	log := NewAuditLog(tmp)
	if log.Root() != tmp {
		t.Errorf("Root() = %q, want %q", log.Root(), tmp)
	}
}

func TestAuditLog_NewWithEmptyRootUsesDefault(t *testing.T) {
	log := NewAuditLog("")
	if log.Root() == "" {
		t.Error("empty root should fall back to DefaultRoot")
	}
}
