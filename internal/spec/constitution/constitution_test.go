package constitution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_AllKeys(t *testing.T) {
	in := []byte("# C\n\n## Config\n\n```yaml\nasdd_mode: true\nspec_strict: true\nspec_strict_no_tasks_md: false\n```\n")
	c := Parse(in)
	if !c.ASDDModeEffective() {
		t.Fatal("expected asdd_mode true")
	}
	if c.SpecStrict == nil || !*c.SpecStrict {
		t.Fatal("expected spec_strict true")
	}
	if c.SpecStrictNoTasksMDEffective() != false {
		t.Fatal("explicit false should win over inheritance")
	}
}

func TestParse_Inheritance(t *testing.T) {
	in := []byte("## Config\n\n```yaml\nspec_strict: true\n```\n")
	c := Parse(in)
	if !c.SpecStrictNoTasksMDEffective() {
		t.Fatal("should inherit spec_strict")
	}
}

func TestParse_NoConfig(t *testing.T) {
	c := Parse([]byte("# Empty\n"))
	if c.ASDDModeEffective() || c.SpecStrictNoTasksMDEffective() {
		t.Fatal("defaults should be false")
	}
}

func TestLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	gd := filepath.Join(dir, ".gemba")
	_ = os.MkdirAll(gd, 0o755)
	body := []byte("## Config\n\n```yaml\nspec_strict_no_tasks_md: true\n```\n")
	if err := os.WriteFile(filepath.Join(gd, "constitution.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, path, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected path")
	}
	if !c.SpecStrictNoTasksMDEffective() {
		t.Fatal("expected true")
	}
}
