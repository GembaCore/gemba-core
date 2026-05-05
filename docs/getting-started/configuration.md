# Configuration reference

Gemba reads from four layers, in this order of precedence (highest
wins):

1. **CLI flags** on `gemba serve`
2. **Environment variables**
3. **Per-workspace config** in `.gemba/` next to the rig
4. **User-level config** at `~/.gemba/config.toml`

A missing file at any layer falls through to the next; if everything
is absent, the built-in defaults take over and `gemba serve --beads-dir <rig>`
just works against any local Beads workspace.

## Layer 1 — CLI flags

`gemba serve` is the surface most operators interact with. Run
`./bin/gemba serve --help` for the live list; the table below covers
every flag the binary accepts today.

### Listening + auth

| Flag | Default | Notes |
|---|---|---|
| `--listen` | `127.0.0.1` | Bind address. Accepts `host` or `host:port`; binding to a non-loopback address requires `--auth`. |
| `--port` | `7666` | TCP port. |
| `--open` | `false` | Open the SPA in a browser after boot. |
| `--auth` | unset | Auth mode. Required when `--listen` is non-loopback. Empty means "no auth, loopback only." |
| `--tls-cert` | unset | Operator-supplied TLS certificate (PEM). Pair with `--tls-key`. |
| `--tls-key` | unset | Operator-supplied TLS key (PEM). |
| `--tls-self-signed` | `false` | Generate an ephemeral ECDSA P-256 cert at boot; print its SHA-256 fingerprint to stderr. Mutually exclusive with `--tls-cert` / `--tls-key`. |

### Data plane (WorkPlane)

Exactly one of `--beads-dir`, `--dolt-url`, or `--noop` must resolve;
the resolution falls through to user-level config when none is passed.

| Flag | Default | Notes |
|---|---|---|
| `--beads-dir` | unset | Path to the Beads workspace; bd subprocesses spawn with this as cwd. |
| `--dolt-url` | unset | `mysql://user[:pass]@host:port/dbname` — direct Dolt SQL connection. Reads and writes are enabled when the Dolt user can write. |
| `--beads-url` | unset | Alias for `--dolt-url`, intended for Beads-only/container-style boot. |
| `--beads-only` | `false` | Run without project or orchestration requirements. Shows Beads management surfaces only. |
| `--beads-read-only` | `false` | Implies `--beads-only` and blocks all Beads mutations. URL mode is not read-only unless this flag is set or the Dolt user itself lacks write permission. |
| `--beads-history` | unset | JSONL session manifest path for Beads-only history. Defaults under the Beads workspace when possible. |
| `--restart` | `false` | Allows `gemba serve` to restart local helper services when a mode needs it. With `--beads-read-only --beads-dir`, Gemba restarts local bd Dolt with `bd --readonly dolt start`; without it, Gemba still blocks mutations at the server/adaptor boundary. |
| `--noop` | `false` | Bind in-memory reference adaptors for both planes. Forces `--orchestration=noop`. Mutually exclusive with `--beads-dir` / `--dolt-url`. |

### Orchestration plane

| Flag | Default | Notes |
|---|---|---|
| `--orchestration` | `native` | `native` (terminal multiplexer), `gastown` (Gas Town Sling/crew/polecat runtime), `mock`, `none` (no plane), or `noop`. |
| `--terminal` | `auto` | Terminal backend when `--orchestration=native`: `auto` \| `tmux` \| `iterm` \| `terminal`. |
| `--agents-registry` | `.gemba/agents.toml` | Agent type registry (see Layer 3). Missing file is non-fatal. |
| `--worktrees-dir` | `<repo>/../worktrees` | Parent dir for native adaptor's worktree provisioner. |
| `--city` / `--town` | unset | Gas City / Gas Town root used as cwd for every `gt` command when `--orchestration=gastown`. Prefer `--city`; `--town` remains for legacy layouts. |

### Other

| Flag | Default | Notes |
|---|---|---|
| `--config` | unset | Explicit path to `gemba.toml` (file loading lands later; flag is stable). |
| `--orchestrator-config` | `.gemba/orchestrator.json` | Path to the shader/orchestrator binding (see Layer 3). |
| `--dangerously-skip-permissions` | `false` | Disable the `X-GEMBA-Confirm` mutation gate for the session. Verbatim Claude Code spelling. |
| `--quiet` | `false` | Suppress non-essential boot output. |

