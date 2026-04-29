---
title: "Containerized sessions \u2014 architecture + threat model"
decision: gm-nsdl
d: D9
ratified_at: 2026-04-24
---

# Containerized sessions — architecture + threat model

> Reference for epic [gm-root.15]. Every implementation bead in the epic
> references a section of this document. Amend here first; don't let
> implementation drift ahead of the spec.

## 1. Intent

Make containers a first-class session model for `gemba native`, peer to
tmux panes. The operator points gemba at an agent image; gemba dispatches
the container under a security envelope it controls (network, volumes,
secrets, resource caps); then talks to the container with the same verbs
it uses against a tmux pane (Spawn / SendKeys / Capture / Kill / Pause /
Resume / End). MCP keeps working. SSH is supported as a transport for
containers on remote Docker hosts and as an alternative wire protocol
even for local containers.

Two pack-in options ride on top of this:

- `gemba-native` — today's default. Sessions are tmux panes on the
  operator's host. No sandbox beyond what the operator's shell already
  has.
- `gemba-containerized` — sessions are Docker containers with a
  read-only rootfs, dropped capabilities, and no network by default.

Both packs use the same OrchestrationPlaneAdaptor (`internal/adapter/native/`).
Selection is configuration, not a different binary.

## 2. Backend abstraction (gm-root.15.2)

Today `internal/adapter/native/backend.Backend` is written in tmux
vocabulary: `Pane`, `SpawnPane`, `ListPanes`, etc. The refactor:

- Rename `Pane` → `Session` with a `Kind` field (`tmux` | `container` |
  `ssh`). Keep `Pane` as a type alias for one release so the existing
  adaptor code compiles unchanged during the transition.
- `SpawnSpec` grows optional fields for container backends (`Image`,
  `Mounts`, `Network`, `Secrets`, `Limits`). Backends that don't
  understand a field ignore it; the validator at config-load time
  rejects combinations that don't match the selected backend (e.g.
  `Image` without `Kind=container`).
- Backend remains an interface, not an abstract class. Adding a backend
  = new file under `internal/adapter/native/backend/`.

**Why one interface instead of two peer interfaces:** the
OrchestrationPlane only wants to know "give me a thing I can Spawn /
SendKeys / Capture / Kill." Every upstream caller would have to branch
on backend-kind if we split. Tmux-specific quirks (like the
pane-id-is-just-a-string-identifier assumption) already worked their
way out of the interface in practice; this refactor finishes the job.

**What the refactor preserves:** zero behavior change for tmux. Existing
tmux tests pass unmodified. The new file (`backend/docker.go`) is a
peer to `backend/tmux.go`; nothing else moves.

## 3. Transport selection (gm-root.15.3, .4, .5)

Three transports reach a containerized session. Select the right one
per deployment:

| Transport | When | Requires on host | Notes |
| --- | --- | --- | --- |
| Docker local daemon | gemba and docker run on the same host | docker CLI + daemon socket access | Default. Fast. |
| `DOCKER_HOST=ssh://user@vps` | gemba local, containers remote | ssh to remote, remote docker daemon | No gemba on the remote; zero code change in gemba. |
| Pure SSH into container | gemba reaches a container via its own sshd | ssh client, sshd in the image | Unlocks: k8s-managed containers, bastion access, remote hosts where we don't have daemon socket. |

Rules:

1. If the operator sets `DOCKER_HOST`, the Docker backend honors it.
   Bind-mount paths must exist on the remote host; gemba does not
   attempt to rsync workspaces.
2. If the agent type declares `transport = "ssh"`, the SSH backend
   runs. SSH and Docker-exec are interchangeable at the Backend
   interface level; the SessionStore records which one a live
   session is using so reconnects don't split the brain.
3. The SSH transport is NOT a replacement for the Docker backend. The
   Docker backend is the default because the daemon gives us richer
   lifecycle (pause/unpause) and cheaper log tailing. SSH is for when
   we don't have that luxury.

### SSH details

- One keypair per agent **type**, not per session. Kept under
  `~/.gemba/keys/` on the host running gemba, 0600.
- `known_hosts` verified; fingerprint pinning supported via agent
  config. Strict host key checking is mandatory — no `-o
  StrictHostKeyChecking=no` escape hatch from the default config.
