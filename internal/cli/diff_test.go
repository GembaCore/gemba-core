// diff_test.go: tests for `gemba diff` (gm-o9t8.1.6.2).
//
// Each scenario spins an httptest.Server returning a canned diff body
// (or a chosen status). The CLI runs through cobra; we assert the
// query string the handler saw and the bytes the CLI wrote to stdout.

package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// installDiffStubs swaps the http client + token lookup so the CLI
// targets `srv` and supplies no token. Returns a cleanup hook.
func installDiffStubs(t *testing.T) func() {
	t.Helper()
	prevTok := diffTokenLookup
	prevClient := diffHTTPClient
	diffTokenLookup = func(string) (string, error) { return "", nil }
	diffHTTPClient = func() *http.Client { return http.DefaultClient }
	return func() {
		diffTokenLookup = prevTok
		diffHTTPClient = prevClient
	}
}

// captureDiffServer records every request its handler sees and serves
// a body the test pins. status defaults to 200 when zero.
type captureDiffServer struct {
	method string
	path   string
	rawQ   string
	auth   string
	status int
	body   string
}

func (c *captureDiffServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.method = r.Method
		c.path = r.URL.Path
		c.rawQ = r.URL.RawQuery
		c.auth = r.Header.Get("Authorization")
		if c.status == 0 {
			c.status = http.StatusOK
		}
		w.Header().Set("Content-Type", "text/x-patch")
		w.WriteHeader(c.status)
		_, _ = io.WriteString(w, c.body)
	})
}

func TestDiff_StreamsServerBody(t *testing.T) {
	cap := &captureDiffServer{body: "diff --git a/x b/x\nbody\n"}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	out, _, err := runCmd(t, "diff", "--server", srv.URL, "--workspace", "ws1")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if out != cap.body {
		t.Errorf("stdout mismatch.\n--- got ---\n%q\n--- want ---\n%q", out, cap.body)
	}
	if cap.method != http.MethodGet {
		t.Errorf("method = %s, want GET", cap.method)
	}
	if cap.path != "/api/v1/workspaces/ws1/diff" {
		t.Errorf("path = %s", cap.path)
	}
}

func TestDiff_StagedFlagPropagates(t *testing.T) {
	cap := &captureDiffServer{body: ""}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	_, _, err := runCmd(t, "diff", "--server", srv.URL, "--workspace", "ws1", "--staged")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(cap.rawQ, "staged=true") {
		t.Errorf("query missing staged=true: %q", cap.rawQ)
	}
}

func TestDiff_PathFiltersPropagate(t *testing.T) {
	cap := &captureDiffServer{body: ""}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	_, _, err := runCmd(t, "diff", "--server", srv.URL, "--workspace", "ws1",
		"--", "internal/cli/diff.go", "internal/server/diff.go")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(cap.rawQ, "path=internal%2Fcli%2Fdiff.go") {
		t.Errorf("query missing first path filter: %q", cap.rawQ)
	}
	if !strings.Contains(cap.rawQ, "path=internal%2Fserver%2Fdiff.go") {
		t.Errorf("query missing second path filter: %q", cap.rawQ)
	}
}

func TestDiff_EmptyBodyExitsZeroWithNoOutput(t *testing.T) {
	cap := &captureDiffServer{body: ""}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	out, _, err := runCmd(t, "diff", "--server", srv.URL, "--workspace", "ws1")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty stdout, got %q", out)
	}
}

func TestDiff_UnauthorizedExit4(t *testing.T) {
	cap := &captureDiffServer{status: http.StatusUnauthorized, body: `{"error":"nope"}`}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	_, _, err := runCmd(t, "diff", "--server", srv.URL, "--workspace", "ws1")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	coded, ok := err.(interface{ ExitCode() int })
	if !ok {
		t.Fatalf("error does not carry ExitCode: %T", err)
	}
	if coded.ExitCode() != 4 {
		t.Errorf("exit code = %d, want 4", coded.ExitCode())
	}
}

func TestDiff_NotFoundExit5(t *testing.T) {
	cap := &captureDiffServer{status: http.StatusNotFound, body: `{"error":"missing"}`}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	_, _, err := runCmd(t, "diff", "--server", srv.URL, "--workspace", "ws1")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	coded, ok := err.(interface{ ExitCode() int })
	if !ok {
		t.Fatalf("error does not carry ExitCode: %T", err)
	}
	if coded.ExitCode() != 5 {
		t.Errorf("exit code = %d, want 5", coded.ExitCode())
	}
}

func TestDiff_DefaultWorkspaceIsUnderscore(t *testing.T) {
	cap := &captureDiffServer{body: ""}
	srv := httptest.NewServer(cap.handler())
	defer srv.Close()
	defer installDiffStubs(t)()

	if _, _, err := runCmd(t, "diff", "--server", srv.URL); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if cap.path != "/api/v1/workspaces/_/diff" {
		t.Errorf("path = %s, want default '_'", cap.path)
	}
}
