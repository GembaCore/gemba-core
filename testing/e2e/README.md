# gemba-e2e

Playwright e2e test library for the Gemba SPA. Two backends, seven
tiers, one project-matrix in `playwright.config.ts`.

> Owning epic: **gm-5v8v** (E2E test library). Scaffold landed by
> **gm-5v8v.1**. Sibling beads fill in the remaining tiers.

## Layout

```
testing/e2e/
  playwright.config.ts        # tier × backend project matrix
  fixtures/
    server.ts                 # backend = 'fake' | 'real' switch
    workplane.ts              # WorkPlane seed fixture (stub → gm-5v8v.5/6)
    auth.ts                   # token + cookie fixture (stub → gm-5v8v.12)
    modes.ts                  # workspace mode fixture (stub → gm-5v8v.11)
    sse.ts                    # SSE emitter (stub → gm-5v8v.10)
  pages/
    AppShell.ts               # POM base — shell assertions
  helpers/
    waits.ts                  # idle waits (small, real)
    dragdrop.ts               # @dnd-kit pointer choreography (stub → gm-5v8v.5)
    hotkeys.ts                # 33-chord helper (stub → gm-5v8v.4)
  specs/
    smoke/                    # gm-5v8v.1   — routes load (this scaffold)
    chrome/                   # gm-5v8v.4   — sidebar/topbar/palette/hotkeys/banner
    board/                    # gm-5v8v.5
    grid/                     # gm-5v8v.6
    graph/                    # gm-5v8v.7
    drawers/                  # gm-5v8v.8
    sessions/                 # gm-5v8v.9
    realtime/                 # gm-5v8v.10
    modes/                    # gm-5v8v.11
    auth/                     # gm-5v8v.12
    error/                    # gm-5v8v.13
    pending/                  # gm-5v8v.14  — living specs for unbuilt UI
    integration/              # gm-5v8v.15  — the migrated hello-world script
```

## Backends

| Backend | What it is | When it runs |
|---|---|---|
| **fake** | `page.route()` intercepts `/api/**` + `/events` with empty-state JSON and an idle SSE stream. Sub-second resets. Parallelizes freely. No Go binary, no Dolt. | Every PR. |
| **deep** | Real `gemba serve` + Dolt + bd. Worker isolation via Dolt-DB + worktree namespacing. Serialized to ≤2 workers. | Merge / nightly. Implementation in **gm-5v8v.2**. |

The fake backend is the current default. Set `GEMBA_E2E_BACKEND=real`
once gm-5v8v.2 lands.

## Tiers and projects

`playwright.config.ts` enumerates the full matrix up-front. Most projects
`testIgnore: ['**/*']` until their child bead replaces the entry. This
keeps the matrix visible without false-greens.

| Project | Tier | Backend | Status |
|---|---|---|---|
| `smoke-fake` | smoke | fake | **active** (this scaffold) |
| `chrome-fake` / `route-fake` / `realtime-fake` / `modes-fake` / `error-fake` | chrome / route / realtime / modes / error | fake | pending |
| `smoke-deep` / `chrome-deep` / `route-deep` / `realtime-deep` / `modes-deep` / `error-deep` / `integration-deep` | full taxonomy | real | pending |

## Tag convention

Tags are **specifically additive** — Playwright `--grep`s on them.

| Tag | Meaning |
|---|---|
| `@smoke` | Routes load + AppShell renders. Zero backend assertions. |
| `@chrome` | Sidebar / Topbar / palette / hotkeys / adaptor banner. |
| `@drag` | Exercises drag-drop. Requires the dragdrop helper. |
| `@realtime` | Asserts SSE → react-query invalidation. |
| `@auth` | Exercises login / middleware enforcement. |
| `@mode-managed` | Asserts the managed-mode confirmation UX. |
| `@deep` | **Asserts on real backend behaviour** — writes, SSE round-trips, Dolt reads. The deep-* projects grep this. Spec is *not* tagged `@deep` if it only renders the SPA shell. |

## Running

From the workspace root:

```bash
pnpm install                       # one-time; pulls @playwright/test
pnpm --filter gemba-e2e exec playwright install chromium  # one-time
pnpm --filter gemba-e2e test       # smoke-fake only (the DoD)
pnpm --filter gemba-e2e test:all   # full matrix (most projects ignore everything for now)
pnpm --filter gemba-e2e typecheck
```

From this directory:

```bash
pnpm test                          # smoke-fake
pnpm test:smoke                    # alias of test
pnpm test -- --grep "/board"       # single-route smoke
pnpm exec playwright test --ui     # UI mode
pnpm report                        # open last HTML report
```

### Pointing at an already-running dev server

```bash
GEMBA_E2E_NO_WEBSERVER=1 GEMBA_E2E_BASE_URL=http://localhost:5173 pnpm test
```

## Selector strategy

Prefer existing `data-testid` attributes — the SPA already ships ~179
of them. ARIA roles + accessible names are the second choice. CSS
class selectors are forbidden; they churn with Tailwind.

## Adding a spec

1. Find the right tier directory under `specs/`.
2. Import `{ test, expect }` from `../../fixtures/server`.
3. Tag the test (`@chrome`, `@drag`, `@deep`, …).
4. Use `pages/AppShell.ts` as a base for any route POM you need.
5. If it asserts on backend behaviour, add `@deep` so the deep
   matrix grep filter picks it up.

## Out of scope for gm-5v8v.1

Real-backend fixture, per-route specs, CI lanes, hello-world
migration — all owned by sibling beads under gm-5v8v.
