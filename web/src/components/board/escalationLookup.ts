// Pure helpers that bucket the global open-escalation list by their
// target work-item id (gm-e11.3). The Board page calls
// buildEscalationsByItem once per render so each card's lookup is
// O(1) rather than re-filtering the full list per card. Escalations
// without a work_item_id (e.g. session-scoped permission prompts) are
// dropped from the map — they don't surface on cards.

import type { EscalationRequest } from '@/api/escalations';

export function buildEscalationsByItem(
  escalations: readonly EscalationRequest[] | undefined,
): Map<string, EscalationRequest[]> {
  const out = new Map<string, EscalationRequest[]>();
  if (!escalations) return out;
  for (const e of escalations) {
    if (e.state !== 'open') continue;
    const id = e.work_item_id;
    if (!id) continue;
    const list = out.get(id);
    if (list) list.push(e);
    else out.set(id, [e]);
  }
  return out;
}

// countEscalationsInScope counts open escalations whose work item id
// is in `scopeIDs`. When `scopeIDs` is undefined, every open
// escalation with a target work item is counted (board scope=all).
export function countEscalationsInScope(
  escalationsByItem: Map<string, EscalationRequest[]>,
  scopeIDs: Set<string> | undefined,
): number {
  let n = 0;
  for (const [id, list] of escalationsByItem) {
    if (scopeIDs && !scopeIDs.has(id)) continue;
    n += list.length;
  }
  return n;
}
