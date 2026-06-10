// shared/helpers/demo-mode.ts (gm-root.27.36)
//
// Helpers that turn an acceptance run into a 30-second screen-capture
// demo. Activated by GEMBA_ACCEPTANCE_DEMO_MODE=1 (the
// 'acceptance-native-demo' Playwright project sets it via
// playwright.config.ts).
//
// Behaviors when active:
//   - On-screen caption banner injected via page.addInitScript +
//     page.evaluate. Banner sits fixed top-center, updates per step.
//   - Visual pauses at known frames (after each milestone) so the
//     viewer's eye can land on the change.
//   - Cursor slowMo applied at the Playwright project level (not in
//     this helper — see playwright.config.ts).
//
// Time-lapse cuts (ffmpeg) are NOT done here; left as a hand-edit
// step on the captured .webm. Default 'video: on' produces a single
// continuous video; the operator splices it down to 30s in post.

import type { Locator, Page } from '@playwright/test';

export const DEMO_MODE = process.env.GEMBA_ACCEPTANCE_DEMO_MODE === '1';

const BANNER_ID = 'gemba-demo-caption';

const INIT_SCRIPT = `
(() => {
  const root = document.documentElement;
  root.classList.add('dark');
  root.dataset.theme = 'dark';
  root.style.colorScheme = 'dark';
  try {
    window.localStorage.setItem('gemba-theme', 'dark');
  } catch {
    // Storage can be blocked during early browser bootstrap; the class
    // above still keeps the recorded frame dark.
  }

  const installBanner = () => {
  if (!document.getElementById('gemba-demo-style')) {
    const style = document.createElement('style');
    style.id = 'gemba-demo-style';
    style.textContent = [
      '[data-testid="adaptor-banner"]{display:none!important;}',
      'html,body{background:#0a0a0a!important;color:#f4f4f5!important;}',
      '@supports selector(body:has(*)){',
      'body:not(:has([data-testid="sidebar-nav"])){padding:32px!important;font:18px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif!important;}',
      'body:not(:has([data-testid="sidebar-nav"])) #root,body:not(:has([data-testid="sidebar-nav"])) [data-testid="app-root"]{min-height:calc(100vh - 64px)!important;color:#f4f4f5!important;}',
      'body:not(:has([data-testid="sidebar-nav"])) table{border-collapse:collapse!important;min-width:420px!important;margin-top:12px!important;background:#111827!important;color:#f4f4f5!important;border:1px solid #334155!important;}',
      'body:not(:has([data-testid="sidebar-nav"])) th,body:not(:has([data-testid="sidebar-nav"])) td{padding:8px 14px!important;border-bottom:1px solid #334155!important;text-align:right!important;}',
      'body:not(:has([data-testid="sidebar-nav"])) th:first-child,body:not(:has([data-testid="sidebar-nav"])) td:first-child{text-align:left!important;}',
      '}',
    ].join('');
    (document.head || document.documentElement).appendChild(style);
  }
  if (document.getElementById('${BANNER_ID}')) return;
  if (!document.body) return;
  const banner = document.createElement('div');
  banner.id = '${BANNER_ID}';
  banner.style.cssText = [
    'position: fixed',
    'top: 12px',
    'left: 50%',
    'transform: translateX(-50%)',
    'z-index: 999999',
    'padding: 10px 20px',
    'background: rgba(20, 20, 30, 0.92)',
    'color: #f4f4f7',
    'font: 600 16px/1.3 -apple-system, system-ui, sans-serif',
    'border-radius: 8px',
    'box-shadow: 0 6px 20px rgba(0,0,0,0.35)',
    'pointer-events: none',
    'transition: opacity 0.3s ease',
    'opacity: 0',
    'max-width: 80vw',
  ].join(';');
  banner.textContent = '';
  document.body.appendChild(banner);
  };

  installBanner();
  if (!document.body) {
    document.addEventListener('DOMContentLoaded', installBanner, { once: true });
  }
})();
`;

/**
 * Install the caption banner once at page load. Idempotent — safe to
 * call repeatedly. No-op when DEMO_MODE is off.
 */
