// errors.go: typed error mapping for the gemba server's JSON error
// envelope (gm-o9t8.1.1.4).
//
// The server returns one of two shapes on 4xx / 5xx:
//
//	Generic envelope (components.schemas.Error):
//	  { "error": "<stable-code>", "message": "<human text>" }
//
//	Unauthorized variant (components.schemas.UnauthorizedResponse):
//	  { "error": "<message>", "code": "<stable-code>" }
//
// mapErr parses either shape, returns one of the sentinels below for
// well-known statuses, and falls back to *ServerError for everything
// else so callers can still surface the actionable message.

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Sentinel errors callers can compare with errors.Is.
var (
	ErrUnauthorized   = errors.New("unauthorized")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrNotImplemented = errors.New("not implemented")
	ErrBadRequest     = errors.New("bad request")
)

// ServerError carries the server's stable error code, the
// human-readable message, and the HTTP status for callers that need
// more than a sentinel.
type ServerError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServerError) Error() string {
	parts := []string{}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	body := strings.Join(parts, ": ")
	if body == "" {
		return fmt.Sprintf("server returned %d", e.Status)
	}
	return fmt.Sprintf("server returned %d: %s", e.Status, body)
}

// errorEnvelope handles both wire shapes. `error` is overloaded: in
// the Error component it's the stable code, in UnauthorizedResponse
// it's the human message. We disambiguate by also reading `code`.
type errorEnvelope struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// mapErr converts an HTTP response to a typed Go error.
//
// Status-to-sentinel mapping is checked first; if the envelope parses
// it wraps the sentinel with a ServerError-style message so
// fmt.Errorf("%w", err) callers still get actionable text. For
// non-mapped statuses mapErr returns a bare *ServerError.
func mapErr(resp *http.Response, body []byte) error {
	if resp == nil {
		return errors.New("client: nil response")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	code, msg := parseEnvelope(body)
	se := &ServerError{Code: code, Message: msg, Status: resp.StatusCode}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, se.envelopeText())
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, se.envelopeText())
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, se.envelopeText())
	case http.StatusNotImplemented:
		return fmt.Errorf("%w: %s", ErrNotImplemented, se.envelopeText())
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, se.envelopeText())
	}
	return se
}

// envelopeText renders the code+message pair for sentinel wrapping.
// Falls back to "<status>" when the envelope is empty so the wrapper
// never produces a dangling "unauthorized: " line.
func (e *ServerError) envelopeText() string {
	parts := []string{}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d", e.Status)
	}
	return strings.Join(parts, ": ")
}

// parseEnvelope normalizes the server's two error shapes into
// (code, message). When `code` is present the doc is treated as the
// UnauthorizedResponse variant (error=message, code=stable token);
// otherwise it's the generic Error envelope (error=stable token,
// message=human text).
func parseEnvelope(body []byte) (code, message string) {
	if len(body) == 0 {
		return "", ""
	}
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", strings.TrimSpace(string(body))
	}
	if env.Code != "" {
		// UnauthorizedResponse shape: error=message, code=code.
		return env.Code, env.Error
	}
	return env.Error, env.Message
}
