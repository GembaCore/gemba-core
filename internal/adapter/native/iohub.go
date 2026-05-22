// Package native: iohub.go is the pure-infra refcounted fan-out the
// OrchestrationPlane's StreamSession (plan B-03) will sit on top of.
//
// Single tmux pipe-pane per session, N subscriber channels, one
// reader goroutine. Refcount semantics live here so plan B-03 stays
// thin: Attach(sessionID) returns a chan; cancel ctx to detach;
// last-subscriber-out triggers backend.StopPipe + FIFO removal.
//
// Opacity (CONTEXT.md C-21): the hub speaks paneID to backend.Streamable
// and sessionID to its caller, with a translation done by the resolver
// closure the OrchestrationPlane injects at construction. The hub
// itself never grep-matches on sessionID shape — keeping it out of the
// backend package preserves the opacity guard core/session_id_opacity_test.go
// enforces.
package native

import (
	"bufio"
	"context"
	"io"
	"os"
	"sync"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
)

// subscriberBuffer is the per-subscriber channel buffer. Large enough
// to absorb a snapshot + a small output burst without blocking the
// reader; small enough that a wedged subscriber drops events quickly
// rather than memory-growing the hub.
const subscriberBuffer = 16

// hubFIFOOpener is the seam for "open the FIFO file for read." In
// production it's os.OpenFile(path, O_RDONLY, 0); tests inject an
// in-memory io.Pipe-backed reader.
type hubFIFOOpener func(path string) (io.ReadCloser, error)

// sessionIOHub multiplexes one Streamable.StartPipe -> N subscriber
// channels per backend session. Construct via newSessionIOHub.
type sessionIOHub struct {
	mu      sync.Mutex
	streams map[string]*hubStream // keyed by paneID, NOT sessionID
	backend backend.Streamable
	// resolver translates the opaque public sessionID -> the
	// backend's internal pane id. Injected by OrchestrationPlane at
	// construction so the hub stays out of the backend package and
	// the opacity test in core/ stays green.
	resolver func(sessionID string) (paneID string, ok bool)
	// snapshotLines bounds the backscroll the hub asks Streamable for
	// on each Attach. Default 2000 per CONTEXT.md.
	snapshotLines int
	// openFIFO is the FIFO-read seam (test override).
	openFIFO hubFIFOOpener
	// closed flips on Close(); Attach refuses new subscribers after.
	closed bool
}

type hubStream struct {
	paneID      string
	fifoPath    string
	subscribers map[*hubSubscriber]struct{}
	cancel      context.CancelFunc // stops the reader goroutine
	done        chan struct{}      // closed when reader exits
	teardown    sync.Once
}

type hubSubscriber struct {
	ch     chan core.SessionEvent
	closed sync.Once
}

// newSessionIOHub constructs a hub. snapshotLines <= 0 defaults to 2000.
func newSessionIOHub(b backend.Streamable, resolver func(string) (string, bool), snapshotLines int) *sessionIOHub {
	if snapshotLines <= 0 {
		snapshotLines = 2000
	}
	return &sessionIOHub{
		streams:       make(map[string]*hubStream),
		backend:       b,
		resolver:      resolver,
		snapshotLines: snapshotLines,
		openFIFO: func(path string) (io.ReadCloser, error) {
			return os.OpenFile(path, os.O_RDONLY, 0)
		},
	}
}

// Attach registers a new subscriber for sessionID. The returned
// channel emits SessionEvents until ctx is cancelled (caller detach),
// the backing session exits, or the hub is closed. The channel is
// always closed by exactly one path; callers can range over it safely.
func (h *sessionIOHub) Attach(ctx context.Context, sessionID string) (<-chan core.SessionEvent, error) {
	paneID, ok := h.resolver(sessionID)
	if !ok {
		return nil, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q not found", sessionID)
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, core.NewAdaptorError(core.KindAdaptorDegraded,
			"native: io hub closed")
	}

	stream, exists := h.streams[paneID]
	if !exists {
		// First subscriber. Open the backend pipe + FIFO BEFORE we
		// register the stream so a failure leaves the hub untouched.
		fifoPath, err := h.backend.StartPipe(ctx, paneID)
		if err != nil {
			h.mu.Unlock()
			return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"native: StartPipe for session %q", sessionID)
		}
		rc, err := h.openFIFO(fifoPath)
		if err != nil {
			// Best-effort: tear down the pipe-pane we just opened.
			_ = h.backend.StopPipe(context.Background(), paneID)
			h.mu.Unlock()
			return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"native: open FIFO %s", fifoPath)
		}
		streamCtx, cancel := context.WithCancel(context.Background())
		stream = &hubStream{
			paneID:      paneID,
			fifoPath:    fifoPath,
			subscribers: make(map[*hubSubscriber]struct{}),
			cancel:      cancel,
			done:        make(chan struct{}),
		}
		h.streams[paneID] = stream
		go h.readLoop(streamCtx, stream, rc)
	}

	sub := &hubSubscriber{ch: make(chan core.SessionEvent, subscriberBuffer)}
	stream.subscribers[sub] = struct{}{}
	h.mu.Unlock()

	// Send the snapshot to THIS subscriber only.
	snap, err := h.backend.SnapshotPane(ctx, paneID, h.snapshotLines)
	if err == nil && len(snap) > 0 {
		sub.ch <- core.SessionEvent{Kind: "output", Bytes: snap}
	}

	// Watch ctx so caller-side cancellation detaches this subscriber.
	go func() {
		<-ctx.Done()
		h.detach(paneID, sub)
	}()

	return sub.ch, nil
}

