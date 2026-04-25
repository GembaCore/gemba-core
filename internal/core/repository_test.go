package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalRepoTOML = `
id = "gemba"
path = "/tmp/repos/gemba"
default_branch = "main"
url = "https://github.com/MikeBengtson/gemba.git"
`

func writeRepoFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestRepository_ValidateAccepts(t *testing.T) {
	r := Repository{
		ID:            "gemba",
		Path:          "/abs/path",
		DefaultBranch: "main",
	}
	if err := r.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestRepository_ValidateRejects(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Repository)
		wantSub string
	}{
		{"empty id", func(r *Repository) { r.ID = "" }, "id must not be empty"},
		{"reserved id", func(r *Repository) { r.ID = RepositoryUnspecified }, "reserved sentinel"},
		{"id with slash", func(r *Repository) { r.ID = "evil/path" }, "path separators"},
		{"empty path", func(r *Repository) { r.Path = "" }, "path must not be empty"},
		{"relative path", func(r *Repository) { r.Path = "relative/path" }, "must be absolute"},
		{"empty default_branch", func(r *Repository) { r.DefaultBranch = "" }, "default_branch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Repository{ID: "gemba", Path: "/abs", DefaultBranch: "main"}
			c.mutate(&r)
			err := r.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestRepository_ResolveWorktreesDir(t *testing.T) {
	tests := []struct {
		name string
		in   Repository
		want string
	}{
		{
			name: "empty falls back to path",
			in:   Repository{Path: "/repos/gemba"},
			want: "/repos/gemba",
		},
		{
			name: "relative joins",
			in:   Repository{Path: "/repos/gemba", WorktreesDir: ".worktrees"},
			want: "/repos/gemba/.worktrees",
		},
		{
			name: "absolute passes through",
			in:   Repository{Path: "/repos/gemba", WorktreesDir: "/scratch/wts"},
			want: "/scratch/wts",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.ResolveWorktreesDir(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestLoadRepositoryFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := writeRepoFile(t, dir, "gemba.toml", minimalRepoTOML)

	r, err := LoadRepositoryFile(path)
	if err != nil {
		t.Fatalf("LoadRepositoryFile: %v", err)
	}
	if r.ID != "gemba" || r.DefaultBranch != "main" {
		t.Errorf("decoded = %+v", r)
	}
	if r.SourcePath() != path {
		t.Errorf("SourcePath = %q, want %q", r.SourcePath(), path)
	}
}

func TestLoadRepositoryFile_RejectsFilenameMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeRepoFile(t, dir, "wrong-name.toml", minimalRepoTOML)
	_, err := LoadRepositoryFile(path)
	if err == nil {
		t.Fatal("expected error for filename/id mismatch")
	}
	if !strings.Contains(err.Error(), "does not match filename stem") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadRepositoryFile_RejectsRelativePath(t *testing.T) {
	body := strings.ReplaceAll(minimalRepoTOML, `"/tmp/repos/gemba"`, `"relative/path"`)
	dir := t.TempDir()
	path := writeRepoFile(t, dir, "gemba.toml", body)
	_, err := LoadRepositoryFile(path)
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("err = %v, want absolute-path error", err)
	}
}

func TestLoadRepositoriesDir_SkipsNonTOMLAndReturnsEmptyOnMissing(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "gemba.toml", minimalRepoTOML)
	writeRepoFile(t, dir, "README.md", "ignore me")

	got, err := LoadRepositoriesDir(dir)
	if err != nil {
		t.Fatalf("LoadRepositoriesDir: %v", err)
	}
	if len(got) != 1 || got["gemba"] == nil {
		t.Fatalf("got %d repositories; want 1 (gemba)", len(got))
	}

	missing, err := LoadRepositoriesDir(filepath.Join(dir, "nope"))
	if err != nil {
		t.Errorf("missing dir should not error: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing dir should yield 0 repos, got %d", len(missing))
	}
}

func TestLoadRepositoriesDir_RejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "gemba.toml", minimalRepoTOML)
	// Different filename stem but same declared ID.
	dup := strings.ReplaceAll(minimalRepoTOML, `id = "gemba"`, `id = "duped"`)
	dup = strings.ReplaceAll(dup, `id = "duped"`, `id = "gemba"`)
	writeRepoFile(t, dir, "duped.toml", dup)

	_, err := LoadRepositoriesDir(dir)
	if err == nil {
		t.Fatal("expected duplicate-id error")
	}
}

func TestRepositoryRegistry_RegisterAndLookup(t *testing.T) {
	reg := NewRepositoryRegistry()
	r := &Repository{ID: "gemba", Path: "/abs", DefaultBranch: "main"}
	if err := reg.Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Idempotent re-register of same pointer.
	if err := reg.Register(r); err != nil {
		t.Errorf("idempotent re-register: %v", err)
	}
	// Conflicting register on different pointer.
	other := &Repository{ID: "gemba", Path: "/abs2", DefaultBranch: "main"}
	if err := reg.Register(other); err == nil {
		t.Error("expected duplicate-id error on different pointer")
	}

	got, ok := reg.Get("gemba")
	if !ok || got != r {
		t.Errorf("Get returned (%v, %v); want same pointer", got, ok)
	}
}

func TestRepositoryRegistry_RegisterValidates(t *testing.T) {
	reg := NewRepositoryRegistry()
	if err := reg.Register(nil); err == nil {
		t.Error("expected error on nil")
	}
	bad := &Repository{ID: "gemba", Path: "relative", DefaultBranch: "main"}
	if err := reg.Register(bad); err == nil {
		t.Error("expected validation error on relative path")
	}
}

func TestRepositoryRegistry_ListAscending(t *testing.T) {
	reg := NewRepositoryRegistry()
	for _, id := range []string{"infra", "frontend", "gemba"} {
		_ = reg.Register(&Repository{ID: RepositoryID(id), Path: "/abs/" + id, DefaultBranch: "main"})
	}
	got := reg.List()
	want := []RepositoryID{"frontend", "gemba", "infra"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadRepositoryRegistry_PopulatesAll(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "gemba.toml", minimalRepoTOML)
	writeRepoFile(t, dir, "infra.toml",
		strings.ReplaceAll(strings.ReplaceAll(minimalRepoTOML,
			`"gemba"`, `"infra"`),
			`/tmp/repos/gemba`, `/tmp/repos/infra`))

	reg, err := LoadRepositoryRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRepositoryRegistry: %v", err)
	}
	if len(reg.List()) != 2 {
		t.Errorf("got %d repos, want 2", len(reg.List()))
	}
}

func TestWorkItem_RepositoryIDJSONRoundtrip(t *testing.T) {
	wi := WorkItem{
		ID:            "gemba/gemba/gm-e3",
		RepositoryID:  "gemba",
		Kind:          "epic",
		Title:         "Plan view",
		Status:        "open",
		StateCategory: StateUnstarted,
	}
	body, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"repository_id":"gemba"`) {
		t.Errorf("repository_id not in JSON: %s", body)
	}

	var got WorkItem
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RepositoryID != "gemba" {
		t.Errorf("RepositoryID = %q, want gemba", got.RepositoryID)
	}
}

func TestWorkItem_OmitemptyRepositoryID(t *testing.T) {
	// Existing beads with no RepositoryID round-trip with the field
	// omitted (omitempty) so wire payloads stay clean.
	wi := WorkItem{
		ID:            "gemba/gemba/gm-e3",
		Kind:          "epic",
		Title:         "x",
		Status:        "open",
		StateCategory: StateUnstarted,
	}
	body, _ := json.Marshal(wi)
	if strings.Contains(string(body), "repository_id") {
		t.Errorf("empty repository_id should be omitted: %s", body)
	}
}

func TestRepositoryUnspecified_RejectedByValidate(t *testing.T) {
	r := Repository{ID: RepositoryUnspecified, Path: "/abs", DefaultBranch: "main"}
	if err := r.Validate(); err == nil {
		t.Error("RepositoryUnspecified should fail Validate")
	}
}
