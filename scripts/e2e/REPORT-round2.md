# gm-0i0d round 2 — E2E test report

**Date:** 2026-04-25
**Build:** gemba 8f57393 (round-1 fixes landed: gm-knrm, gm-nr67, gm-4jsi)
**Driver:** scripts/e2e/hello-world-round2.test.mjs (Playwright/Chromium headless)
**Server:** http://127.0.0.1:7777 with `--orchestration=native --terminal=tmux --dangerously-skip-permissions`

## Headline

**Hands-off success.** Bead created via the SPA's NewWorkItemDialog at default
P2, dragged to In Progress, tmux pane spawned, Claude executed the bead and
created `hello_world.md` in the worktree — **no manual nudge, no priority
bump, no `bd` seed**. Total elapsed from drag to evidence: **15s**.

Driver exit code: **0**.

## Steps

| # | Step | Result | Detail |
|---|------|--------|--------|
| 0 | Server reachable + agents populated | ✅ | `/api/agents` → `claude` |
| 1 | Board loaded | ✅ | swimlane cells rendered within 20s |
| 2 | New work-item dialog opened | ✅ | `[data-testid="board-new-workitem"]` (gm-2xx6 capability — was already there) |
| 3 | Form filled | ✅ | title, kind=epic, **priority=P2 (default)**, description |
| 4 | Bead created via UI | ✅ | `POST /api/work-items` → 201, id=`gemba/gemba/gm-4xto` |
| 5 | EpicDrawer closed | ✅ | NewWorkItemDialog auto-opens drawer (gm-e12.10); ESC dismissed it |
| 6 | Swimlane → none | ✅ | required to surface a fresh orphan epic |
| 7 | Locate epic card on Board | ✅ | **without priority bump** — gm-nr67 fix works (defaultListLimit covers P2) |
| 8 | Drag gesture issued | ✅ | playwright pointer move to In Progress cell on the same row |
| 9 | PATCH /api/work-items fired | ✅ | body `{"state_category":"started"}` → 200 |
| 10 | POST /api/sessions fired | ✅ | body `{"bead_id":"…gm-4xto","agent_type":"claude"}` → 201 |
| 11 | tmux session "gemba" spawned | ❌ (race) | `tmux ls` polled too soon; session appeared ~1s later (`gemba: 2 windows`). Cosmetic — see below. |
| 12 | Evidence: hello_world.md (no manual nudge) | ✅ | path=worktrees/bead-…gm-4xto/hello_world.md, 15 bytes, content `##Hello world!\n`, **elapsed 15s** |

## What round-1 fixes verified

| Round-1 finding | Fix landed in | Round-2 evidence |
|----------------|---------------|------------------|
| gm-knrm — codegen omits 6 string aliases (was: tsc fails) | 8f57393 | `pnpm build` produced the SPA bundle that's now embedded; `/board` rendered |
| gm-nr67 — `/api/work-items` default cap of 50 hides newer beads | 8f57393 | Created at **P2 default**; card was visible without any priority bump |
| gm-4jsi — preamble first-turn SendKeys race | 8f57393 | Claude submitted its first turn and produced `hello_world.md` in 15s with **zero manual `tmux send-keys Enter`** |
| gm-2xx6 — "no create-UI" finding | (already shipped in gm-e12.10) | NewWorkItemDialog used end-to-end |

## Network capture (PATCH + POST /api only)

```
POST  /api/work-items
  body: {"item":{"title":"…","kind":"epic","priority":2,"description":"…"}}
RESP   201 — id=gemba/gemba/gm-4xto

PATCH /api/work-items/gemba%2Fgemba%2Fgm-4xto
  body: {"state_category":"started"}
RESP   200 — kind=epic, state_category=started, status=in_progress

POST  /api/sessions
  body: {"bead_id":"gemba/gemba/gm-4xto","agent_type":"claude"}
RESP   201 — id=tmux:gemba/gemba/gm-4xto:1777090675553511000
        provider_metadata.pane_cwd=…/worktrees/bead-gemba_gemba_gm-4xto
        provider_metadata.agent_type=claude  backend=tmux
```

## Console warnings

- One a11y warning: `DialogContent requires a DialogTitle`. Filed/fixable
  separately; non-blocking. Likely the EpicDrawer or NewWorkItemDialog.

## Step 11 footnote (test-bug, not product-bug)

The `tmux ls` check fires immediately after the `POST /api/sessions` 201
response. The actual `tmux new-session` runs on the server's session-spawn
goroutine and lands a beat later. By the time the test polls for the file
(immediately after), the session is up — and the file appears 15s later in
its worktree, which proves the spawn worked. Subsequent `tmux ls` confirms:

```
gemba: 2 windows (created Sat Apr 25 00:17:55 2026)
```

The simplest fix is to poll `tmux ls` for ~3s before declaring failure, or
to read the session id back from the `POST /api/sessions` 201 body (which
already contains `tmux:…` and `pane_cwd`). Cosmetic; not a regression.

## Conclusion

The drag → dispatch → tmux spawn → preamble → Claude-first-turn chain that
gm-q7dz / gm-mwir / gm-m3c8 / gm-4jsi-fix shipped is **fully hands-off** as
of 8f57393. Round-1's three concrete findings are closed by observation.
gm-0i0d is done.
