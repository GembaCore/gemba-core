// Package gt wraps the `gt` CLI for Gas Town v1.0 workspaces.
//
// Gas Town is the stable runtime Gemba ships against today.
// Version 1.0 was released on April 3, 2026 after 14 iterative releases;
// the CLI surface this adapter depends on is stable.
//
// Operations used by Gemba's UI:
//
//	gt status          — overall town state
//	gt rig list        — rigs in the current town
//	gt agents          — currently running agents and their rigs
//	gt convoy list     — convoys with their bead membership
//	gt convoy create   — group beads into a convoy
//	gt sling <id>      — assign a bead to an agent
//	gt mail            — listing and send (paired with bd)
//	gt escalate        — surface user-initiated escalations
//	gt daemon status   — scheduler health
//
// Every call shells out to the `gt` binary with --json output. Never
// write to Gas Town state directly (Dolt, JSONL, `.gt/` internals).
// Reads can hit `~/gt/` filesystem state for latency via the fs adapter.
//
// See gm-e2.3. The gc adapter (../gc/) is the Gas City counterpart —
// stubbed in v1, becomes primary when Gas City reaches GA.
package gt
