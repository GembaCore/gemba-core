package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	nativeinstall "github.com/GembaCore/gemba-core/internal/adapter/native/install"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

const (
	onboardingOriginNew      = "new"
	onboardingOriginExisting = "existing"
	onboardingOriginImport   = "import"

	onboardingOrchestrationNative  = "native"
	onboardingOrchestrationGastown = "gastown"

	onboardingSourceGitNexus = "gitnexus"
	onboardingSourceNone     = "none"

	runtimeContextStart = "<!-- gemba-runtime-context:start -->"
	runtimeContextEnd   = "<!-- gemba-runtime-context:end -->"
)

type onboardingSetupRequest struct {
	Origin             string `json:"origin"`
	ProjectName        string `json:"project_name"`
	GitHubProject      string `json:"github_project"`
	Orchestration      string `json:"orchestration"`
	WorktreePath       string `json:"worktree_path,omitempty"`
	GastownLocation    string `json:"gastown_location,omitempty"`
	GastownRig         string `json:"gastown_rig,omitempty"`
	GastownWorktree    string `json:"gastown_worktree_path,omitempty"`
	BeadsURL           string `json:"beads_url,omitempty"`
	SourceAnalysisTool string `json:"source_analysis_tool,omitempty"`
}

type onboardingSetupFrame struct {
	Seq   int    `json:"seq"`
	Line  string `json:"line"`
	Level string `json:"level"`
	Done  bool   `json:"done"`
}

type onboardingSetupResponse struct {
	SetupID     string                 `json:"setup_id"`
	ProjectPath string                 `json:"project_path,omitempty"`
	Frames      []onboardingSetupFrame `json:"frames"`
	Warnings    []string               `json:"warnings,omitempty"`
	Checks      map[string]string      `json:"checks,omitempty"`
}

func (r *Router) onboardingSetup(w http.ResponseWriter, req *http.Request) {
	var body onboardingSetupRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", "invalid request body: "+err.Error())
		return
	}
	body.normalize()
	if err := body.validate(); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", err.Error())
		return
	}

	runner := r.attachGitRunner
	if runner == nil {
		runner = execRunner
	}
	now := r.attachNow
	if now == nil {
		now = time.Now
	}

	tx := &onboardingSetupTxn{
		req:     body,
		runner:  runner,
		now:     now,
		cfg:     r.cfg,
		checks:  map[string]string{},
		setupID: "setup-" + newInstanceID(),
	}
	if err := tx.run(req.Context()); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "onboarding_setup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, onboardingSetupResponse{
		SetupID:     tx.setupID,
		ProjectPath: tx.projectPath,
		Frames:      tx.frames,
		Warnings:    tx.warnings,
		Checks:      tx.checks,
	})
}

func (b *onboardingSetupRequest) normalize() {
	b.Origin = strings.ToLower(strings.TrimSpace(b.Origin))
	b.ProjectName = strings.TrimSpace(b.ProjectName)
	b.GitHubProject = strings.TrimSpace(b.GitHubProject)
	b.Orchestration = strings.ToLower(strings.TrimSpace(b.Orchestration))
	b.WorktreePath = strings.TrimSpace(b.WorktreePath)
	b.GastownLocation = strings.TrimSpace(b.GastownLocation)
	b.GastownRig = strings.TrimSpace(b.GastownRig)
	b.GastownWorktree = strings.TrimSpace(b.GastownWorktree)
	b.BeadsURL = strings.TrimSpace(b.BeadsURL)
	b.SourceAnalysisTool = strings.ToLower(strings.TrimSpace(b.SourceAnalysisTool))
	if b.Origin == "" {
		b.Origin = onboardingOriginNew
	}
	if b.Orchestration == "" {
		b.Orchestration = onboardingOrchestrationNative
	}
	if b.SourceAnalysisTool == "" {
		b.SourceAnalysisTool = onboardingSourceGitNexus
	}
}

