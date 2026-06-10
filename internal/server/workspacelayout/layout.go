// Package workspacelayout owns the on-disk layout for a single Gemba
// workspace's per-workspace data tree (gm-o9t8.1.2.4).
//
// Every workspace lives under:
//
//	<root>/workspaces/<id>/
//	    repo/         — operator's working tree (clone or git-init target)
//	    dolt/         — per-workspace Dolt data (RESERVED; see deviation below)
//	    logs/         — server-side runtime logs (ephemeral)
//	    transcripts/  — agent dispatch transcripts (NDJSON event logs)
//
// Root convention: <root> is the gemba-server's data dir, typically the
// directory that also holds the shared "dolt/" subtree (e.g. ./data, or
// the path passed via --data-dir). Callers pass the absolute root.
//
// Permission model:
//   - Directories: 0750 (owner read+write+exec, group read+exec, no
//     other access). The owner is the user the gemba-server process
//     runs as.
//   - Files inside: 0640 by convention (set by the writer, not by this
//     package).
//   - Parents (<root>, <root>/workspaces) are also created with 0750
//     on first use so the umask cannot accidentally widen them.
//
// Layout deviation for M1 (gm-o9t8.1.7): the embedded Dolt supervisor
// runs a single shared data dir per gemba-server today (typically
// ./data/dolt), not per-workspace. We still create <id>/dolt/ as a
// reserved placeholder so the layout is forward-compatible; M3 is
// likely to move embedded Dolt under each workspace. See
// docs/server/workspace-layout.md for the migration notes.
package workspacelayout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirPerm is the canonical permission bit for every directory this
// package creates. Exported so tests and callers can assert against it
// without hard-coding the literal.
const DirPerm os.FileMode = 0o750

// FilePerm is the canonical permission bit for files written *inside*
// the workspace layout. This package does not write files itself; the
// constant is exported so writers (the transcripts NDJSON writer, the
// logs sink, etc.) can share a single source of truth.
const FilePerm os.FileMode = 0o640

// Paths bundles the four canonical subdirectories of one workspace.
// All fields are absolute paths.
type Paths struct {
	// Workspace is the workspace root, <root>/workspaces/<id>.
	Workspace string
	// Repo is the operator's working tree (clone / git-init target).
	Repo string
	// Dolt is the per-workspace Dolt data dir. RESERVED for M3; M1
	// uses a shared server-level Dolt dir (see package doc).
	Dolt string
	// Logs is the server-side runtime log dir (ephemeral).
	Logs string
	// Transcripts is the agent dispatch transcripts dir.
	Transcripts string
}

// Resolve returns the canonical Paths for one workspace under root.
// Pure — does not touch disk. Use EnsureLayout to materialize them.
//
// Empty id or root is rejected (returns an error) so callers cannot
// accidentally smear a workspace across the server's data root.
func Resolve(workspaceID, root string) (Paths, error) {
	id := strings.TrimSpace(workspaceID)
	if id == "" {
		return Paths{}, fmt.Errorf("workspacelayout: workspaceID is required")
	}
	if !validWorkspaceID(id) {
		return Paths{}, fmt.Errorf("workspacelayout: workspaceID %q contains invalid characters (allowed: letters, digits, '-', '_', '.')", id)
	}
	r := strings.TrimSpace(root)
	if r == "" {
		return Paths{}, fmt.Errorf("workspacelayout: root is required")
	}
	if !filepath.IsAbs(r) {
		return Paths{}, fmt.Errorf("workspacelayout: root %q must be absolute", r)
	}
	ws := filepath.Join(r, "workspaces", id)
	return Paths{
		Workspace:   ws,
		Repo:        filepath.Join(ws, "repo"),
		Dolt:        filepath.Join(ws, "dolt"),
		Logs:        filepath.Join(ws, "logs"),
		Transcripts: filepath.Join(ws, "transcripts"),
	}, nil
}

// MustResolve is a convenience wrapper for callers that have already
// validated their inputs (e.g. ratify, which sanity-checks the project
// name before reaching the layout). Panics on error.
func MustResolve(workspaceID, root string) Paths {
	p, err := Resolve(workspaceID, root)
	if err != nil {
		panic(err)
	}
	return p
}

// EnsureLayout creates every directory in the workspace's layout with
// the canonical permissions. Idempotent — a second call against an
// already-materialized tree is a no-op and returns nil. Existing
// directories are reconciled to 0750 via os.Chmod so a previous lax
// umask cannot leave them world-readable.
//
// If any step fails, EnsureLayout returns the underlying error without
// rolling back partial work. Callers that need transactional semantics
// (e.g. /api/v1/newproject/:id/ratify) should layer their own cleanup
// on top.
func EnsureLayout(workspaceID, root string) (Paths, error) {
	p, err := Resolve(workspaceID, root)
	if err != nil {
		return Paths{}, err
	}

	// Create the chain root → root/workspaces → root/workspaces/<id>
	// with 0750 at each level. MkdirAll respects the umask, so we
	// follow up with Chmod to lock the bits down regardless of the
	// process umask.
	chain := []string{
		root,
		filepath.Join(root, "workspaces"),
		p.Workspace,
		p.Repo,
		p.Dolt,
		p.Logs,
		p.Transcripts,
	}
	for _, dir := range chain {
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			return Paths{}, fmt.Errorf("workspacelayout: mkdir %q: %w", dir, err)
		}
		if err := os.Chmod(dir, DirPerm); err != nil {
			return Paths{}, fmt.Errorf("workspacelayout: chmod %q: %w", dir, err)
		}
	}
	return p, nil
}

// validWorkspaceID screens out path-injection vectors. The id ends up
// as a single directory name under <root>/workspaces/, so anything
// that introduces a separator, parent reference, or whitespace is
// rejected.
func validWorkspaceID(id string) bool {
	if id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
