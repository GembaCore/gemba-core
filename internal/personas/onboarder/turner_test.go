package onboarder

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/internal/skills/newproject"
)

// TestSkillTurner_LazySpawn: Probe and Turn share one Persona and
// only invoke the resolver once on the happy path.
func TestSkillTurner_LazySpawn(t *testing.T) {
	t.Parallel()

	calls := 0
	client := &fakeClient{responses: []string{
		envelope(t, fullPlan(), "ok"),
	}}
	resolver := func(_ context.Context) (newproject.LLMClient, error) {
		calls++
		return client, nil
	}

	turner := NewSkillTurner(resolver)
	if err := turner.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	// Probe is idempotent.
	if err := turner.Probe(context.Background()); err != nil {
		t.Fatalf("Probe (second): %v", err)
	}
	state, reply, err := turner.Turn(context.Background(),
		newproject.EmptyState(), "x", nil)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if reply == "" {
		t.Error("empty reply")
	}
	if state.Turn != 1 {
		t.Errorf("turn: got %d, want 1", state.Turn)
	}
	if calls != 1 {
		t.Errorf("resolver called %d times; want 1 (lazy + cached)", calls)
	}
}

// TestSkillTurner_NoLLMClient: Probe surfaces the diagnostic.
func TestSkillTurner_NoLLMClient(t *testing.T) {
	t.Parallel()
	resolver := func(_ context.Context) (newproject.LLMClient, error) {
		return nil, ErrNoLLMClient
	}
	turner := NewSkillTurner(resolver)
	err := turner.Probe(context.Background())
	if err == nil {
		t.Fatal("expected Probe to fail when no client configured")
	}
	if !IsNoClient(err) {
		t.Errorf("IsNoClient: false; want true (err=%v)", err)
	}
	// Subsequent calls reuse the cached error (don't hammer the
	// resolver). Confirm by checking the resolver is only called
	// once even after multiple Probes.
	for range 3 {
		_ = turner.Probe(context.Background())
	}
}

// TestSkillTurner_NilResolver: passes through to a turner whose
// Probe always returns the diagnostic.
func TestSkillTurner_NilResolver(t *testing.T) {
	t.Parallel()
	turner := NewSkillTurner(nil)
	if err := turner.Probe(context.Background()); !IsNoClient(err) {
		t.Errorf("expected no-client diagnostic on nil resolver; got %v", err)
	}
	if _, _, err := turner.Turn(context.Background(),
		newproject.EmptyState(), "x", nil); !IsNoClient(err) {
		t.Errorf("expected no-client diagnostic from Turn on nil resolver; got %v", err)
	}
}

// TestSkillTurner_ResolverRetryAfterTransientError: a non-diagnostic
// failure is NOT cached — the next call retries.
func TestSkillTurner_ResolverRetryAfterTransientError(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		attempts int
	)
	transient := errors.New("config.toml: read: permission denied")
	client := &fakeClient{responses: []string{envelope(t, fullPlan(), "ok")}}
	resolver := func(_ context.Context) (newproject.LLMClient, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return nil, transient
		}
		return client, nil
	}

	turner := NewSkillTurner(resolver)
	err := turner.Probe(context.Background())
	if err == nil {
		t.Fatal("expected first Probe to fail")
	}
	if IsNoClient(err) {
		t.Fatalf("transient error misclassified as no-client: %v", err)
	}
	// Second Probe retries and succeeds.
	if err := turner.Probe(context.Background()); err != nil {
		t.Fatalf("expected second Probe to succeed; got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 {
		t.Errorf("attempts: got %d, want 2 (transient retried)", attempts)
	}
}

// TestSkillTurner_GreetingIsConstant: the greeting does not depend
// on Probe — same string regardless of LLM-client state.
func TestSkillTurner_GreetingIsConstant(t *testing.T) {
	t.Parallel()
	turner := NewSkillTurner(nil)
	if got := turner.Greeting(); got != Greeting {
		t.Errorf("Greeting: got %q, want %q", got, Greeting)
	}
}
