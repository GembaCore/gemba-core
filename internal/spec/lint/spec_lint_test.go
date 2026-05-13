package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles writes a spec.md + constitution.md pair in a temp dir and returns
// their absolute paths.
func writeFiles(t *testing.T, spec, con string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, "spec.md")
	cp := filepath.Join(dir, "constitution.md")
	if err := os.WriteFile(sp, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, []byte(con), 0o644); err != nil {
		t.Fatal(err)
	}
	return sp, cp
}

func findingsByRule(fs []Finding) map[string]int {
	out := map[string]int{}
	for _, f := range fs {
		out[f.Rule]++
	}
	return out
}

const conRequireDP = "## Config\n\n```yaml\nrequire_decision_parent: true\n```\n"
const conForbidOrphan = "## Config\n\n```yaml\nforbid_orphan_beads: true\n```\n"
const conMinAC2 = "## Config\n\n```yaml\nmin_ac_count: 2\n```\n"
const conRequirePri = "## Config\n\n```yaml\nrequire_priority: true\n```\n"

func TestLint_RequireDecisionParent_Missing(t *testing.T) {
	spec := "---\nspec_id: 001\n---\n# Spec\n\n## Story 1\nAC:\n- a\n"
	sp, cp := writeFiles(t, spec, conRequireDP)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["require_decision_parent"] != 1 {
		t.Fatalf("expected require_decision_parent finding, got %+v", fs)
	}
}

func TestLint_RequireDecisionParent_Present(t *testing.T) {
	spec := "---\nspec_id: 001\ndecision_parent: gm-v0sp\n---\n# Spec\n"
	sp, cp := writeFiles(t, spec, conRequireDP)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["require_decision_parent"] != 0 {
		t.Fatalf("expected no finding, got %+v", fs)
	}
}

func TestLint_ForbidOrphanBeads_MissingDP(t *testing.T) {
	spec := "# Spec\n"
	sp, cp := writeFiles(t, spec, conForbidOrphan)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["forbid_orphan_beads"] != 1 {
		t.Fatalf("expected forbid_orphan_beads finding, got %+v", fs)
	}
}

func TestLint_ForbidOrphanBeads_WithDP_DeferredWarn(t *testing.T) {
	spec := "---\ndecision_parent: gm-v0sp\n---\n# Spec\n"
	sp, cp := writeFiles(t, spec, conForbidOrphan)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, f := range fs {
		if f.Rule == "forbid_orphan_beads" && f.Severity == "warn" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("expected one deferred warn, got %+v", fs)
	}
}

func TestLint_MinACCount_Violation(t *testing.T) {
	spec := "# Spec\n\n## Story 1\n\nAC:\n- only one\n\n## Story 2\n\nAC:\n- a\n- b\n"
	sp, cp := writeFiles(t, spec, conMinAC2)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range fs {
		if f.Rule == "min_ac_count" {
			count++
			if !strings.Contains(f.Message, "Story 1") {
				t.Errorf("expected violation to name Story 1, got %q", f.Message)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 min_ac_count finding, got %d: %+v", count, fs)
	}
}

func TestLint_MinACCount_Satisfied(t *testing.T) {
	spec := "# Spec\n\n## Story 1\nAC:\n- a\n- b\n- c\n"
	sp, cp := writeFiles(t, spec, conMinAC2)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["min_ac_count"] != 0 {
		t.Fatalf("expected no min_ac_count findings, got %+v", fs)
	}
}

func TestLint_RequirePriority_Missing(t *testing.T) {
	spec := "# Spec\n\n## Story 1\n\nSome text\n"
	sp, cp := writeFiles(t, spec, conRequirePri)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["require_priority"] != 1 {
		t.Fatalf("expected require_priority finding, got %+v", fs)
	}
}

func TestLint_RequirePriority_PresentInline(t *testing.T) {
	spec := "# Spec\n\n## User Story 1 - Foo (Priority: P1)\n\nbody\n"
	sp, cp := writeFiles(t, spec, conRequirePri)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["require_priority"] != 0 {
		t.Fatalf("expected no finding, got %+v", fs)
	}
}

func TestLint_RequirePriority_PresentInBody(t *testing.T) {
	spec := "# Spec\n\n## Story 1\n\nPriority: P2\n"
	sp, cp := writeFiles(t, spec, conRequirePri)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if findingsByRule(fs)["require_priority"] != 0 {
		t.Fatalf("expected no finding, got %+v", fs)
	}
}

func TestLint_NoConstitution_NoFindings(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "spec.md")
	_ = os.WriteFile(sp, []byte("# Spec\n## Story 1\n"), 0o644)
	fs, err := Lint(sp, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected no findings, got %+v", fs)
	}
}
