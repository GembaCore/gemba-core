package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/core/promptctx"
)

type fakeSummarySrc struct {
	text string
	err  error
}

func (f fakeSummarySrc) Summary(context.Context) (string, error) {
	return f.text, f.err
}

func TestProjectSummaryProvider(t *testing.T) {
	p, err := NewProjectSummaryProvider(fakeSummarySrc{text: "gemba pairs one WPA with one OPA"}, 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.ID() != promptctx.ProviderProjectSummary {
		t.Errorf("ID=%q", p.ID())
	}
	if p.TTL() != 5*time.Minute {
		t.Errorf("default TTL=%v", p.TTL())
	}
	res, err := p.Refresh(context.Background(), map[string]any{"max_tokens": 500})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if res.Content != "gemba pairs one WPA with one OPA" {
		t.Errorf("Content=%q", res.Content)
	}
	if res.TokenEstimate != 500 {
		t.Errorf("TokenEstimate=%d", res.TokenEstimate)
	}
}

func TestProjectSummaryProviderFloatMaxTokens(t *testing.T) {
	p, _ := NewProjectSummaryProvider(fakeSummarySrc{text: "x"}, time.Second)
	res, err := p.Refresh(context.Background(), map[string]any{"max_tokens": float64(250)})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if res.TokenEstimate != 250 {
		t.Errorf("TokenEstimate=%d want 250", res.TokenEstimate)
	}
}

func TestProjectSummaryProviderPropagatesErr(t *testing.T) {
	boom := errors.New("summary source unreachable")
	p, _ := NewProjectSummaryProvider(fakeSummarySrc{err: boom}, time.Second)
	if _, err := p.Refresh(context.Background(), nil); !errors.Is(err, boom) {
		t.Errorf("expected boom, got %v", err)
	}
}

func TestProjectSummaryProviderRejectsNilSource(t *testing.T) {
	if _, err := NewProjectSummaryProvider(nil, time.Second); err == nil {
		t.Error("expected error for nil src")
	}
}
