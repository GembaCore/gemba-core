// Package native: iohub_test.go locks the refcounted fan-out
// semantics of sessionIOHub (gm-v01.3.4). All tests use a fake
// Streamable + injectable FIFO opener so they run cross-platform
// without touching a real named pipe.
package native

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// fakeStreamable is the in-memory test double for backend.Streamable.
// StartPipe returns a fake path; the hub's openFIFO override hooks the
// fake's read-pipe so the reader goroutine sees bytes the test injects
// via Write().
type fakeStreamable struct {
	mu             sync.Mutex
	StartCount     int64
	StopCount      int64
	RemoveCount    int64
	StartErr       error
	SnapshotBytes  []byte
	openedReaders  map[string]*io.PipeReader // keyed by paneID
	openedWriters  map[string]*io.PipeWriter
	currentPath    map[string]string
}

func newFakeStreamable() *fakeStreamable {
	return &fakeStreamable{
		SnapshotBytes: []byte("snapshot"),
		openedReaders: map[string]*io.PipeReader{},
		openedWriters: map[string]*io.PipeWriter{},
		currentPath:   map[string]string{},
	}
}

func (f *fakeStreamable) StartPipe(_ context.Context, paneID string) (string, error) {
	atomic.AddInt64(&f.StartCount, 1)
	if f.StartErr != nil {
		return "", f.StartErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if path, ok := f.currentPath[paneID]; ok {
		return path, nil
	}
	r, w := io.Pipe()
	f.openedReaders[paneID] = r
	f.openedWriters[paneID] = w
	path := "/fake/fifo/" + paneID
	f.currentPath[paneID] = path
	return path, nil
}

func (f *fakeStreamable) StopPipe(_ context.Context, paneID string) error {
	atomic.AddInt64(&f.StopCount, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if w, ok := f.openedWriters[paneID]; ok {
		_ = w.Close() // signals EOF to the reader goroutine
	}
	delete(f.openedWriters, paneID)
	delete(f.openedReaders, paneID)
	if _, ok := f.currentPath[paneID]; ok {
		atomic.AddInt64(&f.RemoveCount, 1)
		delete(f.currentPath, paneID)
	}
	return nil
}

func (f *fakeStreamable) SnapshotPane(_ context.Context, _ string, _ int) ([]byte, error) {
	return f.SnapshotBytes, nil
}

// Write injects bytes into the pane's fake FIFO. Blocks until the
// reader consumes them (io.Pipe semantics).
func (f *fakeStreamable) Write(paneID string, b []byte) (int, error) {
	f.mu.Lock()
	w := f.openedWriters[paneID]
	f.mu.Unlock()
	if w == nil {
		return 0, errors.New("no writer for pane " + paneID)
	}
	return w.Write(b)
}

// readerFor returns the read end the hub should use. Wires through
// the injectable openFIFO hook.
func (f *fakeStreamable) readerFor(paneID string) io.ReadCloser {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openedReaders[paneID]
}

// newTestHub builds a hub with the fake backend + a resolver that maps
// every sessionID to "pane-"+sessionID. openFIFO is wired to the
// fake's in-memory reader.
func newTestHub(fake *fakeStreamable, resolver func(string) (string, bool)) *sessionIOHub {
	if resolver == nil {
		resolver = func(sid string) (string, bool) {
			return "pane-" + sid, true
		}
	}
	h := newSessionIOHub(fake, resolver, 2000)
	h.openFIFO = func(path string) (io.ReadCloser, error) {
		// path is "/fake/fifo/<paneID>"; derive paneID from the suffix
		// "pane-<sid>" if present.
		// The fake keeps the reader keyed by paneID; the FIFO path
		// embeds it. Strip the prefix.
		const pfx = "/fake/fifo/"
		paneID := path[len(pfx):]
		rc := fake.readerFor(paneID)
		if rc == nil {
			return nil, errors.New("no reader for " + paneID)
		}
		return rc, nil
	}
	return h
}

func TestIOHub_SnapshotOnlySingleSubscriber(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := h.Attach(ctx, "sess1")
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// First event is the snapshot
	ev := <-ch
	if ev.Kind != "output" || string(ev.Bytes) != "snapshot" {
		t.Errorf("expected snapshot output event, got %+v", ev)
	}
	cancel()
	// channel should close
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				goto done
			}
		case <-deadline:
			t.Fatal("channel never closed after cancel")
		}
	}
done:
	// Allow tear-down to run
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt64(&fake.StopCount) != 1 {
		t.Errorf("StopPipe count: got %d want 1", fake.StopCount)
	}
	if atomic.LoadInt64(&fake.RemoveCount) != 1 {
		t.Errorf("RemoveCount: got %d want 1", fake.RemoveCount)
	}
}

func TestIOHub_TwoSubscribersOneStartPipe(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch1, err := h.Attach(ctx1, "sess1")
	if err != nil {
		t.Fatalf("Attach 1: %v", err)
	}
	ch2, err := h.Attach(ctx2, "sess1")
	if err != nil {
		t.Fatalf("Attach 2: %v", err)
	}
	// Each gets its own snapshot
	if ev := <-ch1; ev.Kind != "output" {
		t.Errorf("sub1 snapshot: %+v", ev)
	}
	if ev := <-ch2; ev.Kind != "output" {
		t.Errorf("sub2 snapshot: %+v", ev)
	}
	if got := atomic.LoadInt64(&fake.StartCount); got != 1 {
		t.Errorf("StartPipe count: got %d want 1", got)
	}
	// Writes fan out to both
	go func() { _, _ = fake.Write("pane-sess1", []byte("hello")) }()
	ev1 := <-ch1
	ev2 := <-ch2
	if string(ev1.Bytes) != "hello" || string(ev2.Bytes) != "hello" {
		t.Errorf("fan-out mismatch: sub1=%q sub2=%q", ev1.Bytes, ev2.Bytes)
	}
}

