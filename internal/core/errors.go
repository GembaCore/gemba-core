// Package core: see doc.go for the overview.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrorKind is the closed set of discriminators every adaptor-boundary
// error carries (gm-faz). The t3code spike showed that string-matching
// on free-form error messages to decide "retry or fail?" is the single
// biggest source of adaptor rot: a provider renames an exception string
// and every call site that grepped for "rate limit" stops retrying.
//
// Adaptors MUST map every failure they surface to one of these kinds
// and set Retryable to its canonical value (see RetryableDefault) — or
// an explicit override when the transport genuinely knows better (e.g.
// a 429 that carried Retry-After=0). Conformance Group F
// (AssertAdaptorError) fails any adaptor that returns a bare error from
// a boundary method.
type ErrorKind string

const (
	// KindValidation — input failed the adaptor's schema / precondition
	// check (bad WorkItemPatch, illegal state transition, malformed
	// manifest). NOT retryable: the caller must fix the input.
	KindValidation ErrorKind = "validation"
	// KindSessionNotFound — the session/assignment/work-item id the
	// caller referenced does not exist (or has been garbage-collected).
	// NOT retryable: the caller has stale state.
	KindSessionNotFound ErrorKind = "session_not_found"
	// KindSessionClosed — the session exists but has reached a terminal
	// state (completed/failed/canceled). NOT retryable.
	KindSessionClosed ErrorKind = "session_closed"
	// KindRequestFailed — a wire-level call to the adaptor's transport
	// failed (connection refused, 5xx, timeout mid-stream). Retryable by
	// default: the next attempt may succeed after the transient issue
	// clears.
	KindRequestFailed ErrorKind = "request_failed"
	// KindProcessFailed — the adaptor's subprocess/backend raised a
	// structured failure that is NOT a transport glitch (bd exit code 2,
	// LangGraph node raised, MCP tool returned isError=true). Default
	// NOT retryable; callers that know a specific subclass is transient
	// may set Retryable=true at construction.
	KindProcessFailed ErrorKind = "process_failed"
	// KindRateLimited — the adaptor is throttling (HTTP 429, provider
	// quota). Retryable after the cool-down period; adaptors SHOULD
	// populate Detail["retry_after_seconds"] when they know it.
	KindRateLimited ErrorKind = "rate_limited"
	// KindUnsupported — the caller requested a feature this adaptor
	// opted out of in its CapabilityManifest (ReadBudgetRollup on a
	// non-budget adaptor, PeekSession screenshot when peek_modes lacks
	// it). NOT retryable: the UI should hide the control rather than
	// retry.
	KindUnsupported ErrorKind = "unsupported"
	// KindCapabilityDenied — the adaptor CAN do the thing but the
	// caller lacks authority (agent tried to mutate another agent's
	// assignment, permission prompt was denied). NOT retryable by the
	// same actor.
	KindCapabilityDenied ErrorKind = "capability_denied"
	// KindAdaptorDegraded — the adaptor's backend is temporarily
	// unhealthy (Dolt hung, subprocess supervisor restarting). The
	// gm-b1 mutation gate surfaces this verbatim to the SPA banner.
	// Retryable once the /api/adaptors probe reports healthy again.
	KindAdaptorDegraded ErrorKind = "adaptor_degraded"
)

// validErrorKinds is the authoritative set used by ErrorKind.Valid and
// UnmarshalJSON. Keep in sync with the const block above.
var validErrorKinds = map[ErrorKind]struct{}{
	KindValidation:       {},
	KindSessionNotFound:  {},
	KindSessionClosed:    {},
	KindRequestFailed:    {},
	KindProcessFailed:    {},
	KindRateLimited:      {},
	KindUnsupported:      {},
	KindCapabilityDenied: {},
	KindAdaptorDegraded:  {},
}

// Valid reports whether k is one of the nine canonical error kinds.
func (k ErrorKind) Valid() bool {
	_, ok := validErrorKinds[k]
	return ok
}

// String satisfies fmt.Stringer and always returns the lowercase token.
func (k ErrorKind) String() string { return string(k) }

// RetryableDefault returns the canonical retry disposition for a kind.
// Adaptors SHOULD construct errors via NewAdaptorError which applies
// this default; the explicit Retryable field on AdaptorError lets the
// transport override when it genuinely knows better (a 429 with
// Retry-After=0, a 5xx served from a cached error page that will never
// clear, …).
func (k ErrorKind) RetryableDefault() bool {
	switch k {
	case KindRequestFailed, KindRateLimited, KindAdaptorDegraded:
		return true
	default:
		return false
	}
}

