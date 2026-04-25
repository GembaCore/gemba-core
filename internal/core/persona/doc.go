// Package persona is the parsed-config layer for Gemba personas
// (gm-57b / gm-518). A Persona is a typed, opt-in LLM advisor that
// the operator may invoke through one of its declared Skills. The
// persona file (TOML) carries the configuration; coded Skills are
// registered separately by the runtime.
//
// This package owns the boundary between disk and runtime:
//
//   - LoadFile / LoadDir parse `.gemba/personas/*.toml` into a
//     [Persona] value with sensible defaults applied.
//   - [Registry] is the in-memory lookup the rest of the system
//     uses; the HTTP layer holds one Registry per workspace.
//
// What this package does NOT own:
//
//   - The Anthropic / provider call (see internal/persona/providers/*).
//   - Skill schemas or skill-specific prompt composition (see
//     internal/skills/<skill>).
//   - The audit log surface — PersonaConsultRecord lives in this
//     package as a type, but persistence is the server's job.
//
// Stability: every exported field on [Persona] and its sub-structs
// is part of the on-disk schema. Changing a JSON/TOML tag is a
// breaking change to user-authored persona files; bump the schema
// version (declared per-file as `schema_version`) before doing so.
package persona
