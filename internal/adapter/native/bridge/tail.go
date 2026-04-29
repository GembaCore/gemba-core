package bridge

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// Tail watches a single session's bridge log and emits
// OrchestrationEvents for every frame it parses. One Tail per live
// session; the Fanout registry (below) multiplexes every live tail
// onto one output channel the adaptor's Subscribe() returns.
//
// Rotation/truncation handling: if the file shrinks between polls
// (or disappears + reappears with a smaller inode), the tailer
// re-opens from offset 0. This is the crash-recovery path — the
// adaptor rebuilds its pending-escalation index from the file as a
// side effect of consuming the events.
type Tail struct {
	path      string
	agentType string
	events    chan<- core.OrchestrationEvent

	// pollInterval is how often the tailer wakes to check for new
	// data when no fsnotify signal is available (we don't take a
	// hard dependency on fsnotify — polling at 100ms is cheap on
	// local disk, and dramatic enough to avoid missed events).
	pollInterval time.Duration

	translate Translator
}

// NewTail builds a Tail for the session. agentType selects the
// translator; unknown agent types use the passthrough so frames are
// never silently dropped.
func NewTail(path, agentType string, events chan<- core.OrchestrationEvent) *Tail {
	return &Tail{
		path:         path,
		agentType:    agentType,
		events:       events,
		pollInterval: 100 * time.Millisecond,
		translate:    ForAgent(agentType),
	}
}

// Run blocks until ctx is canceled, reading frames from the bridge
// log and emitting events. Safe to call with a file that doesn't
// exist yet — the tailer waits for it to appear. Malformed lines
// are logged and skipped, never fatal.
func (t *Tail) Run(ctx context.Context) {
	var (
		fh         *os.File
		rd         *bufio.Reader
		lastInode  uint64
		lastSize   int64
		haveReader bool
	)
	defer func() {
		if fh != nil {
			_ = fh.Close()
		}
	}()

	open := func() {
		fh, _ = os.OpenFile(t.path, os.O_RDONLY|os.O_CREATE, 0o644)
		if fh == nil {
			haveReader = false
			return
		}
		if st, err := fh.Stat(); err == nil {
			lastInode = inodeOf(st)
			lastSize = st.Size()
		}
		rd = bufio.NewReader(fh)
		haveReader = true
	}

	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	open()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !haveReader {
				open()
				continue
			}
			// Rotation detection: if the file shrank or the inode
			// changed, re-open from the start. Catches both log
			// rotation and truncation-on-append.
			if st, err := os.Stat(t.path); err == nil {
				ino := inodeOf(st)
				sz := st.Size()
				if ino != lastInode || sz < lastSize {
					_ = fh.Close()
					open()
				}
				lastSize = sz
			}
			t.drain(ctx, rd)
		}
	}
}

// drain reads available lines from rd and emits their events. io.EOF
// is a "no more data yet" signal, not a fatal error.
func (t *Tail) drain(ctx context.Context, rd *bufio.Reader) {
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			t.handle(ctx, line)
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			slog.Warn("native/bridge: read error", "path", t.path, "err", err)
			return
		}
	}
}

func (t *Tail) handle(ctx context.Context, line []byte) {
	f, ok := ParseLine(line)
	if !ok {
		return
	}
	// Translator may be nil (misconfigured agent type) — drop to
	// passthrough in that case.
	tr := t.translate
	if tr == nil {
		tr = translatePassthrough
	}
	// Use frame's agent_type if it differs from the one we were
	// configured with (e.g. an operator swapped agents mid-session).
	if f.AgentType != "" && f.AgentType != t.agentType {
		tr = ForAgent(f.AgentType)
	}
	events := tr(f)
	for _, e := range events {
		select {
		case <-ctx.Done():
			return
		case t.events <- e:
		}
	}
}

// Fanout multiplexes events from N per-session Tails and broadcasts
// to every registered subscriber plus an optional observer callback.
// Register/Unregister manage per-session tails; Subscribe / SetObserver
// manage consumers.
//
// Why broadcast (not single-queue): the adaptor has TWO consumers
// in the default wiring — the SPA's SSE relay AND the in-adaptor
// escalation index. A single-consumer queue would force one of
// them to miss events.
type Fanout struct {
	mu       sync.Mutex
	tails    map[string]context.CancelFunc
	subs     map[chan core.OrchestrationEvent]struct{}
	input    chan core.OrchestrationEvent
	observer func(core.OrchestrationEvent)

	// out is the single shared channel historical callers expect
	// (kept for backward compatibility — it drains the input too).
	//
	// Deprecated: prefer Subscribe(); Events() still works but uses it.
	out chan core.OrchestrationEvent

	pumpDone chan struct{}
}