- ProxyJump supported for reaching containers behind a bastion.
- Interactive PTY sessions via `ssh -tt`; exec mode for one-shot
  captures.

## 4. Security envelope (gm-root.15.7)

Defaults the Docker backend applies unless overridden by agents.toml:

- `--read-only` (rootfs)
- `--tmpfs /tmp:size=128M,mode=1777`
- `--cap-drop ALL`
- `--security-opt no-new-privileges`
- `--user <uid>:<gid>` matching the host workspace owner
- `--network none`
- `--pids-limit 512`
- `--memory` and `--cpus` from agent config (required fields)
- `--label gemba.session=<id>` (for the reaper, §8)
- `--label gemba.agent=<agent-type-name>`

### Threat model

Scope of the sandbox: defend **the host** from **the agent**.

In scope:

- Agent writes to host filesystem outside declared mounts → blocked by
  read-only rootfs + mount list.
- Agent acquires new capabilities or privileged ops → blocked by
  cap-drop + no-new-privileges.
- Agent exfiltrates data over network → blocked by `--network none`
  default. Operators who opt into egress declare an explicit policy
  (§5).
- Agent reads secrets from env / process listing → blocked by
  mount-based secret injection (§6), never env.
- Agent exhausts host resources → bounded by cpus/memory/pids limits.
- Agent survives a gemba crash and keeps running → reaped on next
  gemba start (§8).

Out of scope:

- Kernel exploits. We don't ship a custom kernel. Operators on shared
  hardware run rootless docker or gVisor; gemba doesn't enforce it.
- Side-channel attacks (Spectre-class, timing). Standard container
  isolation.
- Malicious images themselves. Gemba trusts the image registry the
  operator points at. Image signing is a recommended operator practice,
  not enforced by gemba.
- Coordinated multi-agent compromise across containers. Each container
  is a silo; attacks that span containers require egress, which the
  default policy denies.

### Non-defaults

`--privileged`, `--cap-add SYS_ADMIN`, and `--network host` are
**rejected** by config validation. An operator who needs one of these
must set `unsafe = true` on the container stanza, which is loud in the
banner and logged on every spawn. Precedent: the `--dangerously-skip-permissions`
flag. Copy the pattern.

## 5. Network policy (gm-root.15.8)

Three network modes:

| Mode | Semantics |
| --- | --- |
| `none` | `--network none`. Default. No socket reachable. |
| `bridge:<name>` | `--network <name>`, creates the bridge on first use. Operator may layer iptables egress rules via a declared allowlist; gemba does not write these rules automatically in v1. |
| `host` | `--network host`. Requires `unsafe = true`. |

Named bridges are shared by any agent type that references the same
name, so (e.g.) `bridge:work` lets a pool of agents see one another but
nothing outside the bridge. Bridge lifecycle: created lazily, torn down
on `gemba serve` exit. Bridges that pre-exist on the host are left
alone.

## 6. Secret injection (gm-root.15.9)

Env vars leak to child processes and to anyone with `docker inspect`.
Files on a tmpfs don't. The Docker backend never uses `--env` for
secrets.

- Secrets are declared by **name** in agents.toml (`secrets =
  ["anthropic_api_key"]`).
- Resolver reads secret material from `/etc/gemba/secrets/<name>` (0600)
  or, at the operator's option, from a host keyring (macOS Keychain,
  libsecret).
- Backend mounts resolved secrets into the container's tmpfs at
  `/run/secrets/<name>` with mode 0400.
- The agent's entrypoint reads from `/run/secrets/<name>` exactly as
  Docker Swarm's own `--secret` model does.

A negative test asserts: after spawning a session with a declared
secret, the value does not appear in `docker inspect`, `ps auxe`, or any
gemba log line.

## 7. Workspace volume model (gm-root.15.10)

The native adaptor already provisions a git worktree per session
(`internal/adapter/native/worktrees/`). Containers reuse that
provisioner.

- Worktree path on host: `~/.gemba/worktrees/<session-id>/` (same as
  tmux path today).
- Bind-mount into the container at the agent type's declared `cwd`
  (default `/work`).
- UID alignment: the container runs as the host UID that owns the
  worktree, so files created by the agent are owned by the operator on
  the host. No post-hoc chown. If the image's entrypoint can't run as
  an arbitrary UID (rare — fixable in the image build), the agent type
  can declare `uid_fixup = true` and gemba will chown the worktree on
  provision/teardown.
