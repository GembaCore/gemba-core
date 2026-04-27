// K8sPodPanel — Workspace.kind=k8s_pod affordance set (gm-e12.15).
//
// Per ui-spec §5.18: **Peek + pod status + kubectl-exec button +
// logs stream**. The pod status block surfaces phase + ready
// containers; logs and peek are hook sites (see foolery-fit-gap
// §e12.15). The Peek button is the read-only inspect path; exec
// drops the operator into a shell.

import { Box, Cloud, ScrollText, Eye } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export function K8sPodPanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  // Pod status fields are stashed under workspace.id today; richer
  // status (phase / ready / restarts) lands when the orchestration
  // adaptor publishes it. Render whatever the adaptor surfaced;
  // unknown fields render as "—".
  return (
    <section
      data-testid="agent-detail-panel-k8s_pod"
      data-kind="k8s_pod"
      className="rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <header className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold tracking-tight">Kubernetes pod</h2>
        <span className="rounded bg-indigo-100 px-1.5 py-0.5 font-mono text-[10px] text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300">
          k8s
        </span>
      </header>

      <dl className="grid grid-cols-[6rem_1fr] gap-y-1 text-xs">
        <Field label="Pod" icon={<Cloud className="h-3 w-3" />}>
          <span className="font-mono">{ws.id || '(unknown)'}</span>
        </Field>
        <Field label="Repo / branch" icon={<Box className="h-3 w-3" />}>
          <span className="font-mono">
            {ws.repository ?? '(no repo)'}/{ws.branch ?? '(no branch)'}
          </span>
        </Field>
        <Field label="Phase">
          <PodStatusBadge />
        </Field>
      </dl>

      <div className="mt-4 flex gap-2">
        <button
          type="button"
          data-testid="agent-detail-peek"
          className="inline-flex items-center gap-1.5 rounded-md border border-neutral-300 bg-white px-3 py-1.5 text-xs hover:bg-neutral-50 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
        >
          <Eye className="h-3.5 w-3.5" aria-hidden />
          Peek
        </button>
        <button
          type="button"
          data-testid="agent-detail-kubectl-exec"
          className="inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-xs text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          kubectl exec
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
          (kubectl logs -f stream wires up with the logs widget —
          foolery-fit-gap §e12.15)
        </p>
      </div>
    </section>
  );
}

function PodStatusBadge() {
  // Placeholder — when the adaptor publishes pod phase the badge
  // colour switches on it. Today we surface "unknown" to make the
  // hook-site obvious.
  return (
    <span
      data-testid="agent-detail-pod-status"
      className="inline-block rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
    >
      unknown
    </span>
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
