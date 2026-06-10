// Package native: session_io_stream_test.go locks the StreamSession
// override on OrchestrationPlane (Plan B-03, gm-v01.3.5). These tests
// build on the fakeStreamable + injectable FIFO opener shipped in
// iohub_test.go (Plan B-02) — kept in the same package so the helpers
// are reusable without an exported testutil shim.
package native

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
)

// streamFakeBackend is a Backend that also satisfies Streamable. The
// embedded fakeStreamable carries the StartPipe / StopPipe / Snapshot
// implementation from iohub_test.go; Name() and the Backend stub
// methods round it out so OrchestrationPlane.NewWithConfig will accept
// it as cfg.Backend.
type streamFakeBackend struct {
	*fakeStreamable
	name string
}

func newStreamFakeBackend(name string) *streamFakeBackend {
	return &streamFakeBackend{
		fakeStreamable: newFakeStreamable(),
		name:           name,
	}
}

func (s *streamFakeBackend) Name() string { return s.name }

func (s *streamFakeBackend) ListPanes(context.Context) ([]backend.Pane, error) {
	return nil, nil
}
func (s *streamFakeBackend) SpawnPane(context.Context, backend.SpawnSpec) (backend.Pane, error) {
	return backend.Pane{}, errors.New("not implemented")
}
func (s *streamFakeBackend) SendKeys(context.Context, string, string) error { return nil }
func (s *streamFakeBackend) SendInput(context.Context, string, core.SessionInput) error {
	return nil
}
func (s *streamFakeBackend) CapturePane(context.Context, string, int) (string, error) {
	return "", nil
}
func (s *streamFakeBackend) Kill(context.Context, string) error { return nil }

// plainFakeBackend is a Backend that does NOT satisfy Streamable. Used
// to assert StreamSession returns KindUnsupported when the backend
// cannot stream.
type plainFakeBackend struct {
	name string
}

func (p *plainFakeBackend) Name() string                                { return p.name }
func (p *plainFakeBackend) ListPanes(context.Context) ([]backend.Pane, error) {
	return nil, nil
}
func (p *plainFakeBackend) SpawnPane(context.Context, backend.SpawnSpec) (backend.Pane, error) {
	return backend.Pane{}, errors.New("not implemented")
}
func (p *plainFakeBackend) SendKeys(context.Context, string, string) error { return nil }
func (p *plainFakeBackend) SendInput(context.Context, string, core.SessionInput) error {
	return nil
}
func (p *plainFakeBackend) CapturePane(context.Context, string, int) (string, error) {
	return "", nil
}
func (p *plainFakeBackend) Kill(context.Context, string) error { return nil }

// newPlaneWithStreamableBackend constructs an OrchestrationPlane wired
// to a streamFakeBackend, with the hub's FIFO opener swapped for the
// fake's in-memory io.Pipe reader so reader-goroutine reads see
// injected bytes. Reaper/reconcile are disabled to keep tests
// deterministic.
func newPlaneWithStreamableBackend(t *testing.T, fake *streamFakeBackend) *OrchestrationPlane {
	t.Helper()
	p := NewWithConfig(Config{
		Backend:          fake,
		DisableReaper:    true,
		DisableReconcile: true,
	})
	if p.hub == nil {
		t.Fatalf("expected hub to be constructed for Streamable backend; got nil")
	}
	// Swap the FIFO opener for the fake's reader. Path format matches
	// the streamFakeBackend's StartPipe: "/fake/fifo/<paneID>".
	p.hub.openFIFO = func(path string) (io.ReadCloser, error) {
		const pfx = "/fake/fifo/"
		paneID := path[len(pfx):]
		rc := fake.readerFor(paneID)
		if rc == nil {
			return nil, errors.New("no reader for " + paneID)
		}
		return rc, nil
	}
	return p
}

// installFakeSession bypasses StartSession and pokes a Session into the
// plane's session map with the given pane_id. Mimics what the real
// StartSession path does, minus all the bridge/worktree/backend spawn
// work.
func installFakeSession(o *OrchestrationPlane, sessionID, paneID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessions[sessionID] = &core.Session{
		ID:     sessionID,
		Status: core.SessionReady,
		ProviderMetadata: map[string]any{
			"pane_id": paneID,
		},
	}
}

