// Package log is the append-only NDJSON event log with rotation, retention,
// and post-rotate archival.
//
// Every event emitted by the core — workitem.*, session.*, escalation.*,
// budget.*, capability.refresh — lands here as a single JSON line per
// gm-e3.6. This package owns the on-disk bytes; the SSE hub and adaptors
// treat it as a write-only sink.
//
// Rotation fires when the active file exceeds Policy.MaxFileBytes.
// Retention bounds both file count (MaxFiles) and age (MaxAge); whichever
// is stricter wins. The post-rotate hook runs exactly once per rotated
// file and gives callers a place to ship rotated files to cold storage
// (S3, filesystem archive, etc.) before local retention may delete them.
//
// See gm-3hg for the full contract.
package log
