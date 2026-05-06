import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';
import type { WorkItem } from '@/types/core.gen';

export interface BootstrapDraftApplyResult {
  target_database: string;
  created: string[];
  count: number;
}

export async function applyBootstrapDraft(
  items: WorkItem[],
  opts: { targetDatabase?: string; nonce: string }
): Promise<BootstrapDraftApplyResult> {
  return apiFetch<BootstrapDraftApplyResult>('/bootstrap/drafts/apply', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce,
    },
    body: JSON.stringify({
      target_database: opts.targetDatabase || 'active',
      items,
    }),
  });
}

export function parseDraftJSONL(raw: string): WorkItem[] {
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, index) => {
      try {
        return JSON.parse(line) as WorkItem;
      } catch (err) {
        throw new Error(`line ${index + 1}: ${(err as Error).message}`);
      }
    });
}

export function draftItemsToJSONL(items: WorkItem[]): string {
  return items.map((item) => JSON.stringify(item)).join('\n') + (items.length ? '\n' : '');
}
