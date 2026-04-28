// Atomic ratification transaction (gm-root.17.6).
//
// Implements the strict-order, all-or-nothing transaction described
// in docs/design/newproject.md §"Atomic ratification". The 10 steps:
//
//   1. Resolve target dir from ~/.gemba/config.toml [projects].default_dir
//      (falls back to ~/gemba/projects/) joined with <project-name>.
//   2. Create the dir. Fail if it already exists — never touch a
//      pre-existing dir.
//   3. git init in the new dir on `main`.
//   4. Write .gemba/workspace.toml with project metadata.
//   5. Initialize the beads database (bd init --non-interactive).
//   6. For each milestone (in order): bd create -t epic -l type:milestone
//      with carried-through metadata.
//   7. For each epic under each milestone: bd create -t epic with
//      parent=milestone-id.
//   8. For each bead under each epic: bd create with parent=epic-id.
//   8a. Resolve intra-tree dep refs to real bd-ids and apply with
//       bd dep add. Cycle detection runs after every edge added; a
//       detected cycle aborts the transaction (rollback).
//   9. Write docs/project.md (the synthesized narrative).
//  10. Stage all files; create initial commit on main
//      ("feat: initial project bootstrap").
//
// Failure rollback: any step that fails after step 2 succeeded
// triggers a deferred cleanup that rm -rf's the new dir. Step 2's
// failure mode (dir already exists) explicitly does NOT rollback —
// we never touch a pre-existing dir.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// NewProjectRatifier executes the atomic ratification transaction.
// Returns the SPA's success envelope on commit, or a structured
// RatifyError on any step failure.
type NewProjectRatifier interface {
	Ratify(ctx context.Context, state NewProjectState) (RatifyResponse, error)
}

// RatifyError is the structured error every step of the transaction
// returns on failure. The HTTP layer maps Step + Code into the JSON
// envelope so the SPA can render a precise diagnostic.
type RatifyError struct {
	// Step is the 1-based transaction step that failed (1..10, or
	// "8a" for the dep-resolution sub-step which we encode as 80).
	Step int
	// Code is a short machine-readable token (validation_failed,
	// dir_exists, git_init_failed, beads_init_failed,
	// milestone_create_failed, epic_create_failed, bead_create_failed,
	// dep_resolve_failed, cycle_detected, project_md_failed,
	// commit_failed, …).
	Code string
	// Message is the human-readable diagnostic surfaced in the toast.
	Message string
	// Cause is the underlying error, retained for logging. May be nil
	// when the step itself synthesised the error.
	Cause error
}

func (e *RatifyError) Error() string {
	if e == nil {
		return "<nil ratify error>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("ratify step %d (%s): %s: %v", e.Step, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("ratify step %d (%s): %s", e.Step, e.Code, e.Message)
}

func (e *RatifyError) Unwrap() error { return e.Cause }

// writeRatifyError maps a RatifyError onto the shared envelope. All
// step failures land as 4xx-or-500 with a JSON body that includes
// the failing step + code so the SPA's toast can read both without
// parsing strings.
func writeRatifyError(w http.ResponseWriter, err error) {
	if re, ok := err.(*RatifyError); ok {
		// Step 2 (dir already exists) is the one operator-correctable
		// failure — surface as 409 so the SPA distinguishes "pick a
		// different name" from "infrastructure broke."
		status := http.StatusInternalServerError
		switch re.Code {
		case "validation_failed":
			status = http.StatusBadRequest
		case "dir_exists":
			status = http.StatusConflict
		case "cycle_detected":
			status = http.StatusUnprocessableEntity
		}
		body := map[string]any{
			"error":   re.Code,
			"message": re.Message,
			"step":    re.Step,
		}
		writeJSON(w, status, body)
		return
	}
	httperr.WriteError(w, err)
}

// ─── Configuration ──────────────────────────────────────────────────

// gembaConfig models the subset of ~/.gemba/config.toml the ratifier
// reads. Only [projects].default_dir matters today; the file is
// otherwise tolerated as opaque.
type gembaConfig struct {
	Projects struct {
		DefaultDir string `toml:"default_dir"`
	} `toml:"projects"`
}

// resolveDefaultDir returns the operator's configured projects
// directory, expanding ~ and falling back to the built-in default
// when the config file is absent or doesn't override the field.
//
// Order:
//   1. ~/.gemba/config.toml [projects].default_dir if set.
//   2. ~/gemba/projects/ (built-in default).
func resolveDefaultDir(home string) (string, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	cfgPath := filepath.Join(home, ".gemba", "config.toml")
	data, err := os.ReadFile(cfgPath)
	if err == nil {
		// Tolerate parse failures — a malformed config falls back to
		// the built-in default rather than aborting ratification.
		if dir := parseDefaultDirTOML(data); dir != "" {
			return expandHome(dir, home), nil
		}
	}
	return filepath.Join(home, "gemba", "projects"), nil
}

// parseDefaultDirTOML extracts the [projects].default_dir value
// without pulling in a TOML library. Looks for the simple
// `default_dir = "…"` line under a [projects] section header.
// Returns "" when not found. Good enough for v1 — when the rest of
// gemba parses config.toml properly, this becomes a thin call into
// that package.
func parseDefaultDirTOML(data []byte) string {
	lines := strings.Split(string(data), "\n")
	inProjects := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inProjects = strings.EqualFold(strings.Trim(line, "[]"), "projects")
			continue
		}
		if !inProjects {
			continue
		}
		if !strings.HasPrefix(line, "default_dir") {
			continue
		}
		// default_dir = "…" — split on first '=' and unquote.
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		val = strings.TrimSuffix(val, "#")
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
			val = val[1 : len(val)-1]
		}
		return val
	}
	return ""
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	if p == "~" {
		return home
	}
	return p
}

