package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// stubLister returns a fixed bead slice regardless of label.
type stubLister struct{ beads []reconcile.Bead }

func (s *stubLister) List(_ context.Context, _ string) ([]reconcile.Bead, error) {
	return s.beads, nil
}

// writeFixture writes a minimal spec.md + lockfile pair for the gate to
// chew on. Stories are seeded with anchors S-01 and S-02 mapped to bd
// IDs gm.s1 and gm.s2.
func writeFixture(t *testing.T, resolutions map[string]string) (specPath, lockPath string) {
	t.Helper()
	dir := t.TempDir()
	specPath = filepath.Join(dir, "spec.md")
	lockPath = filepath.Join(dir, ".spec.lock.json")

	spec := `---
spec_id: SP-gate
nonce: gate-test-nonce
---

# Spec: gate

## Stories

### S-01 First
Status: open
Parent: E-01

### S-02 Second
Status: open
Parent: E-01
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lock{
		SpecID:    "SP-gate",
		Nonce:     "gate-test-nonce",
		AppliedAt: time.Now().UTC(),
		Mappings: []lockfile.Mapping{
			{Anchor: "S-01", BeadID: "gm.s1", ContentHash: "sha256:x", Resolution: resolutions["S-01"]},
			{Anchor: "S-02", BeadID: "gm.s2", ContentHash: "sha256:y", Resolution: resolutions["S-02"]},
		},
	}
	if err := lockfile.Save(lockPath, lock); err != nil {
		t.Fatal(err)
	}
	return specPath, lockPath
}

func TestCheck_CleanAllowsClose(t *testing.T) {
	specPath, lockPath := writeFixture(t, nil)
	lister := &stubLister{beads: []reconcile.Bead{
		{ID: "gm.s1", Status: "closed"},
		{ID: "gm.s2", Status: "closed"},
	}}
	res, err := Check(context.Background(), specPath, lockPath, "gm.epic", CheckOptions{BeadLister: lister})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allow {
		t.Fatalf("expected Allow=true, got %+v", res)
	}
	if len(res.OpenStories) != 0 {
		t.Errorf("expected no open stories, got %+v", res.OpenStories)
	}
}

func TestCheck_MixedRefusesClose(t *testing.T) {
	specPath, lockPath := writeFixture(t, nil)
	lister := &stubLister{beads: []reconcile.Bead{
		{ID: "gm.s1", Status: "closed"},
		{ID: "gm.s2", Status: "open"},
	}}
	res, err := Check(context.Background(), specPath, lockPath, "gm.epic", CheckOptions{BeadLister: lister})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allow {
		t.Fatalf("expected Allow=false with open story, got %+v", res)
	}
	if len(res.OpenStories) != 1 || res.OpenStories[0].Anchor != "S-02" {
		t.Errorf("expected one blocker on S-02, got %+v", res.OpenStories)
	}
	if res.OpenStories[0].BeadID != "gm.s2" || res.OpenStories[0].Status != "open" {
		t.Errorf("blocker projection wrong: %+v", res.OpenStories[0])
	}
	if res.Reason == "" {
		t.Error("expected non-empty Reason on refusal")
	}
}

func TestCheck_WontdoResolutionResolvesOpenStory(t *testing.T) {
	specPath, lockPath := writeFixture(t, map[string]string{"S-02": "wontdo"})
	lister := &stubLister{beads: []reconcile.Bead{
		{ID: "gm.s1", Status: "closed"},
		{ID: "gm.s2", Status: "open"},
	}}
	res, err := Check(context.Background(), specPath, lockPath, "gm.epic", CheckOptions{BeadLister: lister})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allow {
		t.Fatalf("wontdo resolution should resolve open story, got %+v", res)
	}
}

func TestCheck_ForceOverrideAllowsCloseWithReason(t *testing.T) {
	specPath, lockPath := writeFixture(t, nil)
	lister := &stubLister{beads: []reconcile.Bead{
		{ID: "gm.s1", Status: "open"},
		{ID: "gm.s2", Status: "open"},
	}}
	res, err := Check(context.Background(), specPath, lockPath, "gm.epic", CheckOptions{
		BeadLister:  lister,
		Force:       true,
		ForceReason: "operator override: spec abandoned",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allow {
		t.Fatalf("force override should Allow, got %+v", res)
	}
	if res.Reason != "operator override: spec abandoned" {
		t.Errorf("expected Reason to carry operator text, got %q", res.Reason)
	}
	// The blockers should still be reported for the audit trail.
	if len(res.OpenStories) != 2 {
		t.Errorf("expected 2 open stories reported under force, got %d", len(res.OpenStories))
	}
}

func TestCheck_ForceWithoutReasonErrors(t *testing.T) {
	specPath, lockPath := writeFixture(t, nil)
	lister := &stubLister{beads: nil}
	_, err := Check(context.Background(), specPath, lockPath, "gm.epic", CheckOptions{
		BeadLister: lister,
		Force:      true,
	})
	if !errors.Is(err, ErrForceWithoutReason) {
		t.Fatalf("expected ErrForceWithoutReason, got %v", err)
	}
}
