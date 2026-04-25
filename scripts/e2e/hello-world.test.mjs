// gm-0i0d E2E driver: drag-to-In-Progress auto-dispatch + evidence check.
//
// The E2E spec called for creating the work-item via UI; that capability
// doesn't exist yet (filed gm-2xx6). This driver seeds the bead via bd
// CLI before launch and exercises the surfaces that DO exist:
//   - SPA loads + agents endpoint populated
//   - Board renders the seeded epic in a non-started column
//   - Drag the card into "In Progress" via dnd-kit (mouse-event sequence)
//   - Capture the resulting PATCH + POST /sessions network calls
//   - Observe the tmux pane spawn (out-of-band shell check)
//   - Poll for hello_world.md in the spawned worktree
//
// Output: a structured markdown report on stdout. Exit 0 if the chain
// completed (file present), 1 otherwise.

import { chromium } from 'playwright';
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const BEAD_ID = process.env.E2E_BEAD_ID || 'gemba/gemba/gm-dtgq';
const SHORT_ID = BEAD_ID.split('/').pop();
const SERVER = process.env.E2E_SERVER || 'http://127.0.0.1:7777';
const HOME = process.env.HOME;
const WORKTREE = join(HOME, 'gt/gemba/crew/worktrees', `bead-${BEAD_ID.replace(/\//g, '_')}`);
const EVIDENCE_FILE = join(WORKTREE, 'hello_world.md');
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
  step('Setup', 'info', `bead=${BEAD_ID} server=${SERVER}`);

  // 0. Sanity-check API is up + agents populated
  const agents = await fetch(`${SERVER}/api/agents`).then(r => r.json()).catch(e => ({ error: String(e) }));
  if (!agents.agents || agents.agents.length === 0) {
    step('Server reachable + agents populated', 'fail', `agents=${JSON.stringify(agents)}`);
    process.exit(1);
  }
  step('Server reachable + agents populated', 'pass', `${agents.total} agent(s): ${agents.agents.map(a => a.name).join(',')}`);

  // 1. Confirm the seeded bead is visible
  const items = await fetch(`${SERVER}/api/work-items?limit=200`).then(r => r.json());
  const bead = (items.items || []).find(i => i.id === BEAD_ID);
  if (!bead) {
    step('Seeded bead present in /api/work-items', 'fail', `id=${BEAD_ID} not found among ${items.items?.length} items`);
    process.exit(1);
  }
  step('Seeded bead present in /api/work-items', 'pass',
    `kind=${bead.kind} state_category=${bead.state_category} status=${bead.status}`);

  // 2. Launch Playwright + open Board
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
  const page = await ctx.newPage();

  page.on('console', msg => { if (msg.type() === 'error') CONSOLE_ERRORS.push(msg.text()); });
  page.on('request', req => {
    const url = req.url();
    if (url.includes('/api/') && (req.method() === 'PATCH' || req.method() === 'POST')) {
      const body = req.postData();
      NETWORK.push({ ts: Date.now(), method: req.method(), url, body });
    }
  });
  page.on('response', async resp => {
    const url = resp.url();
    if (url.includes('/api/') && (resp.request().method() === 'PATCH' || resp.request().method() === 'POST')) {
      const text = await resp.text().catch(() => '');
      NETWORK.push({ ts: Date.now(), method: 'RESP ' + resp.request().method(), status: resp.status(), url, body: text.slice(0, 500) });
    }
  });

  // SPA holds an SSE connection so networkidle never settles.
  await page.goto(`${SERVER}/board`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-testid^="board-epic-cell-"]', { timeout: 20_000 }).catch(() => {});
  step('Board loaded', 'pass', `url=${page.url()} title="${await page.title()}"`);

  // The default swimlane mode (by-parent-epic) hides orphan epics in
  // the "Orphan epics" lane below the fold; switching to "none" makes
  // every epic visible in a single flat lane regardless of hierarchy.
  const laneSelect = page.locator('select').first();
  if (await laneSelect.count() > 0) {
    await laneSelect.selectOption('none').catch(() => {});
    await page.waitForTimeout(500);
    step('Swimlane → none', 'pass');
  } else {
    step('Swimlane → none', 'warn', 'select not found; falling back to scroll');
  }

  // 3. Find the seeded epic card
  // Card data-testid pattern (from EpicCard.tsx) — try several selectors.
  let cardLocator = page.locator(`[data-epic-card="true"]`).filter({ hasText: SHORT_ID });
  let cardCount = await cardLocator.count();
  if (cardCount === 0) {
    cardLocator = page.locator(`[data-testid*="${BEAD_ID}"]`);
    cardCount = await cardLocator.count();
  }
  if (cardCount === 0) {
    // Last-ditch: scroll through swimlanes
    cardLocator = page.locator('text=' + SHORT_ID).first();
    cardCount = await cardLocator.count();
  }
  if (cardCount === 0) {
    step('Locate epic card on Board', 'fail', `no element matched ${SHORT_ID}`);
    await page.screenshot({ path: 'board-state.png', fullPage: true });
    await browser.close();
    writeReport();
    process.exit(1);
  }
  step('Locate epic card on Board', 'pass', `${cardCount} match(es)`);

  // 4. Find the "In Progress" droppable cell
  const cellLocator = page.locator(`[data-testid*="-started"]`).first();
  if (await cellLocator.count() === 0) {
    step('Locate In Progress droppable cell', 'fail', '');
    await browser.close();
    writeReport();
    process.exit(1);
  }
  step('Locate In Progress droppable cell', 'pass');

  // 5. Drag the card. dnd-kit's PointerSensor needs >=4px movement to
  // start the drag; Playwright's page.mouse dispatches pointer events
  // alongside mouse events, but we still have to (a) scroll the card
  // into view and (b) walk the cursor in small steps so the activation
  // constraint engages cleanly. Targeting the swimlane that shares the
  // card's row keeps the drop target near the card.
  await cardLocator.first().scrollIntoViewIfNeeded();
  await page.waitForTimeout(200);
  // Pick the In-Progress cell on the SAME swimlane row as the card so
  // we don't have to drag across half the page.
  const cardSwimlaneId = await cardLocator.first().evaluate(el => {
    let cur = el;
    while (cur && cur.parentElement) {
      const t = cur.getAttribute && cur.getAttribute('data-testid');
      if (t && t.startsWith('board-epic-cell-')) {
        // Cell testid is board-epic-cell-<rootId>-<cat>
        return t.replace(/-[a-z]+$/, '').replace('board-epic-cell-', '');
      }
      cur = cur.parentElement;
    }
    return null;
  });
  let scopedCell = cellLocator;
  if (cardSwimlaneId) {
    const same = page.locator(`[data-testid="board-epic-cell-${cardSwimlaneId}-started"]`);
    if (await same.count() > 0) scopedCell = same;
  }

  const cardBox = await cardLocator.first().boundingBox();
  const cellBox = await scopedCell.boundingBox();
  if (!cardBox || !cellBox) {
    step('Card + cell bounding boxes', 'fail', `card=${JSON.stringify(cardBox)} cell=${JSON.stringify(cellBox)}`);
    await browser.close();
    writeReport();
    process.exit(1);
  }
  const sx = cardBox.x + cardBox.width / 2;
  const sy = cardBox.y + cardBox.height / 2;
  const tx = cellBox.x + cellBox.width / 2;
  const ty = cellBox.y + cellBox.height / 2;
  // Use page.dragAndDrop which properly synthesizes the pointer event
  // sequence dnd-kit listens for. Fallback to manual mouse if that
  // fails.
  let dragOk = false;
  try {
    await page.mouse.move(sx, sy);
    await page.mouse.down();
    // Wiggle to satisfy distance=4 activation
    await page.mouse.move(sx + 8, sy + 8, { steps: 5 });
    await page.waitForTimeout(50);
    await page.mouse.move(tx, ty, { steps: 25 });
    await page.waitForTimeout(50);
    await page.mouse.move(tx + 1, ty + 1, { steps: 2 });
    await page.mouse.up();
    dragOk = true;
  } catch (e) {
    step('Drag gesture issued', 'fail', String(e));
  }
  if (dragOk) {
    step('Drag gesture issued', 'pass',
      `(${Math.round(sx)},${Math.round(sy)}) → (${Math.round(tx)},${Math.round(ty)}) swimlane=${cardSwimlaneId}`);
  }

  // 6. Wait for PATCH + POST /sessions
  const sawPatch = await waitFor('PATCH', () => NETWORK.some(n => n.method === 'PATCH' && n.url.includes('/api/work-items/')), 10_000, 250);
  step('PATCH /api/work-items fired', sawPatch ? 'pass' : 'fail',
    sawPatch ? NETWORK.find(n => n.method === 'PATCH').body || '<empty body>' : 'never observed');

  const sawPost = await waitFor('POST /sessions', () => NETWORK.some(n => n.method === 'POST' && n.url.includes('/api/sessions')), 15_000, 500);
  step('POST /api/sessions fired', sawPost ? 'pass' : 'fail',
    sawPost ? NETWORK.find(n => n.method === 'POST' && n.url.includes('/api/sessions')).body || '<empty body>' : 'never observed');

  // 7. Verify tmux session spawned
  const tmuxOut = tmux(['ls']);
  const tmuxHasGemba = tmuxOut.includes('gemba:');
  step('tmux session "gemba" spawned', tmuxHasGemba ? 'pass' : 'fail',
    tmuxOut.split('\n').filter(l => l.includes('gemba')).join(' | '));

  // 8. Poll for hello_world.md
  const fileFound = await waitFor('hello_world.md', () => existsSync(EVIDENCE_FILE), 180_000, 3000);
  if (fileFound) {
    const contents = readFileSync(EVIDENCE_FILE, 'utf8');
    step('Evidence: hello_world.md created', 'pass',
      `path=${EVIDENCE_FILE} bytes=${contents.length} content="${contents.slice(0, 100).replace(/\n/g,'\\n')}"`);
  } else {
    step('Evidence: hello_world.md created', 'fail',
      `not found at ${EVIDENCE_FILE} after 3 min`);
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
  console.log('# E2E test report — gm-0i0d');
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
