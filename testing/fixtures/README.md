# Test fixtures

## `project-canonical.jsonl`

Canonical project fixture for the board-pane regression test (`gm-root.2`).
29 WorkItems covering the coverage matrix in the bead DoD: mixed
state_categories, P0–P3 priorities, label namespaces, parent-child tree
depth ≥ 3, blocks chain depth ≥ 4, cross-prefix relations, evidence,
custom `beads:*` fields, sprint membership, close_reason, long markdown
description, top-level orphan swimlane, DoD attached.

Shape: each line is a gemba `core.WorkItem` serialized as JSON (see
`web/src/types/core.gen.ts`). The shape matches what `GET
/api/work-items` returns, so the fixture doubles as a deterministic
React Query preload for the vitest regression run (`web/src/pages/__tests__/BoardRegression.test.tsx`).

### Refresh policy

Edit the JSONL file directly. No snapshot regeneration is needed — the
regression test makes explicit DOM-shape assertions, not
`toMatchSnapshot` comparisons. When a UI change is intentional, update
the assertions in `BoardRegression.test.tsx` to match the new shape;
update the fixture only when the coverage axes themselves need to shift.

### Future: Go loader

`testing/fixtures/load.go` (not yet implemented — tracked in a follow-up
of `gm-root.2`) will convert this JSONL into `bd` native format and
import it into an ephemeral `.beads/` directory so end-to-end tests can
drive a real `gemba serve`. For now the vitest path consumes the JSONL
directly.

## `m1-smoke/`

Bead seed data for `scripts/m1-smoke.sh` — see its README.
