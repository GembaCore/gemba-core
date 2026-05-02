---
title: "Session runtime integration patterns"
decision: none
---

# Session Runtime Integration Patterns

Status: design guidance

Date: 2026-05-01

## Purpose

Gemba can supervise several kinds of agent/session runtimes:

- local native sessions driven through terminals,
- model CLIs such as Claude Code and OpenAI Codex,
- external orchestrators such as Gas Town.

Each runtime exposes different integration points. This document
captures the current contracts, hooks, assumptions, and best-practice
pattern to use when adding future runtimes.

The central rule: Gemba should normalize every runtime into the
OrchestrationPlane contract while preserving the strongest telemetry
surface the runtime actually supports.

## Shared Gemba Session Contract

Every runtime integration should answer the same questions:

1. How does Gemba start work?
2. How does the agent receive project, epic, bead, DoD, and interaction
   guidance?
3. How does Gemba observe lifecycle state?
4. How does the agent ask questions or raise blockers?
5. How does Gemba correlate file/work-item mutations to a session?
6. How does completion become durable and auditable?
7. What is authoritative, and what is cooperative?

The common event sink for local/native tools is a JSONL session log at:

```text
~/.gemba/sessions/<session_id>.jsonl
```

Frames written by `gemba-bridge`, `gemba-state`, `gemba-ask`, and
`gemba-mcp` share the same shape:

```json
{
  "ts": "2026-05-01T23:00:00Z",
  "session_id": "tmux:gm-123:...",
  "agent_type": "claude",
  "hook": "GembaState",
  "event_id": "...",
  "payload": {}
}
```

The bridge translator converts those frames into
`core.OrchestrationEvent` values. The native adaptor consumes those
events to update `core.Session`, escalation indexes, evidence, and
status surfaces.

External orchestrators may not use the JSONL bridge, but they must
still emit equivalent OrchestrationPlane events through their adaptor.

## Runtime Comparison

| Dimension | Claude Code native | Codex native | Gas Town |
| --- | --- | --- | --- |
| Gemba layer | Native OrchestrationPlane agent type | Native OrchestrationPlane agent type plus driver | External OrchestrationPlane adaptor |
| Runtime process | `claude` in tmux/iTerm/Terminal pane | `gemba-codex-driver` runs `codex exec --json` | `gt` CLI / Gas Town service |
| Agent identity | Gemba session metadata; may be paired with persona/pool identity | Gemba session metadata; one-shot Codex execution | Gas Town polecat identity persists across sessions |
| Preamble delivery | `claude_md`: append sentinel block to `CLAUDE.md`, then first message points Claude at it | `codex_exec`: write prompt file; driver passes prompt to `codex exec` via stdin | API/adaptor payload or Gas Town's own assignment/session model |
| Hook surface | Claude Code Hooks API | No Claude-style hooks | Gas Town API/events, not in-agent hooks |
| Installed hook/config | `.claude/settings.local.json` with hooks and `mcpServers.gemba` | Session-scoped `codex exec -c mcp_servers.gemba...` overrides from driver | Gas Town registration/config; no MCP required today |
| Gemba binaries used | `gemba-bridge`, `gemba-state`, `gemba-ask`, `gemba-mcp` | `gemba-codex-driver`, `gemba-state`, `gemba-mcp` | `gt` plus Gemba Gas Town adaptor |
| Lifecycle authority | Claude hooks plus `gemba-state`; pane lifecycle managed by native adaptor | Driver owns process, timeout, close, commit, fallback `bead-done`; MCP is cooperative | Gas Town owns polecat/session lifecycle; adaptor polls or calls API |
| State reporting | Hook frames and explicit `gemba-state` / MCP `report_state` | Driver fallback `gemba-state`; cooperative MCP `report_state` | Gas Town session/polecat status mapped to Gemba session status |
| Tool/file observation | Strong: `PreToolUse` / `PostToolUse` frames, especially Bash/file tools | Limited: Codex JSON stream can be parsed; MCP only reports what model chooses | Gas Town API/session transcript; visibility depends on `gt` surface |
| Questions/blockers | `gemba-ask` or MCP tools; Claude permission prompts via hooks | MCP `ask_question` / `raise_blocker`; shell fallback possible if prompt directs it | Gas Town mail/escalation surfaces mapped by adaptor |
| Skill output | `emit_skill_output` via MCP or CLI-compatible frames | `emit_skill_output` via session-scoped MCP | Adaptor-specific mapping from Gas Town outputs/events |
| Completion signal | Agent closes bead and emits `bead-done`; native adaptor checks clean worktree before SessionReady | Driver closes bead/commits and emits fallback `bead-done`; Codex may also call MCP `bead-done` | Gas Town completion state mapped to session/work-item events |
| Reuse/pooling | Supported for capable native sessions | Intentionally one-shot today; cold-start when no active Codex work exists | Native to Gas Town polecat pools |
| Best confidence source | Hook-observed runtime events | Driver process result plus worktree/bead verification | Gas Town authoritative API state |
| Main compromise | Tied to Claude Code hook semantics and local config | Cooperative MCP cannot replace missing hooks | Less direct local tool visibility; depends on external orchestrator contract |

