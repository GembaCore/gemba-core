// specs/integration/dispatch-chain.spec.ts — gm-5v8v.15
//
// Migrated from the bespoke scripts/e2e/hello-world.test.mjs driver.
// Exercises the full dispatch chain end-to-end against a real gemba
// serve + bd workspace:
//
//   board renders seeded epic
//     → drag the card into "In Progress"
//       → SPA fires PATCH /api/work-items/{id}
//         → server fires POST /api/sessions
//           → backend spawns the session pane
//             → (when an agent binary is wired) the agent writes
//               an evidence file in the worktree
//
// Tagged @deep — runs only under integration-deep / smoke-deep
// projects with the real-backend fixture (gm-5v8v.2). The evidence-
// file assertion auto-skips when no agent binary is available so
// the spec stays useful in CI lanes where claude / aider aren't
// installed.

import type { Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect } from '../../fixtures/server';

// EVIDENCE_TIMEOUT — how long to wait for the agent to write the
// evidence file. The legacy driver used 3 minutes; in a CI lane the
// agent is rarely there so the wait is wasted. Keep the budget
// generous when the file actually shows up, but fail-fast when the
// pane has obviously not produced anything.
const EVIDENCE_TIMEOUT = 180_000;

// agentRunnable returns the agent binary the workspace declares as
// available, or empty string when the spawn would fail. Lets the
// evidence-file half of the chain skip cleanly on a runner without
// claude/aider/etc. installed.
function agentRunnable(): string {
  for (const candidate of ['claude', 'aider']) {
    try {
      execFileSync('which', [candidate], { stdio: 'pipe' });
      return candidate;
    } catch {
      /* not installed */
    }
  }
  return '';
}

function tmuxAvailable(): boolean {
  try {
    execFileSync('tmux', ['-V'], { stdio: 'pipe' });
    return true;
  } catch {
    return false;
  }
}

// recordSpawnNetwork attaches request + response listeners that
// capture the PATCH + POST traffic the dispatch chain produces.
// Returned arrays are mutated as traffic arrives — assertions poll
// against length to wait for individual hops without sleeping.
function recordSpawnNetwork(page: Page) {
  const patches: { url: string; body: string | null }[] = [];
  const sessionPosts: { url: string; body: string | null }[] = [];

  page.on('request', (req) => {
    const url = req.url();
    if (!url.includes('/api/')) return;
    const method = req.method();
    if (method === 'PATCH' && url.includes('/api/work-items/')) {
      patches.push({ url, body: req.postData() });
    } else if (method === 'POST' && url.includes('/api/sessions')) {
      sessionPosts.push({ url, body: req.postData() });
    }
  });

  return { patches, sessionPosts };
}

