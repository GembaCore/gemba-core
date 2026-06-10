// bead_crud_test.go: tests for the `gemba bead {create,list,show,
// update,close,note}` CLI surface (gm-o9t8.1.3.2).
//
// Pattern mirrors login_test.go: spin up an httptest.Server, hand the
// typed client a WithToken option pointed at it, run the cobra
// subcommand, and assert the HTTP method/path/body the handler saw
// plus the bytes the CLI wrote to stdout/stderr.

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gembaclient "github.com/GembaCore/gemba-core/internal/client"
)

// fakeServer captures the request a handler saw plus a per-test
// configurable response.
type fakeServer struct {
	t       *testing.T
	method  string
	path    string
	confirm string
	body    []byte
	status  int
	respond func(w http.ResponseWriter, r *http.Request)
}

func (f *fakeServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.method = r.Method
		f.path = r.URL.Path + queryString(r)
		f.confirm = r.Header.Get("X-GEMBA-Confirm")
		body, _ := io.ReadAll(r.Body)
		f.body = body
		f.status = http.StatusOK
		if f.respond != nil {
			f.respond(w, r)
			return
		}
		_, _ = w.Write([]byte("{}"))
	})
}

func queryString(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}

// installFakeClient swaps crudFactory so the cobra surface routes
// through a typed client pointed at srv. Returns the cleanup hook.
func installFakeClient(t *testing.T, srvURL string) func() {
	t.Helper()
	restore := withCrudFactory(func(server string) (*gembaclient.Client, error) {
		return gembaclient.New(srvURL,
			gembaclient.WithToken("TESTPAT"),
			gembaclient.WithHTTPClient(http.DefaultClient),
		)
	})
	return restore
}

func TestBeadCreate_PostsItemEnvelope(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gm-abc","title":"do thing","kind":"task","status":"todo","state_category":"unstarted"}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "create",
		"--server", srv.URL,
		"--title", "do thing",
		"--label", "urgent",
		"--label", "auth",
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if fs.method != http.MethodPost {
		t.Errorf("method: want POST, got %s", fs.method)
	}
	if fs.path != "/api/work-items" {
		t.Errorf("path: want /api/work-items, got %s", fs.path)
	}
	if fs.confirm == "" {
		t.Errorf("expected X-GEMBA-Confirm header to be set")
	}
	var body struct {
		Item map[string]any `json:"item"`
	}
	if err := json.Unmarshal(fs.body, &body); err != nil {
		t.Fatalf("body decode: %v (raw=%s)", err, string(fs.body))
	}
	if body.Item["title"] != "do thing" {
		t.Errorf("item.title: want %q, got %v", "do thing", body.Item["title"])
	}
	if !strings.Contains(out, "gm-abc") {
		t.Errorf("stdout: want gm-abc, got %q", out)
	}
}

func TestBeadList_Empty(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "list", "--server", srv.URL)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if fs.method != http.MethodGet || !strings.HasPrefix(fs.path, "/api/work-items") {
		t.Errorf("expected GET /api/work-items; got %s %s", fs.method, fs.path)
	}
	if !strings.Contains(out, "(no beads)") {
		t.Errorf("stdout: %q", out)
	}
}

func TestBeadList_WithItems(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[
			{"id":"gm-1","title":"a","kind":"task","status":"todo","state_category":"unstarted"},
			{"id":"gm-2","title":"b","kind":"task","status":"done","state_category":"completed"}
		]}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "list", "--server", srv.URL)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "gm-1") || !strings.Contains(out, "gm-2") {
		t.Errorf("missing ids in output: %q", out)
	}
}

func TestBeadShow_JSON(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gm-x","title":"hello","kind":"task","status":"todo","state_category":"unstarted"}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "show", "gm-x", "--server", srv.URL, "--json")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if fs.path != "/api/work-items/gm-x" {
		t.Errorf("path: %s", fs.path)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout not valid JSON: %v (out=%q)", err, out)
	}
	if got["id"] != "gm-x" {
		t.Errorf("id: %v", got["id"])
	}
}

func TestBeadUpdate_PatchesTitle(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gm-x","title":"new","kind":"task","status":"todo","state_category":"unstarted"}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "update", "gm-x", "--server", srv.URL, "--title", "new")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if fs.method != http.MethodPatch {
		t.Errorf("method: %s", fs.method)
	}
	if fs.path != "/api/work-items/gm-x" {
		t.Errorf("path: %s", fs.path)
	}
	var patch map[string]any
	if err := json.Unmarshal(fs.body, &patch); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if patch["title"] != "new" {
		t.Errorf("patch.title: %v", patch["title"])
	}
	if !strings.Contains(out, "updated gm-x") {
		t.Errorf("stdout: %q", out)
	}
}

