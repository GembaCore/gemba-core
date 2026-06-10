// Package fs provides low-latency reads of workspace runtime state files
// that rarely change and don't need a CLI round-trip.
//
// In v1, the primary layout is Gas Town's — reads from `~/gt/`:
//
//	~/gt/
//	  .gt/
//	    daemon.json         — scheduler state
//	  .beads/
//	    routes.jsonl        — prefix -> rig directory map
//	    (bead storage read via bd adapter, not here)
//	  <rig>/
//	    .events.jsonl       — per-rig event log (we tail via fsnotify)
//
// The adapter is also designed to handle Gas City's `.gc/` layout so the
// v1 code survives the Gas City transition unchanged:
//
//	~/my-city/
//	  city.toml             — desired config (watch for drift display)
//	  .gc/
//	    controller.lock     — flock marker; DO NOT attempt to acquire
//	    controller.sock     — unix socket; DO NOT connect (gc's private)
//	    agents/<n>.json  — live agent registry: session id, pid, provider
//	    events.jsonl        — append-only event log
//	  rigs/<rig>/
//	    rig.toml
//	    .beads/             — read via bd adapter
//
// What we read here (safely, read-only, flock-aware):
//   - Gas Town: daemon.json, routes.jsonl, per-rig .events.jsonl tail
//   - Gas City: agents/*.json, events.jsonl tail, city.toml drift check
//
// What we NEVER do: connect to controller.sock (Gas City's private
// control channel), write to .gc/ or .gt/, or interpret rig bead state
// directly (all bead reads go through the bd adapter).
//
// See gm-e2.4.
package fs
