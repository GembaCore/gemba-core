package httperr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

func TestStatusFor(t *testing.T) {
	tests := []struct {
		kind core.ErrorKind
		want int
	}{
		{core.KindValidation, http.StatusBadRequest},
		{core.KindSessionNotFound, http.StatusNotFound},
		{core.KindReadOnly, http.StatusMethodNotAllowed},
		{core.KindAdaptorDegraded, http.StatusServiceUnavailable},
		{core.KindRequestFailed, http.StatusInternalServerError},
		{core.KindProcessFailed, http.StatusInternalServerError},
		{core.KindRateLimited, http.StatusInternalServerError},
		{core.KindUnsupported, http.StatusInternalServerError},
		{core.KindCapabilityDenied, http.StatusInternalServerError},
		{core.KindSessionClosed, http.StatusInternalServerError},
		{core.ErrorKind("made_up"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := httperr.StatusFor(tc.kind); got != tc.want {
				t.Fatalf("StatusFor(%q) = %d, want %d", tc.kind, got, tc.want)
			}
		})
	}
}

// envelope mirrors the wire shape so tests read it without leaking
// httperr's unexported type into the package API.
type envelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%q", err, rec.Body.String())
	}
	return env
}

func TestWriteError_TaggedAdaptorErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "validation",
			err:         core.NewAdaptorError(core.KindValidation, "missing field %q", "title"),
			wantStatus:  http.StatusBadRequest,
			wantCode:    "validation",
			wantMessage: `missing field "title"`,
		},
		{
			name:        "session_not_found",
			err:         core.NewAdaptorError(core.KindSessionNotFound, "bead gm-foo not found"),
			wantStatus:  http.StatusNotFound,
			wantCode:    "session_not_found",
			wantMessage: "bead gm-foo not found",
		},
		{
			name:        "read_only",
			err:         core.NewAdaptorError(core.KindReadOnly, "adaptor is --dolt-url read-only"),
			wantStatus:  http.StatusMethodNotAllowed,
			wantCode:    "read_only",
			wantMessage: "adaptor is --dolt-url read-only",
		},
		{
			name:        "adaptor_degraded",
			err:         core.NewAdaptorError(core.KindAdaptorDegraded, "bd probe timed out"),
			wantStatus:  http.StatusServiceUnavailable,
			wantCode:    "adaptor_degraded",
			wantMessage: "bd probe timed out",
		},
		{
			name:        "request_failed_falls_through",
			err:         core.NewAdaptorError(core.KindRequestFailed, "HTTP 502"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "request_failed",
			wantMessage: "HTTP 502",
		},
		{
			name:        "rate_limited_falls_through",
			err:         core.NewAdaptorError(core.KindRateLimited, "quota exceeded"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "rate_limited",
			wantMessage: "quota exceeded",
		},
		{
			name:        "wrapped_adaptor_error_still_maps",
			err:         fmt.Errorf("outer context: %w", core.NewAdaptorError(core.KindValidation, "bad input")),
			wantStatus:  http.StatusBadRequest,
			wantCode:    "validation",
			wantMessage: "bad input",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httperr.WriteError(rec, tc.err)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: want %d, got %d; body=%q",
					tc.wantStatus, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content-type: want application/json, got %q", ct)
			}
			env := decode(t, rec)
			if env.Error != tc.wantCode {
				t.Fatalf("error: want %q, got %q", tc.wantCode, env.Error)
			}
			if env.Message != tc.wantMessage {
				t.Fatalf("message: want %q, got %q", tc.wantMessage, env.Message)
			}
		})
	}
}

func TestWriteError_LegacySentinelErrNotFound(t *testing.T) {
	// Legacy adaptors returning the bare sentinel (and callers wrapping
	// it with fmt.Errorf) must still surface as 404 session_not_found so
	// existing WorkPlane implementations don't regress.
	tests := []struct {
		name string
		err  error
	}{
		{"bare_sentinel", core.ErrNotFound},
		{"wrapped_sentinel", fmt.Errorf("lookup %q: %w", "gm-x", core.ErrNotFound)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httperr.WriteError(rec, tc.err)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status: want 404, got %d", rec.Code)
			}
			env := decode(t, rec)
			if env.Error != "session_not_found" {
				t.Fatalf("error: want session_not_found, got %q", env.Error)
			}
			if env.Message == "" {
				t.Fatal("message must carry the original err.Error() for diagnosability")
			}
		})
	}
}

func TestWriteError_UntaggedErrorFallsThroughTo500(t *testing.T) {
	// Conformance Group F requires adaptors to return tagged errors.
	// An untagged error here is a bug, so the mapper surfaces it as
	// "internal" rather than guessing at a friendlier code.
	rec := httptest.NewRecorder()
	httperr.WriteError(rec, errors.New("something went wrong"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d", rec.Code)
	}
	env := decode(t, rec)
	if env.Error != "internal" {
		t.Fatalf("error: want internal, got %q", env.Error)
	}
	if env.Message != "something went wrong" {
		t.Fatalf("message: want raw error text, got %q", env.Message)
	}
}

func TestWriteError_NilErrorStillEmitsEnvelope(t *testing.T) {
	// Defensive: a nil err slipping into WriteError is a handler bug,
	// but the response must still be a valid envelope so clients don't
	// get a zero-length body with no Content-Type.
	rec := httptest.NewRecorder()
	httperr.WriteError(rec, nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type: want application/json, got %q", ct)
	}
	env := decode(t, rec)
	if env.Error != "internal" {
		t.Fatalf("error: want internal, got %q", env.Error)
	}
}

func TestWrite_ProducesEnvelopeWithGivenStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		code    string
		message string
	}{
		{"bad_request", http.StatusBadRequest, "bad_request", "missing bead id"},
		{"adaptor_not_configured", http.StatusServiceUnavailable, "adaptor_not_configured", "no WorkPlane adaptor registered"},
		{"custom_418", http.StatusTeapot, "teapot", "short and stout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httperr.Write(rec, tc.status, tc.code, tc.message)

			if rec.Code != tc.status {
				t.Fatalf("status: want %d, got %d", tc.status, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content-type: want application/json, got %q", ct)
			}
			env := decode(t, rec)
			if env.Error != tc.code {
				t.Fatalf("error: want %q, got %q", tc.code, env.Error)
			}
			if env.Message != tc.message {
				t.Fatalf("message: want %q, got %q", tc.message, env.Message)
			}
		})
	}
}
