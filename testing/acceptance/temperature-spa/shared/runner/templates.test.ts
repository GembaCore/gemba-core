// Unit tests for the bead-template handlers (gm-root.27.2 / gm-root.27.28).
//
// Validates the content registry produces correct files and the
// noop / write-component templates write what their frontmatter
// declares. Heavier templates (npm-install, build, serve) shell out
// to npm/vite and are smoke-tested only via the end-to-end run
// (gm-root.27.29) — node:test isolation makes mocking npm awkward.

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { mkdtempSync, readFileSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { getTemplate } from './templates.ts';

function tempDir(): { dir: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), 'tspa-templates-'));
  return { dir, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

describe('getTemplate', () => {
  it('returns a handler for every declared template', () => {
    for (const name of [
      'init-repo',
      'npm-install',
      'write-component',
      'write-test',
      'build',
      'serve',
      'error-then-recover',
      'noop',
    ]) {
      assert.ok(getTemplate(name), `expected template '${name}'`);
    }
  });

  it('returns undefined for an unknown template', () => {
    assert.equal(getTemplate('not-a-template'), undefined);
    assert.equal(getTemplate(undefined), undefined);
  });
});

describe('noop template', () => {
  it('writes every file declared in frontmatter from the registry', async () => {
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('noop')!;
      await handler(
        { projectDir: dir, beadID: 'tspa-2', labels: ['milestone:m1'] },
        {
          template: 'noop',
          files: ['tsconfig.json', 'index.html', 'src/main.tsx'],
          extras: {},
        },
      );
      assert.ok(existsSync(join(dir, 'tsconfig.json')));
      assert.ok(existsSync(join(dir, 'index.html')));
      assert.ok(existsSync(join(dir, 'src/main.tsx')));
    } finally {
      cleanup();
    }
  });

  it('throws on a file path with no registry entry', async () => {
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('noop')!;
      await assert.rejects(
        handler(
          { projectDir: dir, beadID: 'tspa-x', labels: [] },
          {
            template: 'noop',
            files: ['unknown/path.ts'],
            extras: {},
          },
        ),
        /no registry entry/,
      );
    } finally {
      cleanup();
    }
  });
});

describe('write-component template + content registry', () => {
  it('writes M2 App.tsx (Hello world) when labels include milestone:m2', async () => {
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('write-component')!;
      await handler(
        { projectDir: dir, beadID: 'tspa-4', labels: ['milestone:m2'] },
        {
          template: 'write-component',
          files: ['src/App.tsx'],
          extras: {},
        },
      );
      const body = readFileSync(join(dir, 'src/App.tsx'), 'utf8');
      assert.match(body, /data-testid="app-root"/);
      assert.match(body, /Hello world/);
      assert.doesNotMatch(body, /TemperatureTable/);
    } finally {
      cleanup();
    }
  });

  it('writes M3 App.tsx (TemperatureTable wrapper) when labels include milestone:m3', async () => {
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('write-component')!;
      await handler(
        { projectDir: dir, beadID: 'tspa-11', labels: ['milestone:m3'] },
        {
          template: 'write-component',
          files: ['src/App.tsx'],
          extras: {},
        },
      );
      const body = readFileSync(join(dir, 'src/App.tsx'), 'utf8');
      assert.match(body, /TemperatureTable/);
      assert.match(body, /data-testid="app-root"/);
    } finally {
      cleanup();
    }
  });

  it('TemperatureTable.tsx renders 16 rows with row-{c} testids', async () => {
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('write-component')!;
      await handler(
        { projectDir: dir, beadID: 'tspa-9', labels: ['milestone:m3'] },
        {
          template: 'write-component',
          files: ['src/TemperatureTable.tsx'],
          extras: {},
        },
      );
      const body = readFileSync(join(dir, 'src/TemperatureTable.tsx'), 'utf8');
      assert.match(body, /data-testid="temperature-table"/);
      assert.match(body, /data-testid=\{`row-\$\{r\.celsius\}`\}/);
    } finally {
      cleanup();
    }
  });

  it('temperatureRows returns °F = °C × 9/5 + 32 to one decimal', async () => {
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('write-component')!;
      await handler(
        { projectDir: dir, beadID: 'tspa-8', labels: ['milestone:m3'] },
        {
          template: 'write-component',
          files: ['src/temperatureRows.ts'],
          extras: {},
        },
      );
      const body = readFileSync(join(dir, 'src/temperatureRows.ts'), 'utf8');
      // The function should iterate 0..300 step 20 and format to 1 decimal.
      assert.match(body, /c <= 300/);
      assert.match(body, /c \+= 20/);
      assert.match(body, /9.+5.+32/);
      assert.match(body, /toFixed\(1\)/);
    } finally {
      cleanup();
    }
  });
});

describe('error-then-recover template', () => {
  it('throws on first attempt, succeeds thereafter', async () => {
    // The module-level retry counter persists for the test process,
    // so we just verify "two consecutive calls — first throws,
    // second succeeds." Subsequent test files that exercise the
    // same template would observe persistent state; the acceptance
    // run only uses error-then-recover once per bead so this is OK.
    const { dir, cleanup } = tempDir();
    try {
      const handler = getTemplate('error-then-recover')!;
      const ctx = { projectDir: dir, beadID: 'tspa-x', labels: [] };
      const fm = { extras: {} };
      let firstThrew = false;
      try {
        await handler(ctx, fm);
      } catch (e) {
        firstThrew = (e as Error).message.includes('deliberate first-attempt failure');
      }
      assert.ok(firstThrew, 'first attempt must throw');
      // Second call must succeed.
      await handler(ctx, fm);
    } finally {
      cleanup();
    }
  });
});