// ─── Ratifier implementation ────────────────────────────────────────

// CommandRunner is the shell-out abstraction the ratifier uses. The
// production implementation forwards to exec.CommandContext; tests
// substitute a recording stub so they can drive failures
// deterministically without touching the host's bd / git binaries.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// FilesystemBackend is the file-write abstraction. Production wires
// it to os.MkdirAll / os.WriteFile / os.RemoveAll; tests substitute a
// stub that maps writes onto an in-memory tree (or a tmpfs). Splitting
// it out lets us test step-by-step rollback without spawning
// subprocesses.
type FilesystemBackend interface {
	Stat(path string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	RemoveAll(path string) error
}

type osFS struct{}

func (osFS) Stat(p string) (os.FileInfo, error)                     { return os.Stat(p) }
func (osFS) MkdirAll(p string, m os.FileMode) error                 { return os.MkdirAll(p, m) }
func (osFS) WriteFile(p string, data []byte, m os.FileMode) error  { return os.WriteFile(p, data, m) }
func (osFS) RemoveAll(p string) error                               { return os.RemoveAll(p) }

// RatifierConfig configures the production ratifier. Zero values are
// fine — DefaultDir empty falls back to ~/.gemba/config.toml resolution,
// Runner empty falls back to exec, FS empty falls back to osFS.
type RatifierConfig struct {
	// HomeDir overrides $HOME for default-dir resolution. Tests set
	// this so the transaction lands inside a t.TempDir() instead of
	// the developer's real home.
	HomeDir string

	// DefaultDirOverride bypasses the config-file lookup entirely.
	// Tests use this to force the projects directory without faking
	// out the home-dir filesystem.
	DefaultDirOverride string

	// Runner shells external commands (bd, git). nil → exec-based
	// default.
	Runner CommandRunner

	// FS abstracts filesystem writes. nil → osFS.
	FS FilesystemBackend

	// Now returns the current time; tests pin this for deterministic
	// timestamps in workspace.toml.
	Now func() time.Time
}

// ratifier is the production NewProjectRatifier.
type ratifier struct {
	homeDir            string
	defaultDirOverride string
	runner             CommandRunner
	fs                 FilesystemBackend
	now                func() time.Time
}

// NewRatifier builds a production NewProjectRatifier. cmd/gemba serve
// calls this once at boot and hands the result to AttachNewProject.
func NewRatifier(cfg RatifierConfig) NewProjectRatifier {
	r := &ratifier{
		homeDir:            cfg.HomeDir,
		defaultDirOverride: cfg.DefaultDirOverride,
		runner:             cfg.Runner,
		fs:                 cfg.FS,
		now:                cfg.Now,
	}
	if r.runner == nil {
		r.runner = execRunner
	}
	if r.fs == nil {
		r.fs = osFS{}
	}
	if r.now == nil {
		r.now = time.Now
	}
	return r
}

// execRunner is the production CommandRunner — shells out to a binary
// on PATH with the given working directory.
func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// Force bd into non-interactive mode so a TTY-dependent prompt
	// doesn't hang the transaction.
	cmd.Env = append(os.Environ(),
		"BD_NON_INTERACTIVE=1",
		"CI=true",
	)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		// Surface stderr in the wrapped error so diagnostics include
		// the bd/git output the operator needs to debug.
		stderr := strings.TrimSpace(errBuf.String())
		if stderr != "" {
			return out.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr)
		}
		return out.Bytes(), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out.Bytes(), nil
}

