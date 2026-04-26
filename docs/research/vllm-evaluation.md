# vLLM evaluation for the gemba container backend (gm-teye)

**Status:** Recommendation
**Date:** 2026-04-26
**Decider:** mike (operator)
**Author:** crew/mike3

## Question

Can [vLLM](https://github.com/vllm-project/vllm)'s published Docker images
serve as the agent-runtime image for the gemba containerized session
backend (gm-root.15), letting us skip the Dockerfile build work pinned in
gm-root.15.18 (`deploy/containers/claude-code.Dockerfile`,
`shell-base.Dockerfile`, `gemini.Dockerfile`)?

## Short answer

**Pass for the agent-runtime use case. Defer for the sidecar use case.**

vLLM is the wrong abstraction for the per-session agent container. It is
an LLM **inference server** (Apache-2.0, OpenAI-compatible HTTP, model
weights served on port 8000), not an **agent runtime**. The vllm-openai
images do not include `claude` / `gemini` / `codex` / shell-coding-agent
binaries. Pointing gemba's `[agent.container]` stanza at `vllm/vllm-openai`
would launch a 10.5 GB inference server that gemba then has nothing to
talk to — gemba's session driver expects an interactive CLI agent on the
other end of `docker exec`, not an HTTP endpoint.

The "skip building containers" goal is correct, but vLLM is not the lever
that reaches it. The lever is **Anthropic's published reference
devcontainer Dockerfile**: see *Alternative simplification* below.

## What vLLM actually is

| Property | Value |
|---|---|
| Project | https://github.com/vllm-project/vllm |
| License | Apache-2.0 |
| Role | LLM inference server (OpenAI-compatible HTTP API) |
| GPU image | `vllm/vllm-openai:latest` (~10.5 GB, requires NVIDIA CUDA + GPU) |
| CPU image | `vllm/vllm-openai-cpu:latest-x86_64` and `:latest-arm64` |
| Default port | 8000 |
| Default entrypoint | `python -m vllm.entrypoints.openai.api_server` |
| Required env | `HF_TOKEN` (for gated HF models); `--model <name>` arg |
| Required mounts | `~/.cache/huggingface` for the model cache (10s of GB per model) |
| What it serves | `POST /v1/chat/completions`, `POST /v1/completions`, etc. |
| What it does NOT include | agent CLIs (claude, gemini, codex), shell, git, npm, dev tooling |

vLLM is a peer of Ollama, llama.cpp's server, and TGI — server-side model
hosting. It does not run agents; it runs models that agents call.

## Why this is the wrong fit for gm-root.15.18

gm-root.15.18 commits to "sandboxed claude-code and shell sessions". The
session driver (gm-root.15.3) shells out to `docker exec -i` against a
running container and expects a process on the other end that:

1. Reads instructions from stdin (or files in a bind-mounted workspace)
2. Writes structured output to a known location the bridge tails
3. Calls MCP tools over stdio
4. Speaks to a model provider over HTTPS (Anthropic API for
   claude-code, etc.)

vLLM containers do none of these. They expose port 8000 and wait for
HTTP requests. There is nothing inside them to `docker exec` into for an
agent task.

## Where vLLM legitimately fits — defer to a follow-up bead

Two real but premature use cases:

1. **Self-hosted model behind Claude Code / Codex.** An operator could
   stand up a vLLM container next to the agent container, point a vLLM-
   compatible client (Codex, Continue, Aider, …) at it, and use it as
   a drop-in for Anthropic API. claude-code currently does NOT support
   custom OpenAI-compatible endpoints, so this only helps non-Anthropic
   agents. Track when those agents enter the pack: gm-root.15.18
   currently scopes to claude-code + shell only.

2. **Sidecar inference for gemba's own internal models.** The
   source-analysis layer (gm-s47n.7 concept vocabulary, gm-s47n.8
   retrospective) and the persona-scoring layer
   (potentially gm-twp2 helpers) might want a small embedding /
   classification model. vLLM is a reasonable home for that helper
   model. This becomes worth filing only after the first concrete
   feature in that direction is committed; today it's pure speculation.

Both belong on a follow-up "self-hosted-model integration" bead, not as
amendments to the per-session agent container story.

## Alternative simplification — actually skip building Dockerfiles

Anthropic ships a [reference devcontainer
Dockerfile](https://github.com/anthropics/claude-code/blob/main/.devcontainer/Dockerfile)
in the claude-code repo (Node.js 20 base, claude-code preinstalled, git
+ ZSH + dev tooling, init-firewall.sh that whitelists Anthropic API +
npm + GitHub). It's not published as an OCI image, but it solves the
same problem gm-root.15.18 was going to solve from scratch:

- Sandboxed claude-code session
- Default-deny firewall with explicit whitelist (matches gm-root.15.8
  named-bridge intent)
- `--dangerously-skip-permissions` is safe inside the firewall
- ANTHROPIC_API_KEY (or OAuth token volume) is the only secret needed

We can lift this Dockerfile (Apache-2 / MIT-licensed, check repo) into
`deploy/containers/claude-code.Dockerfile` rather than authoring a fresh
one. The gm-root.15.18 task description's intent — "fresh clone → gemba
init --pack containerized → working sandboxed claude-code in 3 commands"
— stays. The work-effort drops because we vendor a known-good base
instead of designing a security envelope from scratch.

Same approach is viable for `gemini.Dockerfile`: Google publishes a
[gemini-cli devcontainer reference](https://github.com/google-gemini/gemini-cli)
worth checking for similar reusability.

## Concrete shape if we ship containerized claude-code today

Required for a `[agent.container]` stanza pointed at the Anthropic-style
image (NOT the vLLM image — including this for the user's question):

```toml
[agent.container]
image          = "ghcr.io/<org>/gemba-claude-code:0.x"  # built from vendored Dockerfile
command        = ["claude", "--dangerously-skip-permissions"]
working_dir    = "/workspace"

[agent.container.env]
ANTHROPIC_API_KEY = "${secret:anthropic-api-key}"   # tmpfs-mounted secret (gm-root.15.9)

[[agent.container.mounts]]
source = "{{workspace}}"     # the per-session worktree
target = "/workspace"
type   = "bind"
rw     = true

[[agent.container.mounts]]
source = "{{session_id}}-tmp"   # ephemeral writable scratch
target = "/tmp"
type   = "tmpfs"
rw     = true

[agent.container.network]
mode      = "named-bridge"     # gm-root.15.8 — egress firewall scoped to allowlist
allowlist = ["api.anthropic.com:443", "registry.npmjs.org:443", "github.com:443"]

[agent.container.limits]
cpus           = 2.0
memory         = "4Gi"
pids           = 256
read_only_root = true
cap_drop       = ["ALL"]
no_new_privs   = true
userns         = "remap"
```

If the operator does want a vLLM sidecar (for some non-claude agent
in the pack), the second container is a peer in the same docker network
with NO bind-mount of the workspace and NO ANTHROPIC_API_KEY:

```toml
[[sidecars]]
image       = "vllm/vllm-openai:latest"
command     = ["--model", "meta-llama/Meta-Llama-3-8B-Instruct"]
ports       = ["8000"]
env         = { HF_TOKEN = "${secret:hf-token}" }
mounts      = [
  { source = "hf-cache",     target = "/root/.cache/huggingface", type = "volume", rw = true },
]
network     = "named-bridge"   # peer of agent container; no external egress
gpu         = "all"            # requires nvidia-container-toolkit on host
```

Note: `[[sidecars]]` is illustrative — gm-root.15 does not yet have a
sidecar concept. Filing one is a prerequisite for the deferred
inference-target use case.

## Recommendation

1. **Pass on vLLM as the agent-runtime image.** Wrong primitive.
2. **Adjust gm-root.15.18 scope** to vendor / lightly adapt Anthropic's
   reference devcontainer Dockerfile rather than author a fresh
   `claude-code.Dockerfile`. Same DoD; smaller work effort; better
   security posture (we inherit Anthropic's firewall ruleset).
3. **File a follow-up** under gm-root.15 for "self-hosted-model
   integration via vLLM sidecar" — gated on (a) a concrete agent in the
   pack that supports OpenAI-compatible endpoints, or (b) a gemba
   helper-model feature shipping. Do not ship until then; sidecar work
   on speculation accumulates dead config surface.

## What does NOT change

- gm-root.15 architecture: container backend stays a peer of the tmux
  backend; vLLM doesn't displace anything.
- gm-root.15.7 security envelope (read-only rootfs, cap-drop ALL,
  userns, --network none default): vendoring Anthropic's Dockerfile
  reinforces this; the firewall script + named-bridge override
  composes with the vendored image.
- gm-root.15.18's "fresh clone → 3 commands" DoD: still the goal.
