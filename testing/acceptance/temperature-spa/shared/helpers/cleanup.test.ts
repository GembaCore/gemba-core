// Unit tests for the cleanup orchestrator (gm-root.27.20).
//
// Heavy stress tests live alongside the variant specs once .14 +
// .17 land their concrete cleanupNative / cleanupGastown helpers.
// Here we exercise the orchestrator's invariants in isolation:
// reverse-order teardown, panic resilience, timeout fallback, and
// warning surfacing.

import { strict as assert } from 'node:assert';
import {
  existsSync,
  mkdtempSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, it } from 'node:test';

import {
  createCleanupRegistry,
  registerTempDir,
  runWithCleanup,
  type CleanupWarning,
  type TestT,
} from './cleanup.ts';

const noopT: TestT = {};

describe('createCleanupRegistry', () => {
  it('runs callbacks in reverse-allocation order', async () => {
    const order: string[] = [];
    const reg = createCleanupRegistry();
    reg.add({ kind: 'port', label: 'a', fn: () => void order.push('a') });
    reg.add({ kind: 'port', label: 'b', fn: () => void order.push('b') });
    reg.add({ kind: 'port', label: 'c', fn: () => void order.push('c') });

    const warnings = await reg.run();
    assert.deepEqual(order, ['c', 'b', 'a']);
    assert.equal(warnings.length, 0);
  });

  it('surfaces failures as warnings without aborting subsequent cleanup', async () => {
    const order: string[] = [];
    const reg = createCleanupRegistry();
    reg.add({ kind: 'port', label: 'first', fn: () => void order.push('first') });
    reg.add({
      kind: 'process',
      label: 'mid',
      fn: () => {
        throw new Error('boom');
      },
    });
    reg.add({ kind: 'port', label: 'last', fn: () => void order.push('last') });

    const warnings = await reg.run();
    assert.deepEqual(order, ['last', 'first']);
    assert.equal(warnings.length, 1);
    assert.equal(warnings[0]?.label, 'mid');
    assert.equal(warnings[0]?.error.message, 'boom');
  });
});

describe('runWithCleanup', () => {
  it('runs cleanup on success', async () => {
    let cleaned = false;
    const result = await runWithCleanup(noopT, async ({ cleanup }) => {
      cleanup.add({ kind: 'port', label: 'p', fn: () => void (cleaned = true) });
    });
    assert.equal(cleaned, true);
    assert.equal(result.timedOut, false);
  });

  it('runs cleanup on body throw and re-throws', async () => {
    let cleaned = false;
    await assert.rejects(
      runWithCleanup(noopT, async ({ cleanup }) => {
        cleanup.add({
          kind: 'port',
          label: 'p',
          fn: () => void (cleaned = true),
        });
        throw new Error('test failure');
      }),
      /test failure/
    );
    assert.equal(cleaned, true);
  });

  it('runs cleanup on timeout and reports timedOut', async () => {
    let cleaned = false;
    await assert.rejects(
      runWithCleanup(
        noopT,
        async ({ cleanup }) => {
          cleanup.add({
            kind: 'port',
            label: 'p',
            fn: () => void (cleaned = true),
          });
          await new Promise<void>(() => {
            /* never resolves */
          });
        },
        { timeoutMs: 50 }
      ),
      /exceeded timeout of 50ms/
    );
    assert.equal(cleaned, true);
  });

  it('forwards warnings to onCleanupWarning', async () => {
    const seen: CleanupWarning[] = [];
    await runWithCleanup(
      noopT,
      async ({ cleanup }) => {
        cleanup.add({
          kind: 'process',
          label: 'flaky',
          fn: () => {
            throw new Error('SIGKILL failed');
          },
        });
      },
      { onCleanupWarning: (w) => seen.push(w) }
    );
    assert.equal(seen.length, 1);
    assert.equal(seen[0]?.label, 'flaky');
  });
});

describe('registerTempDir', () => {
  it('removes the directory at cleanup time', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'gemba-cleanup-test-'));
    writeFileSync(join(dir, 'file'), 'x', 'utf8');
    assert.ok(existsSync(dir));

    const reg = createCleanupRegistry();
    registerTempDir(reg, dir);
    await reg.run();

    assert.ok(!existsSync(dir));
  });

  it('is a no-op when the directory does not exist', async () => {
    const reg = createCleanupRegistry();
    registerTempDir(reg, join(tmpdir(), 'gemba-cleanup-nonexistent-xyz'));
    const warnings = await reg.run();
    assert.equal(warnings.length, 0);
  });
});

describe('parallel allocation', () => {
  // Light-touch version of the .27.20 stress test. The full
  // 50-pair native+gastown matrix runs once those variants exist;
  // here we just confirm the orchestrator itself doesn't leak
  // when many runs happen concurrently.
  it('handles 50 concurrent runs without losing cleanups', async () => {
    let live = 0;
    let peak = 0;

    const runs = Array.from({ length: 50 }, async () => {
      await runWithCleanup(noopT, async ({ cleanup }) => {
        live++;
        peak = Math.max(peak, live);
        cleanup.add({
          kind: 'port',
          label: 'fake',
          fn: () => {
            live--;
          },
        });
      });
    });

    await Promise.all(runs);
    assert.equal(live, 0);
    assert.ok(peak > 1, 'should overlap at least slightly');
  });
});

// Avoid leaking the import (the runtime hardly cares but keeps
// the bundle list explicit for readers).
void rmSync;
