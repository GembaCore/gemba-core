package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".spec.lock.json")
	lock := &Lock{
		SpecID:    "SP-1",
		Nonce:     "abc",
		AppliedAt: time.Now().UTC(),
		Mappings: []Mapping{
			{Anchor: "M-01", BeadID: "gm-foo.1", ContentHash: "sha256:deadbeef"},
		},
	}
	if err := Save(path, lock); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpecID != "SP-1" || got.Nonce != "abc" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got.Mappings) != 1 || got.Mappings[0].Anchor != "M-01" {
		t.Errorf("mappings: %+v", got.Mappings)
	}
}

func TestSave_RejectsMissingNonce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".spec.lock.json")
	err := Save(path, &Lock{SpecID: "SP-1"})
	if !errors.Is(err, ErrMissingNonce) {
		t.Fatalf("want ErrMissingNonce, got %v", err)
	}
}

func TestLoad_RejectsMissingNonce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".spec.lock.json")
	if err := os.WriteFile(path, []byte(`{"spec_id":"x","mappings":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrMissingNonce) {
		t.Fatalf("want ErrMissingNonce, got %v", err)
	}
}

func TestSave_AtomicNoStubsLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".spec.lock.json")
	if err := Save(path, &Lock{SpecID: "SP-1", Nonce: "n"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".spec.lock.json" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestDiff_CreatesUpdatesOrphans(t *testing.T) {
	spec := &parser.Spec{
		Milestones: []parser.Milestone{{Anchor: "M-01", Title: "Foundations"}},
		Stories: []parser.Story{
			{Anchor: "S-01", Title: "Edited", Status: "open", Parent: "E-01"},
			{Anchor: "S-02", Title: "New", Status: "open", Parent: "E-01"},
		},
	}
	// Lock has S-01 with a stale hash and S-99 that no longer exists.
	lock := &Lock{
		SpecID: "SP-1",
		Nonce:  "n",
		Mappings: []Mapping{
			{Anchor: "M-01", BeadID: "gm.1", ContentHash: HashMilestone(spec.Milestones[0])},
			{Anchor: "S-01", BeadID: "gm.2", ContentHash: "sha256:stale"},
			{Anchor: "S-99", BeadID: "gm.99", ContentHash: "sha256:gone"},
		},
	}
	creates, updates, orphans := Diff(spec, lock)
	if len(creates) != 1 || creates[0].Anchor != "S-02" {
		t.Errorf("creates: %+v", creates)
	}
	if len(updates) != 1 || updates[0].Anchor != "S-01" || updates[0].BeadID != "gm.2" {
		t.Errorf("updates: %+v", updates)
	}
	if updates[0].ContentHash == "sha256:stale" {
		t.Errorf("update hash should be fresh, got %s", updates[0].ContentHash)
	}
	if len(orphans) != 1 || orphans[0].Anchor != "S-99" {
		t.Errorf("orphans: %+v", orphans)
	}
}

// TestLoad_LegacyLockfileNoResolution confirms that a lockfile written
// before the Mapping.Resolution field existed still loads cleanly. The
// field is omitempty so it defaults to "" — which the closure gate
// (gm-v0sp.12) treats as "no operator annotation, defer to bd status".
func TestLoad_LegacyLockfileNoResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".spec.lock.json")
	// Note: no "resolution" key — this is the pre-gm-v0sp.12 shape.
	legacy := `{
  "spec_id": "SP-legacy",
  "nonce": "n-legacy",
  "applied_at": "2026-01-01T00:00:00Z",
  "mappings": [
    {"anchor":"S-01","bead_id":"gm.1","content_hash":"sha256:abc"}
  ]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("legacy lockfile failed to load: %v", err)
	}
	if len(got.Mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got.Mappings))
	}
	if got.Mappings[0].Resolution != "" {
		t.Errorf("expected empty Resolution, got %q", got.Mappings[0].Resolution)
	}
	if got.Mappings[0].BeadID != "gm.1" {
		t.Errorf("expected bead_id gm.1, got %q", got.Mappings[0].BeadID)
	}
}

func TestDiff_NilLockTreatsAllAsCreates(t *testing.T) {
	spec := &parser.Spec{
		Stories: []parser.Story{{Anchor: "S-01", Title: "x"}},
	}
	creates, updates, orphans := Diff(spec, nil)
	if len(creates) != 1 || len(updates) != 0 || len(orphans) != 0 {
		t.Errorf("got creates=%d updates=%d orphans=%d", len(creates), len(updates), len(orphans))
	}
}
