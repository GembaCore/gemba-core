package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/core/promptctx"
)

func TestFuncProviderHappyPath(t *testing.T) {
	p, err := NewFuncProvider(promptctx.ProviderCIStatus, 10*time.Second,
		func(_ context.Context, opts map[string]any) (*promptctx.Result, error) {
			return &promptctx.Result{
				ProviderID: promptctx.ProviderCIStatus,
				Content:    "ci: green",
				Payload:    map[string]any{"opt_count": len(opts)},
			}, nil
		})
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.ID() != promptctx.ProviderCIStatus {
		t.Errorf("ID=%q", p.ID())
	}
	res, err := p.Refresh(context.Background(), map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if res.Content != "ci: green" {
		t.Errorf("Content=%q", res.Content)
	}
	if got, _ := res.Payload["opt_count"].(int); got != 2 {
		t.Errorf("opt_count=%v", res.Payload["opt_count"])
	}
}

func TestFuncProviderValidation(t *testing.T) {
	cases := []struct {
		name string
		id   promptctx.ProviderID
		ttl  time.Duration
		fn   func(context.Context, map[string]any) (*promptctx.Result, error)
	}{
		{"empty id", "", time.Second, func(context.Context, map[string]any) (*promptctx.Result, error) { return nil, nil }},
		{"zero ttl", promptctx.ProviderCIStatus, 0, func(context.Context, map[string]any) (*promptctx.Result, error) { return nil, nil }},
		{"nil fn", promptctx.ProviderCIStatus, time.Second, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewFuncProvider(c.id, c.ttl, c.fn); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestFuncProviderPropagatesErrors(t *testing.T) {
	boom := errors.New("source offline")
	p, err := NewFuncProvider(promptctx.ProviderCIStatus, time.Second,
		func(context.Context, map[string]any) (*promptctx.Result, error) {
			return nil, boom
		})
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if _, err := p.Refresh(context.Background(), nil); !errors.Is(err, boom) {
		t.Errorf("expected errors.Is boom, got %v", err)
	}
}
