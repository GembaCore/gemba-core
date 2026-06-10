// Govern verbs — labels stub registration test (gm-o9t8.14).

package server

import "testing"

func TestLabelsStub_RegisteredAndAuthed(t *testing.T) {
	runGovStubTable(t, []govStubRoute{
		{"GET", "/api/v1/workspaces/dummy/labels", "/api/v1/workspaces/{wsid}/labels", false},
	})
}
