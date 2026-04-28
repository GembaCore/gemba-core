package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// gm-e12.21.2 — git clone endpoint backing the Create-project modal's
// "Clone from URL" repository option. Wraps `git clone` into a typed
// HTTP surface so the SPA submits a URL and target_dir and receives
// structured success/failure.
//
// Safety:
//   - URL scheme allow list (https://, http://, ssh://, git@host:org/repo)
//   - target_dir resolved + asserted under the same allow-list as
//     gm-e12.21.1's fs/list (HOME or configured projects dir)
//   - target_dir must be empty / not yet existing — refusing to clone
//     into a populated directory protects against operator typos
//   - exec.CommandContext bounds the clone with a deadline, never
//     assembles shell strings

const (
	// cloneTimeout is the upper bound for a single clone. Most repos
	// finish in seconds; a 5-minute ceiling protects against a
	// runaway origin pulling forever without freezing the API
	// process.
	cloneTimeout = 5 * time.Minute

	cloneErrorCode = "projects_clone_error"
)

// cloneProjectRequest is the request body of POST /v1/projects/clone.
type cloneProjectRequest struct {
	URL       string `json:"url"`
	TargetDir string `json:"target_dir"`
}

// cloneProjectResponse is returned on a successful clone. The fields
// are populated from `git rev-parse` after clone completes so the
// SPA can stamp the modal's submit-result banner with concrete
// metadata.
type cloneProjectResponse struct {
	RepoPath      string `json:"repo_path"`
	DefaultBranch string `json:"default_branch,omitempty"`
	HeadSHA       string `json:"head_sha,omitempty"`
}

// cloneProject handles POST /api/v1/projects/clone. Nonce-gated by
// the route registration; this handler assumes the middleware has
// already validated X-GEMBA-Confirm.
func (r *Router) cloneProject(w http.ResponseWriter, req *http.Request) {
	var body cloneProjectRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, cloneErrorCode, "invalid JSON body: "+err.Error())
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	body.TargetDir = strings.TrimSpace(body.TargetDir)

	if body.URL == "" {
		httperr.Write(w, http.StatusBadRequest, "clone_unsupported_url", "url is required")
		return
	}
	if !validCloneURL(body.URL) {
		httperr.Write(w, http.StatusBadRequest, "clone_unsupported_url",
			"only http(s):// and ssh:// (or scp-style git@host:org/repo) URLs are supported")
		return
	}
	if body.TargetDir == "" || !filepath.IsAbs(body.TargetDir) {
		httperr.Write(w, http.StatusBadRequest, cloneErrorCode,
			"target_dir must be a non-empty absolute path")
		return
	}

	roots, _, _ := allowedRoots()
	cleaned := filepath.Clean(body.TargetDir)
	if !pathUnderRoots(cleaned, roots) {
		httperr.Write(w, http.StatusForbidden, cloneErrorCode,
			"target_dir is outside the allowed roots ($HOME and the configured projects dir)")
		return
	}

	// Reject if target_dir exists and is non-empty. An empty existing
	// dir is fine — `git clone` populates it.
	if exists, nonEmpty, err := dirState(cleaned); err != nil {
		httperr.Write(w, http.StatusInternalServerError, cloneErrorCode, "stat target_dir: "+err.Error())
		return
	} else if exists && nonEmpty {
		httperr.Write(w, http.StatusConflict, "clone_already_exists",
			"target_dir is non-empty: "+cleaned)
		return
	}

	// Ensure the parent dir exists so `git clone` doesn't fail with
	// a confusing "no such file" — the picker can hand us a path
	// whose parent already exists, but not always.
	parent := filepath.Dir(cleaned)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		httperr.Write(w, http.StatusInternalServerError, cloneErrorCode, "create parent: "+err.Error())
		return
	}

	runner := r.cloneRunner
	if runner == nil {
		runner = execRunner
	}

	ctx, cancel := context.WithTimeout(req.Context(), cloneTimeout)
	defer cancel()

	if _, err := runner(ctx, parent, "git", "clone", body.URL, cleaned); err != nil {
		// Distinguish unauthorized / unreachable from generic failure
		// so the modal can render an actionable message rather than
		// a stderr blob.
		msg := err.Error()
		switch {
		case strings.Contains(msg, "Authentication failed"),
			strings.Contains(msg, "could not read Username"),
			strings.Contains(msg, "Permission denied"):
			httperr.Write(w, http.StatusUnauthorized, "clone_unauthorized",
				"clone unauthorized — check ssh-agent / credentials: "+msg)
			return
		case strings.Contains(msg, "Could not resolve host"),
			strings.Contains(msg, "unable to access"),
			strings.Contains(msg, "connection timed out"):
			httperr.Write(w, http.StatusBadGateway, "clone_unreachable",
				"clone unreachable: "+msg)
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "clone_failed",
			"git clone failed: "+msg)
		return
	}

	resp := cloneProjectResponse{RepoPath: cleaned}
	// Best-effort metadata pulls — failures don't fail the clone.
	if out, err := runner(ctx, cleaned, "git", "rev-parse", "HEAD"); err == nil {
		resp.HeadSHA = strings.TrimSpace(string(out))
	}
	if out, err := runner(ctx, cleaned, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		resp.DefaultBranch = strings.TrimSpace(string(out))
	}

	writeJSON(w, http.StatusOK, resp)
}

// validCloneURL accepts the schemes git can clone over without
// surprising the operator: http(s)://, ssh://, and the scp-style
// git@host:org/repo. Rejects file:// (clones from the local fs are
// not what this endpoint is for; if the operator wants to add an
// existing local repo they pick "Existing on disk" on the modal's
// Repository axis).
func validCloneURL(u string) bool {
	switch {
	case strings.HasPrefix(u, "https://"):
		return true
	case strings.HasPrefix(u, "http://"):
		return true
	case strings.HasPrefix(u, "ssh://"):
		return true
	}
	// scp-style: <user>@<host>:<path>. Reject if there's no @ before
	// the colon, or if it looks like a Windows drive letter.
	at := strings.Index(u, "@")
	colon := strings.Index(u, ":")
	if at > 0 && colon > at && len(u) > colon+1 && u[colon+1] != '/' {
		return true
	}
	return false
}

// dirState returns (exists, nonEmpty, err). exists=false ⇒ not yet
// created; exists=true && nonEmpty=false ⇒ empty dir we can clone
// into; exists=true && nonEmpty=true ⇒ refuse-to-overwrite case.
func dirState(p string) (bool, bool, error) {
	st, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}
	if !st.IsDir() {
		return true, true, nil // a file at the target — definitely refuse
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return true, false, err
	}
	return true, len(entries) > 0, nil
}