export async function installDemoBanner(page: Page): Promise<void> {
  if (!DEMO_MODE) return;
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.addInitScript(INIT_SCRIPT);
  // Also inject into the current document (addInitScript only runs
  // on next navigation; install now too so the first goto sees it).
  await page.evaluate(INIT_SCRIPT).catch(() => undefined);
  await forceDemoDarkMode(page);
}

export async function forceDemoDarkMode(page: Page): Promise<void> {
  if (!DEMO_MODE) return;
  await page.emulateMedia({ colorScheme: 'dark' });
  await page
    .evaluate(() => {
      const root = document.documentElement;
      root.classList.add('dark');
      root.dataset.theme = 'dark';
      root.style.colorScheme = 'dark';
      if (document.body) {
        document.body.style.backgroundColor = '#0a0a0a';
        document.body.style.color = '#f4f4f5';
      }
      window.localStorage.setItem('gemba-theme', 'dark');
    })
    .catch(() => undefined);
}

export async function styleDemoTargetPage(page: Page): Promise<void> {
  if (!DEMO_MODE) return;
  await page
    .evaluate(() => {
      const root = document.documentElement;
      root.style.backgroundColor = '#0a0a0a';
      root.style.colorScheme = 'dark';
      if (document.body) {
        document.body.style.backgroundColor = '#0a0a0a';
        document.body.style.color = '#f4f4f5';
      }
      const mount = document.querySelector<HTMLElement>('#root, [data-testid="app-root"]');
      if (mount) {
        mount.style.color = '#f4f4f5';
        mount.style.minHeight = 'calc(100vh - 64px)';
      }
      const appRoot = document.querySelector<HTMLElement>('[data-testid="app-root"]');
      if (appRoot && appRoot.textContent?.trim() === 'Hello world') {
        appRoot.style.display = 'inline-flex';
        appRoot.style.alignItems = 'center';
        appRoot.style.justifyContent = 'center';
        appRoot.style.minWidth = '360px';
        appRoot.style.minHeight = '160px';
        appRoot.style.border = '1px solid #334155';
        appRoot.style.borderRadius = '12px';
        appRoot.style.background = '#111827';
        appRoot.style.boxShadow = '0 18px 50px rgba(0,0,0,0.35)';
        appRoot.style.fontSize = '32px';
        appRoot.style.fontWeight = '700';
      }
    })
    .catch(() => undefined);
}

/**
 * Update the on-screen caption to `text`. No-op when DEMO_MODE is
 * off. Kept best-effort: a navigation that races the evaluate is
 * silently dropped.
 */
export async function setDemoCaption(page: Page, text: string): Promise<void> {
  if (!DEMO_MODE) return;
  await page
    .evaluate(
      ({ id, body }: { id: string; body: string }) => {
        const el = document.getElementById(id);
        if (!el) return;
        el.textContent = body;
        el.style.opacity = '1';
      },
      { id: BANNER_ID, body: text },
    )
    .catch(() => undefined);
}

/**
 * Pause for the given number of milliseconds, but only when DEMO_MODE
 * is on. Use at known frames (post-import, post-build, post-oracle)
 * so the viewer's eye can land.
 */
export async function demoPause(ms: number): Promise<void> {
  if (!DEMO_MODE) return;
  await new Promise((r) => setTimeout(r, ms));
}

export async function demoDragTo(
  page: Page,
  source: Locator,
  target: Locator,
  opts: { steps?: number } = {},
): Promise<void> {
  const steps = opts.steps ?? 14;
  await source.scrollIntoViewIfNeeded();
  await target.scrollIntoViewIfNeeded();

  const sBox = await source.boundingBox();
  const tBox = await target.boundingBox();
  if (!sBox || !tBox) {
    throw new Error(
      `demoDragTo: source or target has no bounding box (source=${!!sBox}, target=${!!tBox})`,
    );
  }

  const sx = sBox.x + sBox.width / 2;
  const sy = sBox.y + sBox.height / 2;
  const tx = tBox.x + tBox.width / 2;
  const ty = tBox.y + tBox.height / 2;

  await page.mouse.move(sx, sy);
  await page.mouse.down();
  await page.mouse.move(sx + 8, sy + 8);
  for (let i = 1; i <= steps; i += 1) {
    const t = i / steps;
    await page.mouse.move(sx + (tx - sx) * t, sy + (ty - sy) * t, { steps: 1 });
  }
  await page.mouse.up();
}
