package registry

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthBus_SnapshotSeedsFromSourceOnFirstCall(t *testing.T) {
	calls := int64(0)
	src := func() []AdaptorStatus {
		atomic.AddInt64(&calls, 1)
		return []AdaptorStatus{{Name: "x", Plane: WorkPlane, Healthy: true}}
	}
	b := NewHealthBus(time.Hour, src)
	snap := b.Snapshot()
	if len(snap) != 1 || snap[0].Name != "x" {
		t.Fatalf("snapshot did not seed from source: %+v", snap)
	}
	if atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("first Snapshot must call source exactly once; got %d", calls)
	}

	// Second call should hit cache, not source.
	_ = b.Snapshot()
	if atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("cached Snapshot must not re-call source; got %d", calls)
	}
}

func TestHealthBus_SubscribeReceivesCurrentSnapshotImmediately(t *testing.T) {
	src := func() []AdaptorStatus {
		return []AdaptorStatus{{Name: "beads", Plane: WorkPlane, Healthy: true}}
	}
	b := NewHealthBus(time.Hour, src)
	_ = b.Snapshot() // seed

	ch, cancel := b.Subscribe()
	t.Cleanup(cancel)

	select {
	case frame := <-ch:
		if len(frame) != 1 || frame[0].Name != "beads" || !frame[0].Healthy {
			t.Fatalf("first frame shape wrong: %+v", frame)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber did not receive initial snapshot")
	}
}

func TestHealthBus_PollPublishesOnTransition(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	src := func() []AdaptorStatus {
		return []AdaptorStatus{{Name: "beads", Plane: WorkPlane, Healthy: healthy.Load()}}
	}
	b := NewHealthBus(time.Hour, src)
	_ = b.Snapshot() // seed with Healthy=true

	ch, cancel := b.Subscribe()
	t.Cleanup(cancel)

	// Drain the initial frame.
	<-ch

	// Flip state and Poll.
	healthy.Store(false)
	b.Poll()

	select {
	case frame := <-ch:
		if frame[0].Healthy {
			t.Fatalf("transition frame should carry Healthy=false; got %+v", frame)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber did not receive transition frame")
	}
}

func TestHealthBus_PollSuppressesEventsWhenNothingChanged(t *testing.T) {
	src := func() []AdaptorStatus {
		return []AdaptorStatus{{Name: "x", Plane: WorkPlane, Healthy: true}}
	}
	b := NewHealthBus(time.Hour, src)
	_ = b.Snapshot()

	ch, cancel := b.Subscribe()
	t.Cleanup(cancel)
	<-ch // drain seed frame

	// Polling with identical output must not re-publish.
	b.Poll()
	select {
	case frame := <-ch:
		t.Fatalf("unchanged poll leaked a frame: %+v", frame)
	case <-time.After(50 * time.Millisecond):
		// good — no frame delivered.
	}
}

func TestHealthBus_CancelRemovesSubscriber(t *testing.T) {
	src := func() []AdaptorStatus {
		return []AdaptorStatus{{Name: "x", Plane: WorkPlane, Healthy: true}}
	}
	b := NewHealthBus(time.Hour, src)
	_ = b.Snapshot()

	ch, cancel := b.Subscribe()
	<-ch
	cancel()

	// Second cancel is a no-op.
	cancel()

	// Channel must be closed; reading returns the zero value + ok=false.
	if _, ok := <-ch; ok {
		t.Fatalf("channel should be closed after cancel")
	}

	// Publish shouldn't panic even though there are no subscribers.
	b.Poll()
}

func TestHealthBus_StartStopIsIdempotent(t *testing.T) {
	var calls atomic.Int64
	src := func() []AdaptorStatus {
		calls.Add(1)
		return []AdaptorStatus{{Name: "x", Plane: WorkPlane, Healthy: true}}
	}
	b := NewHealthBus(10*time.Millisecond, src)
	b.Start()
	b.Start() // idempotent

	// Wait long enough for at least two ticks.
	time.Sleep(40 * time.Millisecond)

	b.Stop()
	b.Stop() // idempotent

	if calls.Load() < 2 {
		t.Fatalf("ticker should have fired at least twice in 40ms; got %d", calls.Load())
	}
}
