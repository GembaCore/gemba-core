// ContainerPanel — Workspace.kind=container affordance set (gm-e12.15).
//
// OCI container (Docker / Podman / Railway). Per ui-spec §5.18:
// **docker-exec** button + **streaming logs** widget. Logs are a
// hook site for now (foolery-fit-gap §e12.15) — the panel reserves
// the layout slot so the streaming swap is mechanical.

import { Box, Container, ScrollText } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export function ContainerPanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  return (
    <section
      data-testid="agent-detail-panel-container"
      data-kind="container"
      className="rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-tight">Container</h2>
        <span className="rounded bg-sky-100 px-1.5 py-0.5 font-mono text-[10px] text-sky-700 dark:bg-sky-950 dark:text-sky-300">
          OCI
        </span>
      </header>

      <dl className="grid grid-cols-[6rem_1fr] gap-y-1 text-xs">
        <Field label="Container" icon={<Container className="h-3 w-3" />}>
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
          data-testid="agent-detail-docker-exec"
          className="inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-xs text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          docker exec
        </button>
      </div>

      <div
        data-testid="agent-detail-logs"
        className="mt-4 rounded border border-dashed border-neutral-300 p-3 text-xs text-neutral-500 dark:border-neutral-700"
      >
        <div className="mb-1 flex items-center gap-1.5 font-medium text-neutral-700 dark:text-neutral-300">
          <ScrollText className="h-3.5 w-3.5" aria-hidden />
          Streaming logs
        </div>
        <p className="font-mono text-[11px]">
          (log stream connects when the streaming-logs widget lands —
          foolery-fit-gap §e12.15)
        </p>
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
