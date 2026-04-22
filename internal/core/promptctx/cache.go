// Package promptctx: see doc.go for the overview.
package promptctx

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Cache wraps a Registry with TTL-scoped memoization of Refresh
// results. It is the layer PromptEnvelope uses at assembly time — a
// single tick of the envelope builder calls Invoke per declared
// provider, and Cache decides whether to re-fetch or return the last
// Result.
//
// Concurrency: Invoke is safe for concurrent callers. Entries are
// keyed by (ProviderID, canonical options signature) so two personas
// that declare bead_database with different repo scopes each get their
// own cache entry.
type Cache struct {
	registry *Registry
	now      func() time.Time

	mu      sync.Mutex
	entries map[cacheKey]*cacheEntry
}

type cacheKey struct {
	id  ProviderID
	sig string
}

type cacheEntry struct {
	result    *Result
	fetchedAt time.Time
	ttl       time.Duration
	// inflight serializes concurrent misses on the same key so we
	// don't fan out N parallel Refreshes for one persona's context.
	inflight *sync.Mutex
}

// CacheOption configures a Cache.
type CacheOption func(*Cache)

// WithClock overrides the clock used to stamp FetchedAt and compute
// staleness. Tests use this to drive deterministic TTL behavior.
func WithClock(nowFn func() time.Time) CacheOption {
	return func(c *Cache) { c.now = nowFn }
}

// NewCache returns a Cache backed by the given registry.
func NewCache(reg *Registry, opts ...CacheOption) *Cache {
	c := &Cache{
		registry: reg,
		now:      time.Now,
		entries:  make(map[cacheKey]*cacheEntry),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Invoke returns the cached Result for cfg if it is still within its
// TTL; otherwise it calls the provider's Refresh and caches the new
// Result. When a fresh Refresh fails but a previously-cached Result
// exists, Invoke returns the cached Result with Stale=true so the
// caller can render a degraded-but-useful context and surface the
// staleness in the audit log.
func (c *Cache) Invoke(ctx context.Context, cfg Config) (*Result, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p, ok := c.registry.Get(cfg.ID)
	if !ok {
		return nil, fmt.Errorf("promptctx: Invoke: provider %q not registered", cfg.ID)
	}
	key := cacheKey{id: cfg.ID, sig: canonicalOptionsSig(cfg.Options)}

	c.mu.Lock()
	entry, exists := c.entries[key]
	now := c.now()
	if exists && now.Sub(entry.fetchedAt) < entry.ttl {
		cached := *entry.result
		c.mu.Unlock()
		return &cached, nil
	}
	if !exists {
		entry = &cacheEntry{inflight: &sync.Mutex{}}
		c.entries[key] = entry
	}
	inflight := entry.inflight
	c.mu.Unlock()

	inflight.Lock()
	defer inflight.Unlock()

	// Re-check after acquiring inflight — another caller may have
	// refreshed while we waited.
	c.mu.Lock()
	now = c.now()
	if e, ok := c.entries[key]; ok && e.result != nil && now.Sub(e.fetchedAt) < e.ttl {
		cached := *e.result
		c.mu.Unlock()
		return &cached, nil
	}
	c.mu.Unlock()

	res, err := p.Refresh(ctx, cfg.Options)
	fetchedAt := c.now()
	if err != nil {
		// Degrade: if we have a previously-cached result, return it
		// marked Stale so the audit log can record the staleness.
		c.mu.Lock()
		if e, ok := c.entries[key]; ok && e.result != nil {
			cached := *e.result
			c.mu.Unlock()
			cached.Stale = true
			return &cached, err
		}
		c.mu.Unlock()
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("promptctx: provider %q returned nil Result", cfg.ID)
	}
	if res.ProviderID == "" {
		res.ProviderID = cfg.ID
	}
	if res.ProviderID != cfg.ID {
		return nil, fmt.Errorf(
			"promptctx: provider %q returned Result for %q",
			cfg.ID, res.ProviderID)
	}
	res.FetchedAt = fetchedAt
	if res.TTL == 0 {
		res.TTL = p.TTL()
	}
	res.Stale = false

	stored := *res
	c.mu.Lock()
	c.entries[key] = &cacheEntry{
		result:    &stored,
		fetchedAt: fetchedAt,
		ttl:       res.TTL,
		inflight:  inflight,
	}
	c.mu.Unlock()
	out := *res
	return &out, nil
}

// Invalidate drops any cached result for (id, opts). Subsequent Invoke
// calls re-fetch. Used by the audit layer when a state-change event
// known to affect this provider fires (e.g., bead written →
// invalidate bead_database).
func (c *Cache) Invalidate(id ProviderID, opts map[string]any) {
	key := cacheKey{id: id, sig: canonicalOptionsSig(opts)}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// canonicalOptionsSig returns a stable string for opts so equal option
// maps hash to the same cache key regardless of iteration order.
// Values are rendered via fmt with the %#v verb so typed scalars
// (int vs int64 vs string-"1") remain distinct.
func canonicalOptionsSig(opts map[string]any) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var buf []byte
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, '&')
		}
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = fmt.Appendf(buf, "%#v", opts[k])
	}
	return string(buf)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
