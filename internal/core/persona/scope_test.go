package persona

import (
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

func newRepoRegistry(t *testing.T, repos ...*core.Repository) *core.RepositoryRegistry {
	t.Helper()
	reg := core.NewRepositoryRegistry()
	for _, r := range repos {
		if err := reg.Register(r); err != nil {
			t.Fatalf("register %q: %v", r.ID, err)
		}
	}
	return reg
}

func TestPersonaScope_IsZero(t *testing.T) {
	if !(PersonaScope{}).IsZero() {
		t.Error("zero scope should report IsZero")
	}
	if (PersonaScope{Kind: ScopeProject}).IsZero() {
		t.Error("scope with Kind set should not be zero")
	}
}

func TestPersonaScope_ValidateAccepts(t *testing.T) {
	cases := []PersonaScope{
		{Kind: ScopeProject},
		{Kind: ScopeAny},
		{Kind: ScopeRepository, RepositoryID: "frontend"},
	}
	for _, s := range cases {
		if err := s.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", s, err)
		}
	}
}

func TestPersonaScope_ValidateRejects(t *testing.T) {
	cases := []struct {
		name    string
		scope   PersonaScope
		wantSub string
	}{
		{
			name:    "missing kind",
			scope:   PersonaScope{},
			wantSub: "scope.kind is required",
		},
		{
			name:    "unknown kind",
			scope:   PersonaScope{Kind: "global"},
			wantSub: "unknown scope.kind",
		},
		{
			name:    "repository kind missing repo",
			scope:   PersonaScope{Kind: ScopeRepository},
			wantSub: "scope.repository required",
		},
		{
			name:    "project kind with repo",
			scope:   PersonaScope{Kind: ScopeProject, RepositoryID: "x"},
			wantSub: "scope.repository must be empty",
		},
		{
			name:    "any kind with repo",
			scope:   PersonaScope{Kind: ScopeAny, RepositoryID: "x"},
			wantSub: "scope.repository must be empty",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.scope.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestResolveWorkingDir_Project(t *testing.T) {
	reg := newRepoRegistry(t)
	got, err := PersonaScope{Kind: ScopeProject}.ResolveWorkingDir("/work/gemba", reg, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "/work/gemba" {
		t.Errorf("got %q, want /work/gemba", got)
	}
}

func TestResolveWorkingDir_Repository(t *testing.T) {
	reg := newRepoRegistry(t,
		&core.Repository{ID: "frontend", Path: "/repos/frontend", DefaultBranch: "main", BeadPrefix: "fe"},
		&core.Repository{ID: "backend", Path: "/repos/backend", DefaultBranch: "main", BeadPrefix: "be"},
	)
	got, err := PersonaScope{Kind: ScopeRepository, RepositoryID: "frontend"}.ResolveWorkingDir("/work/gemba", reg, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "/repos/frontend" {
		t.Errorf("got %q, want /repos/frontend", got)
	}
}

func TestResolveWorkingDir_RepositoryMissingFromRegistry(t *testing.T) {
	reg := newRepoRegistry(t)
	_, err := PersonaScope{Kind: ScopeRepository, RepositoryID: "ghost"}.ResolveWorkingDir("/work", reg, "")
	if err == nil || !strings.Contains(err.Error(), `repository "ghost" not registered`) {
		t.Errorf("err = %v, want unregistered-repo error", err)
	}
}

func TestResolveWorkingDir_AnyUsesOverride(t *testing.T) {
	reg := newRepoRegistry(t,
		&core.Repository{ID: "frontend", Path: "/repos/frontend", DefaultBranch: "main", BeadPrefix: "fe"},
	)
	got, err := PersonaScope{Kind: ScopeAny}.ResolveWorkingDir("/work", reg, "frontend")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "/repos/frontend" {
		t.Errorf("got %q, want /repos/frontend", got)
	}
}

func TestResolveWorkingDir_AnyRequiresOverride(t *testing.T) {
	reg := newRepoRegistry(t)
	_, err := PersonaScope{Kind: ScopeAny}.ResolveWorkingDir("/work", reg, "")
	if err == nil || !strings.Contains(err.Error(), "requires a repository override") {
		t.Errorf("err = %v, want override-required error", err)
	}
}

func TestResolveWorkingDir_AnyOverrideMustBeRegistered(t *testing.T) {
	reg := newRepoRegistry(t)
	_, err := PersonaScope{Kind: ScopeAny}.ResolveWorkingDir("/work", reg, "ghost")
	if err == nil || !strings.Contains(err.Error(), `repository override "ghost" not registered`) {
		t.Errorf("err = %v, want override-not-registered error", err)
	}
}

func TestResolveWorkingDir_EmptyWorkspaceDirRejected(t *testing.T) {
	reg := newRepoRegistry(t)
	_, err := PersonaScope{Kind: ScopeProject}.ResolveWorkingDir("", reg, "")
	if err == nil || !strings.Contains(err.Error(), "workspaceDir must not be empty") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveWorkingDir_NilRegistryForRepoScope(t *testing.T) {
	_, err := PersonaScope{Kind: ScopeRepository, RepositoryID: "x"}.ResolveWorkingDir("/work", nil, "")
	if err == nil || !strings.Contains(err.Error(), "registry required") {
		t.Errorf("err = %v", err)
	}
}
