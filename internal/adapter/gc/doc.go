// Package gc wraps the `gc` CLI for Gas City workspaces.
//
// Gas City is the declarative-SDK successor to Gas Town. As of v3 it is
// in alpha and on track for "fast GA." Until Gas City reaches GA, this
// adapter is designed, stubbed, and kept in sync with the gastownhall/
// gascity repo README — but the v1 runtime target is Gas Town (see
// ../gt/). The work package accordingly stages implementations here
// under gm-e5.7 / gm-e5.8 / gm-e5.9 which are Gas City-native and won't
// be delivered against Gas Town.
//
// When Gas City reaches GA, this package provides typed access to the
// subset of `gc` that Gemba's UI surface depends on:
//
//	gc config explain  — rendered effective config for a rig/agent
//	gc config validate — parse city.toml, report errors without applying
//	gc rig list        — enumerate rigs and their packs
//	gc rig add         — register a new rig
//	gc session list    — running sessions across all agents and rigs
//	gc session attach  — (human use; Gemba won't proxy this)
//	gc session peek    — scrollback snapshot for a session (read-only)
//	gc topo list       — show loaded topology (packs in use)
//
// Every call shells out to the `gc` binary with --json output where
// supported. We never write to controller state directly. All mutating
// operations go through `gc config` edits and the controller reconciles.
//
// See gm-e2.3. The one exception: Gas City's exec escape hatch lets user
// packs shell out to arbitrary commands; Gemba treats those as
// opaque and surfaces only the output Gas City reports back.
package gc
