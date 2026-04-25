package persona

import (
	"reflect"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
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
