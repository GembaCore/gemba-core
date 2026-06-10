package native

import (
	"context"
	"errors"

	"github.com/GembaCore/gemba-core/core"
)

// This file carries the Phase B overrides for the
// core.UnsupportedSessionIO mixin embedded on OrchestrationPlane.
// SendInput + ResizeSession + StreamSession are all real after
// Plan B-03 (gm-v01.3.5); the mixin embed is retained for
// forward-compatibility in case Phase C adds another verb.
//
// Method-resolution rule we rely on: Go prefers a method defined
// directly on the outer struct over a method promoted from an
// embedded field. So adding these two methods here shadows the mixin
// without changing orchestration.go's struct definition.

// SendInput dispatches a typed core.SessionInput payload through the
// configured Backend after resolving the opaque sessionID to a tmux
// pane id via lookupSessionPane. gm-v01.3.1 + gm-v01.3.2.
//
// Errors:
//
//   - Backend nil               -> core.KindUnsupported
//     (zero-config adaptor degrades cleanly, matching StartSession).
//   - Mode not one of the three LOCKED SessionInputMode values
//     -> core.KindValidation (typed before any backend touch so the
//     wire-token guard in core/codegen_test.go stays meaningful).
//   - sessionID unknown to this adaptor
//     -> core.KindSessionNotFound (propagated unchanged from
//     lookupSessionPane so HTTP routes 404 the same way PauseSession
//     does).
//   - Backend.SendInput returns an *AdaptorError -> returned verbatim.
//   - Backend.SendInput returns a plain error
//     -> wrapped with core.KindProcessFailed so the typed-error
//     pipeline still surfaces a structured envelope.
func (o *OrchestrationPlane) SendInput(ctx context.Context, sessionID string, in core.SessionInput) error {
	if o.cfg.Backend == nil {
		return unsupported("SendInput")
	}
	switch in.Mode {
	case core.InputLiteral, core.InputKeys, core.InputSignal:
		// ok
	default:
		return core.NewAdaptorError(core.KindValidation,
			"native: unknown SessionInputMode %q", in.Mode)
	}
	paneID, _, err := o.lookupSessionPane(sessionID)
	if err != nil {
		return err
	}
	if err := o.cfg.Backend.SendInput(ctx, paneID, in); err != nil {
		// Pass typed adaptor errors through unchanged so callers see the
		// original Kind (e.g. KindValidation on signal-unsupported on
		// AppleScript backends, KindUnsupported on the ITerm signal
		// path). Only wrap plain/foreign errors.
		var ae *core.AdaptorError
		if errors.As(err, &ae) {
			return err
		}
		return core.WrapAdaptorError(core.KindProcessFailed, err,
			"native: send-input %s", sessionID)
	}
	return nil
}

// ResizeSession is a documented no-op for the native tmux adaptor.
// CONTEXT.md scope item 2: tmux owns pane geometry server-side; the
// pty inside the pane already follows it. We still validate inputs +
// confirm the session exists so callers get typed errors (rather than
// silently masking bugs by returning nil for an unknown id), and so
// downstream container / k8s adaptors can shadow this with a real
// resize when they land.
//
// Errors:
//
//   - Backend nil               -> core.KindUnsupported (matches the
//     other Backend-gated verbs so the zero-config adapter degrades
//     consistently).
//   - empty sessionID, cols<=0, rows<=0 -> core.KindValidation.
//   - sessionID unknown         -> core.KindSessionNotFound.
//   - otherwise                 -> nil (no Backend method invoked).
func (o *OrchestrationPlane) ResizeSession(ctx context.Context, sessionID string, cols, rows int) error {
	if o.cfg.Backend == nil {
		return unsupported("ResizeSession")
	}
	if sessionID == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native: ResizeSession requires session id")
	}
	if cols <= 0 || rows <= 0 {
		return core.NewAdaptorError(core.KindValidation,
			"native: ResizeSession requires positive cols + rows (got %d x %d)", cols, rows)
	}
	if _, _, err := o.lookupSessionPane(sessionID); err != nil {
		return err
	}
	// Documented no-op (CONTEXT.md scope item 2). Container / k8s
	// adaptors override this with real geometry pushes.
	_ = ctx
	return nil
}

// StreamSession returns a channel of SessionEvents backed by the
// sessionIOHub (Plan B-02). The hub does the heavy lifting — single
// pipe-pane per pane, refcounted subscribers, FIFO lifecycle — so this
// override stays a thin guard.
//
// Errors:
//
//   - Backend nil  -> core.KindUnsupported (zero-config adaptor matches
//     the other Backend-gated verbs).
//   - hub nil      -> core.KindUnsupported with the backend name in the
//     message. Reached when cfg.Backend does not satisfy
//     backend.Streamable (e.g. AppleScript today). Also reached after
//     Close() has nil-ed the hub — clean shutdown means no re-attach.
//   - empty sessionID -> core.KindValidation.
//   - unknown sessionID -> core.KindSessionNotFound (propagated from
//     the resolver inside hub.Attach).
//
// Snapshot-first contract: every subscriber receives a snapshot event
// (Kind="output") before any live-output events, even when the pipe is
// already running for a prior subscriber. See iohub.go.
//
// Opacity guard: this method speaks sessionID; the pane id translation
// lives inside the resolver closure injected at NewWithConfig time.
// Transport-layer code (this file, the eventual HTTP /stream handler)
// MUST NOT pattern-match on sessionID shape. See
// core/session_id_opacity_test.go.
func (o *OrchestrationPlane) StreamSession(ctx context.Context, sessionID string) (<-chan core.SessionEvent, error) {
	if o.cfg.Backend == nil {
		return nil, unsupported("StreamSession")
	}
	if o.hub == nil {
		return nil, core.NewAdaptorError(core.KindUnsupported,
			"native: backend %q does not implement Streamable", o.cfg.Backend.Name())
	}
	if sessionID == "" {
		return nil, core.NewAdaptorError(core.KindValidation,
			"native: StreamSession requires session id")
	}
	return o.hub.Attach(ctx, sessionID)
}
