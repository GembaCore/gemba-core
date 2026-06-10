// Govern verbs — decision family stub registration tests (gm-o9t8.14).
//
// Mirrors specs_test.go: every decision route returns 501 with a
// stable not_implemented envelope, requires auth, and is declared in
// the embedded OpenAPI document.

package server

import (
	"testing"
)

func TestDecisionsStubs_RegisteredAndAuthed(t *testing.T) {
	runGovStubTable(t, []govStubRoute{
		{"GET", "/api/v1/workspaces/dummy/decisions", "/api/v1/workspaces/{wsid}/decisions", false},
		{"POST", "/api/v1/workspaces/dummy/decisions", "/api/v1/workspaces/{wsid}/decisions", true},
		{"GET", "/api/v1/workspaces/dummy/decisions/refs", "/api/v1/workspaces/{wsid}/decisions/refs", false},
		{"GET", "/api/v1/workspaces/dummy/decisions/abc", "/api/v1/workspaces/{wsid}/decisions/{id}", false},
		{"PATCH", "/api/v1/workspaces/dummy/decisions/abc/lock", "/api/v1/workspaces/{wsid}/decisions/{id}/lock", true},
	})
}