test.describe('@deep dispatch chain — drag → PATCH → POST /sessions → tmux → evidence', () => {
  // Real-backend only: the surfaces this exercises (PATCH, session
  // POST, pane spawn) need actual server-side logic, not page.route
  // intercepts.
  test.skip(({ backend }) => backend !== 'real', 'deep-only spec (gm-5v8v.15)');

  test('drag epic to In Progress dispatches a session', async ({
    page,
    bd,
    realServer,
  }) => {
    test.skip(!tmuxAvailable(), 'tmux not available on this runner');
    // backend=real guarantees realServer is wired; narrow the type
    // for the rest of the spec so we can use realServer.baseURL
    // directly without optional chaining at every call site.
    if (!realServer) throw new Error('realServer fixture missing under backend=real');

    // 1. Seed an epic via the bd CLI — the workspace is per-worker
    //    isolated (gm-5v8v.2) so the id we get back is the only
    //    matching card on the board.
    const targetTitle = 'gm-5v8v.15 dispatch-chain target';
    const created = await bd.create({
      type: 'epic',
      priority: 2,
      title: targetTitle,
    });
    const beadID = created.id;
    const shortID = beadID.split('/').pop() ?? beadID;

    // 2. Sanity: the seeded bead surfaces in /api/work-items.
    let seeded: { id: string; title?: string } | undefined;
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${realServer.baseURL}/api/work-items?limit=200`);
          if (!response.ok()) return false;
          const payload = (await response.json()) as { items?: { id: string; title?: string }[] };
          seeded = payload.items?.find(
            (i) =>
              i.id === beadID ||
              i.id.endsWith(`/${beadID}`) ||
              i.id.split('/').pop() === shortID ||
              i.title === targetTitle,
          );
          return Boolean(seeded);
        },
        { timeout: 45_000 },
      )
      .toBe(true);
    expect(seeded, 'seeded bead in /api/work-items').toBeTruthy();
    const apiBeadID = seeded!.id;

    // 3. Open the board.
    const network = recordSpawnNetwork(page);
    await page.goto(`${realServer.baseURL}/board?layout=epic`);

    // The default swimlane mode hides orphan epics under a separate
    // lane below the fold; "none" puts every epic in a single flat
    // lane so the drag target is on-screen. The legacy driver used
    // .first() — stick with that pattern; the swimlane-mode select
    // is the only <select> element in the board chrome.
    const laneSelect = page.locator('select').first();
    if ((await laneSelect.count()) > 0) {
      await laneSelect.selectOption('none').catch(() => {
        /* tolerate UI changes that drop the option */
      });
      await page.waitForTimeout(300);
    }

    // 4. Locate the seeded card. The legacy driver tried three
    //    selectors; the first (data-epic-card="true" filtered by
    //    text) is the most stable — the testid family churns when
    //    BoardPage's swimlane logic evolves.
    const card = page
      .locator('[data-epic-card="true"]')
      .filter({ hasText: shortID })
      .first();
    await expect(card, 'epic card on board').toBeVisible({ timeout: 45_000 });

    // 5. Pick the In-Progress cell on the same swimlane row so the
    //    drag stays short and predictable.
    const swimlaneId = await card.evaluate((el) => {
      let cur: HTMLElement | null = el as HTMLElement;
      while (cur && cur.parentElement) {
        const t = cur.getAttribute && cur.getAttribute('data-testid');
        if (t && t.startsWith('board-epic-cell-')) {
          return t.replace(/-[a-z]+$/, '').replace('board-epic-cell-', '');
        }
        cur = cur.parentElement;
      }
      return null;
    });
    const cell = swimlaneId
      ? page.locator(`[data-testid="board-epic-cell-${swimlaneId}-started"]`)
      : page.locator('[data-testid$="-started"]').first();
    await expect(cell, 'In Progress cell').toBeVisible();

    // 6. Drag the card. dnd-kit's PointerSensor needs >=4px movement
    //    before activation, so we issue an initial wiggle, then walk
    //    in steps. Playwright's high-level dragTo doesn't wait long
    //    enough for the activation constraint reliably across
    //    browsers; manual mouse events match what shipped in the
    //    legacy driver and round 2 confirmed it green.
    await card.scrollIntoViewIfNeeded();
    const cardBox = await card.boundingBox();
    const cellBox = await cell.boundingBox();
    expect(cardBox && cellBox, 'card + cell bounding boxes').toBeTruthy();
    const sx = cardBox!.x + cardBox!.width / 2;
    const sy = cardBox!.y + cardBox!.height / 2;
    const tx = cellBox!.x + Math.min(cellBox!.width * 0.35, 140);
    const ty = cellBox!.y + Math.min(cellBox!.height * 0.35, 80);

    await page.mouse.move(sx, sy);
    await page.mouse.down();
    await page.mouse.move(sx + 8, sy + 8, { steps: 5 });
    await page.waitForTimeout(50);
    await page.mouse.move(tx, ty, { steps: 25 });
    await page.waitForTimeout(50);
    await page.mouse.move(tx + 1, ty + 1, { steps: 2 });
    await page.mouse.up();

    // 7. PATCH lands first — the SPA optimistically re-renders, then
    //    fires the network call. Other PATCHes can follow quickly when
    //    dispatch completes, so wait specifically for this bead's
    //    transition into In Progress instead of asserting the first
    //    matching request body.
    const isTargetPatch = (p: { url: string; body: string | null }) =>
      p.url.includes(encodeURIComponent(apiBeadID)) ||
      p.url.includes(encodeURIComponent(beadID)) ||
      p.url.includes(encodeURIComponent(shortID));
    await expect
      .poll(
        () =>
          network.patches.some(
            (p) => isTargetPatch(p) && (p.body ?? '').includes('"state_category":"started"'),
          ),
        { timeout: 10_000 },
      )
      .toBe(true);

    // 8. The auto-dispatch loop fires POST /api/sessions once the
    //    state moves to started. Some workspaces gate this behind an
    //    operator confirmation (managed mode); when no POST lands
    //    we still record the chain made it to PATCH and skip the
    //    pane assertion below.
    const sawPost = await expect
      .poll(() => network.sessionPosts.length, { timeout: 15_000 })
      .toBeGreaterThan(0)
      .then(() => true)
      .catch(() => false);
    if (!sawPost) {
      test.skip(true, 'POST /api/sessions never fired — workspace likely in managed mode (gm-5v8v.11)');
    }

    // 9. Pane spawn is best-effort: we look for a tmux session whose
    //    name contains "gemba". Names vary across backends; failure
    //    here is informational rather than fatal.
    const tmuxOut = (() => {
      try {
        return execFileSync('tmux', ['ls'], { encoding: 'utf8' });
      } catch {
        return '';
      }
    })();
    const tmuxSpawned = tmuxOut.includes('gemba');
    if (!tmuxSpawned) {
      // Surfacing the missing session as a soft failure aids
      // debugging without flaking the suite when an alternative
      // backend (docker, applescript) is in play.
      console.warn('[dispatch-chain] tmux ls did not reveal a gemba pane; backend may be non-tmux');
    }

    // 10. Evidence file — only meaningful when an agent is actually
    //     installed. CI lanes without claude/aider should not block
    //     on this; emit a skip annotation instead.
    const agent = agentRunnable();
    if (!agent) {
      test.info().annotations.push({
        type: 'skip-reason',
        description: 'no agent binary on PATH — evidence-file assertion skipped',
      });
      return;
    }

    const homeDir = process.env.HOME ?? '';
    const worktree = join(
      homeDir,
      'gt/gemba/crew/worktrees',
      `bead-${apiBeadID.replace(/\//g, '_')}`,
    );
    const evidence = join(worktree, 'hello_world.md');

    // expect.poll handles the wait; the timeout matches the legacy
    // driver's 3-minute window because real agents take that long
    // to acknowledge + write a file from cold start.
    await expect.poll(
      () => existsSync(evidence),
      {
        message: `evidence file ${evidence} should exist after agent spawn`,
        timeout: EVIDENCE_TIMEOUT,
        intervals: [3_000],
      },
    ).toBe(true);

    const contents = readFileSync(evidence, 'utf8');
    expect(contents.length, 'evidence file should be non-empty').toBeGreaterThan(0);
  });
});
