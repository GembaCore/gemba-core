// Narrator tests (gm-root.27.37) — assert the JSON envelope shape +
// timing arithmetic + persist round-trip. Uses the @playwright/test
// runner that the rest of this package's tests use (no vitest in
// this workspace; see shared/pool-config.test.ts for the precedent).

import { test, expect } from '@playwright/test';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

import { Narrator, noopNarrator } from './narrator';

test.describe('Narrator (gm-root.27.37)', () => {
  test('records events with monotonic at_ms offsets', () => {
    let now = 1000;
    const n = new Narrator(() => now);

    n.emit('A fresh project boots', 'short');
    now += 2200;
    n.emit('The operator configures a pool', 'long');
    now += 4300;
    n.emit('Agents claim M1 work', 'medium');

    const events = n.snapshot();
    expect(events).toHaveLength(3);
    expect(events[0]).toEqual({
      at_ms: 0,
      phrase: 'A fresh project boots',
      duration_hint: 'short',
    });
    expect(events[1]).toEqual({
      at_ms: 2200,
      phrase: 'The operator configures a pool',
      duration_hint: 'long',
    });
    expect(events[2]).toEqual({
      at_ms: 6500,
      phrase: 'Agents claim M1 work',
      duration_hint: 'medium',
    });
  });

  test('default duration_hint is "medium"', () => {
    const n = new Narrator(() => 0);
    n.emit('Just a phrase');
    expect(n.snapshot()[0]?.duration_hint).toBe('medium');
  });

  test('build() returns the spec-shaped envelope', () => {
    let now = 0;
    const n = new Narrator(() => now);
    n.emit('A', 'short');
    now += 1500;
    n.emit('B', 'medium');
    now += 1500;

    const out = n.build({ video_path: 'demo-2026-04-30.mp4' });
    expect(out.video_path).toBe('demo-2026-04-30.mp4');
    expect(out.duration_ms).toBe(3000);
    expect(out.events).toHaveLength(2);
  });

  test('writeTo persists the JSON next to the target path', () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'narration-'));
    try {
      const n = new Narrator(() => 0);
      n.emit('hello', 'short');
      const out = path.join(dir, 'narration.json');
      n.writeTo(out, { video_path: '/tmp/v.mp4' });

      const parsed = JSON.parse(readFileSync(out, 'utf8'));
      expect(parsed.video_path).toBe('/tmp/v.mp4');
      expect(parsed.events).toEqual([
        { at_ms: 0, phrase: 'hello', duration_hint: 'short' },
      ]);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  test('noopNarrator drops emits silently', () => {
    expect(() => noopNarrator.emit('anything', 'short')).not.toThrow();
  });

  test('clamps at_ms to >= 0 even on clock rewind', () => {
    let now = 1000;
    const n = new Narrator(() => now);
    n.emit('first');
    now -= 500; // clock rewind shouldn't produce negative offsets
    n.emit('second');
    expect(n.snapshot()[1]?.at_ms).toBe(0);
  });
});
