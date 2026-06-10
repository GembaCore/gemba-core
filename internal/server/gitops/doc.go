// Package gitops wraps server-side git operations against the workspace
// checkout. Agent-authored commits are pushed back to the user's
// canonical git remote with attribution so the remote remains the source
// of truth — gemba never owns the code, only operates on a server copy.
//
// All operations shell out to the `git` binary so we inherit the user's
// installed git, hooks, ssh-agent, and credentials. This matches the
// pattern in internal/server/beads_health.go and internal/adapter/bd.
//
// Tracks gemba-remote bead gm-o9t8.2.2 (M2 — federation & data
// ownership).
package gitops
