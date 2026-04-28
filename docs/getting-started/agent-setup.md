# Agent setup

Gemba spawns sessions as panes inside a terminal multiplexer
(tmux / iTerm2 / Terminal.app). Each pane runs a CLI binary the
operator has installed; Gemba doesn't ship any agent runtime itself.
You list the agents you want available in `.gemba/agents.toml` —
one `[[agent]]` stanza per dialect — and the SPA's "Start session"
picker reads that file at boot.

This guide covers the most common agents people drop into Gemba.
For the full schema (including the `[agent.container]` stanza for
sandboxed runs), see
[`internal/adapter/native/agents/registry.go`](https://github.com/MikeBengtson/gemba/blob/main/internal/adapter/native/agents/registry.go).

## The schema in one screen

```toml
# .gemba/agents.toml — workspace-local. Not committed by default;
# each operator's machine can carry a different roster.

[[agent]]
name             = "claude"            # operator-chosen identifier; lower-kebab-case
binary           = "claude"            # exec.LookPath'd on the operator's PATH
args             = []                  # fixed argv prefix (before any caller args)
model            = "claude-opus-4-7"   # passed via --model when the binary accepts it
preamble         = "claude_md"         # how the project + epic + bead context reaches the agent
hooks            = "claude_code"       # which gemba-bridge hook profile gets installed
interaction_mode = "balanced"          # which interaction_profile.md section gets injected
intra_parallel   = true                # may a single session carry multiple beads at once?
max_parallel     = 3                   # if so, what's the per-session cap?
```

| Field | Required | Notes |
|---|---|---|
| `name` | ✓ | Unique within the registry; the SPA's picker label. |
| `binary` | ✓ | Looked up via `exec.LookPath`. Missing binary makes the type unavailable (logged, not fatal). |
| `args` |  | Empty list = no fixed prefix. |
| `model` |  | Many CLIs accept `--model`; some don't. Leave blank to take the agent's own default. |
| `preamble` | ✓ | `claude_md` / `first_message` / `stdout_banner`. See § Preamble below. |
| `hooks` | ✓ | `claude_code` / `prompt_command` / `none`. See § Hooks below. |
| `interaction_mode` |  | `dangerous` / `balanced` / `cautious`. Default `balanced`. |
| `intra_parallel` |  | Default `false` (one bead per session). |
| `max_parallel` |  | Required when `intra_parallel = true`; ignored otherwise. |

## Preamble strategies

How the project + epic + bead context reaches the agent at session
start. The composed markdown is the same regardless of strategy —
only the delivery channel differs.

| Value | Mechanism | When to use |
|---|---|---|
| `claude_md` | Appends a fenced block to `CLAUDE.md`, removed on session end. | Claude Code (it auto-reads `CLAUDE.md`). |
| `first_message` | Sends the preamble as the first user prompt. | Most other agent CLIs (Aider, Codex, Cursor's chat). |
| `stdout_banner` | Prints a markdown banner to the terminal. | Shell-only (no agent — operator reads it). |

## Hook profiles

Hooks let the agent's lifecycle (file edits, prompt submission,
session end) flow back to Gemba so the SPA can render session state
and correlate `bd` mutations to the session that made them.

| Value | Installs | Compatibility |
|---|---|---|
| `claude_code` | `.claude/settings.local.json` Claude Code hook stanza. | Claude Code only. |
| `prompt_command` | A `$PROMPT_COMMAND` shellrc fragment. | Bash/zsh shell sessions. |
| `none` | Nothing — Gemba sees only spawn + exit signals. | Anything else. |

Pick `none` for an agent that doesn't expose a hook surface;
sessions still work, you just don't get the live progress badges.

---

## Per-agent recipes

### Claude Code

The first-class path. Claude Code reads `CLAUDE.md` automatically
and exposes a Hooks API that gemba-bridge plugs straight into.

**Install**: <https://claude.com/claude-code> (`brew install claude-code`).

```toml
[[agent]]
name             = "claude"
binary           = "claude"
args             = []
model            = "claude-opus-4-7"          # or claude-sonnet-4-6, claude-haiku-4-5
preamble         = "claude_md"
hooks            = "claude_code"
intra_parallel   = true
max_parallel     = 3
```

What you get: SessionStart preamble injection, PreToolUse safety
prompts surfaced in the SPA, automatic correlation of every `bd
update` back to the session, live "ready / working / prompting /
stalled" pill, transcript tab.

### OpenAI Codex CLI

The `codex` CLI from `openai/codex` is interactive and accepts a
first-message prompt. No `claude_md`-style file convention, no
hook surface — Gemba sees the session as a black box that emits
spawn + exit, plus whatever the hook profile catches.

**Install**: `npm install -g @openai/codex` (or per their README).

```toml
[[agent]]
name             = "codex"
binary           = "codex"
args             = ["chat", "--no-tty-color"]
model            = "gpt-5"                    # or whatever codex accepts; --model is optional
preamble         = "first_message"
hooks            = "none"
interaction_mode = "balanced"
```

Notes:
- `args = ["chat", ...]` enters Codex's interactive mode.
- `hooks = "none"` because there's no Codex equivalent of Claude
  Code's hook API. The SPA still shows pane state (running /
  exited) but no fine-grained progress.
- Set `OPENAI_API_KEY` in your shell env before launching `gemba
  serve` — child sessions inherit it.

### GitHub Copilot CLI

