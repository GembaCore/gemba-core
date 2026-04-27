// SubprocessPanel — Workspace.kind=subprocess affordance set
// (gm-e12.15).
//
// Per ui-spec §5.18: surface a **process tree viewer**. The viewer
// is a hook site today (foolery-fit-gap §e12.15) — the panel
// formalises the layout so swapping in a live ps-tree is mechanical.

import { Box, GitFork } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export interface ProcessNode {
  pid: number;
  command: string;
  children?: ProcessNode[];
}

export function SubprocessPanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  const tree = readProcessTree(ws);
  return (
    <section
      data-testid="agent-detail-panel-subprocess"
      data-kind="subprocess"
      className="rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-tight">Subprocess</h2>
        <span className="rounded bg-teal-100 px-1.5 py-0.5 font-mono text-[10px] text-teal-700 dark:bg-teal-950 dark:text-teal-300">
          spawned
        </span>
      </header>

      <dl className="grid grid-cols-[6rem_1fr] gap-y-1 text-xs">
        <Field label="cwd" icon={<Box className="h-3 w-3" />}>
          <span className="font-mono">{ws.worktree_path || '(unset)'}</span>
        </Field>
      </dl>

      <div className="mt-4">
        <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-neutral-700 dark:text-neutral-300">
          <GitFork className="h-3.5 w-3.5" aria-hidden />
          Process tree
        </div>
        <div
          data-testid="agent-detail-process-tree"
          className="rounded border border-neutral-200 bg-neutral-50 p-2 font-mono text-[11px] dark:border-neutral-800 dark:bg-neutral-950"
        >
          {tree ? (
            <ProcessTreeView node={tree} depth={0} />
          ) : (
            <span className="text-neutral-500">
              (process tree streams in once the process-tree widget lands —
              foolery-fit-gap §e12.15)
            </span>
          )}
        </div>
      </div>
    </section>
  );
}

function ProcessTreeView({ node, depth }: { node: ProcessNode; depth: number }) {
  return (
    <div data-testid={`agent-detail-process-${node.pid}`}>
      <div style={{ paddingLeft: depth * 12 }}>
        <span className="text-neutral-500">{node.pid.toString().padStart(5, ' ')}</span>{' '}
        <span>{node.command}</span>
      </div>
      {node.children?.map((c) => (
        <ProcessTreeView key={c.pid} node={c} depth={depth + 1} />
      ))}
    </div>
  );
}

function readProcessTree(ws: NonNullable<AgentDetailPanelProps['ctx']['workspace']>): ProcessNode | null {
  const maybe = (ws as unknown as { process_tree?: ProcessNode }).process_tree;
  if (!maybe || typeof maybe.pid !== 'number' || typeof maybe.command !== 'string') {
    return null;
  }
  return maybe;
}

function Field({
  label,
  icon,
  children,
}: {
  label: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <>
      <dt className="flex items-center gap-1 text-neutral-500">
        {icon}
        {label}
      </dt>
      <dd>{children}</dd>
    </>
  );
}