// Ratify drives the 10-step transaction. Returns the success envelope
// on commit; returns a *RatifyError describing the failing step on
// any failure (the dir is rolled back if step 2 succeeded).
func (r *ratifier) Ratify(ctx context.Context, state NewProjectState) (RatifyResponse, error) {
	// ── Step 0: validate input ─────────────────────────────────────
	name := strings.TrimSpace(state.ProjectName)
	if name == "" {
		return RatifyResponse{}, &RatifyError{
			Step: 1, Code: "validation_failed",
			Message: "ProjectName is required",
		}
	}
	if !validProjectName(name) {
		return RatifyResponse{}, &RatifyError{
			Step: 1, Code: "validation_failed",
			Message: "ProjectName must contain only letters, digits, '-', '_' (got: " + name + ")",
		}
	}

	// ── Step 1: resolve target dir ─────────────────────────────────
	defaultDir := r.defaultDirOverride
	if defaultDir == "" {
		var err error
		defaultDir, err = resolveDefaultDir(r.homeDir)
		if err != nil {
			return RatifyResponse{}, &RatifyError{
				Step: 1, Code: "validation_failed",
				Message: "could not resolve default projects dir",
				Cause:   err,
			}
		}
	}
	target := filepath.Join(defaultDir, name)

	// ── Step 2: create the dir (fail-closed if it exists) ──────────
	// Stat first — we MUST NOT delete a pre-existing dir on rollback,
	// so checking before MkdirAll is the only correct ordering. (A
	// race between the Stat and MkdirAll is theoretically possible
	// but the worst case is a bd-backed dir we created races a sibling
	// gemba serve, which would also have been creating it — both fail
	// at step 5 with bd-init complaining.)
	if _, err := r.fs.Stat(target); err == nil {
		return RatifyResponse{}, &RatifyError{
			Step: 2, Code: "dir_exists",
			Message: "target directory already exists: " + target,
		}
	} else if !os.IsNotExist(err) {
		return RatifyResponse{}, &RatifyError{
			Step: 2, Code: "stat_failed",
			Message: "could not stat target directory: " + target,
			Cause:   err,
		}
	}
	// Also create the parent path so the projects-root is auto-created
	// on first project (per design: "created on first project if missing").
	if err := r.fs.MkdirAll(defaultDir, 0o755); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 2, Code: "mkdir_failed",
			Message: "could not create projects parent dir",
			Cause:   err,
		}
	}
	if err := r.fs.MkdirAll(target, 0o755); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 2, Code: "mkdir_failed",
			Message: "could not create project dir",
			Cause:   err,
		}
	}
	// Cleanup is armed ONLY now — step 2 succeeded. Any failure from
	// here on rolls back the dir.
	committed := false
	defer func() {
		if !committed {
			_ = r.fs.RemoveAll(target)
		}
	}()

	// ── Step 3: git init on main ───────────────────────────────────
	if _, err := r.runner(ctx, target, "git", "init", "--initial-branch=main"); err != nil {
		// Older git versions (< 2.28) don't know --initial-branch.
		// Retry the bare init then rename the default branch.
		if _, err2 := r.runner(ctx, target, "git", "init"); err2 != nil {
			return RatifyResponse{}, &RatifyError{
				Step: 3, Code: "git_init_failed",
				Message: "git init failed",
				Cause:   err2,
			}
		}
		// Best-effort rename — silently ignored if the branch already
		// is main (fresh repos with no commits have a "default" symbolic
		// ref that just points at refs/heads/main).
		_, _ = r.runner(ctx, target, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	}

	// ── Step 4: write .gemba/workspace.toml ────────────────────────
	gembaDir := filepath.Join(target, ".gemba")
	if err := r.fs.MkdirAll(gembaDir, 0o755); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 4, Code: "workspace_toml_failed",
			Message: "could not create .gemba directory",
			Cause:   err,
		}
	}
	wsTOML := buildWorkspaceTOML(state, r.now().UTC())
	if err := r.fs.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(wsTOML), 0o644); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 4, Code: "workspace_toml_failed",
			Message: "could not write .gemba/workspace.toml",
			Cause:   err,
		}
	}

	// ── Step 5: bd init ────────────────────────────────────────────
	prefix := bdPrefixFor(name)
	if _, err := r.runner(ctx, target,
		"bd", "init",
		"--non-interactive",
		"--role", "maintainer",
		"--prefix", prefix,
		"--skip-hooks",
		"--skip-agents",
		"--quiet",
	); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 5, Code: "beads_init_failed",
			Message: "bd init failed",
			Cause:   err,
		}
	}

	// ── Steps 6-8: walk milestones → epics → beads, capturing ids ──
	// Tracking the resolved bd ids by intra-tree ref so step 8a can
	// translate "milestone:0/epic:1/bead:2" → bd-….
	idsByRef := map[string]string{}

	for mi, m := range state.Milestones {
		// Step 6: milestone (bd epic + type:milestone label).
		labels := append([]string{}, m.Labels...)
		if !containsLabel(labels, "type:milestone") {
			labels = append(labels, "type:milestone")
		}
		mID, err := r.bdCreate(ctx, target, bdCreateArgs{
			Title:       m.Title,
			Type:        "epic",
			Description: m.Description,
			Acceptance:  m.Acceptance,
			Labels:      labels,
			Priority:    m.Priority,
			Estimate:    m.Estimate,
			Skills:      m.Skills,
			Design:      m.DesignNotes,
			Notes:       m.Notes,
		})
		if err != nil {
			return RatifyResponse{}, &RatifyError{
				Step: 6, Code: "milestone_create_failed",
				Message: fmt.Sprintf("creating milestone %d (%q)", mi, m.Title),
				Cause:   err,
			}
		}
		idsByRef[fmt.Sprintf("milestone:%d", mi)] = mID

		for ei, e := range m.Epics {
			// Step 7: epic with parent=milestone.
			eID, err := r.bdCreate(ctx, target, bdCreateArgs{
				Title:       e.Title,
				Type:        "epic",
				Description: e.Description,
				Acceptance:  e.Acceptance,
				Labels:      e.Labels,
				Priority:    e.Priority,
				Estimate:    e.Estimate,
				Skills:      e.Skills,
				Design:      e.DesignNotes,
				Notes:       e.Notes,
				Parent:      mID,
			})
			if err != nil {
				return RatifyResponse{}, &RatifyError{
					Step: 7, Code: "epic_create_failed",
					Message: fmt.Sprintf("creating epic %d (%q) under milestone %d", ei, e.Title, mi),
					Cause:   err,
				}
			}
			idsByRef[fmt.Sprintf("milestone:%d/epic:%d", mi, ei)] = eID

			for bi, b := range e.Beads {
				// Step 8: bead with parent=epic.
				bType := strings.TrimSpace(b.Type)
				if bType == "" {
					bType = "task"
				}
				bID, err := r.bdCreate(ctx, target, bdCreateArgs{
					Title:       b.Title,
					Type:        bType,
					Description: b.Description,
					Acceptance:  b.Acceptance,
					Labels:      b.Labels,
					Priority:    b.Priority,
					Estimate:    b.Estimate,
					Skills:      b.Skills,
					Design:      b.DesignNotes,
					Notes:       b.Notes,
					Parent:      eID,
				})
				if err != nil {
					return RatifyResponse{}, &RatifyError{
						Step: 8, Code: "bead_create_failed",
						Message: fmt.Sprintf("creating bead %d (%q) under epic %d/%d", bi, b.Title, mi, ei),
						Cause:   err,
					}
				}
				idsByRef[fmt.Sprintf("milestone:%d/epic:%d/bead:%d", mi, ei, bi)] = bID
			}
		}
	}

	// ── Step 8a: resolve intra-tree dep refs and apply edges ───────
	if err := r.applyDependencies(ctx, target, state, idsByRef); err != nil {
		// applyDependencies returns RatifyErrors directly so we can
		// pass through the structured failure (cycle_detected vs
		// dep_resolve_failed) without losing the step tag.
		if re, ok := err.(*RatifyError); ok {
			return RatifyResponse{}, re
		}
		return RatifyResponse{}, &RatifyError{
			Step: 8, Code: "dep_resolve_failed",
			Message: "applying dependencies",
			Cause:   err,
		}
	}

	// ── Step 9: write docs/project.md ──────────────────────────────
	docsDir := filepath.Join(target, "docs")
	if err := r.fs.MkdirAll(docsDir, 0o755); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 9, Code: "project_md_failed",
			Message: "could not create docs directory",
			Cause:   err,
		}
	}
	projectMD := state.DraftProjectMD
	if strings.TrimSpace(projectMD) == "" {
		projectMD = synthesiseProjectMD(state)
	}
	if err := r.fs.WriteFile(filepath.Join(docsDir, "project.md"), []byte(projectMD), 0o644); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 9, Code: "project_md_failed",
			Message: "could not write docs/project.md",
			Cause:   err,
		}
	}

	// ── Step 10: stage all files + initial commit ──────────────────
	if _, err := r.runner(ctx, target, "git", "add", "-A"); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 10, Code: "commit_failed",
			Message: "git add failed",
			Cause:   err,
		}
	}
	commitArgs := []string{
		"-c", "user.name=Gemba",
		"-c", "user.email=gemba@local",
		"commit",
		"-m", "feat: initial project bootstrap",
	}
	if _, err := r.runner(ctx, target, "git", commitArgs...); err != nil {
		return RatifyResponse{}, &RatifyError{
			Step: 10, Code: "commit_failed",
			Message: "git commit failed",
			Cause:   err,
		}
	}

	// All steps succeeded — commit the cleanup-deferred guard.
	committed = true
	epicCount := 0
	for _, m := range state.Milestones {
		epicCount += len(m.Epics)
	}
	return RatifyResponse{
		ProjectPath:    target,
		ProjectName:    state.ProjectName,
		MilestoneCount: len(state.Milestones),
		EpicCount:      epicCount,
	}, nil
}

