# Running Gemba against your work items

Gemba is a single-binary Go service that reads a WorkPlane (a work
tracker) and renders a Kanban board in your browser. The only hard
requirement is a data plane; **beads** (`bd`) fulfills that out of
the box, and the work items themselves are bd issues. You can point
Gemba at a beads rig two ways:

- **Mode A — `--beads-dir`**: Gemba shells out to the `bd` CLI for
  every read. Slower but portable; works anywhere you can run `bd`.
- **Mode B — `--dolt-url`**: Gemba opens a direct read-only SQL
  connection to your Dolt server. Faster and no `bd` dependency, but
  needs a reachable Dolt instance.

Exactly one of the two must be passed (`gm-98l` enforces this at
startup).

Agent sessions (optional). Add `--orchestration=native` to the
commands below to light up `/sessions` in the SPA with live tmux /
iTerm2 / Terminal.app-backed dispatch — no external daemon required.
See [`docs/adaptors/native.md`](../adaptors/native.md) for the full
native-orchestration story. Gas Town / Gas City / LangGraph / etc.
adaptors swap in with `--orchestration=<name>` when you want their
specific scheduling semantics.

## Prerequisites

- `gemba` binary on `PATH` — build from source with `make build` and the binary lands at `./bin/gemba`.
- **For Mode A**: [`bd`](https://github.com/steveklabnik/beads) ≥ a version that emits M1-compatible JSON, and a beads rig at `<path>` (a directory containing `.beads/<name>.db` or a rig root whose `.beads/` subdirectory holds the database).
- **For Mode B**: a running Dolt server reachable via `mysql://…`. The default Gas Town Dolt runs on `:3307` and serves every rig's beads database off the same port; see `gt dolt status`.

## Mode A — `--beads-dir` (bd CLI adaptor)

```bash
gemba serve --beads-dir ~/gt/gemba --auth none
# equivalent (the resolver accepts the .beads subdir too):
gemba serve --beads-dir ~/gt/gemba/.beads --auth none
```

`--auth none` is the default and only needs to be spelled out when binding to a non-loopback interface. On a loopback bind (the default `127.0.0.1:7666`) you can omit it.

### What you should see

Startup banner on stderr:

```
▶ gemba <version>  listen=127.0.0.1:7666  auth=none
▶ workplane: beads <ver> (mode=bd-cli dir=/Users/you/gt/gemba)
▶ manifest: 5 states, 3 core + <n> extension edges, feature flags: sprint=no budget=no
```

Then open <http://127.0.0.1:7666/board> — you should land on a five-column board (Backlog / Unstarted / Started / Completed / Canceled) populated with every bead from the rig.

## Mode B — `--dolt-url` (direct Dolt SQL)

```bash
gemba serve --dolt-url 'mysql://root@127.0.0.1:3307/gemba' --auth none
# with credentials:
gemba serve --dolt-url 'mysql://gemba:$DOLT_PW@127.0.0.1:3307/gemba' --auth none
```

The dbname is the snake_case rig name — e.g. `gemba`, `lume_spark_api`, `second_brain`. Pick the database that holds the rig you want to view.

### What you should see

```
▶ gemba <version>  listen=127.0.0.1:7666  auth=none
▶ workplane: beads <ver> (mode=dolt-sql url=mysql://root@127.0.0.1:3307/gemba)
▶ manifest: 5 states, 3 core + <n> extension edges, feature flags: sprint=no budget=no
```

Any password in the URL is redacted from the banner and logs. Mode B is read-only — mutations will surface as `read_only` adaptor errors on the API side; that's by design until a Dolt-write path lands.

## Troubleshooting

**`--beads-dir ...: no .beads/ directory found`** — the path exists but isn't a rig root. Point `--beads-dir` at a directory whose `.beads/` subdirectory contains `<name>.db`, or at the `.beads/` directory itself.

**`--beads-dir and --dolt-url are mutually exclusive`** — pass exactly one. The two modes select different adaptor implementations; Gemba doesn't mix them.

**Board shows a "degraded" banner** — the adaptor is reachable but returned an `adaptor_degraded` error. For Mode A this is usually `bd` missing from `PATH` or the rig being mid-`bd dolt pull`; for Mode B it means the Dolt server answered but the read failed (check `gt dolt status`).

**Board shows "No beads yet"** — the adaptor works but the rig is empty. Run `bd create` or point at a different rig.

**`connection refused` during Mode B startup** — Dolt isn't running on the host/port in your URL. Start it with `gt dolt start`, then retry.

**Non-loopback bind rejected** — `--listen 0.0.0.0` requires `--auth token` or `--auth oidc` (loopback-only is the default safety posture). See `gemba serve --help`.

## Where next

- The full **[Configuration reference](configuration)** covers every
  flag, environment variable, and config file Gemba reads, with the
  order of precedence and the schema for each `.gemba/*` file.
- **[Parallelism in Gemba](parallelism)** is the next thing operators
  reach for once the basics work.
