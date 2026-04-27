package persona

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

func TestResolveSurface_CwdAlone(t *testing.T) {
	s := ResolveSurface(SurfaceRequest{Cwd: "/work/repo"})
	if s.Cwd != "/work/repo" {
		t.Errorf("Cwd = %q, want /work/repo", s.Cwd)
	}
	if len(s.SiblingReads) != 0 {
		t.Errorf("SiblingReads = %v, want empty", s.SiblingReads)
	}
	if len(s.ToolingPaths) == 0 {
		t.Error("default tooling paths should be populated")
	}
}

func TestResolveSurface_SiblingReadsExcludeOwnRepo(t *testing.T) {
	reg := core.NewRepositoryRegistry()
	for _, r := range []*core.Repository{
		{ID: "frontend", Path: "/repos/fe", DefaultBranch: "main", BeadPrefix: "fe"},
		{ID: "backend", Path: "/repos/be", DefaultBranch: "main", BeadPrefix: "be"},
		{ID: "infra", Path: "/repos/in", DefaultBranch: "main", BeadPrefix: "in"},
	} {
		_ = reg.Register(r)
	}
	s := ResolveSurface(SurfaceRequest{
		Cwd:          "/repos/fe",
		Repositories: reg,
	})
	want := map[string]bool{"/repos/be": true, "/repos/in": true}
	for _, p := range s.SiblingReads {
		if !want[p] {
			t.Errorf("unexpected sibling: %q", p)
		}
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("missing siblings: %v", want)
	}
	for _, p := range s.SiblingReads {
		if p == "/repos/fe" {
			t.Error("cwd should not appear in SiblingReads")
		}
	}
}

func TestResolveSurface_WorkspaceMetadata(t *testing.T) {
	s := ResolveSurface(SurfaceRequest{
		Cwd:          "/work/repo",
		WorkspaceDir: "/work",
	})
	if s.WorkspaceMetadata != "/work/.gemba" {
		t.Errorf("WorkspaceMetadata = %q, want /work/.gemba", s.WorkspaceMetadata)
	}
}

func TestResolveSurface_BeadAdditionalPaths(t *testing.T) {
	bead := &core.WorkItem{
		AdditionalReadPaths:  []string{"/notes/spec.md"},
		AdditionalWritePaths: []string{"/scratch"},
	}
	s := ResolveSurface(SurfaceRequest{
		Cwd:  "/work/repo",
		Bead: bead,
	})
	if len(s.AdditionalReads) != 1 || s.AdditionalReads[0] != "/notes/spec.md" {
		t.Errorf("AdditionalReads = %v", s.AdditionalReads)
	}
	if len(s.AdditionalWrites) != 1 || s.AdditionalWrites[0] != "/scratch" {
		t.Errorf("AdditionalWrites = %v", s.AdditionalWrites)
	}
}

func TestResolveSurface_PersonaAdditionalReadPathsMerge(t *testing.T) {
	bead := &core.WorkItem{
		AdditionalReadPaths: []string{"/bead-extra"},
	}
	persona := &corepersona.Persona{
		Scope: corepersona.PersonaScope{
			Kind:                corepersona.ScopeProject,
			AdditionalReadPaths: []string{"/persona-extra", "/bead-extra"}, // dup
		},
	}
	s := ResolveSurface(SurfaceRequest{
		Cwd:     "/work",
		Bead:    bead,
		Persona: persona,
	})
	want := []string{"/bead-extra", "/persona-extra"}
	if !reflect.DeepEqual(s.AdditionalReads, want) {
		t.Errorf("AdditionalReads = %v, want %v", s.AdditionalReads, want)
	}
}

func TestResolveSurface_ToolingPathsOverride(t *testing.T) {
	custom := []string{"$HOME/.foo", "$HOME/.bar"}
	s := ResolveSurface(SurfaceRequest{
		Cwd:          "/work",
		ToolingPaths: custom,
	})
	if !reflect.DeepEqual(s.ToolingPaths, custom) {
		t.Errorf("override not honored: %v", s.ToolingPaths)
	}
}

func TestSurface_AllowedReadsUnionDeduplicated(t *testing.T) {
	reg := core.NewRepositoryRegistry()
	_ = reg.Register(&core.Repository{
		ID: "be", Path: "/repos/be", DefaultBranch: "main", BeadPrefix: "be",
	})
	bead := &core.WorkItem{
		AdditionalReadPaths: []string{"/repos/be"}, // dup with sibling
	}
	s := ResolveSurface(SurfaceRequest{
		Cwd:          "/repos/fe",
		WorkspaceDir: "/work",
		Repositories: reg,
		Bead:         bead,
		ToolingPaths: []string{"$HOME/.gitconfig"},
	})
	got := s.AllowedReads()
	// Expect: cwd + sibling + workspace metadata + tooling. dup
	// "/repos/be" appears once.
	// AllowedReads sorts ascending; "$" (0x24) sorts before "/" (0x2F).
	must := []string{
		"$HOME/.gitconfig",
		"/repos/be",
		"/repos/fe",
		"/work/.gemba",
	}
	if !reflect.DeepEqual(got, must) {
		t.Errorf("AllowedReads = %v, want %v", got, must)
	}
}

