package jsonl

import (
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// TestBoundaryDecodeFailsAtTransport is the gm-io4 contract test for
// jsonl: a malformed params frame surfaces a KindValidation
// AdaptorError with a structured ValidationIssue. The frame never
// reaches the bound adaptor.
func TestBoundaryDecodeFailsAtTransport(t *testing.T) {
	// Missing required mode on end_session.
	_, _, _, err := DecodeEndSession([]byte(`{"session_id":"s1","nonce":"n"}`))
	if err == nil {
		t.Fatal("want validation error")
	}
	if c := core.AssertBoundaryValidation(err); c != nil {
		t.Fatalf("boundary validation contract: %v", c)
	}
}

func TestDecodeUpdateWorkItem_Shape(t *testing.T) {
	body := []byte(`{"id":"a/b/c","patch":{"title":"n"}}`)
	id, patch, err := DecodeUpdateWorkItem(body)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != "a/b/c" || patch.Title == nil {
		t.Errorf("decode drift: %s / %+v", id, patch)
	}
}
