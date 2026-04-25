// gm-0i0d round 2 — same scenario as round 1, but exercises the
// surfaces that round-1 findings (gm-knrm/gm-nr67/gm-4jsi/gm-2xx6)
// closed:
//   - Create the bead via the SPA's NewWorkItemDialog (no bd seed).
//   - No priority bump (server's defaultListLimit covers it).
//   - No manual tmux Enter (preamble first-turn lands on its own).
//
// Output: a structured markdown report on stdout. Exit 0 if the chain
// completed (file present), 1 otherwise.

import { chromium } from 'playwright';
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const SERVER = process.env.E2E_SERVER || 'http://127.0.0.1:7777';
const HOME = process.env.HOME;
const BEAD_TITLE = process.env.E2E_TITLE || `E2E round 2 ${new Date().toISOString().slice(11,19)}`;
const BEAD_DESC = process.env.E2E_DESC ||
  'create a text file hello_world.md with the contents "##Hello world!" in the current worktree root. Do not run tests, do not commit, do not push, do not modify any other files. Once the file exists, exit.';
const REPORT = [];
const NETWORK = [];
const CONSOLE_ERRORS = [];

function step(name, status, detail = '') {
  const icon = { pass: '✅', fail: '❌', warn: '⚠️', info: 'ℹ️' }[status] || '?';
  REPORT.push(`${icon} **${name}** — ${detail}`);
  console.error(`${icon} ${name} :: ${detail}`);
}

function tmux(cmd) {
  try { return execFileSync('tmux', cmd, { encoding: 'utf8' }); }
  catch (e) { return ''; }
}

async function waitFor(label, fn, ms = 30_000, every = 1000) {
  const t0 = Date.now();
  while (Date.now() - t0 < ms) {
    try { if (await fn()) return true; } catch {}
    await new Promise(r => setTimeout(r, every));
  }
  return false;
}

