package gt

import (
	"context"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// A Gas Town rig is a git clone + witness/refinery tmux agents + a
// polecats/ directory. The polecat worktree the worker actually runs
// in lives *inside* the rig, so the rig is the nearest thing Gas Town
// exposes to Gemba's Workspace (DD-5). This file maps the two.
//
// Identity: the rig name is stable across restarts and is the natural
// id to use as Workspace.ID. AcquireWorkspace derives the name from
// req.Repository so a second call for the same repo reuses the rig
// rather than creating a duplicate (DoD: idempotent acquire).

// AcquireWorkspace provisions a rig matching req. It is idempotent:
// if a rig with the derived name already exists, it is reused rather
// than recreated. The caller-supplied required_isolation is checked
// against what a git worktree can actually guarantee — asking for
// network/CPU/memory/snapshot isolation errors with KindUnsupported,
// per DD-5 ("failing to meet the ask must error rather than silently
// downgrade").
func (o *OrchestrationPlane) AcquireWorkspace(ctx context.Context, req core.WorkspaceRequest) (core.Workspace, error) {
	if err := checkWorktreeIsolation(req.RequiredIsolation); err != nil {
		return core.Workspace{}, err
	}
	if req.PreferredKind != "" && req.PreferredKind != core.WorkspaceWorktree {
		return core.Workspace{}, core.NewAdaptorError(core.KindUnsupported,
			"gastown: preferred_kind=%q not supported (worktree only)", req.PreferredKind)
	}
	if strings.TrimSpace(req.Repository) == "" {
		return core.Workspace{}, core.NewAdaptorError(core.KindValidation,
			"gastown: WorkspaceRequest.Repository is required to acquire a rig")
	}

	name := deriveRigName(req.Repository)
	if name == "" {
		return core.Workspace{}, core.NewAdaptorError(core.KindValidation,
			"gastown: cannot derive rig name from repository %q", req.Repository)
	}

	rigs, err := o.loadRigs(ctx)
	if err != nil {
		return core.Workspace{}, err
	}
	for _, r := range rigs {
		if r.Name == name {
			return rigToWorkspace(r, req), nil
		}
	}

	args := []string{"rig", "add", name, req.Repository}
	if req.Branch != "" {
		args = append(args, "--branch", req.Branch)
	}
	if _, err := o.run(ctx, args...); err != nil {
		return core.Workspace{}, wrapGtError(err, "gt "+strings.Join(args, " "))
	}

	return core.Workspace{
		ID:         name,
		Kind:       core.WorkspaceWorktree,
		Repository: req.Repository,
		Branch:     req.Branch,
		Status:     core.WorkspaceReady,
		Isolation:  worktreeIsolation,
		CreatedAt:  time.Now(),
	}, nil
}

// ReleaseWorkspace stops the rig (witness + refinery + polecats) and
// is idempotent: a workspace id the adaptor no longer knows about is
// treated as already-released, matching the interface contract.
func (o *OrchestrationPlane) ReleaseWorkspace(ctx context.Context, workspaceID string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return core.NewAdaptorError(core.KindValidation,
			"gastown: ReleaseWorkspace requires a workspace id")
	}
	rigs, err := o.loadRigs(ctx)
	if err != nil {
		return err
	}
	for _, r := range rigs {
		if r.Name == workspaceID {
			if _, err := o.run(ctx, "rig", "stop", workspaceID); err != nil {
				return wrapGtError(err, "gt rig stop "+workspaceID)
			}
			return nil
		}
	}
	return nil
}

// InspectWorkspace reports the current state of a rig by name. Unknown
// ids return a tagged KindSessionNotFound so callers can differentiate
// "ask me again later" from "this was never real".
func (o *OrchestrationPlane) InspectWorkspace(ctx context.Context, workspaceID string) (core.Workspace, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return core.Workspace{}, core.NewAdaptorError(core.KindValidation,
			"gastown: InspectWorkspace requires a workspace id")
	}
	rigs, err := o.loadRigs(ctx)
	if err != nil {
		return core.Workspace{}, err
	}
	for _, r := range rigs {
		if r.Name == workspaceID {
			return rigToWorkspace(r, core.WorkspaceRequest{}), nil
		}
	}
	return core.Workspace{}, core.NewAdaptorError(core.KindSessionNotFound,
		"gastown: workspace %q unknown", workspaceID)
}

// worktreeIsolation is the isolation profile Gas Town rigs actually
// enforce. Writes are scoped to the rig directory (fs_scoped); nothing
// else is claimed, so the negotiation layer can see the exact shape.
var worktreeIsolation = core.IsolationCapabilities{FSScoped: true}

func checkWorktreeIsolation(req core.IsolationCapabilities) error {
	// A worktree on the host shares the kernel, network, and CPU with
	// every other process. If the caller needs any stronger guarantee,
	// error rather than silently accept (DD-5).
	if req.NetIsolated || req.CPULimited || req.MemLimited || req.SnapshotRestore {
		return core.NewAdaptorError(core.KindUnsupported,
			"gastown: worktree workspace cannot satisfy required isolation (net/cpu/mem/snapshot)")
	}
	return nil
}

func rigToWorkspace(r gtRig, req core.WorkspaceRequest) core.Workspace {
	return core.Workspace{
		ID:         r.Name,
		Kind:       core.WorkspaceWorktree,
		Repository: req.Repository,
		Branch:     req.Branch,
		Status:     rigStatusToWorkspaceStatus(r.Status),
		Isolation:  worktreeIsolation,
		CreatedAt:  time.Now(),
	}
}

// rigStatusToWorkspaceStatus maps the status string `gt rig list` emits
// onto the canonical WorkspaceStatus. Anything unrecognised is reported
// as "ready" — the UI treats unknown-but-listed as live so stale rigs
// don't vanish on a schema bump.
func rigStatusToWorkspaceStatus(s string) core.WorkspaceStatus {
	switch s {
	case "operational":
		return core.WorkspaceReady
	case "stopped", "docked", "parked":
		return core.WorkspaceReleased
	default:
		return core.WorkspaceReady
	}
}

// deriveRigName converts a repository URL into a stable rig name that
// `gt rig add` will accept. It takes the last path segment, strips
// `.git`, lowercases, and substitutes non-alphanumerics with `_` — the
// convention Gas Town uses internally for rig directory names.
func deriveRigName(repo string) string {
	base := strings.TrimSpace(repo)
	if i := strings.LastIndexAny(base, "/:"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".git")
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
