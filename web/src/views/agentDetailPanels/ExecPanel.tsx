// ExecPanel — Workspace.kind=exec affordance set (gm-e12.15).
//
// Unscoped exec — the LangGraph default. Per ui-spec §5.18: surface
// **last command + exit code**. fs_scoped is NOT guaranteed here
// (see core.WorkspaceKind doc), so the panel calls that out so the
// operator knows the agent is running on the host.

import { Box, AlertTriangle, Terminal as TerminalIcon } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export interface ExecLastCommand {
  command: string;
  exit_code: number;
  finished_at?: string;
}

export function ExecPanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  // Adaptors stash exec history under provider-specific paths today;
  // the SPA reads it once a canonical wire field lands. For now we
  // surface a stub so the affordance shape is fixed.
  const last = readLastCommand(ws);
  return (
    <section
      data-testid="agent-detail-panel-exec"
      data-kind="exec"
      className="rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-tight">Exec</h2>
        <span className="rounded bg-amber-100 px-1.5 py-0.5 font-mono text-[10px] text-amber-700 dark:bg-amber-950 dark:text-amber-300">
          unscoped
        </span>
      </header>

      <div
        data-testid="agent-detail-exec-warning"
        className="mb-3 flex items-start gap-2 rounded border border-amber-300 bg-amber-50 px-2 py-1.5 text-[11px] text-amber-800 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200"
      >
        <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" aria-hidden />
        <span>
          Unscoped exec relies on the agent honouring $PWD; the kernel
          enforces no isolation here.
        </span>
      </div>

      <dl className="grid grid-cols-[6rem_1fr] gap-y-1 text-xs">
        <Field label="cwd" icon={<Box className="h-3 w-3" />}>
          <span className="font-mono">{ws.worktree_path || '(unset)'}</span>
        </Field>
        <Field label="Last cmd" icon={<TerminalIcon className="h-3 w-3" />}>
          <span
            data-testid="agent-detail-last-command"
            className="break-all font-mono text-neutral-600 dark:text-neutral-400"
          >
            {last ? last.command : '(no command run)'}
          </span>
        </Field>
        <Field label="Exit code">
          <span
            data-testid="agent-detail-exit-code"
            className={
              'inline-block rounded px-1.5 py-0.5 font-mono text-[10px] ' +
              (last && last.exit_code === 0
                ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
                : last
                  ? 'bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300'
                  : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400')
            }
          >
            {last ? last.exit_code : '—'}
          </span>
        </Field>
      </dl>
    </section>
  );
}

// readLastCommand looks for a provider-supplied last-command record
// on the workspace bag. Today the wire shape doesn't formalise this
// field; we tolerate either a typed record or a missing one. When
// the adaptor wire grows the field we replace this shim with a
// direct read.
function readLastCommand(ws: NonNullable<AgentDetailPanelProps['ctx']['workspace']>): ExecLastCommand | null {
  const maybe = (ws as unknown as { last_command?: ExecLastCommand }).last_command;
  if (!maybe || typeof maybe.command !== 'string' || typeof maybe.exit_code !== 'number') {
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
