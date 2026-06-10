// JSONL import primitives for the grid view (gm-e12.3 DoD piece 2).
//
// Pure functions only — no React, no fetch — so the planner can be
// unit-tested without a DOM, and the dialog can preview a diff before
// any network calls happen. The dialog uses planJsonlImport() to
// classify each line into create / update / skip, then commits via
// createWorkItem / updateWorkItem.

import type { WorkItem } from '@/types/core.gen';
import type { CreateWorkItemInput, WorkItemPatch } from '@/api/workItems';

// Defaults applied when a JSONL row omits one of the four fields the
// backend create surface requires (kind / title / status / state_category).
// Title has no default — a row without title is skipped — but the
// other three round out into the conventional "new task in backlog"
// shape so the operator's bulk import doesn't have to repeat boilerplate.
const DEFAULT_KIND = 'task';
const DEFAULT_STATUS = 'open';
const DEFAULT_STATE_CATEGORY: WorkItem['state_category'] = 'backlog';

// FIELDS the importer accepts on each JSONL row. Anything else trips
// "skipped: unknown_field" so a typo in the source data doesn't get
// silently dropped on the floor (mirrors the server's
// DisallowUnknownFields contract for POST/PATCH).
const ALLOWED_FIELDS = new Set([
  'id',
  'kind',
  'title',
  'description',
  'status',
  'state_category',
  'priority',
  'labels',
  // Note: assignee / owner / custom / sprint_id are deliberately not
  // on the import surface yet. Sprints stay in the drawer; assignees
  // need an AgentRef which has more shape than a JSONL row should
  // carry inline. Adding any of these later is additive — drop the
  // field name in here and extend toPatch / toCreate.
]);

export type ImportAction =
  | { kind: 'create'; line: number; payload: CreateWorkItemInput }
  | { kind: 'update'; line: number; id: string; patch: WorkItemPatch }
  | { kind: 'skip'; line: number; reason: string; raw: string };

export interface ImportPlan {
  actions: ImportAction[];
  // counts is a precomputed convenience for the dialog header — same
  // information you'd derive by scanning actions, but every render of
  // the diff summary would otherwise repeat the work.
  counts: { create: number; update: number; skip: number };
}

// planJsonlImport parses the input as JSONL (one JSON object per line)
// and classifies each row against the existing items. Blank lines are
// ignored (not skipped) so an editor's trailing newline doesn't pollute
// the skipped count.
export function planJsonlImport(input: string, existing: WorkItem[]): ImportPlan {
  const knownIds = new Set(existing.map((it) => it.id));
  const actions: ImportAction[] = [];

  const lines = input.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const trimmed = raw.trim();
    if (trimmed === '') continue;
    const line = i + 1;

    let parsed: unknown;
    try {
      parsed = JSON.parse(trimmed);
    } catch (err) {
      actions.push({
        kind: 'skip',
        line,
        reason: `parse error: ${(err as Error).message}`,
        raw: trimmed,
      });
      continue;
    }

    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      actions.push({
        kind: 'skip',
        line,
        reason: 'row is not a JSON object',
        raw: trimmed,
      });
      continue;
    }
    const row = parsed as Record<string, unknown>;

    const unknown = Object.keys(row).filter((k) => !ALLOWED_FIELDS.has(k));
    if (unknown.length > 0) {
      actions.push({
        kind: 'skip',
        line,
        reason: `unknown field(s): ${unknown.join(', ')}`,
        raw: trimmed,
      });
      continue;
    }

    const id = typeof row.id === 'string' && row.id.trim() !== '' ? row.id : undefined;

    if (id && knownIds.has(id)) {
      const patch = toPatch(row);
      if (Object.keys(patch).length === 0) {
        actions.push({
          kind: 'skip',
          line,
          reason: 'update has no fields to change',
          raw: trimmed,
        });
        continue;
      }
      actions.push({ kind: 'update', line, id, patch });
      continue;
    }

    // No id, or an id that doesn't exist on the server → create. The
    // adaptor will assign its own id; an explicit id in the JSONL is
    // discarded for creates because backends like Beads own the id
    // namespace (see gm-root DD-9).
    const title = typeof row.title === 'string' ? row.title.trim() : '';
    if (title === '') {
      actions.push({
        kind: 'skip',
        line,
        reason: 'create requires a non-empty title',
        raw: trimmed,
      });
      continue;
    }
    const payload = toCreate(row, title);
    actions.push({ kind: 'create', line, payload });
  }

  const counts = { create: 0, update: 0, skip: 0 };
  for (const a of actions) counts[a.kind]++;
  return { actions, counts };
}

function toPatch(row: Record<string, unknown>): WorkItemPatch {
  const p: WorkItemPatch = {};
  if (typeof row.title === 'string') p.title = row.title;
  if (typeof row.description === 'string') p.description = row.description;
  if (typeof row.status === 'string') p.status = row.status;
  if (typeof row.state_category === 'string') {
    p.state_category = row.state_category as WorkItemPatch['state_category'];
  }
  if (row.priority === null) p.priority = null;
  else if (typeof row.priority === 'number') p.priority = row.priority;
  if (Array.isArray(row.labels)) {
    p.labels = row.labels.filter((x): x is string => typeof x === 'string');
  }
  return p;
}

function toCreate(row: Record<string, unknown>, title: string): CreateWorkItemInput {
  const c: CreateWorkItemInput = {
    title,
    kind:
      typeof row.kind === 'string' && row.kind.trim() !== ''
        ? row.kind
        : DEFAULT_KIND,
    status: typeof row.status === 'string' && row.status !== ''
      ? row.status
      : DEFAULT_STATUS,
    state_category:
      typeof row.state_category === 'string' && row.state_category !== ''
        ? (row.state_category as CreateWorkItemInput['state_category'])
        : DEFAULT_STATE_CATEGORY,
  };
  if (typeof row.description === 'string') c.description = row.description;
  if (typeof row.priority === 'number') c.priority = row.priority;
  if (Array.isArray(row.labels)) {
    c.labels = row.labels.filter((x): x is string => typeof x === 'string');
  }
  return c;
}
