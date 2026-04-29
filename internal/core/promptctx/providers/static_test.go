package providers

import (
	"context"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/core/promptctx"
)

func TestStaticProvider(t *testing.T) {
	p, err := NewStaticProvider(promptctx.ProviderProjectSummary, "hello world", 30*time.Second)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.ID() != promptctx.ProviderProjectSummary {
		t.Errorf("ID=%q", p.ID())
	}
	if p.TTL() != 30*time.Second {
		t.Errorf("TTL=%v", p.TTL())
	}
	res, err := p.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if res.Content != "hello world" {
		t.Errorf("Content=%q", res.Content)
	}
	if res.ProviderID != promptctx.ProviderProjectSummary {
		t.Errorf("ProviderID=%q", res.ProviderID)
	}
}

func TestStaticProviderDefaultTTL(t *testing.T) {
	p, err := NewStaticProvider(promptctx.ProviderProjectSummary, "x", 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.TTL() != 24*time.Hour {
		t.Errorf("default TTL=%v want 24h", p.TTL())
	}
}

func TestStaticProviderRejectsEmptyID(t *testing.T) {
	if _, err := NewStaticProvider("", "x", time.Second); err == nil {
		t.Error("expected error for empty id")
	}
}
