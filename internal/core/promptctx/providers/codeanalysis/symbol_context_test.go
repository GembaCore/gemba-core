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

func TestSymbolContextProvider_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		opts        map[string]any
		impactRet   codeanalysis.ImpactReport
		impactErr   error
		wantErr     bool
		wantSubs    []string
		wantPayload func(*testing.T, map[string]any)
	}{
		{
			name: "happy path with neighbours",
			opts: map[string]any{"symbol": "Dispatcher.Run", "depth": 2},
			impactRet: codeanalysis.ImpactReport{
				Target:    "Dispatcher.Run",
				Direction: codeanalysis.ImpactBoth,
				Affected: []codeanalysis.SymbolRef{
					{Name: "main.run", File: "cmd/gemba/main.go", Line: 42},
					{Name: "Dispatcher.dispatch"},
				},
				Risk:  codeanalysis.RiskMedium,
				Notes: []string{"3 callers in cmd/gemba"},
			},
			wantSubs: []string{
				"Symbol context for \"Dispatcher.Run\" in gemba",
				"main.run (cmd/gemba/main.go:42)",
				"Dispatcher.dispatch",
				"risk: medium",
				"3 callers",
			},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if p["symbol"].(string) != "Dispatcher.Run" {
					t.Errorf("symbol=%v", p["symbol"])
				}
				if p["affected_count"].(int) != 2 {
					t.Errorf("affected_count=%v", p["affected_count"])
				}
				if p["direction"].(string) != string(codeanalysis.ImpactBoth) {
					t.Errorf("direction=%v", p["direction"])
				}
			},
		},
		{
			name: "empty backend response",
			opts: map[string]any{"symbol": "Anon"},
			impactRet: codeanalysis.ImpactReport{
				Target:    "Anon",
				Direction: codeanalysis.ImpactBoth,
			},
			wantSubs: []string{"no neighbouring symbols"},
		},
		{
			name:    "missing symbol option",
			opts:    map[string]any{},
			wantErr: true,
		},
		{
			name:      "backend error surfaces",
			opts:      map[string]any{"symbol": "X"},
			impactErr: errors.New("backend down"),
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &stubBackend{
				impact: func(_ codeanalysis.RepoRef, target string, dir codeanalysis.ImpactDirection) (codeanalysis.ImpactReport, error) {
					if dir != codeanalysis.ImpactBoth {
						t.Errorf("expected ImpactBoth, got %q", dir)
					}
					if tc.impactErr != nil {
						return codeanalysis.ImpactReport{}, tc.impactErr
					}
					ret := tc.impactRet
					ret.Target = target
					return ret, nil
				},
			}
			p, err := NewSymbolContextProvider(backend, testRepo, 0)
			if err != nil {
				t.Fatalf("ctor: %v", err)
			}
			if p.ID() != promptctx.ProviderSymbolContext {
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
			if tc.wantPayload != nil {
				tc.wantPayload(t, res.Payload)
			}
		})
	}
}

func TestSymbolContextProviderDefaultTTL(t *testing.T) {
	p, err := NewSymbolContextProvider(&stubBackend{}, testRepo, 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.TTL() != time.Minute {
		t.Errorf("default TTL=%v want 1m", p.TTL())
	}
}

func TestSymbolContextProviderCtorGuards(t *testing.T) {
	if _, err := NewSymbolContextProvider(nil, testRepo, 0); err == nil {
		t.Error("expected error for nil backend")
	}
	if _, err := NewSymbolContextProvider(&stubBackend{}, codeanalysis.RepoRef{}, 0); err == nil {
		t.Error("expected error for empty repo name")
	}
}