func (b onboardingSetupRequest) validate() error {
	switch b.Origin {
	case onboardingOriginNew, onboardingOriginExisting, onboardingOriginImport:
	default:
		return fmt.Errorf("origin must be one of new, existing, import")
	}
	if b.ProjectName == "" {
		return fmt.Errorf("project_name is required")
	}
	if !validProjectName(b.ProjectName) {
		return fmt.Errorf("project_name must contain only letters, digits, '-', '_'")
	}
	if b.GitHubProject == "" {
		return fmt.Errorf("github_project is required")
	}
	switch b.Orchestration {
	case onboardingOrchestrationNative, onboardingOrchestrationGastown:
	default:
		return fmt.Errorf("orchestration must be one of native, gastown")
	}
	switch b.SourceAnalysisTool {
	case onboardingSourceGitNexus, onboardingSourceNone:
	default:
		return fmt.Errorf("source_analysis_tool must be one of gitnexus, none")
	}
	if b.Orchestration == onboardingOrchestrationGastown {
		if b.GastownLocation == "" {
			return fmt.Errorf("gastown_location is required for Gas Town onboarding")
		}
		if !filepath.IsAbs(b.GastownLocation) {
			return fmt.Errorf("gastown_location must be an absolute path")
		}
		if b.GastownWorktree != "" && !filepath.IsAbs(b.GastownWorktree) {
			return fmt.Errorf("gastown_worktree_path must be an absolute path")
		}
		if b.BeadsURL != "" && !strings.HasPrefix(b.BeadsURL, "mysql://") {
			return fmt.Errorf("beads_url must be a mysql:// Dolt URL")
		}
		return nil
	}
	if b.WorktreePath == "" {
		return fmt.Errorf("worktree_path is required for native onboarding")
	}
	if !filepath.IsAbs(b.WorktreePath) {
		return fmt.Errorf("worktree_path must be an absolute path")
	}
	return nil
}

type onboardingSetupTxn struct {
	req         onboardingSetupRequest
	runner      CommandRunner
	now         func() time.Time
	cfg         config.ServeConfig
	setupID     string
	projectPath string
	frames      []onboardingSetupFrame
	warnings    []string
	checks      map[string]string
}

func (t *onboardingSetupTxn) run(ctx context.Context) error {
	if t.req.Orchestration == onboardingOrchestrationGastown {
		return t.runGasTown(ctx)
	}
	target := t.targetPath()
	t.projectPath = target
	t.info("Starting deterministic onboarding setup for %s.", t.req.ProjectName)
	if t.req.Origin == onboardingOriginNew {
		if err := t.prepareNewProject(ctx, target); err != nil {
			return err
		}
	} else {
		if err := t.adoptExistingProject(ctx, target); err != nil {
			return err
		}
	}
	if err := t.ensureWorkspaceFiles(ctx, target); err != nil {
		return err
	}
	t.configureSourceAnalysis(ctx, target)
	t.testMCP(ctx, target)
	if t.req.Origin == onboardingOriginNew {
		t.publishNewProject(ctx, target)
	}
	if t.req.Orchestration == onboardingOrchestrationGastown {
		t.setupGasTown(ctx)
	}
	t.info("Setup complete. The Onboarder can now coach milestones, epics, and beads with this context fixed.")
	return nil
}

func (t *onboardingSetupTxn) runGasTown(ctx context.Context) error {
	t.info("Starting deterministic Gas Town onboarding setup for %s.", t.req.ProjectName)
	rig, worktree := t.setupGasTown(ctx)
	if worktree == "" {
		t.warn("Gas Town rig %s is available, but its worktree path could not be resolved. Provide gastown_worktree_path or upgrade gt rig list JSON to include worktree_path.", rig)
		t.info("Setup complete. Gas Town can dispatch through Sling, but LLM guidance files were not written.")
		return nil
	}
	t.projectPath = worktree
	if _, err := os.Stat(worktree); err != nil {
		t.warn("Gas Town worktree %s is not reachable yet: %v", worktree, err)
		t.info("Setup complete. Gas Town can dispatch through Sling once the rig worktree exists.")
		return nil
	}
	if err := t.ensureGembaWorkspace(worktree); err != nil {
		return err
	}
	if err := t.ensureWorkspaceFiles(ctx, worktree); err != nil {
		return err
	}
	t.configureSourceAnalysis(ctx, worktree)
	t.testMCP(ctx, worktree)
	t.info("Setup complete. The Onboarder can now coach milestones, epics, and beads with Gas Town as the runtime host.")
	return nil
}

