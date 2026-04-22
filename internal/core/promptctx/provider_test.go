package promptctx

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

// fakeProvider is a hand-rolled Provider for testing the registry and
// cache. It avoids importing the providers subpackage, which would
// create a cycle.
type fakeProvider struct {
	id         ProviderID
	ttl        time.Duration
	refreshes  int
	nextErr    error
	contentFor func(opts map[string]any) string
}

func (f *fakeProvider) ID() ProviderID     { return f.id }
func (f *fakeProvider) TTL() time.Duration { return f.ttl }
func (f *fakeProvider) Refresh(_ context.Context, opts map[string]any) (*Result, error) {
	f.refreshes++
	if f.nextErr != nil {
		err := f.nextErr
		f.nextErr = nil
		return nil, err
	}
	content := ""
	if f.contentFor != nil {
		content = f.contentFor(opts)
	}
	return &Result{
		ProviderID: f.id,
		Content:    content,
	}, nil
}

func TestRegistryRegisterRejectsDuplicates(t *testing.T) {
	r := NewRegistry()
	p1 := &fakeProvider{id: ProviderProjectSummary, ttl: time.Minute}
	p2 := &fakeProvider{id: ProviderProjectSummary, ttl: time.Minute}
	if err := r.Register(p1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(p1); err != nil {
		t.Errorf("idempotent re-register of same instance: %v", err)
	}
	if err := r.Register(p2); err == nil {
		t.Error("expected error on duplicate ID with different instance")
	}
}

func TestRegistryRejectsEmptyID(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeProvider{id: ""}); err == nil {
		t.Error("expected error for empty ID")
	}
	if err := r.Register(nil); err == nil {
		t.Error("expected error for nil provider")
	}
}

func TestRegistryListOrdering(t *testing.T) {
	r := NewRegistry()
	// Register a custom + two seeds out of order.
	r.MustRegister(&fakeProvider{id: ProviderDecisionsLog, ttl: time.Minute})
	r.MustRegister(&fakeProvider{id: "custom_z", ttl: time.Minute})
	r.MustRegister(&fakeProvider{id: ProviderProjectSummary, ttl: time.Minute})
	r.MustRegister(&fakeProvider{id: "custom_a", ttl: time.Minute})

	got := r.List()
	want := []ProviderID{
		ProviderProjectSummary,
		ProviderDecisionsLog,
		"custom_a",
		"custom_z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List order mismatch:\n got %v\nwant %v", got, want)
	}
}

func TestCacheInvokeRespectsTTL(t *testing.T) {
	r := NewRegistry()
	p := &fakeProvider{
		id:         ProviderProjectSummary,
		ttl:        10 * time.Second,
		contentFor: func(map[string]any) string { return "hello" },
	}
	r.MustRegister(p)

	now := time.Unix(1_000_000, 0)
	c := NewCache(r, WithClock(func() time.Time { return now }))

	cfg := Config{ID: ProviderProjectSummary}
	res1, err := c.Invoke(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first Invoke: %v", err)
	}
	if res1.Content != "hello" || res1.FetchedAt != now {
		t.Errorf("first Result unexpected: %+v", res1)
	}
	if p.refreshes != 1 {
		t.Errorf("expected 1 refresh, got %d", p.refreshes)
	}

	// Within TTL: cached.
	now = now.Add(5 * time.Second)
	if _, err := c.Invoke(context.Background(), cfg); err != nil {
		t.Fatalf("second Invoke: %v", err)
	}
	if p.refreshes != 1 {
		t.Errorf("within TTL should not re-fetch, got %d refreshes", p.refreshes)
	}

	// Past TTL: re-fetches.
	now = now.Add(10 * time.Second)
	res3, err := c.Invoke(context.Background(), cfg)
	if err != nil {
		t.Fatalf("third Invoke: %v", err)
	}
	if res3.FetchedAt != now {
		t.Errorf("expected FetchedAt=%v, got %v", now, res3.FetchedAt)
	}
	if p.refreshes != 2 {
		t.Errorf("past TTL should re-fetch, got %d", p.refreshes)
	}
}

