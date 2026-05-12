package sse

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// nopCloser wraps an io.Reader as an io.ReadCloser whose Close is a no-op.
type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

func consumeAll(t *testing.T, payload string) []Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body := nopCloser{strings.NewReader(payload)}
	ch := make(chan Event, 16)
	done := make(chan error, 1)
	go func() { done <- Consume(ctx, body, ch) }()
	if err := <-done; err != nil {
		t.Fatalf("Consume: %v", err)
	}
	close(ch)
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestConsume_SingleFrame(t *testing.T) {
	got := consumeAll(t, "data: hello\n\n")
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d (%+v)", len(got), got)
	}
	if string(got[0].Data) != "hello" {
		t.Errorf("data: want %q got %q", "hello", string(got[0].Data))
	}
	if got[0].ID != "" || got[0].Type != "" {
		t.Errorf("expected empty id/type, got %+v", got[0])
	}
}

func TestConsume_MultilineData(t *testing.T) {
	got := consumeAll(t, "data: line1\ndata: line2\ndata: line3\n\n")
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if string(got[0].Data) != "line1\nline2\nline3" {
		t.Errorf("multiline join: want %q got %q",
			"line1\nline2\nline3", string(got[0].Data))
	}
}

func TestConsume_PingCommentsIgnored(t *testing.T) {
	got := consumeAll(t,
		": keepalive\n\n"+
			": ping\n"+
			"data: payload\n\n"+
			": another\n\n")
	if len(got) != 1 {
		t.Fatalf("want 1 event (comments ignored), got %d (%+v)", len(got), got)
	}
	if string(got[0].Data) != "payload" {
		t.Errorf("want payload, got %q", string(got[0].Data))
	}
}

func TestConsume_IDAndEventFields(t *testing.T) {
	got := consumeAll(t, "id: evt-7\nevent: session.transition\ndata: {\"x\":1}\n\n")
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].ID != "evt-7" {
		t.Errorf("id: want evt-7, got %q", got[0].ID)
	}
	if got[0].Type != "session.transition" {
		t.Errorf("type: want session.transition, got %q", got[0].Type)
	}
	if string(got[0].Data) != `{"x":1}` {
		t.Errorf("data: got %q", string(got[0].Data))
	}
}

func TestConsume_TwoFrames(t *testing.T) {
	got := consumeAll(t, "data: one\n\ndata: two\n\n")
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if string(got[0].Data) != "one" || string(got[1].Data) != "two" {
		t.Errorf("frames: %+v", got)
	}
}

func TestConsume_CtxCancelStops(t *testing.T) {
	// A reader that blocks forever.
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Event, 1)
	done := make(chan error, 1)
	go func() { done <- Consume(ctx, nopCloser{pr}, ch) }()
	cancel()
	// Unblock the reader so Consume can observe ctx.
	go pw.Write([]byte(": tick\n\n"))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Consume did not return after ctx cancel")
	}
}