- Teardown is atomic: EndSession removes the container AND the
  worktree. A failed container teardown leaves the worktree behind
  with a `.gemba-zombie` marker; the reaper (§8) reconciles on next
  start.

Read-only rootfs + writable workspace is the right shape: the agent
writes code to the workspace (intended) and can't write anywhere else
(protected).

## 8. Lifecycle mapping + orphan reaper (gm-root.15.12, .13)

OrchestrationPlaneAdaptor method ↔ docker verb:

| Method | Docker verb |
| --- | --- |
| StartSession | `docker run -d` |
| SendKeys (interactive) | `docker exec -i` |
| SendKeys (one-shot) | `docker exec` with no stdin |
| CapturePane | `docker logs --tail N` |
| PauseSession | `docker pause` |
| ResumeSession | `docker unpause` |
| EndSession | `docker stop` (SIGTERM, 10s grace) then `docker rm` |
| Kill | `docker rm -f` |

Pause/Resume is free on containers — the tmux backend didn't have it;
gemba's OrchestrationPlane interface already exposes those verbs, so
the container backend becomes the first real implementation.

Orphan reaper, on `gemba serve` boot:

1. Enumerate `docker ps -a --filter label=gemba.session --format '{{json .}}'`.
2. Cross-check against the persisted session store.
3. Any container with a gemba.session label but no live session record
   → `docker rm -f`, log with container id + image + age.
4. Reaper is opt-out via `--no-reap` for debugging.

## 9. MCP compatibility (gm-root.15.11)

Two cases:

1. **Stdio MCP servers baked into the agent image.** Claude Code's MCP
   servers typically run this way. Inside the container, nothing
   changes: the agent spawns the MCP server as a child process,
   communicates over stdio, done. Gemba is uninvolved.
2. **Network MCP servers running on the host.** These need the
   container to reach the host. By default, `--network none` blocks
   this. The operator opts in by either:
   - Declaring a named bridge with the host IP + MCP port in the
     allowlist (§5), or
   - Bind-mounting a unix socket from the host into the container
     (`/var/run/mcp/<name>.sock`), bypassing network policy entirely.

The unix-socket path is preferred for security: it's narrower (one
MCP endpoint, not a network policy exception) and leaves the network
still locked down for everything else.

### Bridge-to-gemba traffic

`cmd/gemba-bridge` currently assumes host filesystem access. In a
container, the bridge binary is bind-mounted from the host at
`/opt/gemba/bridge` (read-only), and bridge↔gemba traffic flows over a
unix socket bind-mounted at `/run/gemba/bridge.sock`. This keeps
bridge traffic out of the network policy entirely — the bridge can
always reach gemba, regardless of `--network none`.

## 10. Preamble injection (gm-root.15.14)

All three PreambleStrategy values work in containers:

- `claude_md` → written to a bind-mounted `CLAUDE.md` inside the
  workspace volume. Removed on EndSession.
- `first_message` → `docker exec -i <id> …` pipes the preamble to the
  agent's stdin.
- `stdout_banner` → echoed to the container's TTY before the agent
  starts. Implementation: wrap the entrypoint in a shell that emits
  the banner and execs the agent.

Strategy selection stays per-agent-type in agents.toml, unchanged.

## 11. agents.toml extension (gm-root.15.6)

New optional `container` table on each agent type:

```toml
[[agent]]
name = "claude-code-sandboxed"
binary = "claude"
preamble = "claude_md"
hooks = "claude_code"
interaction_mode = "balanced"

[agent.container]
image = "ghcr.io/mikebengtson/gemba-claude:0.3.1"
cwd = "/work"
cpus = 2.0
memory = "4g"
pids_limit = 512
mounts = [
  { src = "{{workspace}}", dst = "/work", mode = "rw" },
  { src = "/opt/gemba/bridge", dst = "/opt/gemba/bridge", mode = "ro" },
]
secrets = ["anthropic_api_key"]
network = "none"
read_only_rootfs = true
# unsafe = true   # would enable --privileged / host net; rejected otherwise
```

Template expansion (`{{workspace}}`, `{{session_id}}`, `{{agent_name}}`)
happens at spawn time. Validation rejects:

