package evidence

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

// fakeCollector is a Collector double whose Collect returns a fixed
// result. It records the number of times it was invoked.
type fakeCollector struct {
	src    Source
	out    []core.Evidence
	err    error
	calls  atomic.Int32
	panics bool
}

func (f *fakeCollector) Source() Source { return f.src }

func (f *fakeCollector) Collect(ctx context.Context, t Target) ([]core.Evidence, error) {
	f.calls.Add(1)
	if f.panics {
		panic("boom")
	}
	return f.out, f.err
}

func TestSynthesizer_Collect_MergesInRegistrationOrder(t *testing.T) {
	a := &fakeCollector{src: "a", out: []core.Evidence{{ID: "a1"}, {ID: "a2"}}}
	b := &fakeCollector{src: "b", out: []core.Evidence{{ID: "b1"}}}
	c := &fakeCollector{src: "c", out: []core.Evidence{{ID: "c1"}, {ID: "c2"}}}

	s := NewSynthesizerWith(a, b, c)
	ev, err := s.Collect(context.Background(), Target{NativeID: "gm-e11.6"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"a1", "a2", "b1", "c1", "c2"}
	if len(ev) != len(want) {
		t.Fatalf("got %d, want %d", len(ev), len(want))
	}
	for i, w := range want {
		if ev[i].ID != w {
			t.Errorf("ev[%d].ID=%q want %q", i, ev[i].ID, w)
		}
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 || c.calls.Load() != 1 {
		t.Errorf("each collector should be called once: %d/%d/%d",
			a.calls.Load(), b.calls.Load(), c.calls.Load())
	}
}

func TestSynthesizer_Collect_PartialFailureContinues(t *testing.T) {
	a := &fakeCollector{src: "a", out: []core.Evidence{{ID: "a1"}}}
	b := &fakeCollector{src: "b", err: errors.New("b broke")}
	c := &fakeCollector{src: "c", out: []core.Evidence{{ID: "c1"}}}

	var observedSrc Source
	var observedErr error
	s := NewSynthesizerWith(a, b, c)
	s.OnError = func(src Source, err error) {
		observedSrc = src
		observedErr = err
	}

	ev, err := s.Collect(context.Background(), Target{NativeID: "x"})
	if err != nil {
		t.Fatalf("partial failure should not surface error: %v", err)
	}
	if len(ev) != 2 {
		t.Fatalf("got %d, want 2 (a + c survive)", len(ev))
	}
	if observedSrc != "b" {
		t.Errorf("OnError src=%q want b", observedSrc)
	}
	if observedErr == nil {
		t.Errorf("OnError err nil")
	}
}

func TestSynthesizer_Collect_AllFailedSurfacesError(t *testing.T) {
	a := &fakeCollector{src: "a", err: errors.New("a")}
	b := &fakeCollector{src: "b", err: errors.New("b")}
	s := NewSynthesizerWith(a, b)
	s.OnError = func(Source, error) {}

	ev, err := s.Collect(context.Background(), Target{NativeID: "x"})
	if err == nil {
		t.Fatal("expected err when every collector failed and no evidence")
	}
	if ev != nil {
		t.Errorf("ev should be nil, got %+v", ev)
	}
}

func TestSynthesizer_Collect_PanicIsContained(t *testing.T) {
	a := &fakeCollector{src: "a", out: []core.Evidence{{ID: "a1"}}}
	b := &fakeCollector{src: "b", panics: true}
	s := NewSynthesizerWith(a, b)
	s.OnError = func(Source, error) {}

	ev, err := s.Collect(context.Background(), Target{NativeID: "x"})
	if err != nil {
		t.Fatalf("partial failure should not surface error: %v", err)
	}
	if len(ev) != 1 {
		t.Fatalf("got %d, want 1", len(ev))
	}
}

func TestSynthesizer_Empty(t *testing.T) {
	var s *Synthesizer
	ev, err := s.Collect(context.Background(), Target{})
	if err != nil || ev != nil {
		t.Errorf("nil synthesizer: ev=%v err=%v", ev, err)
	}

	s2 := NewSynthesizerWith()
	ev, err = s2.Collect(context.Background(), Target{})
	if err != nil || ev != nil {
		t.Errorf("empty synthesizer: ev=%v err=%v", ev, err)
	}
}

func TestSynthesizer_Add(t *testing.T) {
	s := NewSynthesizerWith()
	s.Add(&fakeCollector{src: "a"})
	s.Add(&fakeCollector{src: "b"})
	if len(s.Collectors()) != 2 {
		t.Fatalf("Add: len=%d", len(s.Collectors()))
	}
}

func TestNewSynthesizer_Defaults(t *testing.T) {
	s := NewSynthesizer()
	cs := s.Collectors()
	if len(cs) != 3 {
		t.Fatalf("default has %d collectors, want 3", len(cs))
	}
	wantSrc := []Source{SourceGitLog, SourceGitHubPR, SourceCI}
	for i, c := range cs {
		if c.Source() != wantSrc[i] {
			t.Errorf("[%d] %s want %s", i, c.Source(), wantSrc[i])
		}
	}
}
