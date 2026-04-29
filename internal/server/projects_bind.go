// projects_bind.go — POST /api/v1/projects/bind (gm-root.17.14).
//
// The picker's discovery surface (GET /api/v1/projects) classifies
// every entry by setup state: complete | needs_workspace | needs_repo.
// When the operator clicks an incomplete entry, the SPA opens
// BindDialog which posts here to finish the setup.
//
// Two modes:
//
//   mode == "create"
//     - target_repo_path MUST equal beads_db_path.
//     - The server runs `git init` in target_repo_path (no initial
//       commit; the operator may want to commit the existing tree on
//       their own terms) and writes .gemba/workspace.toml.
//
//   mode == "navigate"
//     - target_repo_path MUST be an existing git working tree (a
//       directory containing .git).
//     - The server COPIES the beads DB at beads_db_path/.beads/ into
//       target_repo_path/.beads/ (preserving content) and writes
//       .gemba/workspace.toml at target_repo_path. The original DB
//       stays in place — moving is a follow-up bead so a partial
//       failure can't strand the operator's only copy.
//
// On success the handler returns the resolved final ProjectEntry with
// kind = "complete" so the SPA can switch the active workspace
// without an extra round-trip. The handler is wrapped by
// requireConfirmNonce in router.go (same pattern as
// /v1/newproject/create and /v1/projects/attach).

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// BindProjectRequest is the request body for POST /api/v1/projects/bind.
type BindProjectRequest struct {
	// BeadsDBPath is the absolute path to the directory holding the
	// beads DB (the parent of the `.beads/` subdir). The handler
	// verifies the directory has a `.beads/` child before doing any
	// work.
	BeadsDBPath string `json:"beads_db_path"`
	// TargetRepoPath is the absolute path to the directory that will
	// hold the final project. For mode == "create" this MUST equal
	// BeadsDBPath. For mode == "navigate" this MUST be a pre-existing
	// git working tree.
	TargetRepoPath string `json:"target_repo_path"`
	// Mode is "create" or "navigate" — see the file header.
	Mode string `json:"mode"`
}

