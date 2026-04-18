// Package model defines the shared domain types used across adapters,
// the API layer, and (via codegen) the TypeScript frontend.
//
// Every identifier is a typed newtype. Rigs and beads carry explicit
// town+prefix+id triples so future Wasteland federation works without
// retrofit. See gm-e2.1.
package model