func (t *onboardingSetupTxn) targetPath() string {
	if t.req.Orchestration == onboardingOrchestrationGastown {
		return t.req.GastownLocation
	}
	return t.req.WorktreePath
}

func (t *onboardingSetupTxn) prepareNewProject(ctx context.Context, target string) error {
	t.info("Preparing new local workspace at %s.", target)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); os.IsNotExist(err) {
		if _, err := t.runner(ctx, target, "git", "init", "--initial-branch=main"); err != nil {
			if _, err2 := t.runner(ctx, target, "git", "init"); err2 != nil {
				return fmt.Errorf("git init: %w", err2)
			}
			_, _ = t.runner(ctx, target, "git", "symbolic-ref", "HEAD", "refs/heads/main")
		}
		t.checks["git"] = "initialized"
	} else if err != nil {
		return fmt.Errorf("stat .git: %w", err)
	} else {
		t.checks["git"] = "already-present"
	}
	if err := t.ensureGembaWorkspace(target); err != nil {
		return err
	}
	t.ensureBeads(ctx, target)
	return nil
}

func (t *onboardingSetupTxn) adoptExistingProject(ctx context.Context, target string) error {
	t.info("Adopting existing worktree at %s.", target)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("worktree does not exist or is not a directory: %s", target)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		return fmt.Errorf("worktree is not a git repository: %s", target)
	}
	dirty, err := t.gitDirty(ctx, target)
	if err != nil {
		t.warn("Could not inspect git status: %v", err)
	} else if dirty {
		t.warn("Git worktree is dirty; skipping fetch/pull to avoid disturbing local changes.")
		t.checks["git_sync"] = "skipped-dirty"
	} else {
		t.syncGit(ctx, target)
	}
	if err := t.ensureGembaWorkspace(target); err != nil {
		return err
	}
	t.ensureBeads(ctx, target)
	return nil
}

func (t *onboardingSetupTxn) ensureWorkspaceFiles(ctx context.Context, target string) error {
	if err := ensureRuntimeGuidanceFile(filepath.Join(target, "AGENTS.md"), t.req.ProjectName); err != nil {
		return fmt.Errorf("AGENTS.md: %w", err)
	}
	t.info("Updated AGENTS.md with Beads, Gemba MCP, and source-analysis guidance.")
	if err := ensureRuntimeGuidanceFile(filepath.Join(target, "CLAUDE.md"), t.req.ProjectName); err != nil {
		return fmt.Errorf("CLAUDE.md: %w", err)
	}
	t.info("Updated CLAUDE.md with the same runtime guidance.")
	if err := ensureCodexSettings(target); err != nil {
		return fmt.Errorf(".Codex/settings.local.json: %w", err)
	}
	t.info("Configured Codex settings with the Gemba MCP server.")
	rep, err := nativeinstall.NewClaude().Install(ctx, nativeinstall.Options{
		Dir:           target,
		BridgeCommand: "gemba-bridge",
		McpCommand:    "gemba-mcp",
	})
	if err != nil {
		t.warn("Claude bridge install did not complete: %v", err)
	} else {
		for _, action := range rep.Actions {
			t.info("Claude bridge %s: %s.", action.Kind, filepath.Base(action.Path))
		}
	}
	return nil
}

func (t *onboardingSetupTxn) configureSourceAnalysis(ctx context.Context, target string) {
	if t.req.SourceAnalysisTool == onboardingSourceNone {
		t.info("Source analysis explicitly skipped.")
		t.checks["source_analysis"] = "skipped"
		return
	}
	t.info("Verifying GitNexus for source analysis.")
	if _, err := t.runner(ctx, target, "gitnexus", "--version"); err != nil {
		t.warn("GitNexus was not available on PATH; attempting npm global install.")
		if _, installErr := t.runner(ctx, target, "npm", "install", "-g", "gitnexus"); installErr != nil {
			t.warn("GitNexus install failed: %v", installErr)
			t.checks["source_analysis"] = "unavailable"
			return
		}
	}
	if err := ensureCodeAnalysisConfig(target, t.req.ProjectName, target); err != nil {
		t.warn("Could not write .gemba/codeanalysis.toml: %v", err)
	} else {
		t.info("Wrote .gemba/codeanalysis.toml with GitNexus as the default backend.")
	}
	if t.req.Origin == onboardingOriginNew {
		t.info("GitNexus selected; initial index will run once code exists.")
		t.checks["source_analysis"] = "configured"
		return
	}
	if _, err := t.runner(ctx, target, "gitnexus", "analyze", "--path", target); err != nil {
		t.warn("GitNexus analysis failed: %v", err)
		t.checks["source_analysis"] = "analysis-failed"
		return
	}
	t.info("GitNexus analysis completed.")
	t.checks["source_analysis"] = "current"
}

