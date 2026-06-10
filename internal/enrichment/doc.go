// Package enrichment owns the [Enrichment] data type the gm-s47n.1
// epic adds to every WorkItem (targets[] glob patterns + concepts[]
// vocabulary tags) plus the [Store] interface the CLI / planner /
// future LLM-extraction path read and write through.
//
// The package is intentionally decoupled from internal/core's
// WorkItem schema. Today the WorkItem.targets / .concepts fields are
// not yet defined (gm-s47n.1.1 is in_progress); enrichment ships the
// CLI surface and storage now and rewires to bd extras the moment
// the schema lands. The migration is a one-method change at the
// [Store] boundary.
//
// Storage today: one JSON file per bead under
// <workspace>/.gemba/enrichment/<safe-id>.json. The safe-id replaces
// the bd id's '/' separator with '__' so workspace-prefixed ids
// (gemba/gemba/gm-1) round-trip across filesystems. The
// [FileStore] is the in-tree implementation; tests use [MemoryStore].
package enrichment
