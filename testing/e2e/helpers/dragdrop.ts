// helpers/dragdrop.ts — STUB
//
// @dnd-kit doesn't respond to native HTML5 drag events; specs need to
// dispatch synthetic pointer sequences. gm-5v8v.5 (board specs) lands
// the impl; this stub keeps the import path stable.

import type { Locator } from '@playwright/test';

export async function dragTo(_source: Locator, _target: Locator): Promise<void> {
  throw new Error('dragdrop helper not implemented — see gm-5v8v.5');
}