func (t *onboardingSetupTxn) testMCP(ctx context.Context, target string) {
	if _, err := t.runner(ctx, target, "gemba-mcp", "--version"); err != nil {
		t.warn("Gemba MCP command could not be executed during setup: %v", err)
		t.checks["gemba_mcp"] = "configured-not-verified"
	} else {
		t.info("Gemba MCP command verified.")
		t.checks["gemba_mcp"] = "verified"
	}
	if t.req.SourceAnalysisTool != onboardingSourceGitNexus {
		return
	}
	mcpCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := t.runner(mcpCtx, target, "gitnexus", "mcp", "--help"); err != nil {
		t.warn("GitNexus MCP probe did not complete cleanly: %v", err)
		t.checks["source_analysis_mcp"] = "configured-not-verified"
	} else {
		t.info("GitNexus MCP command verified.")
		t.checks["source_analysis_mcp"] = "verified"
	}
}

func (t *onboardingSetupTxn) publishNewProject(ctx context.Context, target string) {
	remoteURL := githubRemoteURL(t.req.GitHubProject)
	if remoteURL == "" {
		t.warn("GitHub project %q could not be converted to a remote URL; skipping repository publish.", t.req.GitHubProject)
		t.checks["github"] = "invalid"
		return
	}
	t.info("Preparing GitHub repository %s.", t.req.GitHubProject)
	if _, err := t.runner(ctx, target, "gh", "repo", "view", t.req.GitHubProject, "--json", "name"); err != nil {
		if _, createErr := t.runner(ctx, target, "gh", "repo", "create", t.req.GitHubProject, "--private"); createErr != nil {
			t.warn("GitHub repository create failed: %v", createErr)
			t.checks["github"] = "create-failed"
		} else {
			t.info("Created GitHub repository %s.", t.req.GitHubProject)
			t.checks["github"] = "created"
		}
	} else {
		t.info("GitHub repository %s already exists.", t.req.GitHubProject)
		t.checks["github"] = "already-present"
	}
	t.ensureOrigin(ctx, target, remoteURL)
	t.commitSetupSnapshot(ctx, target)
	if _, err := t.runner(ctx, target, "git", "branch", "-M", "main"); err != nil {
		t.warn("Could not set main as the default branch: %v", err)
	}
	if _, err := t.runner(ctx, target, "git", "push", "-u", "origin", "main"); err != nil {
		t.warn("Initial git push failed: %v", err)
		t.checks["git_push"] = "failed"
		return
	}
	t.info("Pushed initial onboarding setup to origin/main.")
	t.checks["git_push"] = "pushed"
}

func (t *onboardingSetupTxn) ensureOrigin(ctx context.Context, target, remoteURL string) {
	out, err := t.runner(ctx, target, "git", "remote", "get-url", "origin")
	if err != nil {
		if _, addErr := t.runner(ctx, target, "git", "remote", "add", "origin", remoteURL); addErr != nil {
			t.warn("Could not add git origin %s: %v", remoteURL, addErr)
			t.checks["git_remote"] = "failed"
			return
		}
		t.info("Added git origin %s.", remoteURL)
		t.checks["git_remote"] = "added"
		return
	}
	if strings.TrimSpace(string(out)) == remoteURL {
		t.info("Git origin already points at %s.", remoteURL)
		t.checks["git_remote"] = "already-present"
		return
	}
	if _, setErr := t.runner(ctx, target, "git", "remote", "set-url", "origin", remoteURL); setErr != nil {
		t.warn("Could not update git origin to %s: %v", remoteURL, setErr)
		t.checks["git_remote"] = "set-url-failed"
		return
	}
	t.info("Updated git origin to %s.", remoteURL)
	t.checks["git_remote"] = "updated"
}

