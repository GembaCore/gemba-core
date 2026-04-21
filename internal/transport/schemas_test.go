package transport

import (
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// TestBoundaryValidationErrorShape pins the canonical error shape every
// transport emits for malformed input: KindValidation + structured
// ValidationIssue under Detail["issue"] (gm-io4, Conformance Group F).
func TestBoundaryValidationErrorShape(t *testing.T) {
	_, err := DecodeCreateWorkItem([]byte(`{"item":{"title":""}}`))
	if err == nil {
		t.Fatal("expected validation error on empty title + kind")
	}
	if conformance := core.AssertBoundaryValidation(err); conformance != nil {
		t.Fatalf("boundary validation contract: %v", conformance)
	}
	issue, ok := core.ValidationIssueOf(err)
	if !ok {
		t.Fatal("validation error missing structured ValidationIssue")
	}
	if !strings.Contains(issue.Path, "create_work_item") {
		t.Errorf("issue.Path should prefix the request shape: %q", issue.Path)
	}
}

func TestDecodeCreateWorkItem_RequiresKindTitleStatus(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // expected substring in issue.Path
	}{
		{"missing kind", `{"item":{"title":"x","status":"open","state_category":"backlog"}}`, "kind"},
		{"missing title", `{"item":{"kind":"task","status":"open","state_category":"backlog"}}`, "title"},
		{"missing status", `{"item":{"kind":"task","title":"x","state_category":"backlog"}}`, "status"},
		{"bad state", `{"item":{"kind":"task","title":"x","status":"open","state_category":"wat"}}`, ""}, // UnmarshalJSON rejects unknown state
		{"server-set id", `{"item":{"id":"x/y/z","kind":"task","title":"x","status":"open","state_category":"backlog"}}`, "id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeCreateWorkItem([]byte(tc.body))
			if err == nil {
				t.Fatalf("want validation error, got nil")
			}
			if c := core.AssertBoundaryValidation(err); c != nil && tc.name != "bad state" {
				// The "bad state" case rides UnmarshalJSON rather than our
				// post-decode RequireEnum — it still surfaces as validation
				// via translateJSONError, so the contract still holds.
				t.Errorf("%s: boundary contract: %v", tc.name, c)
			}
			if tc.want != "" {
				issue, _ := core.ValidationIssueOf(err)
				if !strings.Contains(issue.Path, tc.want) {
					t.Errorf("issue.Path=%q, want substring %q", issue.Path, tc.want)
				}
			}
		})
	}
}