func TestIOHub_OneDetachKeepsStreamLive(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch1, err := h.Attach(ctx1, "sess1")
	if err != nil {
		t.Fatalf("Attach 1: %v", err)
	}
	ch2, err := h.Attach(ctx2, "sess1")
	if err != nil {
		t.Fatalf("Attach 2: %v", err)
	}
	<-ch1
	<-ch2
	cancel1()
	// Drain any final events on ch1 until close
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&fake.StopCount); got != 0 {
		t.Errorf("StopPipe must NOT fire while one subscriber remains; got %d", got)
	}
	// ch2 still receives writes
	go func() { _, _ = fake.Write("pane-sess1", []byte("after-detach")) }()
	select {
	case ev := <-ch2:
		if string(ev.Bytes) != "after-detach" {
			t.Errorf("remaining sub event: %q", ev.Bytes)
		}
	case <-time.After(time.Second):
		t.Fatal("remaining subscriber did not receive write")
	}
}

func TestIOHub_BothDetachStopsOnce(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	ch1, _ := h.Attach(ctx1, "sess1")
	ch2, _ := h.Attach(ctx2, "sess1")
	<-ch1
	<-ch2
	cancel1()
	cancel2()
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt64(&fake.StopCount); got != 1 {
		t.Errorf("StopPipe count: got %d want 1", got)
	}
	if got := atomic.LoadInt64(&fake.RemoveCount); got != 1 {
		t.Errorf("RemoveCount: got %d want 1", got)
	}
	// stream entry should be gone
	h.mu.Lock()
	n := len(h.streams)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("streams map should be empty; got %d", n)
	}
}

func TestIOHub_DisconnectStorm(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	startG := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			ch, err := h.Attach(ctx, "sess1")
			if err != nil {
				cancel()
				return
			}
			<-ch // consume snapshot
			cancel()
			// drain until close
			for range ch {
			}
		}()
	}
	wg.Wait()
	// Settle window for tear-down goroutines
	time.Sleep(200 * time.Millisecond)

	h.mu.Lock()
	streamN := len(h.streams)
	h.mu.Unlock()
	if streamN != 0 {
		t.Errorf("streams map should be empty after storm; got %d", streamN)
	}
	start := atomic.LoadInt64(&fake.StartCount)
	rm := atomic.LoadInt64(&fake.RemoveCount)
	if rm != start {
		t.Errorf("RemoveCount(%d) != StartCount(%d) — FIFO leak suspected", rm, start)
	}
	endG := runtime.NumGoroutine()
	// Allow some scheduler slack but no big leak.
	if endG-startG > 5 {
		t.Errorf("goroutine leak: start=%d end=%d", startG, endG)
	}
}

func TestIOHub_SlowSubscriberDoesNotBlock(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	ctxSlow, cancelSlow := context.WithCancel(context.Background())
	defer cancelSlow()
	ctxFast, cancelFast := context.WithCancel(context.Background())
	defer cancelFast()
	chSlow, _ := h.Attach(ctxSlow, "sess1")
	chFast, _ := h.Attach(ctxFast, "sess1")
	<-chSlow
	<-chFast
	// Slow sub never reads. Fast sub should still receive bursts.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_, _ = fake.Write("pane-sess1", []byte{byte('a' + i%26)})
		}
	}()
	received := 0
	timeout := time.After(2 * time.Second)
	for received < 10 {
		select {
		case ev, ok := <-chFast:
			if !ok {
				t.Fatal("fast channel closed unexpectedly")
			}
			if ev.Kind == "output" {
				received++
			}
		case <-timeout:
			t.Fatalf("fast subscriber received only %d events", received)
		}
	}
	<-done
}

func TestIOHub_ResolverNotFound(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, func(string) (string, bool) { return "", false })
	_, err := h.Attach(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindSessionNotFound {
		t.Errorf("expected KindSessionNotFound, got %v", err)
	}
}

func TestIOHub_StartPipeErrorWraps(t *testing.T) {
	fake := newFakeStreamable()
	fake.StartErr = errors.New("simulated mkfifo failure")
	h := newTestHub(fake, nil)
	_, err := h.Attach(context.Background(), "sess1")
	if err == nil {
		t.Fatal("expected StartPipe error to surface")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("expected KindAdaptorDegraded, got %v", err)
	}
	h.mu.Lock()
	n := len(h.streams)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("no stream should be recorded on failure; got %d", n)
	}
}

func TestIOHub_CloseStopsAllStreams(t *testing.T) {
	fake := newFakeStreamable()
	h := newTestHub(fake, nil)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch1, _ := h.Attach(ctx1, "sess1")
	ch2, _ := h.Attach(ctx2, "sess2")
	<-ch1
	<-ch2
	h.Close()
	// All subscriber channels close
	for i, ch := range []<-chan core.SessionEvent{ch1, ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				// drain until closed
				for range ch {
				}
			}
		case <-time.After(time.Second):
			t.Errorf("ch%d did not close after hub.Close()", i+1)
		}
	}
	if got := atomic.LoadInt64(&fake.StopCount); got != 2 {
		t.Errorf("StopPipe count after Close: got %d want 2", got)
	}
}