// bindProject handles POST /api/v1/projects/bind.
//
// Wrapped by requireConfirmNonce in router.go so the idempotency guard
// covers double-submit. This function focuses on validation, the
// per-mode side effects, and the response shape.
func (r *Router) bindProject(w http.ResponseWriter, req *http.Request) {
	var body BindProjectRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "invalid request body: "+err.Error())
		return
	}

	beadsDBPath := strings.TrimSpace(body.BeadsDBPath)
	targetPath := strings.TrimSpace(body.TargetRepoPath)
	mode := strings.TrimSpace(body.Mode)

	if beadsDBPath == "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "beads_db_path is required")
		return
	}
	if !filepath.IsAbs(beadsDBPath) {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "beads_db_path must be an absolute path")
		return
	}
	if targetPath == "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "target_repo_path is required")
		return
	}
	if !filepath.IsAbs(targetPath) {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "target_repo_path must be an absolute path")
		return
	}
	if mode != "create" && mode != "navigate" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "mode must be \"create\" or \"navigate\"")
		return
	}

	// Source must exist and contain a beads DB.
	srcInfo, err := os.Stat(beadsDBPath)
	if err != nil {
		if os.IsNotExist(err) {
			httperr.Write(w, http.StatusBadRequest,
				"validation", "beads_db_path does not exist")
			return
		}
		httperr.Write(w, http.StatusInternalServerError,
			"stat_failed", "could not stat beads_db_path: "+err.Error())
		return
	}
	if !srcInfo.IsDir() {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "beads_db_path is not a directory")
		return
	}
	srcBeads := filepath.Join(beadsDBPath, ".beads")
	if info, err := os.Stat(srcBeads); err != nil || !info.IsDir() {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "beads_db_path does not contain a .beads/ directory")
		return
	}

	gitRunner := r.attachGitRunner
	if gitRunner == nil {
		gitRunner = execRunner
	}
	now := r.attachNow
	if now == nil {
		now = time.Now
	}

	switch mode {
	case "create":
		if targetPath != beadsDBPath {
			httperr.Write(w, http.StatusBadRequest,
				"validation", "for mode=\"create\" target_repo_path must equal beads_db_path")
			return
		}
		// `git init` if not already a repo. Never overwrite an existing
		// .git/ — that's the operator's repo and we don't fight it.
		if !hasGitRepoDir(targetPath) {
			if _, err := gitRunner(req.Context(), targetPath, "git", "init", "--initial-branch=main"); err != nil {
				if _, err2 := gitRunner(req.Context(), targetPath, "git", "init"); err2 != nil {
					httperr.Write(w, http.StatusInternalServerError,
						"git_init_failed", "git init failed: "+err2.Error())
					return
				}
				_, _ = gitRunner(req.Context(), targetPath, "git", "symbolic-ref", "HEAD", "refs/heads/main")
			}
		}
	case "navigate":
		// Target must already be a git repo.
		tInfo, err := os.Stat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				httperr.Write(w, http.StatusBadRequest,
					"validation", "target_repo_path does not exist")
				return
			}
			httperr.Write(w, http.StatusInternalServerError,
				"stat_failed", "could not stat target_repo_path: "+err.Error())
			return
		}
		if !tInfo.IsDir() {
			httperr.Write(w, http.StatusBadRequest,
				"validation", "target_repo_path is not a directory")
			return
		}
		if !hasGitRepoDir(targetPath) {
			httperr.Write(w, http.StatusBadRequest,
				"not_a_git_repo", "target_repo_path is not a git working tree (no .git found)")
			return
		}
		// Refuse to clobber an existing .beads at the target — that
		// would silently replace the operator's data. The picker can
		// re-classify and let the operator pick a different action.
		dstBeads := filepath.Join(targetPath, ".beads")
		if _, err := os.Stat(dstBeads); err == nil {
			httperr.Write(w, http.StatusConflict,
				"beads_dir_exists",
				"target_repo_path already contains a .beads/ directory")
			return
		}
		// Copy beads DB into target/.beads/.
		if err := copyDir(req.Context(), srcBeads, dstBeads); err != nil {
			httperr.Write(w, http.StatusInternalServerError,
				"beads_copy_failed", "failed to copy beads DB: "+err.Error())
			return
		}
	}

	// Write workspace.toml at the resolved target.
	name := filepath.Base(targetPath)
	gembaDir := filepath.Join(targetPath, ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"workspace_toml_failed", "could not create .gemba directory: "+err.Error())
		return
	}
	wsPath := filepath.Join(gembaDir, "workspace.toml")
	// Don't clobber an existing workspace.toml — that's the
	// "needs_repo" case where the marker was already there. Re-running
	// bind on a needs_repo dir should be idempotent: leave the marker
	// alone, the missing piece was the git repo.
	if _, err := os.Stat(wsPath); err != nil {
		if !os.IsNotExist(err) {
			httperr.Write(w, http.StatusInternalServerError,
				"workspace_toml_failed", "could not stat workspace.toml: "+err.Error())
			return
		}
		wsTOML := buildBindWorkspaceTOML(name, now().UTC())
		if err := os.WriteFile(wsPath, []byte(wsTOML), 0o644); err != nil {
			httperr.Write(w, http.StatusInternalServerError,
				"workspace_toml_failed", "could not write workspace.toml: "+err.Error())
			return
		}
	}

	// Mark this project active so the picker reflects the bind.
	activeProjectMu.Lock()
	activeProjectName = name
	activeProjectMu.Unlock()

	resp := projectEntry{
		Name:   name,
		Path:   targetPath,
		Active: true,
		Kind:   config.ClassifyProject(targetPath),
	}
	writeJSON(w, http.StatusOK, resp)
}

// hasGitRepoDir mirrors config.hasGitRepo (unexported there) so the
// handler doesn't need to round-trip through the package boundary for
// a single os.Stat. .git may be either a directory or a file
// (worktree pointer).
func hasGitRepoDir(dir string) bool {
	gitDot := filepath.Join(dir, ".git")
	_, err := os.Stat(gitDot)
	return err == nil
}

// buildBindWorkspaceTOML synthesises the minimal workspace.toml for
// a bound project. Only name + created_at — bind is a setup-completion
// step, not a project-creation step. The beads_db field is intentionally
// omitted because the local `.beads/` directory IS the beads DB; a
// later edit to the file may add a remote URL.
func buildBindWorkspaceTOML(name string, now time.Time) string {
	var b strings.Builder
	b.WriteString("# Gemba workspace metadata — generated by /api/v1/projects/bind (gm-root.17.14).\n")
	b.WriteString("# Bound: existing beads DB attached to a repo on this machine.\n\n")
	b.WriteString("[project]\n")
	fmt.Fprintf(&b, "name = %q\n", name)
	fmt.Fprintf(&b, "created_at = %q\n", now.Format(time.RFC3339))
	return b.String()
}

// copyDir recursively copies src → dst, preserving file mode bits.
// The implementation is intentionally small — beads DBs are typically
// a handful of small files (sql/parquet shards, indexes), not a deep
// tree. Tests that need to fault-inject substitute a stub via the
// CommandRunner abstraction (we shell out for git but copy in-process
// because that gives clearer errors than `cp -r`).
func copyDir(ctx context.Context, src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		// Regular file.
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}