func (t *onboardingSetupTxn) commitSetupSnapshot(ctx context.Context, target string) {
	out, err := t.runner(ctx, target, "git", "status", "--porcelain")
	if err != nil {
		t.warn("Could not inspect setup commit status: %v", err)
		t.checks["git_commit"] = "status-failed"
		return
	}
	if strings.TrimSpace(string(out)) == "" {
		t.info("No onboarding setup changes need committing.")
		t.checks["git_commit"] = "clean"
		return
	}
	if _, err := t.runner(ctx, target, "git", "add", "."); err != nil {
		t.warn("Could not stage onboarding setup files: %v", err)
		t.checks["git_commit"] = "stage-failed"
		return
	}
	if _, err := t.runner(ctx, target, "git", "commit", "-m", "chore: initialize gemba onboarding"); err != nil {
		t.warn("Could not commit onboarding setup files: %v", err)
		t.checks["git_commit"] = "commit-failed"
		return
	}
	t.info("Committed onboarding setup files.")
	t.checks["git_commit"] = "committed"
}

func (t *onboardingSetupTxn) setupGasTown(ctx context.Context) (string, string) {
	remoteURL := githubRemoteURL(t.req.GitHubProject)
	if remoteURL == "" {
		t.warn("Gas Town setup needs a GitHub owner/repo or remote URL; skipping rig initialization.")
		t.checks["gastown"] = "invalid-remote"
		return gasTownRigName(t.req.ProjectName), t.req.GastownWorktree
	}
	if err := os.MkdirAll(t.req.GastownLocation, 0o755); err != nil {
		t.warn("Could not create Gas Town location %s: %v", t.req.GastownLocation, err)
		t.checks["gastown"] = "location-failed"
		return gasTownRigName(t.req.ProjectName), t.req.GastownWorktree
	}
	rig := t.req.GastownRig
	if rig == "" {
		rig = gasTownRigName(t.req.ProjectName)
	}
	t.info("Ensuring Gas Town rig %s is available.", rig)
	rigsOut, err := t.runner(ctx, t.req.GastownLocation, "gt", "rig", "list", "--json")
	if err != nil {
		t.warn("Gas Town CLI did not list rigs cleanly: %v", err)
		t.checks["gastown"] = "gt-unavailable"
		return rig, t.req.GastownWorktree
	}
	existing, _ := findGasTownRig(rigsOut, rig)
	if existing.Name != "" {
		t.info("Reusing existing Gas Town rig %s.", rig)
		t.checks["gastown_rig"] = "reused"
	} else {
		if t.req.Origin == onboardingOriginNew {
			t.ensureGitHubRepository(ctx, t.req.GastownLocation)
		}
		if _, err := t.runner(ctx, t.req.GastownLocation, "gt", "rig", "add", rig, remoteURL); err != nil {
			if _, createErr := t.runner(ctx, t.req.GastownLocation, "gt", "rig", "create", rig); createErr != nil {
				t.warn("Gas Town rig initialization failed: %v", err)
				t.checks["gastown"] = "rig-init-failed"
				return rig, t.req.GastownWorktree
			}
			t.info("Created Gas Town rig %s.", rig)
		} else {
			t.info("Added Gas Town rig %s for %s.", rig, remoteURL)
		}
		rigsOut, _ = t.runner(ctx, t.req.GastownLocation, "gt", "rig", "list", "--json")
		existing, _ = findGasTownRig(rigsOut, rig)
		t.checks["gastown_rig"] = "created"
	}
	if _, err := t.runner(ctx, t.req.GastownLocation, "gt", "crew", "create", rig, "onboarder"); err != nil {
		t.warn("Gas Town crew creation was skipped or failed: %v", err)
		if _, err := t.runner(ctx, t.req.GastownLocation, "gt", "polecat", "create", rig, "onboarder"); err != nil {
			t.warn("Gas Town onboarding polecat creation was skipped or failed: %v", err)
		} else {
			t.info("Created Gas Town onboarding polecat for %s.", rig)
		}
	} else {
		t.info("Created or reused Gas Town onboarding crew for %s.", rig)
	}
	worktree := t.resolveGasTownWorktree(rig, existing)
	if worktree != "" {
		t.info("Resolved Gas Town worktree for %s at %s.", rig, worktree)
		t.checks["gastown_worktree"] = worktree
	}
	t.checks["gastown"] = "initialized"
	return rig, worktree
}

