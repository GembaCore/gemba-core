# Workspace Data Layout

> Implementation: `internal/server/workspacelayout` (gm-o9t8.1.2.4).
> Materialized on ratify (`POST /api/v1/newproject/{id}/ratify`).

Every Gemba workspace owns a single, self-contained directory tree under
the gemba-server's data root:

```
<DataRoot>/
└── workspaces/
    └── <workspace-id>/
        ├── repo/          # operator's working tree (clone / git-init target)
        ├── dolt/          # per-workspace Dolt data (RESERVED — see "M1 deviation" below)
        ├── logs/          # server-side runtime logs (ephemeral)
        └── transcripts/   # agent dispatch transcripts (NDJSON event logs)
```

The `<workspace-id>` is the project name. It is validated against the
same character class as bd prefixes: letters, digits, `-`, `_`, `.`.

## Permission model

| Path | Mode | Owner | Notes |
|------|------|-------|-------|
| `<DataRoot>` | 0750 | gemba-server process user | Created on first ratify if missing. |
| `<DataRoot>/workspaces/` | 0750 | gemba-server process user | |
| `<DataRoot>/workspaces/<id>/` | 0750 | gemba-server process user | The workspace root. |
| `<id>/repo/` | 0750 | gemba-server process user | Files inside follow git's defaults. |
| `<id>/dolt/` | 0750 | gemba-server process user | Reserved for M3 per-workspace Dolt. |
| `<id>/logs/` | 0750 | gemba-server process user | Server-side logs only. |
| `<id>/transcripts/` | 0750 | gemba-server process user | Append-only NDJSON event logs. |
| Files (any) | 0640 | gemba-server process user | Convention; writers enforce. |

The package exports `DirPerm` (0750) and `FilePerm` (0640) so log
writers, transcript writers, and any future producers share a single
source of truth. Directory creation is explicit: `EnsureLayout` calls
`os.MkdirAll` followed by `os.Chmod` on each step so a relaxed process
umask cannot widen the bits.

### Owners

- **Server**: writes `logs/`, `transcripts/`, and (M3) `dolt/`. Reads
  `repo/` for git operations.
- **Agents**: write transcripts via the server-side dispatch path. They
  never touch the on-disk layout directly.
- **User / operator**: owns `repo/` (any local modifications, commits,
  branches). The server never rewrites operator-authored files in
  `repo/`.

## M1 deviation: shared Dolt data dir

Per `gm-o9t8.1.7`, the embedded Dolt supervisor runs a **single shared
data directory** per gemba-server (default: `./data/dolt/`,
configurable via `--dolt-data-dir`). M1 does NOT use the per-workspace
`<id>/dolt/` subdir — it is reserved as a forward-compatible
placeholder so the layout contract stays stable.

The placeholder is still created at ratify time so that:

1. Filesystem backup tooling that snapshots `<DataRoot>/workspaces/`
   sees a consistent four-subdir shape per workspace.
2. M3 migration is a data-move operation (copy from shared dir into
   each workspace's `dolt/`), not a layout change.

Plan: M3 will switch embedded Dolt to one server-per-workspace and
point each instance at its own `<id>/dolt/` directory. The layout
contract here does not need to change at that point.

## Backup considerations

- **Critical (back up):**
  - `<id>/dolt/` once M3 lands (today: back up the shared `./data/dolt/`).
  - `<id>/transcripts/` — the audit trail of agent activity, not
    reproducible from any other source.
- **Reconstructible (skip if storage is tight):**
  - `<id>/repo/` — re-clonable from the workspace's git remote. If the
    operator has uncommitted work the operator is responsible for
    pushing.
- **Ephemeral (do NOT back up):**
  - `<id>/logs/` — rotated and pruned on a server-defined cadence;
    historical entries have no operational value after rotation.

## Ratify behaviour

The layout step runs immediately after `.gemba/workspace.toml` is
written. Failure mode: **best-effort**. A layout-creation failure is
surfaced via `SeedWarnings` on the ratify response (prefix:
`workspace data layout:`) but does NOT roll back the project working
tree. The rationale: the project tree (`workspace.toml` + `git` +
beads DB) is fully usable without the server-side data dir, and
`/api/readyz` reports the inconsistency for operators to remediate.

When `RatifierConfig.DataRoot` is empty (legacy serve wirings, tests
that don't care about the data tree) the layout step is skipped
silently — no warning, no error.

## Programmatic access

```go
import "github.com/GembaCore/gemba-core/internal/server/workspacelayout"

paths, err := workspacelayout.EnsureLayout("alpha", "/var/lib/gemba")
// paths.Repo        = "/var/lib/gemba/workspaces/alpha/repo"
// paths.Dolt        = "/var/lib/gemba/workspaces/alpha/dolt"
// paths.Logs        = "/var/lib/gemba/workspaces/alpha/logs"
// paths.Transcripts = "/var/lib/gemba/workspaces/alpha/transcripts"
```

`Resolve(workspaceID, root)` is the pure (no-disk) variant; both
reject empty IDs, relative roots, and IDs containing path separators
or whitespace.