## Claude Code Native Integration

Claude Code is the richest local integration because it exposes a
Hooks API and reads `CLAUDE.md`.

### Integration points

- `.gemba/agents.toml`
  - `binary = "claude"`
  - `preamble = "claude_md"`
  - `hooks = "claude_code"`
- `.claude/settings.local.json`
  - SessionStart, UserPromptSubmit, PreToolUse, PostToolUse,
    Notification, and Stop hooks call `gemba-bridge`.
  - `mcpServers.gemba` points at `gemba-mcp`.
- `CLAUDE.md`
  - Gemba appends a temporary sentinel block containing the composed
    project/epic/bead preamble.
- `gemba-state`
  - Explicit status boundary reporting.
- `gemba-ask` / `gemba-mcp`
  - Structured questions, blockers, and skill output.

### Assumptions

- Claude Code will load `CLAUDE.md`.
- Claude Code will invoke configured hooks with the expected payload
  shape.
- Hook writes are local, fast, and append-only.
- `gemba-bridge` can fail closed for policy prompts only where the
  hook protocol permits it; otherwise hook failure should be visible
  but not corrupt session state.

### Consequences

Claude can support the highest fidelity status and evidence model:

- permission prompts,
- tool use,
- file/Bash correlation,
- lifecycle frames,
- MCP-native structured outputs.

For future runtimes, Claude Code is the reference for what "full
fidelity" looks like, not the minimum bar every runtime must meet.

## Codex Native Integration

Codex does not expose a Claude-style lifecycle hook surface, so the
integration uses a wrapper driver plus cooperative MCP tools.

### Integration points

- `.gemba/agents.toml`
  - `binary = "gemba-codex-driver"`
  - `preamble = "codex_exec"`
  - `hooks = "none"`
- `gemba-codex-driver`
  - Reads the Gemba-generated prompt file.
  - Runs `codex exec --json`.
  - Injects session-scoped MCP configuration with `codex exec -c
    mcp_servers.gemba...`.
  - Emits fallback `working`, `stalled`, and `bead-done` through
    `gemba-state`.
  - Closes the bead and commits dirty worktree content after successful
    Codex execution.
- `gemba-mcp`
  - Exposes `report_state`, `ask_question`, `raise_blocker`, and
    `emit_skill_output`.
- Codex prompt
  - Tells Codex to prefer MCP tools for semantic status, questions,
    blockers, and skill output.

### Assumptions

- Codex CLI supports `codex exec --json`.
- Codex CLI supports MCP server configuration through `-c
  mcp_servers.<name>...` overrides.
- MCP calls are cooperative. The model may omit them, call them late,
  or report imperfect semantic status.
- The driver remains the hard lifecycle boundary and must not depend
  on MCP calls for correctness.

### Consequences

Codex can participate in rich Gemba surfaces, but confidence is layered:

1. Driver process result, timeout, close, commit, and fallback state are
   authoritative.
2. Codex JSON stream parsing can provide additional runtime traces when
   available.
3. MCP tool calls provide semantic intent and operator-facing detail.

Do not treat Codex MCP telemetry as equivalent to Claude `PreToolUse` /
`PostToolUse` hooks.

## Gas Town Session Integration

Gas Town is not a model CLI inside the native adaptor. It is an
external orchestration runtime with its own agent identities and session
model. Gemba integrates through an OrchestrationPlane adaptor.

### Integration points

- `internal/adapter/gt`
  - Implements the Gas Town OrchestrationPlane adaptor.
- `gt` CLI / Gas Town service
  - Provides JSON API-style surfaces such as rig, polecat, mail,
    session, escalation, and transcript commands.
- Gemba adaptor manifest
  - Declares transport, workspace kinds, group modes, cost axes,
    escalation kinds, peek modes, and event delivery.

### Assumptions

- Gas Town is authoritative for polecat identity, pool membership, and
  remote/local session lifecycle.
- Gemba should not inject local `gemba-bridge` hooks into Gas Town
  sessions unless Gas Town explicitly adopts that bridge contract.
