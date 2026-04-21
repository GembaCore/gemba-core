package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorKindValid(t *testing.T) {
	want := []ErrorKind{
		KindValidation, KindSessionNotFound, KindSessionClosed,
		KindRequestFailed, KindProcessFailed, KindRateLimited,
		KindUnsupported, KindCapabilityDenied, KindAdaptorDegraded,
	}
	if len(want) != len(validErrorKinds) {
		t.Fatalf("kind count drift: const list=%d, validErrorKinds=%d",
			len(want), len(validErrorKinds))
	}
	for _, k := range want {
		if !k.Valid() {
			t.Errorf("%q expected Valid()=true", k)
		}
	}
	if ErrorKind("mystery").Valid() {
		t.Error("unknown kind should not be Valid()")
	}
}

func TestRetryableDefault(t *testing.T) {
	retryable := map[ErrorKind]bool{
		KindValidation:       false,
		KindSessionNotFound:  false,
		KindSessionClosed:    false,
		KindRequestFailed:    true,
		KindProcessFailed:    false,
		KindRateLimited:      true,
		KindUnsupported:      false,
		KindCapabilityDenied: false,
		KindAdaptorDegraded:  true,
	}
	for kind, want := range retryable {
		if got := kind.RetryableDefault(); got != want {
			t.Errorf("%s: RetryableDefault=%v want %v", kind, got, want)
		}
	}
}

func TestNewAdaptorErrorDefaults(t *testing.T) {
	ae := NewAdaptorError(KindRateLimited, "provider %q throttled", "openai")
	if ae.Kind != KindRateLimited {
		t.Errorf("Kind=%v", ae.Kind)
	}
	if !ae.Retryable {
		t.Error("rate_limited should default to Retryable=true")
	}
	if !strings.Contains(ae.Message, "openai") {
		t.Errorf("Message lost format args: %q", ae.Message)
	}
	if ae.Cause != nil {
		t.Error("NewAdaptorError should not set Cause")
	}
}

func TestWrapAdaptorErrorUnwrap(t *testing.T) {
	inner := errors.New("connection refused")
	ae := WrapAdaptorError(KindRequestFailed, inner, "bd describe failed")
	if !errors.Is(ae, inner) {
		t.Error("errors.Is must walk the Cause chain")
	}
	if !strings.Contains(ae.Error(), "connection refused") {
		t.Errorf("Error() should include cause: %q", ae.Error())
	}
	if !ae.Retryable {
		t.Error("request_failed should default to Retryable=true")
	}
}

func TestAdaptorErrorIsSentinels(t *testing.T) {
	notFound := &AdaptorError{Kind: KindSessionNotFound, Message: "no such session"}
	if !errors.Is(notFound, ErrNotFound) {
		t.Error("AdaptorError{Kind:SessionNotFound} must match ErrNotFound")
	}
	if errors.Is(notFound, ErrUnsupported) {
		t.Error("session_not_found must NOT match ErrUnsupported")
	}

	unsup := &AdaptorError{Kind: KindUnsupported, Message: "budget rollup"}
	if !errors.Is(unsup, ErrUnsupported) {
		t.Error("AdaptorError{Kind:Unsupported} must match ErrUnsupported")
	}

	// Kind-to-kind equality for table-driven tests.
	other := &AdaptorError{Kind: KindUnsupported}
	if !errors.Is(unsup, other) {
		t.Error("two AdaptorErrors with the same kind should match")
	}

	// And through a wrapper.
	wrapped := fmt.Errorf("plane=work: %w", notFound)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("wrapped AdaptorError must still match ErrNotFound")
	}
}