func TestStreamSession_HappyPath_SnapshotThenLiveThenCancel(t *testing.T) {
	fake := newStreamFakeBackend("tmux-fake")
	o := newPlaneWithStreamableBackend(t, fake)
	defer o.Close()
	installFakeSession(o, "sess-A", "%0")

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := o.StreamSession(ctx, "sess-A")
	if err != nil {
		t.Fatalf("StreamSession: %v", err)
	}
	// 1: snapshot
	select {
	case ev := <-ch:
		if ev.Kind != "output" || string(ev.Bytes) != "snapshot" {
			t.Fatalf("expected snapshot output, got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("never received snapshot")
	}
	// 2: live bytes
	go func() { _, _ = fake.Write("%0", []byte("live")) }()
	select {
	case ev := <-ch:
		if ev.Kind != "output" || string(ev.Bytes) != "live" {
			t.Fatalf("expected live output, got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("never received live output")
	}
	// 3: cancel -> channel closes
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel never closed after cancel")
		}
	}
}

func TestStreamSession_TwoSubscribers_OneStartPipe_OneStopPipe(t *testing.T) {
	fake := newStreamFakeBackend("tmux-fake")
	o := newPlaneWithStreamableBackend(t, fake)
	defer o.Close()
	installFakeSession(o, "sess-B", "%1")

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	ch1, err := o.StreamSession(ctx1, "sess-B")
	if err != nil {
		t.Fatalf("Attach 1: %v", err)
	}
	ch2, err := o.StreamSession(ctx2, "sess-B")
	if err != nil {
		t.Fatalf("Attach 2: %v", err)
	}
	// Each gets a snapshot.
	if ev := <-ch1; ev.Kind != "output" {
		t.Errorf("sub1 snapshot: %+v", ev)
	}
	if ev := <-ch2; ev.Kind != "output" {
		t.Errorf("sub2 snapshot: %+v", ev)
	}
	if got := atomic.LoadInt64(&fake.StartCount); got != 1 {
		t.Errorf("StartPipe count: got %d want 1", got)
	}
	// Detach both -> single StopPipe.
	cancel1()
	cancel2()
	// drain
	for range ch1 {
	}
	for range ch2 {
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&fake.StopCount); got != 1 {
		t.Errorf("StopPipe count: got %d want 1", got)
	}
}

func TestStreamSession_BackendNil_Unsupported(t *testing.T) {
	o := NewWithConfig(Config{})
	defer o.Close()
	_, err := o.StreamSession(context.Background(), "sess-X")
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindUnsupported {
		t.Errorf("expected KindUnsupported, got %v", err)
	}
}

func TestStreamSession_BackendNotStreamable_UnsupportedNamesBackend(t *testing.T) {
	p := &plainFakeBackend{name: "noop-test"}
	o := NewWithConfig(Config{
		Backend:          p,
		DisableReaper:    true,
		DisableReconcile: true,
	})
	defer o.Close()
	if o.hub != nil {
		t.Fatalf("hub should be nil for non-Streamable backend")
	}
	_, err := o.StreamSession(context.Background(), "sess-X")
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindUnsupported {
		t.Fatalf("expected KindUnsupported, got %v", err)
	}
	if !contains(ae.Error(), "noop-test") {
		t.Errorf("error should name backend %q; got %q", "noop-test", ae.Error())
	}
}

func TestStreamSession_EmptySessionID_Validation(t *testing.T) {
	fake := newStreamFakeBackend("tmux-fake")
	o := newPlaneWithStreamableBackend(t, fake)
	defer o.Close()
	_, err := o.StreamSession(context.Background(), "")
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindValidation {
		t.Errorf("expected KindValidation, got %v", err)
	}
}

func TestStreamSession_UnknownSessionID_NotFound(t *testing.T) {
	fake := newStreamFakeBackend("tmux-fake")
	o := newPlaneWithStreamableBackend(t, fake)
	defer o.Close()
	_, err := o.StreamSession(context.Background(), "ghost-session")
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindSessionNotFound {
		t.Errorf("expected KindSessionNotFound, got %v", err)
	}
}

func TestStreamSession_CloseClosesSubscriberAndFastFailsAfter(t *testing.T) {
	fake := newStreamFakeBackend("tmux-fake")
	o := newPlaneWithStreamableBackend(t, fake)
	installFakeSession(o, "sess-C", "%2")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := o.StreamSession(ctx, "sess-C")
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// Drain snapshot
	<-ch
	o.Close()
	// Subscriber channel should close.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				goto closed
			}
		case <-deadline:
			t.Fatal("subscriber channel did not close after Close()")
		}
	}
closed:
	// After Close, hub set to nil; subsequent StreamSession must
	// fast-fail KindUnsupported (the plane has shut down its streaming
	// surface).
	_, err = o.StreamSession(context.Background(), "sess-C")
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindUnsupported {
		t.Errorf("expected KindUnsupported after Close, got %v", err)
	}
	// Idempotent Close.
	o.Close()
}

// contains is a tiny strings.Contains shim that keeps the import list
// minimal (no extra import needed for one substring check).
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
