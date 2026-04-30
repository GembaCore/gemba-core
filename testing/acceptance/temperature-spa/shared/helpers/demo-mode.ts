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

import type { Page } from '@playwright/test';

export const DEMO_MODE = process.env.GEMBA_ACCEPTANCE_DEMO_MODE === '1';

const BANNER_ID = 'gemba-demo-caption';

const INIT_SCRIPT = `
(() => {
  if (document.getElementById('${BANNER_ID}')) return;
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
})();
`;

/**
 * Install the caption banner once at page load. Idempotent — safe to
 * call repeatedly. No-op when DEMO_MODE is off.
 */
export async function installDemoBanner(page: Page): Promise<void> {
  if (!DEMO_MODE) return;
  await page.addInitScript(INIT_SCRIPT);
  // Also inject into the current document (addInitScript only runs
  // on next navigation; install now too so the first goto sees it).
  await page.evaluate(INIT_SCRIPT).catch(() => undefined);
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
