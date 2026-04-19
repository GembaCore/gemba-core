// Package server wires the HTTP surface: chi router, SPA fallback, JSON
// API, and the SSE hub. NewRouter accepts the embedded SPA filesystem as
// a parameter so this package has no compile-time dependency on the
// embed declaration in cmd/gemba.
//
// For v0 of the scaffold, handlers return placeholder 501 responses. The
// real surface lands with gm-e2.6.
package server
