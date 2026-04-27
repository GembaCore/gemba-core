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

func TestImpactAnalysisProvider_TableDriven(t *testing.T) {
	tests := []struct {
		name             string
		defaultDirection codeanalysis.ImpactDirection
		opts             map[string]any
		impactRet        codeanalysis.ImpactReport
		impactErr        error
		wantDir          codeanalysis.ImpactDirection
		wantErr          bool
		wantSubs         []string
		wantPayload      func(*testing.T, map[string]any)
	}{
		{
			name: "happy path upstream",
			opts: map[string]any{"symbol": "Dispatcher.Run"},
			impactRet: codeanalysis.ImpactReport{
				Affected: []codeanalysis.SymbolRef{
					{Name: "main.run", File: "cmd/gemba/main.go", Line: 42},
				},
				Risk:  codeanalysis.RiskHigh,
				Depth: 1,
				Notes: []string{"4 callers in cmd"},
			},
			wantDir: codeanalysis.ImpactUpstream,
			wantSubs: []string{
				"Impact analysis for \"Dispatcher.Run\" in gemba (upstream",
				"depth=1",
				"main.run (cmd/gemba/main.go:42)",
				"risk: high",
				"4 callers",
			},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if p["direction"].(string) != string(codeanalysis.ImpactUpstream) {
					t.Errorf("direction=%v", p["direction"])
				}
				if p["affected_count"].(int) != 1 {
					t.Errorf("affected_count=%v", p["affected_count"])
				}
				if p["risk"].(string) != string(codeanalysis.RiskHigh) {
					t.Errorf("risk=%v", p["risk"])
				}
			},
		},
		{
			name:     "explicit downstream direction",
			opts:     map[string]any{"symbol": "X", "direction": "downstream"},
			wantDir:  codeanalysis.ImpactDownstream,
			wantSubs: []string{"(downstream"},
		},
		{
			name:    "invalid direction falls back to default with note",
			opts:    map[string]any{"symbol": "X", "direction": "sideways"},
			wantDir: codeanalysis.ImpactUpstream,
			wantSubs: []string{
				"ignored invalid direction",
				"sideways",
			},
			wantPayload: func(t *testing.T, p map[string]any) {
				t.Helper()
				if _, ok := p["direction_note"]; !ok {
					t.Error("expected direction_note in Payload")
				}
			},
		},
		{
			name:             "default direction override via ctor",
			defaultDirection: codeanalysis.ImpactBoth,
			opts:             map[string]any{"symbol": "X"},
			wantDir:          codeanalysis.ImpactBoth,
			wantSubs:         []string{"(both"},
		},
		{
			name:      "empty affected",
			opts:      map[string]any{"symbol": "X"},
			impactRet: codeanalysis.ImpactReport{},
			wantDir:   codeanalysis.ImpactUpstream,
			wantSubs:  []string{"no affected symbols"},
		},
		{
			name:    "missing symbol",
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
					if tc.wantDir != "" && dir != tc.wantDir {
						t.Errorf("dir=%q want %q", dir, tc.wantDir)
					}
					if tc.impactErr != nil {
						return codeanalysis.ImpactReport{}, tc.impactErr
					}
					ret := tc.impactRet
					ret.Target = target
					ret.Direction = dir
					return ret, nil
				},
			}
			p, err := NewImpactAnalysisProvider(backend, testRepo, tc.defaultDirection, 0)
			if err != nil {
				t.Fatalf("ctor: %v", err)
			}
			if p.ID() != promptctx.ProviderImpactAnalysis {
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

func TestImpactAnalysisProviderCtorGuards(t *testing.T) {
	if _, err := NewImpactAnalysisProvider(nil, testRepo, "", 0); err == nil {
		t.Error("expected error for nil backend")
	}
	if _, err := NewImpactAnalysisProvider(&stubBackend{}, codeanalysis.RepoRef{}, "", 0); err == nil {
		t.Error("expected error for empty repo name")
	}
	if _, err := NewImpactAnalysisProvider(&stubBackend{}, testRepo, "sideways", 0); err == nil {
		t.Error("expected error for invalid defaultDirection")
	}
}

func TestImpactAnalysisProviderDefaultTTL(t *testing.T) {
	p, err := NewImpactAnalysisProvider(&stubBackend{}, testRepo, "", 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.TTL() != time.Minute {
		t.Errorf("default TTL=%v want 1m", p.TTL())
	}
}
