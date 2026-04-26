package planner

import _ "embed"

// SchemaSQL is the DDL for the new session_profiles table — see
// schema.sql for the column-by-column rationale and gm-s47n.2.1.
//
// Embedded so the migration runner (gm-s47n.2.3) can apply it
// alongside the existing core tables without dragging filesystem
// access through the planner package boundary.
//
//go:embed schema.sql
var SchemaSQL string
