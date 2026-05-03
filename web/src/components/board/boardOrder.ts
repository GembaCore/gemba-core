import type { WorkItem } from '@/types/core.gen';

export type BoardOrderKey = 'created' | 'modified' | 'edited' | 'id';

export interface BoardOrderOption {
  key: BoardOrderKey;
  label: string;
}

export const BOARD_ORDER_OPTIONS: readonly BoardOrderOption[] = [
  { key: 'modified', label: 'Modified' },
  { key: 'created', label: 'Created' },
  { key: 'edited', label: 'Edited' },
  { key: 'id', label: 'ID' },
];

export function parseBoardOrderKey(raw: string | null | undefined): BoardOrderKey | null {
  if (raw === 'created' || raw === 'modified' || raw === 'edited' || raw === 'id') {
    return raw;
  }
  return null;
}

export function compareWorkItemsByOrder(orderKey: BoardOrderKey, a: WorkItem, b: WorkItem): number {
  switch (orderKey) {
    case 'created':
      return compareTimestampDesc(a.created_at, b.created_at) || compareID(a, b);
    case 'modified':
      return compareTimestampDesc(a.updated_at, b.updated_at) || compareID(a, b);
    case 'edited':
      // WorkItem currently exposes one persisted mutation timestamp.
      // Keep Edited as an operator-facing alias until Beads carries a
      // distinct edited_at field.
      return compareTimestampDesc(a.updated_at, b.updated_at) || compareID(a, b);
    case 'id':
      return compareID(a, b);
  }
}

export function sortWorkItems(
  items: readonly WorkItem[],
  orderKey: BoardOrderKey | null | undefined
): WorkItem[] {
  const copy = [...items];
  if (!orderKey) return copy;
  return copy.sort((a, b) => compareWorkItemsByOrder(orderKey, a, b));
}

function compareTimestampDesc(a: string | undefined, b: string | undefined): number {
  const ta = a ? Date.parse(a) : 0;
  const tb = b ? Date.parse(b) : 0;
  return (Number.isFinite(tb) ? tb : 0) - (Number.isFinite(ta) ? ta : 0);
}

function compareID(a: WorkItem, b: WorkItem): number {
  return a.id.localeCompare(b.id, undefined, { numeric: true, sensitivity: 'base' });
}