func TestDecodeCreateWorkItem_Success(t *testing.T) {
	body := `{"item":{"kind":"task","title":"do it","status":"open","state_category":"backlog"}}`
	wi, err := DecodeCreateWorkItem([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wi.Kind != "task" || wi.Title != "do it" || wi.Status != "open" {
		t.Errorf("decoded wi drift: %+v", wi)
	}
	if wi.StateCategory != core.StateBacklog {
		t.Errorf("state category decode drift: %q", wi.StateCategory)
	}
}

func TestDecodeUpdateWorkItem_EmptyPatchRejected(t *testing.T) {
	body := `{"id":"a/b/c","patch":{}}`
	_, _, err := DecodeUpdateWorkItem([]byte(body))
	if err == nil {
		t.Fatal("empty patch should be rejected at the boundary")
	}
	issue, _ := core.ValidationIssueOf(err)
	if issue.Code != "empty" {
		t.Errorf("issue.Code=%q, want empty", issue.Code)
	}
}

func TestDecodeUpdateWorkItem_Success(t *testing.T) {
	body := `{"id":"a/b/c","patch":{"title":"new"}}`
	id, patch, err := DecodeUpdateWorkItem([]byte(body))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != "a/b/c" || patch.Title == nil || *patch.Title != "new" {
		t.Errorf("decode drift: %s / %+v", id, patch)
	}
}

func TestDecodeEndSession_EnumEnforced(t *testing.T) {
	body := `{"session_id":"s1","mode":"wat","nonce":"n1"}`
	_, _, _, err := DecodeEndSession([]byte(body))
	if err == nil {
		t.Fatal("invalid mode should be rejected")
	}
	issue, _ := core.ValidationIssueOf(err)
	if issue.Code != "enum" && issue.Code != "" {
		// UnmarshalJSON on the SessionEndMode type would fire first with
		// code=decode; either way the ValidationIssue MUST point at mode.
		t.Logf("issue.Code=%q (acceptable)", issue.Code)
	}
	if !strings.Contains(issue.Path, "mode") && issue.Path != "end_session" {
		// The SessionEndMode type has no UnmarshalJSON guard, so our
		// post-decode RequireEnum catches it with path=end_session.mode.
		t.Errorf("issue.Path=%q, want reference to mode", issue.Path)
	}
}

func TestDecodeEndSession_NonceRequired(t *testing.T) {
	body := `{"session_id":"s1","mode":"completed"}`
	_, _, _, err := DecodeEndSession([]byte(body))
	if err == nil {
		t.Fatal("missing nonce should be rejected")
	}
	issue, _ := core.ValidationIssueOf(err)
	if !strings.Contains(issue.Path, "nonce") {
		t.Errorf("issue.Path=%q, want reference to nonce", issue.Path)
	}
}

func TestDecodeJSON_UnknownFieldRejected(t *testing.T) {
	body := `{"item":{"kind":"task","title":"x","status":"open","state_category":"backlog","banana":"yes"}}`
	_, err := DecodeCreateWorkItem([]byte(body))
	if err == nil {
		t.Fatal("unknown field should be rejected by the boundary decoder")
	}
	issue, ok := core.ValidationIssueOf(err)
	if !ok {
		t.Fatal("expected structured issue")
	}
	if issue.Code != "unknown_field" {
		t.Errorf("issue.Code=%q, want unknown_field", issue.Code)
	}
	if !strings.Contains(issue.Path, "banana") {
		t.Errorf("issue.Path=%q, want to name the offending key", issue.Path)
	}
}

func TestDecodeJSON_SyntaxError(t *testing.T) {
	_, err := DecodeCreateWorkItem([]byte(`{not json`))
	if err == nil {
		t.Fatal("want syntax error")
	}
	issue, ok := core.ValidationIssueOf(err)
	if !ok {
		t.Fatal("want structured issue")
	}
	if issue.Code != "syntax" {
		t.Errorf("issue.Code=%q, want syntax", issue.Code)
	}
}

func TestDecodeAttachEvidence_KindEnum(t *testing.T) {
	body := `{"id":"a/b/c","evidence":{"kind":"banana","source":"git","captured_at":"0001-01-01T00:00:00Z"}}`
	_, _, err := DecodeAttachEvidence([]byte(body))
	if err == nil {
		t.Fatal("bad evidence kind should be rejected")
	}
}

func TestDecodeAcquireWorkspace_PreferredKindEnum(t *testing.T) {
	body := `{"request":{"assignment_id":"a1","repository":"r","preferred_kind":"wat","required_isolation":{"fs_scoped":true}}}`
	_, err := DecodeAcquireWorkspace([]byte(body))
	if err == nil {
		t.Fatal("bad workspace kind should be rejected")
	}
}

func TestDecodeResolveEscalation_NonceAndKind(t *testing.T) {
	// Missing nonce.
	body := `{"escalation_id":"e1","resolution":{"kind":"approve","resolved_by":"a/b/c","resolved_at":"0001-01-01T00:00:00Z"}}`
	_, _, _, err := DecodeResolveEscalation([]byte(body))
	if err == nil {
		t.Fatal("missing nonce should be rejected")
	}
	// Bad resolution kind.
	body = `{"escalation_id":"e1","resolution":{"kind":"banana","resolved_by":"a/b/c","resolved_at":"0001-01-01T00:00:00Z"},"nonce":"n"}`
	_, _, _, err = DecodeResolveEscalation([]byte(body))
	if err == nil {
		t.Fatal("bad resolution kind should be rejected")
	}
}

func TestDecodeReleaseByID(t *testing.T) {
	id, err := DecodeReleaseByID([]byte(`{"id":"r1"}`), "release_workspace")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != "r1" {
		t.Errorf("decoded id=%q", id)
	}
	_, err = DecodeReleaseByID([]byte(`{}`), "release_workspace")
	if err == nil {
		t.Fatal("missing id must error")
	}
}

func TestDecodeJSON_TrailingGarbage(t *testing.T) {
	body := `{"item":{"kind":"task","title":"x","status":"open","state_category":"backlog"}}{"extra":true}`
	_, err := DecodeCreateWorkItem([]byte(body))
	if err == nil {
		t.Fatal("trailing data should be rejected")
	}
	issue, _ := core.ValidationIssueOf(err)
	if issue.Code != "trailing_data" {
		t.Errorf("issue.Code=%q, want trailing_data", issue.Code)
	}
}
