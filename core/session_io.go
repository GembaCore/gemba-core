// Package core: see doc.go for the overview.
package core

// This file carries the session-IO contract: the typed payloads
// every OrchestrationPlaneAdaptor uses to deliver input to a live
// session and stream output back out. Kept in its own file (rather
// than crammed into orchestration.go) so Phase B's native-tmux
// implementation has a focused growth point and the diff stays
// readable across the matrix of adaptors that will land it.
//
// The three enum tokens here (G-4: SessionInputMode values) and the
// four SessionEvent.Kind tokens (G-5: output/status/exit/disconnect)
// are LOCKED per .planning/phases/A-core-sessionio/CONTEXT.md. Every
// downstream slice — Phase B (native tmux), Phase C (HTTP transport),
// Phases D–G (frontend) — imports these names verbatim; renaming them
// breaks the wire contract.

// SessionInputMode discriminates how a SendInput payload should be
// interpreted by the adaptor.
//
// LOCKED enumeration (CONTEXT.md G-4): exactly these three values are
// permitted. Adding a fourth requires a CONTEXT.md amendment because
// the SSE wire token and the SPA's input control both branch on this
// closed set.
type SessionInputMode string

const (
	// InputLiteral — Keys carries typed characters that should be
	// delivered byte-for-byte to the session's stdin / pty.
	InputLiteral SessionInputMode = "literal"
	// InputKeys — Keys carries one or more named keys (e.g.
	// "Enter", "C-c", "Up") that the adaptor translates to the
	// appropriate control sequence for its runtime.
	InputKeys SessionInputMode = "keys"
	// InputSignal — Keys carries a signal name (e.g. "SIGINT",
	// "SIGTERM") that the adaptor delivers to the session's
	// process group.
	InputSignal SessionInputMode = "signal"
)

// SessionInput is the payload SendInput delivers to a live session.
// Mode discriminates how Keys is interpreted (G-4); see
// SessionInputMode for the LOCKED enumeration.
type SessionInput struct {
	Keys string           `json:"keys"`
	Mode SessionInputMode `json:"mode"`
}

// SessionEvent is a single frame streamed by StreamSession.
//
// Kind carries one of four LOCKED wire tokens (CONTEXT.md G-5):
//
//   - "output"     — Bytes carries terminal output (stdout/stderr,
//     pty data, etc.).
//   - "status"     — Meta carries a status change (e.g. status name,
//     focus change). Bytes MAY be empty.
//   - "exit"       — the session's process exited. Meta MAY carry
//     exit code / signal. This is a transport-level signal — the
//     authoritative lifecycle event is still session_transition on
//     Subscribe.
//   - "disconnect" — the underlying IO link dropped. The channel
//     SHOULD close shortly after.
//
// These four tokens are NOT enum-encoded as a Go type: callers stream
// the raw strings over SSE and the wire token IS the value. Closing
// the set here (rather than via a Go enum) keeps the SSE producer
// and the SPA consumer in lockstep without an extra translation
// layer.
type SessionEvent struct {
	Kind  string         `json:"kind"`
	Bytes []byte         `json:"bytes,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}
