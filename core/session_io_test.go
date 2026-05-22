package core

import (
	"context"
	"testing"
)

// TestSessionIOContract pins the Phase A wire contract for the
// session-IO trio: the three SessionInputMode tokens (G-4), the four
// SessionEvent.Kind tokens (G-5), and the default KindUnsupported
// behavior of the UnsupportedSessionIO mixin (G-9). A future phase
// adding real session-IO must keep these tokens and the mixin's
// default-noop contract intact — this test fails loudly otherwise.
func TestSessionIOContract(t *testing.T) {
	t.Run("InputModeTokens", func(t *testing.T) {
		// G-4 LOCKED: exactly three lowercase wire tokens.
		if got, want := string(InputLiteral), "literal"; got != want {
			t.Errorf("InputLiteral = %q, want %q", got, want)
		}
		if got, want := string(InputKeys), "keys"; got != want {
			t.Errorf("InputKeys = %q, want %q", got, want)
		}
		if got, want := string(InputSignal), "signal"; got != want {
			t.Errorf("InputSignal = %q, want %q", got, want)
		}
	})

	t.Run("EventKindTokens", func(t *testing.T) {
		// G-5 LOCKED: exactly these four wire tokens, in this order.
		want := []string{"output", "status", "exit", "disconnect"}
		if len(SessionEventKinds) != len(want) {
			t.Fatalf("SessionEventKinds len = %d, want %d (%v)",
				len(SessionEventKinds), len(want), SessionEventKinds)
		}
		for i, w := range want {
			if SessionEventKinds[i] != w {
				t.Errorf("SessionEventKinds[%d] = %q, want %q",
					i, SessionEventKinds[i], w)
			}
		}
	})

	t.Run("UnsupportedMixin", func(t *testing.T) {
		// G-9: zero-value UnsupportedSessionIO returns
		// *AdaptorError{Kind: KindUnsupported} from all three methods.
		mixin := UnsupportedSessionIO{}
		ctx := context.Background()

		cases := []struct {
			name string
			call func() error
		}{
			{
				name: "SendInput",
				call: func() error {
					return mixin.SendInput(ctx, "sid", SessionInput{
						Mode: InputLiteral, Keys: "x",
					})
				},
			},
			{
				name: "ResizeSession",
				call: func() error {
					return mixin.ResizeSession(ctx, "sid", 80, 24)
				},
			},
			{
				name: "StreamSession",
				call: func() error {
					ch, err := mixin.StreamSession(ctx, "sid")
					if ch != nil {
						t.Errorf("StreamSession returned non-nil channel; want nil")
					}
					return err
				},
			},
		}

		for _, c := range cases {
			c := c
			t.Run(c.name, func(t *testing.T) {
				err := c.call()
				if err == nil {
					t.Fatalf("%s: want error, got nil", c.name)
				}
				ae := AsAdaptorError(err)
				if ae == nil {
					t.Fatalf("%s: error not tagged AdaptorError: %T (%v)",
						c.name, err, err)
				}
				if ae.Kind != KindUnsupported {
					t.Errorf("%s: Kind = %q, want %q",
						c.name, ae.Kind, KindUnsupported)
				}
			})
		}
	})
}