func TestBeadClose_PatchesStateCompleted(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gm-x","title":"x","kind":"task","status":"closed","state_category":"completed"}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "close", "gm-x", "--server", srv.URL, "--reason", "done")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if fs.method != http.MethodPatch {
		t.Errorf("method: %s", fs.method)
	}
	var patch map[string]any
	if err := json.Unmarshal(fs.body, &patch); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if patch["state_category"] != "completed" {
		t.Errorf("state_category: %v", patch["state_category"])
	}
	if !strings.Contains(out, "closed gm-x") {
		t.Errorf("stdout: %q", out)
	}
}

func TestBeadNote_PostsNoteBody(t *testing.T) {
	fs := &fakeServer{t: t}
	fs.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	out, _, err := runCmd(t, "bead", "note", "gm-x", "this", "is", "a", "note", "--server", srv.URL)
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if fs.path != "/api/work-items/gm-x/notes" {
		t.Errorf("path: %s", fs.path)
	}
	var got map[string]string
	if err := json.Unmarshal(fs.body, &got); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if got["note"] != "this is a note" {
		t.Errorf("note body: %q", got["note"])
	}
	if !strings.Contains(out, "note appended to gm-x") {
		t.Errorf("stdout: %q", out)
	}
}

// Error mapping: 401 → exit 4.
func TestBeadShow_UnauthorizedExitCode(t *testing.T) {
	fs := &fakeServer{t: t, respond: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token expired","code":"unauthorized"}`))
	}}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	_, stderr, err := runCmd(t, "bead", "show", "gm-x", "--server", srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	ee, ok := err.(*exitError)
	if !ok {
		t.Fatalf("want *exitError, got %T (%v)", err, err)
	}
	if ee.ExitCode() != 4 {
		t.Errorf("exit code: want 4, got %d", ee.ExitCode())
	}
	if !strings.Contains(stderr, "unauthorized") {
		t.Errorf("stderr: %q", stderr)
	}
}

// Error mapping: 404 → exit 5.
func TestBeadShow_NotFoundExitCode(t *testing.T) {
	fs := &fakeServer{t: t, respond: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found","message":"bead missing"}`))
	}}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	_, _, err := runCmd(t, "bead", "show", "gm-nope", "--server", srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	ee, ok := err.(*exitError)
	if !ok {
		t.Fatalf("want *exitError, got %T", err)
	}
	if ee.ExitCode() != 5 {
		t.Errorf("exit code: want 5, got %d", ee.ExitCode())
	}
}

// E2E acceptance: create → list → show against one in-memory server.
// The server keeps the items it sees so list returns the created one
// and show returns the same shape.
func TestBeadCRUD_E2ECreateListShow(t *testing.T) {
	type store struct{ items []map[string]any }
	st := &store{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/work-items":
			var req struct {
				Item map[string]any `json:"item"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			req.Item["id"] = "gm-e2e"
			st.items = append(st.items, req.Item)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(req.Item)
		case r.Method == http.MethodGet && r.URL.Path == "/api/work-items":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": st.items})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/work-items/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/work-items/")
			for _, it := range st.items {
				if it["id"] == id {
					_ = json.NewEncoder(w).Encode(it)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer installFakeClient(t, srv.URL)()

	// create
	createOut, _, err := runCmd(t, "bead", "create",
		"--server", srv.URL, "--title", "e2e roundtrip")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(createOut, "gm-e2e") {
		t.Fatalf("create stdout: %q", createOut)
	}

	// list — should show the new id
	listOut, _, err := runCmd(t, "bead", "list", "--server", srv.URL)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "gm-e2e") {
		t.Fatalf("list stdout did not include gm-e2e: %q", listOut)
	}

	// show — should return the same item
	showOut, _, err := runCmd(t, "bead", "show", "gm-e2e",
		"--server", srv.URL, "--json")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(showOut), &got); err != nil {
		t.Fatalf("show stdout not JSON: %v", err)
	}
	if got["id"] != "gm-e2e" || got["title"] != "e2e roundtrip" {
		t.Fatalf("show payload: %+v", got)
	}
}
