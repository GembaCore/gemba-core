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

func TestHealthReportProvider_TableDriven(t *testing.T) {
	tFetched := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	now := tFetched.Add(2 * time.Hour)

	tests := []struct {
		name        string
		opts        map[string]any
		health      codeanalysis.HealthReport
		healthErr   error
		wantErr     bool
		wantSubs    []string
		wantNotSubs []string
		wantPayload func(*testing.T, map[string]any)
	}{
		{
			name: "happy path with sections populated",
			health: codeanalysis.HealthReport{
				Repo:             testRepo,
				FetchedAt:        tFetched,
				StaleAfter:       4 * time.Hour,
				CycleCount:       3,
				UntestedModules:  []string{"web", "api"},
				GodClasses:       []codeanalysis.SymbolRef{{Name: "Dispatcher", File: "internal/persona/dispatcher.go", Line: 12}},
				UndocumentedAPIs: []codeanalysis.SymbolRef{{Name: "Foo"}, {Name: "Bar"}, {Name: "Baz"}},
				Notes:            []string{"embeddings disabled"},
			},
			wantSubs: []string{
				"Health report for gemba",
				"cycles: 3",
				"untested modules (2)",
				"web",
				"god classes (1)",
				"Dispatcher (internal/persona/dispatcher.go:12)",
				"undocumented APIs (3)",
				"stale code: (none)",
				"note: embeddings disabled",
			},
			wantNotSubs: []string{"STALE"},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if p["cycle_count"].(int) != 3 {
					t.Errorf("cycle_count=%v", p["cycle_count"])
				}
				if p["stale"].(bool) != false {
					t.Errorf("stale=%v want false", p["stale"])
				}
			},
		},
		{
			name: "stale report flagged",
			health: codeanalysis.HealthReport{
				Repo:       testRepo,
				FetchedAt:  tFetched,
				StaleAfter: 30 * time.Minute,
			},
			wantSubs: []string{"STALE"},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if p["stale"].(bool) != true {
					t.Error("expected stale=true")
				}
			},
		},
		{
			name: "empty report — clean bill of health",
			health: codeanalysis.HealthReport{
				Repo: testRepo,
			},
			wantSubs: []string{
				"cycles: 0",
				"untested modules: (none)",
				"god classes: (none)",
				"undocumented APIs: (none)",
				"stale code: (none)",
			},
		},
		{
			name: "max_items truncates per section",
			opts: map[string]any{"max_items": 2},
			health: codeanalysis.HealthReport{
				Repo:            testRepo,
				UntestedModules: []string{"a", "b", "c", "d", "e"},
			},
			wantSubs: []string{
				"untested modules (5)",
				"truncated to 2",
			},
		},
		{
			name:      "backend error surfaces",
			healthErr: errors.New("backend down"),
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &stubBackend{
				health: func(_ codeanalysis.RepoRef) (codeanalysis.HealthReport, error) {
					return tc.health, tc.healthErr
				},
			}
			p, err := NewHealthReportProvider(backend, testRepo, time.Minute)
			if err != nil {
				t.Fatalf("ctor: %v", err)
			}
			p.now = func() time.Time { return now }

			if p.ID() != promptctx.ProviderHealthReport {
				t.Errorf("ID=%q", p.ID())
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
			for _, sub := range tc.wantNotSubs {
				if strings.Contains(res.Content, sub) {
					t.Errorf("Content unexpectedly contained %q", sub)
				}
			}
			if tc.wantPayload != nil {
				tc.wantPayload(t, res.Payload)
			}
		})
	}
}

func TestHealthReportProviderCtorGuards(t *testing.T) {
	if _, err := NewHealthReportProvider(nil, testRepo, 0); err == nil {
		t.Error("expected error for nil backend")
	}
	if _, err := NewHealthReportProvider(&stubBackend{}, codeanalysis.RepoRef{}, 0); err == nil {
		t.Error("expected error for empty repo name")
	}
}

func TestHealthReportProviderDefaultTTL(t *testing.T) {
	p, err := NewHealthReportProvider(&stubBackend{}, testRepo, 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.TTL() != 5*time.Minute {
		t.Errorf("default TTL=%v want 5m", p.TTL())
	}
}
