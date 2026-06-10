// helpers/dragdrop.ts — synthetic pointer sequence for @dnd-kit drag
// (gm-5v8v.5).
//
// Why this isn't `Locator.dragTo()`: Playwright's built-in dragTo
// dispatches HTML5 drag events (dragstart / dragover / drop), which
// dnd-kit's PointerSensor ignores. dnd-kit listens for raw pointer
// events with a small activation distance, so the spec needs to:
//
//   1. mousedown on the source
//   2. move at least the activation distance to "claim" the drag
//   3. continue moving towards the target's center in steps so
//      collision detection has intermediate positions to reason about
//   4. mouseup on the target

import type { Locator, Page } from '@playwright/test';

export interface DragOptions {
  // Number of intermediate move events along the drag path. dnd-kit
  // needs at least one move past the activation distance (default 5px)
  // before it commits to the drag; 10 gives collision detection
  // enough samples to settle on the target droppable.
  steps?: number;
}

const DEFAULT_STEPS = 10;

// dragTo drives a dnd-kit-friendly drag from `source` to the centre
// of `target`. The Page is required because Playwright doesn't expose
// page-level mouse events on Locator alone.
export async function dragTo(
  page: Page,
  source: Locator,
  target: Locator,
  opts: DragOptions = {}
): Promise<void> {
  const steps = opts.steps ?? DEFAULT_STEPS;

  await source.scrollIntoViewIfNeeded();
  await target.scrollIntoViewIfNeeded();

  const sBox = await source.boundingBox();
  const tBox = await target.boundingBox();
  if (!sBox || !tBox) {
    throw new Error(
      `dragTo: source or target has no bounding box (source=${!!sBox}, target=${!!tBox})`
    );
  }
  const sx = sBox.x + sBox.width / 2;
  const sy = sBox.y + sBox.height / 2;
  const tx = tBox.x + tBox.width / 2;
  const ty = tBox.y + tBox.height / 2;

  // 1. mousedown on the source.
  await page.mouse.move(sx, sy);
  await page.mouse.down();
  // 2. move past dnd-kit's activation distance (5px) on the FIRST
  //    intermediate. Without this nudge, a "drag" that's already at
  //    the target center on step 1 never crosses activation.
  await page.mouse.move(sx + 8, sy + 8);
  // 3. interpolated path to the target.
  for (let i = 1; i <= steps; i++) {
    const t = i / steps;
    const x = sx + (tx - sx) * t;
    const y = sy + (ty - sy) * t;
    await page.mouse.move(x, y, { steps: 1 });
  }
  // 4. release on the target.
  await page.mouse.up();
}
