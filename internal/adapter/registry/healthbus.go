package registry

import (
	"sync"
	"time"
)

// HealthBus is a shared cache + pub/sub broker over a source of
// AdaptorStatus slices (typically `registry.Status` or the server's
// bound-adaptor variant). Probes run on a background ticker owned by
// the bus, not once per `/api/adaptors` hit — the SSE handler and the
// fallback JSON endpoint both read from the same cached snapshot, so
// a hundred open tabs don't each trigger fresh Dolt pings.
//
// Lifecycle:
//
//   - NewHealthBus constructs the bus but does NOT start the ticker.
//   - Start() is idempotent and spawns the ticker goroutine. Safe to
//     call from multiple sites (e.g. both the SSE handler and the
//     serve-command bootstrap).
//   - Stop() cancels the ticker and waits for the goroutine to exit.
//   - Snapshot() returns the latest cached status. On a fresh bus it
//     synchronously calls the source once so a /api/adaptors hit before
//     the first tick doesn't return an empty envelope.
//   - Subscribe() returns a buffered channel that receives updates on
//     health transitions. The bus sends the current snapshot on the
//     channel immediately so a new subscriber never waits a full tick
//     to render. The returned cancel removes the subscription.
//
// HealthBus is safe for concurrent use.
type HealthBus struct {
	interval time.Duration
	source   func() []AdaptorStatus

	mu    sync.Mutex
	subs  map[chan []AdaptorStatus]struct{}
	snap  []AdaptorStatus
	seeded bool

	startMu sync.Mutex
	started bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewHealthBus wires a bus against the given source. interval is the
// ticker cadence once Start is called; source is invoked both from the
// ticker and from the initial Snapshot() to seed the cache. Callers
// that want the cache populated at construction time should call
// Snapshot once on the returned bus.
func NewHealthBus(interval time.Duration, source func() []AdaptorStatus) *HealthBus {
	return &HealthBus{
		interval: interval,
		source:   source,
		subs:     map[chan []AdaptorStatus]struct{}{},
	}
}

// Start spawns the probe goroutine. Safe to call multiple times —
// subsequent calls are no-ops. When interval <= 0 Start returns
// without starting anything; tests that want to drive the bus
// manually can rely on Snapshot + publish() without a ticker.
func (b *HealthBus) Start() {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.started || b.interval <= 0 {
		return
	}
	b.started = true
	b.stopCh = make(chan struct{})
	b.doneCh = make(chan struct{})
	go b.run()
}

// Stop signals the ticker goroutine to exit and waits for it to
// return. Safe to call when Start was never invoked.
func (b *HealthBus) Stop() {
	b.startMu.Lock()
	if !b.started {
		b.startMu.Unlock()
		return
	}
	b.started = false
	close(b.stopCh)
	done := b.doneCh
	b.startMu.Unlock()
	<-done
}

func (b *HealthBus) run() {
	defer close(b.doneCh)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	// Prime the cache so the first SSE subscriber doesn't wait a full
	// interval for a snapshot.
	b.Poll()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.Poll()
		}
	}
}

// Poll runs the source function once, updates the cache, and publishes
// the result to subscribers if it changed. Exposed (exported) so tests
// can drive the bus deterministically without waiting on a ticker.
func (b *HealthBus) Poll() []AdaptorStatus {
	next := b.source()
	b.mu.Lock()
	changed := !b.seeded || !statusesEqual(b.snap, next)
	b.snap = cloneStatuses(next)
	b.seeded = true
	var targets []chan []AdaptorStatus
	if changed {
		targets = make([]chan []AdaptorStatus, 0, len(b.subs))
		for ch := range b.subs {
			targets = append(targets, ch)
		}
	}
	b.mu.Unlock()
	for _, ch := range targets {
		deliver(ch, next)
	}
	return next
}

// Snapshot returns the cached status. If the cache is empty the bus
// runs Poll() once synchronously so a caller during the boot window
// (before the ticker has fired) still gets a useful answer.
func (b *HealthBus) Snapshot() []AdaptorStatus {
	b.mu.Lock()
	seeded := b.seeded
	snap := b.snap
	b.mu.Unlock()
	if seeded {
		return cloneStatuses(snap)
	}
	return b.Poll()
}

// Subscribe returns a buffered channel that the bus writes to on every
// health transition. The current snapshot is sent immediately so a new
// subscriber can render without waiting a tick. The returned cancel
// removes the subscription and closes the channel; calling it more
// than once is safe.
func (b *HealthBus) Subscribe() (<-chan []AdaptorStatus, func()) {
	ch := make(chan []AdaptorStatus, 8)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	seeded := b.seeded
	snap := cloneStatuses(b.snap)
	b.mu.Unlock()

	if seeded {
		deliver(ch, snap)
	} else {
		// Prime synchronously so first frame isn't empty.
		go func() { b.Snapshot() }()
	}

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subs[ch]; ok {
				delete(b.subs, ch)
				close(ch)
			}
			b.mu.Unlock()
		})
	}
	return ch, cancel
}

// deliver writes to a subscriber channel with a best-effort drop on
// overflow. Slow consumers lose intermediate frames rather than
// blocking the publisher — the most recent state is what the SPA
// banner needs; missed transitions will be superseded by the next one.
func deliver(ch chan<- []AdaptorStatus, s []AdaptorStatus) {
	select {
	case ch <- cloneStatuses(s):
	default:
		// Drop. The subscriber will catch up on the next transition.
	}
}

func cloneStatuses(in []AdaptorStatus) []AdaptorStatus {
	out := make([]AdaptorStatus, len(in))
	copy(out, in)
	return out
}

// statusesEqual compares two slices element-wise. AdaptorStatus is a
// value type; == suffices.
func statusesEqual(a, b []AdaptorStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
