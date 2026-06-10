package noop

import (
	"context"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// TestSessionIO_DefaultUnsupported pins the Phase A invariant that the
// noop plane embeds core.UnsupportedSessionIO and therefore returns
// KindUnsupported from all three session-IO trio methods. The noop
// adaptor never runs real sessions, so KindUnsupported is the correct
// long-term response — this test guards against an accidental removal
// of the embed.
func TestSessionIO_DefaultUnsupported(t *testing.T) {
	p := NewOrchestrationPlane()
	ctx := context.Background()
	const sid = "sid-phase-a"

	cases := []struct {
		name string
		call func() error
	}{
		{"SendInput", func() error {
			return p.SendInput(ctx, sid, core.SessionInput{Mode: core.InputLiteral, Keys: "x"})
		}},
		{"ResizeSession", func() error {
			return p.ResizeSession(ctx, sid, 80, 24)
		}},
		{"StreamSession", func() error {
			ch, err := p.StreamSession(ctx, sid)
			if ch != nil {
				t.Errorf("StreamSession returned non-nil channel; want nil")
			}
			return err
		}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s: want error, got nil", c.name)
			}
			ae := core.AsAdaptorError(err)
			if ae == nil || ae.Kind != core.KindUnsupported {
				t.Errorf("%s: want AdaptorError{KindUnsupported}, got %T: %v", c.name, err, err)
			}
		})
	}
}