// ─── bd create wrapper ──────────────────────────────────────────────

// bdCreateArgs is a convenience struct so each step's call site is
// readable. Maps onto `bd create` flags 1:1.
type bdCreateArgs struct {
	Title       string
	Type        string
	Description string
	Acceptance  string
	Labels      []string
	Priority    int
	Estimate    int
	Skills      []string
	Design      string
	Notes       string
	Parent      string
}

// bdCreate runs `bd create … --json` and returns the new bd id.
// Tolerates both shapes bd's --json output ships with: a single
// object and a one-element array (matching the existing
// adapter/bd/workplane.go decoder).
func (r *ratifier) bdCreate(ctx context.Context, dir string, a bdCreateArgs) (string, error) {
	args := []string{"create", a.Title, "--json"}
	if a.Type != "" {
		args = append(args, "--type", a.Type)
	}
	args = append(args, "--priority", strconv.Itoa(a.Priority))
	if a.Description != "" {
		args = append(args, "--description", a.Description)
	}
	if a.Acceptance != "" {
		args = append(args, "--acceptance", a.Acceptance)
	}
	if a.Design != "" {
		args = append(args, "--design", a.Design)
	}
	if a.Notes != "" {
		args = append(args, "--notes", a.Notes)
	}
	if a.Estimate > 0 {
		args = append(args, "--estimate", strconv.Itoa(a.Estimate))
	}
	if len(a.Labels) > 0 {
		args = append(args, "--labels", strings.Join(a.Labels, ","))
	}
	if len(a.Skills) > 0 {
		args = append(args, "--skills", strings.Join(a.Skills, ","))
	}
	if a.Parent != "" {
		args = append(args, "--parent", a.Parent)
	}
	out, err := r.runner(ctx, dir, "bd", args...)
	if err != nil {
		return "", err
	}
	return parseBdCreateID(out)
}

