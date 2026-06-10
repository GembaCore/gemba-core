// helpers/hotkeys.ts — STUB
//
// 33 hotkeys to cover. The chrome tier (gm-5v8v.4) lands the impl —
// will likely wrap page.keyboard.press() with our chord map and a
// helper to assert the resulting UI state.

import type { Page } from '@playwright/test';

export async function press(_page: Page, _chord: string): Promise<void> {
  throw new Error('hotkeys helper not implemented — see gm-5v8v.4');
}
