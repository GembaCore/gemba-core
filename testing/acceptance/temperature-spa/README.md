# Acceptance test: temperature-spa

End-to-end test that bootstraps a fresh Gemba project, imports a milestone/epic/bead JSONL pack, drives the SPA via Playwright, lets agents complete the work autonomously, and verifies the resulting target SPA renders a correct Celsius/Fahrenheit table.

**Epic:** `gm-root.27`
**Decision:** D15 (`gm-1avi`)
**Design doc:** [`docs/design/acceptance-temperature-spa.md`](../../../docs/design/acceptance-temperature-spa.md)

## Layout

```
shared/                        # variant-agnostic core
  helpers/                     # bootstrap, pool-via-ui, escalation, cleanup, report, bug-filer
  target-jsonl/                # M1/M2/M3 bead packs
  spec.ts                      # the Playwright orchestration body (lands in gm-root.27.6)
variants/
  native/                      # CI-default wrapper; currently boots --orchestration=mock
  gastown/                     # --orchestration=gastown (manual / nightly)
reports/                       # historical run reports
```

## Tooling

- **Type-checking:** files under `shared/` and `variants/` are included in `testing/e2e/tsconfig.json`. Run `pnpm exec tsc --noEmit` from `testing/e2e/`.
- **Test runner:** Playwright. Specs live under `variants/<variant>/`; run via `pnpm exec playwright test --config=variants/<variant>/playwright.config.ts` (lands in gm-root.27.12 / gm-root.27.15).
- **Path aliases:** `@e2e/*` (testing/e2e), `@acc/*` (this dir).

## Running

CI default (mock-backed native wrapper):

```sh
pnpm --filter gemba-acceptance-temperature-spa test:native
```

Real-agent opt-in:

```sh
GEMBA_ACCEPTANCE_REAL_AGENTS=1 pnpm --filter gemba-acceptance-temperature-spa test:native
```

Real-agent demo capture:

```sh
pnpm --filter gemba-acceptance-temperature-spa test:demo:real
```

Claude-agent demo capture:

```sh
pnpm --filter gemba-acceptance-temperature-spa test:demo:claude
```

As of the gm-root.28 mock-adaptor reconciliation, the default native
wrapper intentionally starts `gemba serve --orchestration=mock` so CI
can exercise dispatch, triage, build, serve, and oracle behavior
without API credentials. The real-agent path can use the native
Codex driver (`preamble = "codex_exec"`): auto-dispatch cold-starts a
fresh pool session when no idle worker exists, and the driver emits
`gemba-state` lifecycle frames plus `bead-done` after `codex exec`
completes.
Set `GEMBA_ACCEPTANCE_AGENT=claude` to run the same native real-agent
demo through Claude Code instead of Codex.

Demo mode records `.webm`, emits a narration JSON timeline, and captures
key screenshots in the Playwright output directory (board inspection,
graph/dependency inspection, Gemba walk, escalation triage, milestone
launches, and the final temperature table). Publishable `.mp4` should
be generated after the run, for example:

```sh
ffmpeg -i input.webm -vf "setpts='if(lt(T,10),PTS,PTS/8)'" -an output.mp4
```

The narration JSON is a second artifact aligned to the edited timeline.
Keep long code-generation spans compressed or removed, then regenerate /
adjust `at_ms` values for the final MP4. Current demo guidance targets
about two minutes so the recording can show the major surfaces: dark
mode boot, board milestone inspection, RHP details, graph navigation,
the first drag that starts M1, session status, cascading epic/bead
progress, Gemba walk, escalation triage, evidence, and the M1/M2/M3
SPA launches.

Gastown variant (opt-in; requires gt CLI configured):

```sh
GEMBA_ACCEPTANCE_RUN_GASTOWN=1 pnpm exec playwright test --config=variants/gastown/playwright.config.ts
```

## Implementation status

See `gm-root.27` for the per-bead breakdown. Each child bead's description references this README + the design doc + the relevant sub-decision.