// parseBdCreateID extracts the new bead's id from bd create --json
// output. bd has shipped both `{"id":"bd-xyz",...}` and
// `[{"id":"bd-xyz",...}]` shapes; we tolerate either to keep this
// loose-coupled with bd's CLI version.
func parseBdCreateID(out []byte) (string, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("bd create returned empty output")
	}
	if trimmed[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return "", fmt.Errorf("decode bd create array: %w", err)
		}
		if len(arr) == 0 {
			return "", fmt.Errorf("bd create returned empty array")
		}
		if id, ok := arr[0]["id"].(string); ok && id != "" {
			return id, nil
		}
		return "", fmt.Errorf("bd create array entry missing id")
	}
	var rec map[string]any
	if err := json.Unmarshal(trimmed, &rec); err != nil {
		return "", fmt.Errorf("decode bd create object: %w", err)
	}
	if id, ok := rec["id"].(string); ok && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("bd create object missing id")
}

// ─── Dependencies + cycle detection (step 8a) ───────────────────────

// applyDependencies translates intra-tree refs into bd ids and
// applies the edges with bd dep add. Cycle detection is performed
// in-process over the graph we just built (instead of relying on bd
// dep cycles to scan the whole DB) so a cycle aborts BEFORE any bd
// edge writes hit the database — the rollback then only has to
// rm -rf the project dir.
//
// This keeps step 8a atomic with steps 6–8 in spirit: the
// transaction's failure modes are constrained to "delete the dir"
// even when bd's own dep storage would otherwise need a backout.
func (r *ratifier) applyDependencies(ctx context.Context, dir string, state NewProjectState, idsByRef map[string]string) error {
	var edges []depEdge
	// Walk the tree in deterministic order so error messages name a
	// stable failing edge.
	for mi, m := range state.Milestones {
		for ei, e := range m.Epics {
			for bi, b := range e.Beads {
				selfRef := fmt.Sprintf("milestone:%d/epic:%d/bead:%d", mi, ei, bi)
				selfID := idsByRef[selfRef]
				for _, depRef := range b.DependsOnRefs {
					depID, ok := idsByRef[depRef]
					if !ok {
						return &RatifyError{
							Step: 8, Code: "dep_resolve_failed",
							Message: fmt.Sprintf("bead %s DependsOnRefs: unknown intra-tree ref %q", selfRef, depRef),
						}
					}
					edges = append(edges, depEdge{from: selfID, to: depID})
				}
				for _, blockRef := range b.BlocksRefs {
					blockID, ok := idsByRef[blockRef]
					if !ok {
						return &RatifyError{
							Step: 8, Code: "dep_resolve_failed",
							Message: fmt.Sprintf("bead %s BlocksRefs: unknown intra-tree ref %q", selfRef, blockRef),
						}
					}
					// "self blocks blockID" == "blockID depends on self".
					edges = append(edges, depEdge{from: blockID, to: selfID})
				}
			}
		}
	}

	// In-process cycle detection over the (from depends_on to)
	// graph. A cycle here means at least one bead transitively
	// depends on itself; bd would reject the second-edge write
	// later, but we'd rather catch it BEFORE issuing any dep writes
	// so rollback stays clean.
	if cyc := detectCycle(edges); len(cyc) > 0 {
		return &RatifyError{
			Step: 8, Code: "cycle_detected",
			Message: "dependency cycle: " + strings.Join(cyc, " -> "),
		}
	}

	// Apply each edge with `bd dep add <depender> <dependee>`.
	for _, e := range edges {
		if _, err := r.runner(ctx, dir, "bd", "dep", "add", e.from, e.to); err != nil {
			return &RatifyError{
				Step: 8, Code: "dep_resolve_failed",
				Message: fmt.Sprintf("bd dep add %s %s failed", e.from, e.to),
				Cause:   err,
			}
		}
	}
	return nil
}

