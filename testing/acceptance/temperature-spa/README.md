# Acceptance test: temperature-spa

End-to-end test that bootstraps a fresh Gemba project, imports a milestone/epic/bead JSONL pack, drives the SPA via Playwright, lets agents complete the work autonomously, and verifies the resulting target SPA renders a correct Celsius/Fahrenheit table.

**Epic:** `gm-root.27`
**Decision:** D15 (`gm-1avi`)
**Design doc:** [`docs/design/acceptance-temperature-spa.md`](../../../docs/design/acceptance-temperature-spa.md)

## Layout

```
shared/                        # variant-agnostic core
  helpers/                     # bootstrap, pool-via-ui, escalation, cleanup, report, bug-filer
  runner/                      # AgentRunnerFactory: mock vs real-claude
  oracle/                      # M1/M2/M3 + FTUX assertions
  target-jsonl/                # M1/M2/M3 bead packs
  spec.ts                      # the Playwright orchestration body (lands in gm-root.27.6)
variants/
  native/                      # --orchestration=native (default CI)
  gastown/                     # --orchestration=gastown (manual / nightly)
reports/                       # historical run reports
```

## Tooling

- **Type-checking:** files under `shared/` and `variants/` are included in `testing/e2e/tsconfig.json`. Run `pnpm exec tsc --noEmit` from `testing/e2e/`.
- **Test runner:** Playwright. Specs live under `variants/<variant>/`; run via `pnpm exec playwright test --config=variants/<variant>/playwright.config.ts` (lands in gm-root.27.12 / gm-root.27.15).
- **Path aliases:** `@e2e/*` (testing/e2e), `@acc/*` (this dir).

## Running

CI default (native, mocked agents):

```sh
pnpm exec playwright test --config=variants/native/playwright.config.ts
```

Real-claude opt-in:

```sh
GEMBA_ACCEPTANCE_REAL_AGENTS=1 pnpm exec playwright test ...
```

Gastown variant (opt-in; requires gt CLI configured):

```sh
GEMBA_ACCEPTANCE_RUN_GASTOWN=1 pnpm exec playwright test --config=variants/gastown/playwright.config.ts
```

## Implementation status

See `gm-root.27` for the per-bead breakdown. Each child bead's description references this README + the design doc + the relevant sub-decision.
