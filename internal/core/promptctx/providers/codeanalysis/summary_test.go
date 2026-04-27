package codeanalysis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/codeanalysis"
	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

// stubBackend is a hand-rolled codeanalysis.Provider for tests.
// The four method-specific function fields default to "succeed
// with the zero value" so individual tests only set the bits
// they care about.
type stubBackend struct {
	manifest    func() (codeanalysis.Manifest, error)
	listRepos   func() ([]codeanalysis.RepoRef, error)
	reindex     func(repo codeanalysis.RepoRef, flags codeanalysis.ReindexFlags) error
	modules     func(repo codeanalysis.RepoRef) ([]codeanalysis.Module, error)
	health      func(repo codeanalysis.RepoRef) (codeanalysis.HealthReport, error)
	impact      func(repo codeanalysis.RepoRef, target string, dir codeanalysis.ImpactDirection) (codeanalysis.ImpactReport, error)
}

func (s *stubBackend) Manifest(_ context.Context) (codeanalysis.Manifest, error) {
	if s.manifest != nil {
		return s.manifest()
	}
	return codeanalysis.Manifest{}, nil
}
func (s *stubBackend) ListRepos(_ context.Context) ([]codeanalysis.RepoRef, error) {
	if s.listRepos != nil {
		return s.listRepos()
	}
	return nil, nil
}
func (s *stubBackend) Reindex(_ context.Context, repo codeanalysis.RepoRef, flags codeanalysis.ReindexFlags) error {
	if s.reindex != nil {
		return s.reindex(repo, flags)
	}
	return nil
}
func (s *stubBackend) Modules(_ context.Context, repo codeanalysis.RepoRef) ([]codeanalysis.Module, error) {
	if s.modules != nil {
		return s.modules(repo)
	}
	return nil, nil
}
func (s *stubBackend) Health(_ context.Context, repo codeanalysis.RepoRef) (codeanalysis.HealthReport, error) {
	if s.health != nil {
		return s.health(repo)
	}
	return codeanalysis.HealthReport{Repo: repo}, nil
}
func (s *stubBackend) Impact(_ context.Context, repo codeanalysis.RepoRef, target string, dir codeanalysis.ImpactDirection) (codeanalysis.ImpactReport, error) {
	if s.impact != nil {
		return s.impact(repo, target, dir)
	}
	return codeanalysis.ImpactReport{Target: target, Direction: dir}, nil
}

var testRepo = codeanalysis.RepoRef{Name: "gemba", Path: "/abs/path/gemba"}

func TestSummaryProvider_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		modules     []codeanalysis.Module
		modulesErr  error
		health      codeanalysis.HealthReport
		healthErr   error
		opts        map[string]any
		wantErr     bool
		wantSubs    []string
		wantPayload func(*testing.T, map[string]any)
	}{
		{
			name: "happy path",
			modules: []codeanalysis.Module{
				{Name: "core", Path: "internal/core", LOC: 1200, TestCoverage: 0.8},
				{Name: "web", Path: "web", LOC: 4000, TestCoverage: -1},
			},
			health: codeanalysis.HealthReport{
				Repo:             testRepo,
				FetchedAt:        time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC),
				CycleCount:       2,
				UntestedModules:  []string{"web"},
				GodClasses:       []codeanalysis.SymbolRef{{Name: "Dispatcher"}},
				UndocumentedAPIs: []codeanalysis.SymbolRef{{Name: "Foo"}, {Name: "Bar"}},
				Notes:            []string{"index 1h old"},
			},
			wantSubs: []string{
				"Code-analysis summary for gemba",
				"core",
				"web",
				"cycles=2",
				"untested=1",
				"god_classes=1",
				"undocumented_apis=2",
				"note: index 1h old",
			},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if p["module_count"].(int) != 2 {
					t.Errorf("module_count=%v", p["module_count"])
				}
				if p["cycle_count"].(int) != 2 {
					t.Errorf("cycle_count=%v", p["cycle_count"])
				}
				if p["untested_count"].(int) != 1 {
					t.Errorf("untested_count=%v", p["untested_count"])
				}
			},
		},
		{
			name:    "empty modules",
			modules: nil,
			health:  codeanalysis.HealthReport{Repo: testRepo},
			wantSubs: []string{
				"backend reported no modules",
				"cycles=0",
			},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if p["module_count"].(int) != 0 {
					t.Errorf("module_count=%v", p["module_count"])
				}
			},
		},
		{
			name:       "modules error short-circuits",
			modulesErr: errors.New("backend down"),
			wantErr:    true,
		},
		{
			name:      "health error short-circuits",
			modules:   []codeanalysis.Module{{Name: "core"}},
			healthErr: errors.New("health unavailable"),
			wantErr:   true,
		},
		{
			name: "max_modules option truncates",
			modules: []codeanalysis.Module{
				{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"},
			},
			opts:     map[string]any{"max_modules": 2},
			wantSubs: []string{"truncated to 2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &stubBackend{
				modules: func(_ codeanalysis.RepoRef) ([]codeanalysis.Module, error) {
					return tc.modules, tc.modulesErr
				},
				health: func(_ codeanalysis.RepoRef) (codeanalysis.HealthReport, error) {
					return tc.health, tc.healthErr
				},
			}
			p, err := NewSummaryProvider(backend, testRepo, time.Minute)
			if err != nil {
				t.Fatalf("ctor: %v", err)
			}
			if p.ID() != promptctx.ProviderCodeAnalysisSummary {
				t.Errorf("ID=%q", p.ID())
			}
			if p.TTL() != time.Minute {
				t.Errorf("TTL=%v", p.TTL())
			}
			res, err := p.Refresh(context.Background(), tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("refresh: %v", err)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(res.Content, sub) {
					t.Errorf("Content missing %q\n--- got ---\n%s", sub, res.Content)
				}
			}
			if tc.wantPayload != nil {
				tc.wantPayload(t, res.Payload)
			}
		})
	}
}

func TestSummaryProviderCtorRejectsNilBackend(t *testing.T) {
	if _, err := NewSummaryProvider(nil, testRepo, 0); err == nil {
		t.Error("expected error for nil backend")
	}
}

func TestSummaryProviderCtorRejectsEmptyRepoName(t *testing.T) {
	if _, err := NewSummaryProvider(&stubBackend{}, codeanalysis.RepoRef{}, 0); err == nil {
		t.Error("expected error for empty repo name")
	}
}

func TestSummaryProviderDefaultTTL(t *testing.T) {
	p, err := NewSummaryProvider(&stubBackend{}, testRepo, 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.TTL() != 5*time.Minute {
		t.Errorf("default TTL=%v want 5m", p.TTL())
	}
}
