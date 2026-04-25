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
bead_prefix = "gm"
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
		BeadPrefix:    "gm",
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
			r := Repository{ID: "gemba", Path: "/abs", DefaultBranch: "main", BeadPrefix: "gm"}
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
	r := &Repository{ID: "gemba", Path: "/abs", DefaultBranch: "main", BeadPrefix: "gm"}
	if err := reg.Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Idempotent re-register of same pointer.
	if err := reg.Register(r); err != nil {
		t.Errorf("idempotent re-register: %v", err)
	}
	// Conflicting register on different pointer.
	other := &Repository{ID: "gemba", Path: "/abs2", DefaultBranch: "main", BeadPrefix: "gm"}
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
	bad := &Repository{ID: "gemba", Path: "relative", DefaultBranch: "main", BeadPrefix: "gm"}
	if err := reg.Register(bad); err == nil {
		t.Error("expected validation error on relative path")
	}
}

func TestRepositoryRegistry_ListAscending(t *testing.T) {
	reg := NewRepositoryRegistry()
	for _, id := range []string{"infra", "frontend", "gemba"} {
		_ = reg.Register(&Repository{ID: RepositoryID(id), Path: "/abs/" + id, DefaultBranch: "main", BeadPrefix: id[:2]})
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
	infraTOML := minimalRepoTOML
	infraTOML = strings.ReplaceAll(infraTOML, `"gemba"`, `"infra"`)
	infraTOML = strings.ReplaceAll(infraTOML, `/tmp/repos/gemba`, `/tmp/repos/infra`)
	infraTOML = strings.ReplaceAll(infraTOML, `bead_prefix = "gm"`, `bead_prefix = "in"`)
	writeRepoFile(t, dir, "infra.toml", infraTOML)

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
		ID:                  "gemba/gemba/gm-e3",
		PrimaryRepositoryID: "gemba",
		RepositoryIDs:       []RepositoryID{"gemba"},
		Kind:                "epic",
		Title:               "Plan view",
		Status:              "open",
		StateCategory:       StateUnstarted,
	}
	body, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"primary_repository_id":"gemba"`) {
		t.Errorf("primary_repository_id not in JSON: %s", body)
	}
	if !strings.Contains(string(body), `"repository_ids":["gemba"]`) {
		t.Errorf("repository_ids not in JSON: %s", body)
	}

	var got WorkItem
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PrimaryRepositoryID != "gemba" {
		t.Errorf("PrimaryRepositoryID = %q, want gemba", got.PrimaryRepositoryID)
	}
	if got.RepositoryID() != "gemba" {
		t.Errorf("RepositoryID() = %q, want gemba", got.RepositoryID())
	}
}

func TestWorkItem_OmitemptyRepositoryFields(t *testing.T) {
	// Existing beads with no repository fields round-trip with both
	// keys omitted (omitempty) so wire payloads stay clean for legacy
	// records.
	wi := WorkItem{
		ID:            "gemba/gemba/gm-e3",
		Kind:          "epic",
		Title:         "x",
		Status:        "open",
		StateCategory: StateUnstarted,
	}
	body, _ := json.Marshal(wi)
	for _, key := range []string{"primary_repository_id", "repository_ids"} {
		if strings.Contains(string(body), key) {
			t.Errorf("empty %s should be omitted: %s", key, body)
		}
	}
}

// gm-kdh3: WorkItem.RepositoryID() returns the primary, equivalent
// to the field accessor.
func TestWorkItem_RepositoryIDMethodReturnsPrimary(t *testing.T) {
	wi := WorkItem{PrimaryRepositoryID: "gemba"}
	if got := wi.RepositoryID(); got != "gemba" {
		t.Errorf("RepositoryID() = %q, want gemba", got)
	}
}

// gm-kdh3: NormalizeRepositories auto-promotes a sole primary into a
// single-element RepositoryIDs slice (back-compat with gm-26n4).
func TestWorkItem_NormalizePromotesSolePrimary(t *testing.T) {
	wi := WorkItem{PrimaryRepositoryID: "gemba"}
	wi.NormalizeRepositories()
	if len(wi.RepositoryIDs) != 1 || wi.RepositoryIDs[0] != "gemba" {
		t.Errorf("expected RepositoryIDs=[gemba], got %v", wi.RepositoryIDs)
	}
}

// Normalize is idempotent — running twice does not double the slice.
func TestWorkItem_NormalizeIdempotent(t *testing.T) {
	wi := WorkItem{PrimaryRepositoryID: "gemba"}
	wi.NormalizeRepositories()
	wi.NormalizeRepositories()
	if len(wi.RepositoryIDs) != 1 {
		t.Errorf("idempotent: got %d entries, want 1", len(wi.RepositoryIDs))
	}
}

// Normalize leaves a multi-repo bead untouched.
func TestWorkItem_NormalizeMultiRepoUntouched(t *testing.T) {
	wi := WorkItem{
		PrimaryRepositoryID: "frontend",
		RepositoryIDs:       []RepositoryID{"frontend", "backend"},
	}
	wi.NormalizeRepositories()
	if len(wi.RepositoryIDs) != 2 {
		t.Errorf("normalize mutated multi-repo bead: %v", wi.RepositoryIDs)
	}
}

// gm-kdh3: ValidateRepositories accepts the legacy zero-state — bead
// has no repository info yet (the spawn path rejects later).
func TestWorkItem_ValidateLegacyZeroState(t *testing.T) {
	wi := WorkItem{ID: "gm-1"}
	if err := wi.ValidateRepositories(); err != nil {
		t.Errorf("zero state should be valid: %v", err)
	}
}

func TestWorkItem_ValidateRejects(t *testing.T) {
	cases := []struct {
		name    string
		wi      WorkItem
		wantSub string
	}{
		{
			name: "ids without primary",
			wi: WorkItem{
				ID:            "gm-1",
				RepositoryIDs: []RepositoryID{"frontend"},
			},
			wantSub: "primary_repository_id is unset",
		},
		{
			name: "primary not in ids",
			wi: WorkItem{
				ID:                  "gm-1",
				PrimaryRepositoryID: "ghost",
				RepositoryIDs:       []RepositoryID{"frontend", "backend"},
			},
			wantSub: "not present in repository_ids",
		},
		{
			name: "duplicate id",
			wi: WorkItem{
				ID:                  "gm-1",
				PrimaryRepositoryID: "frontend",
				RepositoryIDs:       []RepositoryID{"frontend", "backend", "frontend"},
			},
			wantSub: "duplicate",
		},
		{
			name: "empty entry",
			wi: WorkItem{
				ID:                  "gm-1",
				PrimaryRepositoryID: "frontend",
				RepositoryIDs:       []RepositoryID{"frontend", ""},
			},
			wantSub: "empty entry",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.wi.ValidateRepositories()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

// Single-repo bead (post-normalize) validates cleanly.
func TestWorkItem_ValidateSingleRepoOK(t *testing.T) {
	wi := WorkItem{
		ID:                  "gm-1",
		PrimaryRepositoryID: "gemba",
		RepositoryIDs:       []RepositoryID{"gemba"},
	}
	if err := wi.ValidateRepositories(); err != nil {
		t.Errorf("single-repo bead should be valid: %v", err)
	}
}

// Multi-repo bead validates when primary is in the list.
func TestWorkItem_ValidateMultiRepoOK(t *testing.T) {
	wi := WorkItem{
		ID:                  "gm-2",
		PrimaryRepositoryID: "backend",
		RepositoryIDs:       []RepositoryID{"frontend", "backend"},
	}
	if err := wi.ValidateRepositories(); err != nil {
		t.Errorf("multi-repo bead should be valid: %v", err)
	}
}

// gm-ou02: ValidateBranches accepts the legacy zero-state.
func TestWorkItem_ValidateBranchesEmpty(t *testing.T) {
	wi := WorkItem{ID: "gm-1"}
	if err := wi.ValidateBranches(); err != nil {
		t.Errorf("empty branches should be valid: %v", err)
	}
}

// Single-repo bead with one branch entry validates cleanly.
func TestWorkItem_ValidateBranchesSingleRepoOK(t *testing.T) {
	wi := WorkItem{
		ID:                  "gm-1",
		PrimaryRepositoryID: "gemba",
		RepositoryIDs:       []RepositoryID{"gemba"},
		Branches: []BeadBranch{
			{RepositoryID: "gemba", Branch: "feature/gm-e3"},
		},
	}
	if err := wi.ValidateBranches(); err != nil {
		t.Errorf("single-branch bead should be valid: %v", err)
	}
}

// Multi-repo bead with one branch per repo validates cleanly.
func TestWorkItem_ValidateBranchesMultiRepoOK(t *testing.T) {
	wi := WorkItem{
		ID:                  "gm-2",
		PrimaryRepositoryID: "backend",
		RepositoryIDs:       []RepositoryID{"frontend", "backend"},
		Branches: []BeadBranch{
			{RepositoryID: "backend", Branch: "feature/x"},
			{RepositoryID: "frontend", Branch: "feature/x-client"},
		},
	}
	if err := wi.ValidateBranches(); err != nil {
		t.Errorf("multi-branch bead should be valid: %v", err)
	}
}

// Multi-repo bead may declare a branch for a subset of repos — the
// spawn path derives the rest. Validation accepts.
func TestWorkItem_ValidateBranchesPartial(t *testing.T) {
	wi := WorkItem{
		ID:                  "gm-3",
		PrimaryRepositoryID: "backend",
		RepositoryIDs:       []RepositoryID{"frontend", "backend"},
		Branches: []BeadBranch{
			{RepositoryID: "backend", Branch: "feature/x"},
		},
	}
	if err := wi.ValidateBranches(); err != nil {
		t.Errorf("partial branch coverage should be valid: %v", err)
	}
}

func TestWorkItem_ValidateBranchesRejects(t *testing.T) {
	cases := []struct {
		name    string
		wi      WorkItem
		wantSub string
	}{
		{
			name: "empty repository_id",
			wi: WorkItem{
				ID:            "gm-1",
				RepositoryIDs: []RepositoryID{"frontend"},
				Branches:      []BeadBranch{{RepositoryID: "", Branch: "x"}},
			},
			wantSub: "empty repository_id",
		},
		{
			name: "empty branch string",
			wi: WorkItem{
				ID:            "gm-1",
				RepositoryIDs: []RepositoryID{"frontend"},
				Branches:      []BeadBranch{{RepositoryID: "frontend", Branch: ""}},
			},
			wantSub: "branch must not be empty",
		},
		{
			name: "duplicate repository_id",
			wi: WorkItem{
				ID:            "gm-1",
				RepositoryIDs: []RepositoryID{"frontend"},
				Branches: []BeadBranch{
					{RepositoryID: "frontend", Branch: "a"},
					{RepositoryID: "frontend", Branch: "b"},
				},
			},
			wantSub: "duplicate repository_id",
		},
		{
			name: "branch references unknown repo",
			wi: WorkItem{
				ID:            "gm-1",
				RepositoryIDs: []RepositoryID{"frontend"},
				Branches:      []BeadBranch{{RepositoryID: "ghost", Branch: "x"}},
			},
			wantSub: "not in repository_ids",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.wi.ValidateBranches()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

// BranchFor accessor returns the recorded branch + true; missing
// returns "" + false.
func TestWorkItem_BranchFor(t *testing.T) {
	wi := WorkItem{
		Branches: []BeadBranch{{RepositoryID: "gemba", Branch: "feature/x"}},
	}
	if got, ok := wi.BranchFor("gemba"); !ok || got != "feature/x" {
		t.Errorf("BranchFor(gemba) = (%q, %v); want (feature/x, true)", got, ok)
	}
	if got, ok := wi.BranchFor("ghost"); ok || got != "" {
		t.Errorf("BranchFor(ghost) = (%q, %v); want empty/false", got, ok)
	}
}

// Branches round-trip through JSON.
func TestWorkItem_BranchesJSONRoundtrip(t *testing.T) {
	wi := WorkItem{
		ID:                  "gm-1",
		PrimaryRepositoryID: "gemba",
		RepositoryIDs:       []RepositoryID{"gemba"},
		Branches:            []BeadBranch{{RepositoryID: "gemba", Branch: "feature/x"}},
		Kind:                "epic",
		Title:               "x",
		Status:              "open",
		StateCategory:       StateUnstarted,
	}
	body, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"branches":[{"repository_id":"gemba","branch":"feature/x"}]`) {
		t.Errorf("branches missing or wrong shape: %s", body)
	}
	var got WorkItem
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Branches) != 1 || got.Branches[0].Branch != "feature/x" {
		t.Errorf("Branches lost: %+v", got.Branches)
	}
}