## Layer 2 — Environment variables

Most settings live in flags or files; a small set use environment
variables for credentials and side-channel wiring.

| Variable | Consumer | Notes |
|---|---|---|
| `BEADS_DOLT_PASSWORD` | bd CLI | Password for the Dolt server when bd connects via `--server`. Avoids putting passwords on the command line. |
| `BD_NON_INTERACTIVE` | bd CLI | When set, `bd` skips all interactive prompts. Used by `examples/my-project/load.sh`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | gemba | OTLP receiver URL. Empty disables OTEL trace export. |
| `ANTHROPIC_API_KEY` | Onboarder persona | Optional fallback when `[llm].api_key` is empty in `~/.gemba/config.toml` (consumer's choice; not the loader's). |
| `GEMBA_E2E_BASE_URL` | playwright e2e | Used by the screenshot pipeline + live probes. |
| `GEMBA_E2E_RUN_DEEP` | playwright e2e | Opt-in flag that runs deep-tier specs against a real `gemba serve` + Dolt + bd setup. |
| `GEMBA_MODE=beads_only` | gemba serve | Enables Beads-only mode without passing `--beads-only`. |
| `GEMBA_BEADS_URL` | gemba serve | Beads/Dolt URL used as the WorkPlane source in Beads-only mode. |
| `GEMBA_BEADS_DIR` | gemba serve | Local Beads workspace used as the WorkPlane source in Beads-only mode. |
| `GEMBA_BEADS_READ_ONLY` | gemba serve | Enables explicit Beads-read-only mode (`true`, `1`, `yes`, or `on`). |
| `GEMBA_BEADS_ONLY_MANIFEST` | gemba serve | JSONL manifest path for the RHP Beads history ledger. |

## Layer 3 — Per-workspace `.gemba/`

Files live in `.gemba/` next to the rig (one level above `.beads/`).
All four are optional.

### `.gemba/agents.toml`

Registry of agent types the native OrchestrationPlane can spawn. See
the [Parallelism guide](parallelism) for `intra_parallel` and
`max_parallel` specifically; the full TOML schema is below.

```toml
[[agent]]
name           = "claude"            # required, unique
binary         = "claude"            # required; looked up via $PATH at boot
args           = []                  # fixed argv prefix
model          = "claude-opus-4-7"   # passed as --model where supported
preamble       = "claude_md"         # claude_md | first_message | stdout_banner
hooks          = "claude_code"       # claude_code | prompt_command | none
interaction_mode = "balanced"        # dangerous | balanced | cautious (default: balanced)
interaction_profile = ""             # path override; default: .gemba/interaction_profile.md
intra_parallel = true                # opt in to per-agent parallelism (gm-root.16)
max_parallel   = 3                   # required when intra_parallel = true

[[agent.container]]                  # optional — present means containerized spawn
image          = "claude:latest"
cwd            = "/work"
cpus           = 2.0
memory         = "4g"
network        = "bridge:gemba-net"  # none | host | bridge:<name>
read_only_rootfs = true
unsafe         = false               # required to allow host networking, etc.
```

Validation runs at server startup — typos or out-of-band combinations
(e.g. `intra_parallel = true` without `max_parallel`) fail loudly with
a clear message.

### `.gemba/interaction_profile.md`

Markdown document with named sections (`## dangerous`, `## balanced`,
`## cautious`) defining how each interaction mode injects
question/blocker/escalation guidance into the agent's preamble. The
agent type's `interaction_mode` selects which section gets composed
in. Default lives in `.gemba/interaction_profile.md` next to the
agent registry; per-agent overrides via `interaction_profile = "path"`.

### `.gemba/orchestrator.json`

Shader binding — wraps the WorkPlane adaptor with adaptor-specific
projection logic (e.g. translating native bd ids to Gas Town's
formatted ids).

```json
{
  "orchestrator": "gastown",
  "config": {
    "rig": "myproject",
    "rig_abbr": "mp",
    "id_format": "%s-%s",
    "title_format": "[%s] %s",
    "kind_prefixes": { "epic": "EP", "task": "" }
  }
}
```

