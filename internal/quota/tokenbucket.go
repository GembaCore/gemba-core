// gm-o9t8.4.2.1 — standalone tier-aware token bucket + per-tenant store.
//
// This file complements the older internal/quota Limiter (still used by
// gm-o9t8.4.1 wiring) with a minimal Bucket that the new tier-aware
// middleware in internal/server/middleware/quota.go can hold directly.
// Two motivations:
//
//   - Tests want a single-purpose token bucket they can drive with a
//     fake clock without depending on the Limiter's per-tenant map.
//   - The new middleware looks up a tenant's tier on every request and
//     constructs its bucket lazily — splitting Bucket from the
//     map-keyed Limiter keeps that path simple.
//
// The BucketStore wraps a map[tenant.ID]*Bucket; production deployments
// will swap this for a Redis-backed implementation, but the in-process
// store is acceptable for single-instance gemba serve (the slice-a
// scope) and for tests.
package quota

import (
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// Bucket is a goroutine-safe token bucket. Tokens accrue at rps per
// second up to burst. The zero value is unusable; obtain a Bucket via
// NewBucket.
type Bucket struct {
	mu     sync.Mutex
	rps    float64
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

// NewBucket returns a Bucket with the given rate and burst. A rps of
// zero or negative is treated as "unlimited" — Allow always returns
// true. burst clamps the maximum bucket size; values <= 0 are treated
// as unlimited as well (defensive: a misconfigured tier should not
// block traffic).
func NewBucket(rps, burst int) *Bucket {
	b := &Bucket{
		rps:   float64(rps),
		burst: float64(burst),
		now:   time.Now,
	}
	b.tokens = b.burst
	b.last = b.now()
	return b
}

// SetClock overrides the wall clock for testing.
func (b *Bucket) SetClock(now func() time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = now
	b.last = now()
}

// Allow consumes one token. Returns true when the request is admitted,
// false when the bucket is empty. Refill happens lazily on every call
// (proportional to elapsed wall-clock since the previous Allow).
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rps <= 0 || b.burst <= 0 {
		return true
	}
	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rps
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RetryAfter reports the wall-clock duration until at least one full
// token will be available. Callers stamp this into the HTTP
// Retry-After header (rounded up to whole seconds, minimum 1s).
func (b *Bucket) RetryAfter() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rps <= 0 {
		return time.Second
	}
	missing := 1 - b.tokens
	if missing <= 0 {
		return 0
	}
	secs := missing / b.rps
	d := time.Duration(secs * float64(time.Second))
	if d < time.Second {
		d = time.Second
	} else if frac := d % time.Second; frac > 0 {
		d += time.Second - frac
	}
	return d
}

// Reset returns the bucket to a full state and snapshots the current
// clock — used by tests, and by the BucketStore when a tenant's tier
// changes underneath an active bucket.
func (b *Bucket) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = b.burst
	b.last = b.now()
}

// BucketStore is a process-local map of tenant.ID -> *Bucket. It is
// the default storage used by WithQuota; production deployments
// override with a Redis-backed implementation (out of scope for the
// gm-o9t8.4.2 slice).
type BucketStore struct {
	mu      sync.Mutex
	buckets map[tenant.ID]*Bucket
}

// NewBucketStore returns an empty store.
func NewBucketStore() *BucketStore {
	return &BucketStore{buckets: map[tenant.ID]*Bucket{}}
}

// Get returns the bucket for id, lazily constructing one with the
// provided rps/burst on first call. Subsequent calls return the same
// instance — even if rps/burst change, the existing bucket is
// preserved so a transient tier read does not silently widen a
// limit. Use Replace to swap the bucket out when the tier genuinely
// changes.
func (s *BucketStore) Get(id tenant.ID, rps, burst int) *Bucket {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.buckets[id]; ok {
		return b
	}
	b := NewBucket(rps, burst)
	s.buckets[id] = b
	return b
}

// Replace installs a fresh bucket for id with the given rate/burst.
// Used by admin handlers when a tenant's tier changes.
func (s *BucketStore) Replace(id tenant.ID, rps, burst int) *Bucket {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := NewBucket(rps, burst)
	s.buckets[id] = b
	return b
}

// Reset returns all buckets to a full state. Used at shutdown to drop
// state cleanly and by tests that want a known-good baseline. The
// in-memory store has no persistence so Reset is effectively a no-op
// followed by lazy rebuild on the next request, but it gives callers
// a hook that future Redis-backed implementations will honor.
func (s *BucketStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.buckets {
		b.Reset()
	}
}

// Len returns the number of tracked tenants. Test-only.
func (s *BucketStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buckets)
}