// readLoop drains the FIFO and fans out to every subscriber. On EOF /
// read error it emits an "exit" event to all subscribers, calls
// StopPipe, removes the FIFO, and deletes the stream entry. Idempotent
// via the stream's teardown sync.Once.
func (h *sessionIOHub) readLoop(ctx context.Context, stream *hubStream, rc io.ReadCloser) {
	defer close(stream.done)
	defer h.teardownStream(stream, rc)

	br := bufio.NewReaderSize(rc, 4096)
	buf := make([]byte, 4096)

	// A small goroutine that closes rc when ctx is cancelled, so the
	// blocking Read in the loop below unblocks.
	go func() {
		<-ctx.Done()
		_ = rc.Close()
	}()

	for {
		n, err := br.Read(buf)
		if n > 0 {
			// Copy the slice so subscribers don't share backing
			// storage with our reusable buf.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			h.fanout(stream, core.SessionEvent{Kind: "output", Bytes: chunk})
		}
		if err != nil {
			// Emit exit / disconnect once; teardown handles cleanup.
			kind := "exit"
			if ctx.Err() != nil {
				kind = "disconnect"
			}
			h.fanout(stream, core.SessionEvent{Kind: kind})
			return
		}
	}
}

// fanout delivers ev to every current subscriber, dropping the event
// for any subscriber whose channel buffer is full (slow subscriber
// protection). MUST hold h.mu across the send: detach/teardown both
// acquire h.mu before closing a subscriber channel, so holding the
// lock here guarantees we never send on a channel another goroutine
// is mid-closing (which would be a panic + race-detector strike).
// The send is non-blocking (select default) so lock-hold time stays
// bounded even under burst output.
func (h *sessionIOHub) fanout(stream *hubStream, ev core.SessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range stream.subscribers {
		select {
		case s.ch <- ev:
		default:
			// Slow subscriber: drop rather than block the reader.
		}
	}
}

// detach removes a subscriber. When the last subscriber leaves, the
// reader goroutine is cancelled (which triggers teardownStream via
// the readLoop's defer). Removing the sub from the map AND closing
// its channel both happen under h.mu so fanout (which also holds h.mu)
// can never race against close(sub.ch).
func (h *sessionIOHub) detach(paneID string, sub *hubSubscriber) {
	h.mu.Lock()
	stream, ok := h.streams[paneID]
	var lastOut bool
	if ok {
		if _, present := stream.subscribers[sub]; present {
			delete(stream.subscribers, sub)
		}
		lastOut = len(stream.subscribers) == 0
	}
	// Close THIS subscriber's channel under the lock so it cannot
	// race a concurrent fanout. sync.Once guards against double-close
	// from the teardown path.
	sub.closed.Do(func() { close(sub.ch) })
	h.mu.Unlock()

	if ok && lastOut {
		// Triggers the reader's ctx-watcher (closes rc) -> read
		// returns -> defer teardownStream runs.
		stream.cancel()
	}
}

// teardownStream closes the FIFO, calls Streamable.StopPipe, removes
// the stream entry, and closes any remaining subscriber channels.
// Idempotent via stream.teardown.
func (h *sessionIOHub) teardownStream(stream *hubStream, rc io.ReadCloser) {
	stream.teardown.Do(func() {
		_ = rc.Close()
		_ = h.backend.StopPipe(context.Background(), stream.paneID)

		h.mu.Lock()
		// Remove the stream entry under lock so Attach cannot race
		// with a stale entry.
		if h.streams[stream.paneID] == stream {
			delete(h.streams, stream.paneID)
		}
		// Close any straggling subscriber channels UNDER the lock so
		// we never race a concurrent fanout (which also holds h.mu
		// across its send). sync.Once handles the case where the
		// per-subscriber ctx-watcher already closed via detach.
		for s := range stream.subscribers {
			sub := s
			sub.closed.Do(func() { close(sub.ch) })
		}
		stream.subscribers = nil
		h.mu.Unlock()
	})
}

// Close shuts every active stream. Used by OrchestrationPlane.Close()
// in plan B-03. Safe to call multiple times.
func (h *sessionIOHub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	streams := make([]*hubStream, 0, len(h.streams))
	for _, s := range h.streams {
		streams = append(streams, s)
	}
	h.mu.Unlock()

	for _, s := range streams {
		s.cancel()
		<-s.done
	}
}