// NewFanout returns a running Fanout. A goroutine pumps events from
// the shared input channel out to all subscribers + the observer.
func NewFanout() *Fanout {
	f := &Fanout{
		tails:    make(map[string]context.CancelFunc),
		subs:     make(map[chan core.OrchestrationEvent]struct{}),
		input:    make(chan core.OrchestrationEvent, 256),
		out:      make(chan core.OrchestrationEvent, 256),
		pumpDone: make(chan struct{}),
	}
	go f.pump()
	return f
}

// SetObserver registers an in-process callback fired synchronously
// for every event. Pass nil to clear. Used by the adaptor to update
// its escalation index without standing up a goroutine that races
// the SPA's subscribers.
func (f *Fanout) SetObserver(fn func(core.OrchestrationEvent)) {
	f.mu.Lock()
	f.observer = fn
	f.mu.Unlock()
}

// Subscribe returns a per-caller channel that receives every event
// until ctx is canceled. Slow subscribers drop events to keep the
// pump from stalling — callers that care about every event must
// consume promptly (the SPA relay does).
func (f *Fanout) Subscribe(ctx context.Context) <-chan core.OrchestrationEvent {
	ch := make(chan core.OrchestrationEvent, 64)
	f.mu.Lock()
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
		close(ch)
	}()
	return ch
}

func (f *Fanout) pump() {
	defer close(f.pumpDone)
	for ev := range f.input {
		f.mu.Lock()
		obs := f.observer
		subs := make([]chan core.OrchestrationEvent, 0, len(f.subs))
		for ch := range f.subs {
			subs = append(subs, ch)
		}
		f.mu.Unlock()

		if obs != nil {
			obs(ev)
		}
		for _, ch := range subs {
			select {
			case ch <- ev:
			default:
				// Drop for this subscriber — its ctx cancellation
				// will clean it up soon.
			}
		}
		// Best-effort legacy drain so callers of Events() still
		// see frames. Non-blocking so a stalled reader doesn't
		// back up the pump.
		select {
		case f.out <- ev:
		default:
		}
	}
	// No more input → close legacy out so Events() consumers drain.
	close(f.out)
}

// Register starts a tailer for the given session. Calling Register
// twice for the same sessionID cancels the first. Events flow
// through the Fanout's input channel, then to the pump, then to
// every subscriber + observer.
func (f *Fanout) Register(ctx context.Context, sessionID, agentType string) error {
	path, err := LogPath(sessionID)
	if err != nil {
		return err
	}
	f.mu.Lock()
	if cancel, ok := f.tails[sessionID]; ok {
		cancel()
		delete(f.tails, sessionID)
	}
	f.mu.Unlock()

	tailCtx, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	f.tails[sessionID] = cancel
	f.mu.Unlock()

	go NewTail(path, agentType, f.input).Run(tailCtx)
	return nil
}

// Unregister stops a session's tailer. Safe to call on unknown ids.
func (f *Fanout) Unregister(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cancel, ok := f.tails[sessionID]; ok {
		cancel()
		delete(f.tails, sessionID)
	}
}

// Events returns the receive-only output channel. Never closed
// while the Fanout is alive — callers cancel their own consume
// context to stop.
func (f *Fanout) Events() <-chan core.OrchestrationEvent { return f.out }

// Publish injects an event into the fanout pipeline. Used by the
// adaptor itself to surface synthetic transitions (e.g.
// session_parallel_changed) that aren't sourced from a bridge tail.
// Best-effort drop when the input channel is full so a slow pump
// can never stall a synchronous mutation path.
func (f *Fanout) Publish(ev core.OrchestrationEvent) {
	select {
	case f.input <- ev:
	default:
	}
}

// Close stops every tailer and the pump. After Close the Fanout
// must not be reused.
func (f *Fanout) Close() {
	f.mu.Lock()
	for _, cancel := range f.tails {
		cancel()
	}
	f.tails = map[string]context.CancelFunc{}
	f.mu.Unlock()
	close(f.input)
	<-f.pumpDone
}
