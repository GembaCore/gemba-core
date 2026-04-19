// Package api is the JSON-over-HTTP transport shim. Server handlers in
// internal/server call into core, then format responses through this
// package so the wire shape can change without rewriting the handlers.
//
// Phase placeholder: real shim lands with gm-e3.4.
package api
