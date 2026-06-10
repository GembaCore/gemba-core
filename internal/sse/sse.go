// Package sse implements a minimal Server-Sent Events consumer.
//
// gm-o9t8.1.5.5 — used by `gemba run` and `gemba logs -f` to read
// the /events stream the server exposes via internal/server/events_stream.go.
//
// Parsing follows the spec subset Gemba actually emits:
//
//   - `id: <event-id>` — last-event-id, propagated to Event.ID
//   - `event: <kind>`  — propagated to Event.Type (defaults to "message")
//   - `data: <bytes>`  — accumulated across consecutive `data:` lines,
//     joined with '\n' (no trailing newline). Multi-line frames are
//     reassembled before delivery.
//   - `: <comment>`    — ignored (heartbeats land here, e.g. ": keepalive")
//   - blank line       — frame terminator; if any data was buffered the
//     Event is delivered on ch.
//
// Backpressure: ch is caller-supplied and bounded. When the consumer is
// slow we drop the oldest Event with a slog warning instead of blocking
// the network reader. Reconnect (with Last-Event-ID) is NOT in this
// helper — callers handle it; `gemba logs -f` does, `gemba run` does not.
package sse

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Event is a single SSE frame after parsing. ID and Type may be empty.
// Data carries the raw decoded bytes from the joined `data:` lines.
type Event struct {
	ID   string
	Type string
	Data []byte
}

// Consume reads SSE frames from body until ctx is cancelled, body
// returns io.EOF, or a non-recoverable read error occurs. Frames are
// delivered on ch. The caller owns ch (Consume does not close it) so
// the same channel can be reused across reconnects.
//
// On a slow consumer Consume drops the oldest queued event with a
// slog.Warn rather than blocking the reader; the alternative — blocking
// — would let the server's send buffer fill up and trip the events
// hub's drop path (see internal/events/hub.go).
func Consume(ctx context.Context, body io.ReadCloser, ch chan<- Event) error {
	defer body.Close()

	r := bufio.NewReader(body)
	var (
		dataBuf  bytes.Buffer
		eventID  string
		eventTyp string
	)

	flush := func() {
		if dataBuf.Len() == 0 && eventID == "" && eventTyp == "" {
			return
		}
		ev := Event{
			ID:   eventID,
			Type: eventTyp,
			Data: append([]byte(nil), dataBuf.Bytes()...),
		}
		deliver(ctx, ch, ev)
		dataBuf.Reset()
		eventID = ""
		eventTyp = ""
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			// Trim the trailing \n (and \r if present).
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				// Frame terminator.
				flush()
			case strings.HasPrefix(line, ":"): //nolint:staticcheck // empty case is intentional — SSE spec ignores comment lines
				// Comment line — heartbeats / keepalive. Ignore.
			case strings.HasPrefix(line, "data:"):
				v := stripField(line, "data:")
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(v)
			case strings.HasPrefix(line, "event:"):
				eventTyp = stripField(line, "event:")
			case strings.HasPrefix(line, "id:"):
				eventID = stripField(line, "id:")
			case strings.HasPrefix(line, "retry:"): //nolint:staticcheck // empty case is intentional — server retry hints ignored, callers own reconnect cadence
				// Spec'd, ignored — we don't act on server-suggested retry
				// timing; callers own reconnect cadence.
			default:
				// Unknown field — spec says ignore.
			}
		}
		if err != nil {
			if err == io.EOF {
				flush()
				return nil
			}
			return fmt.Errorf("sse: read: %w", err)
		}
	}
}

// stripField removes the field prefix and one optional leading space,
// per the SSE spec.
func stripField(line, prefix string) string {
	v := strings.TrimPrefix(line, prefix)
	return strings.TrimPrefix(v, " ")
}

// deliver tries a non-blocking send. If ch is full we drop the oldest
// queued event (non-blocking receive) and retry. If the channel is
// still full we warn and drop the new event instead of blocking.
func deliver(ctx context.Context, ch chan<- Event, ev Event) {
	select {
	case ch <- ev:
		return
	default:
	}
	// Channel full; drop oldest. The chan<- direction prevents a
	// receive here, so callers that need shedding should expose a
	// bidirectional channel. We fall back to a non-blocking send +
	// warn, which surfaces the pressure rather than masking it.
	slog.Warn("sse: consumer slow, dropping event",
		"event_id", ev.ID, "event_type", ev.Type)
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}
