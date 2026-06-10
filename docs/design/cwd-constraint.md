---
title: "cwd-constraint: agent filesystem trust boundary"
decision: gm-f9jt
d: D8
ratified_at: 2026-04-25
---

# cwd-constraint: agent filesystem trust boundary

> Owning epic: **gm-v8vr** (Agent cwd-constraint: scoped surface +
> read whitelist + per-bead overrides). Layer-3 container runtime is
> **gm-utik**.

## What this is

A defense-in-depth contract that governs which host paths a spawned
agent can read or write. Every layer enforces the same `Surface`
shape; each layer is independently sufficient and operators trust
the OS-mediated layer the most.

## The Surface

A `persona.Surface` (`internal/persona/surface.go`) is the resolved
read/write capability set for one spawn:

| Field | Mode | Source |
|---|---|---|
| `Cwd` | rw | spawn working dir resolved by `PersonaScope.ResolveWorkingDir` |
| `AdditionalWrites` | rw | `WorkItem.AdditionalWritePaths` (bead-level operator opt-in) |
| `SiblingReads` | ro | every other repo in the workspace's `RepositoryRegistry` |
| `WorkspaceMetadata` | ro | the workspace's `.gemba/` directory |
| `ToolingPaths` | ro | `DefaultToolingPaths` template (`$HOME/.gitconfig`, `$GOPATH/pkg/mod`, …) |
| `AdditionalReads` | ro | `WorkItem.AdditionalReadPaths` + `Persona.Scope.AdditionalReadPaths` |

`AllowedReads()` / `AllowedWrites()` are the two flat accessors every
enforcement layer consults.

## Three enforcement layers

### Layer 1: model preamble (advisory)

The dispatcher renders the surface into the system prompt so the
agent knows the rules. Easiest to bypass; primary purpose is to
make compliance the path of least resistance for a cooperating
model.

### Layer 2: PreToolUse hook (process-level)

`gemba-bridge`'s tool-call hook checks every path argument the model
emits against `AllowedReads()` / `AllowedWrites()` before letting the
call proceed. Catches misbehaving tools and accidental escapes.
Runs in the same process as the agent — defeated by a model that
patches its own bridge or invokes shell commands directly.

### Layer 3: container volume mounts (OS-mediated)

**This is gm-utik.** When the agent runs in a containerized session,
the surface is translated into the container's volume-mount list:

- Every entry in `AllowedWrites()` becomes `--mount type=bind,...,mode=rw`.
- Every read-only entry becomes `--mount type=bind,...,mode=ro`.
- Anything outside the surface is invisible to the container — the
  kernel refuses opens at the `mountns` boundary, not at any agent-
  enforced layer.

Even if the model rewrites its bridge or `exec()`s a fork bomb of
arbitrary code, the OS still won't let it see a path the spawn-
spec didn't list.

## Layer-3 mechanics

### Path expansion

`DefaultToolingPaths` ships templates (`$HOME/.gitconfig`,
`$GOPATH/pkg/mod`, …) so the persona package stays host-agnostic.
At spawn time, `persona.ExpandPaths(s, env)` resolves them against
the host environment:

- `$NAME` and `${NAME}` are replaced from `env`.
- An unset or empty variable **drops the path entirely**, never
  produces a literal `/pkg/mod` mount source.
- `EnvFromOS` snapshots the closed set the templates reference
  (`HOME`, `GOPATH`, `GOROOT`, `USER`).

### Surface → mount translation

`native.SurfaceMounts(s, pathExists)`
(`internal/adapter/native/surface_mounts.go`) emits the backend
mount slice:

- `AllowedWrites` → `mode=rw` mounts (Src == Dst, host layout
  preserved so agent path arguments stay portable).
- `AllowedReads` minus `AllowedWrites` → `mode=ro`.
- Paths whose Src does not exist on the host are reported via the
  returned `skipped` slice so the spawn driver can decide whether
  to log. (A workspace without Cargo doesn't mount `$HOME/.cargo`.)
- Output is sorted by destination path so the docker argv stays
  deterministic.

### Composition with operator mounts

`agents.toml` can declare extra mounts on the `[agent.container]`
stanza. The `surface_mode` field controls how they compose with the
surface-derived set:

| `surface_mode` | Behavior |
|---|---|
| `additive` (default) | operator mounts layer on top of the surface set; conflicts (same `Dst`) prefer the surface entry — operators can't demote a surface `:rw` to `:ro` |
| `exclusive` | operator mounts are dropped entirely (returned through the merge function's `dropped` slice for caller-side warning); only the surface set ships |

Right defaults:

- Local dev rigs use `additive` so an operator's "mount /tmp/scratch
  into the polecat" workflow still works.
- Remote/headless polecats use `exclusive` so no operator-side
  agents.toml change can widen the trust boundary.

### Spawn-driver wiring

The dispatcher / polecat scheduler calls
`persona.ResolveSurface(req)` and attaches the result to
`SessionPrompt.Extension["gemba:surface"]`. The native adaptor's
`buildSpawnSpec` (`internal/adapter/native/start.go`) reads it back,
expands env vars, calls `SurfaceMounts`, and merges with the
operator slice via the configured `surface_mode`. When no surface is
attached (legacy path, terminal backends, pre-surface dispatch
flows), the original operator-only mount behavior is preserved.

## What this layer does NOT cover

- **macOS Apple Silicon container performance** (rosetta vs native).
  Different concern, separate bead.
- **Network namespace / port mapping.** `--network none` is the
  spawn default; richer policy is gm-root.15.8.
- **Read-only rootfs.** Already handled by the existing
  `ReadOnlyRootfs` field; orthogonal threat model.
- **Polecat worktree pool integration.** Currently uses
  `Repository.Path` directly; `worktrees_dir` integration is a
  follow-up (gm-twp2).
- **Image build / agent install.** This bead changes the spawn-time
  mount list only. Image infrastructure stays untouched.

## Follow-ups

- Polecat worktree pool: when `worktrees_dir` is set on the
  Repository, the worktree pool's absolute path replaces the bare
  repo Path in the cwd / writes mount.
- Operator-visible warning surface: today the `dropped` /
  `skipped` slices are returned but the spawn driver doesn't log
  them yet. Wire them into the existing slog stream so an operator
  who mistypes `surface_mode = "exlusive"` can debug from the
  spawn log instead of running tests.
