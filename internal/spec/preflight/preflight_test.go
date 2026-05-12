package preflight

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

func TestRun_Clean(t *testing.T) {
	dir := t.TempDir()
	tr := true
	if r := Run(dir, "001-foo", constitution.Constitution{SpecStrictNoTasksMD: &tr}); !r.OK {
		t.Fatalf("clean dir should pass, got %+v", r)
	}
}

func TestRun_PlantedTasksMD(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "specs", "001-foo"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "specs", "001-foo", "tasks.md"), []byte("x"), 0o644)
	tr := true
	r := Run(dir, "001-foo", constitution.Constitution{SpecStrictNoTasksMD: &tr})
	if r.OK {
		t.Fatal("expected refuse")
	}
	if len(r.OffenderPaths) == 0 {
		t.Fatal("expected offender list")
	}
}

func TestRun_PermissiveProceeds(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("x"), 0o644)
	if r := Run(dir, "001-foo", constitution.Constitution{}); !r.OK {
		t.Fatal("permissive must proceed")
	}
}
