// WorktreePanel — Workspace.kind=worktree affordance set (gm-e12.15).
//
// Per ui-spec §5.18 + DD-5: a worktree-backed session gets an
// **Attach** button (the operator opens the host terminal at the
// worktree root). xterm-embed is a v1.1 follow-up (see
// foolery-fit-gap.md §e12.15); the stub here documents that hook
// site so the embed swap is mechanical.

import { GitBranch, Box, Terminal } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export function WorktreePanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  const path = ws.worktree_path ?? '';
  const repoBranch = `${ws.repository ?? '(no repo)'}/${ws.branch ?? '(no branch)'}`;
  return (
    <section
      data-testid="agent-detail-panel-worktree"
      data-kind="worktree"
      className="rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-tight">Worktree</h2>
        <span className="rounded bg-emerald-100 px-1.5 py-0.5 font-mono text-[10px] text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
          fs_scoped
        </span>
      </header>

      <dl className="grid grid-cols-[6rem_1fr] gap-y-1 text-xs">
        <Field label="Repo / branch" icon={<GitBranch className="h-3 w-3" />}>
          <span className="font-mono">{repoBranch}</span>
        </Field>
        <Field label="Worktree" icon={<Box className="h-3 w-3" />}>
          <span className="break-all font-mono text-neutral-600 dark:text-neutral-400">
            {path || '(unset)'}
          </span>
        </Field>
      </dl>

      <div className="mt-4 flex gap-2">
        <button
          type="button"
          data-testid="agent-detail-attach"
          // No-op stub: the real attach handler will spawn a terminal
          // at the worktree root. xterm.js embed is a follow-up
          // (foolery-fit-gap §e12.15).
          onClick={() => {
            /* attach is wired to a foolery shell affordance once the
               embed lands; for now this is a placeholder. */
          }}
          className="inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-xs text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          <Terminal className="h-3.5 w-3.5" aria-hidden />
          Attach
        </button>
      </div>
    </section>
  );
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
