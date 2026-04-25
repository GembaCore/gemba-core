package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalTOML = `
id = "tester"
name = "Test"
role = "Tester"
description = "fixture"
system_prompt = "You are a test."

[model]
vendor = "anthropic"
model = "claude-haiku-4-5"

[budget_policy]
counts_against_sprint = false
`

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadFile_MinimalDefaultsToCoach(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML)

	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.ID != "tester" {
		t.Errorf("id = %q, want tester", p.ID)
	}
	if p.Variety != VarietyCoach {
		t.Errorf("variety = %q, want %q (default)", p.Variety, VarietyCoach)
	}
	if p.Model.Vendor != "anthropic" || p.Model.Model != "claude-haiku-4-5" {
		t.Errorf("model = %+v, want anthropic/claude-haiku-4-5", p.Model)
	}
}

func TestLoadFile_RejectsFilenameMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "wrong-name.toml", minimalTOML)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for filename/id mismatch")
	}
	if !strings.Contains(err.Error(), "does not match filename stem") {
		t.Errorf("error = %v, want filename mismatch", err)
	}
}

func TestLoadFile_RejectsMissingRequired(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(s string) string
		wantSub string
	}{
		{
			name:    "missing model.vendor",
			mutate:  func(s string) string { return strings.ReplaceAll(s, `vendor = "anthropic"`, `vendor = ""`) },
			wantSub: "model.vendor",
		},
		{
			name:    "missing system_prompt",
			mutate:  func(s string) string { return strings.ReplaceAll(s, `system_prompt = "You are a test."`, `system_prompt = ""`) },
			wantSub: "system_prompt",
		},
		{
			name:    "missing role",
			mutate:  func(s string) string { return strings.ReplaceAll(s, `role = "Tester"`, `role = ""`) },
			wantSub: "role",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "tester.toml", tc.mutate(minimalTOML))
			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestVariety_ManagerRequiresMutationAuthority(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(minimalTOML, `description = "fixture"`,
		`description = "fixture"
variety = "manager"`, 1)
	path := writeFile(t, dir, "tester.toml", body)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: manager without mutation_authority")
	}
	if !strings.Contains(err.Error(), "mutation_authority") {
		t.Errorf("error = %v, want mention of mutation_authority", err)
	}
}

func TestVariety_CoachRejectsMutationAuthority(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(minimalTOML, `description = "fixture"`,
		`description = "fixture"
mutation_authority = ["docs_edit"]`, 1)
	path := writeFile(t, dir, "tester.toml", body)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: coach with mutation_authority")
	}
}

func TestLoadDir_SkipsNonTOMLAndReturnsEmptyOnMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tester.toml", minimalTOML)
	writeFile(t, dir, "README.md", "ignore me")

	got, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(got) != 1 || got["tester"] == nil {
		t.Fatalf("got %d personas; want 1 (tester)", len(got))
	}

	missing, err := LoadDir(filepath.Join(dir, "nope"))
	if err != nil {
		t.Errorf("missing dir should not error: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing dir should yield 0 personas, got %d", len(missing))
	}
}

func TestLoadDir_RejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	dup := strings.ReplaceAll(minimalTOML, `id = "tester"`, `id = "tester"`)
	writeFile(t, dir, "tester.toml", minimalTOML)
	// Force a second file whose stem matches its declared id but
	// collides on the id field of the first file.
	collide := strings.ReplaceAll(dup, `id = "tester"`, `id = "duped"`)
	collide = strings.ReplaceAll(collide, `id = "duped"`, `id = "tester"`)
	writeFile(t, dir, "duped.toml", collide)

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected duplicate-id error")
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	p := &Persona{
		ID:           "tester",
		Name:         "Test",
		Role:         "Tester",
		Variety:      VarietyCoach,
		Model:        ModelConfig{Vendor: "anthropic", Model: "claude-haiku-4-5"},
		SystemPrompt: "You are a test.",
	}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(p); err != nil {
		t.Errorf("idempotent re-register of same pointer: %v", err)
	}

	other := &Persona{ID: "tester"}
	if err := r.Register(other); err == nil {
		t.Error("expected duplicate-id error on different pointer")
	}

	got, ok := r.Get("tester")
	if !ok || got != p {
		t.Errorf("Get returned (%v, %v); want (%p, true)", got, ok, p)
	}

	if list := r.List(); len(list) != 1 || list[0] != "tester" {
		t.Errorf("List = %v, want [tester]", list)
	}
}

func TestLoadRegistry_SeedFiles(t *testing.T) {
	// The repo ships seed personas under .gemba/personas/. They MUST
	// parse successfully — a malformed seed file would break server
	// startup. Tests run from the package directory, so walk back up.
	r, err := LoadRegistry("../../../.gemba/personas")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	want := []string{"documentarian", "project-manager"}
	for _, id := range want {
		p, ok := r.Get(id)
		if !ok {
			t.Errorf("seed persona %q missing", id)
			continue
		}
		if p.ID != id {
			t.Errorf("seed %q has id %q", id, p.ID)
		}
		if len(p.Skills) == 0 {
			t.Errorf("seed %q declares no skills", id)
		}
	}

	pm, _ := r.Get("project-manager")
	if pm.Variety != VarietyCoach {
		t.Errorf("project-manager variety = %q, want coach", pm.Variety)
	}
	if got := pm.Model.Vendor; got != "anthropic" {
		t.Errorf("project-manager model.vendor = %q, want anthropic", got)
	}
	if len(pm.Skills) != 1 || pm.Skills[0] != "epic_order" {
		t.Errorf("project-manager skills = %v, want [epic_order]", pm.Skills)
	}
}

func TestLoadRegistry_SourcePath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML)

	r, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if got := r.SourcePath("tester"); got != path {
		t.Errorf("SourcePath = %q, want %q", got, path)
	}
	if got := r.SourcePath("missing"); got != "" {
		t.Errorf("missing SourcePath = %q, want \"\"", got)
	}
}