func TestCacheInvokeReturnsStaleOnRefreshError(t *testing.T) {
	r := NewRegistry()
	p := &fakeProvider{
		id:         ProviderDecisionsLog,
		ttl:        time.Second,
		contentFor: func(map[string]any) string { return "good" },
	}
	r.MustRegister(p)

	now := time.Unix(2_000_000, 0)
	c := NewCache(r, WithClock(func() time.Time { return now }))

	cfg := Config{ID: ProviderDecisionsLog}
	if _, err := c.Invoke(context.Background(), cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	now = now.Add(5 * time.Second) // expire TTL
	p.nextErr = errors.New("boom")
	res, err := c.Invoke(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error surfaced alongside stale result")
	}
	if res == nil {
		t.Fatal("expected stale cached Result on refresh failure")
	}
	if !res.Stale {
		t.Error("expected Stale=true on degraded result")
	}
	if res.Content != "good" {
		t.Errorf("expected stale content 'good', got %q", res.Content)
	}
}

func TestCacheInvokeDistinguishesOptions(t *testing.T) {
	r := NewRegistry()
	p := &fakeProvider{
		id:  ProviderBeadDatabase,
		ttl: time.Minute,
		contentFor: func(opts map[string]any) string {
			if v, ok := opts["scope"]; ok {
				return "scope=" + asString(v)
			}
			return "no-scope"
		},
	}
	r.MustRegister(p)

	c := NewCache(r)
	_, err := c.Invoke(context.Background(), Config{
		ID:      ProviderBeadDatabase,
		Options: map[string]any{"scope": "gemba"},
	})
	if err != nil {
		t.Fatalf("invoke a: %v", err)
	}
	_, err = c.Invoke(context.Background(), Config{
		ID:      ProviderBeadDatabase,
		Options: map[string]any{"scope": "gemba_prime"},
	})
	if err != nil {
		t.Fatalf("invoke b: %v", err)
	}
	if p.refreshes != 2 {
		t.Errorf("expected 2 refreshes for distinct options, got %d", p.refreshes)
	}
	// Same option signature in different map order → one more call only
	// if we failed to canonicalize keys.
	_, err = c.Invoke(context.Background(), Config{
		ID:      ProviderBeadDatabase,
		Options: map[string]any{"scope": "gemba"},
	})
	if err != nil {
		t.Fatalf("invoke a again: %v", err)
	}
	if p.refreshes != 2 {
		t.Errorf("expected cached hit for equal options, got %d", p.refreshes)
	}
}

func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return ""
	}
}

func TestCacheInvokeRejectsUnknownProvider(t *testing.T) {
	c := NewCache(NewRegistry())
	_, err := c.Invoke(context.Background(), Config{ID: "mystery"})
	if err == nil {
		t.Fatal("expected error for unregistered ID")
	}
}

func TestCacheInvalidate(t *testing.T) {
	r := NewRegistry()
	p := &fakeProvider{id: ProviderCIStatus, ttl: time.Hour, contentFor: func(map[string]any) string { return "ok" }}
	r.MustRegister(p)
	c := NewCache(r)
	cfg := Config{ID: ProviderCIStatus}
	if _, err := c.Invoke(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Invoke(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if p.refreshes != 1 {
		t.Errorf("expected 1 refresh pre-invalidate, got %d", p.refreshes)
	}
	c.Invalidate(ProviderCIStatus, nil)
	if _, err := c.Invoke(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if p.refreshes != 2 {
		t.Errorf("expected refresh post-invalidate, got %d", p.refreshes)
	}
}

func TestResultJSONRoundTrip(t *testing.T) {
	r := Result{
		ProviderID: ProviderProjectSummary,
		Content:    "gemba is a thing",
		Payload:    map[string]any{"version": "v1"},
		FetchedAt:  time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		TTL:        30 * time.Second,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round Result
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.ProviderID != r.ProviderID || round.Content != r.Content {
		t.Errorf("round-trip mismatch: %+v vs %+v", round, r)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := (Config{ID: ""}).Validate(); err == nil {
		t.Error("expected error for empty ID")
	}
	if err := (Config{ID: ProviderProjectSummary}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
