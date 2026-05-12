// Package restore implements gemba-server's "reconstruct from remotes"
// flow: given a git remote (canonical source) and an optional Dolt
// remote (canonical project state), reproduce a workable workspace
// on a fresh machine.
//
// This is the keystone trust promise behind gemba-remote: deleting a
// gemba account, switching servers, or starting over on a new install
// must never lose user data because both halves of the workspace
// always exist outside the gemba server.
//
// Tracks gemba-remote bead gm-o9t8.2.3 (M2 — Reconstruct-from-remotes
// proof).
package restore
