# Refinery — CI Watch After Merges

After any merge to `main` lands (manual merge, merge queue, or direct push),
check the latest GitHub Actions run. If it's red, either fix it yourself (simple,
obvious) or sling a bead (anything else). Don't leave main broken.

> **Loading this doc:** refinery's local `CLAUDE.md` is `.gitignored` per Gas
> Town convention, so add a one-line pointer in that local file:
> `> Post-merge CI checks: see docs/agents/refinery-ci-watch.md` — this keeps
> the guidance durable (tracked here) while the per-role CLAUDE.md stays local.

## When to run this

Run the check after:
- You (refinery) merge a polecat MR.
- You observe a push to `main` via any path.
- You are asked "is CI green?" or "check CI".

Skip if the most recent run for the commit on `main` is already green.

## Procedure

1. **Identify the latest CI run on `main`:**
   ```bash
   gh run list --repo GembaCore/gemba-core --branch main --limit 1
   ```
2. **If status is failure**, pull the failing log:
   ```bash
   gh run view <run-id> --repo GembaCore/gemba-core --log-failed | head -200
   ```
3. **Classify the failure.** Use the table below. If it matches a known pattern,
   fix in place. Otherwise, file and sling a bead.

## Known patterns — fix in place

| Symptom in log | Root cause | Fix |
|---|---|---|
| `pnpm ... --run` → `ERROR Unknown option: 'run'` (macOS/Linux) | pnpm 9 eats pass-through flags | Move the flag into the npm script itself (e.g. `"test": "vitest run"`), drop the flag from the Makefile |
| `File is not gofmt-ed with -s` on Windows runners only | `.gitattributes` lets Windows check out `.go` as CRLF | Add `*.go text eol=lf` (and peers) to `.gitattributes` |
| `go:embed all:web/dist matches no files` | `web/dist/.keep` missing after clean checkout | `mkdir -p web/dist && touch web/dist/.keep` (or ensure `dist-sentinel` target runs before build) |
| `eslint: command not found` in CI | `web/node_modules` not installed before lint | Add `frontend-install` as a prerequisite to the lint target |
| `golangci-lint` flags unformatted files you just edited | You skipped `make fmt` | Run `gofmt -s -w .` and amend — fast, zero risk |

**Verification before pushing a fix:**
- `make test` and `make lint` clean locally.
- `gh run watch <new-run-id> --exit-status` goes green across all 6 matrix legs.

**Commit convention:** `fix(ci): <what>` — keep the body factual about which leg
was failing and why the fix works.

## When to sling instead of fix

Sling a bead if any of these are true:
- The failure is in test **logic** (an assertion fails), not infrastructure.
- The fix touches more than `.gitattributes`, `Makefile`, `.github/workflows/`,
  or the `web/package.json` scripts block.
- Multiple unrelated failures in one run (triage + dispatch, don't batch-fix).
- Flaky: same test fails intermittently — file, don't paper over with retries.

Sling procedure:

```bash
# File the bead in gemba
bd create -t bug -p 0 \
  -T "BUG: CI <matrix-leg> fails — <one-line root cause>" \
  -d "<failing run url>

<paste the 10-20 relevant log lines>

DoD:
- CI green on all 6 matrix legs
- Root cause addressed (not masked with retries or allow-failure)"

# Sling to an idle polecat (auto-creates if needed)
gt sling <new-bead-id> gemba --create
```

Label the bead `layer:ci` and `risk:medium` unless the failure indicates a
deeper product bug surfaced by CI — then also label `surface:backend` or
`surface:frontend` as appropriate.

## Never

- Never disable a matrix leg to make CI green. If a leg is genuinely
  unsupported, that's a decision for the mayor, not a silent edit.
- Never `--no-verify` a commit or `allow_failure: true` in the workflow as a
  shortcut. Root-cause or sling.
- Never force-push to `main`.