func (t *onboardingSetupTxn) ensureGitHubRepository(ctx context.Context, cwd string) {
	t.info("Preparing GitHub repository %s for Gas Town.", t.req.GitHubProject)
	if _, err := t.runner(ctx, cwd, "gh", "repo", "view", t.req.GitHubProject, "--json", "name"); err != nil {
		if _, createErr := t.runner(ctx, cwd, "gh", "repo", "create", t.req.GitHubProject, "--private"); createErr != nil {
			t.warn("GitHub repository create failed: %v", createErr)
			t.checks["github"] = "create-failed"
			return
		}
		t.info("Created GitHub repository %s.", t.req.GitHubProject)
		t.checks["github"] = "created"
		return
	}
	t.info("GitHub repository %s already exists.", t.req.GitHubProject)
	t.checks["github"] = "already-present"
}

type gasTownRigView struct {
	Name         string `json:"name"`
	Repository   string `json:"repository,omitempty"`
	Repo         string `json:"repo,omitempty"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Path         string `json:"path,omitempty"`
	Dir          string `json:"dir,omitempty"`
}

func findGasTownRig(raw []byte, name string) (gasTownRigView, error) {
	var rigs []gasTownRigView
	if err := json.Unmarshal(raw, &rigs); err != nil {
		return gasTownRigView{}, err
	}
	for _, rig := range rigs {
		if rig.Name == name {
			return rig, nil
		}
	}
	return gasTownRigView{}, nil
}

func (t *onboardingSetupTxn) resolveGasTownWorktree(rig string, view gasTownRigView) string {
	if t.req.GastownWorktree != "" {
		return t.req.GastownWorktree
	}
	for _, candidate := range []string{view.WorktreePath, view.Path, view.Dir} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	for _, candidate := range []string{
		filepath.Join(t.req.GastownLocation, "rigs", rig),
		filepath.Join(t.req.GastownLocation, rig),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func (t *onboardingSetupTxn) effectiveBeadsURL() string {
	if t.req.BeadsURL != "" {
		return t.req.BeadsURL
	}
	return t.cfg.DoltURL
}

func (t *onboardingSetupTxn) gitDirty(ctx context.Context, target string) (bool, error) {
	out, err := t.runner(ctx, target, "git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (t *onboardingSetupTxn) syncGit(ctx context.Context, target string) {
	if _, err := t.runner(ctx, target, "git", "fetch", "--prune"); err != nil {
		t.warn("git fetch failed: %v", err)
		t.checks["git_sync"] = "fetch-failed"
		return
	}
	if _, err := t.runner(ctx, target, "git", "pull", "--ff-only"); err != nil {
		t.warn("git pull --ff-only failed: %v", err)
		t.checks["git_sync"] = "pull-failed"
		return
	}
	t.info("Git worktree synced with remote.")
	t.checks["git_sync"] = "current"
}

func (t *onboardingSetupTxn) ensureGembaWorkspace(target string) error {
	path := filepath.Join(target, ".gemba", "workspace.toml")
	if _, err := os.Stat(path); err == nil {
		t.info(".gemba/workspace.toml already exists.")
		if err := t.ensureWorkspaceBeadsURL(path); err != nil {
			return err
		}
		t.checks["workspace"] = "already-present"
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat workspace.toml: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .gemba: %w", err)
	}
	state := NewProjectState{
		ProjectName:  t.req.ProjectName,
		Description:  "Prepared by deterministic onboarding setup.",
		Architecture: fmt.Sprintf("GitHub project: %s; orchestration: %s", t.req.GitHubProject, t.req.Orchestration),
	}
	if err := os.WriteFile(path, []byte(buildWorkspaceTOML(state, t.now().UTC())), 0o644); err != nil {
		return fmt.Errorf("write workspace.toml: %w", err)
	}
	if err := t.ensureWorkspaceBeadsURL(path); err != nil {
		return err
	}
	t.info("Created .gemba/workspace.toml.")
	t.checks["workspace"] = "created"
	return nil
}

func (t *onboardingSetupTxn) ensureWorkspaceBeadsURL(path string) error {
	beadsURL := t.effectiveBeadsURL()
	if beadsURL == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.Contains(string(raw), "beads_db") {
		t.checks["beads_binding"] = "already-present"
		return nil
	}
	appendix := fmt.Sprintf("\n# Beads database used by this workspace.\nbeads_db = %q\n", beadsURL)
	if err := os.WriteFile(path, append(raw, []byte(appendix)...), 0o644); err != nil {
		return err
	}
	t.info("Recorded Beads Dolt URL in .gemba/workspace.toml.")
	t.checks["beads_binding"] = "recorded"
	return nil
}

func (t *onboardingSetupTxn) ensureBeads(ctx context.Context, target string) {
	if _, err := os.Stat(filepath.Join(target, ".beads")); err == nil {
		t.info("Beads database already exists.")
		t.checks["beads"] = "already-present"
		return
	}
	prefix := bdPrefixFor(t.req.ProjectName)
	if _, err := t.runner(ctx, target,
		"bd", "init",
		"--non-interactive",
		"--role", "maintainer",
		"--prefix", prefix,
		"--skip-hooks",
		"--skip-agents",
		"--quiet",
	); err != nil {
		t.warn("Beads initialization failed: %v", err)
		t.checks["beads"] = "init-failed"
		return
	}
	t.info("Initialized local Beads database.")
	t.checks["beads"] = "created"
}

func (t *onboardingSetupTxn) info(format string, args ...any) {
	t.addFrame("info", fmt.Sprintf(format, args...), true)
}

func (t *onboardingSetupTxn) warn(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	t.warnings = append(t.warnings, line)
	t.addFrame("warn", line, true)
}

func (t *onboardingSetupTxn) addFrame(level, line string, done bool) {
	t.frames = append(t.frames, onboardingSetupFrame{
		Seq:   len(t.frames) + 1,
		Line:  line,
		Level: level,
		Done:  done,
	})
}

func ensureRuntimeGuidanceFile(path, projectName string) error {
	body := fmt.Sprintf(`# %s

%s
`, projectName, agentSetupGuidance)
	return mergeMarkdownBlock(path, runtimeContextStart, runtimeContextEnd, body)
}

func mergeMarkdownBlock(path, start, end, body string) error {
	block := start + "\n" + strings.TrimSpace(body) + "\n" + end + "\n"
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		text := string(existing)
		if strings.Contains(text, start) && strings.Contains(text, end) {
			before, afterStart, _ := strings.Cut(text, start)
			_, after, _ := strings.Cut(afterStart, end)
			return os.WriteFile(path, []byte(before+block+after), 0o644)
		}
		if strings.TrimSpace(text) != "" {
			block = block + "\n" + text
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(block), 0o644)
}

func ensureCodexSettings(target string) error {
	path := filepath.Join(target, ".Codex", "settings.local.json")
	existing := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &existing); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	servers, _ := existing["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["gemba"] = map[string]any{"command": "gemba-mcp"}
	existing["mcp_servers"] = servers
	existing["_gemba_bridge"] = map[string]any{
		"profile": "codex",
		"version": "1",
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

func ensureCodeAnalysisConfig(target, repoName, repoPath string) error {
	path := filepath.Join(target, ".gemba", "codeanalysis.toml")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`# Source-analysis configuration generated by onboarding setup.
default_backend = "gitnexus"

[[repo]]
name = %q
path = %q
backend = "gitnexus"
`, repoName, repoPath)
	return os.WriteFile(path, []byte(body), 0o644)
}

func githubRemoteURL(project string) string {
	trimmed := strings.TrimSpace(project)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "https://") ||
		strings.HasPrefix(trimmed, "git@") ||
		strings.HasPrefix(trimmed, "ssh://") {
		return trimmed
	}
	if strings.Count(trimmed, "/") != 1 {
		return ""
	}
	return "https://github.com/" + trimmed + ".git"
}

func gasTownRigName(projectName string) string {
	name := bdPrefixFor(projectName)
	if name == "" {
		return "gemba"
	}
	return name
}
