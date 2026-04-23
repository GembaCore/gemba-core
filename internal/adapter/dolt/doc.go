// Package dolt implements the core.WorkPlane adaptor by opening a
// direct read-only MySQL connection to a Dolt server that hosts a
// beads database (gm-0fd / milestone M1).
//
// The bd/ sibling package shells out to the `bd` CLI, which in turn
// talks to Dolt. That path is authoritative for mutations — see
// gm-root DD-9 — but carries a subprocess per call and inherits
// whatever version of bd happens to be on PATH. When a `gemba serve`
// operator already has a Dolt server running (the gas-town setup
// does, on port 3307), a direct SQL connection is cheaper, faster,
// and pins the schema version the gemba binary was compiled against.
//
// This adaptor is intentionally read-only. It queries the beads
// schema (issues, dependencies, labels) and projects rows onto
// core.WorkItem. Every mutation method — CreateWorkItem,
// UpdateWorkItem, AttachEvidence — returns core.KindReadOnly so two
// gemba processes pointed at the same Dolt cannot race each other
// into a corrupt state. Operators who need writes use --beads-dir
// (bd CLI path); --dolt-url is board-reads-only.
package dolt
