// Package transport contains the read-side shims that translate between
// internal/core types and the wire formats Gemba speaks: JSON over HTTP
// (api), newline-delimited JSON (jsonl), and the Model Context Protocol
// (mcp). Each lives in its own subpackage so they can evolve independently.
//
// Phase placeholder: real shims land with gm-e3.4.
package transport
