// Custom React Flow node for the dependency graph (gm-e12.16). One
// per WorkItem; renders id + truncated title + a state pip. Click is
// owned by GraphPage via React Flow's onNodeClick — the node itself
// stays stateless so a 1000-card render doesn't pay the cost of 1000
// hooks.

import { Handle, Position } from 'reactflow';
import type { NodeProps } from 'reactflow';
import type { StateCategory } from '@/types/core.gen';
import { cn } from '@/lib/utils';

export interface WorkItemNodeData {
  id: string;
  title: string;
  stateCategory: StateCategory;
  // inCycle / onCriticalPath are pre-computed by GraphPage and pushed
  // onto each node's data. Keeping them in `data` (vs deriving in the
  // node component) lets React Flow's diff skip nodes whose flags
  // haven't changed when the cycle/critical-path mode toggles.
  inCycle?: boolean;
  onCriticalPath?: boolean;
}

const STATE_PIP: Record<StateCategory, string> = {
  backlog: 'bg-neutral-400',
  unstarted: 'bg-sky-400',
  staged: 'bg-violet-400',
  started: 'bg-amber-400',
  completed: 'bg-emerald-500',
  canceled: 'bg-neutral-300',
};

export function WorkItemNode({ data }: NodeProps<WorkItemNodeData>) {
  return (
    <div
      className={cn(
        'flex items-center gap-2 rounded-md border bg-white px-2 py-1.5 text-xs shadow-sm dark:bg-neutral-900',
        data.inCycle
          ? 'border-rose-500 ring-2 ring-rose-300 dark:ring-rose-800'
          : data.onCriticalPath
            ? 'border-amber-500 ring-2 ring-amber-300 dark:ring-amber-800'
            : 'border-neutral-300 dark:border-neutral-700'
      )}
      style={{ width: 200 }}
      data-testid={`graph-node-${data.id}`}
      data-in-cycle={data.inCycle || undefined}
      data-on-critical-path={data.onCriticalPath || undefined}
    >
      <Handle type="target" position={Position.Left} className="!bg-neutral-400" />
      <span
        className={cn('h-2 w-2 shrink-0 rounded-full', STATE_PIP[data.stateCategory])}
        aria-hidden
      />
      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-[10px] text-neutral-500">{data.id}</div>
        <div className="truncate font-medium text-neutral-800 dark:text-neutral-200">
          {data.title}
        </div>
      </div>
      <Handle type="source" position={Position.Right} className="!bg-neutral-400" />
    </div>
  );
}