func TestSurface_AllowedWritesNeverIncludesSiblingsOrTooling(t *testing.T) {
	reg := core.NewRepositoryRegistry()
	_ = reg.Register(&core.Repository{
		ID: "be", Path: "/repos/be", DefaultBranch: "main", BeadPrefix: "be",
	})
	bead := &core.WorkItem{
		AdditionalWritePaths: []string{"/scratch"},
	}
	s := ResolveSurface(SurfaceRequest{
		Cwd:          "/repos/fe",
		Repositories: reg,
		Bead:         bead,
	})
	got := s.AllowedWrites()
	want := []string{"/repos/fe", "/scratch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedWrites = %v, want %v", got, want)
	}
}

func TestAppendUnique(t *testing.T) {
	got := appendUnique([]string{"a"}, "b", "a", "", "c", "b")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDefaultToolingPaths_NotEmpty(t *testing.T) {
	if len(DefaultToolingPaths) == 0 {
		t.Error("default tooling whitelist must not be empty")
	}
	// Spot-check load-bearing entries.
	have := make(map[string]bool, len(DefaultToolingPaths))
	for _, p := range DefaultToolingPaths {
		have[p] = true
	}
	for _, must := range []string{"$HOME/.gitconfig", "$HOME/go/pkg/mod"} {
		if !have[must] {
			t.Errorf("default tooling missing %q", must)
		}
	}
}

// gm-eazw — AllowsRead / AllowsWrite Layer-2 enforcement decisions.

func testEnv(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestSurface_AllowsWrite_HappyPath(t *testing.T) {
	s := Surface{
		Cwd:              "/work/repo",
		AdditionalWrites: []string{"/scratch"},
	}
	for _, p := range []string{
		"/work/repo",
		"/work/repo/main.go",
		"/work/repo/sub/dir/file.txt",
		"/scratch",
		"/scratch/notes.md",
	} {
		ok, reason := s.AllowsWrite(p, nil)
		if !ok {
			t.Errorf("AllowsWrite(%q) = (false, %q), want allow", p, reason)
		}
	}
}

func TestSurface_AllowsWrite_RejectsOutsideCwd(t *testing.T) {
	s := Surface{Cwd: "/work/repo"}
	for _, p := range []string{
		"/work/other",
		"/etc/passwd",
		"/Users/mike/.aws/credentials",
		"/work/repobackup", // false-prefix — must NOT match cwd
	} {
		ok, reason := s.AllowsWrite(p, nil)
		if ok {
			t.Errorf("AllowsWrite(%q) = allow, want deny", p)
		}
		if reason == "" {
			t.Errorf("AllowsWrite(%q) deny but empty reason", p)
		}
	}
}

func TestSurface_AllowsRead_SiblingReadsAllowed(t *testing.T) {
	s := Surface{
		Cwd:          "/work/repo",
		SiblingReads: []string{"/repos/sib"},
	}
	ok, _ := s.AllowsRead("/repos/sib/contracts/api.proto", nil)
	if !ok {
		t.Error("expected sibling-repo file to be readable")
	}
	// Same path should NOT be writable.
	if w, _ := s.AllowsWrite("/repos/sib/contracts/api.proto", nil); w {
		t.Error("sibling-repo file should not be writable")
	}
}

func TestSurface_AllowsRead_ToolingPathsHonored(t *testing.T) {
	s := Surface{
		Cwd:          "/work/repo",
		ToolingPaths: []string{"$HOME/.gitconfig", "$HOME/go/pkg/mod"},
	}
	env := testEnv(map[string]string{"HOME": "/Users/mike"})
	for _, p := range []string{
		"/Users/mike/.gitconfig",
		"/Users/mike/go/pkg/mod/github.com/foo/bar@v1.2.3/x.go",
	} {
		if ok, reason := s.AllowsRead(p, env); !ok {
			t.Errorf("AllowsRead(%q) = (false, %q), want allow", p, reason)
		}
	}
	// Without env expansion, the literal pattern wouldn't match.
	if ok, _ := s.AllowsRead("/Users/mike/.gitconfig", nil); ok {
		t.Error("expected nil env to skip $HOME expansion → deny")
	}
}

func TestSurface_AllowsRead_AdditionalReadsHonored(t *testing.T) {
	s := Surface{
		Cwd:             "/work/repo",
		AdditionalReads: []string{"/notes/spec.md"},
	}
	if ok, _ := s.AllowsRead("/notes/spec.md", nil); !ok {
		t.Error("expected AdditionalReads to allow exact match")
	}
	if ok, _ := s.AllowsRead("/notes/other.md", nil); ok {
		t.Error("AdditionalReads should not allow unrelated sibling")
	}
}

func TestSurface_AllowsRead_EmptyPathDenied(t *testing.T) {
	s := Surface{Cwd: "/work/repo"}
	if ok, _ := s.AllowsRead("", nil); ok {
		t.Error("empty path must always deny")
	}
}

func TestSurface_DenyReason_NamesWriteSurface(t *testing.T) {
	s := Surface{
		Cwd:              "/work/repo",
		AdditionalWrites: []string{"/scratch"},
	}
	_, reason := s.AllowsWrite("/etc/passwd", nil)
	for _, want := range []string{"/etc/passwd", "/work/repo", "/scratch", "write"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason missing %q: got %q", want, reason)
		}
	}
}
