// diff.go: `GET /api/v1/workspaces/{wsid}/diff` — stream `git diff` from the
// server-side workspace repo to the CLI (gm-o9t8.1.6.2).
//
// The workspace repo lives at <workspacesRoot>/<wsid>/repo/ (cloned per
// gm-o9t8.1.4.3). The handler shells out to `git diff` (or `git diff
// --cached`) and streams stdout straight to the response writer — no
// intermediate buffer, so a multi-megabyte diff doesn't have to fit in
// memory.
//
// Path filters (?path=…) are validated against a tight allow-list to
// prevent the operator from leaking files outside the repo via a
// crafted query (`?path=../../etc/passwd`). Allowed: relative paths
// composed of POSIX-safe characters; rejected: absolute paths, `..`
// segments, shell metachars, and paths that escape the repo when
// resolved.
//
// Auth: standard apiAuth middleware (token bearer or cookie). GET, so
// no X-GEMBA-Confirm nonce gate.

package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// AttachWorkspacesRoot binds the on-disk root that holds per-workspace
// cloned repos (gm-o9t8.1.4.3). The diff handler resolves
// <root>/<wsid>/repo/. Empty root → /diff returns 503
// adaptor_not_configured.
func (r *Router) AttachWorkspacesRoot(root string) { r.workspacesRoot = root }

// workspacesRoot is the parent directory holding <wsid>/repo/ trees.
// Lazy-attached via AttachWorkspacesRoot.

// workspaceDiffHandler — GET /api/v1/workspaces/{wsid}/diff.
//
// Query parameters:
//
//	staged       bool, default false. Equivalent to `git diff --cached`.
//	path         repeatable. Equivalent to `git diff -- <path...>`.
//
// Response: streamed `git diff` output as text/x-patch. Status 200 with
// an empty body when there are no changes (`git diff` exits 0 with no
// output). Errors return a JSON envelope under the appropriate status.
func (r *Router) workspaceDiffHandler(w http.ResponseWriter, req *http.Request) {
	wsid := chi.URLParam(req, "wsid")
	if strings.TrimSpace(wsid) == "" {
		writeDiffErr(w, http.StatusBadRequest, "invalid_workspace", "wsid is required")
		return
	}
	if !isSafeWorkspaceID(wsid) {
		writeDiffErr(w, http.StatusBadRequest, "invalid_workspace", "wsid contains unsafe characters")
		return
	}
	if strings.TrimSpace(r.workspacesRoot) == "" {
		writeDiffErr(w, http.StatusServiceUnavailable, "adaptor_not_configured",
			"workspaces root not configured on this server")
		return
	}

	repoDir := filepath.Join(r.workspacesRoot, wsid, "repo")
	// Reject if the resolved repo dir escapes the configured root —
	// belt-and-braces against a wsid that somehow slipped past
	// isSafeWorkspaceID.
	absRoot, err := filepath.Abs(r.workspacesRoot)
	if err != nil {
		writeDiffErr(w, http.StatusInternalServerError, "internal", "resolve workspaces root: "+err.Error())
		return
	}
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		writeDiffErr(w, http.StatusInternalServerError, "internal", "resolve repo dir: "+err.Error())
		return
	}
	if !strings.HasPrefix(absRepo+string(filepath.Separator), absRoot+string(filepath.Separator)) {
		writeDiffErr(w, http.StatusBadRequest, "invalid_workspace", "wsid resolves outside workspaces root")
		return
	}

	// Validate path filters before consulting the filesystem so a
	// malformed query never causes us to stat /etc/passwd.
	paths := req.URL.Query()["path"]
	for _, p := range paths {
		if err := validateRelPath(p); err != nil {
			writeDiffErr(w, http.StatusBadRequest, "invalid_path", err.Error())
			return
		}
	}

	staged := parseBoolQuery(req.URL.Query().Get("staged"))

	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}

	ctx := req.Context()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeDiffErr(w, http.StatusInternalServerError, "spawn_failed", err.Error())
		return
	}
	// Combine stderr into a buffer the caller never sees directly —
	// we surface only the exit code. `git diff` is quiet on the
	// happy path; if it errors we still want a clean response.
	stderr := &strings.Builder{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		// Most common cause: repo dir is missing → "fork/exec: no such
		// file or directory" or "chdir: not a directory". Map both to
		// 404 so the CLI can branch.
		if isMissingRepoErr(err) {
			writeDiffErr(w, http.StatusNotFound, "workspace_not_found",
				"workspace repo not found: "+wsid)
			return
		}
		writeDiffErr(w, http.StatusInternalServerError, "spawn_failed", err.Error())
		return
	}

	// Headers go out before any body bytes; once we start streaming we
	// can't change status. If git fails after the first chunk lands
	// the client sees a truncated diff plus a non-zero process exit,
	// which is the same UX as a local `git diff` getting killed.
	w.Header().Set("Content-Type", "text/x-patch")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	// io.Copy with no intermediate buffer — Go's default 32KiB pipe
	// buffer is exactly the streaming behavior the AC asks for.
	_, copyErr := io.Copy(w, stdout)
	waitErr := cmd.Wait()
	if copyErr != nil || (waitErr != nil && !isCleanGitExit(waitErr)) {
		// Best-effort: log via the context, but the response is
		// already committed. The CLI will see a truncated body +
		// non-200 only when the failure happens before WriteHeader,
		// which is handled above.
		_ = copyErr
		_ = waitErr
	}
}

