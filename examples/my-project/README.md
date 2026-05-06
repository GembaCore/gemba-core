# My Project — sample beads rig

A reproducible "two-or-three tier app dev project" rig used for the
README screenshots and for anyone who wants to see what Gemba looks
like populated without wiring their own work tracker first.

## What's in here

- **`seed.json`** — a `bd create --graph` plan describing one project
  with three milestones (MVP, Beta Release, 1.0 Launch), six epics
  spanning frontend / backend / data / auth / deploy / billing tiers,
  and ~30 work items with realistic dependency edges and a clear longest
  path so critical-path mode is non-trivial.
- **`load.sh`** — bootstrap a scratch rig at `/tmp/my-project-rig`,
  apply the seed graph, and progress a slice through the workflow so
  the SPA's Board view shows cards in multiple columns.

## Prerequisites

A running Dolt server on `127.0.0.1:3307` (the gt-town convention).
`load.sh` registers the rig database with this server via `bd init
--server --external` rather than spinning up its own embedded Dolt
process — important on dev boxes where the embedded mode would
port-conflict with the town server.

If you don't have the gt setup, `gt dolt start` should bring one up.

## Run it

```bash
# Build gemba once.
make build

# Load the sample (idempotent — it drops + recreates the 'mp' db).
examples/my-project/load.sh

# Boot Gemba against the rig.
./bin/gemba serve --project-dir /tmp/my-project-rig --port 7666

# Open in a browser.
open http://127.0.0.1:7666
```

## Regenerate the README screenshots

```bash
examples/my-project/load.sh
scripts/generate-readme-screenshots.sh
```

This boots gemba on `:17890`, runs a playwright spec that sets dark
mode via localStorage, and writes:

- `docs/img/screenshot-board.png`
- `docs/img/screenshot-graph.png`
- `docs/img/screenshot-walk.png`

The spec lives at `testing/e2e/specs/screenshots/readme.spec.ts`.

## Customising

- **Different prefix**: `examples/my-project/load.sh /tmp/my-project-rig acme`
  swaps `mp-XXXX` ids for `acme-XXXX`.
- **Different target**: `examples/my-project/load.sh /path/to/rig` puts
  the rig anywhere you want.
- **Edit the seed**: it's plain JSON. Add a node, add an edge, re-run
  `load.sh` — re-runs are idempotent thanks to a `gt dolt cleanup
  --force` step that drops the prior `mp` database before re-init.

## What the seed represents

A fictional time-tracking SaaS:

| Tier | Epic | Items |
|------|------|-------|
| Frontend | Frontend SPA | Vite scaffold, router + AppShell, auth-guarded routes, time-entry form, daily report, design tokens |
| Backend | Backend API | Go server skeleton, chi router, /api/entries, /api/reports, validation middleware |
| Data | Data Layer | SQLite schema, migration runner, repos, pool config |
| Auth | Auth & Accounts | Signup, login, password reset, JWT, per-user isolation |
| Ops | Deploy & Ops | Multi-stage Dockerfile, GitHub Actions CI, Postgres swap, healthcheck, structured logs |
| Billing | Billing & Plans | Stripe setup, plan UI, subscription webhook, upgrade/downgrade, invoice email |

Filed as **gm-57p6**.
