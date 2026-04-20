// Package transport contains the shims that translate between
// internal/core types and the wire formats Gemba speaks: JSON over HTTP
// (api), newline-delimited JSON (jsonl), and the Model Context Protocol
// (mcp). Each lives in its own subpackage so they can evolve independently.
//
// This package also carries the cross-transport registration contract
// shared by all three hosts: Registration, Plane, Negotiate, plus the
// VersionMismatchError / TransportMismatchError types the CLI surfaces
// verbatim. See host.go for the full protocol (gm-e3.4, DD-12/DD-15).
package transport