func TestAdaptorErrorJSONRoundTrip(t *testing.T) {
	ae := &AdaptorError{
		Kind:      KindRateLimited,
		Retryable: true,
		Message:   "openai throttled",
		Cause:     errors.New("http 429"),
		Detail:    map[string]any{"retry_after_seconds": float64(30)},
	}
	data, err := json.Marshal(ae)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Spot-check the wire shape.
	if !strings.Contains(string(data), `"_kind":"rate_limited"`) {
		t.Errorf("wire shape missing _kind: %s", data)
	}
	if !strings.Contains(string(data), `"retryable":true`) {
		t.Errorf("wire shape missing retryable: %s", data)
	}
	if !strings.Contains(string(data), `"cause":"http 429"`) {
		t.Errorf("wire shape lost cause: %s", data)
	}

	var round AdaptorError
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Kind != ae.Kind || round.Retryable != ae.Retryable {
		t.Errorf("round-trip scalar drift: %+v", round)
	}
	if round.Cause == nil || round.Cause.Error() != "http 429" {
		t.Errorf("round-trip cause drift: %v", round.Cause)
	}
	if round.Detail["retry_after_seconds"] != float64(30) {
		t.Errorf("round-trip detail drift: %+v", round.Detail)
	}
}

func TestAdaptorErrorUnmarshalRejectsUnknownKind(t *testing.T) {
	var ae AdaptorError
	err := json.Unmarshal([]byte(`{"_kind":"banana","retryable":true,"message":"x"}`), &ae)
	if err == nil {
		t.Fatal("expected unmarshal to reject unknown _kind")
	}
}

func TestAsAdaptorErrorWalksChain(t *testing.T) {
	ae := NewAdaptorError(KindProcessFailed, "bd exit 2")
	wrapped := fmt.Errorf("adaptor=bd: %w", ae)
	got := AsAdaptorError(wrapped)
	if got == nil {
		t.Fatal("AsAdaptorError should walk wrap chain")
	}
	if got.Kind != KindProcessFailed {
		t.Errorf("AsAdaptorError.Kind=%v", got.Kind)
	}
	if AsAdaptorError(errors.New("raw")) != nil {
		t.Error("AsAdaptorError must return nil for untagged errors")
	}
}

func TestIsRetryable(t *testing.T) {
	retry := NewAdaptorError(KindRateLimited, "slow down")
	if !IsRetryable(retry) {
		t.Error("rate_limited default must be retryable")
	}

	// Override: adaptor KNOWS this particular 429 is permanent.
	permanent := &AdaptorError{Kind: KindRateLimited, Retryable: false, Message: "quota exhausted"}
	if IsRetryable(permanent) {
		t.Error("explicit Retryable=false must override the default")
	}

	// Non-tagged error is never retryable.
	if IsRetryable(errors.New("who knows")) {
		t.Error("untagged error must not be treated as retryable")
	}
	if IsRetryable(nil) {
		t.Error("nil is not retryable")
	}
}

