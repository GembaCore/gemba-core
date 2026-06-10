// Package onboarder implements the Onboarder persona — the transient,
// single-skill host for the newproject skill (gm-root.17.10).
//
// What this package owns:
//
//   - The Onboarder persona type — bundled with the binary, not stored
//     in ~/.gemba/personas/ or any workspace. Spawned on demand,
//     discarded after the conversation.
//   - The persona-skill binding contract: the Onboarder runs the
//     newproject skill (internal/skills/newproject) and nothing else.
//     Calling [Spawn] returns a [Persona] whose [Persona.Turn] method
//     wraps newproject.Run with the validation-retry-once policy
//     described in docs/design/newproject.md §"Onboarder persona +
//     newproject skill".
//   - Credential resolution: [Resolve] reads the same configuration
//     used elsewhere by Gemba's agent layer (~/.gemba/config.toml,
//     [llm] table). When no chat client is configured, [Spawn]
//     returns [ErrNoLLMClient] with the diagnostic the SPA renders on
//     /onboard or in the board's Onboarder CTA gate. No first-run
//     credential prompt — operators configure credentials in
//     config.toml before kicking off a New project conversation.
//   - A [SkillTurner] adapter so server.AttachNewProject can bind a
//     real Onboarder in place of the stub turner shipped at
//     gm-root.17.4.
//
// What this package does NOT own:
//
//   - The newproject skill itself (gm-root.17.5 — see
//     internal/skills/newproject).
//   - Multi-turn HTTP routing for /api/v1/newproject/* — that lives in
//     internal/server/newproject.go (gm-root.17.4). The server holds
//     one Persona per session in its in-memory store.
//   - The atomic ratify transaction (gm-root.17.6 — see
//     internal/server/newproject_ratify.go).
//   - Persistence: there is none. State lives in process memory for
//     the duration of one conversation; refresh / restart / Ctrl-C
//     discards it. Only the synthesized docs/project.md survives,
//     written by ratify after the operator confirms.
//
// Process model: the Onboarder runs inline in the server process — no
// OrchestrationPlane dispatch, no agent runtime spun up. v1 is
// deliberately narrow; future iterations may move it onto the
// OrchestrationPlane if there's a reason.
package onboarder