A missing file means **no shader** — the WorkPlane's native ids and
titles render as-is. Override the path with `--orchestrator-config`.

### `.gemba/workspace.toml`

Project metadata written by the `/api/v1/newproject/:id/ratify`
bootstrap flow (gm-root.17.6). Operators reading + editing it by hand
is also supported.

```toml
[project]
name = "My Project"
description = "Time-tracking SaaS demo"
created_at = "2026-04-28T10:00:00Z"
tech_stack = ["go", "react", "sqlite"]

[project.architecture]
notes = "Multi-tier; SQLite for MVP, Postgres in beta."

mode = "supervised"   # unsupervised | supervised | managed (gm-e8o)
```

`mode` is read by `gemba serve` at startup and threads through nonce +
audit-trail behavior — `managed` requires extra confirmation for
destructive actions; `unsupervised` lets agents auto-approve. See
[Workspace modes](../design/workspace-modes) for the full contract
(landing as the deep tier specs un-fixme).

## Layer 4 — User-level `~/.gemba/config.toml`

Cross-workspace defaults. Optional — a missing file is the common case.

```toml
[projects]
# Where new projects get bootstrapped. Relative paths resolve against
# $HOME. Defaults to "gemba/projects" (i.e. ~/gemba/projects/).
default_dir = "gemba/projects"

[beads]
# Default Dolt server URL when --dolt-url is not on the command line.
# Format: mysql://user[:pass]@host:port/dbname.
url = "mysql://root@127.0.0.1:3307/gemba"

[llm]
# Shared chat-client credentials for in-process LLM consumers (the
# Onboarder persona today; future agents next).
provider = "anthropic"               # only "anthropic" recognized today
api_key = "sk-ant-…"                 # falls back to $ANTHROPIC_API_KEY when empty
model = "claude-opus-4-7"            # consumer-default when empty
endpoint = ""                        # provider-default when empty
```

Override the path with `--config /alternate/path/config.toml`.

## Layer 5 — Built-in defaults

When everything else is silent:

- Bind: `127.0.0.1:7666`
- Auth: off (loopback-only)
- TLS: off
- WorkPlane: none — `gemba serve` refuses to start unless `--beads-dir`,
  `--dolt-url`, or `--noop` resolves
- OrchestrationPlane: `native` (auto-detects `tmux` → `iterm` → `terminal`)
- Worktrees parent: `<repo>/../worktrees`
- Projects parent: `~/gemba/projects/`
- Beads URL: `mysql://root@127.0.0.1:3307/gemba`

## Adjacent: `.beads/config.yaml`

Owned by the bd CLI, **not** Gemba — but every Gemba workspace runs
on top of one. `bd init` creates it; the most common settings the
operator touches:

| Key | Effect |
|---|---|
| `issue-prefix` | Prefix new bead ids carry (`mp-`, `gm-`, etc.). |
| `events-export` | Export events to `.beads/events.jsonl` on each flush. |
| `output.title-length` | Truncate titles in CLI output to N chars. |
| `actor` | Default audit-trail actor when `$BEADS_ACTOR` and `git user.name` are absent. |

Run `bd config --help` for the full list. Gemba reads from this file
indirectly through bd subprocesses; never write to it.

## Resolving conflicts

A flag set explicitly on the command line **always wins** — there is
no environment variable that can override it, and no file that can
silently flip it. This rule lets operators paste a debug command and
know it does what it says.

Within file layers:

- `.gemba/<file>` wins over `~/.gemba/config.toml` (workspace-specific
  settings beat user-wide ones).
- `~/.gemba/config.toml` wins over the built-in defaults.
- The bd `[server-host]` / `--external` knobs you may have configured
  during `bd init` are read by the bd subprocess directly — Gemba
  doesn't look at them.

## Where next

- [Running Gemba against your work items](running-against-your-work-items) — the basic boot loop.
- [Parallelism in Gemba](parallelism) — agent-type parallelism config in detail.
- [WorkPlane authoring](../adaptors/workplane) — when you want to write a new data-plane adaptor instead of configuring an existing one.
