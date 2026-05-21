# Phase A — Core SessionIO Interface

## Goal

The codebase exposes a complete, runtime-agnostic session IO contract
on `core.OrchestrationPlaneAdaptor`, with every existing adapter
compiling under a default-unsupported helper.

## Why now

This is the foundational interface for gemba-lite. Every downstream
slice (B native tmux impl, C HTTP transport, D-G frontend) depends on
these types existing. Designed once for the full runtime matrix —
native tmux today, Docker + k8s + microVM tomorrow — so we don't
rewire interfaces when the dispatcher moves into a cluster.

See:
- `docs/design/gemba-lite.md` — full design contract, k8s notes,
  adapter implementation matrix
- `docs/design/containerized-sessions.md` — prior container-backend
  SPEC (constraint carry-forward)
- `.planning/intel/decisions.md` — 8 LOCKED decisions from
  gsd-beads-bridge
- `.planning/intel/constraints.md` — G-1..G-10 (gemba-lite),
  C-1..C-21 (containerized-sessions)

## Locked decisions (from intel)

- Bead-driven AND manual sessions live in one UI surface (G-1).
- `OrchestrationPlaneAdaptor` is the abstraction layer extended for
  session IO — NOT the internal `Backend` interface (G-3).
- `SessionInput.Mode` enumerates exactly three values:
  `InputLiteral` | `InputKeys` | `InputSignal` (G-4).
- `SessionEvent.Kind` enumerates exactly four values:
  `output` | `status` | `exit` | `disconnect` (G-5).
- Resize is its own method `ResizeSession(ctx, sessionID, cols, rows)`
  — never smuggled through `SendInput` (G-6).
- One IO channel per session inside the adapter — refcounted fan-out
  (G-7); not per-call.
- Adapters that genuinely cannot satisfy a method return
  `core.KindUnsupported` (G-9).
- `sessionID` stays opaque across the interface; adapters resolve to
  their own identity (e.g. tmux pane id, k8s pod) internally (C-21).

## Scope (this phase)

In scope:
- Add the three core types to `core/orchestration.go`:
  `SessionInputMode` (with `InputLiteral`/`InputKeys`/`InputSignal`),
  `SessionInput`, `SessionEvent`.
- Extend `OrchestrationPlaneAdaptor` interface with `SendInput`,
  `ResizeSession`, `StreamSession`.
- Add an `unsupportedSessionIO` mixin in `core/` (or a sensible
  sub-package) that returns `core.KindUnsupported` for all three.
- Sweep every existing adapter (`internal/adapter/native`,
  `internal/adapter/native/backend/docker`, `applescript`, the k8s
  surface if it exists, `internal/transport/mcp`,
  `internal/transport/testadaptors`, and any test fakes) to embed the
  mixin so they compile against the extended interface.
- Update any inline interface compliance checks (`var _ OrchPlane = …`).
- Unit tests covering: type values, the default-unsupported behavior.

Out of scope (deferred to later phases):
- Real tmux `pipe-pane` impl (Phase B).
- HTTP endpoints (Phase C).
- Frontend (Phases D-G).
- Refcounted fan-out implementation — only the interface here.

## Success criteria

1. `core` package exports `SessionInputMode`, `SessionInput`,
   `SessionEvent`, and the extended `OrchestrationPlaneAdaptor`.
2. `go build ./...` is green.
3. Every existing adapter embeds `unsupportedSessionIO`; calling any
   of the three new methods on any adapter returns
   `core.KindUnsupported`.
4. `sessionID` remains opaque across the interface (no caller in the
   tree assumes it is a tmux pane id — verified by grep / test).
5. `go test ./core/... ./internal/adapter/...` passes.

## Bead linkage

Epic: `gm-v01.2` — Core SessionIO interface + adapter defaults
Stories already seeded (per docs/process/gsd-beads-bridge.md):
- `gm-v01.2.1` — Add SessionInput/SessionEvent/SessionInputMode types
- `gm-v01.2.2` — Extend OrchestrationPlaneAdaptor with the 3 methods
- `gm-v01.2.3` — Adapter sweep — embed unsupportedSessionIO mixin

Each PLAN.md task must carry a `bead: <id>` field per the bridge
contract. New stories can be filed as the planner discovers needed
subtasks; reference the freshly-minted id in the corresponding task.
