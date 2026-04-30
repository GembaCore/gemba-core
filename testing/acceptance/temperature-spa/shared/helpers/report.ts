// Test report writer (gm-root.27.18).
//
// Aggregates a variant run into a JSON snapshot + a human-readable
// markdown report and persists both into the per-run reports dir.

import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import type { ProjectHandle } from './bootstrap.ts';
import type {
  AcceptanceResults,
  AssertionOutcome,
  BeadTransition,
  MilestoneTiming,
} from './types.ts';

export interface WriteReportOptions {
  // Directory the report is persisted under. Falls back to the
  // documented `testing/acceptance/temperature-spa/reports/` root
  // when omitted, so smoke runs don't have to plumb anything.
  reportsRoot?: string;
  // Override for the dated dir name. Tests use this to get a
  // deterministic path; production runs let the helper pick.
  dirOverride?: string;
}

export interface WriteReportResult {
  jsonPath: string;
  markdownPath: string;
  dir: string;
}

const DEFAULT_REPORTS_ROOT = join(
  'testing',
  'acceptance',
  'temperature-spa',
  'reports'
);

export function writeReport(
  handle: ProjectHandle,
  results: AcceptanceResults,
  opts: WriteReportOptions = {}
): WriteReportResult {
  const finalized: AcceptanceResults = {
    ...results,
    endedAt: results.endedAt ?? new Date().toISOString(),
  };

  const root = opts.reportsRoot ?? DEFAULT_REPORTS_ROOT;
  const dirName = opts.dirOverride ?? buildDirName(finalized);
  const dir = join(root, dirName);
  mkdirSync(dir, { recursive: true });

  const jsonPath = join(dir, 'report.json');
  const markdownPath = join(dir, 'report.md');

  writeFileSync(jsonPath, JSON.stringify(finalized, null, 2) + '\n', 'utf8');
  writeFileSync(markdownPath, renderMarkdown(handle, finalized), 'utf8');

  return { jsonPath, markdownPath, dir };
}

function buildDirName(results: AcceptanceResults): string {
  const date = (results.endedAt ?? new Date().toISOString()).slice(0, 10);
  return `${date}-${results.variant}`;
}

function renderMarkdown(
  handle: ProjectHandle,
  results: AcceptanceResults
): string {
  const lines: string[] = [];
  const status = computeStatus(results);

  lines.push(`# Acceptance run: ${results.variant} (${results.agentMode})`);
  lines.push('');
  lines.push('## Status summary');
  lines.push('');
  lines.push('| Field | Value |');
  lines.push('| --- | --- |');
  lines.push(`| Outcome | ${status} |`);
  lines.push(`| Variant | ${results.variant} |`);
  lines.push(`| Agent mode | ${results.agentMode} |`);
  lines.push(`| Rig | ${results.rig} |`);
  lines.push(`| Project | ${handle.projectName} |`);
  lines.push(`| Project dir | \`${handle.projectDir}\` |`);
  lines.push(`| Base URL | ${handle.baseURL} |`);
  lines.push(`| Gemba SHA | ${results.gembaCommitSHA || '(unknown)'} |`);
  lines.push(`| Target SHA | ${results.targetProjectSHA ?? '(none)'} |`);
  lines.push(`| Started | ${results.startedAt} |`);
  lines.push(`| Ended | ${results.endedAt ?? '(not finalized)'} |`);
  lines.push(
    `| Wall clock | ${formatDurationMs(totalDurationMs(results))} |`
  );
  lines.push(`| Assertions | ${countAssertions(results.assertions)} |`);
  lines.push(`| Escalations | ${countEscalations(results.escalations)} |`);
  lines.push(`| Bugs filed | ${results.filedBugs.length} |`);

  if (results.fatalError) {
    lines.push('');
    lines.push('### Fatal error');
    lines.push('');
    lines.push('```');
    lines.push(results.fatalError.message);
    if (results.fatalError.stack) {
      lines.push('');
      lines.push(results.fatalError.stack);
    }
    lines.push('```');
  }

  const failures = results.assertions.filter((a) => !a.passed);
  if (failures.length > 0) {
    lines.push('');
    lines.push('## Failures');
    lines.push('');
    for (const a of failures) {
      lines.push(formatAssertionLine(a));
    }
  }

  for (const milestone of results.milestones) {
    lines.push('');
    lines.push(`## ${milestone.id}`);
    lines.push('');
    lines.push(
      `Wall clock: **${formatDurationMs(milestone.durationMs)}** — ${milestone.startedAt} → ${milestone.endedAt}`
    );

    const milestoneAssertions = assertionsForMilestone(
      results.assertions,
      milestone.id
    );
    if (milestoneAssertions.length > 0) {
      lines.push('');
      lines.push('### Assertions');
      lines.push('');
      for (const a of milestoneAssertions) {
        lines.push(formatAssertionLine(a));
      }
    }

    const milestoneTransitions = transitionsForMilestone(
      results.beadTransitions,
      milestone
    );
    if (milestoneTransitions.length > 0) {
      lines.push('');
      lines.push('### Bead transitions');
      lines.push('');
      lines.push('| Bead | From | To | At |');
      lines.push('| --- | --- | --- | --- |');
      for (const t of milestoneTransitions) {
        lines.push(
          `| ${t.beadID} | ${t.from ?? '(new)'} | ${t.to} | ${t.at} |`
        );
      }
    }
  }

  if (results.escalations.length > 0) {
    lines.push('');
    lines.push('## Escalations');
    lines.push('');
    lines.push('| ID | Injected | Resolved | Duration |');
    lines.push('| --- | --- | --- | --- |');
    for (const esc of results.escalations) {
      lines.push(
        `| ${esc.id} | ${esc.injectedAt} | ${esc.resolvedAt ?? '(unresolved)'} | ${
          esc.durationMs !== undefined ? formatDurationMs(esc.durationMs) : '—'
        } |`
      );
    }
  }

  lines.push('');
  lines.push('## Bugs filed');
  lines.push('');
  if (results.filedBugs.length === 0) {
    lines.push('_No beads filed._');
  } else {
    for (const id of results.filedBugs) {
      lines.push(`- ${id}`);
    }
  }

  return lines.join('\n') + '\n';
}