// AdaptorError is the discriminated-tagged error every adaptor-boundary
// method MUST return on failure (gm-faz, resolves DD-12 + DD-13 per the
// t3code audit). The JSON shape is wire-stable:
//
//	{
//	  "_kind":     "rate_limited",
//	  "retryable": true,
//	  "message":   "provider quota exhausted for session sess-42",
//	  "cause":     "http 429",
//	  "detail":    {"retry_after_seconds": 30}
//	}
//
// The _kind discriminator drives the TS union emitted by
// cmd/gen-core-types; retryable replaces every historical call site
// that tried to guess intent from error strings.
//
// Construction patterns:
//
//	return nil, core.NewAdaptorError(core.KindValidation,
//	    "StateMap missing native status %q", native)
//
//	return nil, core.WrapAdaptorError(core.KindRequestFailed, err,
//	    "bd describe failed")
//
//	return nil, &core.AdaptorError{Kind: core.KindRateLimited,
//	    Retryable: false, Message: "retry-after 0, permanent"}
//
// AdaptorError implements error, errors.Unwrap (via Cause), and a
// custom Is so the legacy sentinels ErrNotFound / ErrUnsupported stay
// compatible with errors.Is.
type AdaptorError struct {
	Kind      ErrorKind      `json:"_kind"`
	Retryable bool           `json:"retryable"`
	Message   string         `json:"message"`
	Cause     error          `json:"-"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// NewAdaptorError constructs a tagged error with Retryable defaulted
// from kind.RetryableDefault(). Message is formatted fmt.Sprintf style.
func NewAdaptorError(kind ErrorKind, format string, args ...any) *AdaptorError {
	return &AdaptorError{
		Kind:      kind,
		Retryable: kind.RetryableDefault(),
		Message:   fmt.Sprintf(format, args...),
	}
}

// WrapAdaptorError wraps a lower-level error (subprocess exit, HTTP
// client failure) with a tagged envelope. The wrapped error becomes
// Cause so errors.Unwrap and errors.Is keep working.
func WrapAdaptorError(kind ErrorKind, cause error, format string, args ...any) *AdaptorError {
	return &AdaptorError{
		Kind:      kind,
		Retryable: kind.RetryableDefault(),
		Message:   fmt.Sprintf(format, args...),
		Cause:     cause,
	}
}

// Error renders the tagged error as "kind: message: cause" so test
// failures and logs stay greppable. Callers MUST NOT parse this string
// — branch on Kind / Retryable instead.
func (e *AdaptorError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Unwrap exposes Cause so errors.Is / errors.As walk past the tagged
// envelope to the transport-level error.
func (e *AdaptorError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is makes the legacy sentinels (ErrNotFound, ErrUnsupported) match a
// tagged AdaptorError of the corresponding kind. This lets older call
// sites keep using errors.Is(err, core.ErrNotFound) while new adaptors
// return a proper AdaptorError underneath.
func (e *AdaptorError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	switch target {
	case ErrNotFound:
		return e.Kind == KindSessionNotFound
	case ErrUnsupported:
		return e.Kind == KindUnsupported
	}
	if other, ok := target.(*AdaptorError); ok {
		// Two AdaptorErrors match if their kinds match. This is useful
		// for table-driven tests that want to assert on kind without
		// constructing an identical instance.
		return e.Kind == other.Kind
	}
	return false
}

// MarshalJSON emits the wire shape with the Cause flattened to its
// Error() string. Keeping Cause as an opaque string on the wire avoids
// leaking stack traces or unrelated structured types into the SPA; the
// Go side keeps the typed Cause available through Unwrap.
func (e *AdaptorError) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte("null"), nil
	}
	payload := struct {
		Kind      ErrorKind      `json:"_kind"`
		Retryable bool           `json:"retryable"`
		Message   string         `json:"message"`
		Cause     string         `json:"cause,omitempty"`
		Detail    map[string]any `json:"detail,omitempty"`
	}{
		Kind:      e.Kind,
		Retryable: e.Retryable,
		Message:   e.Message,
		Detail:    e.Detail,
	}
	if e.Cause != nil {
		payload.Cause = e.Cause.Error()
	}
	return json.Marshal(payload)
}

// UnmarshalJSON rehydrates an AdaptorError from the wire. Cause is
// reconstituted as a plain errors.New-style error since the original
// typed chain cannot cross the wire.
func (e *AdaptorError) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind      ErrorKind      `json:"_kind"`
		Retryable bool           `json:"retryable"`
		Message   string         `json:"message"`
		Cause     string         `json:"cause,omitempty"`
		Detail    map[string]any `json:"detail,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if !raw.Kind.Valid() {
		return fmt.Errorf("core: unknown AdaptorError._kind %q", raw.Kind)
	}
	e.Kind = raw.Kind
	e.Retryable = raw.Retryable
	e.Message = raw.Message
	e.Detail = raw.Detail
	if raw.Cause != "" {
		e.Cause = errors.New(raw.Cause)
	}
	return nil
}

// AsAdaptorError extracts the nearest *AdaptorError in err's Unwrap
// chain, returning nil when no tagged error is present. This is the
// canonical accessor for retry loops and the mutation gate — prefer it
// over string-matching on err.Error().
//
//	if ae := core.AsAdaptorError(err); ae != nil && ae.Retryable {
//	    time.Sleep(backoff); continue
//	}
func AsAdaptorError(err error) *AdaptorError {
	var ae *AdaptorError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

// IsRetryable reports whether err carries a tagged AdaptorError whose
// Retryable flag is true. A non-tagged error is NOT retryable: the
// caller is expected to either wrap it or treat it as terminal.
func IsRetryable(err error) bool {
	ae := AsAdaptorError(err)
	return ae != nil && ae.Retryable
}

// AssertAdaptorError is the Conformance Group F (error semantics) helper:
// given an error observed from an adaptor-boundary call, it returns nil
// when the error carries a tagged AdaptorError of a valid kind with the
// retryable field populated, and returns a descriptive error otherwise.
//
// Conformance suites SHOULD call this against every non-nil error they
// observe from an adaptor method. An adaptor that raises a bare
// errors.New() or a fmt.Errorf() without a tagged envelope fails
// Group F and must be fixed before it can ship.
//
// The empty-message case is tolerated (an adaptor may only carry a
// Kind), but the kind itself must be in the canonical set.
func AssertAdaptorError(err error) error {
	if err == nil {
		return nil
	}
	ae := AsAdaptorError(err)
	if ae == nil {
		return fmt.Errorf(
			"conformance Group F: adaptor returned untagged error %T (%q); every boundary error MUST wrap an *AdaptorError",
			err, err.Error())
	}
	if !ae.Kind.Valid() {
		return fmt.Errorf(
			"conformance Group F: AdaptorError._kind %q is not one of the nine canonical kinds",
			ae.Kind)
	}
	return nil
}
