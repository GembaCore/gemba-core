// Package httperr is the single place every gemba data handler goes to
// turn a Go error into the wire-stable JSON error envelope. The shape is
// fixed so the SPA and the OpenAPI client can decode every error from
// every handler with one contract:
//
//	{"error": "<code>", "message": "..."}
//
// <code> is the `core.ErrorKind` token when the adaptor returned a tagged
// *core.AdaptorError, a handler-supplied string for handler-level
// conditions (bad request param, adaptor not yet registered), or
// "internal" when an adaptor violates Conformance Group F by returning an
// untagged error.
//
// # Why a shared mapper
//
// M1.3 (list beads), M1.4 (capabilities), and M1.4b (single bead) all
// translate the same adaptor-level failures into HTTP responses. Doing
// that inline in every handler produced three drift points:
//
//  1. Status codes went out of sync (bare `errors.New` was surfaced as
//     503 in one handler, 500 in another).
//  2. Envelope shapes leaked internal fields (plane/adaptor) that the
//     SPA had to special-case per handler.
//  3. The rules for "retryable" — the UI banner depends on it —
//     couldn't be read from a single source.
//
// Every data handler MUST call WriteError for errors returned from the
// WorkPlane / OrchestrationPlane, and Write for handler-local envelope
// responses. Do not write ad-hoc JSON error blobs from handlers.
//
// # Status mapping
//
//	core.KindValidation       → 400 Bad Request
//	core.KindSessionNotFound  → 404 Not Found
//	core.KindReadOnly         → 405 Method Not Allowed
//	core.KindAdaptorDegraded  → 503 Service Unavailable
//	any other ErrorKind       → 500 Internal Server Error
//	bare errors.Is(ErrNotFound) → 404 (legacy sentinel compat)
//	untagged error            → 500 "internal"
//
// Other tagged kinds (rate_limited, request_failed, process_failed,
// unsupported, capability_denied, session_closed) land at 500 on
// purpose: they don't have a clean HTTP analogue for the read surface,
// and surfacing them as 500 makes it obvious a handler needs a dedicated
// path (e.g. mutation routes treat rate_limited as 429). When a new
// surface needs a new mapping, add the row here rather than branching
// inside the handler.
package httperr

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/GembaCore/gemba-core/core"
)

// StatusFor returns the HTTP status code for kind. Unknown kinds fall
// through to 500 — callers that want a different default for their
// surface should add a row in this switch rather than re-mapping at the
// call site.
func StatusFor(kind core.ErrorKind) int {
	switch kind {
	case core.KindValidation:
		return http.StatusBadRequest
	case core.KindSessionNotFound:
		return http.StatusNotFound
	case core.KindReadOnly:
		return http.StatusMethodNotAllowed
	case core.KindAdaptorDegraded:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// WriteError writes the envelope for err.
//
//   - A tagged *core.AdaptorError (including wrapped ones reachable via
//     errors.As) produces the kind's status from StatusFor, with "error"
//     set to the kind token and "message" set to the AdaptorError
//     Message.
//   - A bare error that satisfies errors.Is(err, core.ErrNotFound) is
//     treated as session_not_found (404). This preserves compatibility
//     with legacy adaptors that return the sentinel directly instead of
//     a tagged envelope; new adaptors MUST use a tagged AdaptorError
//     (Conformance Group F).
//   - Any other non-nil error is surfaced as 500 "internal" with the
//     raw err.Error() as the message. Untagged errors at the adaptor
//     boundary are programming bugs, not client-level faults.
//
// A nil err is itself a programming bug; WriteError emits a 500 rather
// than silently producing an empty envelope, so the regression is
// visible in tests and logs.
func WriteError(w http.ResponseWriter, err error) {
	if err == nil {
		Write(w, http.StatusInternalServerError, "internal", "unknown error")
		return
	}
	if ae := core.AsAdaptorError(err); ae != nil {
		Write(w, StatusFor(ae.Kind), ae.Kind.String(), ae.Message)
		return
	}
	if errors.Is(err, core.ErrNotFound) {
		Write(w, http.StatusNotFound, core.KindSessionNotFound.String(), err.Error())
		return
	}
	Write(w, http.StatusInternalServerError, "internal", err.Error())
}

// Write emits the envelope shape {"error": code, "message": message}
// with the given status. Use Write for handler-local error responses
// (missing required param, adaptor not registered) that don't originate
// from an adaptor-returned error.
func Write(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: code, Message: message})
}

// envelope is the wire shape. Kept unexported — callers build envelopes
// through Write / WriteError so the shape can't drift.
type envelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
