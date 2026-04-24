package events

import (
	"context"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Hub is the in-process fan-out broker for GembaEvents. Sources
// (adaptor Subscribe streams, server-side mutation hooks, the budget
// engine) Publish; subscribers (SSE clients, the /metrics aggregator,
// in-process consumers) Subscribe with a Filter and receive only
// matching events.
//
// Backpressure model: each subscriber owns a bounded buffer
// (subBufferSize). On Publish, the hub does a non-blocking send per
// subscriber; if the buffer is full, the slow subscriber is unsubscribed
// and its channel closed. Lag budget (Config.LagBudget) is reserved for
// the future when the hub adds blocking-with-timeout sends; today the
// drop-on-full path is simpler and matches the 'slow clients dropped'
// DoD without a tunable knob.
//
// Per gm-e4.3 DoD: 1000 concurrent subscribers sustained for 5 min
// without leak. Subscribe takes a context; cancellation removes the
// subscriber and closes the channel — no goroutine left around. Hub.Close
// drains every subscriber and stops accepting Publish.
type Hub struct {
	cfg Config

	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed bool
}

// Config tunes hub semantics. Zero-value Config is valid and uses
// sensible defaults; callers usually pass NewHub(Config{}) and only
// override for tests or custom deployments.
type Config struct {
	// SubBufferSize is the per-subscriber channel buffer depth. A
	// subscriber that lags by more than this many events is dropped.
	// Default 64.
	SubBufferSize int
	// LagBudget reserved for future blocking-with-timeout fan-out.
	// Today the hub does non-blocking sends; this field is on the
	// struct so the contract stays stable when the budget knob lands.
	LagBudget time.Duration
}

// subscriber is one consumer of the hub. The hub closes ch when it
// removes the subscriber (ctx cancelled, slow-drop, or hub close).
type subscriber struct {
	filter  Filter
	ch      chan GembaEvent
	cancel  context.CancelFunc
	dropped bool
}

const defaultSubBufferSize = 64

// NewHub constructs a Hub with the given config. NewHub does not start
// any goroutines — Subscribe spawns the per-subscriber teardown
// watchdog; Publish runs synchronously on the caller's goroutine.
func NewHub(cfg Config) *Hub {
	if cfg.SubBufferSize <= 0 {
		cfg.SubBufferSize = defaultSubBufferSize
	}
	return &Hub{
		cfg:  cfg,
		subs: make(map[*subscriber]struct{}),
	}
}

// Subscribe registers a new subscriber and returns its event channel.
// The channel closes when ctx is cancelled, when the subscriber lags
// past the buffer (drop-on-full), or when the hub is Closed. Callers
// SHOULD range over the channel and exit cleanly when it closes.
//
// The returned channel is buffered (Config.SubBufferSize) so a single
// Publish into a busy hub never blocks the publisher on a slow client.
func (h *Hub) Subscribe(ctx context.Context, filter Filter) <-chan GembaEvent {
	sub := &subscriber{
		filter: filter,
		ch:     make(chan GembaEvent, h.cfg.SubBufferSize),
	}
	subCtx, cancel := context.WithCancel(ctx)
	sub.cancel = cancel

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(sub.ch)
		cancel()
		return sub.ch
	}
	h.subs[sub] = struct{}{}
	h.mu.Unlock()

	// Watchdog goroutine — exits when the subscriber's ctx is cancelled
	// (caller unsubscribes) and removes the sub from the hub. If the
	// hub already removed the sub (slow-drop, hub close), the
	// removeSub call is a no-op.
	go func() {
		<-subCtx.Done()
		h.removeSub(sub)
	}()

	return sub.ch
}

// Publish fans an event out to every subscriber whose Filter matches.
// Non-blocking: a subscriber with a full buffer is removed and its
// channel closed. Publish returns after every subscriber has either
// received the event or been dropped — sequential, but each per-sub
// send is O(1) so 1000 subs cost ~microseconds total.
func (h *Hub) Publish(ev GembaEvent) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	// Snapshot subs under the lock so we can release it before doing
	// per-sub work; new subscribers added during the fan-out simply
	// miss this one event (they don't observe history).
	snapshot := make([]*subscriber, 0, len(h.subs))
	for s := range h.subs {
		snapshot = append(snapshot, s)
	}
	h.mu.Unlock()

	var slow []*subscriber
	for _, s := range snapshot {
		if !s.filter.Match(ev) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			slow = append(slow, s)
		}
	}
	for _, s := range slow {
		h.dropSub(s)
	}
}

// AttachOrchestrationStream feeds an adaptor's Subscribe channel into
// the hub. The adaptor's adaptorID stamps Source.AdaptorID on every
// event so multi-adaptor deployments can disambiguate. The pump goroutine
// exits when the input channel closes (adaptor shutdown) or when ctx is
// cancelled.
func (h *Hub) AttachOrchestrationStream(ctx context.Context, adaptorID string, in <-chan core.OrchestrationEvent) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				h.Publish(FromOrchestrationEvent(adaptorID, ev))
			}
		}
	}()
}

// AttachWorkPlaneStream feeds a WorkPlane adaptor's Subscribe channel
// into the hub. Counterpart to AttachOrchestrationStream. gm-e4.3.1.
func (h *Hub) AttachWorkPlaneStream(ctx context.Context, adaptorID string, in <-chan core.WorkPlaneEvent) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				h.Publish(FromWorkPlaneEvent(adaptorID, ev))
			}
		}
	}()
}

// Close drops every subscriber and rejects subsequent Subscribe /
// Publish calls. Idempotent. Used at server shutdown to unstick any
// SSE handler waiting on the hub.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subs := h.subs
	h.subs = nil
	h.mu.Unlock()
	for s := range subs {
		closeOnce(s)
		s.cancel()
	}
}

// SubscriberCount returns the live subscriber count. Useful for
// /metrics and leak-tests.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// removeSub deregisters a subscriber and closes its channel. Safe to
// call multiple times — second call is a no-op.
func (h *Hub) removeSub(s *subscriber) {
	h.mu.Lock()
	if _, ok := h.subs[s]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.subs, s)
	h.mu.Unlock()
	closeOnce(s)
}

// dropSub is removeSub for the slow-client case. Called from Publish
// when a subscriber's buffer is full; marks the subscriber as dropped
// (so a stress test can assert the contract) and tears it down.
func (h *Hub) dropSub(s *subscriber) {
	s.dropped = true
	h.removeSub(s)
	s.cancel()
}

// closeOnce closes a subscriber's channel exactly once. The hub holds
// the only sender so a double-close shows up as a programmer error
// here rather than a runtime panic on a re-fan attempt.
func closeOnce(s *subscriber) {
	defer func() {
		// A double-close panics; recover so concurrent removeSub +
		// dropSub paths don't crash the server.
		_ = recover()
	}()
	close(s.ch)
}
