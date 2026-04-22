package providers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

type fakeDecisionsSrc struct {
	lastN     int
	decisions []Decision
}

func (f *fakeDecisionsSrc) Decisions(_ context.Context, lastN int) ([]Decision, error) {
	f.lastN = lastN
	if lastN > 0 && lastN < len(f.decisions) {
		return f.decisions[:lastN], nil
	}
	return f.decisions, nil
}

func TestDecisionsLogProviderRespectsLastNOption(t *testing.T) {
	src := &fakeDecisionsSrc{
		decisions: []Decision{
			{ID: "DD-24", Title: "Context providers primitive", RatifiedAt: time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)},
			{ID: "DD-23", Title: "Personas primitive"},
		},
	}
	p, err := NewDecisionsLogProvider(src, 0, 0)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if p.ID() != promptctx.ProviderDecisionsLog {
		t.Errorf("ID=%q", p.ID())
	}
	res, err := p.Refresh(context.Background(), map[string]any{"last_n": 5})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if src.lastN != 5 {
		t.Errorf("lastN forwarded=%d want 5", src.lastN)
	}
	if !strings.Contains(res.Content, "DD-24") || !strings.Contains(res.Content, "DD-23") {
		t.Errorf("Content missing DD ids: %q", res.Content)
	}
	if got, _ := res.Payload["count"].(int); got != 2 {
		t.Errorf("Payload.count=%v", res.Payload["count"])
	}
	if got, _ := res.Payload["last_n"].(int); got != 5 {
		t.Errorf("Payload.last_n=%v", res.Payload["last_n"])
	}
}

func TestDecisionsLogProviderDefaultsWhenOptMissing(t *testing.T) {
	src := &fakeDecisionsSrc{}
	p, _ := NewDecisionsLogProvider(src, 7, time.Second)
	if _, err := p.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if src.lastN != 7 {
		t.Errorf("defaultLastN forwarded=%d want 7", src.lastN)
	}
}

func TestDecisionsLogRenderEmpty(t *testing.T) {
	if got := renderDecisions(nil); !strings.Contains(got, "no ratified decisions") {
		t.Errorf("empty render=%q", got)
	}
}

func TestDecisionsLogProviderRejectsNilSource(t *testing.T) {
	if _, err := NewDecisionsLogProvider(nil, 0, 0); err == nil {
		t.Error("expected error for nil src")
	}
}
