package api

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// TestBoundaryDecodeFailsAtTransport is the gm-io4 contract test: send
// malformed input through the HTTP boundary decoder and assert that the
// error is a KindValidation AdaptorError with a structured
// ValidationIssue — i.e. it surfaced from the transport layer, not from
// an adaptor method we never invoked.
func TestBoundaryDecodeFailsAtTransport(t *testing.T) {
	req, _ := http.NewRequest("POST", "/api/work-items",
		bytes.NewBufferString(`{"item":{"kind":"","title":"x","status":"open","state_category":"backlog"}}`))
	_, err := DecodeCreateWorkItem(req)
	if err == nil {
		t.Fatal("expected boundary validation error")
	}
	if c := core.AssertBoundaryValidation(err); c != nil {
		t.Fatalf("boundary validation contract violated: %v", c)
	}
}

func TestEmptyBodyRejected(t *testing.T) {
	req, _ := http.NewRequest("POST", "/api/work-items", nil)
	_, err := DecodeCreateWorkItem(req)
	if err == nil {
		t.Fatal("nil body should error")
	}
	issue, _ := core.ValidationIssueOf(err)
	if issue.Code != "empty_body" {
		t.Errorf("issue.Code=%q, want empty_body", issue.Code)
	}
}

func TestDecodeEndSession_WireShape(t *testing.T) {
	req, _ := http.NewRequest("POST", "/api/sessions/end",
		bytes.NewBufferString(`{"session_id":"s1","mode":"completed","nonce":"n"}`))
	sid, mode, nonce, err := DecodeEndSession(req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if sid != "s1" || mode != core.SessionEndCompleted || nonce != "n" {
		t.Errorf("decode drift: %s/%s/%s", sid, mode, nonce)
	}
}
