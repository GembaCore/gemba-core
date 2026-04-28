# Agents

Per-role operating docs for the AI agents Gemba spawns. Each role
has its own loop — what it watches, what it does, when it escalates
— captured as a durable reference rather than a per-session prompt.

## Pages in this section

- **[Refinery — CI watch after merges](refinery-ci-watch)** — the
  refinery role's loop for keeping `main` green after every merge.

## Where next

- The agent-runtime contract: see [OrchestrationPlane authoring](../adaptors/orchestration).
- Architectural model of personas: see [Persona PPPP](../design/persona-pppp).
