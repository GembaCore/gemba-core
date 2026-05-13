// Package quota implements a per-tenant token-bucket rate limiter for
// gemba's control-plane mutating routes (gm-o9t8.4.x — workstream B
// slice a). The bucket fills at Rate tokens/second up to a maximum of
// Burst tokens. Allow consumes one token and reports whether the call
// is permitted; on deny it also returns a retry-after duration the HTTP
// surface stamps into the 429 response.
//
// The Limiter is process-local — multi-instance gemba serve will need
// a shared backing store, but the slice-a scope is single-instance.
// Configuration is per-tenant: lookup returns Defaults() when a tenant
// has no explicit row, so a fresh tenant starts under the system-wide
// default rate.
package quota

import (
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// Config carries the per-tenant token-bucket parameters.
//
//	Rate  — sustained tokens per second (refill speed).
//	Burst — bucket size (the cap; also the initial fill).
//
// A zero Config disables rate limiting entirely; the limiter treats it
// as "unlimited" so callers can opt out by zeroing both fields.
type Config struct {
	Rate  float64
	Burst float64
}

// Defaults returns the system-wide default quota: 10 req/s sustained,
// 20-request burst window. Tunable per tenant via Limiter.Set.
func Defaults() Config {
	return Config{Rate: 10, Burst: 20}
}

// bucket is the per-tenant runtime state. tokens decays toward Burst at
// Rate tokens/sec; last is the wall clock of the most recent refill.
type bucket struct {
	cfg    Config
	tokens float64
	last   time.Time
}

// Limiter is the package's exported entrypoint. Thread-safe; the zero
// value is unusable — obtain a Limiter via NewLimiter.
type Limiter struct {
	mu       sync.Mutex
	now      func() time.Time
	defaults Config
	configs  map[tenant.ID]Config
	buckets  map[tenant.ID]*bucket
}

// NewLimiter returns a Limiter with the system-wide default quota.
// Callers tune per-tenant configs via Set.
func NewLimiter() *Limiter {
	return &Limiter{
		now:      time.Now,
		defaults: Defaults(),
		configs:  map[tenant.ID]Config{},
		buckets:  map[tenant.ID]*bucket{},
	}
}

// SetDefaults overrides the system-wide default Config. Existing
// per-tenant overrides are preserved.
func (l *Limiter) SetDefaults(c Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.defaults = c
}

// Set installs a per-tenant Config override. The bucket is reset so
// the new burst takes effect immediately.
func (l *Limiter) Set(id tenant.ID, c Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configs[id] = c
	delete(l.buckets, id)
}

// configFor returns the active Config for id (override or default).
// Caller must hold l.mu.
func (l *Limiter) configFor(id tenant.ID) Config {
	if c, ok := l.configs[id]; ok {
		return c
	}
	return l.defaults
}

// Allow attempts to consume one token from id's bucket. Returns
// (true, 0) when the request is permitted; (false, retryAfter) when
// denied, where retryAfter is the suggested back-off (rounded up to
// the nearest second, minimum 1s) the HTTP layer stamps into
// Retry-After.
//
// A zero Config (Rate == 0 && Burst == 0) is treated as "unlimited" —
// Allow always returns true.
func (l *Limiter) Allow(id tenant.ID) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cfg := l.configFor(id)
	if cfg.Rate <= 0 && cfg.Burst <= 0 {
		return true, 0
	}
	now := l.now()
	b := l.buckets[id]
	if b == nil {
		b = &bucket{cfg: cfg, tokens: cfg.Burst, last: now}
		l.buckets[id] = b
	} else if b.cfg != cfg {
		// Config changed (operator tuned it); rebuild the bucket
		// rather than carrying stale token state across a config
		// boundary.
		b.cfg = cfg
		b.tokens = cfg.Burst
		b.last = now
	} else {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * cfg.Rate
			if b.tokens > cfg.Burst {
				b.tokens = cfg.Burst
			}
			b.last = now
		}
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// Compute how long until one full token is available again.
	missing := 1 - b.tokens
	if cfg.Rate <= 0 {
		// Refill never happens; surface 60s as a sentinel.
		return false, 60 * time.Second
	}
	secs := missing / cfg.Rate
	d := time.Duration(secs * float64(time.Second))
	// Round up to the nearest whole second, with a 1s floor — HTTP
	// Retry-After is delta-seconds (RFC 9110) and clients shouldn't
	// hammer the door at fractional intervals.
	if d < time.Second {
		d = time.Second
	} else if frac := d % time.Second; frac > 0 {
		d += time.Second - frac
	}
	return false, d
}

// SetClock overrides the wall clock used by the limiter. Test-only.
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}
