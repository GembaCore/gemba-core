// Unit tests for the bug-filing helper (gm-root.27.19).
//
// Strategy: replace the bd binary with a tiny shell stub that
// records argv + stdin into a tmp file and prints a deterministic
// bead id. This keeps the test hermetic — no real beads database
// is mutated and no network round-trip is needed.

import { strict as assert } from 'node:assert';
import {
  chmodSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, it, before, after } from 'node:test';

import { fileBug, __internal } from './file-bug.ts';
import type { ProjectHandle } from './bootstrap.ts';
import type { AcceptanceResults } from './types.ts';

const STUB_BEAD_ID = 'gm-stub42';

function fixtureHandle(): ProjectHandle {
  return {
    baseURL: 'http://127.0.0.1:51001',
    projectDir: '/tmp/gemba-acceptance-fixture',
    projectName: 'tspa-test',
    beadPrefix: 'tspa',
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
    beadTransitions: [],
    milestones: [],
    escalations: [],
    assertions: [],
    filedBugs: [],
  };
}

describe('fileBug', () => {
  let tmp: string;
  let stubPath: string;
  let argsLog: string;
  let stdinLog: string;

  before(() => {
    tmp = mkdtempSync(join(tmpdir(), 'gemba-filebug-test-'));
    stubPath = join(tmp, 'bd-stub.sh');
    argsLog = join(tmp, 'args.log');
    stdinLog = join(tmp, 'stdin.log');
    writeFileSync(
      stubPath,
      `#!/usr/bin/env bash
set -e
printf '%s\\n' "$@" > "${argsLog}"
cat > "${stdinLog}"
echo "${STUB_BEAD_ID}"
`,
      'utf8'
    );
    chmodSync(stubPath, 0o755);
  });

  after(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  it('returns the bead id printed by bd create', async () => {
    const result = await fileBug({
      testName: 'native/M3-conversion-table',
      assertion: {
        label: 'M3:row-count',
        expected: 16,
        got: 15,
        message: 'one row missing',
        stack: 'Error: row missing\n    at ...',
      },
      handle: fixtureHandle(),
      results: fixtureResults(),
      bdBin: stubPath,
      dbPath: join(tmp, 'fake.db'),
    });

    assert.equal(result.beadID, STUB_BEAD_ID);
    assert.equal(result.rig, 'gemba');
  });

  it('passes the expected flags + body to bd create', async () => {
    await fileBug({
      testName: 'native/M3-conversion-table',
      assertion: {
        label: 'M3:row-count',
        expected: 16,
        got: 15,
        message: 'row missing',
        stack: 'Error: row missing\n    at foo.ts:1',
        screenshot: '/tmp/shot.png',
      },
      handle: fixtureHandle(),
      results: fixtureResults(),
      bdBin: stubPath,
      dbPath: join(tmp, 'fake.db'),
    });

    const args = readFileSync(argsLog, 'utf8').trim().split('\n');
    assert.equal(args[0], 'create');
    assert.ok(args.includes('--type'));
    assert.equal(args[args.indexOf('--type') + 1], 'bug');
    assert.ok(args.includes('--priority'));
    assert.equal(args[args.indexOf('--priority') + 1], '2');
    assert.ok(args.includes('--silent'));
    assert.ok(args.includes('--labels'));
    assert.equal(
      args[args.indexOf('--labels') + 1],
      'area:acceptance,kind:bug,from:gm-root.27'
    );

    const titleIdx = args.indexOf('--title');
    assert.ok(titleIdx >= 0);
    assert.equal(
      args[titleIdx + 1],
      '[acceptance-test] native/M3-conversion-table: M3:row-count'
    );

    const body = readFileSync(stdinLog, 'utf8');
    assert.match(body, /Variant: `native`/);
    assert.match(body, /Gemba commit: `deadbeef0000`/);
    assert.match(body, /Target SHA: `cafef00d0000`/);
    assert.match(body, /expected: 16/);
    assert.match(body, /got:      15/);
    assert.match(body, /## Stack trace/);
    assert.match(body, /## Screenshot/);
  });

  it('uses P0 for build-failed classification', async () => {
    await fileBug({
      testName: 'native/M2-hello-world',
      assertion: {
        label: 'build:failed',
        message: 'gemba serve exited 1',
        classification: 'build-failed',
      },
      handle: fixtureHandle(),
      results: fixtureResults(),
      bdBin: stubPath,
      dbPath: join(tmp, 'fake.db'),
    });

    const args = readFileSync(argsLog, 'utf8').trim().split('\n');
    assert.equal(args[args.indexOf('--priority') + 1], '0');
  });

  it('respects GEMBA_BUG_RIG env override', async () => {
    const prev = process.env.GEMBA_BUG_RIG;
    process.env.GEMBA_BUG_RIG = 'gemba_prime';
    try {
      const result = await fileBug({
        testName: 't',
        assertion: { label: 'a' },
        handle: fixtureHandle(),
      results: fixtureResults(),
        bdBin: stubPath,
        dbPath: join(tmp, 'fake.db'),
      });
      assert.equal(result.rig, 'gemba_prime');
    } finally {
      if (prev === undefined) delete process.env.GEMBA_BUG_RIG;
      else process.env.GEMBA_BUG_RIG = prev;
    }
  });

  it('builds title in the [acceptance-test] <testName>: <assertion> form', () => {
    assert.equal(
      __internal.buildTitle('native/M1', {
        label: 'M1:project-tree',
      }),
      '[acceptance-test] native/M1: M1:project-tree'
    );
  });
});
