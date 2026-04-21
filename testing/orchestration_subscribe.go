package gembatesting

import (
	"context"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// probeSubscribeChannel asserts Group D's minimum contract: Subscribe
// returns a usable channel whose lifecycle is bound to ctx. The probe
// cancels ctx and expects the channel to close within a short timeout.
// Event delivery semantics (ordering, backfill, filtering) are out of
// scope — those are adaptor-specific and belong in the adaptor's own
// probe suite.
func probeSubscribeChannel(t probeT, impl core.OrchestrationPlaneAdaptor) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := impl.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if ch == nil {
		t.Fatal("Subscribe: returned nil channel")
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			// Receiving a value is fine — the adaptor may have emitted
			// a trailing event. Drain one more to look for the close.
			select {
			case _, open := <-ch:
				if open {
					t.Error("Subscribe: channel did not close after ctx cancel (delivered a second event instead)")
				}
			case <-time.After(500 * time.Millisecond):
				t.Error("Subscribe: channel did not close within 500ms of ctx cancel")
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Subscribe: channel did not close within 500ms of ctx cancel")
	}
}
