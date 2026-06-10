---
title: "Native support improvements"
decision: gm-root.31.1
---

# Native Support Improvements

Status: draft / decision filed

Date: 2026-05-05

Epic: `gm-root.31`

## Summary

Gemba's native/tmux implementation is already deeper than t3code on
work-item orchestration: Beads hierarchy, milestone/epic cascade,
dependency-aware dispatch, escalations, Gemba Walks, evidence, and
source-analysis context all sit above the coding-agent runtime.

The gap identified from the t3code comparison is not raw orchestration
power. It is session ergonomics. Native mode should make agent/provider
capability, live terminal state, remote operation, source-control
readiness, and conversational controls obvious to an operator before
they dispatch work.

Decision `gm-root.31.1` accepts six improvements:

1. Provider fidelity classes and an explicit OpenCode/generic-CLI path.
2. A live native terminal/session workspace UX.
3. Remote native backend pairing and SSH launch design.
4. Source-control health and action surfaces for scopes/worktrees.
5. Conversational turn controls: send turn, interrupt, rollback/checkpoint.
6. Native/tmux operator guide and help text polish.

## Ratified Product Decisions

The following choices are ratified for `gm-root.31` implementation:

| Question | Decision |
| --- | --- |
| Terminal UX shape | Start with attach/open affordances and captured transcript. Browser-native terminal streaming is deferred. |
| OpenCode support | Add OpenCode as an invoked CLI profile first. A richer SDK/driver remains a later improvement. |
| Remote native scope | Specify the remote pairing/SSH launch model in this epic, but defer implementation. |
| Source-control provider | Implement GitHub only in the first slice; keep the contract provider-neutral and defer GitLab/Bitbucket/Azure. |
| Rollback/checkpoint | Implement real rollback/checkpoint semantics, including "revert to yesterday" style restoration to a known prior state. |
| Operator-facing provider labels | Use **Integrated**, **Managed**, and **Invoked** in UI. Avoid low-confidence wording such as "basic". |

## Context

t3code is a minimal web GUI for coding agents. Its public surface centers
on Codex, Claude, OpenCode, provider/model selection, terminal drawers,
remote pairing, source-control provider workflows, pending approvals,
diff/review views, and observability.

Gemba's native adaptor has a different center of gravity. It supervises
work items and dispatches local shell-backed sessions through the
OrchestrationPlane contract. Native supports tmux/iTerm/Terminal/Docker
backends, worktree provisioning, bridge logs, Claude hooks, Codex driver
execution, MCP reporting, escalation indexes, session peek, and
workspace cleanliness checks.

That means Gemba should not copy t3code's product model wholesale.
Instead, native mode should borrow the clarity of t3code's session
ergonomics while preserving Gemba's work-first operating model.

## Product Principle

Native mode should answer three questions at a glance:

1. What is this agent capable of observing and reporting?
2. Where is the live working session, and how do I inspect or guide it?
3. Is the surrounding project state healthy enough to trust the output?

If a capability is unavailable for a provider or backend, the UI should
say so directly. Capability-gated absence is better than a generic
control that appears to work but cannot provide reliable state.

## Provider Visibility Classes

Native agent profiles should advertise their telemetry and control
fidelity separately from provider name or model. The UI should use
operator-friendly labels, while the implementation can retain more
specific internal details.

| UI label | Internal shape | Example provider | Confidence source | Expected UX |
| --- | --- | --- | --- | --- |
| Integrated | Full hook or SDK/API event integration | Claude Code native; future OpenCode driver | Hook/SDK frames plus explicit `gemba-state` / MCP | Rich status, tool/file correlation, permission prompts, escalation detail. |
| Managed | Gemba-owned driver lifecycle | Codex via `gemba-codex-driver` | Driver process result, JSON stream, fallback state, cooperative MCP | Reliable lifecycle, semantic status when reported, less tool-level certainty than integrated hooks. |
| Invoked | CLI invocation under a terminal/backend | OpenCode CLI profile; shell-only; unknown CLI | Pane/container lifecycle and captured output | Start/stop/peek plus whatever the CLI cooperatively reports. |

The agent registry should keep its current executable configuration, but
UI/API projections should include fidelity class, provider, model,
hook/MCP status, and limitations.

OpenCode enters this epic as an **Invoked** profile: Gemba launches the
CLI and observes it through the native backend. A later OpenCode driver
can promote it to **Integrated** if it consumes OpenCode SDK/API events
directly.

Story: `gm-root.31.2`.

## Terminal And Session Workspace UX

The RHP Status pane should remain the cockpit for sessions, escalations,
and run metrics. It should not be the only way to inspect a native
session.

Native sessions need an explicit workspace affordance:

- attach/open terminal when the backend supports it;
- show recent capture when direct attach is unavailable;
- display cwd, worktree path, branch, active command, backend kind,
  pane/container id, provider, model, and fidelity class;
- provide clear actions for peek, copy session id, open worktree, and
  end session;
- surface pending escalations inline.

For tmux, the first implementation can expose a command or deep action
that attaches to the pane/window and still keeps the Gemba UI in sync.
For browser-native terminal streaming, a later slice can add a PTY
transport behind a capability gate.

First slice scope is attach/open plus capture. Do not build browser
terminal streaming in this epic.

Story: `gm-root.31.3`.

## Remote Native Pairing

Remote native operation is useful when the UI runs locally but the
worktree and tmux sessions live on another machine. The design should be
explicit before implementation:

- `gemba serve` on the remote owns projects, files, git state,
  terminals, and provider sessions.
- A client pairs to that backend with an expiring token or link.
- Non-loopback exposure requires auth and should prefer HTTPS/Tailscale
  or another trusted private network.
