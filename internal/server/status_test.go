// Convenience verbs — workspace status stub registration test (gm-o9t8.14).

package server

import "testing"

func TestWorkspaceStatusStub_RegisteredAndAuthed(t *testing.T) {
	runGovStubTable(t, []govStubRoute{
		{"GET", "/api/v1/workspaces/dummy/status", "/api/v1/workspaces/{wsid}/status", false},
	})
}
