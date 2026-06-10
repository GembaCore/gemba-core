// Package newproject implements the Onboarder persona's bundled
// conversational skill (gm-root.17.5). It turns a free-form
// conversation between the operator and an LLM into a typed
// Milestone -> Epic -> Bead plan tree that the atomic ratify
// transaction (gm-root.17.6) can persist as a freshly-bootstrapped
// project workspace.
//
// What this package owns:
//
//   - The output schema (DraftMilestone / DraftEpic / DraftBead /
//     NewProjectState / ChangeRef) — wire-aligned with the SPA-side
//     shapes in web/src/api/newproject.ts. Both sides hand-roll
//     because the project has no OpenAPI/TS codegen pipeline.
//   - The bundled prompt scaffolding (system prompt, output-schema
//     instructions, ~four few-shot examples) embedded into the
//     binary via go:embed.
//   - The single-turn state machine: Run takes the prior state +
//     operator message + injected LLMClient, returns the updated
//     state and the assistant reply.
//   - Per-turn validators that distinguish "the model returned a
//     malformed plan" (retryable) from "the input was unusable"
//     (caller error). The persona host (gm-root.17.10) decides
//     whether to retry; this package only signals.
//
// What this package does NOT own:
//
//   - The Onboarder persona itself, its lifecycle, or its
//     pre-workspace credential resolution (gm-root.17.10).
//   - The atomic ratify transaction that materialises the plan
//     tree as bd epics + beads + commit (gm-root.17.6).
//   - HTTP routing for /api/v1/newproject/* (gm-root.17.4 — the
//     server handler bead consumes this skill).
//
// One-shot lifetime: NewProjectState lives only in the host's
// memory. This package never persists anything; callers MUST NOT
// add a persistence layer.
package newproject