- Pairing credentials can be revoked and should never be long-lived
  secrets copied into logs.
- Desktop-managed SSH launch may start/reuse the remote Gemba process
  and forward a local port, but the remote backend remains authoritative.

This is a native backend story, not a replacement for Gas Town. Gas Town
still owns Sling/crew/polecat orchestration. Remote native is for users
who want direct shell-backed sessions without the Gas Town layer.

This epic specifies the remote native model and defers implementation.
Implementation should become a follow-up epic/story once pairing,
revocation, transport, and SSH launch security are settled.

Story: `gm-root.31.4`.

## Source-Control Health And Actions

Scope cards and session/worktree details should include source-control
status beside Beads and source-analysis status:

- clean/dirty worktree;
- branch and upstream;
- ahead/behind;
- remote configured and reachable;
- provider auth health;
- existing PR/MR for the branch;
- publish/create-review readiness.

The implementation should start provider-neutral and ship GitHub only.
Use `gh` where appropriate. GitLab, Bitbucket, and Azure follow the same
contract later and should not block this epic.

Actions must be capability-gated. If auth, CLI, remote, or provider
support is missing, the UI should explain the missing setup rather than
showing a dead button.

Story: `gm-root.31.5`.

## Conversational Session Controls

Gemba's current OrchestrationPlane contract is strong for lifecycle:
Start, Pause, Resume, End, Recycle, Peek, workspace acquire/release, and
escalation resolution.

Provider runtimes also have a conversational shape:

- send another turn/message;
- interrupt an active turn;
- respond to structured approval/user-input requests;
- read a thread snapshot;
- rollback/revert a number of turns or restore a checkpoint;
- restore the project to a named or time-relative prior state, such as
  "revert to yesterday", when Gemba has enough checkpoint/history
  evidence to resolve that target unambiguously.

Those controls should be named separately from lifecycle controls. A
session can be alive but not capable of rollback; a backend can support
EndSession without supporting SendTurn. Native should expose this through
capability declarations and UI gating.

Initial guidance:

- `RespondToUserInput` remains escalation/request handling.
- `SendTurn` is only exposed when the provider has a reliable message
  channel or a safe keystroke protocol.
- `Interrupt` is only exposed when it maps to provider/runtime support,
  not just process kill.
- `Rollback` / `RestoreState` is a real capability in this epic. It
  should align with the existing Gemba checkpoint requirement: restore a
  worktree/session/project to a known prior state using auditable
  checkpoint or VCS history. Time-relative requests such as "yesterday"
  resolve to a concrete checkpoint/commit before mutation. If rollback
  changes files, it must be auditable, nonce-gated, and clearly separate
  from `git reset --hard`.

Story: `gm-root.31.6`.

## Documentation And Help Text

Native/tmux mode needs an operator guide that explains:

- when to choose native versus Gas Town versus Beads-only;
- what tmux/iTerm/Terminal/Docker backends mean;
- how `.gemba/agents.toml` works;
- how Claude, Codex, OpenCode, and invoked CLI profiles differ;
- how bridge hooks, MCP, and driver telemetry affect status confidence;
- how to dispatch a bead, inspect a session, handle an escalation, and
  verify evidence;
- common failures: missing binary, missing tmux, stale worktree, dirty
  worktree, degraded bridge, missing MCP, provider auth failure.

The docs should lead with product behavior and use implementation detail
only when it helps the operator troubleshoot.

Story: `gm-root.31.7`.

## Fit / Gap Assessment

| Area | Current Gemba state | Gap | Action |
| --- | --- | --- | --- |
| Work-item orchestration | Stronger than t3code | None for native/tmux comparison | Preserve Gemba's work-first model. |
| Provider clarity | Functional but implementation-heavy | Users cannot easily see fidelity/limitations | Add provider fidelity classes. |
| Live terminal UX | Backend supports panes/capture; UI mostly status/peek | Attach/workspace affordance is too hidden | Add terminal/session workspace UX. |
| Remote native | Deployment docs exist; no crisp pairing model | Remote native story unclear | Design pairing and SSH launch. |
| Source-control health | Scope/worktree cleanliness exists in parts | Hosted review/auth readiness not first-class | Add source-control health/action surface. |
| Turn controls | Lifecycle contract is explicit | Conversational controls are not named consistently | Define capability-gated turn controls. |
| Operator docs | Rich but scattered | Native mode requires code/design reading | Add a concise native/tmux guide. |

## Non-Goals

- Do not replace Gas Town with remote native.
- Do not make the SPA a generic coding-agent chat clone.
- Do not expose controls that the active provider/backend cannot support.
- Do not treat cooperative MCP reports as equivalent to hook-observed
  events.
- Do not implement browser terminal streaming in the first native attach
  slice.
- Do not implement non-GitHub source-control providers in the first
  source-control health slice.
- Do not implement remote native pairing in this epic; specify it and
  defer implementation.
- Do not add provider-specific vocabulary to core UI outside capability
  labels and extension points.

## Implementation Order

Recommended order:

1. Provider fidelity classes (`gm-root.31.2`).
2. Documentation polish (`gm-root.31.7`) in parallel with provider
   metadata so operators can use the new vocabulary immediately.
3. Terminal/session workspace UX (`gm-root.31.3`).
4. Source-control health/actions (`gm-root.31.5`).
5. Conversational controls and real rollback/checkpoint restore
   (`gm-root.31.6`).
6. Remote native pairing design (`gm-root.31.4`), with implementation
   deferred to a later epic/story once the security model is settled.

This sequence gives the product clearer language first, then adds
larger capabilities behind that language.
