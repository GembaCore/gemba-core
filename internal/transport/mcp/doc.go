// Package mcp is the Model Context Protocol transport shim, exposing
// Gemba's read surface to MCP-aware clients (Claude Desktop, IDE plugins).
//
// Host binds an adaptor that declares Transport=mcp and runs version
// negotiation at registration time (gm-e3.4). MCP is recommended-not-
// required (gm-root DD-12); the wire-level MCP server lands with the
// conformance harness (gm-e3.5).
package mcp