- Event delivery may be poll-based rather than immediate hook delivery.
- Escalations may arrive through Gas Town mail/escalation APIs rather
  than `gemba-ask` frames.

### Consequences

Gas Town is the pattern for external orchestrators:

- integrate at the OrchestrationPlane boundary,
- map external agent/session concepts into Gemba core types,
- expose capability manifest truthfully,
- prefer adaptor-level events over pretending the runtime is local.

## Best-Practice Pattern for Future Session Runtimes

Use this order of preference when integrating a new runtime.

### 1. Pick the Right Integration Layer

If the runtime is a local CLI that Gemba launches directly, implement it
as a native agent type.

If the runtime owns scheduling, pools, identity, workspaces, or remote
execution, implement it as an OrchestrationPlane adaptor.

If the runtime is only a model provider and does not manage files or
sessions, integrate it as a provider behind a persona/skill call, not as
a session runtime.

### 2. Separate Authoritative Signals from Cooperative Signals

Authoritative signals are observed or enforced outside the model:

- process start/exit,
- hook callbacks,
- API session state,
- worktree cleanliness,
- work-item close state,
- committed artifacts.

Cooperative signals are emitted by the model because the prompt asked
it to:

- "I am working",
- "I am blocked",
- "this is done",
- "here is my evidence summary".

Use cooperative signals to improve UX. Do not use them as the only
source of correctness.

### 3. Prefer Structured Channels Over Transcript Scraping

Preferred:

- runtime hooks,
- MCP tools,
- sentinel CLIs,
- API events,
- JSONL process streams.

Avoid:

- parsing prose sections from the transcript,
- inferring state from terminal scrollback,
- relying on final chat text as the only evidence.

### 4. Normalize to Gemba Events Early

Every integration should translate its native events into common Gemba
event kinds as close to the boundary as possible:

- `session_state_reported`
- `tool_use`
- `escalation_opened`
- `skill_output_emitted`
- `session_completed`
- `transport_error`
- `evidence_attached`

The SPA and dispatcher should not need to know whether a state came
from Claude hooks, Codex MCP, `gemba-state`, or Gas Town polling.

### 5. Always Provide a Completion Backstop

Completion should require an independently verifiable condition:

- work item closed,
- expected files changed,
- tests/evidence captured,
- worktree clean or safely committed,
- runtime process reached success,
- adaptor emitted the close event.

An agent saying "done" is useful, but not enough.

### 6. Capability-Gate the UI

Expose runtime features through the adaptor manifest and session
metadata. The UI should show controls only when the runtime supports
them:

- live tool trace,
- pause/resume,
- transcript peek,
- escalation response,
- cost/token rollups,
- session reuse,
- workspace acquire/release.

Missing capability is normal. Pretending every runtime is Claude Code
creates brittle UX.

## Integration Checklist

For every new runtime, document these before implementation:

| Question | Required answer |
| --- | --- |
| Runtime type | Native agent type, external OrchestrationPlane adaptor, or persona provider |
| Start command/API | Exact binary/API and required environment |
| Preamble path | File, first message, stdin prompt, API payload, or external assignment |
| State channel | Hook, MCP, sentinel CLI, API polling, JSON stream, or none |
| Question/blocker channel | MCP, CLI, external mail/escalation API, or unsupported |
| Tool/file telemetry | Hook-observed, JSON stream, external API, cooperative only, or unavailable |
| Completion backstop | What proves done independent of model prose |
| Evidence path | Comments, WorkPlane evidence, artifacts, transcript, or issue properties |
| Config install | Which files are written and whether install is idempotent |
| Failure mode | What happens when hooks/tools/config are unavailable |
| Capability flags | Which SPA controls should be visible |

## Current Recommendation

Use three named integration tiers:

1. **Hooked runtime**
   - Example: Claude Code.
   - Runtime exposes lifecycle/tool hooks.
   - Highest fidelity; use hooks plus MCP/sentinels.

2. **Driven runtime**
   - Example: Codex CLI.
   - Gemba owns a wrapper process and uses MCP/tools for cooperative
     semantic telemetry.
   - Reliable lifecycle through driver; partial runtime visibility.

3. **Delegated runtime**
   - Example: Gas Town.
   - External orchestrator owns identities, pools, and session
     lifecycle.
   - Gemba integrates through the OrchestrationPlane API and capability
     manifest.

Future integrations should explicitly choose one tier. If a runtime
does not fit one of these tiers, design the tier first rather than
forcing the runtime into the Claude or Codex shape.
