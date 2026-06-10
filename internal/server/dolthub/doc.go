// Package dolthub wraps server-side Dolt remote push/pull for a
// workspace's project state. Each workspace has its own Dolt database;
// gemba pushes that database to a user-configured DoltHub repo so
// project state (beads, plans, design docs) is durable outside the
// gemba server and the user can reconstruct it on a fresh install.
//
// Credentials (DoltHub auth token, optional remote URL override) are
// stored in the per-workspace secrets vault; this package reads them at
// push/pull time and never persists them outside the vault.
//
// All operations shell out to the `dolt` binary. The binary's
// configuration (~/.dolt/config) is respected for SSH keys and other
// per-host details; gemba does not maintain its own dolt config.
//
// Tracks gemba-remote bead gm-o9t8.2.1 (M2 — DoltHub remote push/pull).
package dolthub
