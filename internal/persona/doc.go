// Package persona is the runtime invocation layer for personas.
// It owns the dispatcher that wires a [core/persona.Persona] +
// [core/persona.Skill] to the spawn-Claude-Code-via-tmux + MCP
// callback infrastructure that the rest of Gemba already runs on
// (gm-twp2).
//
// This package is distinct from [internal/core/persona], which is
// the parsed-config layer (TOML loader, Persona struct, Skill
// interface, registries). The split keeps core/persona free of
// runtime concerns (filesystem, tmux, MCP) so it can stay as a
// pure type-and-validation package the dispatcher and the HTTP
// handlers both consume.
//
// Layout:
//
//   - auditlog.go — PersonaConsultRecord JSON persistence
//   - dispatcher.go (TBD) — Dispatch + Receive + Finish
//   - sessions.go (TBD) — in-memory consult registry
//
// What lives in providers/<vendor>/ subpackages:
//
//   - Concrete spawn drivers (Claude Code today; codex / opencode /
//     gemini follow-up beads), each translating a DispatchRequest
//     into the agent-runtime-specific spawn spec.
package persona