function computeStatus(results: AcceptanceResults): string {
  if (results.fatalError) return '❌ CRASHED';
  const failed = results.assertions.filter((a) => !a.passed).length;
  if (failed > 0) return `❌ FAIL (${failed} assertion${failed === 1 ? '' : 's'})`;
  return '✅ PASS';
}

function totalDurationMs(results: AcceptanceResults): number {
  if (!results.endedAt) return 0;
  const start = Date.parse(results.startedAt);
  const end = Date.parse(results.endedAt);
  if (Number.isNaN(start) || Number.isNaN(end)) return 0;
  return end - start;
}

function countAssertions(assertions: AssertionOutcome[]): string {
  const total = assertions.length;
  const failed = assertions.filter((a) => !a.passed).length;
  return `${total - failed}/${total} passed`;
}

function countEscalations(
  escalations: AcceptanceResults['escalations']
): string {
  const resolved = escalations.filter((e) => e.resolved).length;
  return `${escalations.length} injected, ${resolved} resolved`;
}

function formatAssertionLine(a: AssertionOutcome): string {
  const icon = a.passed ? '✅' : '❌';
  const parts: string[] = [`- ${icon} **${a.name}**`];
  if (!a.passed) {
    if (a.expected !== undefined || a.got !== undefined) {
      parts.push(
        ` — expected \`${formatValue(a.expected)}\`, got \`${formatValue(a.got)}\``
      );
    }
    if (a.message) parts.push(` — ${a.message}`);
    if (a.filedBeadID) parts.push(` (filed: ${a.filedBeadID})`);
    if (a.screenshot) parts.push(` [screenshot](${a.screenshot})`);
  }
  return parts.join('');
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

function formatDurationMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(2)}s`;
  const minutes = Math.floor(seconds / 60);
  const rem = seconds - minutes * 60;
  return `${minutes}m${rem.toFixed(1)}s`;
}

function assertionsForMilestone(
  assertions: AssertionOutcome[],
  milestoneID: string
): AssertionOutcome[] {
  // Convention: assertion names start with "<milestone>:" so the
  // report can group without an extra schema field. Names that
  // don't follow the convention end up only in the top summary.
  const prefix = `${milestoneID}:`;
  return assertions.filter((a) => a.name.startsWith(prefix));
}

function transitionsForMilestone(
  transitions: BeadTransition[],
  milestone: MilestoneTiming
): BeadTransition[] {
  const start = Date.parse(milestone.startedAt);
  const end = Date.parse(milestone.endedAt);
  if (Number.isNaN(start) || Number.isNaN(end)) return [];
  return transitions.filter((t) => {
    const at = Date.parse(t.at);
    return !Number.isNaN(at) && at >= start && at <= end;
  });
}

// Exported only for unit tests.
export const __internal = {
  buildDirName,
  computeStatus,
  formatDurationMs,
  renderMarkdown,
};
