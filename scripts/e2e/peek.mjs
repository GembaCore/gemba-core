import { chromium } from 'playwright';
const b = await chromium.launch({ headless: true });
const ctx = await b.newContext({ viewport: { width: 1600, height: 1200 } });
const p = await ctx.newPage();
await p.goto('http://127.0.0.1:7777/board', { waitUntil: 'domcontentloaded' });
await p.waitForSelector('[data-testid^="board-epic-cell-"]', { timeout: 20000 });
const sel = p.locator('select').first();
if (await sel.count() > 0) await sel.selectOption('none').catch(()=>{});
await p.waitForTimeout(2000);
// dump every text node containing 'gm-' on the page
const txt = await p.evaluate(() => {
  const out = [];
  document.querySelectorAll('*').forEach(el => {
    const t = el.childNodes && Array.from(el.childNodes).filter(n => n.nodeType===3).map(n=>n.textContent).join('').trim();
    if (t && t.match(/gm-(dtgq|root|e\d|97w7)/)) out.push(t.slice(0,80));
  });
  return [...new Set(out)];
});
console.log(JSON.stringify(txt, null, 2));
await p.screenshot({ path: '/tmp/board-none.png', fullPage: true });
await b.close();
