// Package jsonl is the newline-delimited JSON transport shim, used by
// streaming surfaces (SSE event hubs, log tails, watcher pipelines) and
// by stdio-dispatched in-process adaptors.
//
// Host binds an adaptor that declares Transport=jsonl and runs version
// negotiation at registration time (gm-e3.4). The stdio pump — reading
// JSON frames off stdin, dispatching into the bound adaptor, writing
// results back — lands with the conformance harness (gm-e3.5).
package jsonl
