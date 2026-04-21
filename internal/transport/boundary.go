// Package transport hosts the three wire shims (api, jsonl, mcp) that
// bind adaptors to the core. This file defines the shared boundary-
// decoding primitives the three per-transport schemas.go files use to
// satisfy gm-io4 (t3code audit):
//
//   - Decode lives at the boundary. Adaptors receive typed, validated
//     input and do NOT re-validate.
//   - Validation failures surface as *core.AdaptorError{Kind: validation}
//     with a structured core.ValidationIssue in Detail["issue"] — never
//     as a bare json.SyntaxError and never buried in an adaptor stack.
//
// The per-transport schemas.go files wrap these primitives with signatures
// that match each transport's native frame shape (HTTP request body for
// api, JSONL frame for jsonl, tool-call argument map for mcp).
package transport

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/MikeBengtson/gemba/internal/core"
)

// DecodeJSON is the canonical JSON → typed-struct decoder every transport
// uses at the boundary. It returns:
//
//   - nil on success (`out` is populated),
//   - a *core.AdaptorError{Kind: validation} for any structural failure
//     (unknown field, type mismatch, syntax error). json.SyntaxError and
//     json.UnmarshalTypeError are translated into ValidationIssue shapes
//     so the UI has a stable payload to render.
//
// DisallowUnknownFields is ON: the whole point of the boundary contract is
// that adaptors trust their input. Silently dropping unknown keys defeats
// that; operators who intended to set a field and misspelled it get a
// loud error instead of a mystery no-op.
//
// context is the dotted JSON-pointer prefix for the request shape
// ("create_work_item", "end_session") so the ValidationIssue.Path is
// render-ready without the caller having to prepend every error.
func DecodeJSON(raw []byte, out any, context string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return translateJSONError(err, context)
	}
	// Reject trailing garbage: a well-formed frame carries exactly one
	// JSON value. This catches "\n\n" accidentally concatenated frames
	// on jsonl and POST bodies with trailing payloads.
	if dec.More() {
		return core.NewValidationError(core.ValidationIssue{
			Path:   context,
			Code:   "trailing_data",
			Reason: "request body contains more than one JSON value",
		})
	}
	return nil
}

// translateJSONError converts an encoding/json decode error into the
// canonical ValidationIssue shape. json errors are the single biggest
// source of free-form noise at the boundary; catching them here means
// adaptors never have to re-classify them.
func translateJSONError(err error, context string) *core.AdaptorError {
	switch e := err.(type) {
	case *json.SyntaxError:
		return core.NewValidationError(core.ValidationIssue{
			Path:   context,
			Code:   "syntax",
			Reason: fmt.Sprintf("malformed JSON at byte %d: %s", e.Offset, e.Error()),
		})
	case *json.UnmarshalTypeError:
		path := e.Field
		if context != "" {
			if path == "" {
				path = context
			} else {
				path = context + "." + path
			}
		}
		return core.NewValidationError(core.ValidationIssue{
			Path:   path,
			Code:   "type",
			Reason: fmt.Sprintf("want %s, got %s", e.Type.String(), e.Value),
		})
	}
	// encoding/json returns a bare error with this message when
	// DisallowUnknownFields trips. Strip the wrapper so the UI gets a
	// stable shape.
	msg := err.Error()
	if n := len(`json: unknown field "`); len(msg) > n && msg[:n] == `json: unknown field "` {
		fieldName := msg[n : len(msg)-1] // strip trailing `"`
		path := fieldName
		if context != "" {
			path = context + "." + fieldName
		}
		return core.NewValidationError(core.ValidationIssue{
			Path:   path,
			Code:   "unknown_field",
			Reason: "unknown field; the boundary decoder rejects unspecified keys",
		})
	}
	return core.NewValidationError(core.ValidationIssue{
		Path:   context,
		Code:   "decode",
		Reason: msg,
	})
}

// RequireNonEmpty returns a validation error when s is empty. Boundary
// decoders call this on every required string field after DecodeJSON.
// Keeping the helper at the shared layer means the three transports
// emit identical ValidationIssue shapes for the same failure — the UI
// and conformance suite assert on one payload, not three.
func RequireNonEmpty(s, path string) error {
	if s != "" {
		return nil
	}
	return core.NewValidationError(core.ValidationIssue{
		Path:   path,
		Code:   "required",
		Reason: "missing required field",
	})
}

// RequireEnum returns a validation error when value is not in the valid
// set. The Valid set MUST be non-empty; the emitted Reason lists every
// allowed token so the operator does not have to grep the source.
func RequireEnum[T ~string](value T, valid map[T]struct{}, path string) error {
	if _, ok := valid[value]; ok {
		return nil
	}
	// Build a stable list for the message.
	tokens := make([]string, 0, len(valid))
	for k := range valid {
		tokens = append(tokens, string(k))
	}
	return core.NewValidationError(core.ValidationIssue{
		Path:   path,
		Code:   "enum",
		Reason: fmt.Sprintf("must be one of %v, got %q", tokens, string(value)),
	})
}