// detectCycle runs a DFS over the dependency edges (from → to means
// "from depends on to") and returns a representative cycle path on
// the first cycle it sees. Returns nil when the graph is acyclic.
//
// The path returned is in the order discovered (starts at some node
// in the cycle, walks the back-edge target → … → original).
// depEdge is a directional dependency edge: from depends on to.
type depEdge struct {
	from string
	to   string
}

func detectCycle(edges []depEdge) []string {
	adj := map[string][]string{}
	nodes := map[string]struct{}{}
	for _, e := range edges {
		adj[e.from] = append(adj[e.from], e.to)
		nodes[e.from] = struct{}{}
		nodes[e.to] = struct{}{}
	}
	// Sort outgoing edges so the reported cycle is deterministic
	// across runs (helps tests pin the exact diagnostic).
	for k := range adj {
		sort.Strings(adj[k])
	}
	// Sort the iteration order over starting nodes too.
	starts := make([]string, 0, len(nodes))
	for n := range nodes {
		starts = append(starts, n)
	}
	sort.Strings(starts)

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	parent := map[string]string{}
	var cyc []string
	var dfs func(string) bool
	dfs = func(n string) bool {
		color[n] = gray
		for _, next := range adj[n] {
			switch color[next] {
			case white:
				parent[next] = n
				if dfs(next) {
					return true
				}
			case gray:
				// Found a back-edge n → next. Reconstruct cycle:
				// next ← parent ← parent … ← n, then close with next.
				path := []string{next}
				cur := n
				for cur != next && cur != "" {
					path = append(path, cur)
					cur = parent[cur]
				}
				path = append(path, next)
				// Reverse so it reads "next -> … -> n -> next".
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				cyc = path
				return true
			}
		}
		color[n] = black
		return false
	}
	for _, n := range starts {
		if color[n] == white {
			if dfs(n) {
				return cyc
			}
		}
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────

// validProjectName enforces a conservative charset on project names.
// The name becomes a directory + a bd prefix + (eventually) a project
// switcher entry, so we restrict to the intersection of those three
// surfaces' safe charsets: letters, digits, '-', '_'.
func validProjectName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// bdPrefixFor returns the bd issue prefix to use for the new project's
// beads database. Lowercase the project name and strip non-letter/digit
// characters so the prefix is a clean identifier (bd init sanitises
// further, but giving it a clean input avoids surprises in `bd-…` ids).
func bdPrefixFor(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "p"
	}
	return b.String()
}

// containsLabel returns true if the slice already contains the
// given label (case-sensitive — bd labels are case-sensitive).
func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// buildWorkspaceTOML synthesises the .gemba/workspace.toml content.
// Minimal v1 — captures project name + description + tech stack +
// architecture notes + a created_at timestamp. Future revisions will
// add adaptor bindings, persona roster references, etc.
func buildWorkspaceTOML(state NewProjectState, now time.Time) string {
	var b strings.Builder
	b.WriteString("# Gemba workspace metadata — generated by /api/v1/newproject/:id/ratify (gm-root.17.6).\n")
	b.WriteString("# See docs/design/newproject.md for the full bootstrap contract.\n\n")
	b.WriteString("[project]\n")
	fmt.Fprintf(&b, "name = %q\n", state.ProjectName)
	fmt.Fprintf(&b, "description = %q\n", state.Description)
	fmt.Fprintf(&b, "created_at = %q\n", now.Format(time.RFC3339))
	if len(state.TechStack) > 0 {
		b.WriteString("tech_stack = [")
		for i, s := range state.TechStack {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", s)
		}
		b.WriteString("]\n")
	} else {
		b.WriteString("tech_stack = []\n")
	}
	if state.Architecture != "" {
		fmt.Fprintf(&b, "architecture = %q\n", state.Architecture)
	}
	return b.String()
}

// synthesiseProjectMD is a fallback used when the skill doesn't ship
// a DraftProjectMD. We refuse to leave docs/project.md empty — that
// would surprise the Gemba walk surface that consumes it post-ratify
// (gm-3nk).
func synthesiseProjectMD(state NewProjectState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", state.ProjectName)
	if state.Description != "" {
		b.WriteString(state.Description)
		b.WriteString("\n\n")
	}
	if len(state.Milestones) > 0 {
		b.WriteString("## Milestones\n\n")
		for _, m := range state.Milestones {
			fmt.Fprintf(&b, "### %s\n\n", m.Title)
			if m.Description != "" {
				b.WriteString(m.Description)
				b.WriteString("\n\n")
			}
			for _, e := range m.Epics {
				fmt.Fprintf(&b, "- **%s**", e.Title)
				if e.Description != "" {
					fmt.Fprintf(&b, " — %s", e.Description)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
