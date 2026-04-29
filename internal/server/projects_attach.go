// projects_attach.go — POST /api/v1/projects/attach (gm-xwa8).
//
// Configure-project modal target. Two operator scenarios:
//
//   1. The picker resolved a beads DB (legacy / imported / cross-machine)
//      that has no .gemba/workspace.toml on this disk — the operator
//      wants to "adopt" it as a gemba project.
//   2. The disk-side .gemba/workspace.toml is stale or missing while the
//      backing beads DB is fine — same UI surface re-points the project
//      marker at an existing repo.
//
// The conversational /new flow is wrong for both; that flow generates
// a fresh project tree from an LLM transcript. Attach is deterministic
// and minimal: write workspace.toml, verify the DB pings, optionally
// `git init` a fresh repo. NO beads writes — adoption MUST NOT seed,
// migrate, or otherwise mutate the operator's existing data.
//
// Atomic semantics borrow the spirit of newproject_ratify.go's
// committed-flag rollback: if any step after we created the dir fails,
// we rm -rf the dir we created. A pre-existing repo we attached to is
// never mutated on rollback.

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// attachProjectRequest is the request body for POST /api/v1/projects/attach.
//
// Exactly one of RepoPath (existing git worktree) or CreateAt (path that
// doesn't exist yet; gemba creates it + `git init`) must be set.
type attachProjectRequest struct {
	BeadsDB     string `json:"beads_db"`
	ProjectName string `json:"project_name"`
	RepoPath    string `json:"repo_path,omitempty"`
	CreateAt    string `json:"create_at,omitempty"`
}

// attachProjectResponse mirrors GET /api/v1/projects' projectEntry
// shape so the picker can re-render with the new entry.
type attachProjectResponse struct {
	projectEntry
}

// BeadsPinger verifies a beads DB URL is reachable. The production
// implementation opens a mysql connection via the go-sql-driver, pings,
// and closes. Tests substitute a stub so they don't need a real Dolt
// server on port 3307.
type BeadsPinger func(ctx context.Context, beadsURL string) error

// defaultBeadsPinger pings the mysql:// URL with a short timeout. We
// intentionally do NOT verify the schema here (no `verifySchema` call
// from internal/adapter/dolt) — adoption is allowed against an empty
// or non-gemba schema; the operator may be wiring up a brand-new DB
// or a legacy one we don't recognise yet.
func defaultBeadsPinger(ctx context.Context, beadsURL string) error {
	dsn, err := beadsDSN(beadsURL)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// beadsDSN translates mysql://user[:pass]@host:port/dbname into the
// go-sql-driver/mysql DSN. Lightweight twin of
// internal/adapter/dolt.parseDoltURL — that helper is unexported and
// also runs schema verification we do not want at attach time.
func beadsDSN(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("beads_db is empty")
	}
	if !strings.HasPrefix(trimmed, "mysql://") {
		return "", fmt.Errorf("beads_db must start with mysql://")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse beads_db: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("beads_db missing host:port")
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return "", fmt.Errorf("beads_db missing database name")
	}
	user := "root"
	pass := ""
	hasPass := false
	if u.User != nil {
		if u.User.Username() != "" {
			user = u.User.Username()
		}
		pass, hasPass = u.User.Password()
	}
	auth := user
	if hasPass {
		auth = user + ":" + pass
	}
	params := "parseTime=true&loc=UTC&readTimeout=5s&writeTimeout=5s"
	if q := u.RawQuery; q != "" {
		params = q + "&" + params
	}
	return fmt.Sprintf("%s@tcp(%s)/%s?%s", auth, u.Host, dbName, params), nil
}

// AttachConfig configures the attach handler. Zero values are fine —
// Pinger nil falls back to defaultBeadsPinger, GitInitRunner nil falls
// back to exec.
type AttachConfig struct {
	Pinger        BeadsPinger
	GitInitRunner CommandRunner
	Now           func() time.Time
	// AdoptableLister backs GET /api/v1/projects/adoptable (gm-gmyl).
	// Nil falls back to the production lister that opens a mysql
	// connection against the configured Dolt server. Tests inject a
	// stub here.
	AdoptableLister AdoptableLister
}

