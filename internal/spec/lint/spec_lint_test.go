package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimal valid spec frontmatter. spec_id + nonce are required by the parser.
const fmBase = "spec_id: 001\nnonce: abc123\n"

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

func ruleCounts(fs []Finding) map[string]int {
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
	spec := "---\n" + fmBase + "---\n# Spec\n"
	sp, cp := writeFiles(t, spec, conRequireDP)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if ruleCounts(fs)["require_decision_parent"] != 1 {
		t.Fatalf("expected require_decision_parent finding, got %+v", fs)
	}
}

func TestLint_RequireDecisionParent_Present(t *testing.T) {
	spec := "---\n" + fmBase + "decision_parent: gm-v0sp\n---\n# Spec\n"
	sp, cp := writeFiles(t, spec, conRequireDP)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if ruleCounts(fs)["require_decision_parent"] != 0 {
		t.Fatalf("expected no finding, got %+v", fs)
	}
}

func TestLint_ForbidOrphanBeads_MissingDP(t *testing.T) {
	spec := "---\n" + fmBase + "---\n# Spec\n"
	sp, cp := writeFiles(t, spec, conForbidOrphan)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, f := range fs {
		if f.Rule == "forbid_orphan_beads" && f.Severity == "error" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("expected forbid_orphan_beads error, got %+v", fs)
	}
}

func TestLint_ForbidOrphanBeads_WithDP_DeferredWarn(t *testing.T) {
	spec := "---\n" + fmBase + "decision_parent: gm-v0sp\n---\n# Spec\n"
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
	spec := "---\n" + fmBase + "---\n## Stories\n\n### S-01 Title one\n\nAC:\n- only one\n\n### S-02 Title two\n\nAC:\n- a\n- b\n"
	sp, cp := writeFiles(t, spec, conMinAC2)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range fs {
		if f.Rule == "min_ac_count" {
			count++
			if !strings.Contains(f.Message, "S-01") {
				t.Errorf("expected violation to name S-01, got %q", f.Message)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 min_ac_count finding, got %d: %+v", count, fs)
	}
}

func TestLint_MinACCount_Satisfied(t *testing.T) {
	spec := "---\n" + fmBase + "---\n## Stories\n\n### S-01 Foo\n\nAC:\n- a\n- b\n- c\n"
	sp, cp := writeFiles(t, spec, conMinAC2)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if ruleCounts(fs)["min_ac_count"] != 0 {
		t.Fatalf("expected no min_ac_count findings, got %+v", fs)
	}
}

func TestLint_RequirePriority_Missing(t *testing.T) {
	spec := "---\n" + fmBase + "---\n## Stories\n\n### S-01 Foo\n\nSome text\n"
	sp, cp := writeFiles(t, spec, conRequirePri)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if ruleCounts(fs)["require_priority"] != 1 {
		t.Fatalf("expected require_priority finding, got %+v", fs)
	}
}

func TestLint_RequirePriority_Present(t *testing.T) {
	spec := "---\n" + fmBase + "---\n## Stories\n\n### S-01 Foo\n\nPriority: P2\n"
	sp, cp := writeFiles(t, spec, conRequirePri)
	fs, err := Lint(sp, cp)
	if err != nil {
		t.Fatal(err)
	}
	if ruleCounts(fs)["require_priority"] != 0 {
		t.Fatalf("expected no finding, got %+v", fs)
	}
}

func TestLint_NoConstitution_NoFindings(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "spec.md")
	_ = os.WriteFile(sp, []byte("---\n"+fmBase+"---\n# Spec\n"), 0o644)
	fs, err := Lint(sp, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected no findings, got %+v", fs)
	}
}

func TestLint_ParseError_SurfacedAsFinding(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "spec.md")
	_ = os.WriteFile(sp, []byte("no frontmatter here\n"), 0o644)
	fs, err := Lint(sp, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Rule != "spec_parse" {
		t.Fatalf("expected single spec_parse finding, got %+v", fs)
	}
}
