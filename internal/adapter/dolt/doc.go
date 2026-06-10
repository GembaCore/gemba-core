// Package dolt implements the core.WorkPlane adaptor by opening a
// direct MySQL connection to a Dolt server that hosts a beads database
// (gm-0fd / milestone M1).
//
// The bd/ sibling package shells out to the `bd` CLI, which in turn
// talks to Dolt. When a `gemba serve` operator already has a Dolt
// server running (the gas-town setup does, on port 3307), a direct SQL
// connection is cheaper, faster, and pins the schema version the gemba
// binary was compiled against.
//
// This adaptor queries and mutates the beads schema (issues,
// dependencies, labels) and projects rows onto core.WorkItem. Mutations
// are enabled by default and can be hard-disabled with
// --beads-read-only, which makes mutation methods return
// core.KindReadOnly before issuing SQL.
package dolt