// attachProject handles POST /api/v1/projects/attach.
//
// The handler is wrapped by requireConfirmNonce in router.go, so the
// idempotency guard already covers double-submit. This function focuses
// on the validation + atomic workspace.toml write.
func (r *Router) attachProject(w http.ResponseWriter, req *http.Request) {
	var body attachProjectRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "invalid request body: "+err.Error())
		return
	}

	name := strings.TrimSpace(body.ProjectName)
	if name == "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "project_name is required")
		return
	}
	if !validProjectName(name) {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "project_name must contain only letters, digits, '-', '_'")
		return
	}

	beadsURL := strings.TrimSpace(body.BeadsDB)
	if beadsURL == "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "beads_db is required")
		return
	}

	repoPath := strings.TrimSpace(body.RepoPath)
	createAt := strings.TrimSpace(body.CreateAt)
	if repoPath == "" && createAt == "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "exactly one of repo_path or create_at is required")
		return
	}
	if repoPath != "" && createAt != "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "repo_path and create_at are mutually exclusive")
		return
	}

	pinger := r.attachPinger
	if pinger == nil {
		pinger = defaultBeadsPinger
	}
	gitRunner := r.attachGitRunner
	if gitRunner == nil {
		gitRunner = execRunner
	}
	now := r.attachNow
	if now == nil {
		now = time.Now
	}

	// Resolve target dir.
	var target string
	createdDir := false // tracks whether we mkdir'd a new tree (for rollback)
	if createAt != "" {
		if !filepath.IsAbs(createAt) {
			httperr.Write(w, http.StatusBadRequest,
				"validation", "create_at must be an absolute path")
			return
		}
		if _, err := os.Stat(createAt); err == nil {
			httperr.Write(w, http.StatusConflict,
				"dir_exists", "create_at directory already exists: "+createAt)
			return
		} else if !os.IsNotExist(err) {
			httperr.Write(w, http.StatusInternalServerError,
				"stat_failed", "could not stat create_at: "+err.Error())
			return
		}
		// Create parent + target.
		if err := os.MkdirAll(filepath.Dir(createAt), 0o755); err != nil {
			httperr.Write(w, http.StatusInternalServerError,
				"mkdir_failed", "could not create parent dir: "+err.Error())
			return
		}
		if err := os.MkdirAll(createAt, 0o755); err != nil {
			httperr.Write(w, http.StatusInternalServerError,
				"mkdir_failed", "could not create target dir: "+err.Error())
			return
		}
		createdDir = true
		target = createAt
		// git init the new dir.
		if _, err := gitRunner(req.Context(), target, "git", "init", "--initial-branch=main"); err != nil {
			if _, err2 := gitRunner(req.Context(), target, "git", "init"); err2 != nil {
				_ = os.RemoveAll(target)
				httperr.Write(w, http.StatusInternalServerError,
					"git_init_failed", "git init failed: "+err2.Error())
				return
			}
			_, _ = gitRunner(req.Context(), target, "git", "symbolic-ref", "HEAD", "refs/heads/main")
		}
	} else {
		if !filepath.IsAbs(repoPath) {
			httperr.Write(w, http.StatusBadRequest,
				"validation", "repo_path must be an absolute path")
			return
		}
		info, err := os.Stat(repoPath)
		if err != nil || !info.IsDir() {
			httperr.Write(w, http.StatusBadRequest,
				"validation", "repo_path does not exist or is not a directory")
			return
		}
		// Verify it's a git worktree: presence of .git (file or dir).
		gitDot := filepath.Join(repoPath, ".git")
		if _, err := os.Stat(gitDot); err != nil {
			httperr.Write(w, http.StatusBadRequest,
				"not_a_git_repo", "repo_path is not a git working tree (no .git found)")
			return
		}
		target = repoPath
	}

	rollback := func() {
		if createdDir {
			_ = os.RemoveAll(target)
		}
	}

	// Verify DB is reachable BEFORE we write workspace.toml. If the URL
	// is wrong, we'd rather not litter the operator's repo with a stale
	// marker.
	if err := pinger(req.Context(), beadsURL); err != nil {
		rollback()
		httperr.Write(w, http.StatusBadGateway,
			"beads_db_unreachable", "could not reach beads_db: "+err.Error())
		return
	}

	// Write .gemba/workspace.toml.
	gembaDir := filepath.Join(target, ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		rollback()
		httperr.Write(w, http.StatusInternalServerError,
			"workspace_toml_failed", "could not create .gemba directory: "+err.Error())
		return
	}
	wsTOML := buildAttachWorkspaceTOML(name, beadsURL, now().UTC())
	wsPath := filepath.Join(gembaDir, "workspace.toml")
	if err := os.WriteFile(wsPath, []byte(wsTOML), 0o644); err != nil {
		rollback()
		httperr.Write(w, http.StatusInternalServerError,
			"workspace_toml_failed", "could not write .gemba/workspace.toml: "+err.Error())
		return
	}

	// Mark this project active so the picker reflects the switch.
	activeProjectMu.Lock()
	activeProjectName = name
	activeProjectMu.Unlock()

	resp := attachProjectResponse{
		projectEntry: projectEntry{
			Name:   name,
			Path:   target,
			Active: true,
			Kind:   config.ClassifyProject(target),
		},
	}
	writeJSON(w, http.StatusCreated, resp)
}

// buildAttachWorkspaceTOML synthesises the minimal workspace.toml for
// an adopted project. Only name, beads_db, created_at — adoption MUST
// NOT seed milestones / epics / beads. That's /new's job.
func buildAttachWorkspaceTOML(name, beadsURL string, now time.Time) string {
	var b strings.Builder
	b.WriteString("# Gemba workspace metadata — generated by /api/v1/projects/attach (gm-xwa8).\n")
	b.WriteString("# Adopted: existing beads DB attached to a repo on this machine.\n\n")
	b.WriteString("[project]\n")
	fmt.Fprintf(&b, "name = %q\n", name)
	fmt.Fprintf(&b, "beads_db = %q\n", beadsURL)
	fmt.Fprintf(&b, "created_at = %q\n", now.Format(time.RFC3339))
	return b.String()
}

// AttachProjects wires the projects/attach handler dependencies.
// Optional — zero-value Router uses production defaults
// (defaultBeadsPinger, execRunner, time.Now). Tests call this to
// inject stubs.
func (r *Router) AttachProjects(cfg AttachConfig) {
	if cfg.Pinger != nil {
		r.attachPinger = cfg.Pinger
	}
	if cfg.GitInitRunner != nil {
		r.attachGitRunner = cfg.GitInitRunner
	}
	if cfg.Now != nil {
		r.attachNow = cfg.Now
	}
	if cfg.AdoptableLister != nil {
		r.adoptableLister = cfg.AdoptableLister
	}
}
