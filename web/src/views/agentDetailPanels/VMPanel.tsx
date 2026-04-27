// VMPanel — Workspace.kind=vm affordance set (gm-e12.15).
//
// Full virtual machine. Operator affordances: SSH (terminal-style
// attach) + Console (out-of-band power view, useful when SSH is
// down). Snapshot/restore is surfaced when the manifest declares
// snapshot_restore on this kind.

import { Box, Server, Terminal, Monitor } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export function VMPanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  const snap = ws.isolation?.snapshot_restore ?? false;
  return (
    <section
      data-testid="agent-detail-panel-vm"
      data-kind="vm"
      className="rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-tight">Virtual machine</h2>
        <span className="rounded bg-purple-100 px-1.5 py-0.5 font-mono text-[10px] text-purple-700 dark:bg-purple-950 dark:text-purple-300">
          VM
        </span>
      </header>

      <dl className="grid grid-cols-[6rem_1fr] gap-y-1 text-xs">
        <Field label="Instance" icon={<Server className="h-3 w-3" />}>
          <span className="font-mono">{ws.id || '(unknown)'}</span>
        </Field>
        <Field label="Repo / branch" icon={<Box className="h-3 w-3" />}>
          <span className="font-mono">
            {ws.repository ?? '(no repo)'}/{ws.branch ?? '(no branch)'}
          </span>
        </Field>
      </dl>

      <div className="mt-4 flex gap-2">
        <button
          type="button"
          data-testid="agent-detail-ssh"
          className="inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-xs text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          <Terminal className="h-3.5 w-3.5" aria-hidden />
          SSH
        </button>
        <button
          type="button"
          data-testid="agent-detail-console"
          className="inline-flex items-center gap-1.5 rounded-md border border-neutral-300 bg-white px-3 py-1.5 text-xs hover:bg-neutral-50 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
        >
          <Monitor className="h-3.5 w-3.5" aria-hidden />
          Console
        </button>
      </div>

      {snap ? (
        <p
          data-testid="agent-detail-snapshot-hint"
          className="mt-3 text-[11px] text-neutral-500"
        >
          snapshot/restore: supported by this provider
        </p>
      ) : null}
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