// TestAssertAdaptorErrorConformance is the Group F acceptance gate: an
// adaptor that returns a bare error from a boundary call must fail this
// assertion, and one that returns a proper AdaptorError must pass.
// This is the negative-path test called out in the bead's DoD.
func TestAssertAdaptorErrorConformance(t *testing.T) {
	if err := AssertAdaptorError(nil); err != nil {
		t.Errorf("nil error should pass Group F: %v", err)
	}

	// POSITIVE: a well-formed tagged error passes.
	good := NewAdaptorError(KindValidation, "bad input")
	if err := AssertAdaptorError(good); err != nil {
		t.Errorf("tagged error should pass Group F: %v", err)
	}

	// NEGATIVE: a bare errors.New() fails — this is what the DoD demands.
	bare := errors.New("something went wrong")
	err := AssertAdaptorError(bare)
	if err == nil {
		t.Fatal("Group F must reject bare errors.New() from adaptors")
	}
	if !strings.Contains(err.Error(), "Group F") {
		t.Errorf("conformance failure should cite the group: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "untagged") {
		t.Errorf("conformance failure should mention untagged: %q", err.Error())
	}

	// NEGATIVE: an AdaptorError with an unknown _kind fails too (defense
	// against someone constructing one by reflection).
	wrongKind := &AdaptorError{Kind: ErrorKind("made_up"), Retryable: true, Message: "x"}
	if err := AssertAdaptorError(wrongKind); err == nil {
		t.Error("Group F must reject AdaptorError with unknown kind")
	}

	// Wrapped tagged error still passes (the adaptor layered on context
	// with fmt.Errorf("%w", ...) — legitimate).
	wrapped := fmt.Errorf("bd adaptor: %w", good)
	if err := AssertAdaptorError(wrapped); err != nil {
		t.Errorf("wrapped tagged error should pass Group F: %v", err)
	}
}

// TestAssertAdaptorErrorAgainstNoopAdaptor demonstrates how a
// conformance suite wires Group F against a real WorkPlane. The
// noopWorkPlane in workplane_test.go returns ErrNotFound / ErrUnsupported
// (the legacy sentinels) — which are plain errors.New() values, not
// AdaptorErrors. We PROVE that a bare-sentinel adaptor fails Group F so
// there's no ambiguity that the legacy sentinels alone are insufficient
// for the new contract.
func TestAssertAdaptorErrorAgainstNoopAdaptor(t *testing.T) {
	plane := noopWorkPlane{}
	_, err := plane.GetWorkItem(nil, WorkItemID("x"))
	if err == nil {
		t.Fatal("noop adaptor should return ErrNotFound")
	}
	if conformance := AssertAdaptorError(err); conformance == nil {
		t.Error("Group F must reject bare ErrNotFound sentinel — adaptor should return *AdaptorError")
	}
}

// TestValidationIssueRoundTrip — the gm-io4 structured-issue contract:
// NewValidationError carries a ValidationIssue that survives JSON round-
// trip and can be read back out via ValidationIssueOf. Transport
// decoders depend on this invariant so the wire error is indistinguishable
// from the in-process error the UI would read.
func TestValidationIssueRoundTrip(t *testing.T) {
	issue := ValidationIssue{
		Path:   "end_session.mode",
		Code:   "enum",
		Reason: `must be one of ["completed","failed","canceled"]`,
	}
	ae := NewValidationError(issue)
	if ae.Kind != KindValidation {
		t.Fatalf("Kind=%v, want validation", ae.Kind)
	}
	if ae.Retryable {
		t.Error("validation errors must not be retryable")
	}
	got, ok := ValidationIssueOf(ae)
	if !ok {
		t.Fatal("ValidationIssueOf failed on fresh error")
	}
	if got != issue {
		t.Errorf("round-trip drift: got %+v", got)
	}

	// Now through the wire.
	data, err := json.Marshal(ae)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round AdaptorError
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok = ValidationIssueOf(&round)
	if !ok {
		t.Fatal("ValidationIssueOf failed on round-tripped error")
	}
	if got.Path != issue.Path || got.Code != issue.Code || got.Reason != issue.Reason {
		t.Errorf("wire round-trip drift: %+v", got)
	}
}

// TestAssertBoundaryValidation enforces the gm-io4 Group F extension:
// transport-boundary errors must be KindValidation AND carry a
// structured ValidationIssue. Anything else is a conformance failure.
func TestAssertBoundaryValidation(t *testing.T) {
	// Nil err — decoder accepted bad input silently. Fails.
	if err := AssertBoundaryValidation(nil); err == nil {
		t.Error("Group F boundary: nil must fail (decoder ate the failure)")
	}

	// Untagged error. Fails.
	if err := AssertBoundaryValidation(errors.New("raw")); err == nil {
		t.Error("Group F boundary: untagged error must fail")
	}

	// Wrong kind. Fails.
	wrong := NewAdaptorError(KindRequestFailed, "wire oops")
	if err := AssertBoundaryValidation(wrong); err == nil {
		t.Error("Group F boundary: kind=request_failed must fail (decoder should emit validation)")
	}

	// Validation kind but no structured issue. Fails.
	bare := &AdaptorError{Kind: KindValidation, Message: "just a string"}
	if err := AssertBoundaryValidation(bare); err == nil {
		t.Error("Group F boundary: missing ValidationIssue must fail")
	}

	// Correct shape. Passes.
	good := NewValidationError(ValidationIssue{Path: "x", Reason: "required", Code: "required"})
	if err := AssertBoundaryValidation(good); err != nil {
		t.Errorf("Group F boundary: well-formed validation error should pass: %v", err)
	}
}
