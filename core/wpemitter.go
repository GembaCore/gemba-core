package core

import (
	"context"
	"sync"
)

// WorkPlaneEmitter is the fan-out helper every WorkPlane adaptor that
// opts into Subscribe embeds. It maintains a set of active subscribers
// and non-blocking-publishes each WorkPlaneEvent to every matching
// subscriber; a subscriber whose buffer is full is dropped (its channel
// closed) per the interface contract. gm-e4.3.1.
//
// Split out of the individual adaptor files so bd, testadaptors, and
// any future emitting adaptor don't duplicate fan-out / filter /
// teardown code.
type WorkPlaneEmitter struct {
	mu   sync.Mutex
	subs map[*wpSub]struct{}
}

type wpSub struct {
	filter WorkPlaneSubscribeFilter
	ch     chan WorkPlaneEvent
	cancel context.CancelFunc
}

// WorkPlaneEmitterBuffer is the per-subscriber channel buffer depth.
// Matches the hub's default — the fan-out chains hub → client are the
// real drop point, not this inner emitter, so we keep the buffer modest.
const WorkPlaneEmitterBuffer = 64

// NewWorkPlaneEmitter returns a ready-to-use emitter.
func NewWorkPlaneEmitter() *WorkPlaneEmitter {
	return &WorkPlaneEmitter{subs: make(map[*wpSub]struct{})}
}

// Subscribe registers a new subscriber. The returned channel closes
// when ctx is cancelled, the subscriber lags past its buffer, or the
// emitter is Closed.
func (e *WorkPlaneEmitter) Subscribe(ctx context.Context, f WorkPlaneSubscribeFilter) <-chan WorkPlaneEvent {
	s := &wpSub{filter: f, ch: make(chan WorkPlaneEvent, WorkPlaneEmitterBuffer)}
	subCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	e.mu.Lock()
	e.subs[s] = struct{}{}
	e.mu.Unlock()
	go func() {
		<-subCtx.Done()
		e.remove(s)
	}()
	return s.ch
}

// Publish fans ev out to every matching subscriber with a non-blocking
// send. Callers MUST call Publish after the underlying mutation has
// successfully landed — never before, so a failed mutation cannot leak
// a ghost event.
func (e *WorkPlaneEmitter) Publish(ev WorkPlaneEvent) {
	e.mu.Lock()
	snapshot := make([]*wpSub, 0, len(e.subs))
	for s := range e.subs {
		snapshot = append(snapshot, s)
	}
	e.mu.Unlock()
	var slow []*wpSub
	for _, s := range snapshot {
		if !matchWorkPlaneFilter(s.filter, ev) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			slow = append(slow, s)
		}
	}
	for _, s := range slow {
		e.remove(s)
	}
}

// Close drops every subscriber. Idempotent; safe at adaptor shutdown.
func (e *WorkPlaneEmitter) Close() {
	e.mu.Lock()
	subs := e.subs
	e.subs = make(map[*wpSub]struct{})
	e.mu.Unlock()
	for s := range subs {
		closeWpSub(s)
		s.cancel()
	}
}

// SubscriberCount exposes the live subscriber count for leak-tests.
func (e *WorkPlaneEmitter) SubscriberCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.subs)
}

func (e *WorkPlaneEmitter) remove(s *wpSub) {
	e.mu.Lock()
	if _, ok := e.subs[s]; !ok {
		e.mu.Unlock()
		return
	}
	delete(e.subs, s)
	e.mu.Unlock()
	closeWpSub(s)
	s.cancel()
}

func closeWpSub(s *wpSub) {
	defer func() { _ = recover() }()
	close(s.ch)
}

func matchWorkPlaneFilter(f WorkPlaneSubscribeFilter, ev WorkPlaneEvent) bool {
	if len(f.Kinds) > 0 {
		hit := false
		for _, k := range f.Kinds {
			if k == ev.Kind {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if f.WorkItemID != "" && f.WorkItemID != ev.WorkItemID {
		return false
	}
	if f.Since != nil && ev.At.Before(*f.Since) {
		return false
	}
	return true
}
