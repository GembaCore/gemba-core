// Unit tests for the report writer (gm-root.27.18).

import { strict as assert } from 'node:assert';
import { mkdtempSync, readFileSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, it, before, after } from 'node:test';

import { writeReport, __internal } from './report.ts';
import type { ProjectHandle } from './bootstrap.ts';
import type { AcceptanceResults } from './types.ts';

// Stand-in for the bootstrap ProjectHandle. Avoids dragging the
// real bootstrap module in (it imports @e2e/* aliases that the
// node:test runtime can't resolve).
function fixtureHandle(): ProjectHandle {
  return {
    baseURL: 'http://127.0.0.1:51001',
    projectDir: '/tmp/gemba-acceptance-fixture',
    projectName: 'tspa-test',
    cleanup: async () => {},
    server: {} as ProjectHandle['server'],
  };
}

function fixtureResults(): AcceptanceResults {
  return {
    variant: 'native',
    agentMode: 'mock',
    rig: 'local',
    gembaCommitSHA: 'deadbeef0000',
    targetProjectSHA: 'cafef00d0000',
    startedAt: '2026-04-30T10:00:00.000Z',
    endedAt: '2026-04-30T10:02:30.000Z',
    beadTransitions: [
      {
        beadID: 'tgt-1',
        from: null,
        to: 'open',
        at: '2026-04-30T10:00:05.000Z',
      },
      {
        beadID: 'tgt-1',
        from: 'open',
        to: 'closed',
        at: '2026-04-30T10:01:00.000Z',
      },
    ],
    milestones: [
      {
        id: 'M1',
        startedAt: '2026-04-30T10:00:00.000Z',
        endedAt: '2026-04-30T10:01:30.000Z',
        durationMs: 90_000,
      },
    ],
    escalations: [
      {
        id: 'esc-1',
        injectedAt: '2026-04-30T10:01:00.000Z',
        resolvedAt: '2026-04-30T10:01:15.000Z',
        durationMs: 15_000,
        resolved: true,
      },
    ],
    assertions: [
      { name: 'M1:scaffolding-present', passed: true },
      {
        name: 'M3:row-count',
        passed: false,
        expected: 16,
        got: 15,
        message: 'one row missing',
        filedBeadID: 'gm-fake-1',
        screenshot: '/tmp/shot.png',
      },
    ],
    filedBugs: ['gm-fake-1'],
  };
}

describe('writeReport', () => {
  let tmp: string;

  before(() => {
    tmp = mkdtempSync(join(tmpdir(), 'gemba-report-test-'));
  });

  after(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  it('writes JSON + markdown into a per-run dir', () => {
    const out = writeReport(fixtureHandle(), fixtureResults(), {
      reportsRoot: tmp,
      dirOverride: 'fixture-dir',
    });

    assert.equal(out.dir, join(tmp, 'fixture-dir'));
    assert.ok(existsSync(out.jsonPath));
    assert.ok(existsSync(out.markdownPath));

    const json = JSON.parse(readFileSync(out.jsonPath, 'utf8'));
    assert.equal(json.variant, 'native');
    assert.equal(json.assertions.length, 2);
    assert.equal(json.filedBugs[0], 'gm-fake-1');
  });

  it('markdown includes status summary, milestone section, and bugs filed list', () => {
    const out = writeReport(fixtureHandle(), fixtureResults(), {
      reportsRoot: tmp,
      dirOverride: 'fixture-md',
    });
    const md = readFileSync(out.markdownPath, 'utf8');

    assert.match(md, /## Status summary/);
    assert.match(md, /\| Outcome \| ❌ FAIL \(1 assertion\) \|/);
    assert.match(md, /## M1/);
    assert.match(md, /## Bugs filed/);
    assert.match(md, /- gm-fake-1/);
    assert.match(md, /M3:row-count/);
    assert.match(md, /expected `16`, got `15`/);
  });

  it('renders ✅ PASS when all assertions passed', () => {
    const happy = fixtureResults();
    happy.assertions = [{ name: 'M1:ok', passed: true }];
    happy.filedBugs = [];
    const out = writeReport(fixtureHandle(), happy, {
      reportsRoot: tmp,
      dirOverride: 'fixture-pass',
    });
    const md = readFileSync(out.markdownPath, 'utf8');
    assert.match(md, /✅ PASS/);
    assert.match(md, /_No beads filed\._/);
  });

  it('renders CRASHED when fatalError is set', () => {
    const crashed = fixtureResults();
    crashed.fatalError = { message: 'process exited 1', stack: 'at foo' };
    const out = writeReport(fixtureHandle(), crashed, {
      reportsRoot: tmp,
      dirOverride: 'fixture-crash',
    });
    const md = readFileSync(out.markdownPath, 'utf8');
    assert.match(md, /❌ CRASHED/);
    assert.match(md, /process exited 1/);
  });

  it('builds default dir name as YYYY-MM-DD-variant', () => {
    const name = __internal.buildDirName(fixtureResults());
    assert.equal(name, '2026-04-30-native');
  });

  it('formats durations in human units', () => {
    assert.equal(__internal.formatDurationMs(250), '250ms');
    assert.equal(__internal.formatDurationMs(2_500), '2.50s');
    assert.equal(__internal.formatDurationMs(95_000), '1m35.0s');
  });
});
