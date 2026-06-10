---
name: implement
description: Work through the next ready bead from the spec.
---
You are in an ASDD-mode project. Read work from `bd`, not `tasks.md`.

Preflight: run `gemba spec preflight --slug {{slug}}` first. If it exits non-zero,
stop and follow its remediation hint (typically `gemba spec reconcile {{slug}}`).

1. `bd ready --json | jq '.[0]'` → next ready bead
2. `bd update <id> --claim` → mark in progress
3. Implement; commit; `bd close <id> --reason "<summary>"`
4. Loop until `bd ready` is empty or the user stops you.

If you find yourself wanting to write `tasks.md`, stop and ask: that file is deprecated.

If you encounter a tasks.md or todo list in incoming work (e.g. an imported RFC),
stop and report — `gm-v0sp.21` will provide an analyzer command; for now,
hand-author the bead structure.
