// gm-root.27.17 — gastown variant teardown.
//
// Removes the per-run rig + polecat created by setup, then chains to
// cleanupNative (gm-root.27.14) for server / tempdir teardown. UI-driven
// removal is the happy path — drives the same RunCommandModal flow used
// at setup (gm-s47n.17). If the SPA buttons aren't reachable (server
// already dead, page in error state), fall back to `gt rig remove
// <name> --force` so we don't leak a rig.
//
// Idempotent: every step is independently safe to skip if state is
// already gone. The integration spec runs this 10× and asserts zero
// orphan rigs.

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import type { Page } from '@playwright/test';
import { cleanupNative, type NativeHandle } from '../native/cleanup';

const execFileP = promisify(execFile);

export type GastownHandle = NativeHandle & {
  /** Per-run rig the spec created via "+ New rig". */
  rigName: string;
  /** Polecat created via "+ New polecat" inside the rig. */
  polecatName: string;
  /** Live SPA page; absent if the test killed the browser before cleanup. */
  page?: Page;
};

export async function cleanupGastown(handle: GastownHandle): Promise<void> {
  const log = handle.log ?? ((m: string) => console.log(`[cleanupGastown] ${m}`));

  await removePolecat(handle, log);
  await removeRig(handle, log);
  await cleanupNative({ project: handle.project, log });
}

async function removePolecat(handle: GastownHandle, log: (m: string) => void): Promise<void> {
  if (await tryUiRemovePolecat(handle, log)) return;
  await cliRemovePolecat(handle, log);
}

async function removeRig(handle: GastownHandle, log: (m: string) => void): Promise<void> {
  if (await tryUiRemoveRig(handle, log)) return;
  await cliRemoveRig(handle, log);
}

async function tryUiRemovePolecat(handle: GastownHandle, log: (m: string) => void): Promise<boolean> {
  if (!handle.page || handle.page.isClosed()) {
    log('UI polecat removal skipped — page unavailable');
    return false;
  }
  try {
    await handle.page.goto(`${handle.project.baseURL}/settings/pools`, { timeout: 5_000 });
    const btn = handle.page.getByRole('button', {
      name: new RegExp(`remove polecat ${handle.polecatName}`, 'i'),
    });
    if ((await btn.count()) === 0) return false;
    await btn.first().click({ timeout: 5_000 });
    log(`UI removed polecat ${handle.polecatName}`);
    return true;
  } catch (err) {
    log(`UI polecat removal threw: ${(err as Error).message}`);
    return false;
  }
}

async function tryUiRemoveRig(handle: GastownHandle, log: (m: string) => void): Promise<boolean> {
  if (!handle.page || handle.page.isClosed()) {
    log('UI rig removal skipped — page unavailable');
    return false;
  }
  try {
    const btn = handle.page.getByRole('button', {
      name: new RegExp(`remove rig ${handle.rigName}`, 'i'),
    });
    if ((await btn.count()) === 0) return false;
    await btn.first().click({ timeout: 5_000 });
    log(`UI removed rig ${handle.rigName}`);
    return true;
  } catch (err) {
    log(`UI rig removal threw: ${(err as Error).message}`);
    return false;
  }
}

async function cliRemovePolecat(handle: GastownHandle, log: (m: string) => void): Promise<void> {
  try {
    await execFileP('gt', [
      'polecat',
      'remove',
      handle.polecatName,
      '--rig',
      handle.rigName,
      '--force',
    ]);
    log(`CLI removed polecat ${handle.polecatName}`);
  } catch (err) {
    log(`gt polecat remove failed (may already be gone): ${(err as Error).message}`);
  }
}

async function cliRemoveRig(handle: GastownHandle, log: (m: string) => void): Promise<void> {
  try {
    await execFileP('gt', ['rig', 'remove', handle.rigName, '--force']);
    log(`CLI removed rig ${handle.rigName}`);
  } catch (err) {
    log(`gt rig remove failed (may already be gone): ${(err as Error).message}`);
  }
}
