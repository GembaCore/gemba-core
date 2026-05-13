package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSpec = "---\nspec_id: 001\nnonce: abc123def\n---\n# Spec\n\n## Stories\n\n### S-01 Foo\n\nbody\n"

func writeSpec(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(sp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return sp
}

func TestTake_CreatesSnapshotFile(t *testing.T) {
	sp := writeSpec(t, validSpec)
	out, err := Take(sp)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !strings.Contains(out, ".snapshots") {
		t.Fatalf("expected path under .snapshots, got %s", out)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != validSpec {
		t.Fatalf("snapshot content mismatch")
	}
	base := filepath.Base(out)
	if !strings.HasPrefix(base, "001-abc123de-") {
		t.Fatalf("unexpected filename: %s", base)
	}
}

func TestTake_RejectsMissingNonce(t *testing.T) {
	bad := "---\nspec_id: 001\n---\n# Spec\n"
	sp := writeSpec(t, bad)
	_, err := Take(sp)
	if !errors.Is(err, ErrMissingNonce) {
		t.Fatalf("expected ErrMissingNonce, got %v", err)
	}
}

func TestTake_SameSecondDoesNotCollide(t *testing.T) {
	sp := writeSpec(t, validSpec)
	a, err := Take(sp)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Take(sp)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("expected distinct snapshot paths, got identical: %s", a)
	}
	// Both files must exist.
	if _, err := os.Stat(a); err != nil {
		t.Fatalf("first snapshot missing: %v", err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("second snapshot missing: %v", err)
	}
}

func TestTake_RejectsUnparseable(t *testing.T) {
	sp := writeSpec(t, "not a spec\n")
	_, err := Take(sp)
	if err == nil {
		t.Fatal("expected error parsing non-spec input")
	}
}