// Empty Branches is omitted from JSON.
func TestWorkItem_OmitemptyBranches(t *testing.T) {
	wi := WorkItem{
		ID:            "gm-1",
		Kind:          "epic",
		Title:         "x",
		Status:        "open",
		StateCategory: StateUnstarted,
	}
	body, _ := json.Marshal(wi)
	if strings.Contains(string(body), "branches") {
		t.Errorf("empty branches should be omitted: %s", body)
	}
}

// gm-d2ts: BeadPrefix shape rules.
func TestRepository_ValidateBeadPrefix(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		wantSub string // "" = expect nil
	}{
		{"empty", "", "must not be empty"},
		{"too short", "g", "2-8"},
		{"too long", "abcdefghi", "2-8"},
		{"uppercase rejected", "Gm", "invalid character"},
		{"underscore rejected", "g_m", "invalid character"},
		{"slash rejected", "g/m", "invalid character"},
		{"valid 2-char", "gm", ""},
		{"valid with digit", "g1", ""},
		{"valid with hyphen", "g-m", ""},
		{"valid 8-char", "frontend", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Repository{
				ID:            "x",
				Path:          "/abs",
				DefaultBranch: "main",
				BeadPrefix:    c.prefix,
			}
			err := r.Validate()
			if c.wantSub == "" {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

// Registering two repositories that share a prefix is an error —
// uniqueness is the load-bearing property of per-repo prefixes.
func TestRepositoryRegistry_RejectsDuplicatePrefix(t *testing.T) {
	reg := NewRepositoryRegistry()
	a := &Repository{ID: "frontend", Path: "/abs/fe", DefaultBranch: "main", BeadPrefix: "fe"}
	b := &Repository{ID: "feed", Path: "/abs/feed", DefaultBranch: "main", BeadPrefix: "fe"}
	if err := reg.Register(a); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := reg.Register(b)
	if err == nil {
		t.Fatal("expected duplicate-prefix error")
	}
	if !strings.Contains(err.Error(), `bead_prefix "fe" already claimed`) {
		t.Errorf("err = %v, want duplicate-prefix detail", err)
	}
}

// GetByPrefix routes to the right repository.
func TestRepositoryRegistry_GetByPrefix(t *testing.T) {
	reg := NewRepositoryRegistry()
	for _, r := range []*Repository{
		{ID: "frontend", Path: "/abs/fe", DefaultBranch: "main", BeadPrefix: "fe"},
		{ID: "backend", Path: "/abs/be", DefaultBranch: "main", BeadPrefix: "be"},
	} {
		if err := reg.Register(r); err != nil {
			t.Fatalf("Register %q: %v", r.ID, err)
		}
	}
	got, ok := reg.GetByPrefix("fe")
	if !ok || got.ID != "frontend" {
		t.Errorf("GetByPrefix(fe) = (%v, %v), want frontend/true", got, ok)
	}
	if _, ok := reg.GetByPrefix("ghost"); ok {
		t.Error("GetByPrefix(ghost) should report false")
	}
}

// LoadRepositoryRegistry returns a clear error when two TOML files
// declare the same prefix.
func TestLoadRepositoryRegistry_RejectsDuplicatePrefix(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "alpha.toml", `id = "alpha"
path = "/abs/alpha"
default_branch = "main"
bead_prefix = "fe"
`)
	writeRepoFile(t, dir, "beta.toml", `id = "beta"
path = "/abs/beta"
default_branch = "main"
bead_prefix = "fe"
`)
	_, err := LoadRepositoryRegistry(dir)
	if err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Errorf("err = %v, want duplicate-prefix error", err)
	}
}

func TestRepositoryUnspecified_RejectedByValidate(t *testing.T) {
	r := Repository{ID: RepositoryUnspecified, Path: "/abs", DefaultBranch: "main", BeadPrefix: "gm"}
	if err := r.Validate(); err == nil {
		t.Error("RepositoryUnspecified should fail Validate")
	}
}