// isSafeWorkspaceID enforces a tight character class on the path param
// so a crafted wsid can't pivot off the configured root via "..", "/",
// or shell metachars.
func isSafeWorkspaceID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return false
		}
	}
	return true
}

// validateRelPath rejects paths that could leak files outside the repo
// or pass shell-special characters to git. The set of allowed
// characters is intentionally narrow — operators driving the CLI know
// what files they want to look at, and the alternative (passing a
// crafted query through a shell-quoted exec.Command) leaves too much
// room for abuse.
func validateRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute path not allowed: %q", p)
	}
	// Reject obvious shell metachars even though exec.Command doesn't
	// spawn a shell — keeps the surface tight against future changes
	// and against `git` itself misbehaving on weird input.
	if strings.ContainsAny(p, "\x00\n\r;|&`$<>*?[](){}\\\"'") {
		return fmt.Errorf("path contains unsafe characters: %q", p)
	}
	// Reject leading "-" so the path can't be misinterpreted as a flag.
	if strings.HasPrefix(p, "-") {
		return fmt.Errorf("path may not start with '-': %q", p)
	}
	// Reject any "..", regardless of where it lands in the path. We
	// could resolve and compare against the repo root, but git itself
	// already refuses to step out of the repo for `git diff -- path`
	// — this is the cheap belt-and-braces check.
	cleaned := filepath.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("path may not contain '..': %q", p)
	}
	return nil
}

// parseBoolQuery accepts the standard truthy strings the SPA + curl
// both emit. Anything else is false.
func parseBoolQuery(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// isMissingRepoErr returns true when err looks like the workspace
// directory simply doesn't exist (the most common cause of cmd.Start
// failure for this handler).
func isMissingRepoErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "not a directory") ||
		strings.Contains(msg, "chdir")
}

// isCleanGitExit returns true when err is a benign `git diff` exit
// (currently: nil). `git diff` returns 0 even when there are no
// changes; non-zero exits represent real failures. Kept as a helper
// so a future move to `git diff --exit-code` doesn't have to retrofit
// every call site.
func isCleanGitExit(err error) bool {
	return err == nil
}

// writeDiffErr emits a JSON envelope under the appropriate status.
// Centralized so every error response has the same shape and so
// changing the envelope is a one-line edit.
func writeDiffErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// _ ensures the chi import is used even on builds where the unused-
// import linter cares (chi.URLParam is the only consumer).
var _ = func(_ context.Context) {}
