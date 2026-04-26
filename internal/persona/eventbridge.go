package persona

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/MikeBengtson/gemba/internal/events"
)

// FanFromHub subscribes to the events Hub for [events.SkillOutputEmitted]
// frames and routes each one's lines into [Dispatcher.Receive]
// (gm-twp2).
//
// Architecture seam:
//
//   spawned session → gemba-mcp emit_skill_output → bridge tail →
//   OrchestrationEvent{Kind:"skill_output_emitted"} → events.Hub →
//   FanFromHub → Dispatcher.Receive(consultID, lines)
//
// The event's SessionID is treated as the consult id — the spawn
// driver sets GEMBA_SESSION_ID = consult.ID so a frame from inside
// the agent container carries the right correlation key. An event
// whose SessionID does not match a registered consult is dropped
// with a debug log; the dispatcher returns a non-nil error and the
// loop continues. Same shape for malformed payloads.
//
// Lifecycle: blocks until ctx is cancelled or the hub closes the
// subscription channel. cmd/gemba serve runs it on a long-lived
// goroutine; tests cancel ctx to drive shutdown.
func FanFromHub(ctx context.Context, d *Dispatcher, hub *events.Hub) {
	if d == nil || hub == nil {
		return
	}
	stream := hub.Subscribe(ctx, events.Filter{
		Kinds: []events.Kind{events.SkillOutputEmitted},
	})
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-stream:
			if !ok {
				return
			}
			deliverSkillOutput(d, ev)
		}
	}
}

// deliverSkillOutput unpacks one event's `lines` payload and hands
// it to the dispatcher. Pulled out so unit tests can drive it
// without spinning a hub.
func deliverSkillOutput(d *Dispatcher, ev events.GembaEvent) {
	if ev.SessionID == "" {
		slog.Debug("persona/eventbridge: skill_output_emitted with empty SessionID; dropped",
			"event_id", ev.ID)
		return
	}
	lines, err := extractLinesPayload(ev.Payload)
	if err != nil {
		slog.Warn("persona/eventbridge: malformed skill_output_emitted payload; dropped",
			"event_id", ev.ID, "session_id", ev.SessionID, "err", err)
		return
	}
	if len(lines) == 0 {
		// Empty lines slice is a noop — Receive would error, but the
		// frame translator already drops empty payloads (see
		// translateGembaSkillOutput) so this should not happen.
		return
	}
	if err := d.Receive(ev.SessionID, lines); err != nil {
		// Common cause: the spawned session belonged to a different
		// dispatcher instance, or the consult was already finished.
		// Either way the right move is "drop the frame and log";
		// failing the loop would strand every subsequent consult.
		slog.Debug("persona/eventbridge: dispatcher.Receive declined frame",
			"consult_id", ev.SessionID, "err", err, "lines", len(lines))
	}
}

// extractLinesPayload reads the `lines` slot from an event payload
// and converts whatever the hub serialised it as back into a
// []json.RawMessage the dispatcher's Receive expects.
//
// The bridge translator (internal/adapter/native/bridge/translate.go's
// translateGembaSkillOutput) writes lines as []any of
// json.RawMessage. Subscribers via the SSE hub may see them as
// []any of map[string]any or []any of []byte after a JSON round-trip,
// so this helper handles every plausible shape.
func extractLinesPayload(payload map[string]any) ([]json.RawMessage, error) {
	if payload == nil {
		return nil, errors.New("nil payload")
	}
	raw, ok := payload["lines"]
	if !ok {
		return nil, errors.New(`payload missing "lines" key`)
	}
	switch v := raw.(type) {
	case []json.RawMessage:
		return v, nil
	case []any:
		out := make([]json.RawMessage, 0, len(v))
		for i, elem := range v {
			rm, err := normalizeLineElement(elem)
			if err != nil {
				return nil, errLineAt(i, err)
			}
			out = append(out, rm)
		}
		return out, nil
	case json.RawMessage:
		// Whole `lines` slot was already serialised as a JSON-encoded
		// array. Decode through the array shape so each element comes
		// out individually addressable.
		var arr []json.RawMessage
		if err := json.Unmarshal(v, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	default:
		// Last-resort: marshal the value back to JSON and try the
		// array decode. Catches a []map[string]any from a non-RawMessage
		// hub path.
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(b, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
}

func normalizeLineElement(elem any) (json.RawMessage, error) {
	switch v := elem.(type) {
	case json.RawMessage:
		return v, nil
	case []byte:
		return json.RawMessage(v), nil
	case string:
		// Some serialisers escape RawMessage as a JSON string of the
		// underlying body. Try to unmarshal as a quoted JSON value.
		return json.RawMessage(v), nil
	default:
		// Object / array / number / bool / nil — re-marshal so the
		// dispatcher's per-skill ValidateOutputLine sees a stable
		// JSON envelope.
		return json.Marshal(elem)
	}
}

// errLineAt wraps a per-element error with its index for clearer
// log messages when a malformed payload sneaks through.
func errLineAt(i int, err error) error {
	return &lineError{Index: i, Err: err}
}

type lineError struct {
	Index int
	Err   error
}

func (e *lineError) Error() string {
	return "lines[" + itoa(e.Index) + "]: " + e.Err.Error()
}

func (e *lineError) Unwrap() error { return e.Err }

// itoa is the smallest dependency-free int→string we need; avoids
// the strconv import for this single call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