`gh copilot` is a one-shot suggest/explain command, **not** an
interactive coding agent. Gemba's session model assumes a long-
running pane, so Copilot CLI is a poor fit for autonomous loops.

If you want a Copilot-shaped session anyway (operator drives, agent
suggests inline), the closest workable shape is to wrap it in a
shell:

```toml
[[agent]]
name     = "copilot-shell"
binary   = "zsh"
args     = ["-l"]                              # interactive shell, gh copilot called manually
preamble = "stdout_banner"
hooks    = "prompt_command"
```

The operator runs `gh copilot suggest "..."` inside the pane as
needed; gemba-bridge correlates each command via `$PROMPT_COMMAND`.
For real agent-loop work, use Claude Code, Codex, or Aider instead.

### Aider (any provider — OpenAI, Anthropic, local Ollama, …)

Aider is a strong "use someone else's model" path. It supports
OpenAI, Anthropic, and any OpenAI-compatible endpoint (Ollama,
LiteLLM, OpenRouter), and it has an interactive REPL that takes a
first-message prompt.

**Install**: `pipx install aider-install && aider-install` (or
`pip install aider-chat`).

OpenAI flavor (uses `OPENAI_API_KEY`):

```toml
[[agent]]
name     = "aider-openai"
binary   = "aider"
args     = ["--model", "gpt-5", "--no-auto-commits", "--yes"]
preamble = "first_message"
hooks    = "none"
```

Anthropic flavor (uses `ANTHROPIC_API_KEY`):

```toml
[[agent]]
name     = "aider-anthropic"
binary   = "aider"
args     = ["--model", "anthropic/claude-sonnet-4-6", "--no-auto-commits", "--yes"]
preamble = "first_message"
hooks    = "none"
```

Local-model flavor (Ollama, see § Ollama below):

```toml
[[agent]]
name     = "aider-ollama"
binary   = "aider"
args     = [
  "--model", "ollama_chat/qwen2.5-coder:32b",
  "--no-auto-commits",
  "--yes",
]
preamble = "first_message"
hooks    = "none"
```

Set `OLLAMA_API_BASE=http://127.0.0.1:11434` in your shell env
before launching `gemba serve` so Aider's child process inherits it.

### Ollama (raw `ollama run` — no agent loop)

`ollama run <model>` is a chat REPL — useful for interactive
exploration, but it doesn't edit files or call tools, so it's not a
coding agent in the sense Gemba's dispatcher assumes. If you want
agentic behavior with a local model, drive Ollama through Aider
(above) instead.

For raw inspection sessions:

```toml
[[agent]]
name     = "ollama-chat"
binary   = "ollama"
args     = ["run", "qwen2.5-coder:32b"]
preamble = "first_message"
hooks    = "none"
```

### Plain shell

Always useful — a shell session that gemba-bridge correlates so
your manual `bd` invocations show up in the right session row.

```toml
[[agent]]
name     = "shell-only"
binary   = "zsh"                               # or bash, fish
args     = ["-l"]
preamble = "stdout_banner"
hooks    = "prompt_command"
```

### Cursor / Continue / VS Code

These are IDE-resident agents — Gemba's pane-spawn model can't
host them. Run them in your editor as usual; if you want the work
they do to land in Gemba, have them invoke `bd` from a terminal
inside the IDE.

---

## A complete example

A roster covering the four agents most people end up wanting:

```toml
# .gemba/agents.toml

# Claude Code — the first-class path.
[[agent]]
name             = "claude"
binary           = "claude"
args             = []
model            = "claude-opus-4-7"
preamble         = "claude_md"
hooks            = "claude_code"
intra_parallel   = true
max_parallel     = 3

# OpenAI Codex CLI — for hosted-model OpenAI work.
[[agent]]
name     = "codex"
binary   = "codex"
args     = ["chat", "--no-tty-color"]
model    = "gpt-5"
preamble = "first_message"
hooks    = "none"

# Aider against a local Ollama model — fully offline.
[[agent]]
name     = "aider-ollama"
binary   = "aider"
args     = ["--model", "ollama_chat/qwen2.5-coder:32b", "--no-auto-commits", "--yes"]
preamble = "first_message"
hooks    = "none"

# Plain shell — manual bd work, scripts, pair programming.
[[agent]]
name     = "shell-only"
binary   = "zsh"
args     = ["-l"]
preamble = "stdout_banner"
hooks    = "prompt_command"
```

## Verifying the registry

After editing `.gemba/agents.toml`, restart `gemba serve`. The
banner prints the agents that loaded successfully and any that
were skipped because their `binary` wasn't on PATH:

```
agents: registered claude (binary=/opt/homebrew/bin/claude)
agents: skipping codex — binary not found on PATH
```

Validation errors (duplicate names, unknown `preamble`/`hooks`
values, missing required fields) are reported all at once so you
can fix them in one pass.

## Where the agents.toml lives

- **Default**: `<project-root>/.gemba/agents.toml`.
- **Override**: pass `--agents-registry <path>` to `gemba serve`.
- **Missing**: non-fatal — sessions just can't be spawned until you
  drop one in. The SPA's "Start session" picker shows an empty
  state with a link back to this guide.

## See also

- [Parallelism in Gemba](parallelism) — `intra_parallel` /
  `max_parallel` mechanics.
- [Native adaptor reference](../adaptors/native) — the spawn /
  pane / lifecycle implementation under `internal/adapter/native`.
