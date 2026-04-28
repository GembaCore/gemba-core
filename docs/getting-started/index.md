# Getting Started

Operator-facing guides for running Gemba against your own work
items, configuring agent runtimes, and tuning parallelism.

## Pages in this section

- **[Running Gemba against your work items](running-against-your-work-items)** — the
  fastest path from `make build` to a Kanban in the browser.
  Covers both `--beads-dir` (CLI) and `--dolt-url` (direct-SQL) modes,
  expected banner output, and troubleshooting.
- **[Configuration reference](configuration)** — every CLI flag,
  environment variable, and config file Gemba reads, with the order
  of precedence (CLI > env > `.gemba/<file>` > `~/.gemba/config.toml`
  > built-in defaults).
- **[Parallelism in Gemba](parallelism)** — declare per-agent
  parallelism in `.gemba/agents.toml`, understand intra- vs
  inter-session axes, the dispatcher's try-reuse-before-spawn
  policy, and the SPA's pill / global-counter surfaces.

## Where next

- Adaptor authoring: see [Adaptors](../adaptors/).
- Architectural decisions: see [Design](../design/).
- Per-role agent operating docs: see [Agents](../agents/).
