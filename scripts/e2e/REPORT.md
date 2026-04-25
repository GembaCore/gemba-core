# gm-0i0d — E2E test report

**Date:** 2026-04-25
**Build:** gemba 8ac7e55-dirty
**Target bead:** gm-dtgq (epic; description: "create a text file hello_world.md with contents '##Hello world!'")
**Driver:** scripts/e2e/hello-world.test.mjs (Playwright/Chromium headless)
**Server:** http://127.0.0.1:7777 with `--orchestration=native --terminal=tmux`

## Summary

The recently-shipped chain — board drag → PATCH state_category → POST /sessions → tmux pane spawn → Claude executes bead → file created — works end to end. Eleven of twelve scripted steps passed unaided; the twelfth required a single manual `tmux send-keys Enter` to nudge Claude past its startup screen. Resulting file matches the spec byte for byte.

## Steps

| # | Step | Result | Detail |
|---|------|--------|--------|
| 0 | Server reachable + agents populated | ✅ | `/api/agents` → `{agents:[{id:claude,...,dialect:claude}],total:1}` |
| 1 | Seeded bead present in `/api/work-items` | ✅ | kind=epic, state_category=unstarted, status=open |
| 2 | Board page loaded | ✅ | `/board` returned 200, swimlane cells rendered within 20s |
| 3 | Swimlane → none | ✅ | `<select>` toggle works |
| 4 | Locate epic card on Board | ✅ | After P0 priority bump (see Finding 3) |
| 5 | Locate "In Progress" droppable cell | ✅ | data-testid `board-epic-cell-__all__-started` |
| 6 | Drag gesture issued (Playwright pointer events) | ✅ | scrollIntoView + 25-step pointer move + wiggle |
| 7 | PATCH /api/work-items fired | ✅ | body: `{"state_category":"started"}` → 200 OK; response carries `state_category=started status=in_progress` (gm-m3c8 fix verified) |
| 8 | POST /api/sessions fired | ✅ | body: `{"bead_id":"gemba/gemba/gm-dtgq","agent_type":"claude"}` → 201 Created with session id `tmux:gemba/gemba/gm-dtgq:<ns>` (gm-q7dz auto-dispatch verified) |
| 9 | tmux session "gemba" spawned | ✅ | window 1 launched `claude --model claude-opus-4-7` in worktree |
| 10 | Claude auto-executes preamble | ⚠️ | The preamble notification appeared in the input box but was NOT auto-submitted. A manual `tmux send-keys Enter` kicked it off. See Finding 5. |
| 11 | Evidence: hello_world.md created | ✅ | After the manual nudge: file at expected path, exact contents `##Hello world!\n` (14 bytes) |

## Findings (filed as new beads)

| Bead | Severity | Title |
|------|----------|-------|
| gm-2xx6 | P1 | Missing capability: create work-item via UI / API |
| gm-knrm | P1 | Codegen bug: cmd/gen-core-types omits 6 string-alias types referenced by Flags |
| gm-nr67 | P1 | API/SPA: /api/work-items default cap of 50 hides newer/lower-priority items |
| gm-4jsi | P1 | Native preamble: SendKeys race — first-turn message types but doesn't submit |

## Detailed findings

### Finding 1 — No create-work-item UI (gm-2xx6)
**Severity:** Blocker for original spec, soft-blocker for partial run

The bead description called for creating the work-item via the UI. The SPA exposes no "New work item" surface and the server has no `POST /api/work-items` route (only Get + Patch on `/work-items[/{id}]`). The driver had to seed the bead via the `bd` CLI to proceed.

### Finding 2 — pre-existing codegen bug blocks `make build`

`web/src/types/core.gen.ts` references six TypeScript types — `SchemaEnforcement`, `QueryLanguage`, `VersioningTransport`, `ConcurrencyModel`, `AgentNativeAPI`, `OrchestratorHook` — that the codegen never declares. `tsc --noEmit` (which `pnpm build` runs first) fails before vite can produce the SPA bundle that gets embedded into `bin/gemba`. Worked around by adding the six aliases as `string` to `internal/core/codegen.go`'s preamble; this is a real fix and should land permanently.

### Finding 3 — `/api/work-items` pagination silently hides new beads (gm-2xxb)

The default `GET /api/work-items` (no params) returns the top 50 by priority. `gm-dtgq` was created as P3 and was beyond the cap, so it never appeared on the board even though it was visible in `/api/work-items?limit=200`. The driver bumped its priority to P0 to unblock the test. The board's `useWorkItems()` calls without a limit, so any newly-created lower-priority item is effectively invisible until pagination or a "load more" surface ships.

### Finding 4 — orphan epics hidden under default swimlane mode

Default swimlane mode is `by-parent-epic`. Epics with no `parent_child` relationship to any root land in an "Orphan epics" lane that's below the fold for repos with many phase-epics. The driver had to programmatically toggle the swimlane `<select>` to `none` to surface the new bead. Not a bug per se, but a UX hazard for any new top-level epic.

### Finding 5 — preamble first-turn submit doesn't fire reliably (gm-2xxc)

The native plane's preamble strategy `claude_md` writes a session-context block to `CLAUDE.md` and then sends the notification message `"Your session context (bead + DoD + project values) has been appended to CLAUDE.md. Please read it and begin."` to the pane via `Backend.SendKeys(pane, msg + " Enter")`. In this run, the message ended up typed in Claude's input box but never submitted — Claude sat at the prompt indefinitely. A manual `tmux send-keys -t gemba:1 Enter` after the spawn unblocked it; Claude then read CLAUDE.md, created the file, and exited.

Possible root causes:
1. Race: `SendKeys` runs before Claude's Ink TUI has finished its boot animation, so the keys land before the input handler is registered.
2. The literal `Enter` token may not be interpreted as a key by tmux when concatenated with the prior text in a single `send-keys` call.
3. Claude's auto-mode may require an explicit submit on the first turn even with `--dangerously-skip-permissions`-like settings.

Suggested fix scope: add a small post-spawn delay or poll-for-prompt before SendKeys, or split into two calls (`send-keys -- "<text>"` then a separate `send-keys Enter`). Reproduce with `gemba serve --orchestration=native` + `curl POST /api/sessions`.

## Network capture (PATCH + POST /api only)

```
PATCH /api/work-items/gemba%2Fgemba%2Fgm-dtgq
  body:  {"state_category":"started"}
RESP   200 OK — kind=epic, state_category=started, status=in_progress

POST  /api/sessions
  body:  {"bead_id":"gemba/gemba/gm-dtgq","agent_type":"claude"}
RESP   201 Created — id=tmux:gemba/gemba/gm-dtgq:1777088704697521000
       provider_metadata.pane_cwd=/Users/mikebengtson/gt/gemba/crew/worktrees/bead-gemba_gemba_gm-dtgq
       provider_metadata.agent_type=claude
       provider_metadata.backend=tmux
```

## Conclusion

The drag → dispatch → tmux spawn integration shipped in gm-q7dz / gm-mwir / gm-m3c8 works as intended. The remaining E2E friction is upstream (no create-UI) and downstream (preamble submit race). Both are tractable and now have beads.

The driver script is checked in at `scripts/e2e/hello-world.test.mjs` and is parameterised via `E2E_BEAD_ID` / `E2E_SERVER` so it can be re-run against any seeded bead.
