import type { StateCategory, WorkItem } from '@/types/core.gen';

export type BoardColumnID = 'backlog' | 'ready' | 'started' | 'completed';

export interface BoardDisplayColumn {
  id: BoardColumnID;
  label: string;
  states: readonly StateCategory[];
  dropState: StateCategory;
}

const BACKLOG_COLUMN: BoardDisplayColumn = {
  id: 'backlog',
  label: 'Backlog',
  states: ['backlog'],
  dropState: 'backlog',
};

const EXECUTION_COLUMNS: readonly BoardDisplayColumn[] = [
  {
    id: 'ready',
    label: 'Ready',
    states: ['unstarted', 'staged'],
    dropState: 'staged',
  },
  {
    id: 'started',
    label: 'In Progress',
    states: ['started'],
    dropState: 'started',
  },
  {
    id: 'completed',
    label: 'Done',
    states: ['completed'],
    dropState: 'completed',
  },
];

export function visibleBoardColumns(showBacklog: boolean): readonly BoardDisplayColumn[] {
  return showBacklog ? [BACKLOG_COLUMN, ...EXECUTION_COLUMNS] : EXECUTION_COLUMNS;
}

export function groupItemsByBoardColumn(
  items: readonly WorkItem[],
  columns: readonly BoardDisplayColumn[],
): Record<BoardColumnID, WorkItem[]> {
  const out: Record<BoardColumnID, WorkItem[]> = {
    backlog: [],
    ready: [],
    started: [],
    completed: [],
  };
  for (const item of items) {
    const col = columns.find((c) => c.states.includes(item.state_category));
    if (col) out[col.id].push(item);
  }
  return out;
}
