// Package api is the JSON-over-HTTP transport shim. Server handlers in
// internal/server call into core, then format responses through this
// package so the wire shape can change without rewriting the handlers.
//
// Host binds an in-process adaptor that declares Transport=api, runs
// version negotiation at registration time (gm-e3.4), and keeps the
// binding for the handlers in internal/server (gm-e4.x) to dispatch
// through. Out-of-process HTTP wire support lands alongside those
// handlers.
package api
