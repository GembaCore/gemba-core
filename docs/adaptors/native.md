# Native orchestration adaptor

The **native** OrchestrationPlane adaptor ships with the gemba binary
and drives agent sessions directly through local terminals — no
external daemon, no network service, no scheduling layer. It is
Gemba's default orchestration option and the one referenced by most
of the `docs/getting-started/` material.

Epic: [`gm-native`](../../.beads/) (shipped 2026-04-24, 18/18
children closed).

---

## When to use it

- You want agent sessions running right now on the operator's
  workstation.
- You don't have (or don't want) a separate orchestrator daemon.
- You're using Claude Code, or any agent that speaks shell, and want
  its sessions tracked alongside your Beads work items.

Swap to Gas Town / LangGraph / CrewAI / OpenHands / Devin / Factory
when you need those stacks' scheduling, sandboxing, or pool semantics
— the contract is the same (see `orchestration.md`).

---

## What it does

- **Terminal-backend auto-detect** — prefers tmux, then iTerm2, then
  Terminal.app. Override with `.gemba/agents.toml`.
- **StartSession dispatch** — on an operator click, provisions a
  git worktree (`git worktree add -b bead/<id>`), runs `gemba
  install-bridge` in the worktree (idempotent), and launches the
  agent in a fresh pane.
- **Preamble injection** — composes the project + epic + work-item
  preamble via `core/prompt.Envelope` and appends the interaction-
  profile section governing question / blocker behaviour
  (`dangerous` / `balanced` / `cautious`).
- **Escalation surfacing** — permission prompts, HITL requests, and
  `gemba-ask --kind question|blocker` tool calls all surface in the
  SPA. Operator answers route back to the pane as `UserPromptSubmit`.
- **Bead-mutation correlation** — `bd` commands the agent runs inside
  the session attach as `Evidence` on the work item via the bridge's
  `PreToolUse` hook.
- **Session lifecycle** — `EndSession` sends the agent's quit
  sequence, waits a grace period, then kills the pane. Pane death
  detected externally (e.g. operator closes the tab) emits a
  `transport_error` close reason.

---

## Components

| Binary | Role |
|---|---|
| `gemba-bridge` | Hook shim Claude Code invokes per lifecycle event (SessionStart / UserPromptSubmit / PreToolUse / PostToolUse / Notification / Stop). Appends one JSON frame to `~/.gemba/sessions/<id>.jsonl`. Zero state, zero network. |
| `gemba-state` | Session-status sentinel the agent calls at state boundaries (`ready` / `working` / `prompting` / `stalled`). Same frame shape as gemba-bridge. |
| `gemba-ask` | Coach/Manager question/blocker sentinel. `--kind question\|blocker --role coach\|manager --text "…"`. Same frame shape. |
| `gemba-mcp` | MCP-tool server variant of `gemba-ask` + `gemba-state` for agents that speak MCP natively. |

All four are built by `make build-sentinels` and ship in the release
artifact alongside the main `gemba` binary.

---

## Configuration

### `.gemba/agents.toml`

Registers the agent types this workspace wants to spawn and which
terminal backend each uses. Minimal:

```toml
[[agent]]
name             = "claude"
command          = "claude"
interaction_mode = "balanced"  # dangerous | balanced | cautious

[[agent]]
name             = "shell-only"
command          = "bash"
interaction_mode = "dangerous"
```

### `.gemba/interaction_profile.md`

Three named sections (`## dangerous`, `## balanced`, `## cautious`)
the preamble composer injects per agent-type mode. Canonical template
lives at the root of the repo — `cp` it and edit.

### Install step

```bash
gemba install-bridge --agent=claude
```

Merges the hook stanza + `mcpServers.gemba` + a gemba-managed block
into `.claude/settings.local.json` and `CLAUDE.md`. Idempotent;
preserves operator keys outside the sentinel block.

`gemba serve --orchestration=native` runs this install eagerly on
every StartSession, so a pristine worktree just works.

---

## CLI reference

```bash
gemba serve --project-dir <rig> --orchestration=native        # default: auto-detect backend
gemba serve --project-dir <rig> --orchestration=native \
            --backend=tmux                                   # explicit backend
gemba install-bridge                                         # manual install (StartSession does this automatically)
```

---

## Limitations (tracked, not blocking)

- **Localhost only.** Cross-machine sessions are out of scope; use a
  network orchestrator (Gas Town, LangGraph) if you need that.
- **Single-operator.** No mutual exclusion across two `gemba serve`
  processes on the same worktree.
- **Token / cost metering deferred.** Tracked under `gm-root.14`
  (Token spending management epic). Shell-only sessions report
  wallclock only; Claude sessions get token data when that work
  lands.
- **No active-turn interrupt guard.** Ending a session mid-turn kills
  the pane; the operator sees a `transport_error` close reason.

---

## References

- Epic: `gm-native` (closed 2026-04-24).
- Adaptor source: `internal/adapter/native/`.
- Session-state design: `.gemba/interaction_profile.md` + `docs/design/skill-authoring-contract.md`.
- Shared contract: `docs/adaptors/orchestration.md` (required reading
  before implementing a new OrchestrationPlane).