- `network = "host"` without `unsafe = true`
- `image` missing when the selected backend is `docker`
- conflicting mount destinations
- unknown secret names (must resolve at config-load time)

## 12. Pack-ins (gm-root.15.18)

`gemba init` scaffolds a workspace. Three packs:

- `--pack native` — today's default. `.gemba/agents.toml` declares
  tmux-backed agent types. No container stanza.
- `--pack containerized` — all declared agents have `[agent.container]`
  stanzas. Reference Dockerfiles copied into `deploy/containers/`.
  Default images pulled from `ghcr.io/mikebengtson/gemba-*`.
- `--pack gastown` — OrchestrationPlane switches to the Gas Town
  adaptor. Out of scope for this epic; listed for symmetry.

Pack selection is recorded in `.gemba/pack.json` so subsequent
`gemba` invocations know which defaults apply.

## 13. SPA surface (gm-root.15.16)

Session list rows get a backend badge:

```
[tmux]   session-abc   claude-code       cwd=~/work           120m
[docker] session-def   claude-sandboxed  image=gemba-claude   3m
[ssh]    session-ghi   claude-sandboxed  host=vps-01          45m
```

Clicking a row opens a detail panel. For containers, the panel shows
image ref, resolved UID, network policy, cpus/memory caps, created-at,
and the resolved docker inspect (minus secret mounts).

The Spawn dialog picks a backend when the agent type allows more than
one (e.g. an agent type declares both a container spec and a shell
binary, leaving the operator to pick per-session).

## 14. Kubernetes future (gm-root.15.21)

Not in scope for this epic. The backend interface must not assume a
container-id-is-a-string identifier that would break for Pods. Audit
items when the interface change lands:

- `Session.ID` is opaque to callers; internally tmux uses pane id,
  Docker uses container id, Pod backend would use `namespace/name`.
- `SpawnSpec` fields all map to k8s (`image` → container.image,
  `mounts` → Volume/VolumeMount, `limits` → resources.limits, `secrets`
  → Secret mount, `network` → NetworkPolicy). No field assumes Docker.
- Pod lifecycle has a `Pending` state that tmux and Docker don't.
  The interface admits it: Spawn returns a Session, the session emits
  `session.started` (or `session.failed_to_start`) when the scheduler
  places it. Backends that start synchronously fire that event
  immediately.

When the Pod backend lands, it joins `internal/adapter/native/backend/`
as `pod.go`. No further interface churn expected.

## 15. Observability

All container actions emit `GembaEvent`s on the session stream with
payload fields:

- `backend` — `tmux` | `docker` | `ssh`
- `container_id` (when applicable)
- `image` (when applicable)
- `network` policy
- `exit_code` on End

Feeds directly into the existing `session.*` event stream; no schema
churn.

## 16. Release artifacts (gm-root.15.20)

- `ghcr.io/mikebengtson/gemba-bridge:<version>` — bridge binary as a
  scratch image for bind-mounting into agent containers.
- `ghcr.io/mikebengtson/gemba-claude:<version>` — reference Claude
  Code image, operator-forkable.
- `ghcr.io/mikebengtson/gemba-shell:<version>` — minimal shell-only
  sandbox.
- All multi-arch (amd64/arm64), Cosign-signed, SBOM (syft)
  attested — consistent with the gemba binary release flow.

## 17. Open questions

- Rootless docker support. Should work by virtue of only using the CLI;
  we commit to "not actively breaking it" rather than "tested in CI."
- `podman` as a drop-in. Same. The Docker backend shells to `docker`,
  and `alias docker=podman` covers most cases. A future bead could
  add explicit podman detection if the pain emerges.
- Image pull policy. v1 assumes images are pre-pulled by `gemba
  doctor` or by the operator; a `pull_on_start = true` opt-in could
  land later.

## 18. Rollout

1. Design sign-off (this doc).
2. Backend interface refactor (no behavior change).
3. Docker backend + security defaults.
4. agents.toml schema + validation.
5. Workspace mount + lifecycle mapping.
6. SPA surfacing.
7. Pack-in `gemba-containerized` + reference images.
8. Transports: DOCKER_HOST=ssh, then SSH backend.
9. MCP + bridge-in-container hardening.

Each step ends with the corresponding bead closed and the testing/
harness green for all backends.
