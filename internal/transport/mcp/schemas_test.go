package mcp

import (
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

// TestBoundaryDecodeFailsAtTransport is the gm-io4 contract test for
// mcp: malformed tool-call arguments surface a KindValidation
// AdaptorError with a structured ValidationIssue — the bound adaptor
// never sees the request.
func TestBoundaryDecodeFailsAtTransport(t *testing.T) {
	// Missing required assignment_id on start_session.
	_, _, err := DecodeStartSession(map[string]any{"prompt": map[string]any{"text": "go"}})
	if err == nil {
		t.Fatal("want validation error")
	}
	if c := core.AssertBoundaryValidation(err); c != nil {
		t.Fatalf("boundary validation contract: %v", c)
	}
}

func TestNilArgsRejected(t *testing.T) {
	_, _, err := DecodeStartSession(nil)
	if err == nil {
		t.Fatal("nil args should error")
	}
	issue, _ := core.ValidationIssueOf(err)
	if issue.Code != "empty_arguments" {
		t.Errorf("issue.Code=%q, want empty_arguments", issue.Code)
	}
}

func TestDecodeEndSession_Shape(t *testing.T) {
	args := map[string]any{
		"session_id": "s1",
		"mode":       "completed",
		"nonce":      "n",
	}
	sid, mode, nonce, err := DecodeEndSession(args)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if sid != "s1" || mode != core.SessionEndCompleted || nonce != "n" {
		t.Errorf("decode drift: %s/%s/%s", sid, mode, nonce)
	}
}
