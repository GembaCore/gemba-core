package providers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/core/promptctx"
)

type fakeBeadSrc struct {
	calls [][]string
	out   []BeadSnapshot
}

func (f *fakeBeadSrc) Snapshot(_ context.Context, repos []string) ([]BeadSnapshot, error) {
	f.calls = append(f.calls, append([]string(nil), repos...))
	if f.out != nil {
		return f.out, nil
	}
	// Default: echo each repo as an empty snapshot.
	out := make([]BeadSnapshot, 0, len(repos))
	for _, r := range repos {
		out = append(out, BeadSnapshot{Repo: r, GeneratedAt: time.Now()})
	}
	return out, nil
}

func TestBeadDatabaseProviderUsesOptionScope(t *testing.T) {
	src := &fakeBeadSrc{
		out: []BeadSnapshot{
			{Repo: "gemba", OpenCount: 12, InProgress: 3, Blocked: 1, RecentTitles: []string{"gm-eiw: context providers"}},
		},
	}
	p, err := NewBeadDatabaseProvider(src, nil, 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.ID() != promptctx.ProviderBeadDatabase {
		t.Errorf("ID=%q", p.ID())
	}
	res, err := p.Refresh(context.Background(), map[string]any{"scope": []string{"gemba"}})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !strings.Contains(res.Content, "repo: gemba") {
		t.Errorf("Content missing repo: %q", res.Content)
	}
	if !strings.Contains(res.Content, "gm-eiw") {
		t.Errorf("Content missing title: %q", res.Content)
	}
	if len(src.calls) != 1 || src.calls[0][0] != "gemba" {
		t.Errorf("unexpected src calls: %v", src.calls)
	}
}

func TestBeadDatabaseProviderAcceptsAnySliceScope(t *testing.T) {
	src := &fakeBeadSrc{}
	p, _ := NewBeadDatabaseProvider(src, nil, time.Second)
	_, err := p.Refresh(context.Background(), map[string]any{
		"scope": []any{"primary", "sibling"},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(src.calls) != 1 || len(src.calls[0]) != 2 || src.calls[0][1] != "sibling" {
		t.Errorf("scope []any not honored: %v", src.calls)
	}
}

func TestBeadDatabaseProviderRejectsBadScopeType(t *testing.T) {
	p, _ := NewBeadDatabaseProvider(&fakeBeadSrc{}, nil, time.Second)
	if _, err := p.Refresh(context.Background(), map[string]any{"scope": 42}); err == nil {
		t.Error("expected error for int scope")
	}
	if _, err := p.Refresh(context.Background(), map[string]any{"scope": []any{1}}); err == nil {
		t.Error("expected error for non-string scope element")
	}
}

func TestBeadDatabaseProviderFallsBackToDefaultScope(t *testing.T) {
	src := &fakeBeadSrc{}
	p, _ := NewBeadDatabaseProvider(src, []string{"gemba"}, time.Second)
	if _, err := p.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(src.calls) != 1 || src.calls[0][0] != "gemba" {
		t.Errorf("default scope not used: %v", src.calls)
	}
}

func TestBeadDatabaseProviderRequiresScope(t *testing.T) {
	p, _ := NewBeadDatabaseProvider(&fakeBeadSrc{}, nil, time.Second)
	if _, err := p.Refresh(context.Background(), nil); err == nil {
		t.Error("expected error for missing scope")
	}
}

func TestBeadDatabaseProviderRejectsNilSource(t *testing.T) {
	if _, err := NewBeadDatabaseProvider(nil, nil, time.Second); err == nil {
		t.Error("expected error for nil src")
	}
}

func TestBeadDatabaseProviderDefensiveCopyOfDefaultScope(t *testing.T) {
	scope := []string{"gemba"}
	p, _ := NewBeadDatabaseProvider(&fakeBeadSrc{}, scope, time.Second)
	scope[0] = "tampered"
	res, err := p.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if strings.Contains(res.Content, "tampered") {
		t.Errorf("external mutation leaked: %q", res.Content)
	}
}
