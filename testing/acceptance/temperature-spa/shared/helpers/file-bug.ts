// Bug-filing helper (gm-root.27.19).
//
// Files a bead in the gemba rig (NOT the target rig) when the
// acceptance suite hits an assertion failure. The bead becomes the
// durable trail for the failure: title carries the test + assertion,
// body carries enough to reproduce.

import { spawn } from 'node:child_process';
import { homedir } from 'node:os';
import { join } from 'node:path';

import type { ProjectHandle } from './bootstrap.ts';
import type { AcceptanceResults } from './types.ts';

export interface FileBugAssertion {
  // Short label like "M3:row-count" or "triage:resolved". Lands in
  // the bead title verbatim.
  label: string;
  expected?: unknown;
  got?: unknown;
  message?: string;
  stack?: string;
  screenshot?: string;
  // "build failed" → CRITICAL; everything else → P2 by default.
  classification?: 'standard' | 'build-failed';
}

export interface FileBugOptions {
  testName: string;
  assertion: FileBugAssertion;
  // Bootstrap handle — used for projectDir + baseURL pointers in
  // the bead body so a triager can hop to the run.
  handle: ProjectHandle;
  // Run results — supplies variant / agent mode / gemba SHA, the
  // triage metadata the bead body documents.
  results: AcceptanceResults;
  // Override the rig the bead lands in. Defaults to env
  // GEMBA_BUG_RIG and falls back to "gemba".
  rig?: string;
  // Override the gt workspace root. Defaults to env GT_HOME and
  // falls back to ~/gt.
  gtHome?: string;
  // Test override: passes --db to bd instead of cwd-resolving.
  // Used by the unit test against an isolated database.
  dbPath?: string;
  // Test override: bd binary path (default: looks up PATH).
  bdBin?: string;
}

export interface FileBugResult {
  beadID: string;
  rig: string;
}

export async function fileBug(opts: FileBugOptions): Promise<FileBugResult> {
  const rig =
    opts.rig ?? process.env.GEMBA_BUG_RIG ?? 'gemba';
  const gtHome =
    opts.gtHome ?? process.env.GT_HOME ?? join(homedir(), 'gt');

  const title = buildTitle(opts.testName, opts.assertion);
  const body = buildBody(opts);
  const priority = pickPriority(opts.assertion);

  const args: string[] = [
    'create',
    '--title',
    title,
    '--type',
    'bug',
    '--priority',
    priority,
    '--labels',
    'area:acceptance,kind:bug,from:gm-root.27',
    '--silent',
    '--body-file',
    '-',
  ];

  if (opts.dbPath) {
    args.push('--db', opts.dbPath);
  }

  const cwd = opts.dbPath ? process.cwd() : join(gtHome, rig);
  const bin = opts.bdBin ?? 'bd';

  const stdout = await runBd(bin, args, cwd, body);
  const beadID = stdout.trim();
  if (!beadID) {
    throw new Error('bd create returned empty bead id');
  }
  return { beadID, rig };
}

function runBd(
  bin: string,
  args: string[],
  cwd: string,
  body: string
): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, args, {
      cwd,
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (chunk) => {
      stdout += chunk;
    });
    child.stderr.on('data', (chunk) => {
      stderr += chunk;
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve(stdout);
      } else {
        reject(
          new Error(
            `bd create exited ${code}: ${stderr.trim() || '(no stderr)'}`
          )
        );
      }
    });
    child.stdin.end(body);
  });
}

function buildTitle(
  testName: string,
  assertion: FileBugAssertion
): string {
  return `[acceptance-test] ${testName}: ${assertion.label}`;
}

function buildBody(opts: FileBugOptions): string {
  const { handle, results, assertion, testName } = opts;
  const lines: string[] = [];

  lines.push('# Acceptance failure');
  lines.push('');
  lines.push(`Test: \`${testName}\``);
  lines.push(`Assertion: \`${assertion.label}\``);
  lines.push('');
  lines.push('## Run context');
  lines.push('');
  lines.push(`- Variant: \`${results.variant}\``);
  lines.push(`- Agent mode: \`${results.agentMode}\``);
  lines.push(`- Rig: \`${results.rig}\``);
  lines.push(
    `- Gemba commit: \`${results.gembaCommitSHA || '(unknown)'}\``
  );
  if (results.targetProjectSHA) {
    lines.push(`- Target SHA: \`${results.targetProjectSHA}\``);
  }
  lines.push(`- Project: \`${handle.projectName}\``);
  lines.push(`- Project dir: \`${handle.projectDir}\``);
  lines.push(`- Base URL: \`${handle.baseURL}\``);

  if (assertion.expected !== undefined || assertion.got !== undefined) {
    lines.push('');
    lines.push('## Expected vs got');
    lines.push('');
    lines.push('```');
    lines.push(`expected: ${formatValue(assertion.expected)}`);
    lines.push(`got:      ${formatValue(assertion.got)}`);
    lines.push('```');
  }

  if (assertion.message) {
    lines.push('');
    lines.push('## Message');
    lines.push('');
    lines.push(assertion.message);
  }

  if (assertion.screenshot) {
    lines.push('');
    lines.push('## Screenshot');
    lines.push('');
    lines.push(`\`${assertion.screenshot}\``);
  }

  if (assertion.stack) {
    lines.push('');
    lines.push('## Stack trace');
    lines.push('');
    lines.push('```');
    lines.push(assertion.stack);
    lines.push('```');
  }

  return lines.join('\n') + '\n';
}

function pickPriority(assertion: FileBugAssertion): string {
  return assertion.classification === 'build-failed' ? '0' : '2';
}

function formatValue(v: unknown): string {
  if (v === undefined) return 'undefined';
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

export const __internal = {
  buildTitle,
  buildBody,
  pickPriority,
};
