// Package bd wraps the `bd` CLI. Beads is Gas City's Layer-0 task store;
// every pack uses it (unless configured for the file provider). Bullet
// City queries it for the work grid, Kanban, and graph views.
//
// Operations: list, show, ready, create, update, close, dep, label,
// comment, stats, mail, molecule, dolt push/pull.
//
// Every call shells out to the `bd` binary with --json output. Never read
// Dolt or JSONL directly; those are internal to Beads and subject to
// change. Beads v0.60+ uses Dolt; pre-v0.50 used SQLite+JSONL; the CLI
// is the stable contract across both.
//
// Multi-rig note: each rig has its own .beads/ directory, so this package
// must thread a rig-identifying --db flag or working directory through
// every call. See gm-e2.2.
package bd