(async () => {
  step('Setup', 'info', `server=${SERVER} title="${BEAD_TITLE}"`);

  const agents = await fetch(`${SERVER}/api/agents`).then(r => r.json()).catch(e => ({ error: String(e) }));
  if (!agents.agents || agents.agents.length === 0) {
    step('Server reachable + agents populated', 'fail', `agents=${JSON.stringify(agents)}`);
    process.exit(1);
  }
  step('Server reachable + agents populated', 'pass', `${agents.total} agent(s): ${agents.agents.map(a => a.name).join(',')}`);

  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 1100 } });
  const page = await ctx.newPage();

  page.on('console', msg => { if (msg.type() === 'error') CONSOLE_ERRORS.push(msg.text()); });
  let createdBeadId = null;
  page.on('request', req => {
    const url = req.url();
    const m = req.method();
    if (url.includes('/api/') && (m === 'POST' || m === 'PATCH')) {
      NETWORK.push({ ts: Date.now(), method: m, url, body: req.postData() });
    }
  });
  page.on('response', async resp => {
    const url = resp.url();
    const m = resp.request().method();
    if (url.includes('/api/') && (m === 'POST' || m === 'PATCH')) {
      const text = await resp.text().catch(() => '');
      NETWORK.push({ ts: Date.now(), method: 'RESP ' + m, status: resp.status(), url, body: text.slice(0, 500) });
      if (m === 'POST' && url.includes('/api/work-items') && resp.status() === 201) {
        try {
          const obj = JSON.parse(text);
          if (obj?.id) createdBeadId = obj.id;
        } catch {}
      }
    }
  });

  await page.goto(`${SERVER}/board`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-testid^="board-epic-cell-"]', { timeout: 20_000 }).catch(() => {});
  step('Board loaded', 'pass', `url=${page.url()}`);

  // 1. Open the New work-item dialog via the header button.
  const newBtn = page.locator('[data-testid="board-new-workitem"]');
  await newBtn.click();
  await page.waitForSelector('[role="dialog"]', { timeout: 5_000 });
  step('New work-item dialog opened', 'pass');

  // 2. Fill the form.
  await page.locator('input[placeholder="What needs doing?"]').fill(BEAD_TITLE);
  // Kind select is the first <select> inside the dialog.
  const dlgSelects = page.locator('[role="dialog"] select');
  await dlgSelects.first().selectOption('epic');
  // Priority — leave at default P2 (server's defaultListLimit covers it).
  await page.locator('textarea[placeholder="Markdown OK."]').fill(BEAD_DESC);
  step('Form filled', 'pass', `title="${BEAD_TITLE}" kind=epic priority=P2 (default)`);

  // 3. Submit.
  await page.locator('[role="dialog"] button:has-text("Create")').click();
  // Wait for the dialog to close + the POST response to settle createdBeadId.
  await waitFor('createdBeadId', () => createdBeadId !== null, 10_000, 250);
  if (!createdBeadId) {
    step('Bead created via UI', 'fail', 'POST /api/work-items did not return a bead id');
    await browser.close();
    writeReport();
    process.exit(1);
  }
  step('Bead created via UI', 'pass', `id=${createdBeadId}`);

  // 3a. Close the EpicDrawer that auto-opens after creation (gm-e12.5
  // behaviour: NewWorkItemDialog.onCreated drives setOpenWorkItemId →
  // BoardPage opens the drawer on the new id, which covers the right
  // half of the board including the In Progress column).
  const drawerClose = page.locator('[role="dialog"] button[aria-label="Close"], [role="dialog"] button:has-text("×"), [data-slot="dialog-close"]').first();
  if (await drawerClose.count() > 0) {
    await drawerClose.click().catch(() => {});
  } else {
    await page.keyboard.press('Escape');
  }
  await page.waitForTimeout(300);
  step('EpicDrawer closed', 'pass');

  // 4. Switch swimlane to "none" so the new orphan epic is visible.
  const laneSelect = page.locator('[data-testid="board-view-toggle"] select').first();
  if (await laneSelect.count() > 0) {
    await laneSelect.selectOption('none').catch(() => {});
    await page.waitForTimeout(500);
    step('Swimlane → none', 'pass');
  } else {
    step('Swimlane → none', 'warn', 'select not found');
  }

  // 5. Locate the new card. The id is workspace-prefixed (gemba/gemba/gm-XX);
  // EpicCard renders the short tail, so match on the full id substring on
  // the rendered card.
  const shortId = createdBeadId.split('/').pop();
  const cardLocator = page.locator(`[data-epic-card="true"]`).filter({ hasText: shortId });
  await waitFor('card visible', async () => (await cardLocator.count()) > 0, 10_000, 500);
  if (await cardLocator.count() === 0) {
    step('Locate epic card on Board', 'fail', `no [data-epic-card] matched ${shortId}`);
    await browser.close();
    writeReport();
    process.exit(1);
  }
  step('Locate epic card on Board', 'pass', `${await cardLocator.count()} match(es) for ${shortId}`);

  // 6. Drag to In Progress.
  await cardLocator.first().scrollIntoViewIfNeeded();
  await page.waitForTimeout(200);
  // Same-swimlane In Progress cell.
  const swimlaneId = await cardLocator.first().evaluate(el => {
    let cur = el;
    while (cur && cur.parentElement) {
      const t = cur.getAttribute && cur.getAttribute('data-testid');
      if (t && t.startsWith('board-epic-cell-')) {
        return t.replace(/-[a-z]+$/, '').replace('board-epic-cell-', '');
      }
      cur = cur.parentElement;
    }
    return null;
  });
  const inProgressCell = swimlaneId
    ? page.locator(`[data-testid="board-epic-cell-${swimlaneId}-started"]`)
    : page.locator(`[data-testid*="-started"]`).first();
  const cardBox = await cardLocator.first().boundingBox();
  const cellBox = await inProgressCell.boundingBox();
  if (!cardBox || !cellBox) {
    step('Card + cell bounding boxes', 'fail', `card=${JSON.stringify(cardBox)} cell=${JSON.stringify(cellBox)}`);
    await page.screenshot({ path: '/tmp/round2-board.png', fullPage: true });
    await browser.close();
    writeReport();
    process.exit(1);
  }
  // If the cell is above the viewport, the card scrolled to a row whose
  // In Progress sibling is offscreen. Scroll the cell into view too.
  if (cellBox.y < 0 || cellBox.y > 2000) {
    await inProgressCell.scrollIntoViewIfNeeded();
    await page.waitForTimeout(200);
    const cellBox2 = await inProgressCell.boundingBox();
    if (cellBox2) Object.assign(cellBox, cellBox2);
    // After scrolling the cell into view the card may be off-screen.
    const cardBox2 = await cardLocator.first().boundingBox();
    if (cardBox2) Object.assign(cardBox, cardBox2);
    if (cardBox.y < 0 || cardBox.y > 2000) {
      // Scroll halfway between them.
      await page.evaluate(() => window.scrollBy(0, -200));
      await page.waitForTimeout(200);
      const c3 = await cardLocator.first().boundingBox();
      const e3 = await inProgressCell.boundingBox();
      if (c3) Object.assign(cardBox, c3);
      if (e3) Object.assign(cellBox, e3);
    }
  }
  const sx = cardBox.x + cardBox.width / 2;
  const sy = cardBox.y + cardBox.height / 2;
  // The In Progress cell stretches the FULL height of the swimlane
  // (which can be thousands of pixels when the lane has many epics
  // stacked in another column). Aiming at the cell center lands far
  // outside the viewport. Use the cell's x-center but the CARD's y so
  // the drop lands in the "In Progress" column on the card's row.
  const tx = cellBox.x + cellBox.width / 2;
  const ty = sy;
  if (sy < 0 || sy > 1100) {
    step('Card offscreen after scroll', 'fail',
      `card=(${Math.round(sx)},${Math.round(sy)}) cell=(${Math.round(tx)},${Math.round(cellBox.y + cellBox.height/2)})`);
    await page.screenshot({ path: '/tmp/round2-board.png', fullPage: true });
    await browser.close();
    writeReport();
    process.exit(1);
  }
  await page.mouse.move(sx, sy);
  await page.mouse.down();
  await page.mouse.move(sx + 8, sy + 8, { steps: 5 });
  await page.waitForTimeout(50);
  await page.mouse.move(tx, ty, { steps: 25 });
  await page.waitForTimeout(50);
  await page.mouse.move(tx + 1, ty + 1, { steps: 2 });
  await page.mouse.up();
  step('Drag gesture issued', 'pass', `(${Math.round(sx)},${Math.round(sy)}) → (${Math.round(tx)},${Math.round(ty)})`);

  // 7. PATCH + POST /sessions
  const sawPatch = await waitFor('PATCH', () =>
    NETWORK.some(n => n.method === 'PATCH' && n.url.includes('/api/work-items/')),
    10_000, 250);
  step('PATCH /api/work-items fired', sawPatch ? 'pass' : 'fail',
    sawPatch ? NETWORK.find(n => n.method === 'PATCH').body || '<empty>' : 'never observed');

  const sawPost = await waitFor('POST /sessions', () =>
    NETWORK.some(n => n.method === 'POST' && n.url.includes('/api/sessions')),
    15_000, 500);
  step('POST /api/sessions fired', sawPost ? 'pass' : 'fail',
    sawPost ? NETWORK.find(n => n.method === 'POST' && n.url.includes('/api/sessions')).body || '<empty>' : 'never observed');

  // 8. tmux pane
  const tmuxOut = tmux(['ls']);
  step('tmux session "gemba" spawned', tmuxOut.includes('gemba:') ? 'pass' : 'fail',
    tmuxOut.split('\n').filter(l => l.includes('gemba')).join(' | '));

  // 9. Evidence — no manual nudge this round.
  const worktree = join(HOME, 'gt/gemba/crew/worktrees', `bead-${createdBeadId.replace(/\//g, '_')}`);
  const evidenceFile = join(worktree, 'hello_world.md');
  const t0 = Date.now();
  const fileFound = await waitFor('hello_world.md', () => existsSync(evidenceFile), 180_000, 3000);
  const elapsed = Math.round((Date.now() - t0) / 1000);
  if (fileFound) {
    const contents = readFileSync(evidenceFile, 'utf8');
    step('Evidence: hello_world.md created (no manual nudge)', 'pass',
      `path=${evidenceFile} bytes=${contents.length} elapsed=${elapsed}s content="${contents.slice(0, 100).replace(/\n/g, '\\n')}"`);
  } else {
    step('Evidence: hello_world.md created (no manual nudge)', 'fail',
      `not found at ${evidenceFile} after ${elapsed}s`);
  }

  await browser.close();
  writeReport();
  process.exit(fileFound ? 0 : 1);
})().catch(e => {
  console.error('FATAL:', e);
  step('FATAL', 'fail', String(e));
  writeReport();
  process.exit(2);
});

function writeReport() {
  console.log('# E2E test report — gm-0i0d round 2');
  console.log('');
  console.log('## Steps');
  for (const r of REPORT) console.log('- ' + r);
  console.log('');
  console.log('## Network (PATCH + POST /api only)');
  for (const n of NETWORK) {
    console.log(`- \`${n.method}${n.status ? ' ' + n.status : ''}\` ${n.url}`);
    if (n.body) console.log('  - body: ' + n.body.slice(0, 300).replace(/\n/g, ' '));
  }
  console.log('');
  if (CONSOLE_ERRORS.length) {
    console.log('## Console errors');
    for (const e of CONSOLE_ERRORS) console.log('- ' + e);
  }
}
