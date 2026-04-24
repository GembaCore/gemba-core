package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestHubFanOut(t *testing.T) {
	h := NewHub(Config{})
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := h.Subscribe(ctx, Filter{})
	b := h.Subscribe(ctx, Filter{})

	ev := GembaEvent{ID: "e1", Kind: SessionTransition, At: time.Now()}
	h.Publish(ev)

	for _, ch := range []<-chan GembaEvent{a, b} {
		select {
		case got := <-ch:
			if got.ID != "e1" {
				t.Errorf("got id %q", got.ID)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestHubFiltersByKind(t *testing.T) {
	h := NewHub(Config{})
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := h.Subscribe(ctx, Filter{Kinds: []Kind{WorkItemUpdated}})

	h.Publish(GembaEvent{ID: "skip", Kind: SessionTransition, At: time.Now()})
	h.Publish(GembaEvent{ID: "keep", Kind: WorkItemUpdated, At: time.Now()})

	got := <-ch
	if got.ID != "keep" {
		t.Errorf("filter let %q through", got.ID)
	}
	// No more events should arrive.
	select {
	case got := <-ch:
		t.Errorf("unexpected extra event: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubUnsubscribeOnContextCancel(t *testing.T) {
	h := NewHub(Config{})
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch := h.Subscribe(ctx, Filter{})
	if h.SubscriberCount() != 1 {
		t.Fatalf("count=%d", h.SubscriberCount())
	}
	cancel()
	// channel must close once the watchdog runs.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				if h.SubscriberCount() != 0 {
					t.Errorf("count=%d after close", h.SubscriberCount())
				}
				return
			}
		case <-deadline:
			t.Fatal("channel did not close after ctx cancel")
		}
	}
}

func TestHubSlowSubscriberDropped(t *testing.T) {
	h := NewHub(Config{SubBufferSize: 2})
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := h.Subscribe(ctx, Filter{})

	// Send 10 events without reading. Buffer is 2, so events 3..10
	// should drop the subscriber.
	for i := 0; i < 10; i++ {
		h.Publish(GembaEvent{ID: "x", Kind: SessionTransition, At: time.Now()})
	}
	// Allow the dropper to run.
	time.Sleep(50 * time.Millisecond)
	if h.SubscriberCount() != 0 {
		t.Errorf("slow sub not dropped: count=%d", h.SubscriberCount())
	}
	// Drain whatever's buffered + see channel close.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel did not close after slow drop")
		}
	}
}

func TestHubCloseDropsAll(t *testing.T) {
	h := NewHub(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := h.Subscribe(ctx, Filter{})
	b := h.Subscribe(ctx, Filter{})

	h.Close()
	for _, ch := range []<-chan GembaEvent{a, b} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Error("expected channel close")
			}
		case <-time.After(time.Second):
			t.Fatal("channel did not close on hub Close")
		}
	}
	// Subscribe-after-close returns an already-closed channel.
	ch := h.Subscribe(ctx, Filter{})
	if _, ok := <-ch; ok {
		t.Error("subscribe-after-close should yield a closed channel")
	}
}

func TestHubAttachOrchestrationStream(t *testing.T) {
	h := NewHub(Config{})
	defer h.Close()

	in := make(chan core.OrchestrationEvent, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.AttachOrchestrationStream(ctx, "native", in)

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	out := h.Subscribe(subCtx, Filter{Kinds: []Kind{SessionTransition}})

	in <- core.OrchestrationEvent{ID: "1", Kind: "session_transition", At: time.Now(), SessionID: "s-1"}
	in <- core.OrchestrationEvent{ID: "2", Kind: "user_message", At: time.Now()} // filtered out

	got := <-out
	if got.Kind != SessionTransition || got.Source.AdaptorID != "native" {
		t.Errorf("got %+v", got)
	}
	if got.SessionID != "s-1" {
		t.Errorf("scope: %+v", got)
	}
}

// TestHub1000ConcurrentSubscribers exercises the bead's DoD shape
// (1000 concurrent subscribers without leak). Not a load test —
// ramps up subs, verifies fan-out, verifies clean teardown.
func TestHub1000ConcurrentSubscribers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1000-sub test under -short")
	}
	const N = 1000
	h := NewHub(Config{SubBufferSize: 16})
	defer h.Close()

	ctxs := make([]context.CancelFunc, N)
	chans := make([]<-chan GembaEvent, N)
	for i := 0; i < N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctxs[i] = cancel
		chans[i] = h.Subscribe(ctx, Filter{})
	}
	if got := h.SubscriberCount(); got != N {
		t.Fatalf("subscribed %d, hub knows %d", N, got)
	}

	// Fan one event; every sub must receive.
	h.Publish(GembaEvent{ID: "broadcast", Kind: SessionTransition, At: time.Now()})

	var wg sync.WaitGroup
	wg.Add(N)
	for _, ch := range chans {
		go func(c <-chan GembaEvent) {
			defer wg.Done()
			select {
			case <-c:
			case <-time.After(2 * time.Second):
				t.Errorf("subscriber missed event")
			}
		}(ch)
	}
	wg.Wait()

	// Tear them all down — count returns to zero.
	for _, c := range ctxs {
		c()
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.SubscriberCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("subs leaked: count=%d after teardown", h.SubscriberCount())
}
