package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

func plant(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan_DetectsTasksMD(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, "tasks.md")
	plant(t, dir, "specs/001/tasks.md")
	plant(t, dir, ".specify/templates/tasks-template.md") // allowed
	tr := true
	f, err := Scan(dir, constitution.Constitution{SpecStrictNoTasksMD: &tr})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(f), f)
	}
}

func TestScan_PermissiveNoFindings(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, "tasks.md")
	f, err := Scan(dir, constitution.Constitution{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 0 {
		t.Fatalf("permissive mode should report nothing, got %v", f)
	}
}

func TestScan_InheritanceDefault(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, "tasks.md")
	tr := true
	// Only spec_strict is set; spec_strict_no_tasks_md should inherit true.
	f, err := Scan(dir, constitution.Constitution{SpecStrict: &tr})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 {
		t.Fatalf("inheritance should trigger lint, got %d", len(f))
	}
}
